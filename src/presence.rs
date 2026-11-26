// La présence en ligne des contacts (serveur/PROTOCOLE.md, §5.10) : le point
// vert sur l'avatar (liste, Head, en-tête de la conversation) et « Actif il y
// a 12 min » sous le nom. Réglage « Afficher qui est en ligne » (marque
// `presence_en_ligne`, vrai par défaut).
use super::*;

/// Ce que le pont a dit d'une conversation individuelle.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub(crate) struct Presence {
    pub online: bool,
    /// Dernière activité connue (ms) ; 0 = inconnue.
    pub last_active: i64,
}

/// Au-delà, « Actif il y a… » ne dit plus rien.
const ACTIVITE_MAX_MS: i64 = 24 * 3600 * 1000;

/// « Actif maintenant », « Actif il y a 12 min », « Actif il y a 3 h », ou
/// rien (inconnue, ou plus de 24 h).
pub(crate) fn texte_activite(p: Presence, maintenant: i64) -> Option<String> {
    if p.online {
        return Some(t("Actif maintenant").to_string());
    }
    if p.last_active <= 0 {
        return None;
    }
    let ecart = (maintenant - p.last_active).max(0);
    if ecart >= ACTIVITE_MAX_MS {
        return None;
    }
    let minutes = (ecart / 60_000).max(1);
    Some(if minutes < 60 {
        if anglais() { format!("Active {minutes} min ago") } else { format!("Actif il y a {minutes} min") }
    } else {
        let heures = minutes / 60;
        if anglais() { format!("Active {heures} h ago") } else { format!("Actif il y a {heures} h") }
    })
}

/// Le point vert, en bas à gauche d'un avatar de `taille` px (à l'opposé du
/// badge du service), cerclé de la couleur du fond `fond`. Carré dans les
/// thèmes aux avatars pixelisés ou carrés (EGA, Live, Sirène).
pub(crate) fn point_presence(taille: f32, fond: Hsla) -> Div {
    let d = (taille * 0.26).round().clamp(9., 16.);
    let carre = th().ega || retro::retro();
    let vert = if th().ega {
        c(0x55ff55)
    } else if retro::retro() {
        c(0x2fb52a)
    } else {
        c(0x31c45a)
    };
    div()
        .absolute()
        .left(px(if carre { -1. } else { taille * 0.02 }))
        .bottom(px(if carre { -1. } else { taille * 0.02 }))
        .size(px(d))
        .when(!carre, |p| p.rounded(px(d)))
        .bg(vert)
        .border_2()
        .border_color(fond)
}

/// Live et Sirène : leurs rangées de liste n'ont pas d'avatar ; un petit carré
/// vert suit le nom.
pub(crate) fn carre_en_ligne() -> Div {
    div().flex_none().size(px(8.)).bg(c(0x2fb52a)).border_1().border_color(c(0x1d7a19))
}

impl Jasette {
    /// Une ligne `presence` d'un pont (ids déjà préfixés).
    pub(crate) fn presence_recue(&mut self, v: &Value) {
        if !self.reglages.presence_en_ligne {
            return;
        }
        let item = &v["item"];
        let Some(conv) = item.get("conv").and_then(Value::as_str).filter(|c| !c.is_empty()) else {
            return;
        };
        let p = Presence {
            online: item.get("online").and_then(Value::as_bool).unwrap_or(false),
            last_active: item.get("last_active").and_then(Value::as_i64).unwrap_or(0),
        };
        if self.presences.len() > 2000 && !self.presences.contains_key(conv) {
            return;
        }
        self.presences.insert(conv.to_string(), p);
    }

    /// La présence d'une conversation individuelle, si le réglage l'affiche.
    pub(crate) fn presence_de(&self, conv: &Conv) -> Option<Presence> {
        if !self.reglages.presence_en_ligne || conv.group {
            return None;
        }
        self.presences.get(&conv.id).copied()
    }

    /// Le point vert ?
    pub(crate) fn en_ligne(&self, conv: &Conv) -> bool {
        self.presence_de(conv).is_some_and(|p| p.online)
    }

    /// Le sous-titre de l'en-tête : « Actif maintenant », « Actif il y a 12 min ».
    pub(crate) fn activite(&self, conv: &Conv) -> Option<String> {
        texte_activite(self.presence_de(conv)?, maintenant_ms())
    }

    /// Le réglage vient de changer (ici ou sur un autre appareil) : les ponts
    /// locaux le reçoivent (le serveur Jasette, lui, prévient les siens), et
    /// désactivé, tout est oublié.
    pub(crate) fn presence_reglee(&mut self) {
        let oui = self.reglages.presence_en_ligne;
        pont::regler_presence(oui);
        if !oui {
            self.presences.clear();
            self.ecrits.clear();
        }
        for svc in [Service::Messenger, Service::Instagram, Service::WhatsApp, Service::Telegram] {
            if let Some(p) = self.meta(svc).pont.as_ref()
                && p.local()
            {
                p.envoyer(&json!({"cmd": "presence", "on": oui}));
            }
        }
    }
}

#[cfg(test)]
mod essais {
    use super::*;

    #[test]
    fn activite() {
        let m = 1_000_000_000_000;
        assert_eq!(texte_activite(Presence { online: true, last_active: 0 }, m).as_deref(), Some("Actif maintenant"));
        assert_eq!(texte_activite(Presence { online: false, last_active: 0 }, m), None);
        assert_eq!(texte_activite(Presence { online: false, last_active: m - 12 * 60_000 }, m).as_deref(), Some("Actif il y a 12 min"));
        assert_eq!(texte_activite(Presence { online: false, last_active: m - 10_000 }, m).as_deref(), Some("Actif il y a 1 min"));
        assert_eq!(texte_activite(Presence { online: false, last_active: m - 3 * 3600_000 - 5 }, m).as_deref(), Some("Actif il y a 3 h"));
        assert_eq!(texte_activite(Presence { online: false, last_active: m - 25 * 3600_000 }, m), None);
    }
}
