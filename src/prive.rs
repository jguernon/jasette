// La Liste privée (bureau) : des conversations cachées partout dans l'app
// (boîte, Head, favoris, épinglées, archives, recherche, médias, non-lus,
// notifications), qu'on rouvre dans la vue « Privées » avec un code à
// 6 chiffres.
//
// - La liste des conversations privées est une marque (`privees`, PROTOCOLE.md
//   §5.5), partagée par le serveur Jasette comme le Head ou les favoris.
// - Le code, lui, reste sur cet appareil : jamais synchronisé ni envoyé. Seul
//   son haché est gardé (PBKDF2-HMAC-SHA256, sel aléatoire de 16 octets), dans
//   <données>/prive.json (0600), avec le compte des mauvais codes.
// - La vue Privées se reverrouille quand la fenêtre perd le focus, ou après
//   1 minute sans activité (clic, touche, souris). Après 5 mauvais codes :
//   attente croissante (30 s, puis le double à chaque erreur, 1 h au plus).
// - Aucune notification pour une conversation privée (voir `notifier`).
// - Entrées : Ctrl+Maj+P, Réglages > Liste privée, et `jasette --prive`
//   (le lanceur « Jasette (privé) », posé seulement quand la liste est activée).
use super::*;
use base64::Engine as _;
use ring::pbkdf2;
use ring::rand::{SecureRandom, SystemRandom};
use std::num::NonZeroU32;

const ITERATIONS: u32 = 310_000;
const CHIFFRES: usize = 6;
/// Sans activité pendant ce temps, la vue Privées se reverrouille.
const DELAI_VERROU: Duration = Duration::from_secs(60);
/// Mauvais codes permis avant d'attendre.
const ESSAIS_LIBRES: u32 = 5;

fn tr(fr: &'static str, en: &'static str) -> &'static str {
    if anglais() { en } else { fr }
}

fn b64() -> base64::engine::GeneralPurpose {
    base64::engine::general_purpose::STANDARD
}

/// <données>/prive.json : le haché du code et le compte des erreurs. Absent
/// (ou sans haché) : la Liste privée n'est pas activée sur cet appareil.
#[derive(Clone, Debug, Default, Serialize, Deserialize)]
pub(crate) struct FichierPrive {
    #[serde(default)]
    sel: String,
    #[serde(default)]
    hache: String,
    #[serde(default)]
    iterations: u32,
    /// Mauvais codes de suite (gardés : relancer l'app ne les efface pas).
    #[serde(default)]
    echecs: u32,
    /// Heure (ms) avant laquelle aucun code n'est accepté.
    #[serde(default)]
    attente_jusqu: i64,
}

fn chemin_prive() -> PathBuf {
    dossier_donnees().join("prive.json")
}

impl FichierPrive {
    pub(crate) fn charger() -> Self {
        std::fs::read_to_string(chemin_prive())
            .ok()
            .and_then(|t| serde_json::from_str(&t).ok())
            .unwrap_or_default()
    }

    fn garder(&self) {
        let chemin = chemin_prive();
        if !self.actif() {
            let _ = std::fs::remove_file(&chemin);
            return;
        }
        let _ = std::fs::create_dir_all(dossier_donnees());
        if let Ok(texte) = serde_json::to_vec_pretty(self)
            && let Err(e) = ecrire_atomique(&chemin, &texte)
        {
            eprintln!("jasette : prive.json : {e}");
        }
    }

    pub(crate) fn actif(&self) -> bool {
        !self.hache.is_empty() && !self.sel.is_empty()
    }

    fn poser_code(&mut self, code: &str) {
        let mut sel = [0u8; 16];
        if SystemRandom::new().fill(&mut sel).is_err() {
            eprintln!("jasette : pas d'aléa pour le sel du code");
            return;
        }
        let mut hache = [0u8; 32];
        let n = NonZeroU32::new(ITERATIONS).expect("itérations");
        pbkdf2::derive(pbkdf2::PBKDF2_HMAC_SHA256, n, &sel, code.as_bytes(), &mut hache);
        *self = FichierPrive {
            sel: b64().encode(sel),
            hache: b64().encode(hache),
            iterations: ITERATIONS,
            echecs: 0,
            attente_jusqu: 0,
        };
    }

    /// Comparaison en temps constant (ring).
    fn verifier(&self, code: &str) -> bool {
        let (Ok(sel), Ok(hache)) = (b64().decode(&self.sel), b64().decode(&self.hache)) else { return false };
        let Some(n) = NonZeroU32::new(self.iterations.max(1)) else { return false };
        pbkdf2::verify(pbkdf2::PBKDF2_HMAC_SHA256, n, &sel, code.as_bytes(), &hache).is_ok()
    }

    /// Les secondes à attendre avant le prochain essai (0 : on peut).
    fn attente(&self) -> i64 {
        let reste = self.attente_jusqu - maintenant_ms();
        if reste > 0 { (reste + 999) / 1000 } else { 0 }
    }

    fn echec(&mut self) {
        self.echecs += 1;
        if self.echecs >= ESSAIS_LIBRES {
            let n = (self.echecs - ESSAIS_LIBRES).min(7);
            let secondes = (30i64 << n).min(3600);
            self.attente_jusqu = maintenant_ms() + secondes * 1000;
        }
        self.garder();
    }

    fn reussite(&mut self) {
        if self.echecs > 0 || self.attente_jusqu > 0 {
            self.echecs = 0;
            self.attente_jusqu = 0;
            self.garder();
        }
    }
}

/// Où en est le pavé du code.
#[derive(Clone, Debug, PartialEq, Eq)]
pub(crate) enum Etape {
    /// Ouvrir la vue Privées.
    Ouvrir,
    /// Activer : choisir le code, puis le redonner.
    Nouveau,
    Confirmer(String),
    /// Changer : l'ancien, le nouveau, encore le nouveau.
    Ancien,
    ChangerNouveau,
    ChangerConfirmer(String),
    /// Désactiver : le code, puis (s'il reste des privées) quoi en faire.
    Desactiver,
    QueFaire,
    ConfirmerRemettre,
}

#[derive(Clone, Debug)]
pub(crate) struct Pave {
    pub(crate) etape: Etape,
    saisie: String,
    message: Option<&'static str>,
}

#[derive(Default)]
pub(crate) struct EtatPrive {
    pub(crate) fichier: FichierPrive,
    /// La vue Privées est ouverte (bon code donné, pas encore reverrouillée).
    pub(crate) deverrouille: bool,
    pub(crate) activite: Option<Instant>,
    pub(crate) pave: Option<Pave>,
}

impl EtatPrive {
    pub(crate) fn charger() -> Self {
        EtatPrive { fichier: FichierPrive::charger(), ..Default::default() }
    }
}

/// Le lanceur « Jasette (privé) » (bonus du bureau) : posé dans
/// ~/.local/share/applications quand la liste est activée, retiré avec elle.
fn dossier_applis() -> Option<PathBuf> {
    std::env::var_os("XDG_DATA_HOME")
        .map(PathBuf::from)
        .or_else(|| std::env::var_os("HOME").map(|h| PathBuf::from(h).join(".local/share")))
}

const ICONE_PRIVEE: &[u8] = include_bytes!("../assets/png/jasette-prive-256.png");

fn poser_lanceur(oui: bool) {
    // Windows : pas de lanceur .desktop (« jasette --prive » reste possible).
    #[cfg(target_os = "windows")]
    return;
    let Some(donnees) = dossier_applis() else { return };
    let lanceur = donnees.join("applications/jasette-prive.desktop");
    let icone = donnees.join("icons/hicolor/256x256/apps/jasette-prive.png");
    if !oui {
        let _ = std::fs::remove_file(&lanceur);
        let _ = std::fs::remove_file(&icone);
        return;
    }
    // (Un binaire remplacé pendant que l'app tourne : Linux ajoute « (deleted) ».)
    let exe = std::env::current_exe()
        .map(|p| p.display().to_string().trim_end_matches(" (deleted)").to_string())
        .unwrap_or_else(|_| "jasette".into());
    let texte = format!(
        "[Desktop Entry]\nType=Application\nName=Jasette (privé)\nName[en]=Jasette (private)\n\
         Exec=\"{exe}\" --prive\nIcon=jasette-prive\nTerminal=false\nCategories=Network;InstantMessaging;\n\
         StartupWMClass=jasette\nNoDisplay=false\n"
    );
    if let Some(d) = icone.parent()
        && std::fs::create_dir_all(d).is_ok()
    {
        let _ = std::fs::write(&icone, ICONE_PRIVEE);
    }
    if let Some(d) = lanceur.parent()
        && std::fs::create_dir_all(d).is_ok()
    {
        let _ = std::fs::write(&lanceur, texte);
    }
}

impl Jasette {
    /// Cette conversation est-elle dans la Liste privée (de n'importe quel appareil) ?
    pub(crate) fn privee(&self, id: &str) -> bool {
        self.reglages.privees.iter().any(|x| x == id)
    }

    /// La vue Privées est-elle ouverte et déverrouillée ?
    pub(crate) fn vue_privee(&self) -> bool {
        self.vue == Vue::Privees && self.prive.deverrouille
    }

    /// Les conversations de la vue Privées : chargées, archivées ou en cache.
    pub(crate) fn liste_privees(&self) -> Vec<&Conv> {
        if !self.prive.deverrouille {
            return Vec::new();
        }
        let mut vues = HashSet::new();
        let mut l: Vec<&Conv> = self
            .convs
            .iter()
            .chain(self.archives.iter())
            .chain(self.fantomes.iter())
            .filter(|c| self.privee(&c.id) && vues.insert(c.id.as_str()))
            .collect();
        l.sort_by(|a, b| b.time.cmp(&a.time));
        l
    }

    /// Ouvre le pavé (ou, déjà déverrouillée, la vue tout de suite).
    pub(crate) fn ouvrir_privees(&mut self) {
        if !self.prive.fichier.actif() {
            self.section = Some(Section::Prive);
            return;
        }
        if self.prive.deverrouille {
            self.aller_privees();
            return;
        }
        self.prive.pave = Some(Pave { etape: Etape::Ouvrir, saisie: String::new(), message: None });
    }

    fn aller_privees(&mut self) {
        self.prive.activite = Some(Instant::now());
        self.section = None;
        self.galerie = None;
        self.vue = Vue::Privees;
        if self.choisie.as_deref().is_some_and(|id| !self.privee(id)) {
            self.choisie = None;
        }
    }

    pub(crate) fn pave_reglages(&mut self, etape: Etape) {
        self.prive.pave = Some(Pave { etape, saisie: String::new(), message: None });
    }

    /// Referme la vue Privées : plus rien de privé à l'écran.
    pub(crate) fn verrouiller(&mut self) {
        if !self.prive.deverrouille && self.vue != Vue::Privees {
            return;
        }
        self.prive.deverrouille = false;
        self.prive.activite = None;
        if self.vue == Vue::Privees {
            self.vue = Vue::Tout;
        }
        self.cacher_privees_ouvertes();
    }

    /// Une conversation devenue privée (ici ou sur un autre appareil), ou la
    /// vue refermée : la conversation ouverte et ses médias quittent l'écran.
    pub(crate) fn cacher_privees_ouvertes(&mut self) {
        if self.vue_privee() {
            return;
        }
        if self.choisie.as_deref().is_some_and(|id| self.privee(id)) {
            self.choisie = None;
            self.menu_conv = false;
            self.fil_cherche_ouvert = false;
        }
        if self.galerie.as_ref().and_then(|g| g.as_deref()).is_some_and(|id| self.privee(id)) {
            self.galerie = None;
        }
        let privees = self.reglages.privees.clone();
        self.resultats_msgs.retain(|r| !privees.contains(&r.conv));
    }

    /// Toutes les 50 ms : reverrouille après 1 minute sans activité. Vrai s'il faut redessiner.
    pub(crate) fn prive_tic(&mut self) -> bool {
        if self.prive.deverrouille && self.prive.activite.is_none_or(|t| t.elapsed() >= DELAI_VERROU) {
            self.verrouiller();
            return true;
        }
        // Le compte à rebours de l'attente, une fois par seconde.
        if self.prive.pave.is_some() && self.prive.fichier.attente_jusqu > 0 {
            static DERNIER: AtomicU8 = AtomicU8::new(0);
            let s = (maintenant_ms() / 1000 % 250) as u8;
            return DERNIER.swap(s, Ordering::Relaxed) != s;
        }
        false
    }

    pub(crate) fn prive_activite(&mut self) {
        if self.prive.deverrouille {
            self.prive.activite = Some(Instant::now());
        }
    }

    /// Rendre privée (ou ne plus l'être). Hors de la vue Privées, elle quitte l'écran.
    pub(crate) fn rendre_privee(&mut self, id: String, oui: bool) {
        if oui == self.privee(&id) {
            return;
        }
        if oui {
            self.reglages.privees.push(id.clone());
        } else {
            self.reglages.privees.retain(|x| x != &id);
        }
        if self.choisie.as_deref() == Some(id.as_str()) {
            self.choisie = None;
        }
        self.menu_conv = false;
        self.marques_changees();
        self.cacher_privees_ouvertes();
    }

    /// Une touche, quand le pavé est ouvert : vrai si elle est prise.
    pub(crate) fn prive_touche(&mut self, k: &gpui::Keystroke) -> bool {
        let Some(pave) = self.prive.pave.as_mut() else { return false };
        if k.key == "escape" {
            self.prive.pave = None;
            return true;
        }
        if k.modifiers.control || k.modifiers.alt || k.modifiers.platform {
            return true;
        }
        if k.key == "backspace" {
            pave.saisie.pop();
            return true;
        }
        let ch = k.key.strip_prefix("numpad").unwrap_or(&k.key);
        if ch.len() == 1
            && let Some(d) = ch.chars().next().filter(char::is_ascii_digit)
        {
            self.prive_chiffre(d);
        }
        true
    }

    fn prive_chiffre(&mut self, d: char) {
        let attente = self.prive.fichier.attente() > 0;
        let Some(pave) = self.prive.pave.as_mut() else { return };
        if matches!(pave.etape, Etape::QueFaire | Etape::ConfirmerRemettre) {
            return;
        }
        let verifie = matches!(pave.etape, Etape::Ouvrir | Etape::Ancien | Etape::Desactiver);
        if verifie && attente {
            return;
        }
        if pave.saisie.len() < CHIFFRES {
            pave.saisie.push(d);
        }
        if pave.saisie.len() == CHIFFRES {
            self.prive_valider();
        }
    }

    fn prive_valider(&mut self) {
        let Some(pave) = self.prive.pave.as_mut() else { return };
        let code = std::mem::take(&mut pave.saisie);
        pave.message = None;
        let etape = pave.etape.clone();
        let bon = |s: &mut Self| {
            if s.prive.fichier.verifier(&code) {
                s.prive.fichier.reussite();
                true
            } else {
                s.prive.fichier.echec();
                if let Some(p) = s.prive.pave.as_mut() {
                    p.message = Some(tr("Code incorrect", "Wrong code"));
                }
                false
            }
        };
        match etape.clone() {
            Etape::Ouvrir => {
                if bon(self) {
                    self.prive.pave = None;
                    self.prive.deverrouille = true;
                    self.aller_privees();
                }
            }
            Etape::Nouveau | Etape::ChangerNouveau => {
                let suite = if etape == Etape::Nouveau { Etape::Confirmer(code) } else { Etape::ChangerConfirmer(code) };
                self.pave_reglages(suite);
            }
            Etape::Confirmer(premier) | Etape::ChangerConfirmer(premier) => {
                if premier != code {
                    let retour = if matches!(etape, Etape::Confirmer(_)) { Etape::Nouveau } else { Etape::ChangerNouveau };
                    self.pave_reglages(retour);
                    if let Some(p) = self.prive.pave.as_mut() {
                        p.message = Some(tr("Les deux codes diffèrent : recommence.", "The codes don't match: try again."));
                    }
                    return;
                }
                let nouveau = !self.prive.fichier.actif();
                self.prive.fichier.poser_code(&code);
                self.prive.fichier.garder();
                self.prive.pave = None;
                if nouveau {
                    poser_lanceur(true);
                }
            }
            Etape::Ancien => {
                if bon(self) {
                    self.pave_reglages(Etape::ChangerNouveau);
                }
            }
            Etape::Desactiver => {
                if bon(self) {
                    if self.reglages.privees.is_empty() {
                        self.desactiver_prive();
                    } else {
                        self.pave_reglages(Etape::QueFaire);
                    }
                }
            }
            Etape::QueFaire | Etape::ConfirmerRemettre => {}
        }
    }

    /// Désactive la liste sur cet appareil (le haché part, le lanceur aussi).
    fn desactiver_prive(&mut self) {
        self.prive.fichier = FichierPrive::default();
        self.prive.fichier.garder();
        self.prive.pave = None;
        self.verrouiller();
        poser_lanceur(false);
    }

    /// Les privées redeviennent des conversations normales (sur tous les appareils).
    fn remettre_privees(&mut self) {
        self.reglages.privees.clear();
        self.marques_changees();
        self.desactiver_prive();
    }

    /// `jasette --prive` : le pavé dès l'ouverture.
    pub(crate) fn demarrer_prive(&mut self) {
        if std::env::args().any(|a| a == "--prive") {
            self.ouvrir_privees();
        }
    }

    // ---- Dessin ----

    /// L'item « Rendre privée » / « Retirer des privées » du menu ⋮.
    pub(crate) fn item_menu_prive(&self, id: &str, cx: &mut Context<Self>) -> Option<AnyElement> {
        if !self.prive.fichier.actif() {
            return None;
        }
        let privee = self.privee(id);
        let texte = if privee { tr("Retirer des privées", "Remove from private") } else { tr("Rendre privée", "Make private") };
        let id = id.to_string();
        Some(
            div()
                .id("menu-prive")
                .flex()
                .flex_row()
                .items_center()
                .gap(px(10.))
                .px(px(12.))
                .py(px(9.))
                .rounded(rayon(10.))
                .text_size(px(14.))
                .text_color(th().texte_fort)
                .cursor_pointer()
                .hover(|s| s.bg(th().survol))
                .child(icone(if privee { "lock-open" } else { "lock" }, 16., th().texte_fort))
                .child(texte)
                .on_click(cx.listener(move |this, _, _, cx| {
                    this.rendre_privee(id.clone(), !privee);
                    cx.notify();
                }))
                .into_any_element(),
        )
    }

    /// Le bouton « Verrouiller » de l'en-tête de la vue Privées.
    pub(crate) fn bouton_verrouiller(&self, cx: &mut Context<Self>) -> impl IntoElement {
        Self::bouton_icone("f-verrou", "lock", false).on_click(cx.listener(|this, _, _, cx| {
            this.verrouiller();
            cx.notify();
        }))
    }

    /// Réglages > Liste privée.
    pub(crate) fn reglages_prive(&self, cx: &mut Context<Self>) -> AnyElement {
        let actif = self.prive.fichier.actif();
        let n = self.reglages.privees.len();
        let mut g = groupe().child(ligne_interrupteur(
            "p-actif",
            tr("Liste privée", "Private list"),
            tr(
                "Des conversations cachées partout dans Jasette, qu'on rouvre avec un code à 6 chiffres. Le code reste sur cet appareil.",
                "Conversations hidden everywhere in Jasette, reopened with a 6-digit code. The code stays on this device.",
            ),
            actif,
            cx.listener(move |this, _, _, cx| {
                this.pave_reglages(if actif { Etape::Desactiver } else { Etape::Nouveau });
                cx.notify();
            }),
        ));
        let ligne = |id: &'static str, ic: &'static str, titre: &'static str, detail: String| {
            div()
                .id(id)
                .flex()
                .flex_row()
                .items_center()
                .gap(px(14.))
                .px(px(16.))
                .py(px(13.))
                .cursor_pointer()
                .hover(|s| s.bg(th().survol))
                .child(icone(ic, 18., th().accent))
                .child(
                    div()
                        .flex_1()
                        .min_w(px(0.))
                        .flex()
                        .flex_col()
                        .gap(px(2.))
                        .child(div().font_weight(FontWeight::SEMIBOLD).text_color(th().texte_fort).child(titre))
                        .child(div().text_size(px(12.)).text_color(th().texte_pale).child(detail)),
                )
        };
        if actif {
            let detail = if anglais() {
                format!("{n} private conversation(s) · shortcut Ctrl+Shift+P")
            } else {
                format!("{n} conversation(s) privée(s) · raccourci Ctrl+Maj+P")
            };
            g = g
                .child(separateur())
                .child(ligne("p-ouvrir", "lock-keyhole", tr("Ouvrir les conversations privées", "Open private conversations"), detail).on_click(
                    cx.listener(|this, _, _, cx| {
                        this.ouvrir_privees();
                        cx.notify();
                    }),
                ))
                .child(separateur())
                .child(
                    ligne("p-changer", "key-round", tr("Changer le code", "Change the code"), tr("Il faut l'ancien code.", "The current code is required.").into())
                        .on_click(cx.listener(|this, _, _, cx| {
                            this.pave_reglages(Etape::Ancien);
                            cx.notify();
                        })),
                );
        }
        let note = if !actif && n > 0 {
            if anglais() {
                format!("{n} conversation(s) made private on another device stay hidden here. Turn on the private list to see them.")
            } else {
                format!("{n} conversation(s) rendue(s) privée(s) sur un autre appareil reste(nt) cachée(s) ici. Active la Liste privée pour les voir.")
            }
        } else {
            tr(
                "« Rendre privée » apparaît dans le menu ⋮ d'une conversation. Une conversation privée ne donne aucune notification. La vue se reverrouille quand la fenêtre perd le focus, ou après 1 minute sans activité. La liste des privées est partagée par le serveur Jasette ; le code, jamais.",
                "“Make private” appears in a conversation's ⋮ menu. A private conversation never notifies. The view locks again when the window loses focus, or after 1 minute without activity. The private list is shared by the Jasette server; the code never is.",
            )
            .to_string()
        };
        carte_section(tr("Liste privée", "Private list"))
            .child(g)
            .child(div().px(px(6.)).pt(px(12.)).text_size(px(12.)).text_color(th().texte_pale).child(note))
            .into_any_element()
    }

    /// Le pavé du code, par-dessus tout.
    pub(crate) fn pave_vue(&self, cx: &mut Context<Self>) -> Option<AnyElement> {
        let pave = self.prive.pave.as_ref()?;
        let n = self.reglages.privees.len();
        let (titre, sous_titre): (&'static str, String) = match &pave.etape {
            Etape::Ouvrir => (tr("Conversations privées", "Private conversations"), tr("Entre ton code", "Enter your code").into()),
            Etape::Nouveau => (tr("Liste privée", "Private list"), tr("Choisis un code à 6 chiffres", "Choose a 6-digit code").into()),
            Etape::Confirmer(_) | Etape::ChangerConfirmer(_) => {
                (tr("Liste privée", "Private list"), tr("Entre le code une 2e fois", "Enter the code again").into())
            }
            Etape::Ancien => (tr("Changer le code", "Change the code"), tr("Entre le code actuel", "Enter the current code").into()),
            Etape::ChangerNouveau => (tr("Changer le code", "Change the code"), tr("Choisis le nouveau code", "Choose the new code").into()),
            Etape::Desactiver => (tr("Désactiver la liste", "Turn off the list"), tr("Entre ton code", "Enter your code").into()),
            Etape::QueFaire | Etape::ConfirmerRemettre => (
                tr("Désactiver la liste", "Turn off the list"),
                if anglais() {
                    format!("{n} conversation(s) are still private. What should happen to them?")
                } else {
                    format!("Il reste {n} conversation(s) privée(s). Qu'en fait-on ?")
                },
            ),
        };
        let attente = self.prive.fichier.attente();
        let verifie = matches!(pave.etape, Etape::Ouvrir | Etape::Ancien | Etape::Desactiver);
        let message: Option<String> = if verifie && attente > 0 {
            Some(if anglais() { format!("Too many tries: wait {attente} s") } else { format!("Trop d'essais : attends {attente} s") })
        } else {
            pave.message.map(str::to_string)
        };
        let mut carte = verre(0.97, 22.)
            .shadow(ombre_verre())
            .w(px(320.))
            .p(px(24.))
            .flex()
            .flex_col()
            .items_center()
            .gap(px(12.))
            .child(icone("lock", 26., th().accent))
            .child(div().text_size(px(18.)).font_weight(FontWeight::BOLD).text_color(th().texte_fort).child(titre))
            .child(div().text_size(px(13.)).text_color(th().texte_pale).text_center().child(sous_titre));

        if matches!(pave.etape, Etape::QueFaire | Etape::ConfirmerRemettre) {
            let confirmer = pave.etape == Etape::ConfirmerRemettre;
            carte = carte
                .child(
                    bouton_texte(
                        "p-remettre",
                        if confirmer {
                            tr("Oui, les remettre", "Yes, put them back")
                        } else {
                            tr("Les remettre dans l'app (conseillé)", "Put them back in the app (recommended)")
                        },
                        ACCENT,
                    )
                    .on_click(cx.listener(move |this, _, _, cx| {
                        if confirmer {
                            this.remettre_privees();
                        } else {
                            this.pave_reglages(Etape::ConfirmerRemettre);
                        }
                        cx.notify();
                    })),
                )
                .when(!confirmer, |d| {
                    d.child(
                        bouton_texte("p-garder", tr("Les garder cachées", "Keep them hidden"), DANGER).on_click(cx.listener(
                            |this, _, _, cx| {
                                this.desactiver_prive();
                                cx.notify();
                            },
                        )),
                    )
                    .child(
                        div().text_size(px(11.)).text_color(th().texte_pale).text_center().child(tr(
                            "Cachées, elles ne se verront plus avant de réactiver la Liste privée.",
                            "Hidden, they won't show again until the private list is turned back on.",
                        )),
                    )
                })
                .child(lien_annuler(cx));
            return Some(voile(carte).into_any_element());
        }

        // Les six points.
        let mut points = div().flex().flex_row().gap(px(12.)).py(px(6.));
        for i in 0..CHIFFRES {
            let plein = i < pave.saisie.len();
            points = points.child(
                div()
                    .size(px(14.))
                    .rounded(rayon(999.))
                    .border_2()
                    .border_color(th().accent)
                    .when(plein, |d| d.bg(th().accent)),
            );
        }
        carte = carte.child(points).child(
            div()
                .h(px(18.))
                .text_size(px(13.))
                .text_color(th().danger)
                .children(message),
        );
        // Le pavé : 1 à 9, puis ⌫ 0 (Échap ferme).
        let touche = |id: usize, contenu: AnyElement| {
            div()
                .id(("p-touche", id))
                .w(px(72.))
                .h(px(52.))
                .rounded(rayon(14.))
                .flex()
                .items_center()
                .justify_center()
                .cursor_pointer()
                .bg(ton(blanc(0.55), th().menu_champ))
                .hover(|s| s.bg(th().survol))
                .text_size(px(20.))
                .font_weight(FontWeight::SEMIBOLD)
                .text_color(th().texte_fort)
                .child(contenu)
        };
        let mut grille = div().flex().flex_row().flex_wrap().w(px(72. * 3. + 24.)).gap(px(12.));
        for d in ['1', '2', '3', '4', '5', '6', '7', '8', '9'] {
            grille = grille.child(touche(d as usize, d.to_string().into_any_element()).on_click(cx.listener(
                move |this, _, _, cx| {
                    this.prive_chiffre(d);
                    cx.notify();
                },
            )));
        }
        grille = grille
            .child(div().w(px(72.)).h(px(52.)))
            .child(touche('0' as usize, "0".into_any_element()).on_click(cx.listener(|this, _, _, cx| {
                this.prive_chiffre('0');
                cx.notify();
            })))
            .child(touche(99, icone("delete", 20., th().texte_fort).into_any_element()).on_click(cx.listener(
                |this, _, _, cx| {
                    if let Some(p) = this.prive.pave.as_mut() {
                        p.saisie.pop();
                    }
                    cx.notify();
                },
            )));
        carte = carte.child(grille).child(lien_annuler(cx));
        Some(voile(carte).into_any_element())
    }
}

fn voile(carte: Div) -> gpui::Stateful<Div> {
    div()
        .id("pave-prive")
        .absolute()
        .top_0()
        .left_0()
        .right_0()
        .bottom_0()
        .bg(hsla(0., 0., 0., 0.62))
        .flex()
        .items_center()
        .justify_center()
        // Les clics à côté ne passent pas à l'app derrière.
        .on_mouse_down(gpui::MouseButton::Left, |_, _, cx| cx.stop_propagation())
        .child(carte)
}

fn lien_annuler(cx: &mut Context<Jasette>) -> gpui::Stateful<Div> {
    div()
        .id("p-annuler")
        .pt(px(4.))
        .text_size(px(13.))
        .text_color(th().texte_pale)
        .cursor_pointer()
        .hover(|s| s.opacity(0.7))
        .child(tr("Annuler", "Cancel"))
        .on_click(cx.listener(|this, _, _, cx| {
            this.prive.pave = None;
            cx.notify();
        }))
}

#[cfg(test)]
mod essais {
    use super::*;

    #[test]
    fn code_hache() {
        let mut f = FichierPrive::default();
        assert!(!f.actif());
        f.poser_code("123456");
        assert!(f.actif());
        assert!(!f.hache.contains("123456") && !f.sel.is_empty());
        assert!(f.verifier("123456"));
        assert!(!f.verifier("123457"));
        // Deux sels différents : deux hachés différents.
        let mut g = FichierPrive::default();
        g.poser_code("123456");
        assert_ne!(f.hache, g.hache);
    }

    #[test]
    fn attente_croissante() {
        let mut f = FichierPrive { sel: "x".into(), hache: "y".into(), iterations: 1, ..Default::default() };
        // Sans écrire sur le disque : on simule echec() sans garder().
        for _ in 0..4 {
            f.echecs += 1;
        }
        assert_eq!(f.attente(), 0);
        f.echecs += 1;
        let n = (f.echecs - ESSAIS_LIBRES).min(7);
        f.attente_jusqu = maintenant_ms() + (30i64 << n) * 1000;
        assert!((29..=31).contains(&f.attente()));
    }
}
