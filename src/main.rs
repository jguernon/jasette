//! Jasette — Google Messages (SMS + RCS) sur le bureau, et bientôt Messenger.
//!
//! L'interface (GPUI) parle au pont Go (`pont/`, libgm de mautrix-gmessages)
//! qu'elle lance comme processus enfant : des lignes JSON sur son entrée et
//! sa sortie standard (voir `pont.rs`). La connexion passe par le compte
//! Google (`connexion/jasette-connexion.py`) puis un emoji à toucher sur le
//! téléphone.
//!
//! Disposition à la Beeper : un rail de services à gauche (tout, archives,
//! un bouton par service, les réglages en bas), la liste des conversations,
//! puis le fil. Deux thèmes (réglages > Apparence, voir `Palette`) : « Sombre »,
//! plat à la Beeper (aplats gris, fil noir), le défaut ; et « Verre », le look
//! liquid glass du skill stageshow (fond pastel à halos, verre translucide).

mod pont;

use std::collections::{HashMap, HashSet};
use std::path::PathBuf;
use std::process::Child;
use std::sync::atomic::{AtomicU8, AtomicUsize, Ordering};

use std::sync::{Arc, LazyLock, Mutex};
use std::time::{Duration, Instant};

use gpui::{
    AnyElement, App, AppContext, BoxShadow, Bounds, Context, Div, Entity, FontWeight, Hsla, InteractiveElement,
    IntoElement, ObjectFit, ParentElement, PathPromptOptions, Render, ScrollHandle, SharedString,
    StatefulInteractiveElement, Styled, StyledImage, Svg, Window, WindowBounds, WindowOptions, div,
    hsla, img, linear_color_stop, linear_gradient, point, prelude::FluentBuilder, px, rgb, rgba, size, svg,
};
use gpui_component::Root;
use gpui_component::input::{Input, InputEvent, InputState};
use serde::{Deserialize, Serialize};
use serde_json::{Value, json};

use pont::Pont;

// ---- Les jetons du thème glass (stageshow, kit/stage.css) ----
const ACCENT: u32 = 0x3b82f6;
const ACCENT_2: u32 = 0x8b5cf6;
const PECHE: u32 = 0xffa878;
const FOND: u32 = 0xebe9f5; // --ss-well
const TEXTE_FORT: u32 = 0x262628;
const TEXTE: u32 = 0x6a6a72;
const TEXTE_PALE: u32 = 0x9a9a9d;
const SUCCES: u32 = 0x16a34a;
const DANGER: u32 = 0xdc2626;
const AVERTI: u32 = 0xd97706;
const ROSE: u32 = 0xec4899;

const POLICE: &str = "Adwaita Sans";

/// Hauteur de l'en-tête et de la barre d'envoi, qui flottent sur le fil.
const HAUT_ENTETE: f32 = 64.;
const HAUT_COMPOSEUR: f32 = 74.;
/// Le fil commence et finit à cette distance des bords du panneau.
const MARGE_FIL: f32 = 14.;

/// Le catalogue Unicode complet (emoji-test.txt, sans les variantes de
/// teinte) : groupe, emoji, nom anglais.
fn catalogue() -> &'static [(&'static str, &'static str, &'static str)] {
    static C: std::sync::OnceLock<Vec<(&'static str, &'static str, &'static str)>> = std::sync::OnceLock::new();
    C.get_or_init(|| {
        include_str!("../assets/emojis.tsv")
            .lines()
            .filter_map(|l| {
                let mut it = l.split('\t');
                Some((it.next()?, it.next()?, it.next()?))
            })
            .collect()
    })
}

const GROUPES_EMOJI: [(&str, &str); 10] = [
    ("Récents", "🕘"),
    ("Smileys", "😀"),
    ("Personnes", "👋"),
    ("Nature", "🐶"),
    ("Nourriture", "🍕"),
    ("Voyages", "✈️"),
    ("Activités", "⚽"),
    ("Objets", "💡"),
    ("Symboles", "❤️"),
    ("Drapeaux", "🏁"),
];

/// Noto Color Emoji renommée, avec un glyphe pour « m » (assets/polices) :
/// GPUI refuse comme police principale une police sans « m », et retombait
/// alors sur Adwaita Sans, qui a des versions noir et blanc de bien des emojis.
const POLICE_EMOJI: &str = "Jasette Emoji";

/// Un caractère qui s'affiche en emoji (Adwaita Sans a des versions noir et
/// blanc de plusieurs symboles : on force la police couleur pour eux).
fn est_emoji(c: char) -> bool {
    matches!(c as u32,
        0x1F000..=0x1FAFF | 0x2600..=0x27BF | 0x2300..=0x23FF | 0x2B00..=0x2BFF
        | 0x3030 | 0x303D | 0x3297 | 0x3299 | 0xFE0F | 0x200D | 0x20E3 | 0xE0020..=0xE007F)
}

/// Du texte dont les emojis passent par Noto Color Emoji.
fn texte_emoji(s: impl Into<SharedString>, couleur: Hsla, gras: Option<FontWeight>) -> gpui::StyledText {
    let s: SharedString = s.into();
    let mut runs = Vec::new();
    let mut debut = 0;
    let mut emoji_courant: Option<bool> = None;
    let run = |len: usize, emoji: bool| {
        let mut f = gpui::font(if emoji { POLICE_EMOJI } else { police() });
        if let Some(g) = gras {
            f.weight = g;
        }
        gpui::TextRun { len, font: f, color: couleur, background_color: None, underline: None, strikethrough: None }
    };
    for (i, ch) in s.char_indices() {
        // Un joint ou un sélecteur garde la police de ce qui précède.
        let e = if ch == '\u{200D}' || ch == '\u{FE0F}' { emoji_courant.unwrap_or(true) } else { est_emoji(ch) };
        match emoji_courant {
            Some(prec) if prec != e => {
                runs.push(run(i - debut, prec));
                debut = i;
            }
            _ => {}
        }
        emoji_courant = Some(e);
    }
    if let Some(e) = emoji_courant {
        runs.push(run(s.len() - debut, e));
    }
    gpui::StyledText::new(s).with_runs(runs)
}

fn c(hex: u32) -> Hsla {
    rgb(hex).into()
}
/// Blanc à l'opacité donnée.
fn blanc(a: f32) -> Hsla {
    hsla(0., 0., 1., a)
}
/// Une couleur à l'opacité donnée.
fn ca(hex: u32, a: f32) -> Hsla {
    let mut h: Hsla = rgb(hex).into();
    h.a = a;
    h
}
fn hex(s: &str) -> Option<u32> {
    u32::from_str_radix(s.trim_start_matches('#').get(..6)?, 16).ok()
}
/// Une icône Lucide (gpui-kit-assets).
fn icone(nom: &str, taille: f32, couleur: Hsla) -> Svg {
    svg().path(format!("icons/{nom}.svg")).size(px(taille)).flex_none().text_color(couleur)
}

// ---- Les trois thèmes : « Sombre » (plat, à la Beeper), « Verre » et « EGA » ----

/// Les thèmes, gardés dans le réglage `theme` par leur id.
#[derive(Clone, Copy, PartialEq, Eq)]
enum Theme {
    Sombre,
    Verre,
    /// L'écran 16 couleurs d'un PC des années 80, repris du thème EGA de
    /// Claude Desktop (claude-ega-theme) : fond noir, palette EGA, police
    /// IBM VGA 8x16, coins carrés, traits pleins, ombres portées dures.
    Ega,
}

impl Theme {
    fn id(self) -> &'static str {
        match self {
            Theme::Sombre => "sombre",
            Theme::Verre => "verre",
            Theme::Ega => "ega",
        }
    }
    /// Le thème d'un id des réglages ; sombre par défaut.
    fn depuis(id: &str) -> Self {
        match id {
            "verre" => Theme::Verre,
            "ega" => Theme::Ega,
            _ => Theme::Sombre,
        }
    }
}

/// Le thème choisi (`Theme` en u8) : sombre par défaut.
static THEME: AtomicU8 = AtomicU8::new(Theme::Sombre as u8);

fn theme() -> Theme {
    match THEME.load(Ordering::Relaxed) {
        x if x == Theme::Verre as u8 => Theme::Verre,
        x if x == Theme::Ega as u8 => Theme::Ega,
        _ => Theme::Sombre,
    }
}

// La palette EGA exacte (adaptateur IBM EGA, comme ega-theme.css).
const EGA_NOIR: u32 = 0x000000;
const EGA_BLEU: u32 = 0x0000aa;
const EGA_GRIS_CLAIR: u32 = 0xaaaaaa;
const EGA_GRIS: u32 = 0x555555;
const EGA_CYAN_VIF: u32 = 0x55ffff;
const EGA_VERT_VIF: u32 = 0x55ff55;
const EGA_ROUGE_VIF: u32 = 0xff5555;
const EGA_MAGENTA_VIF: u32 = 0xff55ff;
const EGA_JAUNE: u32 = 0xffff55;
const EGA_BLANC: u32 = 0xffffff;

/// Les polices du thème EGA, dans l'ordre de préférence (celles du thème de
/// Claude Desktop), puis des chasses fixes courantes en repli.
const POLICES_EGA: [&str; 8] = [
    "Px437 IBM VGA 8x16",
    "PxPlus IBM VGA 8x16",
    "Mx437 IBM VGA 8x16",
    "Px437 IBM EGA 8x14",
    "PxPlus IBM EGA 8x14",
    "DejaVu Sans Mono",
    "Noto Sans Mono",
    "JetBrains Mono",
];
/// La police EGA trouvée sur le système (choisie au premier `appliquer_theme`).
static POLICE_EGA: std::sync::OnceLock<&'static str> = std::sync::OnceLock::new();

/// La police du texte : Adwaita Sans, ou la police IBM du thème EGA.
fn police() -> &'static str {
    if th().ega { POLICE_EGA.get().copied().unwrap_or(POLICE) } else { POLICE }
}

/// Le rayon d'un coin : celui demandé, ou un coin carré en EGA.
fn rayon(r: f32) -> gpui::Pixels {
    if th().ega { px(0.) } else { px(r) }
}
/// La teinte du thème verre (indice dans `TEINTES`, réglage `teinte`).
static TEINTE: AtomicUsize = AtomicUsize::new(0);

/// Une teinte pastel du thème verre : son nom gardé dans les réglages, son
/// nom affiché, l'angle de son fond et de son givre, puis les trois couleurs
/// des halos (la première est l'accent, la deuxième finit le dégradé des
/// bulles envoyées). Les accents restent assez foncés pour du texte blanc.
struct Teinte {
    id: &'static str,
    nom: &'static str,
    angle: f32,
    accent: u32,
    accent_2: u32,
    halo: u32,
}

const TEINTES: [Teinte; 8] = [
    Teinte { id: "lavande", nom: "Lavande", angle: 250., accent: ACCENT, accent_2: ACCENT_2, halo: PECHE },
    Teinte { id: "rose", nom: "Rose", angle: 335., accent: 0xdb2f74, accent_2: 0xb03fd0, halo: 0xffb38a },
    Teinte { id: "peche", nom: "Pêche", angle: 20., accent: 0xe0552b, accent_2: 0xd93a6a, halo: 0xffd166 },
    Teinte { id: "citron", nom: "Citron", angle: 52., accent: 0xb07a00, accent_2: 0xd08a00, halo: 0x9be27a },
    Teinte { id: "menthe", nom: "Menthe", angle: 155., accent: 0x0b8f6a, accent_2: 0x14a3a0, halo: 0xb8e86a },
    Teinte { id: "ciel", nom: "Ciel", angle: 200., accent: 0x0a7fc4, accent_2: 0x1aa6c9, halo: 0x7be0c4 },
    Teinte { id: "lilas", nom: "Lilas", angle: 285., accent: 0x9333ea, accent_2: 0xc026d3, halo: 0xf9a8d4 },
    Teinte { id: "corail", nom: "Corail", angle: 5., accent: 0xd93b3b, accent_2: 0xe0613a, halo: 0xffc078 },
];

fn teinte() -> &'static Teinte {
    &TEINTES[TEINTE.load(Ordering::Relaxed).min(TEINTES.len() - 1)]
}

/// Les couleurs et mesures d'un thème. Le thème verre garde les valeurs
/// d'origine (blanc translucide sur halos) ; le sombre est plat et opaque.
/// L'EGA suit le sombre partout (`sombre` vrai : plat, opaque), puis
/// ajoute ses traits, ses coins carrés et sa police (`ega`).
struct Palette {
    sombre: bool,
    ega: bool,
    /// Le fond de la fenêtre.
    fond: Hsla,
    /// Le fond des trois panneaux : rail, liste (et menu des réglages), fil.
    rail: Hsla,
    liste: Hsla,
    fil: Hsla,
    rayon_rail: f32,
    rayon_liste: f32,
    rayon_fil: f32,
    /// Le retrait autour de la page, l'écart entre les colonnes, et la marge
    /// propre au panneau du fil (le sombre colle tout sauf le fil).
    marge: f32,
    ecart: f32,
    marge_fil: f32,
    /// La couleur des voiles (« givre ») : celle du panneau qu'ils recouvrent.
    givre_liste: Hsla,
    givre_fil: Hsla,
    /// Bulle reçue, pièce jointe, champ de la barre d'envoi.
    surface: Hsla,
    surface_bord: Hsla,
    /// Les champs de la liste (recherche, numéro) et la bannière.
    champ: Hsla,
    /// Rangée choisie, élément actif du rail ou des réglages.
    choisi: Hsla,
    survol: Hsla,
    /// Séparateurs fins.
    trait_fin: Hsla,
    /// Menus et popovers (emojis, avatar).
    menu: Hsla,
    /// Un champ ou un bouton posé sur un menu.
    menu_champ: Hsla,
    /// Les cartes des réglages.
    carte: Hsla,
    texte_fort: Hsla,
    texte: Hsla,
    texte_pale: Hsla,
    /// L'aperçu d'un message dans la liste.
    apercu: Hsla,
    /// Les icônes discrètes (emoji, micro, rail).
    icone: Hsla,
    accent: Hsla,
    /// Le nom de l'expéditeur dans un groupe.
    expediteur: Hsla,
    jour_fond: Hsla,
    jour_texte: Hsla,
    /// L'heure dans une bulle (sombre seulement) : reçue, envoyée.
    heure_recue: Hsla,
    heure_envoyee: Hsla,
    bord_avatar: Hsla,
    danger: Hsla,
    succes: Hsla,
}

impl Palette {
    fn verre(t: &Teinte) -> Self {
        let givre = hsla(t.angle / 360., 0.4, 0.95, 1.);
        Palette {
            sombre: false,
            ega: false,
            // La lavande garde son fond d'origine (--ss-well).
            fond: if t.id == "lavande" { c(FOND) } else { hsla(t.angle / 360., 0.4, 0.935, 1.) },
            rail: blanc(0.5),
            liste: blanc(0.5),
            fil: blanc(0.42),
            rayon_rail: 26.,
            rayon_liste: 26.,
            rayon_fil: 26.,
            marge: 16.,
            ecart: 14.,
            marge_fil: 0.,
            givre_liste: givre,
            givre_fil: givre,
            surface: blanc(0.85),
            surface_bord: blanc(0.95),
            champ: blanc(0.7),
            choisi: blanc(0.82),
            survol: blanc(0.45),
            trait_fin: blanc(0.55),
            menu: blanc(0.94),
            menu_champ: blanc(0.8),
            carte: blanc(0.72),
            texte_fort: c(TEXTE_FORT),
            texte: c(TEXTE),
            texte_pale: c(TEXTE_PALE),
            apercu: c(TEXTE),
            icone: c(TEXTE),
            accent: c(t.accent),
            expediteur: c(t.accent_2),
            jour_fond: blanc(0.5),
            jour_texte: c(TEXTE),
            heure_recue: c(TEXTE_PALE),
            heure_envoyee: blanc(0.8),
            bord_avatar: blanc(0.8),
            danger: c(DANGER),
            succes: c(SUCCES),
        }
    }
    fn sombre() -> Self {
        Palette {
            sombre: true,
            ega: false,
            fond: c(0x1c1c1e),
            rail: c(0x1c1c1e),
            liste: c(0x252527),
            fil: c(0x000000),
            rayon_rail: 0.,
            rayon_liste: 0.,
            rayon_fil: 12.,
            marge: 0.,
            ecart: 0.,
            marge_fil: 8.,
            givre_liste: c(0x252527),
            givre_fil: c(0x000000),
            surface: c(0x2c2c2e),
            surface_bord: hsla(0., 0., 0., 0.),
            champ: c(0x333336),
            choisi: c(0x3a3a3c),
            survol: c(0x2e2e30),
            trait_fin: c(0x333335),
            menu: c(0x2c2c2e),
            menu_champ: c(0x3a3a3c),
            carte: c(0x1c1c1e),
            texte_fort: c(0xffffff),
            texte: c(0xc7c7cc),
            texte_pale: c(0x8e8e93),
            apercu: c(0xa0a0a5),
            icone: c(0xa0a0a5),
            accent: c(0x2f6bff),
            expediteur: c(0xf08a6c),
            jour_fond: c(0x2c2c2e),
            jour_texte: c(0xffffff),
            heure_recue: c(0x8e8e93),
            heure_envoyee: blanc(0.7),
            bord_avatar: hsla(0., 0., 0., 0.),
            danger: c(0xff453a),
            succes: c(0x30d158),
        }
    }
    /// L'EGA de Claude Desktop (ega-theme.css) : tout sur fond noir, texte
    /// gris clair, titres et noms en jaune, sélection blanc sur bleu, traits
    /// gris (#555555) ou gris clair (#AAAAAA), accent cyan vif.
    fn ega() -> Self {
        Palette {
            sombre: true,
            ega: true,
            fond: c(EGA_NOIR),
            rail: c(EGA_NOIR),
            liste: c(EGA_NOIR),
            fil: c(EGA_NOIR),
            rayon_rail: 0.,
            rayon_liste: 0.,
            rayon_fil: 0.,
            // Trois fenêtres cadrées, séparées par un filet de noir.
            marge: 4.,
            ecart: 4.,
            marge_fil: 0.,
            givre_liste: c(EGA_NOIR),
            givre_fil: c(EGA_NOIR),
            surface: c(EGA_NOIR),
            surface_bord: c(EGA_GRIS_CLAIR),
            champ: c(EGA_NOIR),
            choisi: c(EGA_BLEU),
            survol: c(EGA_GRIS),
            trait_fin: c(EGA_GRIS),
            menu: c(EGA_NOIR),
            menu_champ: c(EGA_BLEU),
            carte: c(EGA_NOIR),
            texte_fort: c(EGA_BLANC),
            texte: c(EGA_GRIS_CLAIR),
            texte_pale: c(EGA_GRIS_CLAIR),
            apercu: c(EGA_GRIS_CLAIR),
            icone: c(EGA_GRIS_CLAIR),
            accent: c(EGA_CYAN_VIF),
            expediteur: c(EGA_JAUNE),
            jour_fond: c(EGA_NOIR),
            jour_texte: c(EGA_JAUNE),
            heure_recue: c(EGA_GRIS),
            heure_envoyee: c(EGA_GRIS_CLAIR),
            bord_avatar: hsla(0., 0., 0., 0.),
            danger: c(EGA_ROUGE_VIF),
            succes: c(EGA_VERT_VIF),
        }
    }
}

/// La palette du thème en cours.
fn th() -> &'static Palette {
    static VERRES: LazyLock<Vec<Palette>> = LazyLock::new(|| TEINTES.iter().map(Palette::verre).collect());
    static SOMBRE: LazyLock<Palette> = LazyLock::new(Palette::sombre);
    static EGA: LazyLock<Palette> = LazyLock::new(Palette::ega);
    match theme() {
        Theme::Sombre => &SOMBRE,
        Theme::Ega => &EGA,
        Theme::Verre => &VERRES[TEINTE.load(Ordering::Relaxed).min(TEINTES.len() - 1)],
    }
}

/// Change de thème (et de teinte du verre, par son id), y compris les
/// couleurs et la police des champs de gpui-component.
fn appliquer_theme(choix: Theme, teinte_id: &str, cx: &mut App) {
    THEME.store(choix as u8, Ordering::Relaxed);
    TEINTE.store(TEINTES.iter().position(|t| t.id == teinte_id).unwrap_or(0), Ordering::Relaxed);
    if choix == Theme::Ega {
        POLICE_EGA.get_or_init(|| {
            let dispo = cx.text_system().all_font_names();
            POLICES_EGA.into_iter().find(|p| dispo.iter().any(|d| d == p)).unwrap_or(POLICE)
        });
    }
    let t = th();
    gpui_component::Theme::update(cx, |g| {
        g.font_family = police().into();
        g.foreground = t.texte_fort;
        // Le filet autour de la fenêtre (décorations côté client) : la couleur du rail.
        g.window_border = t.rail;
        g.muted_foreground = t.texte_pale;
        if t.ega {
            // Comme ega-theme.css : curseur jaune, sélection blanc sur bleu,
            // texte indicatif en gris foncé.
            g.caret = c(EGA_JAUNE);
            g.selection = c(EGA_BLEU);
            g.muted_foreground = c(EGA_GRIS);
        } else {
            g.caret = t.accent;
            g.selection = alpha(t.accent, if t.sombre { 0.4 } else { 0.25 });
        }
    });
}

/// Les 16 couleurs de l'adaptateur EGA.
const EGA_16: [u32; 16] = [
    0x000000, 0x0000aa, 0x00aa00, 0x00aaaa, 0xaa0000, 0xaa00aa, 0xaa5500, 0xaaaaaa,
    0x555555, 0x5555ff, 0x55ff55, 0x55ffff, 0xff5555, 0xff55ff, 0xffff55, 0xffffff,
];

/// La couleur de `parmi` la plus proche de `h` (distance RVB), opacité gardée.
fn plus_proche(h: Hsla, parmi: &[u32]) -> Hsla {
    let r: gpui::Rgba = h.into();
    let d = |x: u32| {
        let canal = |s: u32| ((x >> s) & 0xff) as f32 / 255.;
        (canal(16) - r.r).powi(2) + (canal(8) - r.g).powi(2) + (canal(0) - r.b).powi(2)
    };
    let x = parmi.iter().copied().min_by(|a, b| d(*a).total_cmp(&d(*b))).unwrap_or(0);
    ca(x, h.a)
}

/// En EGA, une couleur ramenée à la palette 16 couleurs (comme le filtre
/// ega-snap de Claude). Un gris foncé qui n'est pas du noir (un survol, un
/// bouton du thème sombre) devient le gris EGA plutôt que de disparaître.
/// Les autres thèmes gardent la couleur telle quelle.
fn ega_snap(h: Hsla) -> Hsla {
    if !th().ega {
        return h;
    }
    let s = plus_proche(h, &EGA_16);
    if s.l == 0. && h.l > 0.12 { plus_proche(h, &EGA_16[1..]) } else { s }
}

/// En EGA, un fond plein qui porte du texte blanc (avatar, pastille) : la
/// couleur EGA foncée la plus proche.
fn ega_fond(h: Hsla) -> Hsla {
    plus_proche(h, &[0x0000aa, 0x00aa00, 0x00aaaa, 0xaa0000, 0xaa00aa, 0xaa5500, 0x555555])
}

/// Le dégradé en biais d'un avatar ; en EGA, un aplat EGA foncé (pas de dégradés).
fn fond_avatar(de: Hsla, a: Hsla) -> gpui::Background {
    if th().ega {
        return ega_fond(a).into();
    }
    linear_gradient(150., linear_color_stop(de, 0.), linear_color_stop(a, 1.))
}

/// L'ombre portée EGA : un bloc gris décalé, sans flou.
fn ombre_dure(decalage: f32) -> Vec<BoxShadow> {
    vec![BoxShadow {
        color: c(EGA_GRIS),
        offset: point(px(decalage), px(decalage)),
        blur_radius: px(0.),
        spread_radius: px(0.),
        inset: false,
    }]
}

/// La tache floue d'une bulle envoyée, sous l'en-tête : le milieu de son dégradé.
fn tache_envoyee() -> Hsla {
    if th().sombre {
        return alpha(th().accent, 0.7);
    }
    let (a, b) = (teinte().accent, teinte().accent_2);
    let milieu = |d: u32| ((((a >> d) & 0xff) + ((b >> d) & 0xff)) / 2) << d;
    ca(milieu(16) | milieu(8) | milieu(0), 0.6)
}

/// La couleur du thème verre, ou celle du sombre (ramenée à la palette EGA
/// dans le thème EGA).
fn ton(verre: Hsla, sombre: Hsla) -> Hsla {
    if th().sombre { ega_snap(sombre) } else { verre }
}

/// Une mesure (ou une opacité) du thème verre, ou celle du sombre.
fn mesure(verre: f32, sombre: f32) -> f32 {
    if th().sombre { sombre } else { verre }
}

/// Une couleur Hsla à une autre opacité.
fn alpha(mut h: Hsla, a: f32) -> Hsla {
    h.a = a;
    h
}

/// Un grand panneau (rail, liste, fil) : verre flottant à ombre, aplat
/// sombre, ou fenêtre EGA noire cadrée de gris clair.
fn panneau(fond: Hsla, r: f32) -> Div {
    if th().ega {
        div().bg(fond).border_1().border_color(c(EGA_GRIS_CLAIR))
    } else if th().sombre {
        div().bg(fond).rounded(rayon(r))
    } else {
        div().bg(fond).border_1().border_color(blanc(0.75)).rounded(rayon(r)).shadow(ombre_verre())
    }
}

/// L'ombre des fenêtres de verre : 0 20px 50px -14px rgba(40,40,90,.28).
/// Le thème sombre est plat : pas d'ombre. L'EGA : l'ombre portée dure de
/// ses dialogues et menus (6 px, gris).
fn ombre_verre() -> Vec<BoxShadow> {
    if th().ega {
        return ombre_dure(6.);
    }
    if th().sombre {
        return Vec::new();
    }
    vec![BoxShadow {
        color: rgba(0x28285a47).into(),
        offset: point(px(0.), px(20.)),
        blur_radius: px(50.),
        spread_radius: px(-14.),
        inset: false,
    }]
}
/// L'ombre des éléments posés (bulles, boutons, rangée choisie) ; en EGA,
/// une petite ombre portée dure.
fn ombre_flottante() -> Vec<BoxShadow> {
    if th().ega {
        return ombre_dure(3.);
    }
    if th().sombre {
        return Vec::new();
    }
    vec![BoxShadow {
        color: rgba(0x28285a38).into(),
        offset: point(px(0.), px(10.)),
        blur_radius: px(28.),
        spread_radius: px(-8.),
        inset: false,
    }]
}
/// Un panneau de verre : blanc translucide, liseré clair, coins larges.
/// En sombre : un aplat gris (`surface`), sans liseré. En EGA : un aplat
/// noir à trait plein gris, coins carrés.
fn verre(opacite: f32, r: f32) -> Div {
    if th().ega {
        return div().bg(th().surface).border_1().border_color(th().trait_fin);
    }
    if th().sombre {
        return div().bg(th().surface).rounded(rayon(r));
    }
    div().bg(blanc(opacite)).border_1().border_color(blanc(0.75)).rounded(rayon(r))
}
/// Le dégradé bleu-violet du verre ; en sombre, le bleu plat ; en EGA, le
/// bleu EGA (#0000AA) des remplissages de Claude.
fn degrade_accent() -> gpui::Background {
    if th().ega {
        return c(EGA_BLEU).into();
    }
    if th().sombre {
        return th().accent.into();
    }
    linear_gradient(135., linear_color_stop(c(teinte().accent), 0.), linear_color_stop(c(teinte().accent_2), 1.))
}

// ---- Les services ----

#[derive(Clone, Copy, PartialEq, Eq)]
enum Service {
    GoogleMessages,
    Messenger,
}

impl Service {
    fn nom(self) -> &'static str {
        match self {
            Service::GoogleMessages => "Google Messages",
            Service::Messenger => "Facebook Messenger",
        }
    }
}

/// Les icônes officielles des services (Play Store), dans l'exécutable.
///
/// GPUI réduit mal une grande image : chaque taille d'affichage prend la
/// version déjà réduite (Lanczos) juste au-dessus de sa taille réelle à
/// l'écran (×2 sur un écran HiDPI).
fn image_service(s: Service, taille: f32) -> Arc<gpui::Image> {
    macro_rules! variantes {
        ($nom:literal) => {
            [
                (40., include_bytes!(concat!("../assets/services/", $nom, "-40.png")).as_slice()),
                (64., include_bytes!(concat!("../assets/services/", $nom, "-64.png")).as_slice()),
                (96., include_bytes!(concat!("../assets/services/", $nom, "-96.png")).as_slice()),
                (128., include_bytes!(concat!("../assets/services/", $nom, "-128.png")).as_slice()),
            ]
        };
    }
    static CACHE: std::sync::OnceLock<Mutex<HashMap<(u8, usize), Arc<gpui::Image>>>> = std::sync::OnceLock::new();
    let (cle, liste) = match s {
        Service::GoogleMessages => (0u8, variantes!("google-messages")),
        Service::Messenger => (1u8, variantes!("messenger")),
    };
    let voulu = taille * 2.;
    let i = liste.iter().position(|(t, _)| *t >= voulu).unwrap_or(liste.len() - 1);
    let mut cache = CACHE.get_or_init(Default::default).lock().unwrap_or_else(|e| e.into_inner());
    cache
        .entry((cle, i))
        .or_insert_with(|| Arc::new(gpui::Image::from_bytes(gpui::ImageFormat::Png, liste[i].1.to_vec())))
        .clone()
}

/// La tuile d'un service : son icône officielle sur un carré blanc arrondi,
/// comme dans le Play Store. L'image est arrondie elle aussi : GPUI ne
/// découpe pas les enfants selon les coins de leur parent.
fn logo(s: Service, taille: f32) -> Div {
    let rayon = px(taille * 0.3);
    div()
        .flex_none()
        .size(px(taille))
        .rounded(rayon)
        .bg(blanc(1.))
        .border_1()
        .border_color(ton(rgba(0x0000001a).into(), hsla(0., 0., 0., 0.)))
        .child(img(image_service(s, taille)).size_full().rounded(rayon).object_fit(ObjectFit::Contain))
}

// ---- Les données ----

#[derive(Clone, Debug, Deserialize)]
struct Conv {
    id: String,
    name: String,
    #[serde(default)]
    preview: String,
    #[serde(default)]
    from_me: bool,
    #[serde(default)]
    time: i64,
    #[serde(default)]
    unread: bool,
    #[serde(default)]
    group: bool,
    #[serde(default)]
    rcs: bool,
    #[serde(default)]
    color: String,
    #[serde(default)]
    members: Vec<String>,
    #[serde(default)]
    read_only: bool,
    #[serde(default)]
    archived: bool,
}

#[derive(Clone, Debug, Deserialize)]
struct Piece {
    media_id: String,
    /// La clé de déchiffrement, en base64, renvoyée telle quelle au pont.
    #[serde(default)]
    key: String,
    #[serde(default)]
    mime: String,
    #[serde(default)]
    name: String,
    #[serde(default)]
    size: i64,
    #[serde(default)]
    w: i64,
    #[serde(default)]
    h: i64,
}

impl Piece {
    fn est_image(&self) -> bool {
        self.mime.starts_with("image/")
    }
    fn est_audio(&self) -> bool {
        self.mime.starts_with("audio/")
    }
    /// La taille d'affichage : au plus 300 × 340, proportions gardées.
    fn cadre(&self) -> (f32, f32) {
        if self.w <= 0 || self.h <= 0 {
            return (260., 200.);
        }
        let (w, h) = (self.w as f32, self.h as f32);
        let k = (300. / w).min(340. / h).min(1.);
        ((w * k).max(80.), (h * k).max(60.))
    }
}

#[derive(Clone, Debug, Deserialize)]
struct Msg {
    id: String,
    conv: String,
    #[serde(default)]
    text: String,
    #[serde(default)]
    media: String,
    #[serde(default)]
    from_me: bool,
    #[serde(default)]
    sender: String,
    #[serde(default)]
    time: i64,
    #[serde(default)]
    status: String,
    #[serde(default)]
    kind: String,
    #[serde(default)]
    tmp_id: String,
    #[serde(default)]
    attachments: Vec<Piece>,
}

#[derive(Clone, Copy, PartialEq, Eq)]
enum Etat {
    Demarrage,
    NonAssocie,
    Association,
    Connexion,
    Pret,
    TelephoneHorsLigne,
    Reconnexion,
}

impl Etat {
    fn texte(self) -> &'static str {
        match self {
            Etat::Demarrage => "Démarrage",
            Etat::NonAssocie => "Non connecté",
            Etat::Association => "Association",
            Etat::Connexion => "Connexion…",
            Etat::Pret => "Connecté",
            Etat::TelephoneHorsLigne => "Téléphone hors ligne",
            Etat::Reconnexion => "Reconnexion…",
        }
    }
    fn couleur(self) -> u32 {
        match self {
            Etat::Pret => SUCCES,
            Etat::TelephoneHorsLigne | Etat::NonAssocie => DANGER,
            _ => AVERTI,
        }
    }
    fn associe(self) -> bool {
        matches!(self, Etat::Connexion | Etat::Pret | Etat::TelephoneHorsLigne | Etat::Reconnexion)
    }
}

/// Ce que montre la liste.
#[derive(Clone, Copy, PartialEq, Eq)]
enum Vue {
    Tout,
    Favoris,
    Archives,
    Service(Service),
}

#[derive(Clone, Copy, PartialEq, Eq)]
enum Section {
    Comptes,
    Notifications,
    Apparence,
    Langue,
    APropos,
}

/// Les réglages gardés entre deux lancements.
#[derive(Clone, Serialize, Deserialize)]
struct Reglages {
    #[serde(default = "vrai")]
    notifications: bool,
    #[serde(default = "vrai")]
    apercu: bool,
    #[serde(default)]
    email: Option<String>,
    #[serde(default)]
    emojis_recents: Vec<String>,
    /// D'où vient mon avatar : "initiale", "google", "messenger" ou "fichier".
    #[serde(default)]
    avatar_source: String,
    #[serde(default)]
    avatar_fichier: Option<PathBuf>,
    /// "fr" (ou vide) ou "en".
    #[serde(default)]
    langue: String,
    /// Le « Head » : la grille d'avatars en tête de liste (ids, préfixés pour
    /// Messenger), dans l'ordre d'ajout. Gardé sous l'ancien nom « favoris ».
    #[serde(default, rename = "favoris")]
    head: Vec<String>,
    /// Les favoris : la vue de l'étoile, dans le rail.
    #[serde(default)]
    etoiles: Vec<String>,
    /// Les conversations épinglées en haut de la liste, dans l'ordre d'épinglage.
    #[serde(default)]
    epingles: Vec<String>,
    /// Le thème : "sombre" (ou vide, le défaut) ou "verre".
    #[serde(default)]
    theme: String,
    /// La teinte du verre (id dans `TEINTES`) ; vide = lavande.
    #[serde(default)]
    teinte: String,
}

fn vrai() -> bool {
    true
}

impl Default for Reglages {
    fn default() -> Self {
        Self {
            notifications: true,
            apercu: true,
            email: None,
            emojis_recents: Vec::new(),
            avatar_source: String::new(),
            avatar_fichier: None,
            theme: String::new(),
            teinte: String::new(),
            langue: String::new(),
            head: Vec::new(),
            etoiles: Vec::new(),
            epingles: Vec::new(),
        }
    }
}

fn dossier_donnees() -> PathBuf {
    if let Some(d) = std::env::var_os("JASETTE_DATA") {
        return PathBuf::from(d);
    }
    let base = std::env::var_os("XDG_DATA_HOME")
        .map(PathBuf::from)
        .unwrap_or_else(|| PathBuf::from(std::env::var_os("HOME").unwrap_or_default()).join(".local/share"));
    base.join("jasette")
}

/// Photos téléchargées et messages vocaux : comme le pont (voir `pont.rs`).
fn dossier_medias() -> PathBuf {
    if let Some(d) = std::env::var_os("JASETTE_MEDIA") {
        return PathBuf::from(d);
    }
    let projet = PathBuf::from(env!("CARGO_MANIFEST_DIR")).join(".cache");
    if projet.is_dir() { projet.join("media") } else { dossier_donnees().join("media") }
}

impl Reglages {
    fn charger() -> Self {
        std::fs::read_to_string(dossier_donnees().join("reglages.json"))
            .ok()
            .and_then(|s| serde_json::from_str(&s).ok())
            .unwrap_or_default()
    }
    fn garder(&self) {
        let dir = dossier_donnees();
        let _ = std::fs::create_dir_all(&dir);
        if let Ok(s) = serde_json::to_string_pretty(self) {
            let tmp = dir.join("reglages.json.tmp");
            if std::fs::write(&tmp, s).is_ok() {
                let _ = std::fs::rename(tmp, dir.join("reglages.json"));
            }
        }
    }
}

/// Un résultat rempli par un fil à part, lu par la boucle de l'interface.
type Boite<T> = Arc<Mutex<Option<T>>>;

/// Un vocal en lecture : ffplay sans fenêtre, parti de `depuis` secondes.
struct Lecture {
    media_id: String,
    enfant: Child,
    debut: Instant,
    depuis: f32,
}

impl Lecture {
    fn position(&self) -> f32 {
        self.depuis + self.debut.elapsed().as_secs_f32()
    }
}

/// Nombre de barres de la forme d'onde d'un vocal.
const BARRES: usize = 34;

/// La forme d'onde et la durée d'un fichier audio (ffmpeg, 4 kHz mono).
fn calculer_onde(chemin: &std::path::Path) -> Option<(Vec<f32>, f32)> {
    let sortie = std::process::Command::new("ffmpeg")
        .args(["-v", "quiet", "-i"])
        .arg(chemin)
        .args(["-ac", "1", "-ar", "4000", "-f", "s16le", "-"])
        .stdin(std::process::Stdio::null())
        .stderr(std::process::Stdio::null())
        .output()
        .ok()?;
    let ech: Vec<f32> =
        sortie.stdout.chunks_exact(2).map(|b| i16::from_le_bytes([b[0], b[1]]) as f32 / 32768.).collect();
    if ech.is_empty() {
        return None;
    }
    let duree = ech.len() as f32 / 4000.;
    let par = ech.len().div_ceil(BARRES).max(1);
    let mut barres: Vec<f32> =
        ech.chunks(par).map(|ch| (ch.iter().map(|x| x * x).sum::<f32>() / ch.len() as f32).sqrt()).collect();
    let max = barres.iter().cloned().fold(0.0001, f32::max);
    for b in &mut barres {
        *b = (*b / max).powf(0.7).max(0.08);
    }
    barres.resize(BARRES, 0.08);
    Some((barres, duree))
}

/// Un message vocal en cours d'enregistrement.
struct Vocal {
    enfant: Child,
    fichier: PathBuf,
    debut: Instant,
}

struct Jasette {
    pont: Option<Pont>,
    etat: Etat,
    qr: Option<qrcode::QrCode>,
    emoji: Option<String>,
    connexion: Boite<Result<Value, String>>,
    connexion_ouverte: bool,
    /// Messenger : son pont, son état, sa fenêtre de connexion, son NIP d'historique.
    pont_meta: Option<Pont>,
    etat_meta: Etat,
    nom_meta: Option<String>,
    avatar_messenger: Option<PathBuf>,
    connexion_meta: Arc<Mutex<Option<Result<Value, String>>>>,
    connexion_meta_ouverte: bool,
    besoin_nip: bool,
    nip: Entity<InputState>,
    convs: Vec<Conv>,
    archives: Vec<Conv>,
    archives_chargees: bool,
    choisie: Option<String>,
    messages: HashMap<String, Vec<Msg>>,
    plus_anciens: HashMap<String, bool>,
    chargement: Option<String>,
    erreur: Option<String>,
    // La liste
    vue: Vue,
    non_lus_seulement: bool,
    recherche_ouverte: bool,
    nouveau_ouvert: bool,
    banniere_fermee: bool,
    recherche: Entity<InputState>,
    numero: Entity<InputState>,
    filtre: String,
    // Le fil
    saisie: Entity<InputState>,
    defile: ScrollHandle,
    descendre: bool,
    envoi_seq: u64,
    emojis_ouverts: bool,
    /// Le choix de l'avatar « Moi » est ouvert ; la photo que Google Messages connaît.
    menu_avatar: bool,
    /// Le menu « … » de la conversation ouverte, et la confirmation de suppression.
    menu_conv: bool,
    confirmer_suppression: bool,
    /// Le champ « nouveau message » et les personnes que Messenger propose (id préfixé, nom, photo).
    nouveau_texte: String,
    resultats_meta: Vec<(String, String)>,
    /// La galerie : fermée (None), tous les comptes (Some(None)) ou une conversation.
    galerie: Option<Option<String>>,
    galerie_onglet: u8,
    galerie_chargee: bool,
    /// Une photo ouverte en grand.
    apercu: Option<PathBuf>,
    /// Le (i) de l'en-tête : l'heure exacte sous chaque message.
    details_heures: bool,
    avatar_google: Option<PathBuf>,
    emoji_groupe: &'static str,
    emoji_recherche: Entity<InputState>,
    emoji_filtre: String,
    indication: String,
    vocal: Option<Vocal>,
    /// Le vocal en train de jouer, et les formes d'onde déjà calculées
    /// (id → barres de 0 à 1, durée en secondes).
    lecture: Option<Lecture>,
    ondes: HashMap<String, (Vec<f32>, f32)>,
    ondes_demandees: HashSet<String>,
    /// Où chaque vocal a été mis en pause.
    pauses: HashMap<String, f32>,
    ondes_pretes: Arc<Mutex<Vec<(String, Vec<f32>, f32)>>>,
    // Pièces jointes : téléchargées (id → fichier), demandées, en échec, à ouvrir dès l'arrivée.
    medias: HashMap<String, PathBuf>,
    demandes: HashSet<String>,
    echecs: HashSet<String>,
    ouvrir_apres: HashSet<String>,
    avatars: HashMap<String, PathBuf>,
    /// Les membres d'un groupe sans photo (nom, couleur, photo), pour la mosaïque.
    mosaiques: HashMap<String, Vec<(String, String, Option<PathBuf>)>>,
    // Réglages
    reglages: Reglages,
    section: Option<Section>,
    ajout_compte: bool,
    /// La fenêtre est-elle au premier plan (sinon : notifications) ?
    active: bool,
    /// La conversation d'une notification cliquée.
    a_ouvrir: Boite<String>,
}

/// Les ids venant de Messenger portent le préfixe « m: » dans l'interface :
/// ils ne peuvent pas se confondre avec ceux de Google, et ils disent à
/// quel pont envoyer une commande.
const PREFIXE_META: &str = "m:";

/// Un avatar recadré en carré (au centre) : GPUI arrondit l'image selon sa
/// propre boîte, et une photo en hauteur dépasserait du rond.
fn carre(chemin: PathBuf) -> PathBuf {
    let Ok(img) = image::open(&chemin) else { return chemin };
    let (w, h) = (img.width(), img.height());
    if w == h {
        return chemin;
    }
    let cote = w.min(h);
    let sortie = chemin.with_file_name(format!(
        "{}-carre.png",
        chemin.file_stem().map(|s| s.to_string_lossy().to_string()).unwrap_or_default()
    ));
    if sortie.is_file() {
        return sortie;
    }
    let rogne = img.crop_imm((w - cote) / 2, (h - cote) / 2, cote, cote).resize(256.min(cote), 256.min(cote), image::imageops::FilterType::Lanczos3);
    match rogne.save(&sortie) {
        Ok(()) => sortie,
        Err(_) => chemin,
    }
}

/// L'icône de Jasette : petite pour la barre de titre, grande pour l'accueil.
fn icone_app() -> Arc<gpui::Image> {
    static I: std::sync::OnceLock<Arc<gpui::Image>> = std::sync::OnceLock::new();
    I.get_or_init(|| {
        Arc::new(gpui::Image::from_bytes(gpui::ImageFormat::Png, include_bytes!("../assets/png/jasette-48.png").to_vec()))
    })
    .clone()
}

fn icone_app_grande() -> Arc<gpui::Image> {
    static I: std::sync::OnceLock<Arc<gpui::Image>> = std::sync::OnceLock::new();
    I.get_or_init(|| {
        Arc::new(gpui::Image::from_bytes(gpui::ImageFormat::Png, include_bytes!("../assets/png/jasette-256.png").to_vec()))
    })
    .clone()
}

/// Un numéro de téléphone (chiffres, espaces, +, -, parenthèses) ?
fn est_numero(s: &str) -> bool {
    let chiffres = s.chars().filter(char::is_ascii_digit).count();
    chiffres >= 3 && s.chars().all(|c| c.is_ascii_digit() || " +-().".contains(c))
}

fn service_de(id: &str) -> Service {
    if id.starts_with(PREFIXE_META) { Service::Messenger } else { Service::GoogleMessages }
}

/// Ajoute le préfixe Messenger aux ids d'une nouvelle du pont Messenger.
fn prefixer_meta(v: &mut Value) {
    fn ids(v: &mut Value, dans_item: bool) {
        match v {
            Value::Object(o) => {
                for (k, x) in o.iter_mut() {
                    let cle = matches!(k.as_str(), "conv" | "media_id") || (dans_item && k == "id");
                    match x {
                        Value::String(s) if cle && !s.is_empty() && !s.starts_with(PREFIXE_META) => {
                            *s = format!("{PREFIXE_META}{s}");
                        }
                        _ => ids(x, matches!(k.as_str(), "item" | "items")),
                    }
                }
            }
            Value::Array(a) => {
                for x in a {
                    ids(x, dans_item);
                }
            }
            _ => {}
        }
    }
    ids(v, false);
    v["_meta"] = Value::Bool(true);
}

/// Une commande pour le pont Messenger ? (et on retire alors les préfixes).
fn deprefixer_meta(v: &mut Value) -> bool {
    let mut meta = false;
    if let Value::Object(o) = v {
        for (k, x) in o.iter_mut() {
            if matches!(k.as_str(), "conv" | "media_id" | "msg" | "id")
                && let Value::String(s) = x
                && let Some(reste) = s.strip_prefix(PREFIXE_META)
            {
                *s = reste.to_string();
                meta = true;
            }
        }
    }
    meta
}

fn script_connexion_meta() -> Option<PathBuf> {
    let mut essais = Vec::new();
    if let Ok(exe) = std::env::current_exe()
        && let Some(dir) = exe.parent()
    {
        essais.push(dir.join("jasette-connexion-meta.py"));
    }
    essais.push(PathBuf::from(env!("CARGO_MANIFEST_DIR")).join("connexion/jasette-connexion-meta.py"));
    essais.into_iter().find(|p| p.is_file())
}

fn script_connexion() -> Option<PathBuf> {
    if let Some(p) = std::env::var_os("JASETTE_CONNEXION") {
        return Some(PathBuf::from(p));
    }
    let mut essais = Vec::new();
    if let Ok(exe) = std::env::current_exe()
        && let Some(dir) = exe.parent()
    {
        essais.push(dir.join("jasette-connexion.py"));
    }
    essais.push(PathBuf::from(env!("CARGO_MANIFEST_DIR")).join("connexion/jasette-connexion.py"));
    essais.into_iter().find(|p| p.is_file())
}

/// Ouvre un fichier avec l'application par défaut.
fn ouvrir_fichier(p: &PathBuf) {
    let _ = std::process::Command::new("xdg-open")
        .arg(p)
        .stdin(std::process::Stdio::null())
        .stdout(std::process::Stdio::null())
        .stderr(std::process::Stdio::null())
        .spawn();
}

/// Sans accents ni majuscules, pour la recherche.
fn plier(s: &str) -> String {
    s.chars()
        .flat_map(char::to_lowercase)
        .map(|c| match c {
            'à' | 'â' | 'ä' | 'á' => 'a',
            'é' | 'è' | 'ê' | 'ë' => 'e',
            'î' | 'ï' | 'í' => 'i',
            'ô' | 'ö' | 'ó' => 'o',
            'ù' | 'û' | 'ü' | 'ú' => 'u',
            'ç' => 'c',
            c => c,
        })
        .collect()
}

impl Jasette {
    fn new(window: &mut Window, cx: &mut Context<Self>) -> Self {
        let saisie = cx.new(|cx| InputState::new(window, cx).placeholder("Message"));
        cx.subscribe_in(&saisie, window, |this, _state, ev: &InputEvent, window, cx| {
            if let InputEvent::PressEnter { shift: false, .. } = ev {
                this.envoyer(window, cx);
            }
            // Le bouton d'envoi du thème sombre s'allume dès qu'il y a du texte.
            if matches!(ev, InputEvent::Change) && th().sombre {
                cx.notify();
            }
        })

        .detach();
        let recherche = cx.new(|cx| InputState::new(window, cx).placeholder(t("Rechercher")));
        cx.subscribe_in(&recherche, window, |this, state, ev: &InputEvent, _window, cx| {
            if let InputEvent::Change = ev {
                this.filtre = plier(state.read(cx).value().trim());
                cx.notify();
            }
        })
        .detach();
        let emoji_recherche = cx.new(|cx| InputState::new(window, cx).placeholder(t("Chercher un emoji (en anglais)")));
        cx.subscribe_in(&emoji_recherche, window, |this, state, ev: &InputEvent, _window, cx| {
            if let InputEvent::Change = ev {
                this.emoji_filtre = state.read(cx).value().trim().to_lowercase();
                cx.notify();
            }
        })
        .detach();
        let nip = cx.new(|cx| InputState::new(window, cx).placeholder(t("NIP Messenger (6 chiffres), puis Entrée")).masked(true));
        cx.subscribe_in(&nip, window, |this, state, ev: &InputEvent, window, cx| {
            if let InputEvent::PressEnter { .. } = ev {
                let pin = state.read(cx).value().trim().to_string();
                this.commande_meta(json!({"cmd": "history_pin", "pin": pin}));
                this.nip.update(cx, |s, cx| s.set_value("", window, cx));
                cx.notify();
            }
        })
        .detach();
        let numero = cx.new(|cx| InputState::new(window, cx).placeholder(t("Nom ou numéro de téléphone")));
        cx.subscribe_in(&numero, window, |this, state, ev: &InputEvent, window, cx| {
            if let InputEvent::Change = ev {
                let texte = state.read(cx).value().trim().to_string();
                if texte != this.nouveau_texte {
                    this.nouveau_texte = texte.clone();
                    this.resultats_meta.clear();
                    // Messenger cherche parmi tes amis et contacts.
                    if texte.chars().count() >= 2 && this.etat_meta == Etat::Pret && !est_numero(&texte) {
                        this.commande_meta(json!({"cmd": "search", "text": texte}));
                    }
                    cx.notify();
                }
            }
            if let InputEvent::PressEnter { .. } = ev {
                let n = state.read(cx).value().trim().to_string();
                if !n.is_empty() {
                    this.commande(json!({"cmd": "new", "number": n}));
                    this.nouveau_ouvert = false;
                    this.numero.update(cx, |s, cx| s.set_value("", window, cx));
                    cx.notify();
                }
            }
        })
        .detach();
        cx.observe_window_activation(window, |this, window, cx| {
            this.active = window.is_window_active();
            // De retour dans la fenêtre : la conversation ouverte est lue.
            if this.active
                && let Some(id) = this.choisie.clone()
            {
                this.marquer_lu(&id);
            }
            cx.notify();
        })
        .detach();

        let (pont, erreur) = match Pont::lancer() {
            Ok(p) => (Some(p), None),
            Err(e) => (None, Some(format!("Pont introuvable : {e}"))),
        };
        let fenetre = window.window_handle();
        // Les nouvelles du pont, de la fenêtre de connexion et des notifications, toutes les 50 ms.
        cx.spawn(async move |this, cx| {
            loop {
                cx.background_executor().timer(Duration::from_millis(50)).await;
                let mut activer = false;
                let ok = this
                    .update(cx, |this, cx| {
                        // Le chrono de l'enregistrement et la lecture défilent.
                        let mut change = this.vocal.is_some() || this.lecture.is_some();
                        let finie = this.lecture.as_mut().is_some_and(|l| !matches!(l.enfant.try_wait(), Ok(None)));
                        if finie {
                            this.lecture = None;
                        }
                        let pretes: Vec<_> = this.ondes_pretes.lock().map(|mut v| std::mem::take(&mut *v)).unwrap_or_default();
                        for (id, barres, duree) in pretes {
                            this.ondes.insert(id, (barres, duree));
                            change = true;
                        }
                        let lignes: Vec<Value> = this.pont.as_ref().map(|p| p.recevoir()).unwrap_or_default();
                        for l in lignes {
                            this.nouvelle(l);
                            change = true;
                        }
                        let lignes: Vec<Value> = this.pont_meta.as_ref().map(|p| p.recevoir()).unwrap_or_default();
                        for mut l in lignes {
                            prefixer_meta(&mut l);
                            this.nouvelle_meta(l);
                            change = true;
                        }
                        let retour = this.connexion_meta.lock().ok().and_then(|mut r| r.take());
                        if let Some(r) = retour {
                            this.connexion_meta_ouverte = false;
                            match r {
                                Ok(v) => {
                                    this.etat_meta = Etat::Association;
                                    this.commande_meta(json!({
                                        "cmd": "login", "cookies": v["cookies"], "platform": v["platform"]
                                    }));
                                }
                                Err(e) => this.erreur = Some(e),
                            }
                            change = true;
                        }
                        let retour = this.connexion.lock().ok().and_then(|mut r| r.take());
                        if let Some(r) = retour {
                            this.connexion_ouverte = false;
                            match r {
                                Ok(v) => {
                                    this.etat = Etat::Association;
                                    this.commande(json!({"cmd": "google", "cookies": v["cookies"]}));
                                }
                                Err(e) => this.erreur = Some(e),
                            }
                            change = true;
                        }
                        let cliquee = this.a_ouvrir.lock().ok().and_then(|mut r| r.take());
                        if let Some(id) = cliquee {
                            this.section = None;
                        this.galerie = None;
                            this.choisir(id);
                            activer = true;
                            change = true;
                        }
                        if change {
                            cx.notify();
                        }
                    })
                    .is_ok();
                if !ok {
                    break;
                }
                if activer {
                    let _ = cx.update_window(fenetre, |_, window, _| window.activate_window());
                }
            }
        })
        .detach();

        Self {
            pont,
            etat: Etat::Demarrage,
            qr: None,
            emoji: None,
            connexion: Arc::new(Mutex::new(None)),
            connexion_ouverte: false,
            pont_meta: Pont::lancer_meta().map_err(|e| eprintln!("jasette : {e}")).ok(),
            etat_meta: Etat::Demarrage,
            nom_meta: None,
            avatar_messenger: None,
            connexion_meta: Arc::new(Mutex::new(None)),
            connexion_meta_ouverte: false,
            besoin_nip: false,
            nip,
            convs: Vec::new(),
            archives: Vec::new(),
            archives_chargees: false,
            choisie: None,
            messages: HashMap::new(),
            plus_anciens: HashMap::new(),
            chargement: None,
            erreur,
            vue: Vue::Tout,
            non_lus_seulement: false,
            recherche_ouverte: false,
            nouveau_ouvert: false,
            banniere_fermee: false,
            recherche,
            numero,
            filtre: String::new(),
            saisie,
            defile: ScrollHandle::new(),
            descendre: false,
            envoi_seq: 0,
            emojis_ouverts: false,
            menu_avatar: false,
            menu_conv: false,
            confirmer_suppression: false,
            nouveau_texte: String::new(),
            resultats_meta: Vec::new(),
            details_heures: false,
            galerie: None,
            galerie_onglet: 0,
            galerie_chargee: false,
            apercu: None,
            avatar_google: None,
            emoji_groupe: "Smileys",
            emoji_recherche,
            emoji_filtre: String::new(),
            indication: String::new(),
            vocal: None,
            lecture: None,
            ondes: HashMap::new(),
            ondes_demandees: HashSet::new(),
            pauses: HashMap::new(),
            ondes_pretes: Arc::new(Mutex::new(Vec::new())),
            medias: HashMap::new(),
            demandes: HashSet::new(),
            echecs: HashSet::new(),
            ouvrir_apres: HashSet::new(),
            avatars: HashMap::new(),
            mosaiques: HashMap::new(),
            reglages: Reglages::charger(),
            section: None,
            ajout_compte: false,
            active: window.is_window_active(),
            a_ouvrir: Arc::new(Mutex::new(None)),
        }
    }

    /// Une commande pour le pont du service de la conversation (ou de la pièce) visée.
    fn commande(&self, mut v: Value) {
        if deprefixer_meta(&mut v) {
            self.commande_meta(v);
        } else if let Some(p) = &self.pont {
            p.envoyer(&v);
        }
    }

    fn commande_meta(&self, v: Value) {
        if let Some(p) = &self.pont_meta {
            p.envoyer(&v);
        }
    }

    /// Les nouvelles du pont Messenger (ids déjà préfixés).
    fn nouvelle_meta(&mut self, v: Value) {
        match v.get("ev").and_then(Value::as_str).unwrap_or_default() {
            "state" => {
                self.etat_meta = match v.get("state").and_then(Value::as_str).unwrap_or_default() {
                    "unpaired" => Etat::NonAssocie,
                    "pairing" => Etat::Association,
                    "connecting" => Etat::Connexion,
                    "ready" => {
                        if self.etat_meta != Etat::Pret {
                            self.commande_meta(json!({"cmd": "conversations", "count": 50}));
                        }
                        self.ajout_compte = false;
                        Etat::Pret
                    }
                    "reconnecting" => Etat::Reconnexion,
                    _ => self.etat_meta,
                };
                if self.etat_meta == Etat::NonAssocie {
                    self.convs.retain(|c| service_de(&c.id) != Service::Messenger);
                    self.archives.retain(|c| service_de(&c.id) != Service::Messenger);
                }
            }
            "paired" => self.etat_meta = Etat::Connexion,
            "account" => self.nom_meta = v.get("name").and_then(Value::as_str).map(str::to_string),
            "avatar_moi" => self.avatar_messenger = v.get("path").and_then(Value::as_str).map(|p| carre(PathBuf::from(p))),
            "history_pin" => {
                if v.get("needed").and_then(Value::as_bool) == Some(true) {
                    self.besoin_nip = true;
                } else if v.get("ok").and_then(Value::as_bool) == Some(true) {
                    self.besoin_nip = false;
                } else if let Some(texte) = v.get("text").and_then(Value::as_str) {
                    self.erreur = Some(format!("Messenger : {texte}"));
                }
            }
            "search" => {
                if v.get("query").and_then(Value::as_str) == Some(self.nouveau_texte.as_str()) {
                    self.resultats_meta = v["items"]
                        .as_array()
                        .map(|a| {
                            a.iter()
                                .filter_map(|x| {
                                    Some((x.get("id")?.as_str()?.to_string(), x.get("name")?.as_str()?.to_string()))
                                })
                                .collect()
                        })
                        .unwrap_or_default();
                }
            }
            "hello" | "e2ee" | "typing" => {}
            "error" => {
                if let Some(texte) = v.get("text").and_then(Value::as_str) {
                    self.erreur = Some(format!("Messenger : {texte}"));
                }
            }
            _ => self.nouvelle(v),
        }
    }

    /// Ouvre la fenêtre de connexion Messenger ; ses cookies reviennent par `self.connexion_meta`.
    fn connecter_messenger(&mut self) {
        if self.connexion_meta_ouverte {
            return;
        }
        let Some(script) = script_connexion_meta() else {
            self.erreur = Some("jasette-connexion-meta.py introuvable".into());
            return;
        };
        self.erreur = None;
        self.connexion_meta_ouverte = true;
        let retour = self.connexion_meta.clone();
        std::thread::spawn(move || {
            let res = std::process::Command::new("python3")
                .arg(&script)
                .stdin(std::process::Stdio::null())
                .stderr(std::process::Stdio::inherit())
                .output();
            let r = match res {
                Ok(out) => {
                    let texte = String::from_utf8_lossy(&out.stdout);
                    match texte.lines().rev().find_map(|l| serde_json::from_str::<Value>(l).ok()) {
                        Some(v) if v.get("cookies").is_some() => Ok(v),
                        _ => Err(t("Connexion annulée").to_string()),
                    }
                }
                Err(e) => Err(format!("Fenêtre de connexion : {e}")),
            };
            if let Ok(mut slot) = retour.lock() {
                *slot = Some(r);
            }
        });
    }

    fn conv(&self, id: &str) -> Option<&Conv> {
        self.convs.iter().chain(self.archives.iter()).find(|c| c.id == id)
    }

    fn non_lus(&self) -> usize {
        self.convs.iter().filter(|c| c.unread).count()
    }

    /// Demande au pont de télécharger une pièce jointe (une seule fois).
    fn telecharger(&mut self, p: &Piece) {
        if self.medias.contains_key(&p.media_id) || !self.demandes.insert(p.media_id.clone()) {
            return;
        }
        self.echecs.remove(&p.media_id);
        self.commande(json!({"cmd": "download", "media_id": p.media_id, "key": p.key, "mime": p.mime}));
    }

    /// Les images s'affichent d'elles-mêmes : on les télécharge dès qu'elles arrivent.
    fn telecharger_images(&mut self, msgs: &[Msg]) {
        let images: Vec<Piece> =
            msgs.iter().flat_map(|m| m.attachments.iter()).filter(|p| p.est_image() || p.est_audio()).cloned().collect();
        for p in &images {
            self.telecharger(p);
        }
    }

    fn marquer_lu(&mut self, id: &str) {
        let mut lu = false;
        if let Some(conv) = self.convs.iter_mut().find(|c| c.id == id) {
            lu = conv.unread;
            conv.unread = false;
        }
        if lu && let Some(last) = self.messages.get(id).and_then(|l| l.iter().rev().find(|m| !m.from_me)) {
            self.commande(json!({"cmd": "read", "conv": id, "msg": last.id}));
        }
    }

    /// Ouvre la fenêtre de connexion Google ; ses cookies reviennent par `self.connexion`.
    fn connecter_google(&mut self) {
        if self.connexion_ouverte {
            return;
        }
        let Some(script) = script_connexion() else {
            self.erreur = Some("jasette-connexion.py introuvable".into());
            return;
        };
        self.erreur = None;
        self.emoji = None;
        self.connexion_ouverte = true;
        let retour = self.connexion.clone();
        std::thread::spawn(move || {
            let res = std::process::Command::new("python3")
                .arg(&script)
                .stdin(std::process::Stdio::null())
                .stderr(std::process::Stdio::inherit())
                .output();
            let r = match res {
                Ok(out) => {
                    let texte = String::from_utf8_lossy(&out.stdout);
                    match texte.lines().rev().find_map(|l| serde_json::from_str::<Value>(l).ok()) {
                        Some(v) if v.get("cookies").is_some() => Ok(v),
                        _ => Err("Connexion annulée".to_string()),
                    }
                }
                Err(e) => Err(format!("Fenêtre de connexion : {e}")),
            };
            if let Ok(mut slot) = retour.lock() {
                *slot = Some(r);
            }
        });
    }

    /// Une notification de bureau ; un clic ouvre la conversation.
    fn notifier(&self, m: &Msg) {
        if !self.reglages.notifications {
            return;
        }
        let conv = self.conv(&m.conv);
        let nom = conv.map(|c| c.name.clone()).filter(|n| !n.is_empty()).unwrap_or_else(|| "Jasette".into());
        let titre = match conv {
            Some(c) if c.group && !m.sender.is_empty() => format!("{} · {nom}", m.sender),
            _ => nom,
        };
        let corps = if !self.reglages.apercu {
            t("Nouveau message").into()
        } else if !m.text.is_empty() {
            m.text.clone()
        } else if m.attachments.iter().any(Piece::est_image) {
            "📷 Photo".into()
        } else {
            t("📎 Pièce jointe").into()
        };
        let icone = self
            .avatars
            .get(&m.conv)
            .map(|p| p.to_string_lossy().to_string())
            .unwrap_or_else(|| "mail-message-new".into());
        let conv_id = m.conv.clone();
        let boite = self.a_ouvrir.clone();
        std::thread::spawn(move || {
            let sortie = std::process::Command::new("notify-send")
                .args(["-a", "Jasette", "-i", &icone, "-h", "string:desktop-entry:jasette"])
                .args(["-A", "default=Ouvrir"])
                .arg(&titre)
                .arg(&corps)
                .stdin(std::process::Stdio::null())
                .stderr(std::process::Stdio::null())
                .output();
            if let Ok(o) = sortie
                && String::from_utf8_lossy(&o.stdout).trim() == "default"
                && let Ok(mut b) = boite.lock()
            {
                *b = Some(conv_id);
            }
        });
    }

    /// Une ligne du pont.
    fn nouvelle(&mut self, v: Value) {
        let ev = v.get("ev").and_then(Value::as_str).unwrap_or_default();
        match ev {
            "state" => {
                let avant = self.etat;
                self.etat = match v.get("state").and_then(Value::as_str).unwrap_or_default() {
                    "unpaired" => {
                        self.emoji = None;
                        Etat::NonAssocie
                    }
                    "pairing" => Etat::Association,
                    "connecting" => Etat::Connexion,
                    "ready" => {
                        self.qr = None;
                        self.emoji = None;
                        self.ajout_compte = false;
                        Etat::Pret
                    }
                    "phone_offline" => Etat::TelephoneHorsLigne,
                    "reconnecting" => Etat::Reconnexion,
                    _ => self.etat,
                };
                if avant != self.etat {
                    self.banniere_fermee = false;
                }
            }
            "account" => {
                if let Some(email) = v.get("email").and_then(Value::as_str) {
                    self.reglages.email = Some(email.to_string());
                    self.reglages.garder();
                }
            }
            "emoji" => self.emoji = v.get("emoji").and_then(Value::as_str).map(str::to_string),
            "qr" => {
                if let Some(data) = v.get("data").and_then(Value::as_str) {
                    self.qr = qrcode::QrCode::new(data.as_bytes()).ok();
                }
            }
            "paired" => {
                self.qr = None;
                self.emoji = None;
                self.etat = Etat::Connexion;
            }
            "conversations" => {
                if let Ok(items) = serde_json::from_value::<Vec<Conv>>(v["items"].clone()) {
                    self.erreur = None;
                    // Google renvoie parfois une conversation deux fois : une seule par id.
                    let mut vues = HashSet::new();
                    let items: Vec<Conv> = items.into_iter().filter(|c| vues.insert(c.id.clone())).collect();
                    // Chaque pont ne remplace que ses propres conversations.
                    let meta = v.get("_meta").is_some();
                    let garder = |c: &Conv| (service_de(&c.id) == Service::Messenger) != meta;
                    let fusion = |anciennes: &mut Vec<Conv>, items: Vec<Conv>| {
                        anciennes.retain(garder);
                        anciennes.extend(items);
                        anciennes.sort_by(|a, b| b.time.cmp(&a.time));
                    };
                    if v.get("folder").and_then(Value::as_str) == Some("archive") || v.get("archive").and_then(Value::as_bool) == Some(true) {
                        fusion(&mut self.archives, items);
                    } else {
                        fusion(&mut self.convs, items);
                        if self.choisie.is_none()
                            && let Some(first) = self.convs.first()
                        {
                            let id = first.id.clone();
                            self.choisir(id);
                        }
                    }
                }
            }
            "conversation" => {
                if let Ok(conv) = serde_json::from_value::<Conv>(v["item"].clone()) {
                    let id = conv.id.clone();
                    self.convs.retain(|c| c.id != id);
                    self.archives.retain(|c| c.id != id);
                    if conv.archived {
                        self.archives.push(conv);
                        self.archives.sort_by(|a, b| b.time.cmp(&a.time));
                    } else {
                        self.convs.push(conv);
                        self.convs.sort_by(|a, b| b.time.cmp(&a.time));
                    }
                    if v.get("open").and_then(Value::as_bool) == Some(true) {
                        self.vue = Vue::Tout;
                        self.choisir(id);
                    }
                }
            }
            "avatars_groupe" => {
                if let (Some(conv), Some(membres)) =
                    (v.get("conv").and_then(Value::as_str), v.get("members").and_then(Value::as_array))
                {
                    let liste = membres
                        .iter()
                        .map(|m| {
                            let champ = |k: &str| m.get(k).and_then(Value::as_str).unwrap_or_default().to_string();
                            let chemin = champ("path");
                            (champ("name"), champ("color"), (!chemin.is_empty()).then(|| carre(PathBuf::from(chemin))))
                        })
                        .collect::<Vec<_>>();
                    if !liste.is_empty() {
                        self.mosaiques.insert(conv.to_string(), liste);
                    }
                }
            }
            "avatar_moi" => {
                if let Some(path) = v.get("path").and_then(Value::as_str) {
                    self.avatar_google = Some(carre(PathBuf::from(path)));
                    // Par défaut, dès qu'une photo existe, on la prend.
                    if self.reglages.avatar_source.is_empty() {
                        self.reglages.avatar_source = "google".into();
                        self.reglages.garder();
                    }
                }
            }
            "avatar" => {
                if let (Some(conv), Some(path)) =
                    (v.get("conv").and_then(Value::as_str), v.get("path").and_then(Value::as_str))
                {
                    self.avatars.insert(conv.to_string(), carre(PathBuf::from(path)));
                }
            }
            "media" => {
                let id = v.get("media_id").and_then(Value::as_str).unwrap_or_default().to_string();
                self.demandes.remove(&id);
                if let Some(path) = v.get("path").and_then(Value::as_str) {
                    let path = PathBuf::from(path);
                    if self.ouvrir_apres.remove(&id) {
                        ouvrir_fichier(&path);
                    }
                    self.calculer_onde(&id, &path);
                    self.medias.insert(id, path);
                } else {
                    self.ouvrir_apres.remove(&id);
                    self.echecs.insert(id);
                }
            }
            "messages" => {
                let conv = v.get("conv").and_then(Value::as_str).unwrap_or_default().to_string();
                let items = serde_json::from_value::<Vec<Msg>>(v["items"].clone()).unwrap_or_default();
                let older = v.get("older").and_then(Value::as_bool).unwrap_or(false);
                let more = v.get("more").and_then(Value::as_bool).unwrap_or(false);
                self.telecharger_images(&items);
                let liste = self.messages.entry(conv.clone()).or_default();
                if older {
                    let mut nouveaux: Vec<Msg> =
                        items.into_iter().filter(|m| !liste.iter().any(|x| x.id == m.id)).collect();
                    nouveaux.append(liste);
                    *liste = nouveaux;
                } else {
                    // Garde les envois locaux pas encore confirmés.
                    let attente: Vec<Msg> = liste
                        .iter()
                        .filter(|m| m.id == m.tmp_id && !items.iter().any(|x| x.tmp_id == m.tmp_id))
                        .cloned()
                        .collect();
                    *liste = items;
                    liste.extend(attente);
                    self.descendre = true;
                }
                self.plus_anciens.insert(conv.clone(), more);
                if self.chargement.as_deref() == Some(conv.as_str()) {
                    self.chargement = None;
                }
                if !older && self.choisie.as_deref() == Some(conv.as_str()) && self.active {
                    self.marquer_lu(&conv);
                }
            }
            "message" => {
                if let Ok(m) = serde_json::from_value::<Msg>(v["item"].clone()) {
                    self.telecharger_images(std::slice::from_ref(&m));
                    let ouverte = self.choisie.as_deref() == Some(m.conv.as_str());
                    let liste = self.messages.entry(m.conv.clone()).or_default();
                    let nouveau;
                    if let Some(i) = liste
                        .iter()
                        .position(|x| x.id == m.id || (!m.tmp_id.is_empty() && x.tmp_id == m.tmp_id))
                    {
                        let etait_local = liste[i].id == liste[i].tmp_id;
                        liste[i] = m.clone();
                        nouveau = etait_local && !m.from_me;
                    } else {
                        nouveau = true;
                        liste.push(m.clone());
                        liste.sort_by_key(|x| x.time);
                        if ouverte {
                            self.descendre = true;
                        }
                    }
                    let actif = self.active;
                    if let Some(conv) = self.convs.iter_mut().find(|c| c.id == m.conv) {
                        if nouveau || conv.time <= m.time {
                            conv.preview = if !m.text.is_empty() {
                                m.text.clone()
                            } else if m.attachments.iter().any(Piece::est_image) {
                                "📷 Photo".into()
                            } else if m.attachments.iter().any(Piece::est_audio) {
                                t("🎤 Message vocal").into()
                            } else {
                                t("📎 Pièce jointe").into()
                            };
                            conv.from_me = m.from_me;
                        }
                        conv.time = conv.time.max(m.time);
                        if nouveau && !m.from_me && !(ouverte && actif) {
                            conv.unread = true;
                        }
                    }
                    self.convs.sort_by(|a, b| b.time.cmp(&a.time));
                    if nouveau && !m.from_me {
                        if ouverte && self.active {
                            self.commande(json!({"cmd": "read", "conv": m.conv, "msg": m.id}));
                        } else {
                            self.notifier(&m);
                        }
                    }
                }
            }
            "sent" => {
                if !v.get("ok").and_then(Value::as_bool).unwrap_or(false) {
                    let tmp = v.get("tmp_id").and_then(Value::as_str).unwrap_or_default();
                    for liste in self.messages.values_mut() {
                        if let Some(m) = liste.iter_mut().find(|m| m.tmp_id == tmp) {
                            m.status = "ÉCHEC".into();
                        }
                    }
                }
            }
            "error" => {
                let texte = v.get("text").and_then(Value::as_str).unwrap_or_default();
                self.erreur = Some(texte.to_string());
                if v.get("where").and_then(Value::as_str) == Some("messages") {
                    self.chargement = None;
                }
            }
            "pont_ferme" => self.erreur = Some("Le pont s'est arrêté (voir le terminal).".into()),
            _ => {}
        }
    }

    fn choisir(&mut self, id: String) {
        // Changer de conversation ferme la galerie et le menu d'options.
        self.galerie = None;
        self.menu_conv = false;
        self.confirmer_suppression = false;
        self.emojis_ouverts = false;
        if !self.messages.contains_key(&id) {
            self.chargement = Some(id.clone());
            self.commande(json!({"cmd": "messages", "conv": id, "count": 60}));
        } else {
            self.descendre = true;
            self.marquer_lu(&id);
        }
        self.choisie = Some(id);
    }

    fn plus_vieux(&mut self) {
        let Some(id) = self.choisie.clone() else { return };
        let Some(premier) = self.messages.get(&id).and_then(|l| l.first()).cloned() else { return };
        self.chargement = Some(id.clone());
        self.commande(json!({"cmd": "messages", "conv": id, "count": 60, "before": premier.id, "before_time": premier.time}));
    }

    /// Un message local en attente de confirmation du téléphone.
    fn en_attente(&mut self, conv: &str, texte: String, media: String, pieces: Vec<Piece>) -> String {
        self.envoi_seq += 1;
        let tmp = format!("jasette_{}_{}", chrono::Utc::now().timestamp_millis(), self.envoi_seq);
        self.messages.entry(conv.to_string()).or_default().push(Msg {
            id: tmp.clone(),
            conv: conv.to_string(),
            text: texte,
            media,
            from_me: true,
            sender: String::new(),
            time: chrono::Utc::now().timestamp_millis(),
            status: "ENVOI".into(),
            kind: String::new(),
            tmp_id: tmp.clone(),
            attachments: pieces,
        });
        self.descendre = true;
        tmp
    }

    fn envoyer(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        if self.vocal.is_some() {
            self.arreter_vocal(true);
            cx.notify();
            return;
        }
        let texte = self.saisie.read(cx).value().trim().to_string();
        let Some(conv) = self.choisie.clone() else { return };
        if texte.is_empty() || self.etat != Etat::Pret {
            return;
        }
        let tmp = self.en_attente(&conv, texte.clone(), String::new(), Vec::new());
        self.commande(json!({"cmd": "send", "conv": conv, "text": texte, "tmp_id": tmp}));
        self.saisie.update(cx, |s, cx| s.set_value("", window, cx));
        self.emojis_ouverts = false;
        cx.notify();
    }

    fn envoyer_fichier(&mut self, chemin: PathBuf, mime: Option<&str>) {
        let Some(conv) = self.choisie.clone() else { return };
        let nom = chemin.file_name().map(|n| n.to_string_lossy().to_string()).unwrap_or_default();
        let ext = chemin.extension().map(|e| e.to_string_lossy().to_lowercase()).unwrap_or_default();
        let mime_local = mime.map(str::to_string).unwrap_or_else(|| {
            match ext.as_str() {
                "jpg" | "jpeg" => "image/jpeg",
                "png" => "image/png",
                "gif" => "image/gif",
                "webp" => "image/webp",
                "ogg" | "oga" | "opus" => "audio/ogg",
                "m4a" | "aac" => "audio/mp4",
                "mp3" => "audio/mpeg",
                "wav" => "audio/wav",
                _ => "application/octet-stream",
            }
            .to_string()
        });
        // L'aperçu local : la pièce pointe sur le fichier tant que le téléphone n'a pas confirmé.
        let id_local = format!("local:{}", chemin.display());
        let taille_fichier = std::fs::metadata(&chemin).map(|m| m.len() as i64).unwrap_or(0);
        let piece = Piece {
            media_id: id_local.clone(),
            key: String::new(),
            mime: mime_local,
            name: nom,
            size: taille_fichier,
            w: 0,
            h: 0,
        };
        self.medias.insert(id_local.clone(), chemin.clone());
        if piece.est_audio() {
            self.calculer_onde(&id_local, &chemin);
        }
        let tmp = self.en_attente(&conv, String::new(), String::new(), vec![piece]);
        self.commande(json!({
            "cmd": "send_media", "conv": conv, "path": chemin, "tmp_id": tmp, "mime": mime.unwrap_or_default()
        }));
    }

    /// Calcule la forme d'onde d'un vocal dans un fil à part.
    fn calculer_onde(&mut self, id: &str, chemin: &std::path::Path) {
        if self.ondes.contains_key(id) || !self.ondes_demandees.insert(id.to_string()) {
            return;
        }
        let (id, chemin, boite) = (id.to_string(), chemin.to_path_buf(), self.ondes_pretes.clone());
        std::thread::spawn(move || {
            if let Some((barres, duree)) = calculer_onde(&chemin)
                && let Ok(mut b) = boite.lock()
            {
                b.push((id, barres, duree));
            }
        });
    }

    fn arreter_lecture(&mut self) -> Option<(String, f32)> {
        let mut l = self.lecture.take()?;
        let pos = l.position();
        let _ = l.enfant.kill();
        let _ = l.enfant.wait();
        Some((l.media_id, pos))
    }

    /// Ouvre la galerie : tous les comptes (`None`) ou une conversation.
    fn ouvrir_galerie(&mut self, conv: Option<String>) {
        self.section = None;
        if conv.is_none() && !self.galerie_chargee {
            // Google n'a pas de « liste des médias » : on charge les messages
            // récents de chaque conversation, et la galerie se remplit.
            self.galerie_chargee = true;
            let ids: Vec<String> =
                self.convs.iter().map(|c| c.id.clone()).filter(|id| !self.messages.contains_key(id)).collect();
            for id in ids {
                self.commande(json!({"cmd": "messages", "conv": id, "count": 60}));
            }
        }
        self.galerie = Some(conv);
    }

    /// La galerie des médias : photos et vidéos, vocaux, fichiers.
    fn galerie_vue(&mut self, cx: &mut Context<Self>) -> impl IntoElement {
        let filtre = self.galerie.clone().flatten();
        let mut pieces: Vec<(Msg, Piece)> = self
            .messages
            .iter()
            .filter(|(conv, _)| filtre.as_ref().is_none_or(|f| f == *conv))
            .flat_map(|(_, l)| l.iter())
            .flat_map(|m| m.attachments.iter().map(move |p| (m.clone(), p.clone())))
            .collect();
        pieces.sort_by(|a, b| b.0.time.cmp(&a.0.time));
        let est_visuel = |p: &Piece| p.est_image() || p.mime.starts_with("video/");
        let onglet = self.galerie_onglet;
        let choisies: Vec<(Msg, Piece)> = pieces
            .into_iter()
            .filter(|(_, p)| match onglet {
                0 => est_visuel(p),
                1 => p.est_audio(),
                _ => !est_visuel(p) && !p.est_audio(),
            })
            .collect();
        let nom_conv = |id: &str| -> String {
            self.conv(id).map(|c| c.name.clone()).filter(|n| !n.is_empty()).unwrap_or_else(|| t("Sans nom").into())
        };
        let titre = match &filtre {
            Some(id) => format!("{} · {}", t("Médias"), nom_conv(id)),
            None => t("Médias").to_string(),
        };

        let mut onglets = div().flex().flex_row().gap(px(6.));
        for (i, nom) in [t("Photos et vidéos"), t("Vocaux"), t("Fichiers")].into_iter().enumerate() {
            let actif = onglet as usize == i;
            onglets = onglets.child(
                div()
                    .id(("onglet-galerie", i))
                    .px(px(14.))
                    .py(px(6.))
                    .rounded(rayon(999.))
                    .cursor_pointer()
                    .text_size(px(13.))
                    .when(actif, |d| d.bg(th().accent).text_color(blanc(1.)))
                    .when(!actif, |d| d.bg(th().survol).text_color(th().texte).hover(|s| s.opacity(0.8)))
                    .on_click(cx.listener(move |this, _, _, cx| {
                        this.galerie_onglet = i as u8;
                        cx.notify();
                    }))
                    .child(nom),
            );
        }

        let mut contenu = div().flex().flex_col().gap(px(8.)).p(px(20.));
        if choisies.is_empty() {
            contenu = contenu.child(div().p(px(20.)).text_color(th().texte_pale).child(if self.galerie_chargee || filtre.is_some() {
                t("Rien ici pour l'instant.")
            } else {
                t("Chargement…")
            }));
        } else if onglet == 0 {
            let mut grille = div().flex().flex_row().flex_wrap().gap(px(8.));
            for (i, (m, p)) in choisies.iter().enumerate() {
                let chemin = self.medias.get(&p.media_id).cloned();
                let video = p.mime.starts_with("video/");
                let piece = p.clone();
                let ouvrir = chemin.clone();
                grille = grille.child(
                    div()
                        .id(("vignette", i))
                        .relative()
                        .size(px(150.))
                        .rounded(rayon(10.))
                        .overflow_hidden()
                        .bg(th().surface)
                        .cursor_pointer()
                        .hover(|s| s.opacity(0.85))
                        .on_click(cx.listener(move |this, _, _, cx| {
                            match (&ouvrir, video) {
                                (Some(c), false) => this.apercu = Some(c.clone()),
                                (Some(c), true) => ouvrir_fichier(c),
                                (None, _) => {
                                    this.ouvrir_apres.insert(piece.media_id.clone());
                                    this.telecharger(&piece);
                                }
                            }
                            cx.notify();
                        }))
                        .child(match (&chemin, video) {
                            (Some(c), false) => img(c.clone()).size_full().rounded(rayon(10.)).object_fit(ObjectFit::Cover).into_any_element(),
                            _ => div()
                                .size_full()
                                .flex()
                                .items_center()
                                .justify_center()
                                .child(icone(if video { "play" } else { "image" }, 28., th().texte_pale))
                                .into_any_element(),
                        })
                        .child(div().absolute().right(px(6.)).bottom(px(6.)).child(logo(service_de(&m.conv), 18.))),
                );
            }
            contenu = contenu.child(grille);
        } else {
            for (m, p) in &choisies {
                let mut ligne = div()
                    .flex()
                    .flex_row()
                    .items_center()
                    .gap(px(12.))
                    .p(px(10.))
                    .rounded(rayon(14.))
                    .bg(th().surface)
                    .child(logo(service_de(&m.conv), 22.))
                    .child(
                        div()
                            .flex()
                            .flex_col()
                            .w(px(200.))
                            .flex_none()
                            .child(div().truncate().text_color(th().texte_fort).child(nom_conv(&m.conv)))
                            .child(
                                div()
                                    .text_size(px(12.))
                                    .text_color(th().texte_pale)
                                    .child(format!("{} · {}", jour(m.time), heure(m.time, false))),
                            ),
                    );
                if onglet == 1 {
                    ligne = ligne.child(self.lecteur_vocal(m, p, cx));
                } else {
                    let nom = if p.name.is_empty() { p.mime.clone() } else { p.name.clone() };
                    let piece = p.clone();
                    ligne = ligne
                        .child(icone("file", 18., th().texte))
                        .child(div().flex_1().min_w(px(0.)).truncate().text_color(th().texte_fort).child(nom))
                        .child(div().text_size(px(12.)).text_color(th().texte_pale).child(taille(p.size)))
                        .child(
                            div()
                                .id(SharedString::from(format!("ouvrir-{}", p.media_id)))
                                .cursor_pointer()
                                .p(px(4.))
                                .rounded(rayon(8.))
                                .hover(|s| s.bg(th().survol))
                                .on_click(cx.listener(move |this, _, _, cx| {
                                    if let Some(c) = this.medias.get(&piece.media_id) {
                                        ouvrir_fichier(c);
                                    } else {
                                        this.ouvrir_apres.insert(piece.media_id.clone());
                                        this.telecharger(&piece);
                                    }
                                    cx.notify();
                                }))
                                .child(icone("external-link", 16., th().accent)),
                        );
                }
                contenu = contenu.child(ligne);
            }
        }

        panneau(th().fil, th().rayon_fil)
            .flex_1()
            .h_full()
            .flex()
            .flex_col()
            .overflow_hidden()
            .child(
                div()
                    .flex()
                    .flex_row()
                    .items_center()
                    .gap(px(14.))
                    .px(px(20.))
                    .pt(px(18.))
                    .pb(px(12.))
                    .child(icone("images", 22., th().texte_fort))
                    .child(div().flex_1().truncate().text_size(px(18.)).font_weight(FontWeight::BOLD).text_color(th().texte_fort).child(titre))
                    .child(onglets)
                    .when(filtre.is_some(), |d| {
                        d.child(
                            div()
                                .id("fermer-galerie")
                                .cursor_pointer()
                                .p(px(6.))
                                .rounded(rayon(999.))
                                .hover(|s| s.bg(th().survol))
                                .on_click(cx.listener(|this, _, _, cx| {
                                    this.galerie = None;
                                    cx.notify();
                                }))
                                .child(icone("x", 18., th().texte)),
                        )
                    }),
            )
            .child(div().id("galerie").flex_1().min_h(px(0.)).overflow_y_scroll().child(contenu))
    }

    /// Lecture / pause d'un vocal, dans la bulle.
    fn basculer_lecture(&mut self, p: &Piece) {
        let Some(chemin) = self.medias.get(&p.media_id).cloned() else {
            self.telecharger(p);
            return;
        };
        let duree = self.ondes.get(&p.media_id).map(|o| o.1).unwrap_or(0.);
        // Le même vocal : pause, et on retient où on en était.
        if let Some((id, pos)) = self.arreter_lecture() {
            if id == p.media_id {
                self.pauses.insert(id, pos);
                return;
            }
        }
        let mut depuis = self.pauses.remove(&p.media_id).unwrap_or(0.);
        if duree > 0. && depuis >= duree - 0.05 {
            depuis = 0.;
        }
        match std::process::Command::new("ffplay")
            .args(["-nodisp", "-autoexit", "-loglevel", "quiet", "-ss", &format!("{depuis:.2}")])
            .arg(&chemin)
            .stdin(std::process::Stdio::null())
            .stdout(std::process::Stdio::null())
            .stderr(std::process::Stdio::null())
            .spawn()
        {
            Ok(enfant) => {
                self.lecture = Some(Lecture { media_id: p.media_id.clone(), enfant, debut: Instant::now(), depuis })
            }
            Err(e) => self.erreur = Some(format!("Lecture : {e}")),
        }
    }

    /// L'avatar d'une conversation : sa photo, sinon (groupe) la mosaïque de
    /// ses membres, sinon les initiales.
    fn avatar_conv(&self, conv: &Conv, nom: &str, taille: f32) -> AnyElement {
        let photo = self.avatars.get(&conv.id);
        if conv.group
            && photo.is_none()
            && let Some(membres) = self.mosaiques.get(&conv.id)
        {
            return mosaique(membres, taille);
        }
        avatar(nom, &conv.color, conv.group, photo, taille)
    }

    fn photo_moi(&self) -> Option<PathBuf> {
        match self.reglages.avatar_source.as_str() {
            "google" => self.avatar_google.clone(),
            "messenger" => self.avatar_messenger.clone(),
            "fichier" => self.reglages.avatar_fichier.clone().filter(|p| p.is_file()),
            _ => None,
        }
    }

    /// Mon avatar : la photo choisie, sinon l'initiale sur fond vert.
    fn mon_avatar(&self, initiale: &str, taille: f32) -> AnyElement {
        if let Some(photo) = self.photo_moi() {
            return avatar("", "", false, Some(&photo), taille);
        }
        div()
            .flex_none()
            .size(px(taille))
            .rounded(rayon(taille))
            .bg(fond_avatar(c(0x34d399), c(0x059669)))
            .border_1()
            .border_color(ton(blanc(0.85), th().bord_avatar))
            .flex()
            .items_center()
            .justify_center()
            .text_color(blanc(1.))
            .font_weight(FontWeight::BOLD)
            .child(initiale.to_string())
            .into_any_element()
    }

    /// Le menu « D'où vient mon avatar ? ».
    fn menu_avatar_vue(&self, cx: &mut Context<Self>) -> impl IntoElement {
        let actuel = if self.reglages.avatar_source.is_empty() { "initiale" } else { self.reglages.avatar_source.as_str() };
        let ligne = |id: &'static str, source: &'static str, titre: &'static str, detail: String, vignette: AnyElement, dispo: bool| {
            let choisi = actuel == source;
            div()
                .id(id)
                .flex()
                .flex_row()
                .items_center()
                .gap(px(10.))
                .px(px(10.))
                .py(px(7.))
                .rounded(rayon(12.))
                .when(choisi, |d| d.bg(ton(blanc(0.9), th().menu_champ)))
                .when(dispo, |d| d.cursor_pointer().hover(|s| s.bg(ton(blanc(0.75), th().menu_champ))))
                .when(!dispo, |d| d.opacity(0.5))
                .child(vignette)
                .child(
                    div()
                        .flex()
                        .flex_col()
                        .flex_1()
                        .min_w(px(0.))
                        .child(div().text_size(px(14.)).text_color(th().texte_fort).child(titre))
                        .child(div().truncate().text_size(px(12.)).text_color(th().texte_pale).child(detail)),
                )
                .when(choisi, |d| d.child(icone("check", 16., th().accent)))
        };
        let avec_badge = |photo: AnyElement, s: Service| {
            div()
                .relative()
                .flex_none()
                .child(photo)
                .child(div().absolute().right(px(-3.)).bottom(px(-3.)).child(logo(s, 15.)))
                .into_any_element()
        };
        let google_dispo = self.avatar_google.is_some();
        let fichier = self.reglages.avatar_fichier.clone().filter(|p| p.is_file());
        verre(0.94, 18.)
            .when(th().sombre, |d| d.bg(th().menu).border_1().border_color(th().trait_fin))
            .shadow(ombre_verre())
            .mx(px(4.))
            .mb(px(8.))
            .p(px(6.))
            .flex()
            .flex_col()
            .gap(px(2.))
            .child(
                ligne(
                    "av-google",
                    "google",
                    t("Avatar de Google Messages"),
                    if google_dispo { t("Votre fiche sur le téléphone").into() } else { t("Aucune photo trouvée").into() },
                    avec_badge(avatar("G", "", false, self.avatar_google.as_ref(), 34.), Service::GoogleMessages),
                    google_dispo,
                )
                .when(google_dispo, |d| {
                    d.on_click(cx.listener(|this, _, _, cx| {
                        this.reglages.avatar_source = "google".into();
                        this.reglages.garder();
                        this.menu_avatar = false;
                        cx.notify();
                    }))
                }),
            )
            .child(
                ligne(
                    "av-fb",
                    "messenger",
                    t("Avatar de Facebook"),
                    if self.avatar_messenger.is_some() {
                        self.nom_meta.clone().unwrap_or_else(|| "Messenger".into())
                    } else {
                        t("Quand Messenger sera connecté").into()
                    },
                    avec_badge(avatar("F", "#0084ff", false, self.avatar_messenger.as_ref(), 34.), Service::Messenger),
                    self.avatar_messenger.is_some(),
                )
                .when(self.avatar_messenger.is_some(), |d| {
                    d.on_click(cx.listener(|this, _, _, cx| {
                        this.reglages.avatar_source = "messenger".into();
                        this.reglages.garder();
                        this.menu_avatar = false;
                        cx.notify();
                    }))
                }),
            )
            .child(
                ligne(
                    "av-fichier",
                    "fichier",
                    t("Choisir une image…"),
                    fichier
                        .as_ref()
                        .and_then(|p| p.file_name())
                        .map(|n| n.to_string_lossy().to_string())
                        .unwrap_or_else(|| t("Une photo de votre ordinateur").into()),
                    match &fichier {
                        Some(p) => avatar("", "", false, Some(p), 34.),
                        None => div()
                            .size(px(34.))
                            .flex_none()
                            .rounded(rayon(34.))
                            .bg(ton(blanc(0.8), th().menu_champ))
                            .flex()
                            .items_center()
                            .justify_center()
                            .child(icone("image", 16., th().texte))
                            .into_any_element(),
                    },
                    true,
                )
                .on_click(cx.listener(|this, _, _, cx| this.choisir_photo(cx))),
            )
            .child(
                ligne("av-initiale", "initiale", t("Initiale"), t("Sans photo").into(), self.initiale_seule(34.), true)
                    .on_click(cx.listener(|this, _, _, cx| {
                        this.reglages.avatar_source = "initiale".into();
                        this.reglages.garder();
                        this.menu_avatar = false;
                        cx.notify();
                    })),
            )
    }

    fn initiale_seule(&self, taille: f32) -> AnyElement {
        div()
            .flex_none()
            .size(px(taille))
            .rounded(rayon(taille))
            .bg(fond_avatar(c(0x34d399), c(0x059669)))
            .flex()
            .items_center()
            .justify_center()
            .text_color(blanc(1.))
            .font_weight(FontWeight::BOLD)
            .child("J")
            .into_any_element()
    }

    fn choisir_photo(&mut self, cx: &mut Context<Self>) {
        let choix = cx.prompt_for_paths(PathPromptOptions {
            files: true,
            directories: false,
            multiple: false,
            prompt: Some(t("Choisir").into()),
        });
        cx.spawn(async move |this, cx| {
            if let Ok(Ok(Some(chemins))) = choix.await
                && let Some(ch) = chemins.into_iter().next()
            {
                let _ = this.update(cx, |this, cx| {
                    this.reglages.avatar_fichier = Some(ch);
                    this.reglages.avatar_source = "fichier".into();
                    this.reglages.garder();
                    this.menu_avatar = false;
                    cx.notify();
                });
            }
        })
        .detach();
    }

    /// Le « + » : choisir des fichiers à envoyer.
    fn joindre(&mut self, cx: &mut Context<Self>) {
        let choix = cx.prompt_for_paths(PathPromptOptions {
            files: true,
            directories: false,
            multiple: true,
            prompt: Some(t("Envoyer").into()),
        });
        cx.spawn(async move |this, cx| {
            if let Ok(Ok(Some(chemins))) = choix.await {
                let _ = this.update(cx, |this, cx| {
                    for ch in chemins {
                        this.envoyer_fichier(ch, None);
                    }
                    cx.notify();
                });
            }
        })
        .detach();
    }

    /// Le micro : un clic enregistre (PipeWire), un autre envoie.
    fn basculer_vocal(&mut self) {
        if self.vocal.is_some() {
            self.arreter_vocal(true);
            return;
        }
        let dir = dossier_medias().join("vocaux");
        let _ = std::fs::create_dir_all(&dir);
        let fichier = dir.join(format!("vocal-{}.ogg", chrono::Local::now().format("%Y%m%d-%H%M%S")));
        match std::process::Command::new("pw-record")
            .args(["--channels", "1", "--rate", "48000"])
            .arg(&fichier)
            .stdin(std::process::Stdio::null())
            .stdout(std::process::Stdio::null())
            .stderr(std::process::Stdio::null())
            .spawn()
        {
            Ok(enfant) => self.vocal = Some(Vocal { enfant, fichier, debut: Instant::now() }),
            Err(e) => self.erreur = Some(format!("Micro : {e}")),
        }
    }

    fn arreter_vocal(&mut self, envoyer: bool) {
        let Some(mut v) = self.vocal.take() else { return };
        // SIGINT : pw-record termine proprement son fichier.
        let _ = std::process::Command::new("kill").args(["-INT", &v.enfant.id().to_string()]).status();
        let _ = v.enfant.wait();
        if envoyer && v.debut.elapsed() >= Duration::from_millis(600) && v.fichier.is_file() {
            self.envoyer_fichier(v.fichier, Some("audio/ogg"));
        } else {
            let _ = std::fs::remove_file(&v.fichier);
        }
    }

    fn descendre_bientot(&self, cx: &mut Context<Self>) {
        let h = self.defile.clone();
        cx.spawn(async move |_, cx| {
            // Plusieurs fois : les grandes bulles finissent leur mise en page une image plus tard.
            for attente in [16, 60, 180, 450] {
                cx.background_executor().timer(Duration::from_millis(attente)).await;
                h.scroll_to_bottom();
                cx.update(|cx| cx.refresh_windows());
            }
        })
        .detach();
    }

    // ---- Dessin : le rail ----

    fn bouton_rail(id: &'static str, actif: bool) -> gpui::Stateful<Div> {
        div()
            .id(id)
            .relative()
            .size(px(44.))
            .rounded(rayon(14.))
            .flex()
            .items_center()
            .justify_center()
            .cursor_pointer()
            // th().choisi vaut le 0x3a3a3c du sombre ; en EGA, le bleu de sélection.
            .when(actif, |d| d.bg(ton(blanc(0.85), th().choisi)).shadow(ombre_flottante()))
            .when(!actif, |d| d.hover(|s| s.bg(ton(blanc(0.5), c(0x2c2c2e)))))
    }

    fn rail(&self, cx: &mut Context<Self>) -> impl IntoElement {
        let en_reglages = self.section.is_some();
        let non_lus = self.non_lus();
        let gm_probleme = !matches!(self.etat, Etat::Pret | Etat::Demarrage);
        let initiale = self
            .reglages
            .email
            .as_deref()
            .and_then(|e| e.chars().next())
            .or_else(|| std::env::var("USER").ok().and_then(|u| u.chars().next()))
            .unwrap_or('J')
            .to_uppercase()
            .to_string();
        panneau(th().rail, th().rayon_rail)
            .w(px(68.))
            .h_full()
            .flex()
            .flex_col()
            .items_center()
            .gap(px(8.))
            .py(px(14.))
            .child(
                Self::bouton_rail("r-tout", !en_reglages && self.vue == Vue::Tout)
                    .on_click(cx.listener(|this, _, _, cx| {
                        this.section = None;
                        this.galerie = None;
                        this.vue = Vue::Tout;
                        cx.notify();
                    }))
                    .child(icone("inbox", 22., th().texte_fort))
                    .when(non_lus > 0, |d| d.child(pastille_nombre(non_lus, th().accent))),
            )
            .child(
                Self::bouton_rail("r-favoris", !en_reglages && self.galerie.is_none() && self.vue == Vue::Favoris)
                    .on_click(cx.listener(|this, _, _, cx| {
                        this.section = None;
                        this.galerie = None;
                        this.vue = Vue::Favoris;
                        cx.notify();
                    }))
                    .child(icone("star", 22., ton(th().texte_fort, th().icone))),
            )
            .child(
                Self::bouton_rail("r-archives", !en_reglages && self.vue == Vue::Archives)
                    .on_click(cx.listener(|this, _, _, cx| {
                        this.section = None;
                        this.galerie = None;
                        this.vue = Vue::Archives;
                        if !this.archives_chargees {
                            this.archives_chargees = true;
                            this.commande(json!({"cmd": "conversations", "folder": "archive", "count": 100}));
                        }
                        cx.notify();
                    }))
                    .child(icone("archive", 22., ton(th().texte_fort, th().icone))),
            )
            .child(
                Self::bouton_rail("r-medias", !en_reglages && self.galerie == Some(None))
                    .on_click(cx.listener(|this, _, _, cx| {
                        this.ouvrir_galerie(None);
                        cx.notify();
                    }))
                    .child(icone("images", 22., ton(th().texte_fort, th().icone))),
            )
            .child(
                Self::bouton_rail("r-nouveau", false)
                    .border_1()
                    .border_color(ton(ca(TEXTE_PALE, 0.6), c(0x48484a)))
                    .on_click(cx.listener(|this, _, window, cx| {
                        this.section = None;
                        this.galerie = None;
                        this.nouveau_ouvert = true;
                        this.numero.update(cx, |s, cx| s.focus(window, cx));
                        cx.notify();
                    }))
                    .child(icone("message-circle-plus", 20., ton(th().texte_fort, th().icone))),
            )
            .child(div().w(px(32.)).h(px(1.)).my(px(4.)).bg(ton(blanc(0.8), c(0x38383a))))
            .when(self.etat != Etat::NonAssocie, |d| d.child(
                Self::bouton_rail("r-gm", !en_reglages && self.vue == Vue::Service(Service::GoogleMessages))
                    .on_click(cx.listener(|this, _, _, cx| {
                        this.section = None;
                        this.galerie = None;
                        this.vue = Vue::Service(Service::GoogleMessages);
                        cx.notify();
                    }))
                    .child(logo(Service::GoogleMessages, 32.))
                    .when(matches!(self.etat, Etat::Association | Etat::Connexion | Etat::Reconnexion), |d| {
                        d.child(pastille_chargement("chargement-google"))
                    })
                    .when(gm_probleme && !matches!(self.etat, Etat::Association | Etat::Connexion | Etat::Reconnexion), |d| {
                        d.child(pastille_alerte())
                    })
                    .when(!gm_probleme && non_lus > 0, |d| d.child(pastille_nombre(non_lus, ton(c(DANGER), th().accent)))),
            ))
            .when(!matches!(self.etat_meta, Etat::NonAssocie | Etat::Demarrage), |d| d.child(
                Self::bouton_rail("r-messenger", !en_reglages && self.vue == Vue::Service(Service::Messenger))
                    .on_click(cx.listener(|this, _, _, cx| {
                        this.section = None;
                        this.galerie = None;
                        this.vue = Vue::Service(Service::Messenger);
                        cx.notify();
                    }))
                    .child(logo(Service::Messenger, 32.))
                    .when(matches!(self.etat_meta, Etat::Association | Etat::Connexion | Etat::Reconnexion), |d| {
                        d.child(pastille_chargement("chargement-messenger"))
                    })
                    .when(matches!(self.etat_meta, Etat::TelephoneHorsLigne), |d| d.child(pastille_alerte())),
            ))
            // Les services non connectés n'ont pas d'icône : « ••• » mène à l'ajout de compte.
            .child(
                Self::bouton_rail("r-plus-services", false)
                    .on_click(cx.listener(|this, _, _, cx| {
                        this.section = Some(Section::Comptes);
                        this.ajout_compte = true;
                        cx.notify();
                    }))
                    .child(icone("ellipsis", 22., ton(th().texte_fort, th().icone))),
            )
            .child(div().flex_1())
            .child(
                Self::bouton_rail("r-reglages", en_reglages)
                    .on_click(cx.listener(|this, _, _, cx| {
                        this.section = Some(this.section.unwrap_or(Section::Comptes));
                        cx.notify();
                    }))
                    .child(self.mon_avatar(&initiale, 36.)),
            )
    }

    // ---- Dessin : la liste ----

    fn correspond(&self, conv: &Conv) -> bool {
        if self.non_lus_seulement && !conv.unread {
            return false;
        }
        if let Vue::Service(s) = self.vue
            && service_de(&conv.id) != s
        {
            return false;
        }
        if self.vue == Vue::Favoris && !self.reglages.etoiles.contains(&conv.id) {
            return false;
        }
        if self.filtre.is_empty() {
            return true;
        }
        plier(&conv.name).contains(&self.filtre)
            || plier(&conv.preview).contains(&self.filtre)
            || conv.members.iter().any(|m| plier(m).contains(&self.filtre))
            || self.messages.get(&conv.id).is_some_and(|l| l.iter().any(|m| plier(&m.text).contains(&self.filtre)))
    }

    fn bouton_icone(id: &'static str, nom: &'static str, actif: bool) -> gpui::Stateful<Div> {
        div()
            .id(id)
            .size(px(32.))
            .rounded(rayon(10.))
            .flex()
            .items_center()
            .justify_center()
            .cursor_pointer()
            .when(actif, |d| d.bg(alpha(th().accent, mesure(0.14, 0.25))))
            .hover(|s| s.bg(ton(blanc(0.6), c(0x3a3a3c))))
            .child(icone(nom, 18., if actif { th().accent } else { ton(th().texte_fort, th().icone) }))
    }

    fn colonne_liste(&self, cx: &mut Context<Self>) -> impl IntoElement {
        let titre = t(match self.vue {
            Vue::Tout => "Boîte de réception",
            Vue::Favoris => "Favoris",
            Vue::Archives => "Archives",
            Vue::Service(s) => s.nom(),
        });
        // La hauteur de l'en-tête flottant : rangée du titre, champs, bannière.
        let mut haut = 58.;
        let mut col = div()
            .flex()
            .flex_col()
            .child(
                div()
                    .flex()
                    .flex_row()
                    .items_center()
                    .gap(px(2.))
                    .pl(px(20.))
                    .pr(px(12.))
                    .pt(px(16.))
                    .pb(px(10.))
                    .child(
                        div()
                            .flex_1()
                            .truncate()
                            .text_size(px(mesure(18., 16.)))
                            .font_weight(FontWeight::BOLD)
                            .text_color(th().texte_fort)
                            .child(titre),
                    )
                    .child(Self::bouton_icone("f-nonlus", "list-filter", self.non_lus_seulement).on_click(
                        cx.listener(|this, _, _, cx| {
                            this.non_lus_seulement = !this.non_lus_seulement;
                            cx.notify();
                        }),
                    ))
                    .child(Self::bouton_icone("f-cherche", "search", self.recherche_ouverte).on_click(cx.listener(
                        |this, _, window, cx| {
                            this.recherche_ouverte = !this.recherche_ouverte;
                            if this.recherche_ouverte {
                                this.recherche.update(cx, |s, cx| s.focus(window, cx));
                            } else {
                                this.filtre.clear();
                                this.recherche.update(cx, |s, cx| s.set_value("", window, cx));
                            }
                            cx.notify();
                        },
                    )))
                    .child(Self::bouton_icone("f-nouveau", "square-pen", self.nouveau_ouvert).on_click(cx.listener(
                        |this, _, window, cx| {
                            this.nouveau_ouvert = !this.nouveau_ouvert;
                            if this.nouveau_ouvert {
                                this.numero.update(cx, |s, cx| s.focus(window, cx));
                            }
                            cx.notify();
                        },
                    ))),
            );
        let champ = |etat: &Entity<InputState>, nom: &'static str| {
            div().px(px(14.)).pb(px(10.)).child(
                verre(0.7, 999.)
                    .when(th().sombre, |d| d.bg(th().champ))
                    .flex()
                    .flex_row()
                    .items_center()
                    .gap(px(6.))
                    .pl(px(14.))
                    .pr(px(6.))
                    .py(px(2.))
                    .child(icone(nom, 15., th().texte_pale))
                    .child(div().flex_1().child(Input::new(etat).appearance(false).cleanable(true))),
            )
        };
        if self.nouveau_ouvert {
            col = col.child(champ(&self.numero, "pen-line"));
            haut += 46.;
            let (suggestions, n) = self.suggestions(cx);
            if n > 0 {
                col = col.child(suggestions);
                haut += 10. + n as f32 * 50.;
            }
        }
        if self.recherche_ouverte {
            col = col.child(champ(&self.recherche, "search"));
            haut += 46.;
        }
        // La bannière d'état du service.
        if self.etat != Etat::Pret && self.etat != Etat::Demarrage && !self.banniere_fermee {
            let texte = match self.etat {
                Etat::NonAssocie => "Google Messages déconnecté".to_string(),
                e => format!("Google Messages · {}", e.texte()),
            };
            haut += 54.;
            col = col.child(
                div().px(px(12.)).pb(px(10.)).child(
                    verre(0.75, 16.)
                        .when(th().sombre, |d| d.bg(th().champ).rounded(rayon(12.)))
                        .id("banniere")
                        .flex()
                        .flex_row()
                        .items_center()
                        .gap(px(10.))
                        .px(px(12.))
                        .py(px(9.))
                        .cursor_pointer()
                        .on_click(cx.listener(|this, _, _, cx| {
                            this.section = Some(Section::Comptes);
                            cx.notify();
                        }))
                        .child(logo(Service::GoogleMessages, 26.))
                        .child(div().flex_1().text_size(px(13.)).text_color(th().texte_fort).child(texte))
                        .child(
                            div()
                                .id("banniere-x")
                                .cursor_pointer()
                                .on_click(cx.listener(|this, _, _, cx| {
                                    this.banniere_fermee = true;
                                    cx.stop_propagation();
                                    cx.notify();
                                }))
                                .child(icone("x", 16., th().texte)),
                        ),
                ),
            );
        }
        // Comme le fil : la liste passe sous un en-tête givré qui s'efface
        // en dégradé, et ne dessine jamais dans les coins arrondis.
        let givre = |a: f32| alpha(th().givre_liste, a);
        let haut_voile = haut + 24.;
        // Les voiles en retrait d'un pixel (le liseré du verre), arrondis comme le panneau.
        let r = (th().rayon_liste - 1.).max(0.);
        panneau(th().liste, th().rayon_liste)
            .relative()
            .w(px(370.))
            .h_full()
            .overflow_hidden()
            .child(
                div()
                    .absolute()
                    .top(px(MARGE_FIL))
                    .left_0()
                    .right_0()
                    .bottom(px(MARGE_FIL))
                    .child(self.liste(haut - MARGE_FIL + 4., cx)),
            )
            .child(
                div()
                    .absolute()
                    .top(px(1.))
                    .left(px(1.))
                    .right(px(1.))
                    .rounded_t(rayon(r))
                    .when(!th().ega, |d| {
                        d.h(px(haut_voile)).bg(linear_gradient(
                            180.,
                            linear_color_stop(givre(mesure(0.97, 1.)), haut / haut_voile),
                            linear_color_stop(givre(0.), 1.),
                        ))
                    })
                    // En EGA, un en-tête noir plein, coupé net par un filet.
                    .when(th().ega, |d| d.h(px(haut)).bg(givre(1.)).border_b_1().border_color(th().trait_fin))
                    .child(col),
            )
            .child(
                div()
                    .absolute()
                    .bottom(px(1.))
                    .left(px(1.))
                    .right(px(1.))
                    .h(px(if th().ega { MARGE_FIL } else { MARGE_FIL + 18. }))
                    .rounded_b(rayon(r))
                    .when(!th().ega, |d| {
                        d.bg(linear_gradient(
                            180.,
                            linear_color_stop(givre(0.), 0.),
                            linear_color_stop(givre(mesure(0.9, 1.)), 0.6),
                        ))
                    })
                    .when(th().ega, |d| d.bg(givre(1.))),
            )
    }

    /// Les choix sous « nouveau message » : conversations existantes (tous les
    /// comptes), personnes trouvées sur Messenger, ou SMS au numéro tapé.
    fn suggestions(&self, cx: &mut Context<Self>) -> (Div, usize) {
        let q = plier(&self.nouveau_texte);
        let mut boite = div().flex().flex_col().px(px(8.)).pb(px(10.));
        let mut n = 0;
        if q.is_empty() {
            return (boite, 0);
        }
        let ligne = |id: SharedString, nom: String, detail: String, service: Service, photo: Option<&PathBuf>| {
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
                        .min_w(px(0.))
                        .child(div().truncate().text_color(th().texte_fort).child(nom))
                        .child(div().truncate().text_size(px(12.)).text_color(th().texte_pale).child(detail)),
                )
        };
        // Les conversations qui existent déjà.
        let existantes: Vec<&Conv> = self
            .convs
            .iter()
            .chain(self.archives.iter())
            .filter(|c| !c.group && (plier(&c.name).contains(&q) || c.members.iter().any(|m| plier(m).contains(&q))))
            .take(4)
            .collect();
        for c in existantes {
            let id = c.id.clone();
            let s = service_de(&c.id);
            boite = boite.child(
                ligne(SharedString::from(format!("sugg-{}", c.id)), c.name.clone(), s.nom().into(), s, self.avatars.get(&c.id))
                    .on_click(cx.listener(move |this, _, window, cx| {
                        this.nouveau_ouvert = false;
                        this.numero.update(cx, |s, cx| s.set_value("", window, cx));
                        this.vue = Vue::Tout;
                        this.galerie = None;
                        this.choisir(id.clone());
                        cx.notify();
                    })),
            );
            n += 1;
        }
        // Les personnes trouvées par Messenger, sans conversation ici.
        for (id, nom) in self.resultats_meta.iter().take(5) {
            if self.conv(id).is_some() {
                continue;
            }
            let fbid = id.trim_start_matches(PREFIXE_META).to_string();
            boite = boite.child(
                ligne(SharedString::from(format!("sugg-{id}")), nom.clone(), t("Nouvelle conversation Messenger").into(), Service::Messenger, None)
                    .on_click(cx.listener(move |this, _, window, cx| {
                        this.commande_meta(json!({"cmd": "new", "number": fbid}));
                        this.nouveau_ouvert = false;
                        this.numero.update(cx, |s, cx| s.set_value("", window, cx));
                        cx.notify();
                    })),
            );
            n += 1;
        }
        // Un numéro : un SMS par Google Messages.
        if est_numero(&self.nouveau_texte) && self.etat.associe() {
            let numero = self.nouveau_texte.clone();
            boite = boite.child(
                ligne("sugg-sms".into(), numero.clone(), t("Nouveau message par Google Messages").into(), Service::GoogleMessages, None)
                    .on_click(cx.listener(move |this, _, window, cx| {
                        this.commande(json!({"cmd": "new", "number": numero}));
                        this.nouveau_ouvert = false;
                        this.numero.update(cx, |s, cx| s.set_value("", window, cx));
                        cx.notify();
                    })),
            );
            n += 1;
        }
        (boite, n)
    }

    fn liste(&self, haut: f32, cx: &mut Context<Self>) -> impl IntoElement {
        let source = if self.vue == Vue::Archives { &self.archives } else { &self.convs };
        let badge = self.vue == Vue::Tout || self.vue == Vue::Archives;
        let mut col =
            div().id("convs").flex().flex_col().size_full().overflow_y_scroll().px(px(8.)).pt(px(haut)).pb(px(18.));
        let mut visibles: Vec<&Conv> = source.iter().filter(|c| self.correspond(c)).collect();
        // Les épinglées d'abord, dans l'ordre où elles ont été épinglées.
        let rang = |c: &Conv| self.reglages.epingles.iter().position(|x| x == &c.id).unwrap_or(usize::MAX);
        visibles.sort_by_key(|c| rang(c));
        let nb_epingles = visibles.iter().filter(|c| rang(c) != usize::MAX).count();
        if source.is_empty() {
            let t = if self.vue == Vue::Archives && !self.archives_chargees {
                t("Chargement des archives…")
            } else if self.vue == Vue::Archives {
                t("Aucune conversation archivée")
            } else {
                t("Chargement des conversations…")
            };
            col = col.child(div().p(px(16.)).text_color(th().texte_pale).child(t));
        } else if visibles.is_empty() && self.vue == Vue::Favoris && self.filtre.is_empty() {
            col = col.child(
                div().p(px(16.)).text_color(th().texte_pale).child(t("Aucun favori : ajoutez-en par le menu « … » d'une conversation.")),
            );
        } else if visibles.is_empty() {
            col = col.child(div().p(px(16.)).text_color(th().texte_pale).child(t("Aucun résultat")));
        }
        // Le Head : une grille d'avatars en tête de liste (sans recherche en cours).
        let favoris: Vec<&Conv> = if self.filtre.is_empty() && !self.non_lus_seulement && self.vue == Vue::Tout {
            self.reglages.head.iter().filter_map(|id| self.conv(id)).filter(|c| self.correspond(c)).collect()
        } else {
            Vec::new()
        };
        if !favoris.is_empty() {
            let mut grille = div().flex().flex_row().flex_wrap().pb(px(6.));
            for conv in &favoris {
                let id = conv.id.clone();
                let choisie = self.choisie.as_deref() == Some(conv.id.as_str());
                let nom = if conv.name.is_empty() { t("Sans nom").to_string() } else { conv.name.clone() };
                let prenom = if conv.group { nom.clone() } else { nom.split_whitespace().next().unwrap_or(&nom).to_string() };
                grille = grille.child(
                    div()
                        .id(SharedString::from(format!("fav-{}", conv.id)))
                        .w(px(88.))
                        .flex()
                        .flex_col()
                        .items_center()
                        .gap(px(5.))
                        .py(px(8.))
                        .rounded(rayon(14.))
                        .cursor_pointer()
                        .when(choisie, |d| d.bg(th().choisi))
                        .when(!choisie, |d| d.hover(|s| s.bg(th().survol)))
                        .on_click(cx.listener(move |this, _, _, cx| {
                            this.choisir(id.clone());
                            cx.notify();
                        }))
                        .child(
                            div()
                                .relative()
                                .child(self.avatar_conv(conv, &nom, 54.))
                                .child(div().absolute().right(px(-3.)).bottom(px(-1.)).child(logo(service_de(&conv.id), 18.)))
                                .when(conv.unread, |d| {
                                    d.child(
                                        div()
                                            .absolute()
                                            .top(px(0.))
                                            .right(px(0.))
                                            .size(px(12.))
                                            .rounded(rayon(999.))
                                            .bg(th().accent)
                                            .border_2()
                                            .border_color(th().liste),
                                    )
                                }),
                        )
                        .child(
                            div()
                                .w_full()
                                .px(px(4.))
                                .text_center()
                                .truncate()
                                .text_size(px(12.))
                                .text_color(if conv.unread { th().texte_fort } else { th().texte })
                                .child(prenom),
                        ),
                );
            }
            col = col.child(grille).child(div().h(px(1.)).mx(px(10.)).mb(px(4.)).bg(th().survol));
        }
        let n = visibles.len();
        for (i, conv) in visibles.into_iter().enumerate() {
            // Un peu d'air sous les épinglées.
            if nb_epingles > 0 && i == nb_epingles {
                col = col.child(div().h(px(1.)).mx(px(10.)).my(px(8.)).bg(th().survol));
            }
            let epinglee = i < nb_epingles;
            let choisie = self.choisie.as_deref() == Some(conv.id.as_str());
            let id = conv.id.clone();
            let nom = if conv.name.is_empty() { t("Sans nom").to_string() } else { conv.name.clone() };
            let lu_par_eux = conv.from_me
                && self
                    .messages
                    .get(&conv.id)
                    .and_then(|l| l.last())
                    .is_some_and(|m| m.from_me && m.status.contains("READ"));
            col = col.child(
                div()
                    .id(SharedString::from(format!("c-{}", conv.id)))
                    .flex()
                    .flex_row()
                    .items_center()
                    .gap(px(12.))
                    .pl(px(10.))
                    .rounded(rayon(mesure(16., 10.)))
                    .cursor_pointer()
                    .when(choisie, |d| d.bg(th().choisi).shadow(ombre_flottante()))
                    .when(!choisie, |d| d.hover(|s| s.bg(th().survol)))
                    .on_click(cx.listener(move |this, _, _, cx| {
                        this.choisir(id.clone());
                        cx.notify();
                    }))
                    .child(
                        div()
                            .relative()
                            .flex_none()
                            .py(px(10.))
                            .child(self.avatar_conv(conv, &nom, 50.))
                            .when(badge, |d| {
                                d.child(div().absolute().right(px(-3.)).bottom(px(7.)).child(logo(service_de(&conv.id), 20.)))
                            }),
                    )
                    .child(
                        div()
                            .flex()
                            .flex_col()
                            .flex_1()
                            .min_w(px(0.))
                            .gap(px(2.))
                            .py(px(10.))
                            .pr(px(10.))
                            .when(i + 1 < n && !choisie, |d| d.border_b_1().border_color(th().trait_fin))
                            .child(
                                div()
                                    .flex()
                                    .flex_row()
                                    .justify_between()
                                    .gap(px(8.))
                                    .child(
                                        div()
                                            .truncate()
                                            .text_size(px(15.))
                                            .text_color(th().texte_fort)
                                            .font_weight(if conv.unread { FontWeight::BOLD } else { FontWeight::SEMIBOLD })
                                            .child(nom),
                                    )
                                    .child(
                                        div()
                                            .flex_none()
                                            .flex()
                                            .flex_row()
                                            .items_center()
                                            .gap(px(4.))
                                            .text_size(px(12.))
                                            .text_color(if conv.unread { th().accent } else { th().texte_pale })
                                            .when(epinglee, |d| d.child(icone("pin", 12., th().texte_pale)))
                                            .child(heure(conv.time, true)),
                                    ),
                            )
                            .child(
                                div()
                                    .flex()
                                    .flex_row()
                                    .items_center()
                                    .gap(px(5.))
                                    .when(conv.from_me, |d| {
                                        d.child(icone(
                                            if lu_par_eux { "check-check" } else { "check" },
                                            14.,
                                            if lu_par_eux { th().accent } else { th().texte_pale },
                                        ))
                                    })
                                    .child(
                                        div()
                                            .flex_1()
                                            .truncate()
                                            .text_size(px(13.))
                                            .child(texte_emoji(
                                                if conv.preview.trim().is_empty() {
                                                    t("📎 Pièce jointe").to_string()
                                                } else {
                                                    une_ligne(&conv.preview)
                                                },
                                                if conv.unread { th().texte_fort } else { th().apercu },

                                                None,
                                            )),
                                    )
                                    .when(conv.unread, |d| {
                                        d.child(div().flex_none().size(px(9.)).rounded(rayon(999.)).bg(degrade_accent()))
                                    }),
                            ),
                    ),
            );
        }
        col
    }

    // ---- Dessin : le fil ----

    /// Une pièce jointe : l'image elle-même, ou une pastille à ouvrir.
    fn piece(&self, m: &Msg, p: &Piece, cx: &mut Context<Self>) -> AnyElement {
        if p.est_audio() {
            return self.lecteur_vocal(m, p, cx);
        }
        let chemin = self.medias.get(&p.media_id).cloned();
        let id = SharedString::from(format!("p-{}-{}", m.id, p.media_id));
        if p.est_image() {
            let (w, h) = p.cadre();
            let cadre = div()
                .id(id)
                .w(px(w))
                .h(px(h))
                .rounded(rayon(18.))
                .overflow_hidden()
                .bg(ton(blanc(0.55), th().surface))
                .border_1()
                .border_color(ton(blanc(0.9), th().surface_bord))
                .shadow(ombre_flottante())
                .flex()
                .items_center()
                .justify_center();
            return match chemin {
                Some(chemin) => {
                    let ouvrir = chemin.clone();
                    cadre
                        .cursor_pointer()
                        .on_click(move |_, _, _| ouvrir_fichier(&ouvrir))
                        .child(img(chemin).size_full().rounded(rayon(18.)).object_fit(ObjectFit::Cover))
                        .into_any_element()
                }
                None => {
                    let echec = self.echecs.contains(&p.media_id);
                    let piece = p.clone();
                    cadre
                        .text_size(px(12.))
                        .text_color(c(if echec { DANGER } else { TEXTE_PALE }))
                        .when(echec, |d| {
                            d.cursor_pointer().on_click(cx.listener(move |this, _, _, cx| {
                                this.telecharger(&piece);
                                cx.notify();
                            }))
                        })
                        .child(if echec { t("Image indisponible · réessayer") } else { t("Chargement de l'image…") })
                        .into_any_element()
                }
            };
        }
        let nom_icone = if p.mime.starts_with("video/") {
            "film"
        } else if p.mime.starts_with("audio/") {
            "audio-lines"
        } else {
            "paperclip"
        };
        let nom = if p.name.is_empty() { p.mime.clone() } else { p.name.clone() };
        let attente = self.demandes.contains(&p.media_id);
        let piece = p.clone();
        div()
            .id(id)
            .flex()
            .flex_row()
            .items_center()
            .gap(px(10.))
            .max_w(px(320.))
            .px(px(14.))
            .py(px(10.))
            .rounded(rayon(18.))
            .bg(th().surface)
            .border_1()
            .border_color(th().surface_bord)
            .shadow(ombre_flottante())
            .cursor_pointer()
            .hover(|s| s.bg(ton(blanc(0.95), c(0x3a3a3c))))
            .on_click(cx.listener(move |this, _, _, cx| {
                if let Some(chemin) = this.medias.get(&piece.media_id) {
                    ouvrir_fichier(chemin);
                } else {
                    this.ouvrir_apres.insert(piece.media_id.clone());
                    this.telecharger(&piece);
                }
                cx.notify();
            }))
            .child(
                div()
                    .size(px(36.))
                    .flex_none()
                    .rounded(rayon(12.))
                    .bg(alpha(th().accent, 0.12))
                    .flex()
                    .items_center()
                    .justify_center()
                    .child(icone(nom_icone, 18., th().accent)),
            )
            .child(
                div()
                    .flex()
                    .flex_col()
                    .min_w(px(0.))
                    .child(div().truncate().text_size(px(13.)).text_color(th().texte_fort).child(nom))
                    .child(div().text_size(px(11.)).text_color(th().texte_pale).child(if attente {
                        t("Téléchargement…").to_string()
                    } else {
                        taille(p.size)
                    })),
            )
            .into_any_element()
    }

    /// Le fil défilant, et en dessous la copie floue qui sert de verre dépoli
    /// sous l'en-tête (voir `panneau_fil`).
    fn fil(&mut self, cx: &mut Context<Self>) -> AnyElement {
        let Some(contenu) = self.fil_contenu(false, cx) else {
            return div()
                .flex()
                .size_full()
                .items_center()
                .justify_center()
                .text_color(th().texte_pale)
                .child(t("Choisissez une conversation"))
                .into_any_element();
        };
        if self.descendre {
            self.descendre = false;
            self.descendre_bientot(cx);
        }
        div()
            .id("fil")
            .size_full()
            .overflow_y_scroll()
            .track_scroll(&self.defile)
            // La copie floue suit le défilement : on redessine à chaque cran.
            .on_scroll(cx.listener(|_, _, _, cx| cx.notify()))
            .child(contenu)
            .into_any_element()
    }

    /// Les messages de la conversation ouverte. Avec `flou`, la même mise en
    /// page, mais chaque bulle n'est plus qu'une ombre très floue de sa
    /// couleur, sans texte ni clic : GPUI n'a pas de flou d'arrière-plan,
    /// cette copie posée sous l'en-tête en tient lieu.
    fn fil_contenu(&self, flou: bool, cx: &mut Context<Self>) -> Option<Div> {
        let id = self.choisie.clone()?;
        let invisible = hsla(0., 0., 0., 0.);
        let tache = |couleur: Hsla, rayon: f32| {
            vec![BoxShadow {
                color: couleur,
                offset: point(px(0.), px(0.)),
                blur_radius: px(rayon),
                spread_radius: px(-2.),
                inset: false,
            }]
        };
        let groupe = self.conv(&id).is_some_and(|c| c.group);
        // Le fil passe sous l'en-tête et la barre d'envoi : la marge leur laisse la place.
        let mut col = div()
            .flex()
            .flex_col()
            .gap(px(4.))
            .px(px(22.))
            .pt(px(HAUT_ENTETE + 14. - MARGE_FIL))
            .pb(px(HAUT_COMPOSEUR + 10. - MARGE_FIL))
            .when(flou, |d| d.text_color(invisible));
        if self.chargement.as_deref() == Some(id.as_str()) {
            col = col.child(
                div()
                    .self_center()
                    .text_size(px(13.))
                    .text_color(if flou { invisible } else { th().texte_pale })
                    .child(t("Chargement…")),
            );
        } else if self.plus_anciens.get(&id).copied().unwrap_or(false) {
            let bouton = div()
                .self_center()
                .mb(px(8.))
                .px(px(14.))
                .py(px(5.))
                .rounded(rayon(999.))
                .border_1()
                .text_size(px(13.))
                .child(t("Messages plus anciens"));
            col = col.child(if flou {
                bouton.border_color(invisible).text_color(invisible).shadow(tache(ton(blanc(0.8), th().surface), 10.)).into_any_element()
            } else {
                bouton
                    .id("plus-vieux")
                    .bg(ton(blanc(0.6), th().surface))
                    .border_color(ton(blanc(0.85), th().surface_bord))
                    .text_color(ton(th().accent, c(0x6b9bff)))
                    .cursor_pointer()
                    .hover(|s| s.bg(ton(blanc(0.85), c(0x3a3a3c))))
                    .on_click(cx.listener(|this, _, _, cx| {
                        this.plus_vieux();
                        cx.notify();
                    }))
                    .into_any_element()
            });
        }
        let liste = self.messages.get(&id).cloned().unwrap_or_default();
        let mut jour_prec = String::new();
        for (i, m) in liste.iter().enumerate() {
            let j = jour(m.time);
            if j != jour_prec {
                col = col.child(
                    div()
                        .self_center()
                        .my(px(10.))
                        .px(px(12.))
                        .py(px(3.))
                        .rounded(rayon(999.))
                        .text_size(px(12.))
                        .when(flou, |d| d.shadow(tache(ton(blanc(0.7), th().jour_fond), 10.)))
                        .when(!flou, |d| d.bg(th().jour_fond).text_color(th().jour_texte))
                        .child(j.clone()),
                );
                jour_prec = j;
            }
            // Le dernier d'une suite du même côté porte l'heure.
            let dernier = liste.get(i + 1).is_none_or(|n| n.from_me != m.from_me || n.sender != m.sender);
            let premier = i == 0 || liste[i - 1].from_me != m.from_me || liste[i - 1].sender != m.sender;
            let mut ligne = div()
                .flex()
                .flex_col()
                .gap(px(4.))
                .when(m.from_me, |d| d.items_end())
                .when(!m.from_me, |d| d.items_start());
            if groupe && !m.from_me && premier && !m.sender.is_empty() {
                ligne = ligne.child(
                    div()
                        .ml(px(12.))
                        .text_size(px(12.))
                        .text_color(if flou { invisible } else { th().expediteur })
                        .when(th().sombre, |d| d.font_weight(FontWeight::SEMIBOLD))
                        .child(m.sender.clone()),
                );
            }
            for p in &m.attachments {
                ligne = ligne.child(if flou { self.piece_floue(p) } else { self.piece(m, p, cx) });
            }
            if m.attachments.is_empty() && !m.media.is_empty() {
                ligne = ligne.child(
                    div()
                        .flex()
                        .flex_row()
                        .items_center()
                        .gap(px(6.))
                        .px(px(12.))
                        .py(px(8.))
                        .rounded(rayon(16.))
                        .text_size(px(13.))
                        .when(flou, |d| d.shadow(tache(ton(blanc(0.85), th().surface), 16.)))
                        .when(!flou, |d| d.bg(ton(blanc(0.7), th().surface)).text_color(th().texte))
                        .child(icone("paperclip", 14., if flou { invisible } else { th().texte }))
                        .child(m.media.clone()),
                );
            }
            // En sombre, l'heure se loge dans la bulle, en bas à droite.
            let heure_dedans = th().sombre && !m.text.is_empty() && !self.details_heures;
            if !m.text.is_empty() {
                let bulle = div()
                    .max_w(px(520.))
                    .px(px(mesure(14., 12.)))
                    .py(px(mesure(8., 7.)))
                    .rounded(rayon(mesure(20., 18.)))
                    .border_1()
                    .when(heure_dedans, |d| d.flex().flex_row().flex_wrap().items_end().justify_end().gap_x(px(8.)));
                let couleur_heure = if flou {
                    invisible
                } else if statut(&m.status) == "Échec" {
                    th().danger
                } else if m.from_me {
                    th().heure_envoyee
                } else {
                    th().heure_recue
                };
                let mut dans_bulle = heure(m.time, false);
                if m.from_me && statut(&m.status) == "Échec" {
                    dans_bulle = format!("{dans_bulle} · {}", t("Échec"));
                }
                ligne = ligne.child(
                    match (flou, m.from_me) {
                        (true, true) => bulle.border_color(invisible).shadow(tache(tache_envoyee(), 24.)),
                        (true, false) => bulle.border_color(invisible).shadow(tache(ton(blanc(0.9), th().surface), 24.)),
                        (false, true) => bulle
                            .border_color(invisible)
                            .bg(degrade_accent())
                            .text_color(blanc(1.))
                            .shadow(ombre_flottante()),
                        (false, false) => {
                            bulle.bg(th().surface).border_color(th().surface_bord).text_color(th().texte_fort)
                        }
                    }
                    // Le texte peut rétrécir sous sa largeur d'une ligne : il se replie
                    // dans la bulle, et l'heure passe alors en dessous, à droite.
                    .child(div().min_w(px(0.)).child(if flou {
                        m.text.clone().into_any_element()
                    } else {
                        texte_emoji(m.text.clone(), if m.from_me { blanc(1.) } else { th().texte_fort }, None)
                            .into_any_element()
                    }))
                    .when(heure_dedans
, |d| {
                        d.child(div().flex_none().mb(px(-1.)).text_size(px(11.)).text_color(couleur_heure).child(dans_bulle))
                    }),
                );
            }
            if self.details_heures || (!heure_dedans && (dernier || statut(&m.status) == "Échec")) {
                let mut pied =
                    if self.details_heures { format!("{} · {}", jour(m.time), heure(m.time, false)) } else { heure(m.time, false) };
                if m.from_me {
                    let s = statut(&m.status);
                    if !s.is_empty() {
                        pied = format!("{pied} · {}", t(s));
                    }
                }
                if !m.kind.is_empty() {
                    pied = format!("{pied} · {}", m.kind.to_uppercase());
                }
                let couleur = if flou {
                    invisible
                } else if statut(&m.status) == "Échec" {
                    th().danger
                } else {
                    th().texte_pale
                };
                ligne = ligne.child(div().mx(px(8.)).mb(px(6.)).text_size(px(11.)).text_color(couleur).child(pied));
            } else if heure_dedans && dernier {
                // La fin d'une suite garde son souffle.
                ligne = ligne.child(div().h(px(4.)));
            }
            col = col.child(ligne);
        }
        Some(col)
    }

    /// Un vocal : lecture/pause, forme d'onde qui se remplit, durée.
    fn lecteur_vocal(&self, m: &Msg, p: &Piece, cx: &mut Context<Self>) -> AnyElement {
        let onde = self.ondes.get(&p.media_id);
        let duree = onde.map(|o| o.1).unwrap_or(0.);
        let en_cours = self.lecture.as_ref().filter(|l| l.media_id == p.media_id);
        let pos = en_cours.map(|l| l.position()).or_else(|| self.pauses.get(&p.media_id).copied()).unwrap_or(0.);
        let fraction = if duree > 0. { (pos / duree).clamp(0., 1.) } else { 0. };
        let (fort, pale, fond_bouton, icone_bouton) = if th().ega {
            // Couleurs EGA pleines : bouton gris clair à triangle noir, barres
            // jouées en blanc, les autres en cyan vif (sur le bleu) ou en gris.
            let pale = if m.from_me { c(EGA_CYAN_VIF) } else { c(EGA_GRIS) };
            (c(EGA_BLANC), pale, gpui::Background::from(c(EGA_GRIS_CLAIR)), c(EGA_NOIR))
        } else if m.from_me {
            (blanc(1.), blanc(0.45), gpui::Background::from(blanc(0.95)), th().accent)
        } else if th().sombre {
            (blanc(1.), blanc(0.3), gpui::Background::from(blanc(1.)), c(0x2c2c2e))
        } else {
            (th().accent, alpha(th().accent, 0.28), degrade_accent(), blanc(1.))
        };
        let pret = self.medias.contains_key(&p.media_id);
        let mut barres = div().flex().flex_row().items_center().gap(px(2.)).h(px(30.));
        for i in 0..BARRES {
            let h = onde.map(|o| o.0[i]).unwrap_or(0.15);
            let jouee = (i as f32 + 0.5) / BARRES as f32 <= fraction;
            barres = barres.child(div().w(px(3.)).h(px(4. + 26. * h)).rounded(rayon(2.)).bg(if jouee { fort } else { pale }));
        }
        let temps = if en_cours.is_some() || pos > 0. { pos } else { duree };
        let piece = p.clone();
        div()
            .id(SharedString::from(format!("v-{}-{}", m.id, p.media_id)))
            .flex()
            .flex_row()
            .items_center()
            .gap(px(10.))
            .pl(px(6.))
            .pr(px(14.))
            .py(px(6.))
            .rounded(rayon(999.))
            .when(m.from_me, |d| d.bg(degrade_accent()).shadow(ombre_flottante()))
            .when(!m.from_me, |d| d.bg(th().surface).border_1().border_color(th().surface_bord))
            .child(
                div()
                    .id(SharedString::from(format!("vb-{}-{}", m.id, p.media_id)))
                    .size(px(36.))
                    .flex_none()
                    .rounded(rayon(999.))
                    .bg(fond_bouton)
                    .flex()
                    .items_center()
                    .justify_center()
                    .cursor_pointer()
                    .hover(|s| s.opacity(0.85))
                    .on_click(cx.listener(move |this, _, _, cx| {
                        this.basculer_lecture(&piece);
                        cx.notify();
                    }))
                    .child(icone(
                        if !pret { "loader" } else if en_cours.is_some() { "pause" } else { "play" },
                        16.,
                        icone_bouton,
                    )),
            )
            .child(barres)
            .child(
                div()
                    .w(px(34.))
                    .text_size(px(12.))
                    .text_color(match (th().ega, m.from_me) {
                        (true, true) => c(EGA_BLANC),
                        (true, false) => th().texte,
                        (false, true) => blanc(0.9),
                        (false, false) => ton(th().texte, th().heure_recue),
                    })
                    .child(if duree > 0. || pos > 0. {
                        format!("{}:{:02}", temps as u32 / 60, temps as u32 % 60)
                    } else {
                        "··".into()
                    }),
            )
            .into_any_element()
    }

    /// La version floue d'une pièce jointe : même taille, une tache de couleur.
    fn piece_floue(&self, p: &Piece) -> AnyElement {
        let ombre = |couleur: Hsla| {
            vec![BoxShadow {
                color: couleur,
                offset: point(px(0.), px(0.)),
                blur_radius: px(20.),
                spread_radius: px(-2.),
                inset: false,
            }]
        };
        if p.est_image() {
            let (w, h) = p.cadre();
            return div().w(px(w)).h(px(h)).rounded(rayon(18.)).shadow(ombre(ton(ca(0x8a90b8, 0.7), c(0x3a3a3c)))).into_any_element();
        }
        if p.est_audio() {
            // 6 + 36 + 10 + barres + 10 + 34 + 14 de large, 48 de haut.
            let w = 6. + 36. + 10. + BARRES as f32 * 5. - 2. + 10. + 34. + 14.;
            return div()
                .w(px(w))
                .h(px(48.))
                .rounded(rayon(999.))
                .shadow(ombre(tache_envoyee()))
                .into_any_element();
        }
        let nom = if p.name.is_empty() { p.mime.clone() } else { p.name.clone() };
        let invisible = hsla(0., 0., 0., 0.);
        div()
            .flex()
            .flex_row()
            .items_center()
            .gap(px(10.))
            .max_w(px(320.))
            .px(px(14.))
            .py(px(10.))
            .rounded(rayon(18.))
            .border_1()
            .border_color(invisible)
            .shadow(ombre(ton(blanc(0.9), th().surface)))
            .child(div().size(px(36.)).flex_none())
            .child(
                div()
                    .flex()
                    .flex_col()
                    .min_w(px(0.))
                    .text_color(invisible)
                    .child(div().truncate().text_size(px(13.)).child(nom))
                    .child(div().text_size(px(11.)).child(taille(p.size))),
            )
            .into_any_element()
    }

    fn bouton_rond(id: &'static str, nom: &'static str, plein: bool) -> gpui::Stateful<Div> {
        div()
            .id(id)
            .size(px(42.))
            .flex_none()
            .rounded(rayon(999.))
            .flex()
            .items_center()
            .justify_center()
            .cursor_pointer()
            .when(plein, |d| d.bg(degrade_accent()).shadow(ombre_flottante()))
            .when(!plein, |d| {
                d.bg(th().surface).border_1().border_color(th().surface_bord).shadow(ombre_flottante())
            })
            .hover(|s| s.opacity(0.88))
            .child(icone(nom, 20., if plein { blanc(1.) } else { ton(th().texte_fort, c(0xd1d1d6)) }))
    }

    fn composeur(&self, conv: Option<&Conv>, cx: &mut Context<Self>) -> AnyElement {
        if conv.is_some_and(|c| c.read_only) {
            return div()
                .text_center()
                .text_size(px(13.))
                .text_color(th().texte_pale)
                .child(t("Conversation en lecture seule"))
                .into_any_element();
        }
        let centre: AnyElement = match &self.vocal {
            Some(v) => {
                let s = v.debut.elapsed().as_secs();
                div()
                    .flex_1()
                    .flex()
                    .flex_row()
                    .items_center()
                    .gap(px(8.))
                    .child(div().size(px(10.)).rounded(rayon(10.)).bg(th().danger))
                    .child(
                        div()
                            .text_color(th().texte_fort)
                            .child(format!("Enregistrement {}:{:02} · ↑ pour envoyer", s / 60, s % 60)),
                    )
                    .child(div().flex_1())
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
                div().flex_1().min_w(px(0.)).child(Input::new(&self.saisie).appearance(false)).into_any_element()
            }
        };
        div()
            .flex()
            .flex_row()
            .items_center()
            .gap(px(10.))
            .child(Self::bouton_rond("joindre", "plus", false).on_click(cx.listener(|this, _, _, cx| this.joindre(cx))))
            .child(
                verre(0.88, 999.)
                    .shadow(ombre_flottante())
                    .when(th().sombre, |d| d.bg(th().surface))
                    .flex_1()
                    .min_w(px(0.))
                    .h(px(44.))
                    .flex()
                    .flex_row()
                    .items_center()
                    .gap(px(8.))
                    .pl(px(12.))
                    .pr(px(10.))
                    .child(logo(self.choisie.as_deref().map(service_de).unwrap_or(Service::GoogleMessages), 20.))
                    .child(centre)
                    .child(
                        div()
                            .id("emojis")
                            .cursor_pointer()
                            .rounded(rayon(8.))
                            .p(px(3.))
                            .hover(|s| s.bg(ton(blanc(0.8), c(0x3a3a3c))))
                            .on_click(cx.listener(|this, _, _, cx| {
                                this.emojis_ouverts = !this.emojis_ouverts;
                                cx.notify();
                            }))
                            .child(icone("face-slightly-smiling", 21., if self.emojis_ouverts { th().accent } else { th().icone })),
                    )
                    .child(
                        div()
                            .id("micro")
                            .cursor_pointer()
                            .rounded(rayon(8.))
                            .p(px(3.))
                            .hover(|s| s.bg(ton(blanc(0.8), c(0x3a3a3c))))
                            .on_click(cx.listener(|this, _, _, cx| {
                                this.basculer_vocal();
                                cx.notify();
                            }))
                            .child(icone("audio-lines", 21., if self.vocal.is_some() { th().danger } else { th().icone })),
                    ),
            )
            .child(
                // En sombre, le bouton d'envoi ne s'allume que s'il y a quelque chose à envoyer.
                Self::bouton_rond(
                    "envoyer",
                    "arrow-up",
                    !th().sombre || self.vocal.is_some() || !self.saisie.read(cx).value().trim().is_empty(),
                )
                    .on_click(cx.listener(|this, _, window, cx| this.envoyer(window, cx))),
            )
            .into_any_element()
    }

    fn panneau_emojis(&self, cx: &mut Context<Self>) -> impl IntoElement {
        let choix: Vec<&'static str> = if !self.emoji_filtre.is_empty() {
            catalogue().iter().filter(|(_, _, nom)| nom.contains(&self.emoji_filtre)).map(|(_, e, _)| *e).take(400).collect()
        } else if self.emoji_groupe == "Récents" {
            // Les récents vivent dans les réglages : on retrouve leur &'static dans le catalogue.
            self.reglages
                .emojis_recents
                .iter()
                .filter_map(|r| catalogue().iter().find(|(_, e, _)| e == r).map(|(_, e, _)| *e))
                .collect()
        } else {
            catalogue().iter().filter(|(g, _, _)| *g == self.emoji_groupe).map(|(_, e, _)| *e).collect()
        };
        let mut onglets = div().flex().flex_row().gap(px(2.)).px(px(6.)).pb(px(6.));
        for (i, (groupe, emoji)) in GROUPES_EMOJI.iter().enumerate() {
            let actif = self.emoji_filtre.is_empty() && self.emoji_groupe == *groupe;
            let groupe = *groupe;
            onglets = onglets.child(
                div()
                    .id(("onglet-emoji", i))
                    .size(px(34.))
                    .rounded(rayon(10.))
                    .flex()
                    .items_center()
                    .justify_center()
                    .font_family(POLICE_EMOJI)
                    .text_size(px(19.))
                    .cursor_pointer()
                    .when(actif, |d| d.bg(ton(blanc(0.95), th().menu_champ)).shadow(ombre_flottante()))
                    .when(!actif, |d| d.opacity(0.75).hover(|s| s.bg(ton(blanc(0.7), th().menu_champ)).opacity(1.)))
                    .on_click(cx.listener(move |this, _, window, cx| {
                        this.emoji_groupe = groupe;
                        this.emoji_filtre.clear();
                        this.emoji_recherche.update(cx, |s, cx| s.set_value("", window, cx));
                        cx.notify();
                    }))
                    .child(*emoji),
            );
        }
        let mut grille = div().flex().flex_row().flex_wrap().gap(px(2.)).px(px(6.)).pb(px(8.));
        if choix.is_empty() {
            grille = grille.child(div().p(px(14.)).text_size(px(13.)).text_color(th().texte_pale).child(
                if self.emoji_groupe == "Récents" && self.emoji_filtre.is_empty() {
                    t("Les emojis que vous utilisez apparaîtront ici.")
                } else {
                    t("Aucun emoji")
                },
            ));
        }
        for (i, e) in choix.into_iter().enumerate() {
            grille = grille.child(
                div()
                    .id(("emoji", i))
                    .size(px(40.))
                    .rounded(rayon(10.))
                    .flex()
                    .items_center()
                    .justify_center()
                    .font_family(POLICE_EMOJI)
                    .text_size(px(24.))
                    .cursor_pointer()
                    .hover(|s| s.bg(ton(blanc(0.95), th().menu_champ)))
                    .on_click(cx.listener(move |this, _, window, cx| {
                        this.saisie.update(cx, |s, cx| {
                            let v = format!("{}{e}", s.value());
                            s.set_value(v, window, cx);
                            s.focus(window, cx);
                        });
                        let r = &mut this.reglages.emojis_recents;
                        r.retain(|x| x != e);
                        r.insert(0, e.to_string());
                        r.truncate(32);
                        this.reglages.garder();
                        cx.notify();
                    }))
                    .child(e),
            );
        }
        verre(0.94, 22.)
            .when(th().sombre, |d| d.bg(th().menu).border_1().border_color(th().trait_fin).rounded(rayon(14.)))
            .shadow(ombre_verre())
            .w(px(392.))
            .h(px(380.))
            .flex()
            .flex_col()
            .pt(px(10.))
            .child(
                div().px(px(10.)).pb(px(8.)).child(
                    verre(0.8, 999.)
                        .when(th().sombre, |d| d.bg(th().menu_champ))
                        .flex()
                        .flex_row()
                        .items_center()
                        .gap(px(6.))
                        .pl(px(12.))

                        .pr(px(6.))
                        .py(px(1.))
                        .child(icone("search", 14., th().texte_pale))
                        .child(div().flex_1().child(Input::new(&self.emoji_recherche).appearance(false).cleanable(true))),
                ),
            )
            .child(onglets)
            .child(div().id("grille-emojis").flex_1().min_h(px(0.)).overflow_y_scroll().child(grille))
    }

    fn panneau_fil(&mut self, cx: &mut Context<Self>) -> impl IntoElement {
        let choisie = self.choisie.as_ref().and_then(|id| self.conv(id)).cloned();
        // (i) : l'heure exacte de chaque message, ou non.
        let details = self.details_heures;
        let bouton_info = div()
            .id("details-heures")
            .size(px(28.))
            .rounded(rayon(999.))
            .flex()
            .items_center()
            .justify_center()
            .cursor_pointer()
            .when(details, |d| d.bg(alpha(th().accent, 0.15)))
            .hover(|s| s.bg(th().survol))
            .on_click(cx.listener(|this, _, _, cx| {
                this.details_heures = !this.details_heures;
                cx.notify();
            }))
            .child(icone("info", 17., if details { th().accent } else { th().texte_pale }));
        let service = self.choisie.as_deref().map(service_de).unwrap_or(Service::GoogleMessages);
        let menu_ouvert = self.menu_conv;
        let bouton_options = div()
            .id("options-conv")
            .size(px(28.))
            .rounded(rayon(999.))
            .flex()
            .items_center()
            .justify_center()
            .cursor_pointer()
            .when(menu_ouvert, |d| d.bg(th().survol))
            .hover(|s| s.bg(th().survol))
            .on_click(cx.listener(|this, _, _, cx| {
                this.menu_conv = !this.menu_conv;
                this.confirmer_suppression = false;
                cx.notify();
            }))
            .child(icone("ellipsis", 17., th().texte_pale));
        let conv_medias = self.choisie.clone();
        let bouton_medias = div()
            .id("medias-conv")
            .size(px(28.))
            .rounded(rayon(999.))
            .flex()
            .items_center()
            .justify_center()
            .cursor_pointer()
            .hover(|s| s.bg(th().survol))
            .on_click(cx.listener(move |this, _, _, cx| {
                this.ouvrir_galerie(conv_medias.clone());
                cx.notify();
            }))
            .child(icone("images", 17., th().texte_pale));
        let badge_service = |d: Div| d.child(div().absolute().right(px(-3.)).bottom(px(-2.)).child(logo(service, 16.)));

        // L'en-tête flotte sur le fil : un voile dégradé fait disparaître les messages qui passent dessous.
        // En retrait d'un pixel et arrondi comme le panneau : GPUI ne découpe pas
        // les enfants selon les coins arrondis, le voile garderait des coins carrés.
        let givre = |a: f32| alpha(th().givre_fil, a);
        let r = (th().rayon_fil - 1.).max(0.);
        let haut_voile = HAUT_ENTETE + 26.;
        // Le verre dépoli, en trois couches sous la rangée de l'en-tête :
        // 1. un fond givré opaque qui cache le vrai fil, puis s'efface sur 26 px ;
        // 2. la copie floue du fil (taches de couleur), calée sur le défilement,
        //    tenue loin des coins arrondis (GPUI ne découpe qu'en rectangle) ;
        // 3. un lavis givré qui adoucit les taches et les fond dans le bas.
        const BORD: f32 = 16.;
        let decalage = self.defile.offset().y + px(MARGE_FIL - 1. - 6.);
        // L'EGA n'a ni flou ni verre : un en-tête noir plein, filet en dessous.
        let ega = th().ega;
        let copie_floue = (!ega).then(|| self.fil_contenu(true, cx)).flatten().map(|contenu| {
            div()
                .absolute()
                .top(px(6.))
                .left(px(BORD))
                .right(px(BORD))
                .h(px(HAUT_ENTETE - 6.))
                .overflow_hidden()
                .child(div().absolute().top(decalage).left(px(-1. - BORD)).right(px(-1. - BORD)).child(contenu))
                // Les bords de la découpe se fondent dans le givre opaque d'en dessous :
                // pas d'arête nette, ni en haut, ni sur les côtés.
                .child(div().absolute().top_0().left_0().right_0().h(px(18.)).bg(linear_gradient(
                    180.,
                    linear_color_stop(givre(1.), 0.),
                    linear_color_stop(givre(0.), 1.),
                )))
                .child(div().absolute().top_0().bottom_0().left_0().w(px(48.)).bg(linear_gradient(
                    90.,
                    linear_color_stop(givre(1.), 0.),
                    linear_color_stop(givre(0.), 1.),
                )))
                .child(div().absolute().top_0().bottom_0().right_0().w(px(48.)).bg(linear_gradient(
                    90.,
                    linear_color_stop(givre(0.), 0.),
                    linear_color_stop(givre(1.), 1.),
                )))
        });
        let entete = div()
            .absolute()
            .top(px(1.))
            .left(px(1.))
            .right(px(1.))
            .rounded_t(rayon(r))
            .when(!ega, |d| {
                d.h(px(haut_voile)).bg(linear_gradient(
                    180.,
                    linear_color_stop(givre(1.), HAUT_ENTETE / haut_voile),
                    linear_color_stop(givre(0.), 1.),
                ))
            })
            .when(ega, |d| d.h(px(HAUT_ENTETE)).bg(givre(1.)).border_b_1().border_color(th().trait_fin))
            .children(copie_floue)
            .child(
                div()
                    .absolute()
                    .top_0()
                    .left_0()
                    .right_0()
                    .bottom_0()
                    .rounded_t(rayon(r))
                    .bg(linear_gradient(
                        180.,
                        linear_color_stop(givre(0.55), 0.),
                        linear_color_stop(givre(0.), 1.),
                    )),
            )
            // Le bas de la copie se fond dans le givre opaque, avant le dégradé.
            .child(
                div()
                    .absolute()
                    .top(px(HAUT_ENTETE - 22.))
                    .left_0()
                    .right_0()
                    .h(px(22.))
                    .bg(linear_gradient(180., linear_color_stop(givre(0.), 0.), linear_color_stop(givre(1.), 1.))),
            )
            .child(
                div().h(px(HAUT_ENTETE)).flex().flex_row().items_center().gap(px(12.)).px(px(20.)).when_some(
                    choisie.clone(),
                    |d, conv| {
                        let nom = if conv.name.is_empty() { t("Sans nom").to_string() } else { conv.name.clone() };
                        let detail = if conv.group {
                            format!("{} participants · {}", conv.members.len() + 1, service.nom())
                        } else {
                            let qui = conv.members.first().filter(|m| **m != nom).cloned();
                            match qui {
                                Some(q) => format!("{q} · {}", service.nom()),
                                None => service.nom().into(),
                            }
                        };
                        d.child(badge_service(
                            div().relative().child(self.avatar_conv(&conv, &nom, 38.)),
                        ))
                        .child(
                            div()
                                .flex()
                                .flex_col()
                                .flex_1()
                                .min_w(px(0.))
                                .child(div().truncate().font_weight(FontWeight::SEMIBOLD).text_color(th().texte_fort).child(nom))
                                .child(div().truncate().text_size(px(12.)).text_color(th().texte_pale).child(detail)),
                        )
                        .child(bouton_medias)
                        .child(bouton_info)
                        .child(bouton_options)
                        .child(
                            div()
                                .px(px(10.))
                                .py(px(2.))
                                .rounded(rayon(999.))
                                .text_size(px(11.))
                                .font_weight(FontWeight::SEMIBOLD)
                                .when(conv.rcs, |d| d.bg(alpha(th().accent, 0.12)).text_color(th().accent).child("RCS"))
                                .when(!conv.rcs, |d| d.bg(alpha(th().succes, 0.12)).text_color(th().succes).child("SMS")),
                        )
                    },
                ),
            );

        // La barre d'envoi flotte elle aussi, sur un fondu vers le bas.
        let composeur = div()
            .absolute()
            .bottom(px(1.))
            .left(px(1.))
            .right(px(1.))
            .rounded_b(rayon(r))
            .px(px(16.))
            .pb(px(14.))
            .when(!ega, |d| {
                d.pt(px(26.)).bg(linear_gradient(
                    0.,
                    linear_color_stop(alpha(th().givre_fil, mesure(0.9, 1.)), 0.35),
                    linear_color_stop(alpha(th().givre_fil, 0.), 1.),
                ))
            })
            .when(ega, |d| d.pt(px(14.)).bg(th().givre_fil).border_t_1().border_color(th().trait_fin))
            .child(self.composeur(choisie.as_ref(), cx));

        let fil = self.fil(cx);
        panneau(th().fil, th().rayon_fil)
            .relative()
            .flex_1()
            .h_full()
            // En sombre, le fil est un panneau noir posé avec une petite marge.
            .when(th().marge_fil > 0., |d| d.h_auto().m(px(th().marge_fil)))
            .overflow_hidden()

            // Le fil ne dessine pas sur les coins arrondis du panneau : il commence
            // et finit sous les voiles de l'en-tête et de la barre d'envoi.
            .child(div().absolute().top(px(MARGE_FIL)).left_0().right_0().bottom(px(MARGE_FIL)).child(fil))
            .child(entete)
            .child(composeur)
            // En EGA, le fil est une boîte de dialogue DOS à double trait (╔═╗) :
            // un second cadre en retrait par-dessus le contenu, sans clic.
            .when(ega, |d| {
                d.child(div().absolute().top(px(3.)).left(px(3.)).right(px(3.)).bottom(px(3.)).border_1().border_color(c(EGA_GRIS_CLAIR)))
            })
            .when(self.emojis_ouverts, |d| {
                d.child(div().absolute().right(px(70.)).bottom(px(HAUT_COMPOSEUR + 4.)).child(self.panneau_emojis(cx)))
            })
            .when(self.menu_conv && choisie.is_some(), |d| {
                d.child(div().absolute().right(px(16.)).top(px(HAUT_ENTETE - 4.)).child(self.menu_conversation(cx)))
            })
    }

    /// Le menu « … » : médias, archiver, supprimer.
    fn menu_conversation(&self, cx: &mut Context<Self>) -> impl IntoElement {
        let Some(id) = self.choisie.clone() else { return div() };
        let google = service_de(&id) == Service::GoogleMessages;
        let archivee = self.archives.iter().any(|c| c.id == id);
        let item = |cle: &'static str, ic: &'static str, texte: String, couleur: Hsla, actif: bool| {
            div()
                .id(cle)
                .flex()
                .flex_row()
                .items_center()
                .gap(px(10.))
                .px(px(12.))
                .py(px(9.))
                .rounded(rayon(10.))
                .text_size(px(14.))
                .text_color(couleur)
                .when(actif, |d| d.cursor_pointer().hover(|s| s.bg(th().survol)))
                .when(!actif, |d| d.opacity(0.45))
                .child(icone(ic, 16., couleur))
                .child(texte)
        };
        let id_m = id.clone();
        let id_a = id.clone();
        let id_f = id.clone();
        let favori = self.reglages.head.contains(&id);
        let etoile = self.reglages.etoiles.contains(&id);
        let id_t = id.clone();
        let epinglee = self.reglages.epingles.contains(&id);
        let id_e = id.clone();
        let mut menu = verre(0.96, 16.)
            .shadow(ombre_verre())
            .w(px(240.))
            .p(px(6.))
            .flex()
            .flex_col()
            .child(
                item(
                    "menu-head",
                    "layout-grid",
                    if favori { t("Retirer du Head") } else { t("Ajouter au Head") }.into(),
                    th().texte_fort,
                    true,
                )
                .on_click(cx.listener(move |this, _, _, cx| {
                    let f = &mut this.reglages.head;
                    if favori {
                        f.retain(|x| x != &id_f);
                    } else {
                        f.push(id_f.clone());
                    }
                    this.reglages.garder();
                    this.menu_conv = false;
                    cx.notify();
                })),
            )
            .child(
                item(
                    "menu-etoile",
                    "star",
                    if etoile { t("Retirer des favoris") } else { t("Ajouter aux favoris") }.into(),
                    th().texte_fort,
                    true,
                )
                .on_click(cx.listener(move |this, _, _, cx| {
                    let f = &mut this.reglages.etoiles;
                    if etoile {
                        f.retain(|x| x != &id_t);
                    } else {
                        f.push(id_t.clone());
                    }
                    this.reglages.garder();
                    this.menu_conv = false;
                    cx.notify();
                })),
            )
            .child(
                item(
                    "menu-epingle",
                    if epinglee { "pin-off" } else { "pin" },
                    if epinglee { t("Désépingler") } else { t("Épingler en haut") }.into(),
                    th().texte_fort,
                    true,
                )
                .on_click(cx.listener(move |this, _, _, cx| {
                    let e = &mut this.reglages.epingles;
                    if epinglee {
                        e.retain(|x| x != &id_e);
                    } else {
                        e.push(id_e.clone());
                    }
                    this.reglages.garder();
                    this.menu_conv = false;
                    cx.notify();
                })),
            )
            .child(item("menu-medias", "images", t("Médias").into(), th().texte_fort, true).on_click(cx.listener(
                move |this, _, _, cx| {
                    this.menu_conv = false;
                    this.ouvrir_galerie(Some(id_m.clone()));
                    cx.notify();
                },
            )))
            .child(
                item(
                    "menu-archiver",
                    "archive",
                    if archivee { t("Désarchiver") } else { t("Archiver") }.into(),
                    th().texte_fort,
                    google,
                )
                .when(google, |d| {
                    d.on_click(cx.listener(move |this, _, _, cx| {
                        this.menu_conv = false;
                        this.commande(json!({"cmd": "archive", "conv": id_a, "archived": !archivee}));
                        // Tout de suite à l'écran ; le téléphone confirmera.
                        if archivee {
                            if let Some(i) = this.archives.iter().position(|c| c.id == id_a) {
                                let mut c = this.archives.remove(i);
                                c.archived = false;
                                this.convs.push(c);
                                this.convs.sort_by(|a, b| b.time.cmp(&a.time));
                            }
                        } else if let Some(i) = this.convs.iter().position(|c| c.id == id_a) {
                            let mut c = this.convs.remove(i);
                            c.archived = true;
                            this.archives.insert(0, c);
                            this.choisie = None;
                        }
                        cx.notify();
                    }))
                }),
            );
        if !google {
            menu = menu.child(
                div().px(px(12.)).py(px(6.)).text_size(px(12.)).text_color(th().texte_pale).child(t("Archiver et supprimer : bientôt pour Messenger.")),
            );
        }
        let id_s = id.clone();
        menu.child(div().h(px(1.)).mx(px(8.)).my(px(4.)).bg(th().survol)).child(
            item(
                "menu-supprimer",
                "trash",
                if self.confirmer_suppression { t("Confirmer la suppression") } else { t("Supprimer la conversation") }.into(),
                th().danger,
                google,
            )
            .when(self.confirmer_suppression, |d| d.bg(alpha(th().danger, 0.12)))
            .when(google, |d| {
                d.on_click(cx.listener(move |this, _, _, cx| {
                    if !this.confirmer_suppression {
                        this.confirmer_suppression = true;
                    } else {
                        this.commande(json!({"cmd": "delete", "conv": id_s}));
                        this.convs.retain(|c| c.id != id_s);
                        this.archives.retain(|c| c.id != id_s);
                        this.messages.remove(&id_s);
                        this.choisie = None;
                        this.menu_conv = false;
                        this.confirmer_suppression = false;
                    }
                    cx.notify();
                }))
            }),
        )
    }

    // ---- Dessin : les réglages ----

    fn reglages_vue(&self, cx: &mut Context<Self>) -> impl IntoElement {
        let section = self.section.unwrap_or(Section::Comptes);
        let item = |id: &'static str, s: Section, nom: &'static str, ic: &'static str, couleur: u32, compte: Option<usize>| {
            div()
                .id(id)
                .flex()
                .flex_row()
                .items_center()
                .gap(px(12.))
                .px(px(10.))
                .py(px(9.))
                .rounded(rayon(14.))
                .cursor_pointer()
                .when(section == s, |d| d.bg(ton(blanc(0.85), th().choisi)).shadow(ombre_flottante()))
                .when(section != s, |d| d.hover(|st| st.bg(ton(blanc(0.5), th().survol))))
                .child(
                    div()
                        .size(px(30.))
                        .rounded(rayon(9.))
                        .bg(if th().ega { ega_fond(c(couleur)) } else { c(couleur) })
                        .flex()
                        .items_center()
                        .justify_center()
                        .child(icone(ic, 17., blanc(1.))),
                )
                .child(div().flex_1().text_color(th().texte_fort).child(nom))
                .when_some(compte, |d, n| {
                    d.child(
                        div()
                            .px(px(8.))
                            .rounded(rayon(999.))
                            .bg(ton(blanc(0.7), c(0x48484a)))
                            .text_size(px(12.))
                            .text_color(th().texte)
                            .child(n.to_string()),
                    )
                })
        };
        let comptes_actifs = usize::from(self.etat.associe());
        let gauche = panneau(th().liste, th().rayon_liste)
            .w(px(300.))
            .h_full()
            .flex()
            .flex_col()
            .gap(px(4.))
            .p(px(14.))
            .child(
                div()
                    .px(px(8.))
                    .pt(px(4.))
                    .pb(px(10.))
                    .text_size(px(mesure(22., 18.)))
                    .font_weight(FontWeight::BOLD)
                    .text_color(th().texte_fort)
                    .child(t("Réglages")),
            )
            .child(
                div()
                    .flex()
                    .flex_row()
                    .items_center()
                    .gap(px(12.))
                    .px(px(10.))
                    .pb(px(12.))
                    .mb(px(6.))
                    .border_b_1()
                    .border_color(ton(blanc(0.7), th().trait_fin))
                    .child(
                        div()
                            .id("moi-avatar")
                            .relative()
                            .cursor_pointer()
                            .hover(|s| s.opacity(0.85))
                            .on_click(cx.listener(|this, _, _, cx| {
                                this.menu_avatar = !this.menu_avatar;
                                cx.notify();
                            }))
                            .child(self.mon_avatar("J", 40.))
                            .child(
                                div()
                                    .absolute()
                                    .right(px(-3.))
                                    .bottom(px(-3.))
                                    .size(px(18.))
                                    .rounded(rayon(18.))
                                    .bg(ton(blanc(1.), c(0x3a3a3c)))
                                    .border_1()
                                    .border_color(ton(rgba(0x0000001a).into(), th().liste))
                                    .flex()
                                    .items_center()
                                    .justify_center()
                                    .child(icone("pencil", 10., th().texte)),
                            ),
                    )
                    .child(
                        div()
                            .flex()
                            .flex_col()
                            .min_w(px(0.))
                            .child(div().font_weight(FontWeight::SEMIBOLD).text_color(th().texte_fort).child(t("Moi")))
                            .child(
                                div()
                                    .truncate()
                                    .text_size(px(12.))
                                    .text_color(th().texte_pale)
                                    .child(self.reglages.email.clone().unwrap_or_else(|| t("Compte local").into())),
                            ),
                    ),
            )
            .when(self.menu_avatar, |d| d.child(self.menu_avatar_vue(cx)))
            .child(
                item("s-comptes", Section::Comptes, t("Comptes"), "at-sign", ACCENT_2, Some(comptes_actifs)).on_click(
                    cx.listener(|this, _, _, cx| {
                        this.section = Some(Section::Comptes);
                        cx.notify();
                    }),
                ),
            )
            .child(item("s-notifs", Section::Notifications, t("Notifications"), "bell", SUCCES, None).on_click(
                cx.listener(|this, _, _, cx| {
                    this.section = Some(Section::Notifications);
                    cx.notify();
                }),
            ))
            .child(item("s-apparence", Section::Apparence, t("Apparence"), "palette", ROSE, None).on_click(
                cx.listener(|this, _, _, cx| {
                    this.section = Some(Section::Apparence);
                    cx.notify();
                }),
            ))
            .child(item("s-langue", Section::Langue, t("Langue"), "languages", 0x0ea5e9, None).on_click(cx.listener(
                |this, _, _, cx| {
                    this.section = Some(Section::Langue);
                    cx.notify();
                },
            )))
            .child(item("s-apropos", Section::APropos, t("À propos"), "info", 0x64748b, None).on_click(cx.listener(
                |this, _, _, cx| {
                    this.section = Some(Section::APropos);
                    cx.notify();
                },
            )));

        let contenu: AnyElement = match section {
            // L'écran de connexion Google seulement si Google n'est pas connecté ;
            // sinon « Ajouter un compte » montre la liste (Messenger s'y connecte).
            Section::Comptes
                if (self.ajout_compte && !self.etat.associe())
                    || (self.etat == Etat::NonAssocie && self.connexion_ouverte) =>
            {
                self.ecran_connexion(cx).into_any_element()
            }
            Section::Comptes => self.comptes(cx).into_any_element(),
            Section::Notifications => carte_section("Notifications")
                .child(
                    groupe()
                        .child(ligne_interrupteur(
                            "n-bureau",
                            t("Notifications de bureau"),
                            t("Une bulle quand un message arrive et que Jasette n'est pas au premier plan."),
                            self.reglages.notifications,
                            cx.listener(|this, _, _, cx| {
                                this.reglages.notifications = !this.reglages.notifications;
                                this.reglages.garder();
                                cx.notify();
                            }),
                        ))
                        .child(separateur())
                        .child(ligne_interrupteur(
                            "n-apercu",
                            t("Aperçu du message"),
                            t("Montrer le texte du message dans la notification."),
                            self.reglages.apercu,
                            cx.listener(|this, _, _, cx| {
                                this.reglages.apercu = !this.reglages.apercu;
                                this.reglages.garder();
                                cx.notify();
                            }),
                        )),
                )
                .into_any_element(),
            Section::Apparence => {
                // Les rangées d'un groupe arrondi : GPUI ne découpe pas les enfants
                // selon les coins, le fond de survol prend donc lui-même les coins
                // du groupe (moins son liseré) en haut pour la première, en bas pour la dernière.
                let r = mesure(17., 12.);
                let choix = |id: &'static str, choix: Theme, titre: &'static str, detail: &'static str, apercu: Div| {
                    let teinte_id = teinte().id;
                    div()
                        .id(id)
                        .flex()
                        .flex_row()
                        .items_center()
                        .gap(px(14.))
                        .p(px(14.))
                        .cursor_pointer()
                        .when(choix == Theme::Sombre, |d| d.rounded_t(rayon(r)))
                        .when(choix == Theme::Ega, |d| d.rounded_b(rayon(r)))
                        .hover(|s| s.bg(if th().ega { th().survol } else { ton(blanc(0.5), c(0x2a2a2c)) }))
                        .child(apercu.flex_none().size(px(54.)).rounded(rayon(14.)).border_1())
                        .child(
                            div()
                                .flex_1()
                                .flex()
                                .flex_col()
                                .child(div().font_weight(FontWeight::SEMIBOLD).text_color(th().texte_fort).child(titre))
                                .child(div().text_size(px(12.)).text_color(th().texte_pale).child(detail)),
                        )
                        .when(theme() == choix, |d| d.child(icone("check", 20., th().accent)))
                        .on_click(cx.listener(move |this, _, _, cx| {
                            this.reglages.theme = choix.id().into();
                            this.reglages.garder();
                            appliquer_theme(choix, teinte_id, cx);
                            cx.refresh_windows();
                            cx.notify();
                        }))
                };
                // L'aperçu EGA : une petite fenêtre DOS, barre bleue, lignes de texte.
                let ligne_ega = |w: f32, couleur: u32| div().h(px(3.)).w(px(w)).bg(c(couleur));
                let apercu_ega = div()
                    .bg(c(EGA_NOIR))
                    .border_color(c(EGA_GRIS_CLAIR))
                    .flex()
                    .flex_col()
                    .gap(px(4.))
                    // Arrondie comme la tuile (14 px, moins le trait) : GPUI ne découpe pas les enfants.
                    .child(div().h(px(9.)).rounded_t(rayon(13.)).bg(c(EGA_BLEU)))
                    .child(div().flex().flex_col().gap(px(4.)).px(px(5.)).pt(px(1.))
                        .child(ligne_ega(28., EGA_JAUNE))
                        .child(ligne_ega(38., EGA_GRIS_CLAIR))
                        .child(ligne_ega(22., EGA_CYAN_VIF))
                        .child(ligne_ega(32., EGA_MAGENTA_VIF)));
                // Les teintes du verre : une pastille ronde par teinte, la choisie cerclée.
                let verre_actif = theme() == Theme::Verre;
                let mut pastilles = div().flex().flex_row().flex_wrap().gap(px(10.));
                for (i, t) in TEINTES.iter().enumerate() {
                    let choisie = verre_actif && teinte().id == t.id;
                    let id = t.id;
                    pastilles = pastilles.child(
                        div()
                            .id(("teinte", i))
                            .size(px(38.))
                            .rounded(rayon(999.))
                            .border_2()
                            .border_color(if choisie { c(t.accent) } else { hsla(0., 0., 0., 0.) })
                            .flex()
                            .items_center()
                            .justify_center()
                            .when(verre_actif, |d| {
                                d.cursor_pointer().on_click(cx.listener(move |this, _, _, cx| {
                                    this.reglages.teinte = id.into();
                                    this.reglages.garder();
                                    appliquer_theme(Theme::Verre, id, cx);
                                    cx.refresh_windows();
                                    cx.notify();
                                }))
                            })
                            .child(
                                div()
                                    .size(px(28.))
                                    .rounded(rayon(999.))
                                    .border_1()
                                    .border_color(ton(blanc(0.9), c(0x48484a)))
                                    .bg(linear_gradient(
                                        135.,
                                        linear_color_stop(hsla(t.angle / 360., 0.75, 0.86, 1.), 0.),
                                        linear_color_stop(c(t.accent), 1.),
                                    )),
                            ),
                    );
                }
                carte_section(t("Apparence"))
                    .child(
                        groupe()
                            .child(choix(
                                "t-sombre",
                                Theme::Sombre,
                                t("Sombre"),
                                t("Aplats gris et fil noir, bulles plates."),
                                div()
                                    .bg(linear_gradient(
                                        90.,
                                        linear_color_stop(c(0x252527), 0.5),
                                        linear_color_stop(c(0x000000), 0.5),
                                    ))
                                    .border_color(ton(blanc(0.9), c(0x48484a))),
                            ))
                            .child(separateur())
                            .child(choix(
                                "t-verre",
                                Theme::Verre,
                                t("Verre"),
                                t("Verre clair sur halos pastel."),
                                div()
                                    .bg(linear_gradient(
                                        135.,
                                        linear_color_stop(ca(teinte().accent, 0.5), 0.),
                                        linear_color_stop(ca(teinte().halo, 0.6), 1.),
                                    ))
                                    .border_color(ton(blanc(0.9), c(0x48484a))),
                            ))
                            .child(separateur())
                            .child(choix(
                                "t-ega",
                                Theme::Ega,
                                t("EGA"),
                                t("Seize couleurs et police IBM, comme un PC de 1984."),
                                apercu_ega,
                            )),
                    )
                    .child(
                        div()
                            .pt(px(22.))
                            .pb(px(12.))
                            .text_size(px(15.))
                            .font_weight(FontWeight::SEMIBOLD)
                            .text_color(th().texte)
                            .child(if verre_actif {
                                format!("Teinte du verre · {}", teinte().nom)
                            } else {
                                t("Teinte du verre").to_string()
                            }),
                    )
                    .child(
                        groupe().child(
                            div()
                                .flex()
                                .flex_col()
                                .gap(px(10.))
                                .p(px(14.))
                                .child(pastilles.when(!verre_actif, |d| d.opacity(0.35)))
                                .when(!verre_actif, |d| {
                                    d.child(
                                        div()
                                            .text_size(px(12.))
                                            .text_color(th().texte_pale)
                                            .child(t("Choisissez le thème Verre pour changer de teinte.")),
                                    )
                                }),
                        ),
                    )
                    .into_any_element()
            }
            Section::Langue => {
                let mut g = groupe();
                for (i, (code, nom)) in [("fr", "Français"), ("en", "English")].into_iter().enumerate() {
                    let actif = if code == "en" { anglais() } else { !anglais() };
                    if i > 0 {
                        g = g.child(separateur());
                    }
                    g = g.child(
                        div()
                            .id(code)
                            .flex()
                            .flex_row()
                            .items_center()
                            .px(px(16.))
                            .py(px(12.))
                            .cursor_pointer()
                            .when(i == 0, |d| d.rounded_t(rayon(15.)))
                            .when(i == 1, |d| d.rounded_b(rayon(15.)))
                            .hover(|s| s.bg(th().survol))
                            .on_click(cx.listener(move |this, _, _, cx| {
                                this.reglages.langue = code.to_string();
                                this.reglages.garder();
                                ANGLAIS.store(code == "en", std::sync::atomic::Ordering::Relaxed);
                                cx.refresh_windows();
                            }))
                            .child(div().flex_1().text_color(th().texte_fort).child(nom))
                            .when(actif, |d| d.child(icone("check", 16., th().accent))),
                    );
                }
                carte_section("Langue").child(g).into_any_element()
            }
            Section::APropos => carte_section("À propos")
                .child(
                    groupe()
                        .child(ligne_info("Créé par", "Jonathan Guernon".into()))
                        .child(separateur())
                        .child(ligne_info("Version", env!("CARGO_PKG_VERSION").to_string()))
                        .child(separateur())
                        .child(ligne_info("Google Messages", "libgm (mautrix-gmessages)".into()))
                        .child(separateur())
                        .child(ligne_info("Messenger", "messagix (mautrix-meta)".into()))
                        .child(separateur())
                        .child(ligne_info("Interface", "GPUI · gpui-component".into()))
                        .child(separateur())
                        .child(ligne_info(t("Données"), dossier_donnees().display().to_string()))
                        .child(separateur())
                        .child(ligne_info(t("Médias"), dossier_medias().display().to_string())),
                )
                .into_any_element(),
        };

        div()
            .flex()
            .flex_row()
            .flex_1()
            .h_full()
            .gap(px(th().ecart))
            .when(th().sombre, |d| d.bg(th().liste))
            .child(gauche)
            .child(
                panneau(th().fil, th().rayon_fil)
                    .flex_1()
                    .h_full()
                    .when(th().marge_fil > 0., |d| d.h_auto().m(px(th().marge_fil)))
                    .overflow_hidden()
                    .child(contenu),
            )
    }

    fn comptes(&self, cx: &mut Context<Self>) -> impl IntoElement {
        let associe = self.etat.associe();
        let gm = div()
            .flex()
            .flex_row()
            .items_center()
            .gap(px(14.))
            .px(px(16.))
            .py(px(13.))
            .child(logo(Service::GoogleMessages, 40.))
            .child(
                div()
                    .flex_1()
                    .flex()
                    .flex_col()
                    .gap(px(2.))
                    .child(
                        div()
                            .flex()
                            .flex_row()
                            .items_center()
                            .gap(px(8.))
                            .child(div().size(px(8.)).rounded(rayon(8.)).bg(c(self.etat.couleur())))
                            .child(div().font_weight(FontWeight::SEMIBOLD).text_color(th().texte_fort).child("Google Messages")),
                    )
                    .child(div().text_size(px(12.)).text_color(th().texte_pale).child(format!(
                        "{} · {}",
                        self.reglages.email.clone().unwrap_or_else(|| t("SMS et RCS").into()),
                        self.etat.texte()
                    ))),
            )
            .when(!matches!(self.etat, Etat::Pret | Etat::Demarrage), |d| {
                d.child(
                    div()
                        .size(px(20.))
                        .rounded(rayon(20.))
                        .bg(th().danger)
                        .flex()
                        .items_center()
                        .justify_center()
                        .text_size(px(12.))
                        .font_weight(FontWeight::BOLD)
                        .text_color(blanc(1.))
                        .child("!"),
                )
            })
            .child(if associe {
                bouton_texte("gm-deco", t("Déconnecter"), DANGER)
                    .on_click(cx.listener(|this, _, _, cx| {
                        this.commande(json!({"cmd": "unpair"}));
                        this.convs.clear();
                        this.archives.clear();
                        this.messages.clear();
                        this.choisie = None;
                        this.reglages.email = None;
                        this.reglages.garder();
                        cx.notify();
                    }))
                    .into_any_element()
            } else {
                bouton_texte("gm-co", t("Connecter"), ACCENT)
                    .on_click(cx.listener(|this, _, _, cx| {
                        this.ajout_compte = true;
                        cx.notify();
                    }))
                    .into_any_element()
            });
        let meta_associe = !matches!(self.etat_meta, Etat::NonAssocie | Etat::Demarrage);
        let detail_meta = if self.pont_meta.is_none() {
            t("Pont Messenger introuvable").to_string()
        } else if self.connexion_meta_ouverte {
            t("Connectez-vous dans la fenêtre Facebook…").to_string()
        } else if meta_associe {
            format!("{} · {}", self.nom_meta.clone().unwrap_or_else(|| "Messenger".into()), self.etat_meta.texte())
        } else {
            t("Non connecté").to_string()
        };
        let messenger = div()
            .flex()
            .flex_col()
            .child(
                div()
                    .flex()
                    .flex_row()
                    .items_center()
                    .gap(px(14.))
                    .px(px(16.))
                    .py(px(13.))
                    .child(logo(Service::Messenger, 40.))
                    .child(
                        div()
                            .flex_1()
                            .flex()
                            .flex_col()
                            .gap(px(2.))
                            .child(
                                div()
                                    .flex()
                                    .flex_row()
                                    .items_center()
                                    .gap(px(8.))
                                    .when(meta_associe, |d| {
                                        d.child(div().size(px(8.)).rounded(px(8.)).bg(c(self.etat_meta.couleur())))
                                    })
                                    .child(div().font_weight(FontWeight::SEMIBOLD).text_color(th().texte_fort).child("Facebook Messenger")),
                            )
                            .child(div().text_size(px(12.)).text_color(th().texte_pale).child(detail_meta)),
                    )
                    .when(self.pont_meta.is_some(), |d| {
                        d.child(if meta_associe {
                            bouton_texte("fb-deco", t("Déconnecter"), DANGER)
                                .on_click(cx.listener(|this, _, _, cx| {
                                    this.commande_meta(json!({"cmd": "unpair"}));
                                    this.convs.retain(|c| service_de(&c.id) != Service::Messenger);
                                    this.archives.retain(|c| service_de(&c.id) != Service::Messenger);
                                    if this.choisie.as_deref().is_some_and(|c| service_de(c) == Service::Messenger) {
                                        this.choisie = None;
                                    }
                                    cx.notify();
                                }))
                                .into_any_element()
                        } else {
                            bouton_texte("fb-co", t("Connecter"), ACCENT)
                                .on_click(cx.listener(|this, _, _, cx| {
                                    this.connecter_messenger();
                                    cx.notify();
                                }))
                                .into_any_element()
                        })
                    }),
            )
            // L'historique chiffré de Messenger demande le NIP choisi dans l'app Messenger.
            .when(self.besoin_nip, |d| {
                d.child(
                    div()
                        .flex()
                        .flex_col()
                        .gap(px(6.))
                        .px(px(16.))
                        .pb(px(13.))
                        .child(div().text_size(px(12.)).text_color(th().texte).child(t(
                            "Pour voir l'historique chiffré, entrez le NIP de Messenger (celui choisi dans l'app).",
                        )))
                        .child(verre(0.8, 999.).pl(px(14.)).pr(px(6.)).py(px(1.)).child(Input::new(&self.nip).appearance(false))),
                )
            });


        carte_section(t("Comptes"))
            .child(groupe().child(gm).child(separateur()).child(messenger))
            .child(
                div()
                    .id("ajouter-compte")
                    .self_center()
                    .mt(px(14.))
                    .flex()
                    .flex_row()
                    .items_center()
                    .gap(px(6.))
                    .cursor_pointer()
                    .text_color(th().accent)
                    .hover(|s| s.opacity(0.7))
                    .on_click(cx.listener(|this, _, _, cx| {
                        if !this.etat.associe() {
                            this.ajout_compte = true;
                        } else {
                            this.erreur = Some(t("Messenger arrive bientôt ; Google Messages est déjà connecté.").into());
                        }
                        cx.notify();
                    }))
                    .child(icone("circle-plus", 17., th().accent))
                    .child(t("Ajouter un compte")),
            )
    }

    fn bouton_principal(id: &'static str, texte: &'static str) -> gpui::Stateful<Div> {
        div()
            .id(id)
            .flex()
            .items_center()
            .justify_center()
            .px(px(26.))
            .py(px(11.))
            .rounded(rayon(999.))
            .bg(degrade_accent())
            .text_color(blanc(1.))
            .font_weight(FontWeight::SEMIBOLD)
            .shadow(ombre_flottante())
            .cursor_pointer()
            .hover(|s| s.opacity(0.92))
            .child(texte)
    }

    fn ecran_connexion(&self, cx: &mut Context<Self>) -> impl IntoElement {
        let mut carte = verre(0.62, 28.)
            .when(th().sombre, |d| d.bg(th().liste).rounded(rayon(16.)))
            .shadow(ombre_verre())
            .w(px(460.))
            .flex()
            .flex_col()
            .items_center()
            .gap(px(14.))
            .px(px(40.))
            .py(px(38.))
            .child(
                img(icone_app_grande())
                .size(px(84.)),
            )
            .child(div().text_size(px(26.)).font_weight(FontWeight::BOLD).text_color(th().texte_fort).child("Jasette"))
            .child(
                div()
                    .text_center()
                    .text_color(th().texte)
                    .child(t("Vos SMS et RCS de Google Messages, avec tout l'historique, sur votre bureau.")),
            );

        if let Some(emoji) = &self.emoji {
            carte = carte
                .child(
                    div()
                        .mt(px(10.))
                        .size(px(150.))
                        .rounded(rayon(36.))
                        .bg(ton(blanc(0.85), th().surface))
                        .border_1()
                        .border_color(ton(blanc(1.), th().surface_bord))
                        .shadow(ombre_flottante())
                        .flex()
                        .items_center()
                        .justify_center()
                        .text_size(px(84.))
                        .font_family(POLICE_EMOJI)
                        .child(emoji.clone()),
                )
                .child(div().font_weight(FontWeight::SEMIBOLD).text_color(th().texte_fort).child(t("Touchez cet emoji sur votre téléphone")))
                .child(
                    div()
                        .text_center()
                        .text_size(px(13.))
                        .text_color(th().texte)
                        .child(t("Google Messages vous montre quelques emojis : choisissez le même.")),
                );
        } else if matches!(self.etat, Etat::Association) || self.connexion_ouverte {
            carte = carte.child(div().mt(px(10.)).text_color(th().texte).child(if self.connexion_ouverte {
                t("Connectez-vous dans la fenêtre Google…")
            } else {
                t("Demande envoyée au téléphone…")
            }));
        } else if let Some(qr) = &self.qr {
            carte = carte
                .child(div().mt(px(6.)).p(px(14.)).rounded(rayon(20.)).bg(blanc(1.)).child(dessiner_qr(qr)))
                .child(
                    div()
                        .text_center()
                        .text_size(px(13.))
                        .text_color(th().texte)
                        .child("Google Messages > Association d'appareils > Scanner le code QR"),
                );
        } else {
            carte = carte
                .child(
                    Self::bouton_principal("google", t("Se connecter avec Google"))
                        .mt(px(10.))
                        .on_click(cx.listener(|this, _, _, cx| {
                            this.connecter_google();
                            cx.notify();
                        })),
                )
                .child(
                    div()
                        .id("qr")
                        .text_size(px(13.))
                        .text_color(th().accent)
                        .cursor_pointer()
                        .hover(|s| s.opacity(0.7))
                        .on_click(cx.listener(|this, _, _, cx| {
                            this.erreur = None;
                            this.commande(json!({"cmd": "pair"}));
                            cx.notify();
                        }))
                        .child(t("Utiliser plutôt un code QR")),
                );
        }
        if let Some(e) = &self.erreur {
            carte = carte.child(
                div()
                    .px(px(14.))
                    .py(px(8.))
                    .rounded(rayon(14.))
                    .bg(alpha(th().danger, 0.08))
                    .text_size(px(13.))
                    .text_color(th().danger)
                    .child(e.clone()),
            );
        }
        div().flex().size_full().items_center().justify_center().child(carte)
    }
}

impl Render for Jasette {
    /// La fenêtre dessine sa propre barre de titre (GNOME ne la dessine pas
    /// pour une app qui demande des décorations côté client) et son bord.
    fn render(&mut self, window: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        let contenu = self.rendu(window, cx);
        // Sans ombre : son bord était peint en blanc autour de la fenêtre.
        gpui_component::window_border().shadow_size(px(0.)).child(
            div()
                .size_full()
                .flex()
                .flex_col()
                .bg(th().fond)
                .child(
                    gpui_component::TitleBar::new()
                        .bg(th().rail)
                        .border_color(ton(blanc(0.6), c(0x2c2c2e)))
                        .child(
                            div()
                                .flex()
                                .flex_row()
                                .items_center()
                                .gap(px(8.))
                                .font_family(police())
                                .text_size(px(13.))
                                .font_weight(FontWeight::SEMIBOLD)
                                .text_color(th().texte)
                                .child(img(icone_app()).size(px(18.)))
                                .child("Jasette"),
                        ),
                )
                .child(div().flex_1().min_h(px(0.)).child(contenu)),
        )
    }
}

impl Jasette {
    fn rendu(&mut self, window: &mut Window, cx: &mut Context<Self>) -> Div {
        // L'indication du champ nomme la conversation ouverte.
        let nom = self.choisie.as_ref().and_then(|id| self.conv(id)).map(|c| c.name.clone()).filter(|n| !n.is_empty());
        let service = self.choisie.as_deref().map(service_de).unwrap_or(Service::GoogleMessages);
        let indication = if anglais() {
            format!("Message {} on {}", nom.unwrap_or_else(|| "this contact".into()), service.nom())
        } else {
            format!("Message à {} sur {}", nom.unwrap_or_else(|| "ce contact".into()), service.nom())
        };
        if self.indication != indication {
            self.indication = indication.clone();
            self.saisie.update(cx, |s, cx| s.set_placeholder(indication, window, cx));
        }
        let page = div()
            .relative()
            .flex()
            .flex_col()
            .size_full()
            .overflow_hidden()
            .bg(th().fond)
            .font_family(police())
            // La police IBM 8x16 n'est nette qu'à 16 px.
            .text_size(px(if th().ega { 16. } else { 15. }))
            .text_color(th().texte_fort)
            // Les halos du thème glass : bleu en haut à gauche, violet à droite, pêche en bas.
            .when(!th().sombre, |d| {
                d.child(halo(teinte().accent, 0.42, -180., -220., 760.))
                    .child(halo(teinte().accent_2, 0.36, 760., -160., 720.))
                    .child(halo(teinte().halo, 0.40, 300., 520., 760.))
            });

        // Premier lancement, aucun compte : l'écran de connexion seul.
        if !self.etat.associe() && self.convs.is_empty() && self.section.is_none() {
            return page.child(self.ecran_connexion(cx));
        }

        let centre: AnyElement = if self.section.is_some() {
            self.reglages_vue(cx).into_any_element()
        } else if self.galerie == Some(None) {
            self.galerie_vue(cx).into_any_element()
        } else if self.galerie.is_some() {
            // Les médias d'une conversation : la liste reste à gauche.
            let liste = self.colonne_liste(cx).into_any_element();
            let galerie = self.galerie_vue(cx).into_any_element();
            div()
                .flex()
                .flex_row()
                .flex_1()
                .h_full()
                .gap(px(th().ecart))
                .when(th().sombre, |d| d.bg(th().liste))
                .child(liste)
                .child(galerie)
                .into_any_element()
        } else {
            let liste = self.colonne_liste(cx).into_any_element();
            let fil = self.panneau_fil(cx).into_any_element();
            div()
                .flex()
                .flex_row()
                .flex_1()
                .h_full()
                .gap(px(th().ecart))
                // En sombre, la marge autour du fil noir prend le gris de la liste.
                .when(th().sombre, |d| d.bg(th().liste))
                .child(liste)
                .child(fil)
                .into_any_element()
        };

        page.child(
            div()
                .flex()
                .flex_row()
                .flex_1()
                .min_h(px(0.))
                .gap(px(th().ecart))
                .p(px(th().marge))
                .child(self.rail(cx))
                .child(centre),
        )
        .when_some(self.apercu.clone(), |d, chemin| {
            d.child(
                div()
                    .id("apercu")
                    .absolute()
                    .top_0()
                    .left_0()
                    .right_0()
                    .bottom_0()
                    .bg(hsla(0., 0., 0., 0.88))
                    .p(px(40.))
                    .flex()
                    .items_center()
                    .justify_center()
                    .cursor_pointer()
                    .on_click(cx.listener(|this, _, _, cx| {
                        this.apercu = None;
                        cx.notify();
                    }))
                    .child(img(chemin).size_full().object_fit(ObjectFit::Contain)),
            )
        })
        .when_some(self.erreur.clone().filter(|_| self.section.is_none()), |d, e| {
            d.child(
                div()
                    .id("erreur")
                    .absolute()
                    .top(px(22.))
                    .right(px(30.))
                    .max_w(px(460.))
                    .flex()
                    .flex_row()
                    .items_center()
                    .gap(px(8.))
                    .px(px(16.))
                    .py(px(8.))
                    .rounded(rayon(999.))
                    .bg(ton(blanc(0.95), c(0x3a3a3c)))
                    .shadow(ombre_flottante())
                    .text_size(px(13.))
                    .text_color(th().danger)
                    .cursor_pointer()
                    .on_click(cx.listener(|this, _, _, cx| {
                        this.erreur = None;
                        cx.notify();
                    }))
                    .child(icone("triangle-alert", 15., th().danger))
                    .child(div().truncate().child(e))
                    .child(icone("x", 14., th().texte_pale)),
            )
        })
    }
}

// ---- Petites pièces ----

fn pastille_nombre(n: usize, couleur: Hsla) -> impl IntoElement {
    div()
        .absolute()
        .top(px(-3.))
        .right(px(-3.))
        .min_w(px(18.))
        .h(px(18.))
        .px(px(5.))
        .rounded(rayon(999.))
        .bg(couleur)
        .border_1()
        .border_color(ton(blanc(0.9), th().rail))
        .flex()
        .items_center()
        .justify_center()
        .text_size(px(11.))
        .font_weight(FontWeight::BOLD)
        .text_color(blanc(1.))
        .child(if n > 99 { "99+".to_string() } else { n.to_string() })
}

/// Une petite bulle « … » dont les points s'allument tour à tour : le
/// service est en train de se connecter.
fn pastille_chargement(id: &'static str) -> impl IntoElement {
    use gpui::{Animation, AnimationExt};
    let mut bulle = div()
        .absolute()
        .top(px(-4.))
        .right(px(-6.))
        .h(px(14.))
        .px(px(4.))
        .rounded(rayon(999.))
        .bg(th().accent)
        .border_1()
        .border_color(ton(blanc(0.9), th().rail))
        .flex()
        .flex_row()
        .items_center()
        .gap(px(2.));
    for i in 0..3usize {
        bulle = bulle.child(
            div().size(px(3.)).rounded(rayon(999.)).bg(blanc(1.)).with_animation(
                (id, i),
                Animation::new(Duration::from_millis(1100)).repeat(),
                move |d, t| {
                    // Chaque point culmine à son tiers du cycle.
                    let phase = (t - i as f32 / 3.).rem_euclid(1.);
                    let eclat = (1. - (phase * 2. - 1.).abs()).powf(2.);
                    d.opacity(0.3 + 0.7 * eclat).mt(px(-2. * eclat))
                },
            ),
        );
    }
    bulle
}

fn pastille_alerte() -> impl IntoElement {
    div()
        .absolute()
        .top(px(-3.))
        .right(px(-3.))
        .size(px(18.))
        .rounded(rayon(999.))
        .bg(th().danger)
        .border_1()
        .border_color(ton(blanc(0.9), th().rail))
        .flex()
        .items_center()
        .justify_center()
        .text_size(px(11.))
        .font_weight(FontWeight::BOLD)
        .text_color(blanc(1.))
        .child("!")
}

fn carte_section(titre: &'static str) -> Div {
    let titre = t(titre);
    div()
        .flex()
        .flex_col()
        .w_full()
        .max_w(px(760.))
        .px(px(40.))
        .py(px(30.))
        .child(div().pb(px(12.)).text_size(px(15.)).font_weight(FontWeight::SEMIBOLD).text_color(th().texte).child(titre))
}

/// Un trait entre deux rangées, en retrait : il ne touche jamais les coins arrondis.
fn separateur() -> Div {
    div().h(px(1.)).mx(px(16.)).bg(ton(blanc(0.65), th().trait_fin))
}

fn groupe() -> Div {
    verre(0.72, 18.)
        .when(th().sombre, |d| d.bg(th().carte).rounded(rayon(12.)))
        .shadow(ombre_flottante())
        .flex()
        .flex_col()
        .overflow_hidden()
}

fn bouton_texte(id: &'static str, texte: &'static str, couleur: u32) -> gpui::Stateful<Div> {
    div()
        .id(id)
        .px(px(14.))
        .py(px(6.))
        .rounded(rayon(999.))
        .text_size(px(13.))
        .font_weight(FontWeight::SEMIBOLD)
        .cursor_pointer()
        .when(!th().ega, |d| {
            d.bg(ca(couleur, 0.1)).text_color(c(couleur)).hover(|s| s.bg(ca(couleur, 0.18)))
        })
        // En EGA, un bouton DOS : aplat EGA foncé, texte blanc, ombre portée dure.
        .when(th().ega, |d| {
            d.bg(ega_fond(c(couleur)))
                .text_color(c(EGA_BLANC))
                .shadow(ombre_flottante())
                .hover(|s| s.bg(ega_snap(c(couleur))))
        })
        .child(texte)
}

fn ligne_info(nom: &'static str, valeur: String) -> impl IntoElement {
    let nom = t(nom);
    div()
        .flex()
        .flex_row()
        .gap(px(14.))
        .px(px(16.))
        .py(px(12.))
        .child(div().w(px(150.)).flex_none().text_color(th().texte).child(nom))
        .child(div().flex_1().min_w(px(0.)).truncate().text_color(th().texte_fort).child(valeur))
}

fn ligne_interrupteur(
    id: &'static str,
    titre: &'static str,
    detail: &'static str,
    actif: bool,
    clic: impl Fn(&gpui::ClickEvent, &mut Window, &mut App) + 'static,
) -> impl IntoElement {
    div()
        .id(id)
        .flex()
        .flex_row()
        .items_center()
        .gap(px(14.))
        .px(px(16.))
        .py(px(13.))
        .cursor_pointer()
        .on_click(clic)
        .child(
            div()
                .flex_1()
                .flex()
                .flex_col()
                .gap(px(2.))
                .child(div().font_weight(FontWeight::SEMIBOLD).text_color(th().texte_fort).child(titre))
                .child(div().text_size(px(12.)).text_color(th().texte_pale).child(detail)),
        )
        .child(
            div()
                .w(px(44.))
                .h(px(26.))
                .rounded(rayon(999.))
                .p(px(3.))
                .flex()
                .flex_row()
                .when(actif, |d| d.bg(degrade_accent()).justify_end())
                .when(!actif, |d| d.bg(ton(ca(TEXTE_PALE, 0.35), c(0x48484a))).justify_start())
                .child(div().size(px(20.)).rounded(rayon(999.)).bg(blanc(1.)).shadow(ombre_flottante())),
        )
}

/// Un halo de couleur flou derrière le verre.
fn halo(couleur: u32, opacite: f32, x: f32, y: f32, taille: f32) -> impl IntoElement {
    div()
        .absolute()
        .left(px(x))
        .top(px(y))
        .size(px(taille))
        .rounded(rayon(taille))
        // Pas de fond : seule l'ombre, très floue, dessine un halo sans bord.
        .shadow(vec![BoxShadow {
            color: ca(couleur, (opacite * 1.9).min(1.)),
            offset: point(px(0.), px(0.)),
            blur_radius: px(taille * 0.5),
            spread_radius: px(0.),
            inset: false,
        }])
}

/// Le rond d'une conversation : la photo du contact, sinon ses initiales sur
/// la couleur que Google lui donne.
/// Les photos des membres d'un groupe assemblées dans le rond de l'avatar :
/// deux en diagonale, trois en triangle, quatre en grille.
fn mosaique(membres: &[(String, String, Option<PathBuf>)], taille: f32) -> AnyElement {
    let n = membres.len().min(4);
    let (d, places): (f32, Vec<(f32, f32)>) = match n {
        1 => (taille, vec![(0., 0.)]),
        2 => {
            let d = taille * 0.66;
            (d, vec![(0., 0.), (taille - d, taille - d)])
        }
        3 => {
            let d = taille * 0.54;
            (d, vec![((taille - d) / 2., 0.), (0., taille - d), (taille - d, taille - d)])
        }
        _ => {
            let d = taille * 0.5;
            (d, vec![(0., 0.), (taille - d, 0.), (0., taille - d), (taille - d, taille - d)])
        }
    };
    let mut boite = div().relative().flex_none().size(px(taille));
    for ((nom, couleur, photo), (x, y)) in membres.iter().zip(places) {
        boite = boite.child(
            div()
                .absolute()
                .left(px(x))
                .top(px(y))
                .child(avatar(nom, couleur, false, photo.as_ref(), d)),
        );
    }
    boite.into_any_element()
}

fn avatar(nom: &str, couleur: &str, groupe: bool, photo: Option<&PathBuf>, taille: f32) -> AnyElement {
    if let Some(photo) = photo {
        return div()
            .flex_none()
            .size(px(taille))
            .rounded(rayon(taille))
            .overflow_hidden()
            .border_1()
            .border_color(th().bord_avatar)
            .child(img(photo.clone()).size_full().rounded(rayon(taille)).object_fit(ObjectFit::Cover))
            .into_any_element();
    }
    let base = hex(couleur).unwrap_or(ACCENT);
    let initiales: Option<String> = if groupe {
        None
    } else {
        let lettres: String = nom
            .split_whitespace()
            .filter_map(|m| m.chars().find(|c| c.is_alphanumeric()))
            .take(2)
            .collect::<String>()
            .to_uppercase();
        (!lettres.is_empty() && !lettres.chars().all(|c| c.is_ascii_digit())).then_some(lettres)
    };
    div()
        .flex_none()
        .size(px(taille))
        .rounded(rayon(taille))
        .bg(fond_avatar(ca(base, 0.75), c(base)))
        .border_1()
        .border_color(ton(blanc(0.7), th().bord_avatar))
        .flex()
        .items_center()
        .justify_center()
        .text_size(px(taille * 0.38))
        .font_weight(FontWeight::SEMIBOLD)
        .text_color(blanc(1.))
        .child(match initiales {
            Some(l) => l.into_any_element(),
            None => icone(if groupe { "users" } else { "user-round" }, taille * 0.48, blanc(1.)).into_any_element(),
        })
        .into_any_element()
}

fn dessiner_qr(qr: &qrcode::QrCode) -> impl IntoElement {
    let n = qr.width();
    let couleurs = qr.to_colors();
    let case = 5.0;
    let mut grille = div().flex().flex_col();
    for y in 0..n {
        let mut ligne = div().flex().flex_row();
        for x in 0..n {
            let fonce = couleurs[y * n + x] == qrcode::Color::Dark;
            ligne = ligne.child(div().w(px(case)).h(px(case)).bg(if fonce { c(TEXTE_FORT) } else { blanc(1.) }));
        }
        grille = grille.child(ligne);
    }
    grille
}

fn une_ligne(s: &str) -> String {
    s.split_whitespace().collect::<Vec<_>>().join(" ")
}

fn taille(octets: i64) -> String {
    match octets {
        n if n <= 0 => "Ouvrir".into(),
        n if n < 1024 * 1024 => format!("{} Ko", (n + 1023) / 1024),
        n => format!("{:.1} Mo", n as f64 / (1024. * 1024.)),
    }
}

fn local(ms: i64) -> Option<chrono::DateTime<chrono::Local>> {
    chrono::DateTime::from_timestamp_millis(ms).map(|d| d.with_timezone(&chrono::Local))
}

const JOURS: [&str; 7] = ["lundi", "mardi", "mercredi", "jeudi", "vendredi", "samedi", "dimanche"];
const MOIS: [&str; 12] =
    ["janv.", "févr.", "mars", "avr.", "mai", "juin", "juil.", "août", "sept.", "oct.", "nov.", "déc."];

/// L'interface en anglais (Réglages › Langue).
static ANGLAIS: std::sync::atomic::AtomicBool = std::sync::atomic::AtomicBool::new(false);

fn anglais() -> bool {
    ANGLAIS.load(std::sync::atomic::Ordering::Relaxed)
}

/// La traduction d'un texte de l'interface (écrit en français dans le code).
fn t(fr: &'static str) -> &'static str {
    if !anglais() {
        return fr;
    }
    match fr {
        "🎤 Message vocal" => "🎤 Voice message",
        "📎 Pièce jointe" => "📎 Attachment",
        "Activités" => "Activities",
        "Ajouter un compte" => "Add an account",
        "Annuler" => "Cancel",
        "Aperçu du message" => "Message preview",
        "Aplats gris et fil noir, bulles plates." => "Flat grays, black thread, flat bubbles.",
        "Apparence" => "Appearance",
        "À propos" => "About",
        "Archives" => "Archive",
        "Association" => "Pairing",
        "Aucune conversation archivée" => "No archived conversations",
        "Aucun emoji" => "No emoji",
        "Aucune photo trouvée" => "No photo found",
        "Aucun résultat" => "No results",
        "Aujourd'hui" => "Today",
        "Avatar de Facebook" => "Facebook avatar",
        "Avatar de Google Messages" => "Google Messages avatar",
        "Bientôt disponible" => "Coming soon",
        "Boîte de réception" => "Inbox",
        "Chargement…" => "Loading…",
        "Chargement de l'image…" => "Loading image…",
        "Chargement des archives…" => "Loading archive…",
        "Chargement des conversations…" => "Loading conversations…",
        "Chercher un emoji (en anglais)" => "Search emoji",
        "Choisir" => "Choose",
        "Choisir une image…" => "Choose an image…",
        "Choisissez le thème Verre pour changer de teinte." => "Pick the Glass theme to change the tint.",
        "Choisissez une conversation" => "Pick a conversation",
        "Ciel" => "Sky",
        "Citron" => "Lemon",
        "Compte local" => "Local account",
        "Comptes" => "Accounts",
        "Connecté" => "Connected",
        "Connecter" => "Connect",
        "Connectez-vous dans la fenêtre Google…" => "Sign in in the Google window…",
        "Connexion…" => "Connecting…",
        "Connexion annulée" => "Sign-in cancelled",
        "Conversation en lecture seule" => "Read-only conversation",
        "Corail" => "Coral",
        "Déconnecter" => "Disconnect",
        "Demande envoyée au téléphone…" => "Request sent to the phone…",
        "Démarrage" => "Starting",
        "Distribué" => "Delivered",
        "Données" => "Data",
        "Drapeaux" => "Flags",
        "Échec" => "Failed",
        "Envoi…" => "Sending…",
        "Envoyé" => "Sent",
        "Envoyer" => "Send",
        "Google Messages déconnecté" => "Google Messages disconnected",
        "Google Messages vous montre quelques emojis : choisissez le même." => "Google Messages shows you a few emojis: pick the same one.",
        "Hier" => "Yesterday",
        "Image indisponible · réessayer" => "Image unavailable · retry",
        "Initiale" => "Initial",
        "Lavande" => "Lavender",
        "Lilas" => "Lilac",
        "Les emojis que vous utilisez apparaîtront ici." => "Emojis you use will show up here.",
        "Lu" => "Read",
        "Médias" => "Media",
        "Menthe" => "Mint",
        "Messages plus anciens" => "Older messages",
        "Messenger arrive bientôt ; Google Messages est déjà connecté." => "Messenger is coming soon; Google Messages is already connected.",
        "Moi" => "Me",
        "Montrer le texte du message dans la notification." => "Show the message text in the notification.",
        "Non connecté" => "Not connected",
        "Notifications de bureau" => "Desktop notifications",
        "Nourriture" => "Food",
        "Nouveau message" => "New message",
        "Numéro de téléphone, puis Entrée" => "Phone number, then Enter",
        "Objets" => "Objects",
        "Ouvrir" => "Open",
        "Pêche" => "Peach",
        "Personnes" => "People",
        "Quand Messenger sera connecté" => "Once Messenger is connected",
        "Récents" => "Recent",
        "Rechercher" => "Search",
        "Reconnexion…" => "Reconnecting…",
        "Réglages" => "Settings",
        "Rose" => "Pink",
        "Sans nom" => "Unnamed",
        "Sans photo" => "No photo",
        "Se connecter avec Google" => "Sign in with Google",
        "SMS et RCS" => "SMS and RCS",
        "Sombre" => "Dark",
        "Symboles" => "Symbols",
        "Teinte du verre" => "Glass tint",
        "Téléchargement…" => "Downloading…",
        "Téléphone hors ligne" => "Phone offline",
        "Touchez cet emoji sur votre téléphone" => "Tap this emoji on your phone",
        "Une bulle quand un message arrive et que Jasette n'est pas au premier plan." => "A popup when a message arrives and Jasette isn't in front.",
        "Une photo de votre ordinateur" => "A photo from your computer",
        "Utiliser plutôt un code QR" => "Use a QR code instead",
        "Verre" => "Glass",
        "Verre clair sur halos pastel." => "Light glass over pastel halos.",
        "Vos SMS et RCS de Google Messages, avec tout l'historique, sur votre bureau." => "Your Google Messages SMS and RCS, with full history, on your desktop.",
        "Votre fiche sur le téléphone" => "Your contact card on the phone",
        "Voyages" => "Travel",
        "Le pont s'est arrêté (voir le terminal)." => "The bridge stopped (see the terminal).",
        "Langue" => "Language",
        "Message à {} sur Google Messages" => "Message {} on Google Messages",
        "Teinte du verre · {}" => "Glass tint · {}",
        "Enregistrement {}:{:02} · ↑ pour envoyer" => "Recording {}:{:02} · ↑ to send",
        "NIP Messenger (6 chiffres), puis Entrée" => "Messenger PIN (6 digits), then Enter",
        "Pont Messenger introuvable" => "Messenger bridge not found",
        "Connectez-vous dans la fenêtre Facebook…" => "Sign in in the Facebook window…",
        "Pour voir l'historique chiffré, entrez le NIP de Messenger (celui choisi dans l'app)." => "To see your encrypted history, enter your Messenger PIN (the one set in the app).",
        "Créé par" => "Created by",
        "Archiver" => "Archive",
        "Épingler en haut" => "Pin to top",
        "Désépingler" => "Unpin",
        "Ajouter aux favoris" => "Add to favorites",
        "Ajouter au Head" => "Add to Head",
        "Retirer du Head" => "Remove from Head",
        "Favoris" => "Favorites",
        "Aucun favori : ajoutez-en par le menu « … » d'une conversation." => "No favorites yet: add some from a conversation's “…” menu.",
        "Retirer des favoris" => "Remove from favorites",
        "Désarchiver" => "Unarchive",
        "Supprimer la conversation" => "Delete conversation",
        "Confirmer la suppression" => "Confirm delete",
        "Archiver et supprimer : bientôt pour Messenger." => "Archive and delete: coming soon for Messenger.",
        "Nom ou numéro de téléphone" => "Name or phone number",
        "Nouvelle conversation Messenger" => "New Messenger chat",
        "Nouveau message par Google Messages" => "New message via Google Messages",
        "Photos et vidéos" => "Photos & videos",
        "Vocaux" => "Voice",
        "Fichiers" => "Files",
        "Rien ici pour l'instant." => "Nothing here yet.",
        "Seize couleurs et police IBM, comme un PC de 1984." => "Sixteen colors and the IBM font, like a 1984 PC.",
        "Interface" => "Interface",
        _ => fr,
    }
}


fn heure(ms: i64, court: bool) -> String {
    use chrono::Datelike;
    let Some(t) = local(ms) else { return String::new() };
    let aujourdhui = chrono::Local::now().date_naive();
    if court && t.date_naive() != aujourdhui {
        if aujourdhui.pred_opt() == Some(t.date_naive()) {
            return crate::t("Hier").into();
        }
        if (aujourdhui - t.date_naive()).num_days() < 7 {
            return JOURS[t.weekday().num_days_from_monday() as usize][..3].to_string();
        }
        return format!("{} {}", t.day(), MOIS[t.month0() as usize]);
    }
    t.format("%H:%M").to_string()
}

fn jour(ms: i64) -> String {
    use chrono::Datelike;
    let Some(t) = local(ms) else { return String::new() };
    let aujourdhui = chrono::Local::now().date_naive();
    let d = t.date_naive();
    if d == aujourdhui {
        return crate::t("Aujourd'hui").into();
    }
    if aujourdhui.pred_opt() == Some(d) {
        return crate::t("Hier").into();
    }
    format!("{} {} {} {}", JOURS[t.weekday().num_days_from_monday() as usize], d.day(), MOIS[d.month0() as usize], d.year())
}

fn statut(s: &str) -> &'static str {
    match s {
        "ENVOI" => "Envoi…",
        "ÉCHEC" => "Échec",
        s if s.contains("FAILED") => "Échec",
        s if s.contains("READ") => "Lu",
        s if s.contains("DELIVERED") => "Distribué",
        s if s.contains("COMPLETE") => "Envoyé",
        s if s.contains("SENDING") || s.contains("YET_TO_SEND") => "Envoi…",
        _ => "",
    }
}

fn main() {
    gpui_platform::application().with_assets(gpui_kit_assets::AllAssets).run(|cx: &mut App| {
        if let Err(e) = cx
            .text_system()
            .add_fonts(vec![std::borrow::Cow::Borrowed(include_bytes!("../assets/polices/JasetteEmoji.ttf").as_slice())])
        {
            eprintln!("jasette : police d'emojis : {e}");
        }
        gpui_component::init(cx);
        gpui_component::Theme::update(cx, |t| {
            t.font_family = POLICE.into();
            t.font_size = px(15.);
            t.radius = px(12.);
            t.radius_lg = px(20.);
            t.focus_ring = false;
            t.input = blanc(0.);
        });
        let reglages = Reglages::charger();
        ANGLAIS.store(reglages.langue == "en", std::sync::atomic::Ordering::Relaxed);
        appliquer_theme(Theme::depuis(&reglages.theme), &reglages.teinte, cx);

        let bounds = Bounds::centered(None, size(px(1280.), px(800.)), cx);
        cx.open_window(
            WindowOptions {
                window_bounds: Some(WindowBounds::Windowed(bounds)),
                window_decorations: Some(gpui::WindowDecorations::Client),
                window_background: gpui::WindowBackgroundAppearance::Transparent,
                app_id: Some("jasette".into()),
                ..gpui_component::TitleBar::window_options()
            },
            |window, cx| {
                let vue = cx.new(|cx| Jasette::new(window, cx));
                cx.new(|cx| Root::new(vue, window, cx))
            },
        )
        .expect("fenêtre");
        cx.activate(true);
    });
}
