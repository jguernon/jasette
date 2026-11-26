//! L'onglet Groupes (PROTOCOLE.md, §5.13) : les grands espaces seulement —
//! serveurs Discord et leurs salons, canaux et supergroupes Telegram,
//! communautés WhatsApp, canaux de diffusion et discussions de communauté
//! de Messenger. Les groupes de personnes restent dans la boîte de réception.
//!
//! Une section repliable par service (badge, nombre d'espaces non lus ;
//! l'état replié est gardé dans les réglages de cet appareil). Discord : la
//! liste des serveurs ; un serveur ouvre ses salons rangés par catégorie.

use super::*;

fn tr(fr: &'static str, en: &'static str) -> &'static str {
    if anglais() { en } else { fr }
}

/// L'ordre des sections : les services qui ont le plus de grands espaces d'abord.
const ORDRE_GROUPES: [Service; 9] = [
    Service::Discord,
    Service::Telegram,
    Service::WhatsApp,
    Service::Messenger,
    Service::Instagram,
    Service::Signal,
    Service::X,
    Service::Bluesky,
    Service::GoogleMessages,
];

/// L'icône d'un grand espace selon sa sorte.
fn icone_sorte(c: &Conv) -> &'static str {
    match c.kind.as_str() {
        "server" if c.announce => "megaphone",
        "server" => "hash",
        "channel" | "broadcast" | "newsletter" => "volume-2",
        _ if c.announce => "megaphone",
        _ => "users",
    }
}

/// Le rang d'un salon Discord : sans catégorie d'abord, puis par position
/// de catégorie, position du salon et nom.
fn rang_salon(c: &Conv) -> (bool, i64, String, i64, String) {
    (!c.section.is_empty(), c.section_order, plier(&c.section), c.order, plier(&c.name))
}

/// Une pastille de nombre, dans la ligne (pas posée en coin).
fn compteur(texte: String, couleur: Hsla) -> Div {
    div()
        .flex_none()
        .min_w(px(20.))
        .h(px(20.))
        .px(px(6.))
        .rounded(rayon(999.))
        .bg(couleur)
        .flex()
        .items_center()
        .justify_center()
        .text_size(px(11.))
        .font_weight(FontWeight::BOLD)
        .text_color(blanc(1.))
        .child(texte)
}

/// Le sous-titre du fil d'un grand espace : « Pixel Nord · Discord »,
/// « Canal · Telegram »…
pub(crate) fn detail_espace(c: &Conv, service: Service) -> String {
    let lieu = if !c.parent_name.is_empty() {
        c.parent_name.clone()
    } else {
        match c.kind.as_str() {
            "channel" => tr("Canal", "Channel"),
            "supergroup" => tr("Supergroupe", "Supergroup"),
            "community" => tr("Communauté", "Community"),
            "newsletter" => tr("Chaîne", "Channel"),
            "broadcast" => tr("Canal de diffusion", "Broadcast channel"),
            "community_chat" => tr("Discussion de communauté", "Community chat"),
            _ => tr("Groupe", "Group"),
        }
        .to_string()
    };
    format!("{lieu} · {}", service.nom())
}

impl Jasette {
    /// Les grands espaces visibles (sans les privés ; recherche et « non lus
    /// seulement » compris), par service.
    fn espaces(&self) -> Vec<&Conv> {
        self.convs.iter().filter(|c| c.large && self.correspond(c)).collect()
    }

    /// Menu ⋮ d'un grand espace : « Montrer dans la boîte de réception » ou
    /// « Garder dans Groupes seulement ». Rien pour une autre conversation.
    pub(crate) fn item_menu_groupes(&self, id: &str, cx: &mut Context<Self>) -> Option<AnyElement> {
        if !self.conv(id)?.large {
            return None;
        }
        let montre = self.reglages.groupes_inbox.iter().any(|x| x == id);
        let texte = if montre {
            tr("Garder dans Groupes seulement", "Keep in Groups only")
        } else {
            tr("Montrer dans la boîte de réception", "Show in inbox")
        };
        let id = id.to_string();
        Some(
            div()
                .id("menu-groupes")
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
                .child(icone(if montre { "users" } else { "inbox" }, 16., th().texte_fort))
                // Le texte passe à la ligne plutôt que de déborder (police EGA).
                .child(div().flex_1().min_w(px(0.)).child(texte))
                .on_click(cx.listener(move |this, _, _, cx| {
                    this.montrer_dans_boite(&id, !montre);
                    this.menu_conv = false;
                    cx.notify();
                }))
                .into_any_element(),
        )
    }

    /// La liste de l'onglet Groupes (à la place de la liste des conversations).
    pub(crate) fn liste_groupes(&self, haut: f32, cx: &mut Context<Self>) -> gpui::Stateful<Div> {
        let mut col = div()
            .id("groupes")
            .flex()
            .flex_col()
            .size_full()
            .overflow_y_scroll()
            .track_scroll(&self.defil_liste)
            .px(px(8.))
            .pt(px(haut))
            .pb(px(18.));
        let espaces = self.espaces();
        if let Some(serveur) = self.serveur_ouvert.clone() {
            let salons: Vec<&Conv> =
                espaces.iter().copied().filter(|c| format!("{}{}", service_de(&c.id).prefixe(), c.parent) == serveur).collect();
            if !salons.is_empty() {
                return self.salons_serveur(col, salons, cx);
            }
        }
        if espaces.is_empty() {
            let texte = if self.filtre.is_empty() && !self.non_lus_seulement {
                tr(
                    "Aucun groupe. Les serveurs Discord, les canaux et supergroupes Telegram, les communautés WhatsApp et les canaux de diffusion apparaîtront ici.",
                    "No groups. Discord servers, Telegram channels and supergroups, WhatsApp communities and broadcast channels will show up here.",
                )
            } else {
                tr("Aucun résultat", "No results")
            };
            return col.child(div().p(px(16.)).text_color(th().texte_pale).child(texte));
        }
        for svc in ORDRE_GROUPES {
            let mut miens: Vec<&Conv> = espaces.iter().copied().filter(|c| service_de(&c.id) == svc).collect();
            if miens.is_empty() {
                continue;
            }
            miens.sort_by(|a, b| b.time.cmp(&a.time));
            let non_lus = miens.iter().filter(|c| c.unread).count();
            let cle = svc.id_rail();
            // Une recherche ouvre toutes les sections.
            let replie = self.filtre.is_empty() && self.reglages.groupes_replies.iter().any(|x| x == cle);
            col = col.child(
                div()
                    .id(SharedString::from(format!("gs-{cle}")))
                    .flex()
                    .flex_row()
                    .items_center()
                    .gap(px(10.))
                    .mt(px(6.))
                    .px(px(10.))
                    .py(px(8.))
                    .rounded(rayon(10.))
                    .cursor_pointer()
                    .hover(|s| s.bg(th().survol))
                    .on_click(cx.listener(move |this, _, _, cx| {
                        let l = &mut this.reglages.groupes_replies;
                        if l.iter().any(|x| x == cle) {
                            l.retain(|x| x != cle);
                        } else {
                            l.push(cle.to_string());
                        }
                        this.reglages.garder();
                        cx.notify();
                    }))
                    .child(icone(if replie { "chevron-right" } else { "chevron-down" }, 16., th().texte_pale))
                    .child(logo(svc, 22.))
                    .child(
                        div()
                            .flex_1()
                            .truncate()
                            .text_size(px(14. + self.plus_liste()))
                            .font_weight(FontWeight::BOLD)
                            .text_color(th().texte_fort)
                            .child(svc.nom()),
                    )
                    .when(non_lus > 0, |d| d.child(compteur(non_lus.to_string(), th().accent))),
            );
            if replie {
                continue;
            }
            col = if svc == Service::Discord {
                self.serveurs_discord(col, &miens, cx)
            } else {
                self.espaces_service(col, &miens, cx)
            };
        }
        col
    }

    /// Discord : une ligne par serveur (les plus actifs d'abord).
    fn serveurs_discord(&self, mut col: gpui::Stateful<Div>, salons: &[&Conv], cx: &mut Context<Self>) -> gpui::Stateful<Div> {
        let mut serveurs: Vec<(String, String, i64, usize, u32, usize)> = Vec::new();
        for c in salons {
            let cle = format!("d:{}", c.parent);
            let attention = (c.unread as u32) * c.mentions + self.attention.contains(&c.id) as u32;
            match serveurs.iter_mut().find(|s| s.0 == cle) {
                Some(s) => {
                    s.2 = s.2.max(c.time);
                    s.3 += c.unread as usize;
                    s.4 += attention;
                    s.5 += 1;
                }
                None => serveurs.push((cle, c.parent_name.clone(), c.time, c.unread as usize, attention, 1)),
            }
        }
        serveurs.sort_by(|a, b| b.2.cmp(&a.2));
        for (cle, nom, temps, non_lus, mentions, n) in serveurs {
            let nom = if nom.is_empty() { tr("Serveur", "Server").to_string() } else { nom };
            let ouvrir = cle.clone();
            let detail = if n == 1 { tr("1 salon", "1 channel").to_string() } else { format!("{n} {}", tr("salons", "channels")) };
            col = col.child(
                div()
                    .id(SharedString::from(format!("srv-{cle}")))
                    .flex()
                    .flex_row()
                    .items_center()
                    .gap(px(12.))
                    .pl(px(18.))
                    .pr(px(10.))
                    .py(px(7.))
                    .rounded(rayon(mesure(14., 10.)))
                    .cursor_pointer()
                    .hover(|s| s.bg(th().survol))
                    .on_click(cx.listener(move |this, _, _, cx| {
                        this.serveur_ouvert = Some(ouvrir.clone());
                        cx.notify();
                    }))
                    .child(avatar(&nom, "", true, None, 40.))
                    .child(
                        div()
                            .flex()
                            .flex_col()
                            .flex_1()
                            .min_w(px(0.))
                            .child(
                                div()
                                    .truncate()
                                    .text_size(px(15. + self.plus_liste()))
                                    .font_weight(if non_lus > 0 { FontWeight::BOLD } else { FontWeight::SEMIBOLD })
                                    .text_color(th().texte_fort)
                                    .child(nom),
                            )
                            .child(div().text_size(px(12. + self.plus_liste())).text_color(th().texte_pale).child(detail)),
                    )
                    .child(
                        div()
                            .flex_none()
                            .flex()
                            .flex_row()
                            .items_center()
                            .gap(px(6.))
                            .when(temps > 0, |d| {
                                d.child(div().text_size(px(12.)).text_color(th().texte_pale).child(heure(temps, true)))
                            })
                            .when(mentions > 0, |d| d.child(compteur(format!("@{mentions}"), ton(c(DANGER), th().danger))))
                            .when(non_lus > 0, |d| d.child(compteur(non_lus.to_string(), th().accent)))
                            .child(icone("chevron-right", 16., th().texte_pale)),
                    ),
            );
        }
        col
    }

    /// Un serveur Discord ouvert : ses salons, rangés par catégorie.
    fn salons_serveur(&self, mut col: gpui::Stateful<Div>, mut salons: Vec<&Conv>, cx: &mut Context<Self>) -> gpui::Stateful<Div> {
        salons.sort_by_key(|c| rang_salon(c));
        let nom = salons.first().map(|c| c.parent_name.clone()).filter(|n| !n.is_empty()).unwrap_or_else(|| tr("Serveur", "Server").into());
        col = col.child(
            div()
                .id("srv-retour")
                .flex()
                .flex_row()
                .items_center()
                .gap(px(8.))
                .px(px(8.))
                .py(px(8.))
                .rounded(rayon(10.))
                .cursor_pointer()
                .hover(|s| s.bg(th().survol))
                .on_click(cx.listener(|this, _, _, cx| {
                    this.serveur_ouvert = None;
                    cx.notify();
                }))
                .child(icone("chevron-left", 18., th().texte_fort))
                .child(logo(Service::Discord, 22.))
                .child(
                    div()
                        .flex_1()
                        .truncate()
                        .text_size(px(16. + self.plus_liste()))
                        .font_weight(FontWeight::BOLD)
                        .text_color(th().texte_fort)
                        .child(nom),
                ),
        );
        let mut section: Option<String> = None;
        for conv in salons {
            if section.as_deref() != Some(conv.section.as_str()) {
                section = Some(conv.section.clone());
                if !conv.section.is_empty() {
                    col = col.child(
                        div()
                            .px(px(12.))
                            .pt(px(12.))
                            .pb(px(4.))
                            .text_size(px(11.5 + self.plus_liste()))
                            .font_weight(FontWeight::BOLD)
                            .text_color(th().texte_pale)
                            .child(conv.section.to_uppercase()),
                    );
                }
            }
            col = col.child(self.ligne_salon(conv, cx));
        }
        col
    }

    /// Un salon Discord : « # nom », non lus et mentions.
    fn ligne_salon(&self, conv: &Conv, cx: &mut Context<Self>) -> impl IntoElement {
        let choisie = self.choisie.as_deref() == Some(conv.id.as_str());
        let id = conv.id.clone();
        let mentions = if conv.unread { conv.mentions } else { 0 } + self.attention.contains(&conv.id) as u32;
        let couleur = if conv.unread || choisie { th().texte_fort } else { th().texte };
        div()
            .id(SharedString::from(format!("c-{}", conv.id)))
            .flex()
            .flex_row()
            .items_center()
            .gap(px(8.))
            .px(px(12.))
            .py(px(7.))
            .rounded(rayon(mesure(12., 8.)))
            .cursor_pointer()
            .when(choisie, |d| d.bg(th().choisi))
            .when(!choisie, |d| d.hover(|s| s.bg(th().survol)))
            .on_click(cx.listener(move |this, _, _, cx| {
                this.choisir(id.clone());
                cx.notify();
            }))
            .child(icone(icone_sorte(conv), 16., if conv.unread { th().texte_fort } else { th().texte_pale }))
            .child(
                div()
                    .flex_1()
                    .truncate()
                    .text_size(px(14.5 + self.plus_liste()))
                    .font_weight(if conv.unread { FontWeight::BOLD } else { FontWeight::NORMAL })
                    .text_color(couleur)
                    .child(conv.name.clone()),
            )
            .when(conv.read_only, |d| d.child(icone("lock", 12., th().texte_pale)))
            .when(mentions > 0, |d| d.child(compteur(format!("@{mentions}"), ton(c(DANGER), th().danger))))
            .when(conv.unread && mentions == 0, |d| {
                d.child(div().flex_none().size(px(8.)).rounded(rayon(999.)).bg(degrade_accent()))
            })
    }

    /// Les autres services : les communautés avec leurs groupes en retrait,
    /// puis les canaux, supergroupes et autres espaces.
    fn espaces_service(&self, mut col: gpui::Stateful<Div>, miens: &[&Conv], cx: &mut Context<Self>) -> gpui::Stateful<Div> {
        let prefixe = |c: &Conv| service_de(&c.id).prefixe();
        let ids: HashSet<String> = miens.iter().map(|c| c.id.clone()).collect();
        let parent_de = |c: &Conv| if c.parent.is_empty() { String::new() } else { format!("{}{}", prefixe(c), c.parent) };
        // Les enfants dont le parent n'est pas dans la liste : sous le nom du parent.
        let mut orphelins: Vec<(String, String)> = Vec::new();
        for c in miens.iter() {
            let p = parent_de(c);
            if !p.is_empty() && !ids.contains(&p) && !orphelins.iter().any(|(k, _)| *k == p) {
                orphelins.push((p, c.parent_name.clone()));
            }
        }
        // Les espaces sans parent (communautés, canaux…), leurs enfants en retrait.
        for c in miens.iter().filter(|c| parent_de(c).is_empty()) {
            col = col.child(self.ligne_espace(c, 18., cx));
            for enfant in miens.iter().filter(|e| parent_de(e) == c.id) {
                col = col.child(self.ligne_espace(enfant, 40., cx));
            }
        }
        for (p, nom) in orphelins {
            let nom = if nom.is_empty() { tr("Communauté", "Community").to_string() } else { nom };
            col = col.child(
                div()
                    .flex()
                    .flex_row()
                    .items_center()
                    .gap(px(6.))
                    .pl(px(20.))
                    .pt(px(8.))
                    .pb(px(2.))
                    .text_size(px(12. + self.plus_liste()))
                    .font_weight(FontWeight::BOLD)
                    .text_color(th().texte_pale)
                    .child(icone("users", 13., th().texte_pale))
                    .child(nom),
            );
            for enfant in miens.iter().filter(|e| parent_de(e) == p) {
                col = col.child(self.ligne_espace(enfant, 40., cx));
            }
        }
        col
    }

    /// Une ligne d'espace : photo, icône de la sorte, nom, aperçu, heure.
    fn ligne_espace(&self, conv: &Conv, retrait: f32, cx: &mut Context<Self>) -> impl IntoElement {
        let choisie = self.choisie.as_deref() == Some(conv.id.as_str());
        let id = conv.id.clone();
        let nom = if conv.name.is_empty() { t("Sans nom").to_string() } else { conv.name.clone() };
        let mentions = if conv.unread { conv.mentions } else { 0 } + self.attention.contains(&conv.id) as u32;
        let apercu = if conv.preview.trim().is_empty() { String::new() } else { une_ligne(&conv.preview) };
        div()
            .id(SharedString::from(format!("c-{}", conv.id)))
            .flex()
            .flex_row()
            .items_center()
            .gap(px(10.))
            .pl(px(retrait))
            .pr(px(10.))
            .py(px(7.))
            .rounded(rayon(mesure(14., 10.)))
            .cursor_pointer()
            .when(choisie, |d| d.bg(th().choisi))
            .when(!choisie, |d| d.hover(|s| s.bg(th().survol)))
            .on_click(cx.listener(move |this, _, _, cx| {
                this.choisir(id.clone());
                cx.notify();
            }))
            .child(self.avatar_conv(conv, &nom, 40.))
            .child(
                div()
                    .flex()
                    .flex_col()
                    .flex_1()
                    .min_w(px(0.))
                    .gap(px(1.))
                    .child(
                        div()
                            .flex()
                            .flex_row()
                            .items_center()
                            .gap(px(5.))
                            .child(icone(icone_sorte(conv), 13., th().texte_pale))
                            .child(
                                div()
                                    .flex_1()
                                    .truncate()
                                    .text_size(px(14.5 + self.plus_liste()))
                                    .font_weight(if conv.unread { FontWeight::BOLD } else { FontWeight::SEMIBOLD })
                                    .text_color(th().texte_fort)
                                    .child(nom),
                            )
                            .child(
                                div()
                                    .flex_none()
                                    .text_size(px(12. + self.plus_liste()))
                                    .text_color(if conv.unread { th().accent } else { th().texte_pale })
                                    .child(heure(conv.time, true)),
                            ),
                    )
                    .child(
                        div()
                            .flex()
                            .flex_row()
                            .items_center()
                            .gap(px(6.))
                            .child(
                                div()
                                    .flex_1()
                                    .truncate()
                                    .text_size(px(12.5 + self.plus_liste()))
                                    .text_color(if conv.unread { th().texte_fort } else { th().apercu })
                                    .child(apercu),
                            )
                            .when(mentions > 0, |d| d.child(compteur(format!("@{mentions}"), ton(c(DANGER), th().danger))))
                            .when(conv.unread && mentions == 0, |d| {
                                d.child(div().flex_none().size(px(8.)).rounded(rayon(999.)).bg(degrade_accent()))
                            }),
                    ),
            )
    }
}

#[cfg(test)]
mod essais {
    use super::*;

    fn conv(v: serde_json::Value) -> Conv {
        serde_json::from_value(v).unwrap()
    }

    #[test]
    fn boite_et_groupes() {
        let rien: HashSet<String> = HashSet::new();
        let salon = conv(serde_json::json!({"id": "d:g5:11", "name": "général", "large": true, "kind": "server", "parent": "g5"}));
        let groupe = conv(serde_json::json!({"id": "w:1@g.us", "name": "Chalet", "group": true}));
        // Un groupe de personnes reste dans la boîte ; un salon n'y va pas.
        assert!(!hors_boite(&groupe, &[], &[], &rien));
        assert!(hors_boite(&salon, &[], &[], &rien));
        // Montré dans la boîte, épinglé, ou en attente de mon attention : il y revient.
        assert!(!hors_boite(&salon, &["d:g5:11".into()], &[], &rien));
        assert!(!hors_boite(&salon, &[], &["d:g5:11".into()], &rien));
        let attention: HashSet<String> = ["d:g5:11".to_string()].into();
        assert!(!hors_boite(&salon, &[], &[], &attention));
        // Des mentions non lues comptées par le service : aussi.
        let mentionne = conv(serde_json::json!({"id": "d:g5:12", "name": "x", "large": true, "unread": true, "mentions": 2}));
        assert!(!hors_boite(&mentionne, &[], &[], &rien));
        // Lues : non.
        let lu = conv(serde_json::json!({"id": "d:g5:12", "name": "x", "large": true, "unread": false, "mentions": 2}));
        assert!(hors_boite(&lu, &[], &[], &rien));
    }

    #[test]
    fn ordre_des_salons() {
        let s = |nom: &str, section: &str, so: i64, o: i64| {
            conv(serde_json::json!({"id": nom, "name": nom, "section": section, "section_order": so, "order": o}))
        };
        let mut l = [s("jeux", "Jeux", 1, 0), s("règles", "", 0, 3), s("annonces", "Général", 0, 1), s("bienvenue", "Général", 0, 0)];
        l.sort_by_key(rang_salon);
        let noms: Vec<&str> = l.iter().map(|c| c.name.as_str()).collect();
        assert_eq!(noms, ["règles", "bienvenue", "annonces", "jeux"]);
        assert_eq!(icone_sorte(&conv(serde_json::json!({"id": "t:c1", "name": "", "kind": "channel"}))), "volume-2");
    }
}
