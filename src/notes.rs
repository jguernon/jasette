// « Quoi de neuf ? » (serveur/PROTOCOLE.md, §5.12) : publier une note (ou un
// statut) sur plusieurs services d'un geste, lire les notes Instagram des
// contacts (bulles du Head) et, en option, la musique écoutée (src/musique.rs).
//
// L'écran « Moi » des réglages : la bulle « Quoi de neuf ? » (note actuelle,
// champ, Publier, Effacer, caractères restants), « Publier sur : Tout ▸ »
// (marque `notes_services`) et « Changer d'avatar ▸ ». Réglages > Quoi de
// neuf : `notes_contacts`, `notes_musique` (cet appareil), `notes_musique_format`
// et le même sélecteur.
use super::*;

/// Les services qui savent publier une note, dans l'ordre de l'écran.
/// Discord (statut personnalisé, 128 caractères) s'ajoutera ici avec son pont.
pub(crate) const SERVICES_NOTES: [Service; 6] =
    [Service::Instagram, Service::Messenger, Service::WhatsApp, Service::Telegram, Service::Bluesky, Service::Discord];

/// La longueur maximale d'une note sur un service (points de code).
pub(crate) fn limite(s: Service) -> usize {
    match s {
        Service::Instagram | Service::Messenger => 60,
        Service::WhatsApp => 139,
        Service::Telegram => 70,
        Service::Bluesky => 256,
        Service::Discord => 128,
        _ => 60,
    }
}

/// Ce que la note change sur chaque service.
fn ce_qui_change(s: Service) -> &'static str {
    match s {
        Service::Instagram => tr("Note de la messagerie · 24 h", "Inbox note · 24 h"),
        Service::Messenger => tr("Note de Messenger · 24 h", "Messenger note · 24 h"),
        Service::WhatsApp => tr("Infos du profil · 24 h", "Profile About · 24 h"),
        Service::Telegram => tr("Bio du profil, jusqu'à l'effacement", "Profile bio, until cleared"),
        Service::Bluesky => tr("Description du profil (bio), jusqu'à l'effacement", "Profile description (bio), until cleared"),
        Service::Discord => tr("Statut personnalisé, jusqu'à l'effacement", "Custom status, until cleared"),
        _ => "",
    }
}

/// Un texte en français ou en anglais, selon la langue de l'interface.
pub(crate) fn tr(fr: &'static str, en: &'static str) -> &'static str {
    if anglais() { en } else { fr }
}

/// Lecture des notes des contacts : au plus toutes les 15 minutes.
const INTERVALLE_LISTE: Duration = Duration::from_secs(15 * 60);

/// Ma note sur un service (`note_moi`).
#[derive(Clone, Debug, Default)]
pub(crate) struct NoteMoi {
    pub text: String,
    pub time: i64,
    pub expires: i64,
    /// "main" ou "musique".
    pub source: String,
    /// La note écrite à la main, mise de côté par la musique.
    pub main_text: String,
}

/// La note d'un contact (Instagram).
#[derive(Clone, Debug)]
pub(crate) struct NoteContact {
    pub text: String,
}

/// Où en est une publication, par service.
#[derive(Clone, Debug, PartialEq, Eq)]
pub(crate) enum EtatPublication {
    Attente,
    Ok,
    Erreur(String),
}

#[derive(Default)]
pub(crate) struct Notes {
    pub moi: HashMap<Service, NoteMoi>,
    /// Conversation (id préfixé « i: ») → la note de la personne.
    pub contacts: HashMap<String, NoteContact>,
    /// Le résultat de la dernière publication faite ici : (service, req, état).
    pub resultats: Vec<(Service, String, EtatPublication)>,
    req: u64,
    pub selecteur_moi: bool,
    pub selecteur_reglages: bool,
    dernier_liste: Option<Instant>,
    dernier_tic: Option<Instant>,
    moteur: musique::Moteur,
    pub lecteur: Arc<musique::Lecteur>,
}

impl Jasette {
    // ---- Les nouvelles des ponts ----

    /// `note_moi` : ma note actuelle sur ce service.
    pub(crate) fn note_moi_recue(&mut self, svc: Service, v: &Value) {
        let s = |k: &str| v.get(k).and_then(Value::as_str).unwrap_or_default().to_string();
        let n = |k: &str| v.get(k).and_then(Value::as_i64).unwrap_or(0);
        self.notes.moi.insert(
            svc,
            NoteMoi { text: s("text"), time: n("time"), expires: n("expires"), source: s("source"), main_text: s("main_text") },
        );
    }

    /// `note_result` : le ✓ ou l'erreur d'une publication.
    pub(crate) fn note_resultat_recu(&mut self, svc: Service, v: &Value) {
        let req = v.get("req").and_then(Value::as_str).unwrap_or_default();
        let ok = v.get("ok").and_then(Value::as_bool).unwrap_or(false);
        let erreur = v.get("error").and_then(Value::as_str).unwrap_or(tr("échec", "failed")).to_string();
        if !ok && req.starts_with('m') {
            // Une publication de la musique : dans le journal seulement.
            eprintln!("jasette : note musicale {} : {erreur}", svc.court());
        }
        for (s, r, e) in &mut self.notes.resultats {
            if *s == svc && (r == req || req.is_empty()) && *e == EtatPublication::Attente {
                *e = if ok { EtatPublication::Ok } else { EtatPublication::Erreur(erreur.clone()) };
            }
        }
    }

    /// `notes` (Instagram) : les notes des contacts, liste complète.
    pub(crate) fn notes_contacts_recues(&mut self, v: &Value) {
        if !self.reglages.notes_contacts {
            return;
        }
        self.notes.contacts.clear();
        for it in v.get("items").and_then(Value::as_array).into_iter().flatten() {
            let (Some(conv), Some(text)) = (it.get("conv").and_then(Value::as_str), it.get("text").and_then(Value::as_str)) else {
                continue;
            };
            if !conv.is_empty() && !text.trim().is_empty() {
                self.notes.contacts.insert(conv.to_string(), NoteContact { text: text.trim().to_string() });
            }
        }
    }

    /// La marque `notes_contacts` vient de changer (ici ou ailleurs) : le pont
    /// Instagram local le reçoit (le serveur prévient le sien).
    pub(crate) fn notes_reglees(&mut self) {
        let oui = self.reglages.notes_contacts;
        pont::regler_notes(oui);
        if !oui {
            self.notes.contacts.clear();
        }
        self.notes.dernier_liste = None;
        if let Some(p) = self.meta(Service::Instagram).pont.as_ref()
            && p.local()
        {
            p.envoyer(&json!({"cmd": "notes_contacts", "on": oui}));
        }
    }

    // ---- Ce qui est choisi ----

    /// Les services compatibles et connectés.
    pub(crate) fn services_notes_connectes(&self) -> Vec<Service> {
        SERVICES_NOTES.into_iter().filter(|s| !self.meta(*s).absent && self.meta(*s).etat == Etat::Pret).collect()
    }

    fn service_note_choisi(&self, s: Service) -> bool {
        self.reglages.notes_services.is_empty() || self.reglages.notes_services.iter().any(|x| x == s.cle())
    }

    /// Les services où publier maintenant : choisis, compatibles et prêts.
    pub(crate) fn services_notes_choisis(&self) -> Vec<Service> {
        self.services_notes_connectes().into_iter().filter(|s| self.service_note_choisi(*s)).collect()
    }

    /// La limite du service le plus limitant parmi les choisis.
    fn limite_choisie(&self) -> Option<usize> {
        self.services_notes_choisis().into_iter().map(limite).min()
    }

    /// Ma note actuelle : la plus récente des notes non vides et non échues.
    pub(crate) fn note_actuelle(&self) -> Option<&NoteMoi> {
        let m = maintenant_ms();
        self.notes.moi.values().filter(|n| !n.text.is_empty() && (n.expires == 0 || n.expires > m)).max_by_key(|n| n.time)
    }

    fn basculer_service_note(&mut self, s: Service, cx: &mut Context<Self>) {
        let mut l: Vec<String> = if self.reglages.notes_services.is_empty() {
            self.services_notes_connectes().iter().map(|x| x.cle().to_string()).collect()
        } else {
            self.reglages.notes_services.clone()
        };
        if l.iter().any(|x| x == s.cle()) {
            l.retain(|x| x != s.cle());
            if l.is_empty() {
                // Au moins un service : décocher le dernier ne veut pas dire « tout ».
                return;
            }
        } else {
            l.push(s.cle().to_string());
        }
        self.reglages.notes_services = l;
        self.marques_changees();
        cx.notify();
    }

    fn choisir_tout_notes(&mut self, cx: &mut Context<Self>) {
        if !self.reglages.notes_services.is_empty() {
            self.reglages.notes_services.clear();
            self.marques_changees();
        }
        cx.notify();
    }

    // ---- Publier ----

    fn prochaine_req(&mut self, prefixe: char) -> String {
        self.notes.req += 1;
        format!("{prefixe}{}", self.notes.req)
    }

    /// Publier : une trame `note_set` par service choisi et prêt, une seule fois.
    pub(crate) fn publier_note(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        let texte = self.note_saisie.read(cx).value().trim().to_string();
        let services = self.services_notes_choisis();
        let Some(max) = self.limite_choisie() else { return };
        if texte.is_empty() || texte.chars().count() > max {
            return;
        }
        let audience = self.reglages.notes_audience.min(1);
        self.notes.resultats.clear();
        for s in services {
            let req = self.prochaine_req('n');
            self.commande_svc(
                s,
                json!({"cmd": "note_set", "text": texte, "audience": audience, "source": "main", "req": req}),
            );
            self.notes.resultats.push((s, req, EtatPublication::Attente));
        }
        self.note_saisie.update(cx, |s, cx| s.set_value("", window, cx));
        cx.notify();
    }

    /// Effacer : une trame `note_clear` par service choisi et prêt.
    fn effacer_note(&mut self, cx: &mut Context<Self>) {
        self.notes.resultats.clear();
        for s in self.services_notes_choisis() {
            let req = self.prochaine_req('n');
            self.commande_svc(s, json!({"cmd": "note_clear", "req": req}));
            self.notes.resultats.push((s, req, EtatPublication::Attente));
        }
        cx.notify();
    }

    // ---- Le tic : les notes des contacts, la musique ----

    /// Appelé par la boucle des nouvelles (50 ms) ; ne travaille que toutes
    /// les 5 s. Vrai si l'écran doit être redessiné.
    pub(crate) fn notes_tic(&mut self) -> bool {
        if self.notes.dernier_tic.is_some_and(|t| t.elapsed() < Duration::from_secs(5)) {
            return false;
        }
        self.notes.dernier_tic = Some(Instant::now());
        // Les notes des contacts : au plus toutes les 15 minutes, si le Head en montre.
        if self.reglages.notes_contacts
            && self.instagram.etat == Etat::Pret
            && self.reglages.head.iter().any(|id| id.starts_with(Service::Instagram.prefixe()))
            && self.notes.dernier_liste.is_none_or(|t| t.elapsed() >= INTERVALLE_LISTE)
        {
            // Le pont lit le plateau lui-même dès qu'il est prêt : le premier tic attend.
            if self.notes.dernier_liste.is_some() {
                self.commande_svc(Service::Instagram, json!({"cmd": "notes_list"}));
            }
            self.notes.dernier_liste = Some(Instant::now());
        }
        self.musique_tic()
    }

    /// Cet appareil publie-t-il la musique ?
    pub(crate) fn musique_ici(&self) -> bool {
        !self.reglages.notes_musique.is_empty() && self.reglages.notes_musique == nom_machine()
    }

    fn musique_tic(&mut self) -> bool {
        let ici = self.musique_ici();
        self.notes.lecteur.actif.store(ici, std::sync::atomic::Ordering::Relaxed);
        if !ici {
            return false;
        }
        self.notes.lecteur.demarrer(dossier_donnees().join("musique-genres.json"));
        let Some(lu) = self.notes.lecteur.derniere.lock().ok().and_then(|mut d| d.take()) else { return false };
        let musicale = self.notes.moi.values().any(|n| n.source == "musique" && !n.text.is_empty());
        let Some(max) = self.limite_choisie() else { return false };
        let lecteur = self.notes.lecteur.clone();
        let format = self.reglages.notes_musique_format.clone();
        let action =
            self.notes.moteur.sondage(maintenant_ms(), lu.as_ref(), musicale, &format, anglais(), max, &|a| lecteur.genre(a));
        match action {
            Some(musique::Action::Publier(texte)) => {
                let audience = self.reglages.notes_audience.min(1);
                for s in self.services_notes_choisis() {
                    let req = self.prochaine_req('m');
                    self.commande_svc(
                        s,
                        json!({"cmd": "note_set", "text": texte, "audience": audience, "source": "musique", "req": req}),
                    );
                }
                true
            }
            Some(musique::Action::Effacer) => {
                for s in self.services_notes_choisis() {
                    let req = self.prochaine_req('m');
                    self.commande_svc(s, json!({"cmd": "note_clear", "source": "musique", "req": req}));
                }
                true
            }
            None => false,
        }
    }

    // ---- L'interface ----

    /// La bulle de note d'un contact, au-dessus de son avatar dans le Head.
    pub(crate) fn bulle_note_head(&self, conv: &Conv, avatar: f32, largeur: f32) -> Option<Div> {
        if !self.reglages.notes_contacts || conv.group {
            return None;
        }
        let n = self.notes.contacts.get(&conv.id)?;
        let ega = th().ega || retro::retro();
        let bulle = div()
            .max_w(px(largeur))
            .px(px(6.))
            .py(px(2.))
            .when(!ega, |d| d.rounded(rayon(9.)))
            .bg(ton(blanc(0.97), th().menu))
            .border_1()
            .border_color(ton(blanc(1.), th().trait_fin))
            .shadow(ombre_flottante())
            .text_size(px(10.5))
            .line_height(px(13.))
            .text_color(th().texte_fort)
            .child(div().truncate().child(n.text.clone()));
        Some(
            div()
                .absolute()
                // Au-dessus de la tête, sans cacher la pastille de non-lu.
                .top(px(-17.))
                .left(px((avatar - largeur) / 2.))
                .w(px(largeur))
                .flex()
                .flex_row()
                .justify_center()
                .child(bulle),
        )
    }

    /// Une case à cocher.
    fn case_note(actif: bool) -> Div {
        let carre = th().ega || retro::retro();
        div()
            .size(px(18.))
            .flex_none()
            .flex()
            .items_center()
            .justify_center()
            .when(!carre, |d| d.rounded(px(5.)))
            .border_2()
            .border_color(if actif { th().accent } else { th().texte_pale })
            .when(actif, |d| d.bg(th().accent).child(icone("check", 12., blanc(1.))))
    }

    /// Un bouton radio.
    fn rond_note(actif: bool) -> Div {
        let carre = th().ega || retro::retro();
        div()
            .size(px(18.))
            .flex_none()
            .flex()
            .items_center()
            .justify_center()
            .when(!carre, |d| d.rounded(px(9.)))
            .border_2()
            .border_color(if actif { th().accent } else { th().texte_pale })
            .when(actif, |d| d.child(div().size(px(8.)).when(!carre, |d| d.rounded(px(4.))).bg(th().accent)))
    }

    /// Une rangée repliable : « Publier sur : Tout ▸ », « Changer d'avatar ▸ ».
    fn rangee_repliable(id: &'static str, titre: String, ouvert: bool) -> gpui::Stateful<Div> {
        div()
            .id(id)
            .flex()
            .flex_row()
            .items_center()
            .gap(px(10.))
            .px(px(16.))
            .py(px(13.))
            .cursor_pointer()
            .hover(|s| s.bg(th().survol))
            .child(div().flex_1().min_w(px(0.)).truncate().font_weight(FontWeight::SEMIBOLD).text_color(th().texte_fort).child(titre))
            .child(icone(if ouvert { "chevron-down" } else { "chevron-right" }, 16., th().texte_pale))
    }

    /// Le résumé du choix : « Tout », ou les services choisis.
    fn resume_services(&self) -> String {
        if self.reglages.notes_services.is_empty() {
            return tr("Tout", "All").into();
        }
        let noms: Vec<&str> = SERVICES_NOTES
            .into_iter()
            .filter(|s| self.reglages.notes_services.iter().any(|x| x == s.cle()))
            .map(Service::court)
            .collect();
        if noms.is_empty() { tr("Aucun service", "No service").into() } else { noms.join(", ") }
    }

    /// Le sélecteur « Publier sur : Tout ▸ » (écran « Moi » et Réglages : la
    /// même marque `notes_services`).
    fn selecteur_services(&self, reglages: bool, cx: &mut Context<Self>) -> Div {
        let ouvert = if reglages { self.notes.selecteur_reglages } else { self.notes.selecteur_moi };
        let titre = format!("{} {}", tr("Publier sur :", "Post to:"), self.resume_services());
        let mut g = groupe().child(
            Self::rangee_repliable(if reglages { "notes-sel-r" } else { "notes-sel-m" }, titre, ouvert).on_click(cx.listener(
                move |this, _, _, cx| {
                    if reglages {
                        this.notes.selecteur_reglages = !this.notes.selecteur_reglages;
                    } else {
                        this.notes.selecteur_moi = !this.notes.selecteur_moi;
                    }
                    cx.notify();
                },
            )),
        );
        if !ouvert {
            return g;
        }
        let tout = self.reglages.notes_services.is_empty();
        let rangee = |id: SharedString, actif: bool, titre: String, detail: String, logo_svc: Option<Service>| {
            div()
                .id(id)
                .flex()
                .flex_row()
                .items_center()
                .gap(px(12.))
                .px(px(16.))
                .py(px(10.))
                .cursor_pointer()
                .hover(|s| s.bg(th().survol))
                .child(Self::case_note(actif))
                .when_some(logo_svc, |d, s| d.child(logo(s, 20.)))
                .child(
                    div()
                        .flex_1()
                        .min_w(px(0.))
                        .flex()
                        .flex_col()
                        .child(div().text_color(th().texte_fort).child(titre))
                        .when(!detail.is_empty(), |d| d.child(div().text_size(px(12.)).text_color(th().texte_pale).child(detail))),
                )
        };
        g = g.child(separateur()).child(
            rangee(
                SharedString::from(format!("notes-tout-{reglages}")),
                tout,
                tr("Tout", "All").into(),
                tr("Tous les services connectés, y compris ceux ajoutés plus tard", "Every connected service, including ones added later")
                    .into(),
                None,
            )
            .on_click(cx.listener(|this, _, _, cx| this.choisir_tout_notes(cx))),
        );
        let connectes = self.services_notes_connectes();
        if connectes.is_empty() {
            g = g.child(separateur()).child(
                div()
                    .px(px(16.))
                    .py(px(10.))
                    .text_size(px(13.))
                    .text_color(th().texte_pale)
                    .child(tr("Aucun service compatible n'est connecté.", "No compatible service is connected.")),
            );
        }
        for s in connectes {
            let detail = format!("{} · {} {}", ce_qui_change(s), limite(s), tr("caractères", "characters"));
            g = g.child(separateur()).child(
                rangee(SharedString::from(format!("notes-{}-{reglages}", s.cle())), self.service_note_choisi(s), s.court().into(), detail, Some(s))
                    .on_click(cx.listener(move |this, _, _, cx| this.basculer_service_note(s, cx))),
            );
        }
        // L'audience Instagram (propre à cet appareil).
        if self.instagram.etat == Etat::Pret && self.service_note_choisi(Service::Instagram) {
            g = g.child(separateur()).child(
                div()
                    .px(px(16.))
                    .pt(px(10.))
                    .text_size(px(12.))
                    .font_weight(FontWeight::SEMIBOLD)
                    .text_color(th().texte_pale)
                    .child(tr("Audience Instagram", "Instagram audience")),
            );
            for (a, titre) in [
                (0u8, tr("Abonnés que je suis en retour", "Followers you follow back")),
                (1u8, tr("Amis proches", "Close friends")),
            ] {
                g = g.child(
                    div()
                        .id(SharedString::from(format!("notes-aud-{a}-{reglages}")))
                        .flex()
                        .flex_row()
                        .items_center()
                        .gap(px(12.))
                        .px(px(16.))
                        .py(px(8.))
                        .cursor_pointer()
                        .hover(|s| s.bg(th().survol))
                        .child(Self::rond_note(self.reglages.notes_audience == a))
                        .child(div().text_color(th().texte_fort).child(titre))
                        .on_click(cx.listener(move |this, _, _, cx| {
                            this.reglages.notes_audience = a;
                            this.reglages.garder();
                            cx.notify();
                        })),
                );
            }
            g = g.child(div().h(px(6.)));
        }
        g
    }

    /// L'écran « Moi » : « Quoi de neuf ? », où publier, l'avatar.
    pub(crate) fn moi_vue(&self, cx: &mut Context<Self>) -> AnyElement {
        let actuelle = self.note_actuelle().cloned();
        let texte = self.note_saisie.read(cx).value().to_string();
        let n = texte.trim().chars().count();
        let max = self.limite_choisie();
        let restant = max.map(|m| m as i64 - n as i64);
        let peut_publier = n > 0 && restant.is_some_and(|r| r >= 0);
        let peut_effacer = actuelle.is_some() && max.is_some();
        let musicale = actuelle.as_ref().is_some_and(|a| a.source == "musique");
        let ega = th().ega || retro::retro();

        // La bulle de la note actuelle, à droite de mon avatar.
        let bulle = div()
            .flex_1()
            .min_w(px(0.))
            .px(px(14.))
            .py(px(10.))
            .when(!ega, |d| d.rounded(rayon(18.)).rounded_tl(rayon(4.)))
            .bg(ton(blanc(0.92), th().surface))
            .border_1()
            .border_color(ton(blanc(1.), th().surface_bord))
            .child(
                div()
                    .text_size(px(16.))
                    .font_weight(FontWeight::SEMIBOLD)
                    .text_color(if actuelle.is_some() { th().texte_fort } else { th().texte_pale })
                    .child(actuelle.as_ref().map(|a| a.text.clone()).unwrap_or_else(|| tr("Quoi de neuf ?", "What's up?").into())),
            )
            .when_some(actuelle.as_ref().filter(|a| a.time > 0), |d, a| {
                let depuis = (maintenant_ms() - a.time).max(0) / 60_000;
                let quand = if depuis < 60 {
                    if anglais() { format!("{} min ago", depuis.max(1)) } else { format!("il y a {} min", depuis.max(1)) }
                } else if anglais() {
                    format!("{} h ago", depuis / 60)
                } else {
                    format!("il y a {} h", depuis / 60)
                };
                d.child(div().text_size(px(12.)).text_color(th().texte_pale).child(quand))
            });

        let compteur = match restant {
            Some(r) => div()
                .flex_none()
                .text_size(px(12.))
                .font_weight(FontWeight::SEMIBOLD)
                .text_color(if r < 0 { th().danger } else if r <= 10 { c(0xf59e0b) } else { th().texte_pale })
                .child(r.to_string()),
            None => div(),
        };

        let bouton_plein = |id: &'static str, titre: &'static str, actif: bool| {
            div()
                .id(id)
                .px(px(18.))
                .py(px(7.))
                .when(!th().ega, |d| d.rounded(rayon(999.)))
                .text_size(px(13.))
                .font_weight(FontWeight::SEMIBOLD)
                .text_color(blanc(1.))
                .bg(degrade_accent())
                .when(actif, |d| d.cursor_pointer().hover(|s| s.opacity(0.88)))
                .when(!actif, |d| d.opacity(0.4))
                .child(titre)
        };

        let mut carte = groupe()
            .p(px(16.))
            .gap(px(12.))
            .child(
                div()
                    .text_size(px(13.))
                    .font_weight(FontWeight::BOLD)
                    .text_color(th().accent)
                    .child(tr("Quoi de neuf ?", "What's up?")),
            )
            .child(div().flex().flex_row().items_start().gap(px(12.)).child(self.mon_avatar("J", 44.)).child(bulle));
        if musicale {
            let mut m = div()
                .flex()
                .flex_col()
                .gap(px(2.))
                .text_size(px(12.))
                .text_color(th().texte_pale)
                .child(tr("🎵 La musique remplace ta note pendant que tu écoutes", "🎵 Music replaces your note while you listen"));
            if let Some(a) = actuelle.as_ref().filter(|a| !a.main_text.is_empty()) {
                m = m.child(format!("{} « {} »", tr("Ta note :", "Your note:"), a.main_text));
            }
            carte = carte.child(m);
        }
        carte = carte
            .child(
                verre(0.8, 999.)
                    .flex()
                    .flex_row()
                    .items_center()
                    .gap(px(8.))
                    .pl(px(14.))
                    .pr(px(12.))
                    .py(px(2.))
                    .child(div().flex_1().child(Input::new(&self.note_saisie).appearance(false)))
                    .child(compteur),
            )
            .child(
                div()
                    .flex()
                    .flex_row()
                    .items_center()
                    .gap(px(10.))
                    .child(bouton_plein("note-publier", tr("Publier", "Post"), peut_publier).when(peut_publier, |d| {
                        d.on_click(cx.listener(|this, _, window, cx| this.publier_note(window, cx)))
                    }))
                    .child(
                        bouton_texte("note-effacer", tr("Effacer", "Clear"), 0xef4444)
                            .when(!peut_effacer, |d| d.opacity(0.4))
                            .when(peut_effacer, |d| d.on_click(cx.listener(|this, _, _, cx| this.effacer_note(cx)))),
                    )
                    .when(max.is_none(), |d| {
                        d.child(
                            div()
                                .text_size(px(12.))
                                .text_color(th().texte_pale)
                                .child(tr("Aucun service choisi n'est connecté.", "No chosen service is connected.")),
                        )
                    }),
            );
        // Après la publication : ✓ ou l'erreur, par service.
        if !self.notes.resultats.is_empty() {
            let mut r = div().flex().flex_col().gap(px(4.));
            for (s, _, e) in &self.notes.resultats {
                let (marque, couleur, detail) = match e {
                    EtatPublication::Attente => ("…", th().texte_pale, String::new()),
                    EtatPublication::Ok => ("✓", th().succes, String::new()),
                    EtatPublication::Erreur(x) => ("✕", th().danger, x.clone()),
                };
                r = r.child(
                    div()
                        .flex()
                        .flex_row()
                        .items_center()
                        .gap(px(8.))
                        .text_size(px(13.))
                        .child(div().w(px(14.)).font_weight(FontWeight::BOLD).text_color(couleur).child(marque))
                        .child(logo(*s, 16.))
                        .child(div().text_color(th().texte_fort).child(s.court()))
                        .when(!detail.is_empty(), |d| {
                            d.child(div().flex_1().min_w(px(0.)).text_size(px(12.)).text_color(th().danger).child(detail))
                        }),
                );
            }
            carte = carte.child(r);
        }

        let avatar = groupe()
            .child(
                Self::rangee_repliable("moi-avatar-ligne", tr("Changer d'avatar", "Change avatar").into(), self.menu_avatar)
                    .on_click(cx.listener(|this, _, _, cx| {
                        this.menu_avatar = !this.menu_avatar;
                        cx.notify();
                    })),
            )
            .when(self.menu_avatar, |d| d.child(div().p(px(6.)).child(self.menu_avatar_vue(cx))));

        carte_section(tr("Moi", "Me"))
            .child(carte)
            .child(div().h(px(14.)))
            .child(self.selecteur_services(false, cx))
            .child(div().h(px(14.)))
            .child(avatar)
            .into_any_element()
    }

    /// Réglages > Quoi de neuf.
    pub(crate) fn reglages_quoi_de_neuf(&self, cx: &mut Context<Self>) -> AnyElement {
        let ici = self.musique_ici();
        let ailleurs = (!self.reglages.notes_musique.is_empty() && !ici).then(|| self.reglages.notes_musique.clone());
        let detail_musique: String = match &ailleurs {
            Some(app) => {
                if anglais() {
                    format!("On for {app}. Turn it on here to share from this computer.")
                } else {
                    format!("Activé sur {app}. Activez-le ici pour partager depuis cet ordinateur.")
                }
            }
            None => tr(
                "Votre note devient ce que Spotify joue sur cet ordinateur (au plus une fois toutes les 15 min).",
                "Your note becomes what Spotify plays on this computer (at most once every 15 min).",
            )
            .into(),
        };
        let mut g = groupe()
            .child(ligne_interrupteur(
                "qdn-contacts",
                tr("Afficher les notes de mes contacts", "Show my contacts' notes"),
                tr(
                    "Les notes Instagram en petite bulle au-dessus des avatars du Head (lues au plus toutes les 15 min).",
                    "Instagram notes in a small bubble above Head avatars (read at most every 15 min).",
                ),
                self.reglages.notes_contacts,
                cx.listener(|this, _, _, cx| {
                    this.reglages.notes_contacts = !this.reglages.notes_contacts;
                    this.marques_changees();
                    this.notes_reglees();
                    cx.notify();
                }),
            ))
            .child(separateur())
            .child(ligne_interrupteur(
                "qdn-musique",
                tr("Partager ce que j'écoute sur Spotify", "Share what I'm listening to on Spotify"),
                detail_musique,
                ici,
                cx.listener(move |this, _, _, cx| {
                    this.reglages.notes_musique = if this.musique_ici() { String::new() } else { nom_machine() };
                    this.marques_changees();
                    cx.notify();
                }),
            ));
        if ici {
            let format = self.reglages.notes_musique_format.clone();
            for (cle, titre, detail) in [
                ("", tr("Artiste ou émission", "Artist or show"), tr("« 🎵 Écoute Daft Punk »", "“🎵 Listening to Daft Punk”")),
                (
                    "genre",
                    tr("Genre", "Genre"),
                    tr(
                        "Le genre est trouvé sur MusicBrainz à partir du nom de l'artiste.",
                        "The genre is looked up on MusicBrainz from the artist's name.",
                    ),
                ),
                ("titre", tr("Titre de la chanson", "Song title"), tr("« 🎵 Titre — Artiste »", "“🎵 Title — Artist”")),
            ] {
                g = g.child(
                    div()
                        .id(SharedString::from(format!("qdn-format-{cle}")))
                        .flex()
                        .flex_row()
                        .items_center()
                        .gap(px(12.))
                        .pl(px(32.))
                        .pr(px(16.))
                        .py(px(8.))
                        .cursor_pointer()
                        .hover(|s| s.bg(th().survol))
                        .child(Self::rond_note(format == cle))
                        .child(
                            div()
                                .flex_1()
                                .min_w(px(0.))
                                .flex()
                                .flex_col()
                                .child(div().text_color(th().texte_fort).child(titre))
                                .child(div().text_size(px(12.)).text_color(th().texte_pale).child(detail)),
                        )
                        .on_click(cx.listener(move |this, _, _, cx| {
                            this.reglages.notes_musique_format = cle.to_string();
                            this.marques_changees();
                            cx.notify();
                        })),
                );
            }
            g = g.child(div().h(px(6.)));
        }
        carte_section(tr("Quoi de neuf", "What's up"))
            .child(
                div()
                    .px(px(4.))
                    .pb(px(8.))
                    .text_size(px(13.))
                    .text_color(th().texte_pale)
                    .child(tr(
                        "Une note courte publiée d'un geste sur vos services, depuis l'écran « Moi ».",
                        "A short note posted in one go to your services, from the “Me” screen.",
                    )),
            )
            .child(g)
            .child(div().h(px(14.)))
            .child(self.selecteur_services(true, cx))
            .into_any_element()
    }
}
