// Les sons des conversations, comme Messenger (marque `sons_conversation`,
// serveur/PROTOCOLE.md §5.5) : un jeu (Bulle, Bois, Verre) pour l'envoi et la
// réception, plus « écrit » (les « … ») et l'émoji rapide, communs aux jeux.
//
// Seulement dans la conversation ouverte et visible, Jasette au premier plan.
// Les mp3 sont dans l'exécutable (assets/sons) ; rodio (MIT ou Apache-2.0)
// les décode avec symphonia à la première lecture et les joue par cpal (ALSA,
// donc PulseAudio ou PipeWire). Sans sortie audio : rien, sans erreur ni
// blocage (tout se passe dans un fil à part).
use super::*;
use std::sync::mpsc;

/// Le volume des sons (1 : celui des fichiers).
const VOLUME: f32 = 0.5;
/// Au plus un son de réception par 400 ms.
const ECART_RECU: Duration = Duration::from_millis(400);
/// Les « … » ne resonnent que s'ils avaient disparu depuis au moins 3 s.
const ECART_ECRIT: Duration = Duration::from_secs(3);
/// Un message plus vieux n'est pas « nouveau » (synchro, rattrapage).
const RECU_RECENT_MS: i64 = 120_000;

/// Les jeux, dans l'ordre des Réglages : (id, nom).
pub(crate) const JEUX: [(&str, &str); 4] = [("bulle", "Bulle"), ("bois", "Bois"), ("verre", "Verre"), ("aucun", "Aucun son")];

#[derive(Clone, Copy, PartialEq, Eq, Debug)]
pub(crate) enum Son {
    Envoye,
    Recu,
    Ecrit,
    Emoji,
}

const ENVOYE_BULLE: &[u8] = include_bytes!("../assets/sons/envoye-bulle.mp3");
const RECU_BULLE: &[u8] = include_bytes!("../assets/sons/recu-bulle.mp3");
const ENVOYE_BOIS: &[u8] = include_bytes!("../assets/sons/envoye-bois.mp3");
const RECU_BOIS: &[u8] = include_bytes!("../assets/sons/recu-bois.mp3");
const ENVOYE_VERRE: &[u8] = include_bytes!("../assets/sons/envoye-verre.mp3");
const RECU_VERRE: &[u8] = include_bytes!("../assets/sons/recu-verre.mp3");
const ECRIT: &[u8] = include_bytes!("../assets/sons/ecrit.mp3");
const EMOJI: &[u8] = include_bytes!("../assets/sons/emoji.mp3");

/// Le fichier d'un son dans un jeu ("aucun" : rien ; inconnu : "bulle").
fn fichier(jeu: &str, son: Son) -> Option<&'static [u8]> {
    Some(match (jeu, son) {
        ("aucun", _) => return None,
        (_, Son::Ecrit) => ECRIT,
        (_, Son::Emoji) => EMOJI,
        ("bois", Son::Envoye) => ENVOYE_BOIS,
        ("bois", Son::Recu) => RECU_BOIS,
        ("verre", Son::Envoye) => ENVOYE_VERRE,
        ("verre", Son::Recu) => RECU_VERRE,
        (_, Son::Envoye) => ENVOYE_BULLE,
        (_, Son::Recu) => RECU_BULLE,
    })
}

/// Le jeu rangé dans les réglages ("" : le défaut, "bulle").
pub(crate) fn jeu(reglage: &str) -> &str {
    match reglage {
        "bois" | "verre" | "aucun" => reglage,
        _ => "bulle",
    }
}

/// Joue un son du jeu, sans attendre. Le fil audio s'ouvre à la première
/// demande ; s'il n'y a pas de sortie, les demandes se perdent.
pub(crate) fn jouer(jeu: &str, son: Son) {
    let Some(octets) = fichier(jeu, son) else { return };
    static FIL: std::sync::OnceLock<Mutex<mpsc::Sender<&'static [u8]>>> = std::sync::OnceLock::new();
    let fil = FIL.get_or_init(|| {
        let (tx, rx) = mpsc::channel::<&'static [u8]>();
        let _ = std::thread::Builder::new().name("sons".into()).spawn(move || lecteur(rx));
        Mutex::new(tx)
    });
    let _ = fil.lock().unwrap_or_else(|e| e.into_inner()).send(octets);
}

/// Le fil audio : la sortie par défaut, et chaque son décodé une fois.
fn lecteur(rx: mpsc::Receiver<&'static [u8]>) {
    use rodio::Source;
    let sortie = match rodio::DeviceSinkBuilder::from_default_device()
        .and_then(|b| b.with_error_callback(|_| {}).open_sink_or_fallback())
    {
        Ok(mut s) => {
            s.log_on_drop(false);
            s
        }
        Err(e) => {
            eprintln!("sons : pas de sortie audio ({e})");
            // Les demandes suivantes se perdent sans bruit.
            for _ in rx {}
            return;
        }
    };
    let mut decodes: HashMap<usize, rodio::source::Buffered<rodio::Decoder<std::io::Cursor<&'static [u8]>>>> = HashMap::new();
    for octets in rx {
        let cle = octets.as_ptr() as usize;
        if !decodes.contains_key(&cle) {
            match rodio::Decoder::new_mp3(std::io::Cursor::new(octets)) {
                Ok(d) => {
                    decodes.insert(cle, d.buffered());
                }
                Err(e) => {
                    eprintln!("sons : mp3 illisible ({e})");
                    continue;
                }
            }
        }
        if let Some(d) = decodes.get(&cle) {
            sortie.mixer().add(d.clone().amplify(VOLUME));
        }
    }
}

/// Ce qu'il faut retenir entre deux sons.
#[derive(Default)]
pub(crate) struct Memoire {
    dernier_recu: Option<Instant>,
    /// Quand les « … » de chaque conversation ont disparu.
    ecrit_parti: HashMap<String, Instant>,
}

impl Jasette {
    /// Le jeu choisi ("aucun" : silence).
    pub(crate) fn jeu_sons(&self) -> &str {
        jeu(&self.reglages.sons_conversation)
    }

    /// La conversation `conv` est-elle ouverte et visible, Jasette au premier plan ?
    fn sons_ici(&self, conv: &str) -> bool {
        self.active && self.section.is_none() && self.choisie.as_deref() == Some(conv) && self.jeu_sons() != "aucun"
    }

    /// Mon message part (envoi local) ; `emoji` : l'émoji rapide.
    pub(crate) fn son_envoi(&mut self, conv: &str, emoji: bool) {
        if self.sons_ici(conv) {
            jouer(self.jeu_sons(), if emoji { Son::Emoji } else { Son::Envoye });
        }
    }

    /// Un nouveau message reçu (pas l'historique, ni une réaction) d'heure `temps` (ms).
    pub(crate) fn son_recu(&mut self, conv: &str, temps: i64) {
        if !self.sons_ici(conv) || temps < maintenant_ms() - RECU_RECENT_MS {
            return;
        }
        if self.memoire_sons.dernier_recu.is_some_and(|t| t.elapsed() < ECART_RECU) {
            return;
        }
        self.memoire_sons.dernier_recu = Some(Instant::now());
        jouer(self.jeu_sons(), Son::Recu);
    }

    /// Les « … » apparaissent dans `conv`.
    pub(crate) fn son_ecrit(&mut self, conv: &str) {
        let recent = self.memoire_sons.ecrit_parti.get(conv).is_some_and(|t| t.elapsed() < ECART_ECRIT);
        if self.sons_ici(conv) && !recent {
            jouer(self.jeu_sons(), Son::Ecrit);
        }
    }

    /// Les « … » de `conv` viennent de disparaître.
    pub(crate) fn ecrit_parti(&mut self, conv: &str) {
        let m = &mut self.memoire_sons.ecrit_parti;
        m.retain(|_, t| t.elapsed() < ECART_ECRIT);
        m.insert(conv.to_string(), Instant::now());
    }

    /// Réglages > Sons : choisir un jeu le fait entendre (l'envoi, puis la
    /// réception une demi-seconde après), même ici.
    pub(crate) fn choisir_sons(&mut self, j: &str, cx: &mut Context<Self>) {
        self.reglages.sons_conversation = if j == "bulle" { String::new() } else { j.to_string() };
        self.reglages.garder();
        self.marques_changees();
        if j != "aucun" {
            let j = j.to_string();
            jouer(&j, Son::Envoye);
            cx.spawn(async move |_, cx| {
                cx.background_executor().timer(Duration::from_millis(500)).await;
                jouer(&j, Son::Recu);
            })
            .detach();
        }
        cx.notify();
    }

    /// Réglages > Sons : un bouton radio par jeu.
    pub(crate) fn reglages_sons(&self, cx: &mut Context<Self>) -> AnyElement {
        let choisi = self.jeu_sons().to_string();
        let carre = th().ega || retro::retro();
        let mut g = groupe();
        for (i, (id, nom)) in JEUX.into_iter().enumerate() {
            let actif = choisi == id;
            if i > 0 {
                g = g.child(separateur());
            }
            let rond = div()
                .size(px(18.))
                .flex_none()
                .flex()
                .items_center()
                .justify_center()
                .when(!carre, |d| d.rounded(px(9.)))
                .border_2()
                .border_color(if actif { th().accent } else { th().texte_pale })
                .when(actif, |d| d.child(div().size(px(8.)).when(!carre, |d| d.rounded(px(4.))).bg(th().accent)));
            g = g.child(
                div()
                    .id(SharedString::from(format!("sons-{id}")))
                    .flex()
                    .flex_row()
                    .items_center()
                    .gap(px(12.))
                    .px(px(16.))
                    .py(px(12.))
                    .cursor_pointer()
                    .when(i == 0, |d| d.rounded_t(rayon(15.)))
                    .when(i == JEUX.len() - 1, |d| d.rounded_b(rayon(15.)))
                    .hover(|s| s.bg(th().survol))
                    .on_click(cx.listener(move |this, _, _, cx| this.choisir_sons(id, cx)))
                    .child(rond)
                    .child(div().flex_1().text_color(th().texte_fort).child(t(nom))),
            );
        }
        carte_section("Sons")
            .child(
                div()
                    .px(px(4.))
                    .pb(px(8.))
                    .text_size(px(13.))
                    .text_color(th().texte_pale)
                    .child(t("Dans la conversation ouverte : à l'envoi, à la réception et quand on vous écrit.")),
            )
            .child(g)
            .into_any_element()
    }
}

#[cfg(test)]
mod essais {
    use super::*;

    #[test]
    fn fichiers() {
        assert_eq!(jeu(""), "bulle");
        assert_eq!(jeu("klaxon"), "bulle");
        assert_eq!(jeu("verre"), "verre");
        assert!(fichier("aucun", Son::Recu).is_none());
        assert_eq!(fichier("bois", Son::Envoye), Some(ENVOYE_BOIS));
        assert_eq!(fichier("bulle", Son::Ecrit), Some(ECRIT));
        assert_eq!(fichier("verre", Son::Emoji), Some(EMOJI));
        // Chaque mp3 se décode.
        for o in [ENVOYE_BULLE, RECU_BULLE, ENVOYE_BOIS, RECU_BOIS, ENVOYE_VERRE, RECU_VERRE, ECRIT, EMOJI] {
            let d = rodio::Decoder::new_mp3(std::io::Cursor::new(o)).expect("mp3");
            assert!(d.count() > 1000);
        }
    }
}
