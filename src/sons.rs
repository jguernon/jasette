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

/// Le volume au cran 100 % (1 : celui des fichiers).
const VOLUME: f32 = 0.5;
/// Les crans du curseur de volume (Réglages > Sons), en % du volume d'avant
/// le curseur : 100 % le garde ; jusqu'à 200 % pour monter (Bulle, Bois et
/// Verre ont été égalisés plus bas), 0 % coupe tout sans oublier le jeu.
pub(crate) const CRANS_VOLUME: [u32; 9] = [0, 25, 50, 75, 100, 125, 150, 175, 200];
/// Le cran par défaut.
pub(crate) fn volume_defaut() -> u32 {
    100
}
/// La crête permise après le gain : -1 dBFS. Un son qui la dépasserait est
/// baissé juste assez (un limiteur de crête par son : jamais de saturation).
const CRETE_MAX: f32 = 0.891;
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

/// Le gain d'un cran de volume (en %), sur une courbe au carré, proche de
/// l'oreille : 25 % ≈ -24 dB, 50 % ≈ -12 dB, 75 % ≈ -5 dB, 100 % : le volume
/// d'avant, 150 % ≈ +7 dB, 200 % ≈ +12 dB.
pub(crate) fn gain(volume: u32) -> f32 {
    let r = volume.min(200) as f32 / 100.;
    VOLUME * r * r
}

/// Le gain, plafonné pour qu'un son de crête `crete` (0 à 1) ne sature pas.
fn gain_limite(gain: f32, crete: f32) -> f32 {
    if crete > 0. { gain.min(CRETE_MAX / crete) } else { gain }
}

/// Le cran le plus proche d'un volume (une valeur venue d'ailleurs).
pub(crate) fn cran_volume(volume: u32) -> usize {
    CRANS_VOLUME.iter().enumerate().min_by_key(|(_, v)| v.abs_diff(volume)).map(|(i, _)| i).unwrap_or(4)
}

/// Joue un son du jeu au `volume` (un cran, en %), sans attendre. Le fil
/// audio s'ouvre à la première demande ; s'il n'y a pas de sortie, les
/// demandes se perdent.
pub(crate) fn jouer(jeu: &str, son: Son, volume: u32) {
    let Some(octets) = fichier(jeu, son) else { return };
    if volume == 0 {
        return;
    }
    static FIL: std::sync::OnceLock<Mutex<mpsc::Sender<(&'static [u8], f32)>>> = std::sync::OnceLock::new();
    let fil = FIL.get_or_init(|| {
        let (tx, rx) = mpsc::channel::<(&'static [u8], f32)>();
        let _ = std::thread::Builder::new().name("sons".into()).spawn(move || lecteur(rx));
        Mutex::new(tx)
    });
    let _ = fil.lock().unwrap_or_else(|e| e.into_inner()).send((octets, gain(volume)));
}

/// Le fil audio : la sortie par défaut, et chaque son décodé une fois (avec sa crête).
fn lecteur(rx: mpsc::Receiver<(&'static [u8], f32)>) {
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
    type Decode = rodio::source::Buffered<rodio::Decoder<std::io::Cursor<&'static [u8]>>>;
    let mut decodes: HashMap<usize, (Decode, f32)> = HashMap::new();
    for (octets, gain) in rx {
        let cle = octets.as_ptr() as usize;
        if !decodes.contains_key(&cle) {
            match rodio::Decoder::new_mp3(std::io::Cursor::new(octets)) {
                Ok(d) => {
                    let d = d.buffered();
                    let crete = d.clone().fold(0f32, |m, x| m.max(x.abs()));
                    decodes.insert(cle, (d, crete));
                }
                Err(e) => {
                    eprintln!("sons : mp3 illisible ({e})");
                    continue;
                }
            }
        }
        if let Some((d, crete)) = decodes.get(&cle) {
            sortie.mixer().add(d.clone().amplify(gain_limite(gain, *crete)));
        }
    }
}

/// Ce qu'il faut retenir entre deux sons.
#[derive(Default)]
pub(crate) struct Memoire {
    dernier_recu: Option<Instant>,
    /// Quand les « … » de chaque conversation ont disparu.
    ecrit_parti: HashMap<String, Instant>,
    /// Le bouton du curseur de volume est tenu (l'aperçu part au relâché).
    volume_tenu: bool,
}

impl Jasette {
    /// Le jeu choisi ("aucun" : silence).
    pub(crate) fn jeu_sons(&self) -> &str {
        jeu(&self.reglages.sons_conversation)
    }

    /// La conversation `conv` est-elle ouverte et visible, Jasette au premier plan ?
    fn sons_ici(&self, conv: &str) -> bool {
        self.active && self.section.is_none() && self.choisie.as_deref() == Some(conv) && self.jeu_sons() != "aucun"
            && self.reglages.volume_sons > 0
    }

    /// Mon message part (envoi local) ; `emoji` : l'émoji rapide.
    pub(crate) fn son_envoi(&mut self, conv: &str, emoji: bool) {
        if self.sons_ici(conv) {
            jouer(self.jeu_sons(), if emoji { Son::Emoji } else { Son::Envoye }, self.reglages.volume_sons);
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
        jouer(self.jeu_sons(), Son::Recu, self.reglages.volume_sons);
    }

    /// Les « … » apparaissent dans `conv`.
    pub(crate) fn son_ecrit(&mut self, conv: &str) {
        let recent = self.memoire_sons.ecrit_parti.get(conv).is_some_and(|t| t.elapsed() < ECART_ECRIT);
        if self.sons_ici(conv) && !recent {
            jouer(self.jeu_sons(), Son::Ecrit, self.reglages.volume_sons);
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
            let v = self.reglages.volume_sons;
            jouer(&j, Son::Envoye, v);
            cx.spawn(async move |_, cx| {
                cx.background_executor().timer(Duration::from_millis(500)).await;
                jouer(&j, Son::Recu, v);
            })
            .detach();
        }
        cx.notify();
    }

    /// Réglages > Sons : poser le cran `i` du volume (vrai s'il a changé).
    /// Un réglage de cet appareil (gardé dans reglages.json, jamais partagé :
    /// les haut-parleurs du PC et du téléphone diffèrent).
    fn poser_volume(&mut self, i: usize) -> bool {
        self.memoire_sons.volume_tenu = true;
        let v = CRANS_VOLUME[i.min(CRANS_VOLUME.len() - 1)];
        if self.reglages.volume_sons == v {
            return false;
        }
        self.reglages.volume_sons = v;
        true
    }

    /// Le bouton du volume relâché : gardé, et l'aperçu (le son reçu du jeu).
    fn relacher_volume(&mut self) {
        if !std::mem::take(&mut self.memoire_sons.volume_tenu) {
            return;
        }
        self.reglages.garder();
        jouer(self.jeu_sons(), Son::Recu, self.reglages.volume_sons);
    }

    /// Le curseur de volume : un haut-parleur, la piste aimantée aux crans
    /// avec ses encoches dessous (0, 100 et 200 marqués), la valeur à droite.
    fn curseur_volume(&self, cx: &mut Context<Self>) -> Div {
        let volume = self.reglages.volume_sons;
        let choisi = cran_volume(volume);
        let n = CRANS_VOLUME.len();
        let ega = th().ega || retro::retro();
        let vide = hsla(0., 0., 0., 0.);
        let mut piste = div()
            .flex_1()
            .min_w(px(0.))
            .flex()
            .flex_row()
            .on_mouse_up(MouseButton::Left, cx.listener(|this, _, _, cx| {
                this.relacher_volume();
                cx.notify();
            }))
            .on_mouse_up_out(MouseButton::Left, cx.listener(|this, _, _, cx| {
                this.relacher_volume();
                cx.notify();
            }));
        for (i, v) in CRANS_VOLUME.into_iter().enumerate() {
            let plein = i < choisi;
            let gauche = if i == 0 { vide } else if i <= choisi { th().accent } else { th().trait_fin };
            let droite = if i + 1 == n { vide } else if plein { th().accent } else { th().trait_fin };
            let majeur = v % 100 == 0;
            // L'encoche : un trait fin, ou un bloc de pixels en EGA ; plus longue aux centaines.
            let encoche = div()
                .w(px(if ega { 4. } else { 2. }))
                .h(px(if majeur { 8. } else { 4. }))
                .when(!ega, |d| d.rounded(px(1.)))
                .bg(if i <= choisi { th().accent } else { th().texte_pale });
            let bouton = if i == choisi {
                Some(
                    div()
                        .absolute()
                        .top(px(4.))
                        .size(px(20.))
                        .rounded(rayon(999.))
                        .bg(th().accent)
                        .border_2()
                        .border_color(if ega { th().texte_fort } else { blanc(1.) })
                        .when(!ega, |d| d.shadow(ombre_flottante())),
                )
            } else {
                None
            };
            piste = piste.child(
                div()
                    .id(("volume", i))
                    .flex_1()
                    .flex()
                    .flex_col()
                    .items_center()
                    .cursor_pointer()
                    .on_mouse_down(MouseButton::Left, cx.listener(move |this, _, _, cx| {
                        this.poser_volume(i);
                        cx.notify();
                    }))
                    .on_mouse_move(cx.listener(move |this, ev: &gpui::MouseMoveEvent, _, cx| {
                        if ev.pressed_button == Some(MouseButton::Left) && this.memoire_sons.volume_tenu && this.poser_volume(i) {
                            cx.notify();
                        }
                    }))
                    .child(
                        div()
                            .relative()
                            .w_full()
                            .h(px(28.))
                            .flex()
                            .justify_center()
                            .child(div().absolute().left_0().w(gpui::relative(0.5)).top(px(12.)).h(px(4.)).bg(gauche))
                            .child(div().absolute().right_0().w(gpui::relative(0.5)).top(px(12.)).h(px(4.)).bg(droite))
                            .children(bouton),
                    )
                    .child(div().h(px(8.)).flex().items_start().child(encoche))
                    .child(
                        div()
                            .h(px(14.))
                            .text_size(px(10.))
                            .text_color(th().texte_pale)
                            .when(majeur, |d| d.child(format!("{v}"))),
                    ),
            );
        }
        let haut_parleur = match volume {
            0 => "volume-x",
            1..=75 => "volume-1",
            _ => "volume-2",
        };
        div()
            .flex()
            .flex_row()
            .items_start()
            .gap(px(12.))
            .px(px(16.))
            .pt(px(10.))
            .pb(px(4.))
            .child(div().pt(px(6.)).child(icone(haut_parleur, 18., if volume > 0 { th().texte_fort } else { th().texte_pale })))
            .child(piste)
            .child(
                div()
                    .flex_none()
                    .w(px(52.))
                    .pt(px(4.))
                    .text_right()
                    .font_weight(FontWeight::SEMIBOLD)
                    .text_color(if volume > 0 { th().accent } else { th().texte_pale })
                    .child(if volume > 0 { format!("{volume} %") } else { t("Muet").to_string() }),
            )
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
            .child(
                div()
                    .px(px(4.))
                    .pt(px(18.))
                    .pb(px(8.))
                    .text_size(px(13.))
                    .text_color(th().texte_pale)
                    .child(t("Volume sur cet appareil. Chaque cran relâché fait entendre le son reçu.")),
            )
            .child(groupe().child(self.curseur_volume(cx)))
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
        // Le volume : 100 % garde l'ancien, la courbe monte au carré, le plafond tient.
        assert_eq!(gain(volume_defaut()), VOLUME);
        assert_eq!(gain(0), 0.);
        assert_eq!(gain(200), VOLUME * 4.);
        assert!(CRANS_VOLUME.windows(2).all(|w| gain(w[0]) < gain(w[1])));
        assert_eq!(gain_limite(2., 0.53), CRETE_MAX / 0.53);
        assert_eq!(gain_limite(0.5, 0.1), 0.5);
        assert_eq!(cran_volume(110), 4);
        assert_eq!(CRANS_VOLUME[cran_volume(999)], 200);
        // Chaque mp3 se décode.
        for o in [ENVOYE_BULLE, RECU_BULLE, ENVOYE_BOIS, RECU_BOIS, ENVOYE_VERRE, RECU_VERRE, ECRIT, EMOJI] {
            let d = rodio::Decoder::new_mp3(std::io::Cursor::new(o)).expect("mp3");
            assert!(d.count() > 1000);
        }
    }
}
