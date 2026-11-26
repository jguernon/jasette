//! L'onglet Groupes (PROTOCOLE.md, §5.13) : les grands espaces seulement —
//! serveurs Discord et leurs salons, canaux et supergroupes Telegram,
//! communautés WhatsApp, canaux de diffusion et discussions de communauté
//! de Messenger. Les groupes de personnes restent dans la boîte de réception.
//!
//! Une section repliable par service (badge, nombre d'espaces non lus ;
//! l'état replié est gardé dans les réglages de cet appareil). Rien n'y est
//! propre à un service : les conversations d'un même `parent` (serveur
//! Discord, communauté WhatsApp…) se rangent sous lui, qui s'ouvre sur ses
//! salons rangés par catégorie.

use super::*;

fn tr(fr: &'static str, en: &'static str) -> &'static str {
    if anglais() { en } else { fr }
}

/// L'ordre des sections : les services qui ont le plus de grands espaces d'abord.
const ORDRE_GROUPES: [Service; 11] = [
    Service::Discord,
    Service::Reddit,
    Service::Telegram,
    Service::WhatsApp,
    Service::Messenger,
    Service::Instagram,
    Service::Signal,
    Service::X,
    Service::Bluesky,
    Service::LinkedIn,
    Service::GoogleMessages,
];

/// L'icône d'un grand espace selon sa sorte.
fn icone_sorte(c: &Conv) -> &'static str {
    match c.kind.as_str() {
        "server" if c.announce => "megaphone",
        "server" => "hash",
        "channel" | "broadcast" | "newsletter" => "volume-2",
        "subreddit" => "hash",
        _ if c.announce => "megaphone",
        _ => "users",
    }
}

/// Le rang d'un salon Discord : sans catégorie d'abord, puis par position
/// de catégorie, position du salon et nom.
fn rang_salon(c: &Conv) -> (bool, i64, String, i64, String) {
    (!c.section.is_empty(), c.section_order, plier(&c.section), c.order, plier(&c.name))
}

/// La clé d'un parent, préfixée comme un id de conversation (« d:g81 »,
/// « w:4101@g.us »), ou "" sans parent.
pub(crate) fn cle_parent(c: &Conv) -> String {
    if c.parent.is_empty() { String::new() } else { format!("{}{}", service_de(&c.id).prefixe(), c.parent) }
}

/// Les parents d'une section et leurs espaces, dans l'ordre d'apparition.
/// Une conversation qui est elle-même un parent (communauté WhatsApp) va
/// avec ses enfants.
fn parents<'a>(miens: &[&'a Conv]) -> Vec<(String, Vec<&'a Conv>)> {
    let mut l: Vec<(String, Vec<&'a Conv>)> = Vec::new();
    for c in miens {
        let p = cle_parent(c);
        if p.is_empty() {
            continue;
        }
        match l.iter_mut().find(|(k, _)| *k == p) {
            Some((_, v)) => v.push(c),
            None => l.push((p, vec![c])),
        }
    }
    for (k, v) in l.iter_mut() {
        if let Some(c) = miens.iter().find(|c| c.id == *k) {
            v.insert(0, c);
        }
    }
    l
}

/// « Tout marquer comme lu » : les espaces à marquer parmi [espaces] (ceux
/// que [garder] accepte) — seulement ceux qui ont des non-lus ou des
/// mentions.
pub(crate) fn a_marquer_lu<'a>(espaces: impl Iterator<Item = &'a Conv>, garder: impl Fn(&Conv) -> bool) -> Vec<String> {
    espaces.filter(|c| c.large && (c.unread || c.mentions > 0) && garder(c)).map(|c| c.id.clone()).collect()
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
            "subreddit" => tr("Subreddit", "Subreddit"),
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
        if let Some(ouvert) = self.serveur_ouvert.clone() {
            let salons: Vec<&Conv> = espaces.iter().copied().filter(|c| cle_parent(c) == ouvert || c.id == ouvert).collect();
            if !salons.is_empty() {
                return self.salons_parent(col, &ouvert, salons, cx);
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
            let miens: Vec<&Conv> = espaces.iter().copied().filter(|c| service_de(&c.id) == svc).collect();
            if miens.is_empty() {
                continue;
            }
            let non_lus = miens.iter().filter(|c| c.unread).count();
            let a_marquer = a_marquer_lu(miens.iter().copied(), |_| true);
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
                    .when(non_lus > 0, |d| d.child(compteur(non_lus.to_string(), th().accent)))
                    .children(self.bouton_tout_lu(format!("lu-{cle}"), svc.nom().to_string(), a_marquer, cx)),
            );
            if replie {
                continue;
            }
            col = self.espaces_service(col, &miens, cx);
        }
        col
    }

    /// Le bouton « enveloppe ouverte » : demande « Tout marquer comme lu ? »
    /// pour [ids] (caché s'il n'y a rien à marquer).
    fn bouton_tout_lu(&self, id: String, cible: String, ids: Vec<String>, cx: &mut Context<Self>) -> Option<AnyElement> {
        if ids.is_empty() {
            return None;
        }
        Some(
            div()
                .id(SharedString::from(id))
                .flex_none()
                .p(px(4.))
                .rounded(rayon(8.))
                .cursor_pointer()
                .hover(|s| s.bg(th().survol))
                .tooltip(move |w, cx| gpui_component::tooltip::Tooltip::new(t("Tout marquer comme lu")).build(w, cx))
                .on_click(cx.listener(move |this, _, _, cx| {
                    cx.stop_propagation();
                    this.confirmer_tout_lu = Some((cible.clone(), ids.clone()));
                    cx.notify();
                }))
                .child(icone("mail-open", 17., th().texte_pale))
                .into_any_element(),
        )
    }

    /// La confirmation de « Tout marquer comme lu », posée sur la fenêtre.
    pub(crate) fn modale_tout_lu(&self, cx: &mut Context<Self>) -> Option<AnyElement> {
        let (cible, ids) = self.confirmer_tout_lu.as_ref()?;
        let n = ids.len();
        let texte = match (anglais(), n) {
            (false, 1) => format!("Le salon non lu de {cible} sera marqué comme lu."),
            (false, _) => format!("Les {n} salons non lus de {cible} seront marqués comme lus."),
            (true, 1) => format!("The unread channel in {cible} will be marked as read."),
            (true, _) => format!("The {n} unread channels in {cible} will be marked as read."),
        };
        Some(
            div()
                .id("modale-tout-lu")
                .absolute()
                .inset_0()
                .occlude()
                .flex()
                .items_center()
                .justify_center()
                .bg(hsla(0., 0., 0., 0.35))
                .on_click(cx.listener(|this, _, _, cx| {
                    this.confirmer_tout_lu = None;
                    cx.notify();
                }))
                .child(
                    groupe()
                        .id("modale-tout-lu-carte")
                        .when(!th().sombre && !th().ega, |d| d.bg(blanc(0.97)))
                        .w(px(380.))
                        .px(px(20.))
                        .py(px(18.))
                        .gap(px(10.))
                        .on_click(|_, _, cx| cx.stop_propagation())
                        .child(
                            div()
                                .flex()
                                .flex_row()
                                .items_center()
                                .gap(px(10.))
                                .child(icone("mail-open", 20., th().accent))
                                .child(
                                    div()
                                        .text_size(px(16.))
                                        .font_weight(FontWeight::BOLD)
                                        .text_color(th().texte_fort)
                                        .child(tr("Tout marquer comme lu ?", "Mark all as read?")),
                                ),
                        )
                        .child(div().text_size(px(13.5)).text_color(th().texte).child(texte))
                        .child(
                            div()
                                .mt(px(6.))
                                .flex()
                                .flex_row()
                                .justify_end()
                                .gap(px(8.))
                                .child(bouton_texte("tout-lu-annuler", t("Annuler"), 0x8e8e93).on_click(cx.listener(
                                    |this, _, _, cx| {
                                        this.confirmer_tout_lu = None;
                                        cx.notify();
                                    },
                                )))
                                .child(bouton_texte("tout-lu-ok", t("Tout marquer comme lu"), ACCENT).on_click(cx.listener(
                                    |this, _, _, cx| {
                                        if let Some((_, ids)) = this.confirmer_tout_lu.take() {
                                            this.tout_marquer_lu(&ids);
                                        }
                                        cx.notify();
                                    },
                                ))),
                        ),
                )
                .into_any_element(),
        )
    }

    /// Marque lus, tout de suite ici, les espaces [ids], et envoie un `read`
    /// ordinaire pour chacun (les ponts les étalent, §5.13).
    fn tout_marquer_lu(&mut self, ids: &[String]) {
        for id in ids {
            self.attention.remove(id);
            let Some(c) = self.convs.iter_mut().find(|c| &c.id == id) else { continue };
            if !c.unread && c.mentions == 0 {
                continue;
            }
            c.unread = false;
            c.mentions = 0;
            let mut cmd = json!({"cmd": "read", "conv": id});
            if let Some(last) = self.messages.get(id).and_then(|l| l.iter().rev().find(|m| !m.from_me)) {
                cmd["msg"] = json!(last.id);
            }
            self.commande(cmd);
        }
    }

    /// Le nom d'un parent : son `parent_name`, sinon le nom de la
    /// conversation parente, sinon un mot selon la sorte.
    fn nom_parent(&self, cle: &str, salons: &[&Conv]) -> String {
        salons
            .iter()
            .map(|c| c.parent_name.as_str())
            .find(|n| !n.is_empty())
            .map(str::to_string)
            .or_else(|| self.conv(cle).map(|c| c.name.clone()).filter(|n| !n.is_empty()))
            .unwrap_or_else(|| {
                if salons.iter().any(|c| c.kind == "server") { tr("Serveur", "Server") } else { tr("Communauté", "Community") }
                    .to_string()
            })
    }

    /// Les espaces d'une section : une ligne par parent (serveur,
    /// communauté…), qui s'ouvre sur ses salons, et une par espace sans
    /// parent ; les plus actifs d'abord. Rien de propre à un service.
    fn espaces_service(&self, mut col: gpui::Stateful<Div>, miens: &[&Conv], cx: &mut Context<Self>) -> gpui::Stateful<Div> {
        enum Ligne<'a> {
            Parent(String, Vec<&'a Conv>),
            Seul(&'a Conv),
        }
        let mut lignes: Vec<(i64, Ligne)> = Vec::new();
        for (cle, salons) in parents(miens) {
            let temps = salons.iter().map(|c| c.time).max().unwrap_or(0);
            lignes.push((temps, Ligne::Parent(cle, salons)));
        }
        let cles: HashSet<String> = lignes.iter().filter_map(|(_, l)| if let Ligne::Parent(k, _) = l { Some(k.clone()) } else { None }).collect();
        for c in miens.iter().filter(|c| c.parent.is_empty() && !cles.contains(&c.id)) {
            lignes.push((c.time, Ligne::Seul(c)));
        }
        lignes.sort_by(|a, b| b.0.cmp(&a.0));
        for (_, l) in lignes {
            col = match l {
                Ligne::Parent(cle, salons) => col.child(self.ligne_parent(&cle, &salons, cx)),
                Ligne::Seul(c) => col.child(self.ligne_espace(c, 18., cx)),
            };
        }
        col
    }

    /// Un parent (serveur Discord, communauté…) : icône, nom, salons, non-lus.
    fn ligne_parent(&self, cle: &str, salons: &[&Conv], cx: &mut Context<Self>) -> impl IntoElement {
        let nom = self.nom_parent(cle, salons);
        let non_lus = salons.iter().filter(|c| c.unread).count();
        let mentions: u32 = salons.iter().map(|c| self.mentions_espace(c)).sum();
        let temps = salons.iter().map(|c| c.time).max().unwrap_or(0);
        // Les salons, sans la conversation parente (une communauté) elle-même.
        let n = salons.iter().filter(|c| c.id != cle).count().max(1);
        let enfants: Vec<&&Conv> = salons.iter().filter(|c| c.id != cle).collect();
        let (un, plusieurs) = if enfants.iter().any(|c| matches!(c.kind.as_str(), "server" | "subreddit")) {
            (tr("salon", "channel"), tr("salons", "channels"))
        } else if !enfants.is_empty() && enfants.iter().all(|c| matches!(c.kind.as_str(), "channel" | "broadcast" | "newsletter")) {
            (tr("canal", "channel"), tr("canaux", "channels"))
        } else {
            (tr("groupe", "group"), tr("groupes", "groups"))
        };
        let detail = format!("{n} {}", if n == 1 { un } else { plusieurs });
        let ouvrir = cle.to_string();
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
            .child(avatar(&nom, "", true, self.avatars.get(cle), 40.))
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
                            .child(texte_emoji(nom, th().texte_fort, Some(if non_lus > 0 { FontWeight::BOLD } else { FontWeight::SEMIBOLD }))),
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
                    .when(temps > 0, |d| d.child(div().text_size(px(12.)).text_color(th().texte_pale).child(heure(temps, true))))
                    .when(mentions > 0, |d| d.child(compteur(format!("@{mentions}"), ton(c(DANGER), th().danger))))
                    .when(non_lus > 0, |d| d.child(compteur(non_lus.to_string(), th().accent)))
                    .child(icone("chevron-right", 16., th().texte_pale)),
            )
    }

    /// Les mentions non lues d'un espace : celles du service, ou une reçue ici.
    fn mentions_espace(&self, c: &Conv) -> u32 {
        (if c.unread { c.mentions } else { 0 }).max(self.attention.contains(&c.id) as u32)
    }

    /// Un parent ouvert : son en-tête (retour, icône, nom, Tout marquer comme
    /// lu), la conversation parente s'il y en a une, puis ses salons rangés
    /// par catégorie.
    fn salons_parent(&self, mut col: gpui::Stateful<Div>, cle: &str, mut salons: Vec<&Conv>, cx: &mut Context<Self>) -> gpui::Stateful<Div> {
        salons.sort_by_key(|c| (c.id != cle, rang_salon(c)));
        let enfants: Vec<&Conv> = salons.iter().copied().filter(|c| c.id != cle).collect();
        let nom = self.nom_parent(cle, &enfants);
        let a_marquer = a_marquer_lu(salons.iter().copied(), |_| true);
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
                .child(avatar(&nom, "", true, self.avatars.get(cle), 26.))
                .child(
                    div()
                        .flex_1()
                        .truncate()
                        .text_size(px(16. + self.plus_liste()))
                        .font_weight(FontWeight::BOLD)
                        .text_color(th().texte_fort)
                        .child(texte_emoji(nom.clone(), th().texte_fort, Some(FontWeight::BOLD))),
                )
                .children(self.bouton_tout_lu("lu-parent".into(), nom, a_marquer, cx)),
        );
        let mut section: Option<String> = None;
        for conv in salons {
            if conv.id != cle && section.as_deref() != Some(conv.section.as_str()) {
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
            // Les salons (avec catégories) en lignes compactes « # nom » ; les
            // autres espaces avec leur photo et leur aperçu.
            col = if conv.kind == "server" { col.child(self.ligne_salon(conv, cx)) } else { col.child(self.ligne_espace(conv, 12., cx)) };
        }
        col
    }

    /// Un salon : « # nom », non lus et mentions.
    fn ligne_salon(&self, conv: &Conv, cx: &mut Context<Self>) -> impl IntoElement {
        let choisie = self.choisie.as_deref() == Some(conv.id.as_str());
        let id = conv.id.clone();
        let mentions = self.mentions_espace(conv);
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
                    .child(texte_emoji(conv.name.clone(), couleur, Some(if conv.unread { FontWeight::BOLD } else { FontWeight::NORMAL }))),
            )
            .when(conv.read_only, |d| d.child(icone("lock", 12., th().texte_pale)))
            .when(mentions > 0, |d| d.child(compteur(format!("@{mentions}"), ton(c(DANGER), th().danger))))
            .when(conv.unread && mentions == 0, |d| {
                d.child(div().flex_none().size(px(8.)).rounded(rayon(999.)).bg(degrade_accent()))
            })
    }

    /// Une ligne d'espace : photo, icône de la sorte, nom, aperçu, heure.
    fn ligne_espace(&self, conv: &Conv, retrait: f32, cx: &mut Context<Self>) -> impl IntoElement {
        let choisie = self.choisie.as_deref() == Some(conv.id.as_str());
        let id = conv.id.clone();
        let nom = self.nom_affiche(&conv);
        let mentions = self.mentions_espace(conv);
        let apercu = if conv.preview.trim().is_empty() { String::new() } else { une_ligne(&crate::ponts_langue::affiche(&conv.preview, false)) };
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
                                    .child(texte_emoji(nom, th().texte_fort, Some(if conv.unread { FontWeight::BOLD } else { FontWeight::SEMIBOLD }))),
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
                                    .child(texte_emoji(apercu, if conv.unread { th().texte_fort } else { th().apercu }, None)),
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
    fn service_de_tous_les_ids() {
        for (id, svc) in [
            ("33612345678", Service::GoogleMessages),
            ("m:123", Service::Messenger),
            ("i:123", Service::Instagram),
            ("w:1@g.us", Service::WhatsApp),
            ("s:abc", Service::Signal),
            ("x:1-2", Service::X),
            ("t:c7003", Service::Telegram),
            ("b:did", Service::Bluesky),
            ("d:123", Service::Discord),
            ("d:g1:2", Service::Discord),
            ("d:g1", Service::Discord),
        ] {
            assert!(service_de(id) == svc, "{id}");
        }
        // Les ids d'un pont Discord arrivent préfixés, salons et parents compris.
        let mut l = serde_json::json!({"ev": "conversations", "items": [{"id": "g1:2", "parent": "g1"}, {"id": "123"}]});
        prefixer_meta(&mut l, Service::Discord);
        assert_eq!(l["items"][0]["id"], "d:g1:2");
        assert_eq!(l["items"][1]["id"], "d:123");
        let mut a = serde_json::json!({"ev": "avatar", "conv": "g1", "path": "/x.png"});
        prefixer_meta(&mut a, Service::Discord);
        assert!(service_de(a["conv"].as_str().unwrap()) == Service::Discord);
    }

    #[test]
    fn tout_marquer_lu_seulement_les_non_lus() {
        let l = [
            conv(serde_json::json!({"id": "d:g5:1", "name": "a", "large": true, "unread": true, "parent": "g5"})),
            conv(serde_json::json!({"id": "d:g5:2", "name": "b", "large": true, "parent": "g5"})),
            conv(serde_json::json!({"id": "d:g5:3", "name": "c", "large": true, "mentions": 1, "parent": "g5"})),
            conv(serde_json::json!({"id": "d:g6:1", "name": "d", "large": true, "unread": true, "parent": "g6"})),
            conv(serde_json::json!({"id": "d:42", "name": "e", "unread": true})),
        ];
        // Tout le service : les espaces non lus ou mentionnés, pas les messages privés.
        assert_eq!(a_marquer_lu(l.iter(), |_| true), vec!["d:g5:1", "d:g5:3", "d:g6:1"]);
        // Un parent ouvert : seulement ses salons.
        assert_eq!(a_marquer_lu(l.iter(), |c| cle_parent(c) == "d:g5"), vec!["d:g5:1", "d:g5:3"]);
        assert!(a_marquer_lu(l[1..2].iter(), |_| true).is_empty());
    }

    #[test]
    fn parents_generiques() {
        // Une communauté (conversation parente dans la liste) et un service
        // quelconque : même règle que les serveurs Discord.
        let l = [
            conv(serde_json::json!({"id": "w:4101@g.us", "name": "Quartier", "large": true, "kind": "community"})),
            conv(serde_json::json!({"id": "w:4102@g.us", "name": "Annonces", "large": true, "parent": "4101@g.us"})),
            conv(serde_json::json!({"id": "t:c9001", "name": "Nouvelles", "large": true, "parent": "f1", "parent_name": "Dossier"})),
            conv(serde_json::json!({"id": "t:c9002", "name": "Sport", "large": true, "parent": "f1"})),
            conv(serde_json::json!({"id": "t:c7003", "name": "Seul", "large": true})),
        ];
        let refs: Vec<&Conv> = l.iter().collect();
        let p = parents(&refs);
        assert_eq!(p.len(), 2);
        assert_eq!(p[0].0, "w:4101@g.us");
        assert_eq!(p[0].1.iter().map(|c| c.id.as_str()).collect::<Vec<_>>(), vec!["w:4101@g.us", "w:4102@g.us"]);
        assert_eq!(p[1].0, "t:f1");
        assert_eq!(p[1].1.len(), 2);
    }

    #[test]
    fn boite_et_groupes() {
        let salon = conv(serde_json::json!({"id": "d:g5:11", "name": "général", "large": true, "kind": "server", "parent": "g5"}));
        let groupe = conv(serde_json::json!({"id": "w:1@g.us", "name": "Chalet", "group": true}));
        // Un groupe de personnes reste dans la boîte ; un salon n'y va jamais,
        // même avec des mentions non lues (« inbox = chat »).
        assert!(!hors_boite(&groupe));
        assert!(hors_boite(&salon));
        let mentionne = conv(serde_json::json!({"id": "d:g5:12", "name": "x", "large": true, "unread": true, "mentions": 2}));
        assert!(hors_boite(&mentionne));
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
