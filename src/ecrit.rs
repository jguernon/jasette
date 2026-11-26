// « En train d'écrire » : l'événement `typing` des ponts (serveur/PROTOCOLE.md,
// §5.11). Une bulle reçue aux trois points qui rebondissent en bas du fil,
// « écrit… » dans la liste et dans l'en-tête. Couvert par le réglage
// « Afficher qui est en ligne et qui écrit » (marque `presence_en_ligne`).
// Jasette n'envoie jamais son propre « en train d'écrire ».
use super::*;
use gpui::{Animation, AnimationExt};

/// Sans nouvelle depuis 6 s, la bulle s'en va (au cas où `on:false` ne vient jamais).
pub(crate) const ECRIT_DUREE: Duration = Duration::from_secs(6);

/// Quelqu'un écrit dans une conversation.
#[derive(Clone, Debug)]
pub(crate) struct Ecrit {
    /// Le nom de la personne (groupes), s'il est connu.
    pub nom: String,
    pub depuis: Instant,
}

impl Jasette {
    /// Une ligne `typing` d'un pont (ids déjà préfixés).
    pub(crate) fn ecrit_recu(&mut self, v: &Value) {
        if !self.reglages.presence_en_ligne {
            return;
        }
        let Some(conv) = v.get("conv").and_then(Value::as_str).filter(|c| !c.is_empty()) else {
            return;
        };
        if v.get("on").and_then(Value::as_bool).unwrap_or(false) {
            let nom = v.get("name").and_then(Value::as_str).unwrap_or_default().to_string();
            let nouveau = !self.ecrits.contains_key(conv);
            self.ecrits.insert(conv.to_string(), Ecrit { nom, depuis: Instant::now() });
            if nouveau && self.choisie.as_deref() == Some(conv) {
                // La bulle apparaît en bas du fil.
                self.descendre = true;
            }
            if nouveau {
                self.son_ecrit(conv);
            }
        } else {
            self.ecrit_fini(conv);
        }
    }

    /// Un message reçu de cette conversation : la bulle s'en va.
    pub(crate) fn ecrit_fini(&mut self, conv: &str) {
        if self.ecrits.remove(conv).is_some() {
            self.ecrit_parti(conv);
        }
    }

    /// Les bulles sans nouvelle depuis 6 s s'en vont ; vrai si l'écran change.
    pub(crate) fn ecrits_perimes(&mut self) -> bool {
        let perimes: Vec<String> =
            self.ecrits.iter().filter(|(_, e)| e.depuis.elapsed() >= ECRIT_DUREE).map(|(k, _)| k.clone()).collect();
        for k in &perimes {
            self.ecrit_fini(k);
        }
        !perimes.is_empty()
    }

    /// Quelqu'un écrit-il dans cette conversation (et le réglage le montre-t-il) ?
    pub(crate) fn ecrit(&self, conv: &str) -> Option<&Ecrit> {
        if !self.reglages.presence_en_ligne {
            return None;
        }
        self.ecrits.get(conv)
    }

    /// La bulle « … » en bas du fil : la couleur des bulles reçues (`fond`,
    /// Mood compris) ; dans un groupe, le petit avatar de qui écrit.
    pub(crate) fn bulle_ecrit(&self, conv: &Conv, fond: Hsla) -> Option<Div> {
        let e = self.ecrit(&conv.id)?;
        let carre = th().ega || retro::retro();
        let point = |i: usize| {
            div().size(px(7.)).when(!carre, |d| d.rounded(px(7.))).bg(th().texte_pale).with_animation(
                ("ecrit-point", i),
                Animation::new(Duration::from_millis(1200)).repeat(),
                move |d, t| {
                    // Chaque point bondit à son tour, puis se repose.
                    let phase = (t - i as f32 * 0.16).rem_euclid(1.);
                    let saut = if phase < 0.4 { (phase / 0.4 * std::f32::consts::PI).sin() } else { 0. };
                    d.mt(px(-5. * saut)).mb(px(5. * saut)).opacity(0.45 + 0.55 * saut)
                },
            )
        };
        let bulle = div()
            .flex()
            .flex_row()
            .items_center()
            .gap(px(5.))
            .px(px(14.))
            .pt(px(13.))
            .pb(px(11.))
            .rounded(rayon(if retro::retro() && th().live { 0. } else { 18. }))
            .rounded_bl(rayon(5.))
            .bg(fond)
            .border_1()
            .border_color(th().surface_bord)
            .child(point(0))
            .child(point(1))
            .child(point(2));
        let avatar_qui = (conv.group && !e.nom.is_empty()).then(|| {
            // La photo du membre s'il est dans la mosaïque du groupe, sinon ses initiales.
            let membre = self.mosaiques.get(&conv.id).and_then(|l| l.iter().find(|(n, _, _)| *n == e.nom));
            let (couleur, photo) = membre.map(|(_, c, p)| (c.as_str(), p.as_ref())).unwrap_or(("", None));
            avatar(&e.nom, couleur, false, photo, 24.)
        });
        Some(
            div()
                .mt(px(4.))
                .flex()
                .flex_row()
                .items_end()
                .gap(px(6.))
                .children(avatar_qui)
                .child(bulle),
        )
    }
}

/// « écrit… » dans la liste : en couleur d'accent, avec trois petits points animés.
pub(crate) fn apercu_ecrit(taille: f32) -> Div {
    let carre = th().ega || retro::retro();
    let mut points = div().flex().flex_row().items_end().gap(px(2.)).pb(px(2.));
    for i in 0..3usize {
        points = points.child(
            div().size(px(3.)).when(!carre, |d| d.rounded(px(3.))).bg(th().accent).with_animation(
                ("ecrit-liste", i),
                Animation::new(Duration::from_millis(1200)).repeat(),
                move |d, t| {
                    let phase = (t - i as f32 * 0.16).rem_euclid(1.);
                    let saut = if phase < 0.4 { (phase / 0.4 * std::f32::consts::PI).sin() } else { 0. };
                    d.opacity(0.35 + 0.65 * saut)
                },
            ),
        );
    }
    div()
        .flex()
        .flex_row()
        .items_center()
        .gap(px(4.))
        .text_size(px(taille))
        .text_color(th().accent)
        .font_weight(FontWeight::SEMIBOLD)
        .child(t("écrit…"))
        .child(points)
}
