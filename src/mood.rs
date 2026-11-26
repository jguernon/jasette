// Le « Mood » d'une conversation (bureau) : la couleur de mes bulles, une
// teinte légère des bulles reçues et le fond du fil, un dégradé doux à trois
// taches floues, en variante sombre (profonde) ou verre (pastel).
//
// - Choisi par le menu ⋮ du fil (item « Mood »), dans une feuille avec un
//   aperçu en direct et une grille de cartes ; « Par défaut » le retire.
// - Gardé dans reglages.json (`moods`, id → nom), et partagé par le serveur
//   Jasette s'il y en a un (marque `moods`, PROTOCOLE.md §5.5). Android a la
//   même table (ui/Mood.kt) : mêmes noms, mêmes couleurs.
// - L'EGA n'en a pas : ni l'item, ni les couleurs (un mood posé est ignoré).
use super::*;

/// Un mood : mes bulles (dégradé début → fin, et leur texte), puis trois
/// taches de couleur, d'où viennent le fond du fil et la bulle reçue.
pub(crate) struct Mood {
    pub id: &'static str,
    fr: &'static str,
    en: &'static str,
    envoye: (u32, u32),
    texte: u32,
    taches: [u32; 3],
}

/// Les moods, dans l'ordre de la grille (« Par défaut » vient avant).
/// Contraste du texte sur mes bulles : 4,5 au moins aux deux bouts du dégradé.
pub(crate) const MOODS: [Mood; 12] = [
    Mood { id: "rose_bebe", fr: "Rose bébé", en: "Baby pink", envoye: (0xffb8d2, 0xf7a1c6), texte: 0x4a1130, taches: [0xff9ec4, 0xffc4dc, 0xe9a8ff] },
    Mood { id: "amour", fr: "Amour", en: "Love", envoye: (0xe11d48, 0xbe123c), texte: 0xffffff, taches: [0xff3b5c, 0xff7a8a, 0xb0174f] },
    Mood { id: "bleu_bebe", fr: "Bleu bébé", en: "Baby blue", envoye: (0xaedcff, 0x8ec5ff), texte: 0x0d2a4a, taches: [0x8ecbff, 0xbde3ff, 0xa9b6ff] },
    Mood { id: "vert_pomme", fr: "Vert pomme", en: "Apple green", envoye: (0x9be15d, 0x6fd14a), texte: 0x123508, taches: [0x9be15d, 0xc6f27a, 0x4fd18b] },
    Mood { id: "lavande", fr: "Lavande", en: "Lavender", envoye: (0x7c5cf0, 0x6544dc), texte: 0xffffff, taches: [0xb69cff, 0xd4c2ff, 0x8fa8ff] },
    Mood { id: "peche", fr: "Pêche", en: "Peach", envoye: (0xffbf98, 0xffa07e), texte: 0x4a1d08, taches: [0xffb38a, 0xffd3b0, 0xff9aa2] },
    Mood { id: "menthe", fr: "Menthe", en: "Mint", envoye: (0x0c8470, 0x0b7a76), texte: 0xffffff, taches: [0x6ee7c5, 0xa7f3d0, 0x5ccfe6] },
    Mood { id: "soleil", fr: "Soleil", en: "Sunshine", envoye: (0xffd84a, 0xffb92e), texte: 0x3d2a00, taches: [0xffd93d, 0xffe98a, 0xffa94d] },
    Mood { id: "ocean", fr: "Océan", en: "Ocean", envoye: (0x0a78b8, 0x0b5aa0), texte: 0xffffff, taches: [0x1fb6ff, 0x3ad1c6, 0x2a5bd7] },
    Mood { id: "aurore", fr: "Aurore", en: "Aurora", envoye: (0x7c3aed, 0xc0267a), texte: 0xffffff, taches: [0xa78bfa, 0xf472b6, 0x60a5fa] },
    Mood { id: "nuit", fr: "Nuit étoilée", en: "Starry night", envoye: (0x3346c8, 0x5233b8), texte: 0xffffff, taches: [0x3949ab, 0x5e35b1, 0x1e88e5] },
    Mood { id: "coucher", fr: "Coucher de soleil", en: "Sunset", envoye: (0xcc3f0c, 0xbe2a5a), texte: 0xffffff, taches: [0xff8a3d, 0xff5e7e, 0x8b5cf6] },
];

/// Les taches : (x, y) en fraction du cadre, et l'opacité en sombre et en verre.
const PLACES: [(f32, f32, f32, f32); 3] = [(0.15, 0.20, 0.55, 0.62), (0.85, 0.55, 0.45, 0.55), (0.35, 0.92, 0.42, 0.50)];

fn tr(fr: &'static str, en: &'static str) -> &'static str {
    if anglais() { en } else { fr }
}

/// Le mood de ce nom ; jamais en EGA.
pub(crate) fn mood(nom: &str) -> Option<&'static Mood> {
    if th().ega {
        return None;
    }
    MOODS.iter().find(|m| m.id == nom)
}

/// a → b, canal par canal (t = 0 : a ; t = 1 : b).
fn mix(a: u32, b: u32, t: f32) -> u32 {
    let canal = |s: u32| {
        let (x, y) = (((a >> s) & 0xff) as f32, ((b >> s) & 0xff) as f32);
        ((x + (y - x) * t).round() as u32).min(255) << s
    };
    canal(16) | canal(8) | canal(0)
}

impl Mood {
    pub(crate) fn nom(&self) -> &'static str {
        if anglais() { self.en } else { self.fr }
    }

    /// Le haut et le bas du dégradé du fond (sombre : profond ; verre : pastel).
    pub(crate) fn haut(&self) -> Hsla {
        c(if th().sombre { mix(self.taches[0], 0, 0.84) } else { mix(self.taches[0], 0xffffff, 0.80) })
    }
    pub(crate) fn bas(&self) -> Hsla {
        c(if th().sombre { mix(self.taches[1], 0, 0.88) } else { mix(self.taches[1], 0xffffff, 0.84) })
    }

    pub(crate) fn fond(&self) -> gpui::Background {
        linear_gradient(180., linear_color_stop(self.haut(), 0.), linear_color_stop(self.bas(), 1.))
    }

    /// Mes bulles : le dégradé en biais, et la couleur de leur texte.
    pub(crate) fn envoye(&self) -> gpui::Background {
        linear_gradient(135., linear_color_stop(c(self.envoye.0), 0.), linear_color_stop(c(self.envoye.1), 1.))
    }
    pub(crate) fn texte_envoye(&self) -> Hsla {
        c(self.texte)
    }
    /// L'heure et les coches sur mes bulles.
    pub(crate) fn heure_envoyee(&self) -> Hsla {
        ca(self.texte, 0.72)
    }
    /// La tache floue de mes bulles sous l'en-tête.
    pub(crate) fn tache_envoyee(&self) -> Hsla {
        ca(mix(self.envoye.0, self.envoye.1, 0.5), 0.6)
    }

    /// Les bulles reçues : une teinte légère de la première tache.
    pub(crate) fn recue(&self) -> Hsla {
        if th().sombre { c(mix(self.taches[0], 0x1c1c1e, 0.80)) } else { ca(mix(self.taches[0], 0xffffff, 0.86), 0.85) }
    }

    /// Les taches floues du fond, sur toute la boîte parente ; `l` : la taille
    /// d'une tache (px). Pas de fond : seule l'ombre, très floue, dessine.
    pub(crate) fn taches(&self, l: f32) -> Div {
        let mut d = div().absolute().top_0().left_0().right_0().bottom_0().overflow_hidden();
        for (i, (x, y, a_sombre, a_verre)) in PLACES.into_iter().enumerate() {
            let a = if th().sombre { a_sombre } else { a_verre };
            d = d.child(
                div().absolute().left(gpui::relative(x)).top(gpui::relative(y)).size(px(0.)).child(
                    div()
                        .absolute()
                        .left(px(-l / 2.))
                        .top(px(-l / 2.))
                        .size(px(l))
                        .rounded(px(l))
                        .shadow(vec![BoxShadow {
                            color: ca(self.taches[i], a),
                            offset: point(px(0.), px(0.)),
                            blur_radius: px(l * 0.5),
                            spread_radius: px(0.),
                            inset: false,
                        }]),
                ),
            );
        }
        d
    }
}

impl Jasette {
    /// Le mood de la conversation ouverte (aucun en EGA).
    pub(crate) fn mood_fil(&self) -> Option<&'static Mood> {
        let id = self.choisie.as_deref()?;
        mood(self.reglages.moods.get(id)?)
    }

    /// L'item « Mood » du menu ⋮ (absent en EGA).
    pub(crate) fn item_menu_mood(&self, id: &str, cx: &mut Context<Self>) -> Option<AnyElement> {
        if th().ega {
            return None;
        }
        let id = id.to_string();
        let actuel = self.reglages.moods.get(&id).and_then(|n| mood(n));
        Some(
            div()
                .id("menu-mood")
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
                .child(icone("palette", 16., th().texte_fort))
                .child(div().flex_1().child("Mood"))
                .when_some(actuel, |d, m| {
                    d.child(div().size(px(14.)).rounded(px(999.)).bg(m.envoye()).border_1().border_color(blanc(0.6)))
                })
                .on_click(cx.listener(move |this, _, _, cx| {
                    this.menu_conv = false;
                    let essai = this.reglages.moods.get(&id).and_then(|n| mood(n)).map(|m| m.id);
                    this.mood_feuille = Some((id.clone(), essai));
                    cx.notify();
                }))
                .into_any_element(),
        )
    }

    /// Pose (ou retire, avec None) le mood d'une conversation : gardé, puis
    /// envoyé au serveur avec les autres marques.
    pub(crate) fn poser_mood(&mut self, id: &str, nom: Option<&'static str>) {
        let avant = self.reglages.moods.get(id).cloned();
        if avant.as_deref() == nom {
            return;
        }
        match nom {
            Some(n) => {
                self.reglages.moods.insert(id.to_string(), n.to_string());
            }
            None => {
                self.reglages.moods.remove(id);
            }
        }
        self.marques_changees();
    }

    /// Des bulles d'exemple sur le fond d'un mood (ou du thème, sans mood).
    fn exemple_bulle(m: Option<&Mood>, texte: &'static str, moi: bool, taille: f32, dernier: bool) -> Div {
        let d = div()
            .max_w(px(300.))
            .px(px(taille * 0.9))
            .py(px(taille * 0.5))
            .rounded(rayon(taille * 1.3))
            .when(dernier && moi, |d| d.rounded_br(rayon(5.)))
            .when(dernier && !moi, |d| d.rounded_bl(rayon(5.)))
            .text_size(px(taille))
            .child(texte);
        match (m, moi) {
            (Some(m), true) => d.bg(m.envoye()).text_color(m.texte_envoye()).shadow(ombre_flottante()),
            (Some(m), false) => d.bg(m.recue()).border_1().border_color(th().surface_bord).text_color(th().texte_fort),
            (None, true) => d.bg(degrade_accent()).text_color(blanc(1.)).shadow(ombre_flottante()),
            (None, false) => d.bg(th().surface).border_1().border_color(th().surface_bord).text_color(th().texte_fort),
        }
    }

    /// Le fond d'un aperçu : celui du mood (dégradé et taches), ou celui du fil.
    /// `r` : le rayon de ses coins. GPUI ne découpe qu'en rectangle : un cadre
    /// de la couleur de la feuille, arrondi par l'intérieur, recouvre ce que
    /// les taches laissent déborder dans les coins (il faut 8 px ≥ 0,42 r).
    fn fond_apercu(m: Option<&Mood>, d: Div, l: f32, r: f32) -> Div {
        let d = d.rounded(px(r)).overflow_hidden();
        match m {
            Some(m) => d.bg(m.fond()).child(m.taches(l)).child(
                div()
                    .absolute()
                    .top(px(-8.))
                    .left(px(-8.))
                    .right(px(-8.))
                    .bottom(px(-8.))
                    .rounded(px(r + 8.))
                    .border_8()
                    .border_color(Self::fond_feuille()),
            ),
            None => d.bg(if th().sombre { c(0x000000) } else { blanc(0.55) }),
        }
    }

    /// La couleur de la feuille « Mood ».
    fn fond_feuille() -> Hsla {
        if th().sombre { th().menu } else { blanc(0.97) }
    }

    /// Une carte de la grille : le mood en miniature (fond, une bulle reçue,
    /// une envoyée), son nom dessous.
    fn carte_mood(&self, m: Option<&'static Mood>, choisi: bool, cx: &mut Context<Self>) -> AnyElement {
        let nom = m.map(|m| m.nom()).unwrap_or_else(|| tr("Par défaut", "Default"));
        let cle = m.map(|m| m.id).unwrap_or("defaut");
        let pastille = |moi: bool, w: f32| {
            let d = div().h(px(9.)).w(px(w)).rounded(px(999.));
            match (m, moi) {
                (Some(m), true) => d.bg(m.envoye()),
                (Some(m), false) => d.bg(m.recue()),
                (None, true) => d.bg(degrade_accent()),
                (None, false) => d.bg(th().surface),
            }
        };
        let mini = Self::fond_apercu(
            m,
            div()
                .relative()
                .w_full()
                .h(px(64.))
                .border_1()
                .border_color(if th().sombre { blanc(0.08) } else { hsla(0., 0., 0., 0.07) }),
            46.,
            14.,
        )
        .child(
            div()
                .absolute()
                .top_0()
                .left_0()
                .right_0()
                .bottom_0()
                .flex()
                .flex_col()
                .justify_center()
                .gap(px(6.))
                .px(px(10.))
                .child(div().flex().flex_row().child(pastille(false, 38.)))
                .child(div().flex().flex_row().justify_end().child(pastille(true, 30.))),
        )
        .when(m.is_none(), |d| {
            d.child(
                div()
                    .absolute()
                    .top(px(5.))
                    .right(px(6.))
                    .child(icone("x", 12., th().texte_pale)),
            )
        });
        let anneau = if choisi { th().accent } else { hsla(0., 0., 0., 0.) };
        div()
            .id(SharedString::from(format!("mood-{cle}")))
            .w(px(94.))
            .flex()
            .flex_col()
            .items_center()
            .gap(px(6.))
            .cursor_pointer()
            .child(
                div()
                    .relative()
                    .w_full()
                    .p(px(3.))
                    .rounded(px(18.))
                    .border_2()
                    .border_color(anneau)
                    .hover(|s| s.bg(th().survol))
                    .child(mini)
                    .when(choisi, |d| {
                        d.child(
                            div()
                                .absolute()
                                .bottom(px(-6.))
                                .right(px(-6.))
                                .size(px(22.))
                                .rounded(px(999.))
                                .bg(th().accent)
                                .border_2()
                                .border_color(th().menu)
                                .flex()
                                .items_center()
                                .justify_center()
                                .child(icone("check", 12., blanc(1.))),
                        )
                    }),
            )
            .child(
                div()
                    .text_size(px(12.))
                    .line_height(px(15.))
                    .text_center()
                    .text_color(if choisi { th().texte_fort } else { th().texte })
                    .when(choisi, |d| d.font_weight(FontWeight::SEMIBOLD))
                    .max_w_full()
                    .child(nom),
            )
            .on_click(cx.listener(move |this, ev: &gpui::ClickEvent, _, cx| {
                if let Some((id, essai)) = this.mood_feuille.as_mut() {
                    // Un double clic (ou un clic sur la carte déjà choisie) applique.
                    let nouveau = m.map(|m| m.id);
                    if ev.click_count() >= 2 || *essai == nouveau {
                        let id = id.clone();
                        this.mood_feuille = None;
                        this.poser_mood(&id, nouveau);
                    } else {
                        *essai = nouveau;
                    }
                }
                cx.notify();
            }))
            .into_any_element()
    }

    /// La feuille « Mood » : aperçu en direct, grille, Annuler / Appliquer.
    pub(crate) fn feuille_mood(&self, cx: &mut Context<Self>) -> Option<AnyElement> {
        let (id, essai) = self.mood_feuille.clone()?;
        if th().ega {
            return None;
        }
        let m = essai.and_then(mood);
        let nom_conv = self.conv(&id).map(|c| c.name.clone()).filter(|n| !n.is_empty()).unwrap_or_default();
        let titre = div()
            .flex()
            .flex_row()
            .items_center()
            .gap(px(12.))
            .child(
                div()
                    .size(px(36.))
                    .rounded(px(12.))
                    .flex()
                    .items_center()
                    .justify_center()
                    .bg(m.map(|m| m.envoye()).unwrap_or_else(degrade_accent))
                    .child(icone("palette", 18., m.map(|m| m.texte_envoye()).unwrap_or(blanc(1.)))),
            )
            .child(
                div()
                    .flex_1()
                    .flex()
                    .flex_col()
                    .child(div().text_size(px(18.)).font_weight(FontWeight::BOLD).text_color(th().texte_fort).child("Mood"))
                    .child(
                        div().text_size(px(12.)).text_color(th().texte_pale).truncate().child(if nom_conv.is_empty() {
                            tr("Les couleurs de cette conversation", "This conversation's colours").to_string()
                        } else {
                            format!("{} · {}", nom_conv, m.map(|m| m.nom()).unwrap_or_else(|| tr("Par défaut", "Default")))
                        }),
                    ),
            )
            .child(
                div()
                    .id("mood-fermer")
                    .size(px(30.))
                    .rounded(px(999.))
                    .flex()
                    .items_center()
                    .justify_center()
                    .cursor_pointer()
                    .hover(|s| s.bg(th().survol))
                    .on_click(cx.listener(|this, _, _, cx| {
                        this.mood_feuille = None;
                        cx.notify();
                    }))
                    .child(icone("x", 16., th().texte_pale)),
            );
        // L'aperçu en direct : le fond choisi, trois bulles, dans le thème actuel.
        let apercu = Self::fond_apercu(
            m,
            div().relative().w_full().h(px(186.)).border_1().border_color(if th().sombre { blanc(0.06) } else { blanc(0.85) }),
            260.,
            18.,
        )
        .child(
            div()
                .absolute()
                .top_0()
                .left_0()
                .right_0()
                .bottom_0()
                .flex()
                .flex_col()
                .justify_center()
                .gap(px(6.))
                .px(px(18.))
                .child(div().flex().flex_row().child(Self::exemple_bulle(
                    m,
                    tr("T'as vu le ciel ce soir ?", "Did you see the sky tonight?"),
                    false,
                    14.,
                    true,
                )))
                .child(div().flex().flex_row().justify_end().child(Self::exemple_bulle(
                    m,
                    tr("Oui ! Il était magnifique 😍", "Yes! It was gorgeous 😍"),
                    true,
                    14.,
                    false,
                )))
                .child(div().flex().flex_row().justify_end().child(Self::exemple_bulle(
                    m,
                    tr("On y retourne demain ?", "Back there tomorrow?"),
                    true,
                    14.,
                    true,
                ))),
        );
        let mut grille = div().flex().flex_row().flex_wrap().gap_x(px(9.)).gap_y(px(14.)).justify_center();
        grille = grille.child(self.carte_mood(None, essai.is_none(), cx));
        for mo in MOODS.iter() {
            grille = grille.child(self.carte_mood(Some(mo), essai == Some(mo.id), cx));
        }
        let bouton = |cle: &'static str, texte: &'static str, plein: bool| {
            div()
                .id(cle)
                .px(px(18.))
                .py(px(8.))
                .rounded(px(999.))
                .text_size(px(14.))
                .font_weight(FontWeight::SEMIBOLD)
                .cursor_pointer()
                .when(plein, |d| d.bg(degrade_accent()).text_color(blanc(1.)).shadow(ombre_flottante()))
                .when(!plein, |d| d.bg(th().menu_champ).text_color(th().texte_fort))
                .hover(|s| s.opacity(0.88))
                .child(texte)
        };
        let id_ok = id.clone();
        let boutons = div()
            .flex()
            .flex_row()
            .justify_end()
            .gap(px(10.))
            .child(bouton("mood-annuler", t("Annuler"), false).on_click(cx.listener(|this, _, _, cx| {
                this.mood_feuille = None;
                cx.notify();
            })))
            .child(bouton("mood-ok", tr("Appliquer", "Apply"), true).on_click(cx.listener(move |this, _, _, cx| {
                this.mood_feuille = None;
                this.poser_mood(&id_ok, essai);
                cx.notify();
            })));
        let feuille = verre(0.97, 26.)
            .when(th().sombre, |d| d.bg(th().menu).border_1().border_color(th().trait_fin))
            .shadow(ombre_verre())
            .id("mood-feuille")
            .w(px(760.))
            .max_h_full()
            .overflow_y_scroll()
            .p(px(22.))
            .flex()
            .flex_col()
            .gap(px(18.))
            // Un clic dans la feuille ne la ferme pas.
            .on_click(|_, _, cx| cx.stop_propagation())
            .child(titre)
            .child(apercu)
            .child(grille)
            .child(boutons);
        Some(
            div()
                .id("mood-voile")
                .absolute()
                .top_0()
                .left_0()
                .right_0()
                .bottom_0()
                .bg(hsla(0., 0., 0., if th().sombre { 0.55 } else { 0.25 }))
                .p(px(24.))
                .flex()
                .items_center()
                .justify_center()
                .on_click(cx.listener(|this, _, _, cx| {
                    this.mood_feuille = None;
                    cx.notify();
                }))
                .child(feuille)
                .into_any_element(),
        )
    }
}
