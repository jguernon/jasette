//! « Nouveau message » : la recherche de personnes, les puces de service
//! (Tout, puis chaque service connecté) et « Créer un groupe » (personnes
//! choisies en puces, un seul service par groupe, commande `create_group`
//! des ponts). Même comportement que `NouveauMessage` d'Android (ui/App.kt).

use super::*;

impl Service {
    /// Les ponts qui savent créer un groupe (commande `create_group`, §5.6 bis
    /// du protocole), Discord compris. X (mautrix-twitter) et Bluesky ne le
    /// savent pas.
    pub(super) fn cree_groupes(self) -> bool {
        !matches!(self, Service::X | Service::Bluesky)
    }

    /// Le service prend-il un nom de groupe à la création ? Instagram, non.
    pub(super) fn nomme_groupes(self) -> bool {
        self.cree_groupes() && self != Service::Instagram
    }
}

/// Tous les services, dans l'ordre des puces.
pub(super) fn tous_services() -> impl Iterator<Item = Service> {
    std::iter::once(Service::GoogleMessages).chain(SERVICES_META)
}

/// Un résultat de `search` (id préfixé) qui désigne un groupe, pas une
/// personne : il ne peut pas être membre d'un nouveau groupe.
pub(super) fn id_groupe(id: &str) -> bool {
    let svc = service_de(id);
    let brut = &id[svc.prefixe().len()..];
    match svc {
        Service::WhatsApp => brut.ends_with("@g.us") || brut.ends_with("@newsletter") || brut.ends_with("@broadcast"),
        // Un groupe Signal : son identifiant en base64 (44 caractères), jamais un UUID.
        Service::Signal => brut.len() == 44 && !brut.contains('-') && !brut.starts_with('+'),
        // Telegram : g<n> (groupe simple) ou c<n> (canal, supergroupe).
        Service::Telegram => brut.starts_with('g') || brut.starts_with('c'),
        _ => false,
    }
}

/// Ce que l'écran « Nouveau message » retient tant qu'il est ouvert : la puce
/// de service choisie, le mode groupe et les personnes choisies (id préfixé,
/// nom). Remis à zéro (« Tout ») à chaque ouverture.
#[derive(Default)]
pub(super) struct ChoixNouveau {
    pub filtre: Option<Service>,
    pub groupe: bool,
    pub choisis: Vec<(String, String)>,
}

impl ChoixNouveau {
    /// Le service du groupe en cours : celui de la première personne choisie.
    pub fn verrou(&self) -> Option<Service> {
        self.choisis.first().map(|(id, _)| service_de(id))
    }

    /// Une puce (None : « Tout ») peut-elle être touchée ? Une fois une
    /// personne choisie, seulement celle de son service.
    pub fn puce_permise(&self, s: Option<Service>) -> bool {
        self.verrou().is_none_or(|v| s == Some(v))
    }

    /// Choisit une puce ; vrai si le filtre a changé. Une puce sans groupes
    /// (ou « Tout ») fait sortir du mode groupe.
    pub fn choisir_filtre(&mut self, s: Option<Service>) -> bool {
        if !self.puce_permise(s) || self.filtre == s {
            return false;
        }
        self.filtre = s;
        if self.groupe && !self.groupe_offert() {
            self.groupe = false;
            self.choisis.clear();
        }
        true
    }

    /// « Créer un groupe » n'est offert que sur la puce d'un seul service
    /// qui sait créer des groupes (jamais sur « Tout »).
    pub fn groupe_offert(&self) -> bool {
        self.filtre.is_some_and(Service::cree_groupes)
    }

    /// Entre en mode groupe, ou en sort (les personnes choisies sont oubliées).
    pub fn basculer_groupe(&mut self) {
        if self.groupe {
            self.groupe = false;
            self.choisis.clear();
        } else if self.groupe_offert() {
            self.groupe = true;
        }
    }

    pub fn est_choisi(&self, id: &str) -> bool {
        self.choisis.iter().any(|(i, _)| i == id)
    }

    /// Ajoute une personne, ou l'enlève si elle l'était déjà. La première
    /// choisie fixe le service du groupe (sa puce est choisie). Faux si elle
    /// est refusée (autre service, groupe, service sans groupes).
    pub fn basculer(&mut self, id: &str, nom: &str) -> bool {
        if let Some(i) = self.choisis.iter().position(|(x, _)| x == id) {
            self.choisis.remove(i);
            return true;
        }
        let svc = service_de(id);
        if id_groupe(id) || !svc.cree_groupes() || self.verrou().is_some_and(|v| v != svc) || self.choisis.len() >= 50 {
            return false;
        }
        self.choisis.push((id.to_string(), nom.to_string()));
        self.filtre = Some(svc);
        true
    }

    /// Les résultats (et la recherche dans les ponts) de ce service sont-ils montrés ?
    pub fn montre(&self, svc: Service) -> bool {
        self.filtre.is_none_or(|f| f == svc) && (!self.groupe || svc.cree_groupes())
    }

    /// Les membres pour `create_group` : ids sans le préfixe du service.
    pub fn membres(&self) -> Vec<String> {
        self.choisis.iter().map(|(id, _)| id[service_de(id).prefixe().len()..].to_string()).collect()
    }

    /// Le bouton « Créer » : au moins deux personnes.
    pub fn pret(&self) -> bool {
        self.groupe && self.choisis.len() >= 2
    }
}

/// La hauteur d'une puce (et le côté d'une puce de service non choisie).
const HAUT_PUCE: f32 = 28.;
const ECART_PUCES: f32 = 4.;
const LOGO_PUCE: f32 = 18.;
/// La place des puces : la liste (370 px) moins ses marges (2 × 14 px).
const LARGEUR_PUCES: f32 = 342.;

/// La largeur estimée d'un texte de puce (13 px ; la police EGA est plus large).
fn largeur_texte(texte: &str) -> f32 {
    texte.chars().count() as f32 * if th().ega { 8. } else { 7. }
}

/// Le nombre de lignes des puces de service (elles passent à la ligne) :
/// « Tout » avec son texte, les services en icônes rondes, le service
/// choisi avec son nom. Sert à la hauteur de l'en-tête.
fn lignes_puces(filtre: Option<Service>, services: &[Service], largeur: f32) -> usize {
    let tout = 20. + largeur_texte(t("Tout"));
    let mut lignes = 1;
    let mut x = tout;
    for s in services {
        let l = if filtre == Some(*s) { 20. + LOGO_PUCE + 6. + largeur_texte(s.court()) } else { HAUT_PUCE };
        if x + ECART_PUCES + l > largeur {
            lignes += 1;
            x = l;
        } else {
            x += ECART_PUCES + l;
        }
    }
    lignes
}

/// Une puce : service (avec son logo), « Tout » ou personne choisie.
fn puce(id: impl Into<gpui::ElementId>, actif: bool, permise: bool) -> gpui::Stateful<Div> {
    div()
        .id(id)
        .flex_none()
        .flex()
        .flex_row()
        .items_center()
        .gap(px(6.))
        .h(px(HAUT_PUCE))
        .px(px(10.))
        .rounded(rayon(999.))
        .border_1()
        .border_color(if actif { th().accent } else { th().trait_fin })
        .bg(if actif { th().choisi } else { th().champ })
        .text_size(px(13.))
        .text_color(if actif {
            th().accent
        } else if permise {
            th().texte_fort
        } else {
            th().texte_pale
        })
        .when(actif, |d| d.font_weight(FontWeight::SEMIBOLD))
        // EGA : pas de transparence, le texte pâle suffit.
        .when(!permise && !th().ega, |d| d.opacity(0.45))
        .when(permise, |d| d.cursor_pointer().hover(|s| s.bg(th().survol)))
}

impl Jasette {
    /// Ouvre (ou ferme) « Nouveau message » : toujours sur « Tout », sans groupe.
    pub(super) fn basculer_nouveau(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        self.nouveau_ouvert = !self.nouveau_ouvert;
        self.choix_nouveau = ChoixNouveau::default();
        self.nom_groupe.update(cx, |s, cx| s.set_value("", window, cx));
        if self.nouveau_ouvert {
            self.numero.update(cx, |s, cx| s.focus(window, cx));
        }
        self.chercher_nouveau();
    }

    /// Ferme « Nouveau message » (après un choix).
    pub(super) fn fermer_nouveau(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        self.nouveau_ouvert = false;
        self.choix_nouveau = ChoixNouveau::default();
        self.numero.update(cx, |s, cx| s.set_value("", window, cx));
        self.nom_groupe.update(cx, |s, cx| s.set_value("", window, cx));
    }

    /// Le service est-il connecté (il a sa puce) ?
    pub(super) fn service_connecte(&self, s: Service) -> bool {
        if s == Service::GoogleMessages { self.etat.associe() } else { self.meta(s).etat.associe() }
    }

    fn service_cherche(&self, s: Service) -> bool {
        if s == Service::GoogleMessages { self.etat == Etat::Pret } else { self.meta(s).etat == Etat::Pret }
    }

    /// Relance la recherche de personnes dans les ponts : seulement ceux de
    /// la puce choisie (et, en mode groupe, ceux qui savent créer un groupe).
    pub(super) fn chercher_nouveau(&mut self) {
        self.resultats_meta.clear();
        let texte = self.nouveau_texte.clone();
        if !self.nouveau_ouvert || texte.chars().count() < 2 || est_numero(&texte) {
            return;
        }
        for svc in tous_services() {
            if self.choix_nouveau.montre(svc) && self.service_cherche(svc) {
                self.commande_svc(svc, json!({"cmd": "search", "text": texte}));
            }
        }
    }

    /// Envoie `create_group` au pont du service choisi, puis ferme l'écran :
    /// le pont répond par la conversation (open), qui s'ouvre.
    fn creer_groupe(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        let Some(svc) = self.choix_nouveau.verrou().filter(|_| self.choix_nouveau.pret()) else {
            return;
        };
        let nom = if svc.nomme_groupes() { self.nom_groupe.read(cx).value().trim().to_string() } else { String::new() };
        self.commande_svc(svc, json!({"cmd": "create_group", "members": self.choix_nouveau.membres(), "name": nom}));
        self.fermer_nouveau(window, cx);
    }

    /// Sous le champ « À : » : les puces de service, « Créer un groupe », les
    /// personnes choisies et le bouton « Créer ». Rend l'élément et sa hauteur.
    pub(super) fn entete_nouveau(&self, cx: &mut Context<Self>) -> (Div, f32) {
        let choix = &self.choix_nouveau;
        let mut col = div().flex().flex_col().px(px(14.));
        let mut haut = 0.;
        // Les puces de service : Tout, puis chaque service connecté, en
        // icônes rondes ; la puce choisie montre aussi son nom. Elles passent
        // à la ligne plutôt que de défiler (panneau étroit, beaucoup de services).
        let services: Vec<Service> = tous_services().filter(|s| self.service_connecte(*s)).collect();
        let lignes = lignes_puces(choix.filtre, &services, LARGEUR_PUCES);
        let mut rangee = div()
            .id("nv-services")
            .flex()
            .flex_row()
            .flex_wrap()
            .items_center()
            .gap(px(ECART_PUCES))
            .pb(px(8.))
            .child(
                puce("nv-tout", choix.filtre.is_none(), choix.puce_permise(None)).child(t("Tout")).on_click(cx.listener(
                    |this, _, _, cx| {
                        if this.choix_nouveau.choisir_filtre(None) {
                            this.chercher_nouveau();
                        }
                        cx.notify();
                    },
                )),
            );
        for s in services {
            let choisie = choix.filtre == Some(s);
            let nom = s.court();
            rangee = rangee.child(
                puce(SharedString::from(format!("nv-{}", s.cle())), choisie, choix.puce_permise(Some(s)))
                    .child(logo(s, LOGO_PUCE))
                    // Les autres : l'icône seule, ronde, le nom dans l'infobulle.
                    .when(choisie, |d| d.child(nom))
                    .when(!choisie, |d| {
                        d.w(px(HAUT_PUCE))
                            .px(px(0.))
                            .justify_center()
                            .tooltip(move |w, cx| gpui_component::tooltip::Tooltip::new(nom).build(w, cx))
                    })
                    .on_click(cx.listener(move |this, _, _, cx| {
                        if this.choix_nouveau.choisir_filtre(Some(s)) {
                            this.chercher_nouveau();
                        }
                        cx.notify();
                    })),
            );
        }
        col = col.child(rangee);
        haut += 8. + lignes as f32 * HAUT_PUCE + (lignes - 1) as f32 * ECART_PUCES;
        // Une courte explication quand des puces sont désactivées.
        let explication = choix.verrou().is_some().then(|| t("Un groupe reste sur un seul service").to_string());
        // « Créer un groupe » : une rangée qui bascule le mode groupe.
        let offert = choix.groupe_offert();
        // Seulement sur la puce d'un service qui sait créer des groupes.
        if offert || choix.groupe {
        col = col.child(
            div()
                .id("nv-groupe")
                .flex()
                .flex_row()
                .items_center()
                .gap(px(10.))
                .px(px(8.))
                .h(px(44.))
                .rounded(rayon(12.))
                .when(choix.groupe, |d| d.bg(th().choisi))
                .when(offert, |d| d.cursor_pointer().hover(|s| s.bg(th().survol)))
                .when(!offert && !th().ega, |d| d.opacity(0.5))
                .on_click(cx.listener(|this, _, window, cx| {
                    this.choix_nouveau.basculer_groupe();
                    if !this.choix_nouveau.groupe {
                        this.nom_groupe.update(cx, |s, cx| s.set_value("", window, cx));
                    }
                    this.chercher_nouveau();
                    cx.notify();
                }))
                .child(
                    div()
                        .size(px(30.))
                        .flex_none()
                        .rounded(rayon(999.))
                        .flex()
                        .items_center()
                        .justify_center()
                        .bg(if choix.groupe { th().accent } else { th().champ })
                        .child(icone("users", 16., if choix.groupe { blanc(1.) } else { th().accent })),
                )
                .child(
                    div()
                        .flex()
                        .flex_col()
                        .flex_1()
                        .min_w(px(0.))
                        .child(
                            div()
                                .truncate()
                                .text_color(if offert { th().texte_fort } else { th().texte_pale })
                                .font_weight(FontWeight::SEMIBOLD)
                                .child(t("Créer un groupe")),
                        )
                        .when(!offert, |d| {
                            d.child(
                                div()
                                    .truncate()
                                    .text_size(px(12.))
                                    .text_color(th().texte_pale)
                                    .child(t("Pas encore offert pour ce service")),
                            )
                        })
                        .when(offert && choix.groupe && choix.choisis.is_empty(), |d| {
                            d.child(
                                div()
                                    .truncate()
                                    .text_size(px(12.))
                                    .text_color(th().texte_pale)
                                    .child(t("Choisis les personnes du groupe")),
                            )
                        }),
                )
                .when(choix.groupe, |d| d.child(icone("x", 16., th().texte_pale))),
        );
        haut += 44.;
        }
        if let Some(e) = explication {
            col = col.child(div().px(px(8.)).pt(px(2.)).h(px(20.)).truncate().text_size(px(12.)).text_color(th().texte_pale).child(e));
            haut += 20.;
        }
        if !choix.groupe || choix.choisis.is_empty() {
            return (col, haut);
        }
        // Les personnes choisies : une puce chacune, ✕ pour l'enlever.
        let mut gens = div().id("nv-choisis").flex().flex_row().items_center().gap(px(6.)).pt(px(6.)).pb(px(6.)).overflow_x_scroll();
        for (id, nom) in &choix.choisis {
            let svc = service_de(id);
            let photo = if svc == Service::GoogleMessages { cle_numero(&id[..]).and_then(|k| self.photos_numeros.get(&k)) } else { None };
            let cle = id.clone();
            gens = gens.child(
                puce(SharedString::from(format!("nv-choisi-{id}")), true, true)
                    .pl(px(3.))
                    .child(avatar(nom, "", false, photo, 22.))
                    .child(div().max_w(px(140.)).truncate().child(nom.clone()))
                    .child(icone("x", 14., th().accent))
                    .on_click(cx.listener(move |this, _, _, cx| {
                        this.choix_nouveau.basculer(&cle, "");
                        cx.notify();
                    })),
            );
        }
        col = col.child(gens);
        haut += 42.;
        // Le nom (si le service le permet) et « Créer · n ».
        let svc = choix.verrou().unwrap_or(Service::GoogleMessages);
        let pret = choix.pret();
        let mut bas = div().flex().flex_row().items_center().gap(px(8.)).pb(px(8.)).h(px(46.));
        if svc.nomme_groupes() {
            bas = bas.child(
                champ_rond()
                    .flex_1()
                    .min_w(px(0.))
                    .pl(px(12.))
                    .pr(px(6.))
                    .py(px(2.))
                    .child(Input::new(&self.nom_groupe).appearance(false).cleanable(true)),
            );
        } else {
            bas = bas.child(div().flex_1().text_size(px(12.)).text_color(th().texte_pale).child(if pret {
                ""
            } else {
                t("Choisis au moins deux personnes")
            }));
        }
        bas = bas.child(
            div()
                .id("nv-creer")
                .flex_none()
                .h(px(34.))
                .px(px(14.))
                .rounded(rayon(999.))
                .flex()
                .items_center()
                .text_size(px(14.))
                .font_weight(FontWeight::SEMIBOLD)
                .bg(if pret { th().accent } else { th().champ })
                .text_color(if pret { blanc(1.) } else { th().texte_pale })
                .when(pret, |d| d.cursor_pointer())
                .child(format!("{} · {}", t("Créer"), choix.choisis.len()))
                .on_click(cx.listener(|this, _, window, cx| {
                    this.creer_groupe(window, cx);
                    cx.notify();
                })),
        );
        col = col.child(bas);
        haut += 46.;
        (col, haut)
    }

    /// Les choix sous « Nouveau message » : conversations existantes (tous
    /// les comptes), personnes trouvées par les ponts, ou un numéro tapé ;
    /// seulement ceux de la puce choisie. En mode groupe : les personnes
    /// seulement, cochées quand elles sont choisies (les toucher les ajoute
    /// ou les enlève).
    pub(super) fn suggestions(&self, cx: &mut Context<Self>) -> (Div, usize) {
        let q = plier(&self.nouveau_texte);
        let choix = &self.choix_nouveau;
        let groupe = choix.groupe;
        let mut boite = div().flex().flex_col().px(px(8.)).pb(px(10.));
        let mut n = 0;
        if q.is_empty() {
            return (boite, 0);
        }
        let ligne = |id: SharedString, nom: String, detail: String, service: Service, photo: Option<&PathBuf>, coche: Option<bool>| {
            div()
                .id(id)
                .flex()
                .flex_row()
                .items_center()
                .gap(px(10.))
                .px(px(8.))
                .h(px(50.))
                .rounded(rayon(12.))
                .cursor_pointer()
                .hover(|s| s.bg(th().survol))
                .when(coche == Some(true), |d| d.bg(th().choisi))
                .child(
                    div()
                        .relative()
                        .child(avatar(&nom, "", false, photo, 36.))
                        .child(div().absolute().right(px(-3.)).bottom(px(-3.)).child(logo(service, 15.))),
                )
                .child(
                    div()
                        .flex()
                        .flex_col()
                        .flex_1()
                        .min_w(px(0.))
                        .child(div().truncate().text_color(th().texte_fort).child(nom))
                        .child(div().truncate().text_size(px(12.)).text_color(th().texte_pale).child(detail)),
                )
                .when_some(coche, |d, oui| {
                    d.child(
                        div()
                            .size(px(22.))
                            .flex_none()
                            .rounded(rayon(999.))
                            .border_2()
                            .border_color(if oui { th().accent } else { th().trait_fin })
                            .when(oui, |d| d.bg(th().accent))
                            .flex()
                            .items_center()
                            .justify_center()
                            .when(oui, |d| d.child(icone("check", 14., blanc(1.)))),
                    )
                })
        };
        // Les conversations qui existent déjà (pas en mode groupe : ce sont des fils, pas des personnes).
        if !groupe {
            let existantes: Vec<&Conv> = self
                .convs
                .iter()
                .chain(self.archives.iter())
                .filter(|c| {
                    !c.group
                        && choix.montre(service_de(&c.id))
                        && !self.privee(&c.id)
                        && (plier(&c.name).contains(&q) || c.members.iter().any(|m| plier(m).contains(&q)))
                })
                .take(4)
                .collect();
            for c in existantes {
                let id = c.id.clone();
                let s = service_de(&c.id);
                boite = boite.child(
                    ligne(SharedString::from(format!("sugg-{}", c.id)), c.name.clone(), s.nom().into(), s, self.photo_conv(c), None)
                        .on_click(cx.listener(move |this, _, window, cx| {
                            this.fermer_nouveau(window, cx);
                            this.vue = Vue::Tout;
                            this.galerie = None;
                            this.choisir(id.clone());
                            cx.notify();
                        })),
                );
                n += 1;
            }
        }
        // Les personnes trouvées par les ponts (en mode groupe, même avec une conversation ici).
        let mut montres = 0;
        for (id, nom) in &self.resultats_meta {
            let svc = service_de(id);
            if !choix.montre(svc) || (groupe && id_groupe(id)) || (!groupe && self.conv(id).is_some()) {
                continue;
            }
            if montres >= if groupe { 12 } else { 8 } {
                break;
            }
            montres += 1;
            let fbid = id[svc.prefixe().len()..].to_string();
            let detail = match svc {
                Service::Instagram => t("Nouvelle conversation Instagram").to_string(),
                Service::GoogleMessages => format!("{} · {id}", t("Contact")),
                Service::WhatsApp => t("Nouvelle conversation WhatsApp").to_string(),
                Service::Signal => t("Nouvelle conversation Signal").to_string(),
                Service::X => t("Nouvelle conversation X").to_string(),
                Service::Telegram => t("Nouvelle conversation Telegram").to_string(),
                Service::Bluesky => t("Nouvelle conversation Bluesky").to_string(),
                Service::Discord => t("Nouvelle conversation Discord").to_string(),
                Service::Messenger => t("Nouvelle conversation Messenger").to_string(),
            };
            let detail = if groupe { svc.court().to_string() } else { detail };
            // Un contact du téléphone : sa photo, s'il en a une.
            let photo = if svc == Service::GoogleMessages { cle_numero(id).and_then(|k| self.photos_numeros.get(&k)) } else { None };
            let coche = groupe.then(|| choix.est_choisi(id));
            let (cle, nom_c) = (id.clone(), nom.clone());
            boite = boite.child(
                ligne(SharedString::from(format!("sugg-{id}")), nom.clone(), detail, svc, photo, coche).on_click(cx.listener(
                    move |this, _, window, cx| {
                        if this.choix_nouveau.groupe {
                            Self::choisir_membre(this, &cle, &nom_c, window, cx);
                        } else {
                            this.commande_svc(svc, json!({"cmd": "new", "number": fbid}));
                            this.fermer_nouveau(window, cx);
                        }
                        cx.notify();
                    },
                )),
            );
            n += 1;
        }
        // Un numéro (avec l'indicatif pour WhatsApp et Signal) : Google, WhatsApp, Signal.
        if est_numero(&self.nouveau_texte) {
            for svc in [Service::GoogleMessages, Service::WhatsApp, Service::Signal] {
                let dispo = if svc == Service::GoogleMessages { self.etat.associe() } else { self.meta(svc).etat == Etat::Pret };
                if !dispo || !choix.montre(svc) {
                    continue;
                }
                let numero = self.nouveau_texte.clone();
                let (id, texte) = match svc {
                    Service::GoogleMessages => ("sugg-sms", t("Nouveau message par Google Messages")),
                    Service::WhatsApp => ("sugg-wa", t("Nouveau message par WhatsApp")),
                    _ => ("sugg-signal", t("Nouveau message par Signal")),
                };
                let cle = format!("{}{numero}", svc.prefixe());
                let coche = groupe.then(|| choix.est_choisi(&cle));
                let detail = if groupe { svc.court().to_string() } else { texte.to_string() };
                boite = boite.child(ligne(id.into(), numero.clone(), detail, svc, None, coche).on_click(cx.listener(
                    move |this, _, window, cx| {
                        if this.choix_nouveau.groupe {
                            Self::choisir_membre(this, &cle, &numero, window, cx);
                        } else {
                            this.commande_svc(svc, json!({"cmd": "new", "number": numero}));
                            this.fermer_nouveau(window, cx);
                        }
                        cx.notify();
                    },
                )));
                n += 1;
            }
        }
        (boite, n)
    }

    /// Mode groupe : ajoute (ou enlève) une personne ; la première fixe le
    /// service, et la recherche ne se fait plus que là. Le champ se vide pour
    /// chercher la suivante.
    fn choisir_membre(this: &mut Self, id: &str, nom: &str, window: &mut Window, cx: &mut Context<Self>) {
        let avant = this.choix_nouveau.filtre;
        let deja = this.choix_nouveau.est_choisi(id);
        if this.choix_nouveau.basculer(id, nom) && !deja {
            this.numero.update(cx, |s, cx| {
                s.set_value("", window, cx);
                s.focus(window, cx);
            });
        }
        if this.choix_nouveau.filtre != avant {
            this.chercher_nouveau();
        }
    }
}

#[cfg(test)]
mod essais {
    use super::*;

    #[test]
    fn puces_compactes() {
        // Les neuf services en icônes, « Tout » choisi : une seule ligne.
        let tous: Vec<Service> = tous_services().collect();
        assert_eq!(tous.len(), 9);
        assert_eq!(lignes_puces(None, &tous, LARGEUR_PUCES), 1);
        // Un service choisi montre son nom : la rangée passe à la ligne, sans défiler.
        assert_eq!(lignes_puces(Some(Service::GoogleMessages), &tous, LARGEUR_PUCES), 2);
        // Sept services, Signal choisi : tout tient.
        assert_eq!(lignes_puces(Some(Service::Signal), &tous[..7], LARGEUR_PUCES), 1);
    }

    #[test]
    fn puces_et_groupe() {
        let mut c = ChoixNouveau::default();
        assert!(c.filtre.is_none() && !c.groupe_offert(), "pas de groupe sur « Tout »");
        assert!(c.choisir_filtre(Some(Service::X)));
        assert!(!c.groupe_offert(), "X ne crée pas de groupe");
        c.basculer_groupe();
        assert!(!c.groupe);
        assert!(c.choisir_filtre(Some(Service::WhatsApp)));
        c.basculer_groupe();
        assert!(c.groupe);
        assert!(!c.montre(Service::X) && c.montre(Service::WhatsApp));
        // La première personne fixe le service.
        assert!(c.basculer("w:15145550101@s.whatsapp.net", "Ana"));
        assert!(c.filtre == Some(Service::WhatsApp));
        assert!(!c.puce_permise(None) && !c.puce_permise(Some(Service::Signal)));
        assert!(!c.choisir_filtre(Some(Service::Signal)));
        assert!(!c.basculer("s:+15145550102", "Bob"), "un seul service par groupe");
        assert!(!c.basculer("w:1203630000@g.us", "Un groupe"), "un groupe n'est pas une personne");
        assert!(c.basculer("w:+15145550103", "+15145550103"));
        assert!(c.pret());
        assert_eq!(c.membres(), vec!["15145550101@s.whatsapp.net".to_string(), "+15145550103".to_string()]);
        // La toucher de nouveau l'enlève ; plus personne : les puces reviennent.
        assert!(c.basculer("w:15145550101@s.whatsapp.net", ""));
        assert!(!c.pret());
        assert!(c.basculer("w:+15145550103", ""));
        assert!(c.choisis.is_empty() && c.puce_permise(None) && c.puce_permise(Some(Service::Signal)));
        // « Tout » (ou un service sans groupes) fait sortir du mode groupe.
        assert!(c.choisir_filtre(None));
        assert!(!c.groupe);
        // Sortir du mode groupe oublie tout le monde.
        c.choisir_filtre(Some(Service::Messenger));
        c.basculer_groupe();
        c.basculer("m:100", "Cy");
        c.basculer_groupe();
        assert!(!c.groupe && c.choisis.is_empty());
    }

    #[test]
    fn ids_de_groupe() {
        assert!(id_groupe("w:1203630000@g.us"));
        assert!(!id_groupe("w:15145550101@s.whatsapp.net"));
        assert!(id_groupe("t:g123") && id_groupe("t:c123") && !id_groupe("t:u123"));
        assert!(id_groupe("s:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="));
        assert!(!id_groupe("s:6f1c2c43-6c1f-4d0b-9e0c-0a1b2c3d4e5f"));
        assert!(!id_groupe("m:100") && !id_groupe("5145550101"));
    }

    #[test]
    fn services_de_groupe() {
        let avec: Vec<Service> = tous_services().filter(|s| s.cree_groupes()).collect();
        assert_eq!(avec.len(), 7);
        assert!(!Service::X.cree_groupes() && !Service::Bluesky.cree_groupes());
        assert!(Service::Discord.cree_groupes() && Service::Discord.nomme_groupes());
        assert!(!Service::Instagram.nomme_groupes() && Service::WhatsApp.nomme_groupes());
    }
}
