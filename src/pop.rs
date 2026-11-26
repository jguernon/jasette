// Un message qui n'est qu'un émoji (notes::emoji_seul) : deux fois plus gros,
// sans bulle, avec un « pop » élastique (0 → 1,25 → 0,95 → 1, 420 ms) quand
// il arrive en direct dans le fil ouvert, reçu ou envoyé (l'emoji rapide
// compris, qui passe par le même envoi). Jamais à l'ouverture du fil ni pour
// l'historique : seuls l'événement `message` et l'envoi local le déclenchent.
use super::*;
use gpui::{Animation, AnimationExt};
use std::cell::RefCell;

/// La durée du pop.
const DUREE_POP: Duration = Duration::from_millis(420);

/// Le grossissement d'un émoji seul par rapport à la taille du texte.
pub(crate) const ECHELLE_EMOJI_SEUL: f32 = 3.;

thread_local! {
    /// Les pops en cours : id (ou tmp_id) du message → son départ.
    static POPS: RefCell<HashMap<String, Instant>> = RefCell::new(HashMap::new());
}

/// Un message vient d'arriver dans le fil ouvert : s'il n'est qu'un émoji, il fera son pop.
pub(crate) fn arrive(cle: &str, texte: &str) {
    if cle.is_empty() || !notes::emoji_seul(texte) {
        return;
    }
    POPS.with(|p| {
        let mut p = p.borrow_mut();
        p.retain(|_, t| t.elapsed() < Duration::from_secs(5));
        p.insert(cle.to_string(), Instant::now());
    });
}

/// La clé stable d'un message : son tmp_id (un envoi confirmé garde son pop), sinon son id.
fn cle(m: &Msg) -> &str {
    if m.tmp_id.is_empty() { &m.id } else { &m.tmp_id }
}

/// Le pop de ce message est-il en cours ?
fn en_cours(m: &Msg) -> bool {
    POPS.with(|p| {
        let p = p.borrow();
        [m.id.as_str(), m.tmp_id.as_str()]
            .iter()
            .filter(|k| !k.is_empty())
            .any(|k| p.get(*k).is_some_and(|t| t.elapsed() < DUREE_POP + Duration::from_millis(150)))
    })
}

/// L'échelle du pop au temps `t` (0 à 1) : monte à 1,25, redescend à 0,95, se pose à 1.
pub(crate) fn echelle_pop(t: f32) -> f32 {
    let doux = |x: f32| {
        let x = x.clamp(0., 1.);
        x * x * (3. - 2. * x)
    };
    if t < 0.45 {
        let x = t / 0.45;
        // Sortie rapide (ease-out).
        1.25 * (1. - (1. - x) * (1. - x))
    } else if t < 0.72 {
        1.25 + (0.95 - 1.25) * doux((t - 0.45) / 0.27)
    } else {
        0.95 + (1. - 0.95) * doux((t - 0.72) / 0.28)
    }
}

impl Jasette {
    /// Un émoji seul, sans bulle ni fond : `taille` fois la taille du texte des
    /// messages ; la place est réservée, le pop ne pousse pas le fil.
    pub(crate) fn emoji_seul_vue(&self, m: &Msg, flou: bool) -> AnyElement {
        let taille = 16. * self.reglages.taille_messages * ECHELLE_EMOJI_SEUL;
        let texte = m.text.trim().to_string();
        let cadre = div().flex().items_center().justify_center().h(px(taille * 1.25)).min_w(px(taille * 1.25)).px(px(2.));
        if flou {
            return cadre.into_any_element();
        }
        let emoji = |s: f32| {
            div()
                .text_size(px(taille * s))
                .line_height(px(taille * 1.25 * s.max(0.01)))
                .child(texte_emoji(texte.clone(), th().texte_fort, None))
        };
        if en_cours(m) {
            let id = SharedString::from(format!("pop-{}", cle(m)));
            cadre.child(emoji(1.).with_animation(id, Animation::new(DUREE_POP), move |d, t| {
                let s = echelle_pop(t);
                d.text_size(px(taille * s)).line_height(px(taille * 1.25 * s.max(0.01)))
            }))
            .into_any_element()
        } else {
            cadre.child(emoji(1.)).into_any_element()
        }
    }
}

#[cfg(test)]
mod essais {
    use super::*;

    #[test]
    fn courbe() {
        assert!(echelle_pop(0.) < 0.01);
        assert!((echelle_pop(0.45) - 1.25).abs() < 0.01);
        assert!((echelle_pop(0.72) - 0.95).abs() < 0.01);
        assert!((echelle_pop(1.) - 1.).abs() < 0.001);
    }
}
