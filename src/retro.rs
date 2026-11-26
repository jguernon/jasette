// Live et Sirène : les deux thèmes clairs « années 2000 » du bureau. Android
// a les mêmes (ui/Retro.kt et ui/Theme.kt) : mêmes couleurs, mêmes choix.
//
// - Live (maquette Main.dc.html) : une barre de titre bleue brillante, coupée
//   net au milieu ; des volets bleu pâle accolés ; les photos dans un cadre
//   vert brillant ; des boutons en pilule blanc → bleu pâle ; l'état en bonhomme.
// - Sirène (maquette Sirene.dc.html) : Windows 2000. Gris #d4d0c8, reliefs
//   (clair en haut à gauche, sombre en bas à droite) et creux (l'inverse) faits
//   d'ombres intérieures nettes, barre de titre verte, coins carrés, sélection
//   bleu marine, barre de menu et barre d'état, aucune animation fluide ;
//   l'état en goutte.
// - Dans les deux, la liste a des groupes « En ligne » et « Hors ligne »
//   (selon l'état du service de la conversation) qui se replient, avec une
//   pastille d'état et une rangée compacte. Le fil est une zone d'historique
//   encadrée, sans bulles : un en-tête par suite de messages (« Camille dit
//   (19:24) : » en Live, le nom en rouge ou en bleu puis l'heure en Sirène),
//   le texte en retrait. Les grandes photos (l'autre en haut, moi en bas) à
//   droite, la barre d'outils, puis la zone d'écriture encadrée. Le Mood ne
//   fait que teinter discrètement le fond de l'historique.
// - Aucune marque : ni logo ni nom des messageries d'origine.
use super::*;

impl Palette {
    /// Live : texte #1d2f4a, noms #12335e, liserés bleus, blanc pour les champs.
    pub(crate) fn live() -> Self {
        Palette {
            sombre: false,
            ega: false,
            live: true,
            sirene: false,
            fond: c(0xeaf3fd),
            rail: c(0xd6e8fb),
            liste: c(0xffffff),
            fil: c(0xffffff),
            rayon_rail: 0.,
            rayon_liste: 0.,
            rayon_fil: 0.,
            marge: 0.,
            ecart: 0.,
            marge_fil: 0.,
            givre_liste: c(0xffffff),
            givre_fil: c(0xffffff),
            surface: c(0xffffff),
            surface_bord: c(0x94bbe8),
            champ: c(0xf6faff),
            choisi: c(0xd4e6fa),
            survol: c(0xe3f0fd),
            trait_fin: c(0xc3dbf5),
            menu: c(0xf7fbff),
            menu_champ: c(0xe3f0fd),
            carte: c(0xffffff),
            texte_fort: c(0x12335e),
            texte: c(0x1d2f4a),
            texte_pale: c(0x4d6584),
            apercu: c(0x4d6c92),
            icone: c(0x1f5fb8),
            accent: c(0x0b5bc4),
            expediteur: c(0x0b5bc4),
            jour_fond: c(0xe3f0fd),
            jour_texte: c(0x3f5d85),
            heure_recue: c(0x5d7088),
            heure_envoyee: blanc(0.85),
            bord_avatar: hsla(0., 0., 0., 0.),
            danger: c(0xc0281a),
            succes: c(0x2f8a22),
            averti: c(0xb86400),
        }
    }

    /// Sirène : noir sur gris #d4d0c8 et sur blanc, accent vert.
    pub(crate) fn sirene() -> Self {
        Palette {
            sombre: false,
            ega: false,
            live: false,
            sirene: true,
            fond: c(GRIS_2000),
            rail: c(GRIS_2000),
            liste: c(0xffffff),
            fil: c(0xffffff),
            rayon_rail: 0.,
            rayon_liste: 0.,
            rayon_fil: 0.,
            marge: 6.,
            ecart: 6.,
            marge_fil: 0.,
            givre_liste: c(0xffffff),
            givre_fil: c(0xffffff),
            surface: c(0xffffff),
            surface_bord: c(0x808080),
            champ: c(0xffffff),
            choisi: c(0xc9d3ea),
            survol: c(0xe4f3e0),
            trait_fin: c(0x808080),
            menu: c(GRIS_2000),
            menu_champ: c(0xffffff),
            carte: c(0xffffff),
            texte_fort: c(0x000000),
            texte: c(0x202020),
            texte_pale: c(0x505050),
            apercu: c(0x404040),
            icone: c(0x202020),
            accent: c(0x1f6f2a),
            expediteur: c(0xc01818),
            jour_fond: c(0xffffe1),
            jour_texte: c(0x000000),
            heure_recue: c(0x6a6a6a),
            heure_envoyee: blanc(0.85),
            bord_avatar: hsla(0., 0., 0., 0.),
            danger: c(0xc01818),
            succes: c(0x1f8a2f),
            averti: c(0xa05a00),
        }
    }
}

/// Le gris des fenêtres Windows 2000 (Sirène).
pub(crate) const GRIS_2000: u32 = 0xd4d0c8;
/// La sélection de Sirène : bleu marine, texte blanc.
const MARINE: u32 = 0x0a246a;

/// Live ou Sirène.
pub(crate) fn retro() -> bool {
    th().live || th().sirene
}

/// Le relief de Windows 2000 en ombres intérieures nettes de 2 px : en relief,
/// clair en haut à gauche et sombre en bas à droite ; en creux, l'inverse.
pub(crate) fn relief(enfonce: bool) -> Vec<BoxShadow> {
    if enfonce { relief_de(0x808080, 0xffffff, 2.) } else { relief_de(0xffffff, 0x404040, 2.) }
}

/// Un relief aux couleurs données : `hg` en haut à gauche, `bd` en bas à droite.
pub(crate) fn relief_de(hg: u32, bd: u32, e: f32) -> Vec<BoxShadow> {
    let bande = |couleur: u32, d: f32| BoxShadow {
        color: c(couleur),
        offset: point(px(d), px(d)),
        blur_radius: px(0.),
        spread_radius: px(0.),
        inset: true,
    };
    vec![bande(hg, e), bande(bd, -e)]
}

/// Le reflet brillant de Live : la moitié haute d'un parent `relative`, d'un
/// dégradé à lui. Avec le dégradé du parent dessous, la coupure au milieu est
/// nette. GPUI ne découpe pas les enfants : le reflet prend les coins `r`.
pub(crate) fn reflet(haut: u32, bas: u32, r: f32) -> Div {
    div()
        .absolute()
        .top_0()
        .left_0()
        .right_0()
        .h(gpui::relative(0.5))
        .rounded_t(px(r))
        .bg(linear_gradient(180., linear_color_stop(c(haut), 0.), linear_color_stop(c(bas), 1.)))
}

/// Le fond de la barre de titre : la moitié basse (Live) ou le vert (Sirène).
pub(crate) fn fond_titre() -> gpui::Background {
    if th().live {
        linear_gradient(180., linear_color_stop(c(0x2a6fcf), 0.5), linear_color_stop(c(0x4d97ea), 1.))
    } else {
        linear_gradient(90., linear_color_stop(c(0x0d5c1f), 0.), linear_color_stop(c(0x3aa64a), 1.))
    }
}

/// La moitié haute de la barre de titre de Live (rien en Sirène).
pub(crate) fn reflet_titre() -> Option<Div> {
    th().live.then(|| reflet(0x7db6f2, 0x3a86e0, 0.))
}

// ---- L'état : bonhomme (Live) ou goutte (Sirène) ----

#[derive(Clone, Copy, PartialEq, Eq, Hash)]
pub(crate) enum Niveau {
    Vert,
    Orange,
    Rouge,
    Gris,
}

impl Niveau {
    /// (tête, corps, trait) du bonhomme ; (remplissage, trait) de la goutte.
    fn couleurs(self) -> (&'static str, &'static str, &'static str, &'static str, &'static str) {
        match self {
            Niveau::Vert => ("#bff0a8", "#7fd45f", "#2f8a22", "#5fd06a", "#0b4a15"),
            Niveau::Orange => ("#ffe2a8", "#f7b844", "#c07a12", "#ffd34d", "#8a6200"),
            Niveau::Rouge => ("#ffc2b8", "#ef6a52", "#b32a17", "#ff7a6a", "#8a1a0a"),
            Niveau::Gris => ("#e3e8ee", "#c4ccd6", "#8a97a8", "#d84040", "#6a0a0a"),
        }
    }
}

/// Le niveau d'un état de service : connecté (vert), coupé (rouge), arrêté
/// (gris), en route (orange).
pub(crate) fn niveau(e: Etat) -> Niveau {
    match e {
        Etat::Pret => Niveau::Vert,
        Etat::TelephoneHorsLigne => Niveau::Rouge,
        Etat::NonAssocie => Niveau::Gris,
        _ => Niveau::Orange,
    }
}

/// La pastille d'état, en SVG couleur (GPUI dessine un SVG à sa taille propre :
/// une copie par taille).
pub(crate) fn pastille(n: Niveau, taille: f32) -> impl IntoElement {
    static CACHE: std::sync::OnceLock<Mutex<HashMap<(bool, Niveau, u32), Arc<gpui::Image>>>> = std::sync::OnceLock::new();
    let live = th().live;
    let cote = taille.ceil().max(1.) as u32;
    let image = CACHE
        .get_or_init(Default::default)
        .lock()
        .unwrap_or_else(|e| e.into_inner())
        .entry((live, n, cote))
        .or_insert_with(|| {
            let (tete, corps, trait_, goutte, bord) = n.couleurs();
            let dessin = if live {
                format!(
                    r#"<circle cx="12" cy="8" r="4.5" fill="{tete}" stroke="{trait_}" stroke-width="1.3"/><path d="M3.5 21c0-4.6 3.8-7.3 8.5-7.3s8.5 2.7 8.5 7.3z" fill="{corps}" stroke="{trait_}" stroke-width="1.3"/>"#
                )
            } else {
                format!(r#"<path d="M12 3c3 3 5 6 5 9a5 5 0 0 1-10 0c0-3 2-6 5-9z" fill="{goutte}" stroke="{bord}" stroke-width="1.4"/>"#)
            };
            let svg = format!(r#"<svg xmlns="http://www.w3.org/2000/svg" width="{cote}" height="{cote}" viewBox="0 0 24 24">{dessin}</svg>"#);
            Arc::new(gpui::Image::from_bytes(gpui::ImageFormat::Svg, svg.into_bytes()))
        })
        .clone();
    img(image).size(px(taille)).flex_none()
}

/// Le cadre d'un avatar de `taille` : vert brillant aux coins arrondis (Live),
/// carré en creux (Sirène). `contenu` dessine l'avatar à la taille intérieure.
pub(crate) fn cadre_avatar(taille: f32, contenu: impl FnOnce(f32) -> AnyElement) -> AnyElement {
    if th().live {
        let marge = (taille * 0.055).max(2.).round();
        return div()
            .flex_none()
            .size(px(taille))
            .rounded(px(taille * 0.16))
            .p(px(marge))
            .bg(linear_gradient(180., linear_color_stop(c(0xb7f08f), 0.), linear_color_stop(c(0x4bb52a), 1.)))
            .when(taille >= 30., |d| {
                d.shadow(vec![BoxShadow {
                    color: ca(0x1e5a14, 0.35),
                    offset: point(px(0.), px((taille / 28.).clamp(1., 3.))),
                    blur_radius: px((taille / 14.).clamp(2., 8.)),
                    spread_radius: px(0.),
                    inset: false,
                }])
            })
            .child(contenu(taille - 2. * marge))
            .into_any_element();
    }
    let e = if taille >= 40. { 2. } else { 1. };
    div()
        .flex_none()
        .relative()
        .size(px(taille))
        .p(px(e))
        .bg(c(0xffffff))
        .child(contenu(taille - 2. * e))
        // Le creux par-dessus la photo (les ombres intérieures passent sous les enfants).
        .child(div().absolute().top_0().left_0().right_0().bottom_0().shadow(relief_de(0x808080, 0xffffff, e)))
        .into_any_element()
}

/// L'aperçu de Live dans Réglages > Apparence : le ciel, une barre brillante, un cadre vert.
pub(crate) fn apercu_live() -> Div {
    div()
        .relative()
        .bg(linear_gradient(160., linear_color_stop(c(0x5f9fe8), 0.), linear_color_stop(c(0xd7e9fb), 1.)))
        .border_color(c(0x3f78c4))
        .child(div().absolute().top(px(7.)).left(px(7.)).right(px(7.)).h(px(9.)).rounded(px(3.)).bg(fond_titre_live()))
        .child(div().absolute().top(px(7.)).left(px(7.)).right(px(7.)).h(px(4.5)).rounded_t(px(3.)).bg(c(0x7db6f2)))
        .child(
            div()
                .absolute()
                .left(px(7.))
                .bottom(px(7.))
                .size(px(22.))
                .rounded(px(4.))
                .p(px(2.))
                .bg(linear_gradient(180., linear_color_stop(c(0xb7f08f), 0.), linear_color_stop(c(0x4bb52a), 1.)))
                .child(div().size_full().rounded(px(3.)).bg(linear_gradient(135., linear_color_stop(c(0xffd6a5), 0.), linear_color_stop(c(0xf08a5d), 1.)))),
        )
        .child(div().absolute().right(px(7.)).bottom(px(16.)).w(px(14.)).h(px(3.)).bg(c(0xffffff)))
        .child(div().absolute().right(px(7.)).bottom(px(9.)).w(px(10.)).h(px(3.)).bg(c(0x0b3c8a)))
}

fn fond_titre_live() -> gpui::Background {
    linear_gradient(180., linear_color_stop(c(0x2a6fcf), 0.5), linear_color_stop(c(0x4d97ea), 1.))
}

/// L'aperçu de Sirène : une fenêtre grise en relief, barre de titre verte, liste en creux.
pub(crate) fn apercu_sirene() -> Div {
    div()
        .relative()
        .bg(c(GRIS_2000))
        .border_color(c(0x404040))
        .child(
            div().absolute().top(px(5.)).left(px(5.)).right(px(5.)).h(px(9.)).bg(linear_gradient(
                90.,
                linear_color_stop(c(0x0d5c1f), 0.),
                linear_color_stop(c(0x3aa64a), 1.),
            )),
        )
        .child(
            div()
                .absolute()
                .top(px(18.))
                .left(px(5.))
                .right(px(5.))
                .bottom(px(5.))
                .bg(c(0xffffff))
                .shadow(relief_de(0x808080, 0xffffff, 1.))
                .p(px(4.))
                .flex()
                .flex_col()
                .gap(px(3.))
                .child(div().h(px(3.)).w(px(18.)).bg(c(0xc01818)))
                .child(div().h(px(3.)).w(px(28.)).bg(c(0x404040)))
                .child(div().h(px(3.)).w(px(14.)).bg(c(0x1838c0))),
        )
}

/// Un bouton d'outil : pilule blanc → bleu pâle (Live), gris en relief (Sirène).
pub(crate) fn bouton_outil(id: impl Into<gpui::ElementId>) -> gpui::Stateful<Div> {
    let d = div().id(id).flex().flex_row().items_center().justify_center().gap(px(5.)).flex_none().cursor_pointer();
    if th().live {
        d.h(px(28.))
            .px(px(11.))
            .rounded(px(14.))
            .border_1()
            .border_color(c(0x7aa6dc))
            .bg(linear_gradient(180., linear_color_stop(c(0xffffff), 0.), linear_color_stop(c(0xd6e7fb), 1.)))
            .text_size(px(13.))
            .text_color(c(0x12335e))
            .hover(|s| s.border_color(c(0x2f6fc4)))
    } else {
        d.h(px(26.))
            .px(px(9.))
            .bg(c(GRIS_2000))
            .shadow(relief(false))
            .text_size(px(13.))
            .text_color(c(0x000000))
            .active(|s| s.shadow(relief(true)))
    }
}

/// Le bouton Envoyer : bleu brillant (Live), vert en relief (Sirène).
pub(crate) fn bouton_envoi(id: impl Into<gpui::ElementId>, texte: &'static str) -> gpui::Stateful<Div> {
    let d = div().id(id).relative().flex_none().flex().items_center().justify_center().cursor_pointer();
    let etiquette = div().relative().font_weight(FontWeight::BOLD).text_color(c(0xffffff)).child(texte);
    if th().live {
        d.w(px(96.))
            .rounded(px(8.))
            .border_1()
            .border_color(c(0x2f6fc4))
            .bg(linear_gradient(180., linear_color_stop(c(0x2f7ee0), 0.5), linear_color_stop(c(0x5aa6f2), 1.)))
            .hover(|s| s.opacity(0.92))
            .child(reflet(0x9fd0ff, 0x4a96ec, 7.))
            .child(etiquette)
    } else {
        d.w(px(92.))
            .h(px(30.))
            .bg(c(0x2e9a3f))
            .shadow(relief_de(0x8fe39a, 0x0b4a15, 2.))
            .active(|s| s.shadow(relief_de(0x0b4a15, 0x8fe39a, 2.)))
            .child(etiquette)
    }
}

impl Jasette {
    /// L'état d'un service (Google a le sien à part).
    pub(crate) fn etat_service(&self, s: Service) -> Etat {
        if s == Service::GoogleMessages { self.etat } else { self.meta(s).etat }
    }

    /// Les services connectés, et ceux qui sont là (connus ou en route).
    fn services_connectes(&self) -> usize {
        let google = (self.etat == Etat::Pret) as usize;
        google + SERVICES_META.into_iter().filter(|s| self.meta(*s).etat == Etat::Pret).count()
    }

    /// Mon nom pour le fil et la liste : celui d'un compte (pas une adresse ni
    /// un numéro), sinon celui de la session.
    pub(crate) fn mon_nom(&self) -> Option<String> {
        let bon = |n: &str| {
            let n = n.trim();
            (!n.is_empty() && !n.contains('@') && n.chars().filter(|c| c.is_alphabetic()).count() >= 2).then(|| n.to_string())
        };
        SERVICES_META
            .into_iter()
            .filter_map(|s| self.meta(s).nom.as_deref().and_then(bon))
            .next()
    }

    fn initiale_moi(&self) -> String {
        self.mon_nom()
            .and_then(|n| n.chars().next())
            .or_else(|| self.reglages.email.as_deref().and_then(|e| e.chars().next()))
            .or_else(|| std::env::var("USER").ok().and_then(|u| u.chars().next()))
            .unwrap_or('J')
            .to_uppercase()
            .to_string()
    }

    /// Le bandeau « moi » en haut de la liste : ma photo, mon nom et mon état.
    pub(crate) fn moi_retro(&self) -> (Div, f32) {
        let n = self.services_connectes();
        let etat = if n > 0 { t("En ligne") } else { t("Hors ligne") };
        let services = format!("{n} {}", if n > 1 { t("services connectés") } else { t("service connecté") });
        let nom = self.mon_nom().unwrap_or_else(|| t("Moi").to_string());
        if th().live {
            let d = div()
                .flex()
                .flex_row()
                .items_center()
                .gap(px(12.))
                .px(px(14.))
                .py(px(12.))
                .bg(linear_gradient(180., linear_color_stop(c(0xffffff), 0.), linear_color_stop(c(0xe3f0fd), 1.)))
                .border_b_1()
                .border_color(c(0xc3dbf5))
                .child(self.mon_avatar(&self.initiale_moi(), 52.))
                .child(
                    div()
                        .flex()
                        .flex_col()
                        .gap(px(2.))
                        .min_w(px(0.))
                        .child(
                            div()
                                .flex()
                                .flex_row()
                                .gap(px(5.))
                                .text_size(px(14.))
                                .child(div().truncate().font_weight(FontWeight::BOLD).text_color(c(0x12335e)).child(texte_emoji(nom, c(0x12335e), Some(FontWeight::BOLD))))
                                .child(div().flex_none().text_color(if n > 0 { c(0x3a7d1e) } else { c(0x6d7f96) }).child(format!("({etat})"))),
                        )
                        .child(div().text_size(px(12.)).italic().text_color(c(0x4d6c92)).child(services)),
                );
            return (d, 77.);
        }
        let d = div()
            .flex()
            .flex_row()
            .items_center()
            .gap(px(8.))
            .px(px(8.))
            .py(px(6.))
            .mb(px(6.))
            .bg(c(0xffffff))
            .shadow(relief(true))
            .child(pastille(if n > 0 { Niveau::Vert } else { Niveau::Gris }, 18.))
            .child(div().truncate().font_weight(FontWeight::BOLD).text_color(c(0x000000)).child(texte_emoji(nom, c(0x000000), Some(FontWeight::BOLD))))
            .child(div().flex_none().text_color(c(0x1f6f2a)).child(format!("· {etat}")))
            .child(div().flex_1())
            .child(div().flex_none().text_size(px(12.)).text_color(c(0x404040)).child(services));
        (d, 40.)
    }

    /// La liste façon années 2000 : « En ligne », puis « Hors ligne » (selon
    /// l'état du service), chaque groupe repliable, une rangée compacte par
    /// conversation avec sa pastille d'état.
    pub(crate) fn lignes_retro(
        &self,
        mut col: gpui::Stateful<Div>,
        visibles: Vec<&Conv>,
        badge: bool,
        cx: &mut Context<Self>,
    ) -> gpui::Stateful<Div> {
        let live = th().live;
        let (en_ligne, hors_ligne): (Vec<&Conv>, Vec<&Conv>) =
            visibles.into_iter().partition(|c| !self.est_fantome(&c.id) && self.etat_service(service_de(&c.id)) == Etat::Pret);
        for (g, liste) in [en_ligne, hors_ligne].into_iter().enumerate() {
            if liste.is_empty() {
                continue;
            }
            let replie = self.retro_replie[g];
            let titre = if g == 0 { t("En ligne") } else { t("Hors ligne") };
            let entete = div()
                .id(("groupe-retro", g))
                .flex()
                .flex_row()
                .items_center()
                .gap(px(6.))
                .cursor_pointer()
                .font_weight(FontWeight::BOLD)
                .text_size(px(13. + self.plus_liste()))
                .on_click(cx.listener(move |this, _, _, cx| {
                    this.retro_replie[g] = !this.retro_replie[g];
                    cx.notify();
                }));
            col = col.child(if live {
                entete
                    .px(px(6.))
                    .pt(px(if g == 0 { 4. } else { 10. }))
                    .pb(px(4.))
                    .text_color(c(0x12335e))
                    .child(format!("{} {titre} ({})", if replie { "▸" } else { "▾" }, liste.len()))
            } else {
                entete
                    .px(px(6.))
                    .py(px(3.))
                    .when(g > 0, |d| d.mt(px(6.)))
                    .bg(c(if g == 0 { 0xe4f3e0 } else { 0xf7e3e3 }))
                    .text_color(c(if g == 0 { 0x0b4a15 } else { 0x7a1010 }))
                    .child(format!("{} {titre} ({})", if replie { "+" } else { "−" }, liste.len()))
            });
            if replie {
                continue;
            }
            for conv in liste {
                col = col.child(self.rangee_retro(conv, badge, cx));
            }
        }
        col
    }

    fn rangee_retro(&self, conv: &Conv, badge: bool, cx: &mut Context<Self>) -> gpui::Stateful<Div> {
        let live = th().live;
        let svc = service_de(&conv.id);
        let choisie = self.choisie.as_deref() == Some(conv.id.as_str());
        let id = conv.id.clone();
        let nom = self.nom_affiche(&conv);
        let apercu = if conv.preview.trim().is_empty() { t("📎 Pièce jointe").to_string() } else { une_ligne(&conv.preview) };
        let plus = self.plus_liste();
        let n = niveau(if self.est_fantome(&conv.id) { Etat::NonAssocie } else { self.etat_service(svc) });
        let rangee = div()
            .id(SharedString::from(format!("c-{}", conv.id)))
            .flex()
            .flex_row()
            .items_center()
            .cursor_pointer()
            .on_click(cx.listener(move |this, _, _, cx| {
                this.choisir(id.clone());
                cx.notify();
            }));
        if live {
            let texte = if n == Niveau::Gris { c(0x7d8fa6) } else { c(0x1d2f4a) };
            return rangee
                .gap(px(10.))
                .px(px(8.))
                .py(px(6.))
                .rounded(px(6.))
                .border_1()
                .when(choisie, |d| {
                    d.border_color(c(0x8fb9ec)).bg(linear_gradient(
                        180.,
                        linear_color_stop(c(0xd9ebff), 0.),
                        linear_color_stop(c(0xbcd9fb), 1.),
                    ))
                })
                .when(!choisie, |d| d.border_color(hsla(0., 0., 0., 0.)).hover(|s| s.bg(c(0xeef5fe))))
                .child(pastille(n, 18.))
                .child(
                    div()
                        .flex_1()
                        .min_w(px(0.))
                        .flex()
                        .flex_col()
                        .child(
                            div()
                                .flex()
                                .flex_row()
                                .items_center()
                                .gap(px(5.))
                                .child(
                                    div()
                                        .truncate()
                                        .text_size(px(14. + plus))
                                        .font_weight(FontWeight::BOLD)
                                        .text_color(texte)
                                        .child(texte_emoji(nom, texte, Some(FontWeight::BOLD))),
                                )
                                .when(self.en_ligne(conv), |d| d.child(presence::carre_en_ligne())),
                        )
                        .when(self.ecrit(&conv.id).is_some(), |d| d.child(ecrit::apercu_ecrit(12. + plus)))
                        .when(self.ecrit(&conv.id).is_none(), |d| {
                            d.child(
                                div()
                                    .truncate()
                                    .text_size(px(12. + plus))
                                    .child(texte_emoji(apercu, if conv.unread { c(0x12335e) } else { c(0x4d6c92) }, None)),
                            )
                        }),
                )
                .child(
                    div()
                        .flex_none()
                        .flex()
                        .flex_col()
                        .items_end()
                        .gap(px(3.))
                        .text_size(px(11. + plus))
                        .when(conv.unread, |d| d.font_weight(FontWeight::BOLD).text_color(c(0x0b5bc4)))
                        .when(!conv.unread, |d| d.text_color(c(0x4d6c92)))
                        .child(heure(conv.time, true))
                        .when(badge, |d| d.child(logo(svc, 14.))),
                );
        }
        // Sirène : une ligne, bleu marine et texte blanc quand elle est choisie.
        let (texte, pale) = if choisie {
            (c(0xffffff), c(0xdde4f5))
        } else if n == Niveau::Gris {
            (c(0x6a6a6a), c(0x808080))
        } else {
            (c(0x000000), c(0x606060))
        };
        rangee
            .gap(px(8.))
            .px(px(8.))
            .py(px(5.))
            .when(choisie, |d| d.bg(c(MARINE)))
            .when(!choisie, |d| d.hover(|s| s.bg(c(0xe4f3e0))))
            .child(pastille(n, 16.))
            .child(
                div()
                    .flex_none()
                    .max_w(px(170.))
                    .truncate()
                    .text_size(px(14. + plus))
                    .when(conv.unread, |d| d.font_weight(FontWeight::BOLD))
                    .text_color(texte)
                    .child(texte_emoji(nom, texte, conv.unread.then_some(FontWeight::BOLD))),
            )
            .when(self.en_ligne(conv), |d| d.child(presence::carre_en_ligne()))
            .when(self.ecrit(&conv.id).is_some(), |d| d.child(div().flex_1().min_w(px(0.)).child(ecrit::apercu_ecrit(12. + plus))))
            .when(self.ecrit(&conv.id).is_none(), |d| {
                d.child(div().flex_1().min_w(px(0.)).truncate().text_size(px(12. + plus)).child(texte_emoji(apercu, pale, None)))
            })
            .when(conv.unread, |d| {
                // L'enveloppe jaune d'un nouveau message.
                d.child(div().flex_none().w(px(14.)).h(px(11.)).bg(c(0xffe26a)).border_1().border_color(c(0x8a6d00)))
            })
            .when(badge, |d| d.child(logo(svc, 14.)))
            .child(div().flex_none().text_size(px(11. + plus)).text_color(pale).child(heure(conv.time, true)))
    }

    /// L'en-tête d'une suite de messages : « Camille dit (19:24) : » (Live),
    /// ou le nom en rouge (l'autre) ou en bleu (moi) puis l'heure (Sirène).
    pub(crate) fn entete_retro(&self, m: &Msg, nom_conv: &str, flou: bool) -> AnyElement {
        let quand = if self.details_heures {
            format!("{} · {}", jour(m.time), heure(m.time, false))
        } else {
            heure(m.time, false)
        };
        let qui: Option<String> = if m.from_me {
            self.mon_nom()
        } else if !m.sender.is_empty() {
            Some(m.sender.clone())
        } else {
            (!nom_conv.is_empty()).then(|| nom_conv.to_string())
        };
        let invisible = hsla(0., 0., 0., 0.);
        if th().live {
            let prenom = qui.as_deref().and_then(|q| q.split_whitespace().next()).map(str::to_string);
            let texte = match (prenom, anglais()) {
                (Some(p), false) => format!("{p} dit ({quand}) :"),
                (Some(p), true) => format!("{p} says ({quand}):"),
                (None, false) => format!("Moi ({quand}) :"),
                (None, true) => format!("Me ({quand}):"),
            };
            return div()
                .mt(px(6.))
                .text_size(px(12.))
                .text_color(if flou { invisible } else { c(0x5d7088) })
                .child(texte)
                .into_any_element();
        }
        let nom = qui.unwrap_or_else(|| t("Moi").to_string());
        div()
            .mt(px(6.))
            .flex()
            .flex_row()
            .gap(px(5.))
            .child(
                div()
                    .font_weight(FontWeight::BOLD)
                    .text_color(if m.from_me { c(0x1838c0) } else { c(0xc01818) })
                    .child(texte_emoji(nom, if m.from_me { c(0x1838c0) } else { c(0xc01818) }, Some(FontWeight::BOLD))),
            )
            .child(div().text_color(c(0x6a6a6a)).child(format!("({quand})")))
            .into_any_element()
    }

    /// Le fil façon années 2000 : en-tête, historique encadré (le Mood en
    /// teinte le fond) et grandes photos à droite, barre d'outils, zone d'écriture.
    pub(crate) fn panneau_fil_retro(&mut self, cx: &mut Context<Self>) -> AnyElement {
        let live = th().live;
        let choisie = self.choisie.as_ref().and_then(|id| self.conv(id)).cloned();
        let service = self.choisie.as_deref().map(service_de).unwrap_or(Service::GoogleMessages);
        let mood = self.mood_fil();

        // L'en-tête : le nom, le service et les boutons de la conversation.
        let mut entete = div().flex().flex_row().items_center().gap(px(10.)).flex_none();
        entete = if live {
            entete
                .px(px(18.))
                .py(px(10.))
                .min_h(px(58.))
                .bg(linear_gradient(180., linear_color_stop(c(0xe8f3ff), 0.), linear_color_stop(c(0xcfe3fa), 1.)))
                .border_b_1()
                .border_color(c(0xa9c8ee))
        } else {
            entete
                .px(px(8.))
                .py(px(6.))
                .border_1()
                .border_color(c(0x808080))
                .bg(linear_gradient(180., linear_color_stop(c(0xeef8ec), 0.), linear_color_stop(c(GRIS_2000), 1.)))
        };
        if let Some(conv) = choisie.clone() {
            let nom = self.nom_affiche(&conv);
            let etat = self.etat_service(service);
            let detail = if conv.group {
                format!("{} · {} participants", service.nom(), conv.members.len() + 1)
            } else if self.ecrit(&conv.id).is_some() {
                // Quelqu'un écrit (src/ecrit.rs).
                if live { format!("<{}> · {}", service.nom(), t("écrit…")) } else { format!("{} · {}", service.nom(), t("écrit…")) }
            } else if let Some(a) = self.activite(&conv) {
                // « Actif il y a 12 min » (src/presence.rs).
                if live { format!("<{}> · {a}", service.nom()) } else { format!("{} · {a}", service.nom()) }
            } else if live {
                format!("<{}> · {}", service.nom(), t(etat.texte()))
            } else {
                format!("{} · {}", service.nom(), t(etat.texte()))
            };
            let icone_outil = |nom: &'static str| icone(nom, 15., if live { c(0x1f5fb8) } else { c(0x000000) });
            let appel = adresse_appel(&conv);
            let video = if service == Service::GoogleMessages { None } else { appel.clone() };
            let conv_medias = self.choisie.clone();
            let conv_mood = conv.id.clone();
            // À deux : toucher le nom ou l'avatar ouvre la fiche de la personne.
            let a_fiche = !conv.group && !conv.large;
            let (id_a, id_b) = (conv.id.clone(), conv.id.clone());
            entete = entete
                .when(!live, |d| {
                    d.child(
                        div()
                            .id("entete-personne-avatar")
                            .relative()
                            .when(a_fiche, |d| {
                                d.cursor_pointer().on_click(cx.listener(move |this, _, _, cx| {
                                    this.ouvrir_fiche(&id_a);
                                    cx.notify();
                                }))
                            })
                            .child(self.avatar_affiche(&conv, 40.))
                            .when(self.en_ligne(&conv), |d| d.child(presence::point_presence(40., c(0xe4e8df)))),
                    )
                })
                .child(
                    div()
                        .id("entete-personne")
                        .when(a_fiche, |d| {
                            d.cursor_pointer().on_click(cx.listener(move |this, _, _, cx| {
                                this.ouvrir_fiche(&id_b);
                                cx.notify();
                            }))
                        })
                        .flex_1()
                        .min_w(px(0.))
                        .flex()
                        .flex_col()
                        .child(
                            div()
                                .truncate()
                                .text_size(px(15.))
                                .font_weight(FontWeight::BOLD)
                                .text_color(if live { c(0x12335e) } else { c(0x000000) })
                                .child(texte_emoji(nom, if live { c(0x12335e) } else { c(0x000000) }, Some(FontWeight::BOLD))),
                        )
                        .child(div().truncate().text_size(px(12.)).text_color(if live { c(0x4d6c92) } else { c(0x404040) }).child(detail)),
                )
                .when_some(video, |d, a| d.child(bouton_outil("video").child(icone_outil("video")).on_click(move |_, _, _| ouvrir_adresse(&a))))
                .when_some(appel, |d, a| d.child(bouton_outil("appel").child(icone_outil("phone")).on_click(move |_, _, _| ouvrir_adresse(&a))))
                .child(
                    bouton_outil("medias-conv")
                        .child(icone_outil("images"))
                        .child(t("Médias"))
                        .on_click(cx.listener(move |this, _, _, cx| {
                            this.ouvrir_galerie(conv_medias.clone());
                            cx.notify();
                        })),
                )
                .child(bouton_outil("mood-conv").child(icone_outil("palette")).child("Mood").on_click(cx.listener(
                    move |this, _, _, cx| {
                        this.ouvrir_feuille_mood(&conv_mood);
                        cx.notify();
                    },
                )))
                .child(
                    bouton_outil("details-heures")
                        .child(icone(
                            "info",
                            15.,
                            if self.details_heures { th().accent } else if live { c(0x1f5fb8) } else { c(0x000000) },
                        ))
                        .when(!live && self.details_heures, |d| d.shadow(relief(true)))
                        .on_click(cx.listener(|this, _, _, cx| {
                            this.details_heures = !this.details_heures;
                            cx.notify();
                        })),
                )
                .child(bouton_outil("options-conv").child(icone_outil("ellipsis")).on_click(cx.listener(|this, _, _, cx| {
                    this.menu_conv = !this.menu_conv;
                    this.confirmer_suppression = false;
                    cx.notify();
                })));
        }

        // L'historique, encadré : bordé de bleu à reflet (Live), en creux (Sirène).
        let fil = self.fil(cx);
        let mut historique = div().relative().flex_1().min_w(px(0.)).h_full().overflow_hidden();
        historique = match (mood, live) {
            (Some(m), _) => historique.bg(m.fond()),
            (None, true) => historique.bg(linear_gradient(180., linear_color_stop(c(0xeef6ff), 0.), linear_color_stop(c(0xffffff), 0.3))),
            (None, false) => historique.bg(c(0xffffff)),
        };
        historique = if live {
            historique.rounded(px(6.)).border_1().border_color(c(0xa9c8ee))
        } else {
            historique.p(px(2.))
        };
        historique = historique.child(div().absolute().top(px(2.)).bottom(px(2.)).left(px(2.)).right(px(2.)).child(fil));
        if !live {
            // Le creux par-dessus les messages qui défilent.
            historique = historique.child(div().absolute().top_0().left_0().right_0().bottom_0().shadow(relief(true)));
        }

        // Les grandes photos : l'autre en haut, moi en bas.
        let photos = choisie.as_ref().map(|conv| {
            div()
                .flex_none()
                .w(px(if live { 150. } else { 124. }))
                .h_full()
                .flex()
                .flex_col()
                .items_center()
                .justify_between()
                .py(px(if live { 10. } else { 2. }))
                .when(live, |d| {
                    d.ml(px(10.))
                        .rounded(px(6.))
                        .border_1()
                        .border_color(c(0xd3e4f7))
                        .bg(linear_gradient(180., linear_color_stop(c(0xf2f8ff), 0.), linear_color_stop(c(0xe4effc), 1.)))
                })
                .when(!live, |d| d.ml(px(6.)))
                .child({
                    // La grande photo de l'autre ouvre aussi sa fiche.
                    let id = conv.id.clone();
                    div()
                        .id("photo-personne")
                        .relative()
                        .when(!conv.group && !conv.large, |d| {
                            d.cursor_pointer().on_click(cx.listener(move |this, _, _, cx| {
                                this.ouvrir_fiche(&id);
                                cx.notify();
                            }))
                        })
                        .child(self.avatar_affiche(conv, 112.))
                        .when(self.en_ligne(conv), |d| {
                            d.child(presence::point_presence(112., if live { c(0xeef5fe) } else { c(GRIS_2000) }))
                        })
                })
                .child(self.mon_avatar(&self.initiale_moi(), 112.))
        });
        let corps = div()
            .flex_1()
            .min_h(px(0.))
            .flex()
            .flex_row()
            .when(live, |d| d.px(px(12.)).pt(px(12.)).pb(px(8.)))
            .when(!live, |d| d.pt(px(6.)))
            .child(historique)
            .children(photos);

        let composeur = self.composeur_retro(choisie.as_ref(), cx);
        // Le sélecteur d'emojis s'ouvre juste au-dessus de la barre d'outils.
        let haut_composeur = if live { 114. } else { 164. };

        let panneau = if live {
            panneau(th().fil, 0.).bg(c(0xf7fbff))
        } else {
            div()
        };
        panneau
            .relative()
            .flex_1()
            .h_full()
            .min_w(px(0.))
            .flex()
            .flex_col()
            .overflow_hidden()
            .child(entete)
            .child(corps)
            .child(composeur)
            .when(self.emojis_ouverts, |d| {
                d.child(div().absolute().left(px(14.)).bottom(px(haut_composeur)).child(self.panneau_emojis(cx)))
            })
            .when(self.fil_cherche_ouvert && choisie.is_some(), |d| {
                d.child(
                    div()
                        .absolute()
                        .right(px(16.))
                        .left(px(16.))
                        .top(px(64.))
                        .flex()
                        .flex_row()
                        .justify_end()
                        .child(self.barre_recherche_fil(cx)),
                )
            })
            .when(!self.fil_cherche_ouvert && self.cible.is_some() && self.fil_plus_loin, |d| {
                d.child(
                    div().absolute().left_0().right_0().top(px(70.)).flex().flex_row().justify_center().child(
                        verre(0.94, 8.).shadow(ombre_verre()).px(px(14.)).py(px(5.)).text_size(px(13.)).text_color(th().texte).child(t("Recherche plus loin…")),
                    ),
                )
            })
            .when(self.menu_conv && choisie.is_some(), |d| {
                d.child(div().absolute().right(px(16.)).top(px(54.)).child(self.menu_conversation(cx)))
            })
            .into_any_element()
    }

    /// La barre d'outils (emoji, fichier, vocal, emoji rapide) et la zone d'écriture encadrée.
    fn composeur_retro(&self, conv: Option<&Conv>, cx: &mut Context<Self>) -> Div {
        let live = th().live;
        let bas = div().flex_none().flex().flex_col();
        let bas = if live {
            bas.gap(px(8.))
                .px(px(18.))
                .pt(px(10.))
                .pb(px(14.))
                .bg(linear_gradient(180., linear_color_stop(c(0xe6f1fd), 0.), linear_color_stop(c(0xd4e6fa), 1.)))
                .border_t_1()
                .border_color(c(0xb9d3f1))
        } else {
            bas.gap(px(6.)).pt(px(6.))
        };
        if conv.is_none() {
            return bas;
        }
        if conv.is_some_and(|c| c.read_only) {
            return bas.child(div().py(px(10.)).text_center().text_size(px(13.)).text_color(th().texte_pale).child(t("Conversation en lecture seule")));
        }
        let teinte_icone = if live { c(0x1f5fb8) } else { c(0x000000) };
        let petit = |id: &'static str| {
            let b = bouton_outil(id);
            if live { b.h(px(26.)).rounded(px(5.)).border_color(c(0x8fb4e2)) } else { b.h(px(24.)) }
        };
        let rapide = self.emoji_rapide_fil().map(str::to_string);
        let outils = div()
            .flex()
            .flex_row()
            .items_center()
            .gap(px(6.))
            .child(
                petit("emojis")
                    .child(icone("face-slightly-smiling", 15., if self.emojis_ouverts { th().accent } else { teinte_icone }))
                    .child("▾")
                    .when(!live && self.emojis_ouverts, |d| d.shadow(relief(true)))
                    .on_click(cx.listener(|this, _, _, cx| {
                        this.emojis_ouverts = !this.emojis_ouverts;
                        this.reaction_cible = None;
                        cx.notify();
                    })),
            )
            .child(petit("joindre").child(icone("paperclip", 14., teinte_icone)).child(t("Fichier…")).on_click(cx.listener(|this, _, _, cx| this.joindre(cx))))
            .child(
                petit("micro")
                    .child(icone("audio-lines", 14., if self.vocal.is_some() { th().danger } else { teinte_icone }))
                    .child(t("Vocal"))
                    .on_click(cx.listener(|this, _, _, cx| {
                        this.basculer_vocal();
                        cx.notify();
                    })),
            )
            .when_some(rapide, |d, e| {
                d.child(
                    petit("emoji-rapide")
                        .child(div().font_family(POLICE_EMOJI).text_size(px(15.)).child(e))
                        .on_click(cx.listener(|this, _, _, cx| this.envoyer_emoji_rapide(cx))),
                )
            })
            .child(div().flex_1())
            .child(logo(conv.map(|c| service_de(&c.id)).unwrap_or(Service::GoogleMessages), 18.));
        let centre: AnyElement = match &self.vocal {
            Some(v) => {
                let s = v.debut.elapsed().as_secs();
                div()
                    .flex()
                    .flex_row()
                    .items_center()
                    .gap(px(8.))
                    .child(div().size(px(10.)).rounded(rayon(10.)).bg(th().danger))
                    .child(div().flex_1().text_color(th().texte_fort).child(format!("Enregistrement {}:{:02}", s / 60, s % 60)))
                    .child(
                        div()
                            .id("vocal-annuler")
                            .cursor_pointer()
                            .text_size(px(13.))
                            .text_color(th().danger)
                            .on_click(cx.listener(|this, _, _, cx| {
                                this.arreter_vocal(false);
                                cx.notify();
                            }))
                            .child(t("Annuler")),
                    )
                    .into_any_element()
            }
            None => {
                let vue = cx.entity().downgrade();
                Input::new(&self.saisie)
                    .appearance(false)
                    .on_paste(move |item, _, cx| vue.update(cx, |this, cx| this.coller_pieces(item, cx)).unwrap_or(false))
                    .into_any_element()
            }
        };
        let zone = div().flex_1().min_w(px(0.)).bg(c(0xffffff)).px(px(8.)).py(px(6.)).child(centre);
        let envoi = bouton_envoi("envoyer", t("Envoyer")).on_click(cx.listener(|this, _, window, cx| this.envoyer(window, cx)));
        if live {
            bas.child(outils).child(
                div()
                    .flex()
                    .flex_row()
                    .gap(px(10.))
                    .h(px(58.))
                    .child(zone.rounded(px(6.)).border_1().border_color(c(0x8fb4e2)))
                    .child(envoi),
            )
        } else {
            bas.child(outils)
                .child(div().h(px(64.)).flex().flex_row().child(zone.p(px(6.)).shadow(relief(true))))
                .child(div().flex().flex_row().justify_end().child(envoi))
        }
    }

    /// La barre de menu de Sirène : chaque menu ouvre sa partie de Jasette.
    pub(crate) fn barre_menu_sirene(&self, cx: &mut Context<Self>) -> Div {
        let menu = |id: &'static str, texte: &'static str| {
            let mut lettres = texte.chars();
            let premiere = lettres.next().map(String::from).unwrap_or_default();
            let reste: String = lettres.collect();
            div()
                .id(id)
                .flex()
                .flex_row()
                .px(px(6.))
                .py(px(1.))
                .cursor_pointer()
                .hover(|s| s.bg(c(MARINE)).text_color(c(0xffffff)))
                .child(div().underline().child(premiere))
                .child(div().child(reste))
        };
        div()
            .flex_none()
            .h(px(24.))
            .flex()
            .flex_row()
            .items_center()
            .gap(px(6.))
            .px(px(6.))
            .bg(c(GRIS_2000))
            .border_b_1()
            .border_color(c(0x808080))
            .text_size(px(13.))
            .text_color(c(0x000000))
            .child(menu("menu-fichier", t("Fichier")).on_click(cx.listener(|this, _, _, cx| {
                this.galerie = None;
                this.section = Some(Section::Comptes);
                cx.notify();
            })))
            .child(menu("menu-contacts", t("Contacts")).on_click(cx.listener(|this, _, _, cx| {
                this.section = None;
                this.galerie = None;
                this.vue = Vue::Tout;
                cx.notify();
            })))
            .child(menu("menu-affichage", t("Affichage")).on_click(cx.listener(|this, _, _, cx| {
                this.galerie = None;
                this.section = Some(Section::Apparence);
                cx.notify();
            })))
            .child(menu("menu-aide", t("Aide")).on_click(cx.listener(|this, _, _, cx| {
                this.galerie = None;
                this.section = Some(Section::APropos);
                cx.notify();
            })))
    }

    /// La barre d'état de Sirène : les services connectés, le dernier message.
    pub(crate) fn barre_etat_sirene(&self) -> Div {
        let n = self.services_connectes();
        let dernier = self.convs.iter().map(|c| c.time).max().filter(|t| *t > 0);
        let separateur = || div().w(px(1.)).h(px(14.)).bg(c(0x808080));
        div()
            .flex_none()
            .h(px(22.))
            .flex()
            .flex_row()
            .items_center()
            .gap(px(12.))
            .px(px(8.))
            .bg(c(GRIS_2000))
            .border_t_1()
            .border_color(c(0x808080))
            .text_size(px(12.))
            .text_color(c(0x303030))
            .child(pastille(if n > 0 { Niveau::Vert } else { Niveau::Gris }, 14.))
            .child(format!("{n} {}", if n > 1 { t("services connectés") } else { t("service connecté") }))
            .when_some(dernier, |d, ms| {
                d.child(separateur()).child(format!("{} {}", t("Dernier message :"), heure(ms, true)))
            })
    }
}

/// Le texte d'un message sans bulle : bleu foncé pour moi en Live, noir en Sirène.
pub(crate) fn texte_ligne(moi: bool) -> Hsla {
    if th().live {
        if moi { c(0x0b3c8a) } else { c(0x1d2f4a) }
    } else {
        c(0x111111)
    }
}

/// Le côté de la photo réduite (en pixels d'époque), et l'agrandissement au plus
/// proche voisin : chaque pixel devient un bloc de 16 × 16, assez grand pour que
/// GPUI, qui lisse en réduisant, garde des blocs nets à toutes les tailles.
const COTE_PIXEL: u32 = 32;
const BLOC_PIXEL: u32 = 16;

/// Une photo de profil pixelisée (Live et Sirène) : recadrée au carré, réduite
/// à 32 × 32, puis agrandie sans lissage. Gardée en mémoire par fichier et par
/// date de modification : calculée une fois, pas à chaque image.
pub(crate) fn photo_pixel(chemin: &std::path::Path) -> Option<Arc<gpui::Image>> {
    static CACHE: std::sync::OnceLock<Mutex<HashMap<(PathBuf, Option<std::time::SystemTime>), Option<Arc<gpui::Image>>>>> =
        std::sync::OnceLock::new();
    let date = std::fs::metadata(chemin).and_then(|m| m.modified()).ok();
    let cle = (chemin.to_path_buf(), date);
    let cache = CACHE.get_or_init(Default::default);
    if let Some(i) = cache.lock().unwrap_or_else(|e| e.into_inner()).get(&cle) {
        return i.clone();
    }
    let image = pixeliser(chemin);
    cache.lock().unwrap_or_else(|e| e.into_inner()).insert(cle, image.clone());
    image
}

fn pixeliser(chemin: &std::path::Path) -> Option<Arc<gpui::Image>> {
    use image::imageops::FilterType;
    let source = image::open(chemin).ok()?;
    let (w, h) = (source.width(), source.height());
    let cote = w.min(h).max(1);
    let carre = source.crop_imm((w - cote) / 2, (h - cote) / 2, cote, cote);
    // La réduction fait la moyenne des pixels ; l'agrandissement les répète.
    let petite = carre.resize_exact(COTE_PIXEL, COTE_PIXEL, FilterType::Triangle);
    let grande = petite.resize_exact(COTE_PIXEL * BLOC_PIXEL, COTE_PIXEL * BLOC_PIXEL, FilterType::Nearest);
    let mut png = Vec::new();
    image::DynamicImage::ImageRgba8(grande.to_rgba8())
        .write_to(&mut std::io::Cursor::new(&mut png), image::ImageFormat::Png)
        .ok()?;
    Some(Arc::new(gpui::Image::from_bytes(gpui::ImageFormat::Png, png)))
}
