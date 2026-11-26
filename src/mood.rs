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
// - La même feuille choisit l'« emoji rapide » de la conversation (aucun par
//   défaut) : un bouton rond à droite de la saisie, à la place de l'envoi
//   quand le champ est vide, qui l'envoie d'un clic. Il existe aussi en EGA :
//   le menu ⋮ y a l'item « Emoji rapide », et la feuille n'a que cette rangée.
//   Marque `emojis` (id → emoji, 32 octets au plus), comme `moods`.
use super::*;

/// L'état de la feuille ouverte : la conversation, le mood essayé, l'emoji
/// rapide essayé (None : aucun) et le sélecteur « Autre… » déplié.
#[derive(Clone)]
pub(crate) struct FeuilleMood {
    pub id: String,
    pub essai: Option<&'static str>,
    pub emoji: Option<String>,
    pub autre: bool,
}

/// Les emojis rapides proposés (« Aucun » vient avant, « Autre… » après).
pub(crate) const EMOJIS_RAPIDES: [&str; 16] =
    ["👍", "❤️", "😂", "😍", "🥰", "🔥", "🎉", "😘", "🤗", "👋", "🌹", "✨", "🙏", "😎", "💯", "😊"];

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
                        .rounded(coin(l))
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

/// Un emoji bien centré dans un rond : une boîte carrée à sa taille, avec une
/// hauteur de ligne égale à la taille (la hauteur de ligne par défaut, plus
/// haute, et les métriques de la police d'emojis le poussaient vers le bas).
pub(crate) fn emoji_centre(e: &str, taille: f32) -> Div {
    div()
        .size(px(taille * 1.25))
        .flex()
        .items_center()
        .justify_center()
        .font_family(POLICE_EMOJI)
        .text_size(px(taille))
        .line_height(px(taille * 1.25))
        .child(e.to_string())
}

impl Jasette {
    /// Le mood de la conversation ouverte (aucun en EGA).
    pub(crate) fn mood_fil(&self) -> Option<&'static Mood> {
        let id = self.choisie.as_deref()?;
        mood(self.reglages.moods.get(id)?)
    }

    /// L'emoji rapide de la conversation ouverte (aucun par défaut).
    pub(crate) fn emoji_rapide_fil(&self) -> Option<&str> {
        let id = self.choisie.as_deref()?;
        self.reglages.emojis.get(id).map(String::as_str).filter(|e| !e.is_empty())
    }

    /// Le bouton rond de l'emoji rapide, à droite de la saisie : un clic
    /// l'envoie seul, comme un message texte.
    pub(crate) fn bouton_emoji_rapide(&self, e: &str, cx: &mut Context<Self>) -> AnyElement {
        div()
            .id("emoji-rapide")
            .size(px(42.))
            .flex_none()
            .rounded(rayon(999.))
            .flex()
            .items_center()
            .justify_center()
            .cursor_pointer()
            .bg(th().surface)
            .border_1()
            .border_color(th().surface_bord)
            .shadow(ombre_flottante())
            .hover(|s| s.bg(th().survol))
            // La pression : le bouton pâlit sous le clic.
            .active(|s| s.opacity(0.6))
            .on_click(cx.listener(|this, _, _, cx| this.envoyer_emoji_rapide(cx)))
            .child(emoji_centre(e, 22.))
            .into_any_element()
    }

    /// Envoie l'emoji rapide de la conversation ouverte, par le même chemin
    /// qu'un message texte (message en attente, tmp_id, coches).
    pub(crate) fn envoyer_emoji_rapide(&mut self, cx: &mut Context<Self>) {
        let Some(e) = self.emoji_rapide_fil().map(str::to_string) else { return };
        let Some(conv) = self.choisie.clone() else { return };
        if self.etat != Etat::Pret {
            return;
        }
        let tmp = self.en_attente(&conv, e.clone(), String::new(), Vec::new());
        self.commande(json!({"cmd": "send", "conv": conv, "text": e, "tmp_id": tmp}));
        self.emojis_ouverts = false;
        cx.notify();
    }

    /// Pose (ou retire, avec None) l'emoji rapide d'une conversation.
    pub(crate) fn poser_emoji_rapide(&mut self, id: &str, e: Option<String>) {
        let e = e.filter(|e| !e.is_empty() && e.len() <= pont::distant::EMOJI_RAPIDE_MAX);
        if self.reglages.emojis.get(id) == e.as_ref() {
            return;
        }
        match e {
            Some(e) => {
                self.reglages.emojis.insert(id.to_string(), e);
            }
            None => {
                self.reglages.emojis.remove(id);
            }
        }
        self.marques_changees();
    }

    /// Ouvre la feuille pour une conversation, avec ses choix actuels.
    pub(crate) fn ouvrir_feuille_mood(&mut self, id: &str) {
        self.menu_conv = false;
        self.emojis_ouverts = false;
        self.reaction_cible = None;
        let essai = self.reglages.moods.get(id).and_then(|n| mood(n)).map(|m| m.id);
        let emoji = self.reglages.emojis.get(id).cloned();
        self.mood_feuille = Some(FeuilleMood { id: id.to_string(), essai, emoji, autre: false });
    }

    /// Applique ce que la feuille a d'essayé, et la ferme.
    fn appliquer_feuille_mood(&mut self) {
        if let Some(f) = self.mood_feuille.take() {
            if !th().ega {
                self.poser_mood(&f.id, f.essai);
            }
            self.poser_emoji_rapide(&f.id, f.emoji);
        }
    }

    /// L'item « Mood » du menu ⋮ ; en EGA, « Emoji rapide » (sans mood).
    pub(crate) fn item_menu_mood(&self, id: &str, cx: &mut Context<Self>) -> Option<AnyElement> {
        let emoji = self.reglages.emojis.get(id).cloned();
        if th().ega {
            let id = id.to_string();
            return Some(
                div()
                    .id("menu-emoji-rapide")
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
                    .child(icone("face-slightly-smiling", 16., th().texte_fort))
                    .child(div().flex_1().child(tr("Emoji rapide", "Quick emoji")))
                    .when_some(emoji, |d, e| d.child(div().font_family(POLICE_EMOJI).child(e)))
                    .on_click(cx.listener(move |this, _, _, cx| {
                        this.ouvrir_feuille_mood(&id);
                        cx.notify();
                    }))
                    .into_any_element(),
            );
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
                .when_some(emoji, |d, e| d.child(div().font_family(POLICE_EMOJI).text_size(px(13.)).child(e)))
                .when_some(actuel, |d, m| {
                    d.child(div().size(px(14.)).rounded(coin(999.)).bg(m.envoye()).border_1().border_color(blanc(0.6)))
                })
                .on_click(cx.listener(move |this, _, _, cx| {
                    this.ouvrir_feuille_mood(&id);
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
        // Live et Sirène : pas de bulles, l'en-tête puis le texte en retrait (comme leur fil).
        if retro::retro() {
            let quand = if moi { "19:25" } else { "19:24" };
            let entete = if th().live {
                let qui = if moi { tr("Moi", "Me") } else { "Camille" };
                div().text_size(px(taille * 0.85)).text_color(c(0x5d7088)).child(if anglais() {
                    format!("{qui} says ({quand}):")
                } else {
                    format!("{qui} dit ({quand}) :")
                })
            } else {
                div()
                    .flex()
                    .flex_row()
                    .gap(px(5.))
                    .text_size(px(taille * 0.9))
                    .child(
                        div()
                            .font_weight(FontWeight::BOLD)
                            .text_color(if moi { c(0x1838c0) } else { c(0xc01818) })
                            .child(if moi { tr("Moi", "Me") } else { "Camille Tremblay" }),
                    )
                    .child(div().text_color(c(0x6a6a6a)).child(format!("({quand})")))
            };
            return div()
                .flex()
                .flex_col()
                .gap(px(1.))
                .child(entete)
                .child(div().pl(px(12.)).text_size(px(taille)).text_color(retro::texte_ligne(moi)).child(texte))
                .when(!dernier, |d| d.mb(px(-2.)));
        }
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
        let d = d.rounded(coin(r)).overflow_hidden();
        match m {
            // Live et Sirène : le fond seulement teinté, sans taches.
            Some(m) if retro::retro() => d.bg(m.fond()),
            Some(m) => d.bg(m.fond()).child(m.taches(l)).child(
                div()
                    .absolute()
                    .top(px(-8.))
                    .left(px(-8.))
                    .right(px(-8.))
                    .bottom(px(-8.))
                    .rounded(coin(r + 8.))
                    .border_8()
                    .border_color(Self::fond_feuille()),
            ),
            None => d.bg(if th().sombre { c(0x000000) } else if retro::retro() { th().fil } else { blanc(0.55) }),
        }
    }

    /// La couleur de la feuille « Mood ».
    fn fond_feuille() -> Hsla {
        if th().sombre || retro::retro() { th().menu } else { blanc(0.97) }
    }

    /// Une carte de la grille : le mood en miniature (fond, une bulle reçue,
    /// une envoyée), son nom dessous.
    fn carte_mood(&self, m: Option<&'static Mood>, choisi: bool, cx: &mut Context<Self>) -> AnyElement {
        let nom = m.map(|m| m.nom()).unwrap_or_else(|| tr("Par défaut", "Default"));
        let cle = m.map(|m| m.id).unwrap_or("defaut");
        let pastille = |moi: bool, w: f32| {
            if retro::retro() {
                // En miniature : un trait pour l'en-tête (rouge ou bleu en Sirène), un pour le texte.
                let nom = if th().live { c(0x8a9bb0) } else if moi { c(0x1838c0) } else { c(0xc01818) };
                return div()
                    .flex()
                    .flex_col()
                    .gap(px(3.))
                    .child(div().h(px(3.)).w(px(w * 0.5)).bg(nom))
                    .child(div().ml(px(5.)).h(px(4.)).w(px(w * 0.85)).bg(alpha(retro::texte_ligne(moi), 0.7)));
            }
            let d = div().h(px(9.)).w(px(w)).rounded(coin(999.));
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
                .child(div().flex().flex_row().when(!retro::retro(), |d| d.justify_end()).child(pastille(true, 30.))),
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
                    .rounded(coin(18.))
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
                                .rounded(coin(999.))
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
                if let Some(f) = this.mood_feuille.as_mut() {
                    // Un double clic (ou un clic sur la carte déjà choisie) applique.
                    let nouveau = m.map(|m| m.id);
                    let appliquer = ev.click_count() >= 2 || f.essai == nouveau;
                    f.essai = nouveau;
                    if appliquer {
                        this.appliquer_feuille_mood();
                    }
                }
                cx.notify();
            }))
            .into_any_element()
    }

    /// La rangée « Emoji rapide » de la feuille : Aucun, les emojis courants,
    /// celui choisi ailleurs s'il n'y est pas, et « Autre… » (le sélecteur).
    fn rangee_emoji_rapide(&self, f: &FeuilleMood, cx: &mut Context<Self>) -> AnyElement {
        let pastille = |cle: SharedString, choisi: bool| {
            div()
                .id(cle)
                .h(px(40.))
                .min_w(px(40.))
                .px(px(6.))
                .rounded(rayon(999.))
                .flex()
                .items_center()
                .justify_center()
                .cursor_pointer()
                .border_2()
                .border_color(if choisi { th().accent } else { hsla(0., 0., 0., 0.) })
                .when(choisi, |d| d.bg(th().menu_champ))
                .hover(|s| s.bg(th().survol))
        };
        let choisir = |e: Option<String>| {
            cx.listener(move |this: &mut Self, _: &gpui::ClickEvent, _: &mut Window, cx: &mut Context<Self>| {
                if let Some(f) = this.mood_feuille.as_mut() {
                    f.emoji = e.clone();
                    f.autre = false;
                }
                cx.notify();
            })
        };
        let mut rangee = div().flex().flex_row().flex_wrap().items_center().gap(px(4.));
        rangee = rangee.child(
            pastille("emoji-rapide-aucun".into(), f.emoji.is_none())
                .px(px(12.))
                .text_size(px(13.))
                .text_color(th().texte_fort)
                .on_click(choisir(None))
                .child(tr("Aucun", "None")),
        );
        let mut liste: Vec<String> = EMOJIS_RAPIDES.iter().map(|e| e.to_string()).collect();
        if let Some(e) = &f.emoji
            && !liste.contains(e)
        {
            liste.push(e.clone());
        }
        for (i, e) in liste.into_iter().enumerate() {
            let choisi = f.emoji.as_deref() == Some(e.as_str());
            rangee = rangee.child(
                pastille(format!("emoji-rapide-{i}").into(), choisi)
                    .on_click(choisir(Some(e.clone())))
                    .child(emoji_centre(&e, 21.)),
            );
        }
        rangee = rangee.child(
            pastille("emoji-rapide-autre".into(), f.autre)
                .px(px(12.))
                .text_size(px(13.))
                .text_color(th().texte_fort)
                .on_click(cx.listener(|this, _, _, cx| {
                    if let Some(f) = this.mood_feuille.as_mut() {
                        f.autre = !f.autre;
                    }
                    cx.notify();
                }))
                .child(tr("Autre…", "Other…")),
        );
        div()
            .flex()
            .flex_col()
            .gap(px(8.))
            .child(
                div()
                    .flex()
                    .flex_col()
                    .child(div().text_size(px(15.)).font_weight(FontWeight::SEMIBOLD).text_color(th().texte_fort).child(tr("Emoji rapide", "Quick emoji")))
                    .child(div().text_size(px(12.)).text_color(th().texte_pale).child(tr(
                        "Un bouton à droite de la zone d'écriture l'envoie d'un clic quand le champ est vide.",
                        "A button right of the message box sends it in one click when the box is empty.",
                    ))),
            )
            .child(rangee)
            .into_any_element()
    }

    /// La feuille « Mood » : aperçu en direct, grille, emoji rapide, Annuler /
    /// Appliquer. En EGA : l'emoji rapide seul.
    pub(crate) fn feuille_mood(&self, cx: &mut Context<Self>) -> Option<AnyElement> {
        let f = self.mood_feuille.clone()?;
        let (id, essai) = (f.id.clone(), f.essai);
        let ega = th().ega;
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
                    .rounded(coin(12.))
                    .flex()
                    .items_center()
                    .justify_center()
                    .bg(m.map(|m| m.envoye()).unwrap_or_else(degrade_accent))
                    .child(icone(if ega { "face-slightly-smiling" } else { "palette" }, 18., m.map(|m| m.texte_envoye()).unwrap_or(blanc(1.)))),
            )
            .child(
                div()
                    .flex_1()
                    .flex()
                    .flex_col()
                    .child(
                        div()
                            .text_size(px(18.))
                            .font_weight(FontWeight::BOLD)
                            .text_color(th().texte_fort)
                            .child(if ega { tr("Emoji rapide", "Quick emoji") } else { "Mood" }),
                    )
                    .child(
                        div().text_size(px(12.)).text_color(th().texte_pale).truncate().child(if ega {
                            nom_conv.clone()
                        } else if nom_conv.is_empty() {
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
                    .rounded(coin(999.))
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
        // La saisie en miniature : le bouton de l'emoji rapide essayé à droite
        // (champ vide), sinon celui d'envoi.
        let rond = |d: Div| d.size(px(34.)).flex_none().rounded(rayon(999.)).flex().items_center().justify_center();
        let mini_saisie = || {
            div()
            .flex()
            .flex_row()
            .items_center()
            .gap(px(8.))
            .child(rond(div()).bg(th().surface).border_1().border_color(th().surface_bord).child(icone("plus", 16., th().texte_fort)))
            .child(
                div()
                    .flex_1()
                    .h(px(36.))
                    .rounded(rayon(999.))
                    .bg(th().surface)
                    .border_1()
                    .border_color(th().surface_bord)
                    .flex()
                    .flex_row()
                    .items_center()
                    .px(px(14.))
                    .text_size(px(13.))
                    .text_color(th().texte_pale)
                    .child(div().flex_1().child("Message"))
                    .child(icone("face-slightly-smiling", 17., th().icone)),
            )
            .child(match f.emoji.as_deref() {
                Some(e) => rond(div())
                    .bg(th().surface)
                    .border_1()
                    .border_color(th().accent)
                    .shadow(ombre_flottante())
                    .child(emoji_centre(e, 17.)),
                None => rond(div()).bg(degrade_accent()).shadow(ombre_flottante()).child(icone("arrow-up", 16., blanc(1.))),
            })
        };
        // L'aperçu en direct : le fond choisi, trois bulles et la saisie, dans le thème actuel.
        let apercu = Self::fond_apercu(
            m,
            div().relative().w_full().h(px(if ega { 64. } else { 220. })).border_1().border_color(if th().sombre { blanc(0.06) } else { blanc(0.85) }),
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
                .child(div().flex().flex_row().when(!retro::retro(), |d| d.justify_end()).child(Self::exemple_bulle(
                    m,
                    tr("Oui ! Il était magnifique 😍", "Yes! It was gorgeous 😍"),
                    true,
                    14.,
                    false,
                )))
                .child(div().flex().flex_row().when(!retro::retro(), |d| d.justify_end()).child(Self::exemple_bulle(
                    m,
                    tr("On y retourne demain ?", "Back there tomorrow?"),
                    true,
                    14.,
                    true,
                )))
                .child(div().pt(px(8.)).child(mini_saisie())),
        );
        let apercu = if ega {
            div().w_full().p(px(14.)).border_1().border_color(th().trait_fin).child(mini_saisie())
        } else {
            apercu
        };
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
                .rounded(coin(999.))
                .text_size(px(14.))
                .font_weight(FontWeight::SEMIBOLD)
                .cursor_pointer()
                .when(plein, |d| d.bg(degrade_accent()).text_color(blanc(1.)).shadow(ombre_flottante()))
                .when(!plein, |d| d.bg(th().menu_champ).text_color(th().texte_fort))
                .hover(|s| s.opacity(0.88))
                .child(texte)
        };
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
                this.appliquer_feuille_mood();
                cx.notify();
            })));
        let emojis = self.rangee_emoji_rapide(&f, cx);
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
            // La feuille défile si la fenêtre est basse : ses parties gardent leur taille.
            .child(apercu.flex_none())
            .when(!ega, |d| d.child(grille.flex_none()))
            .child(div().flex_none().child(emojis))
            .when(f.autre, |d| d.child(div().flex().flex_row().justify_center().child(self.panneau_emojis(cx))))
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

/// Un coin de la feuille Mood : arrondi, sauf en Sirène (coins carrés).
fn coin(r: f32) -> gpui::Pixels {
    if th().sirene { px(0.) } else { px(r) }
}
