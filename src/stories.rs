// Les stories des contacts (serveur/PROTOCOLE.md, §5.15) : un cercle autour
// de l'avatar d'un contact qui a une story Instagram ou Facebook active ; le
// cliquer ouvre la story dans le navigateur. Jasette n'affiche pas la story,
// ne télécharge aucun média et ne marque jamais rien comme vu chez Meta.
//
// Le réglage (Réglages > Comptes) est la marque partagée `stories` ; la marque
// `stories_ouvertes` retient, par conversation, l'heure de la story la plus
// récente ouverte depuis Jasette : le cercle disparaît alors sur tous les
// appareils, et revient avec une story plus récente.
use super::*;

/// La sorte de cercle : son dégradé.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub(crate) enum SorteStory {
    Instagram,
    Facebook,
}

/// La story active d'un contact, telle que son pont l'annonce.
#[derive(Clone, PartialEq)]
pub(crate) struct StoryContact {
    /// Le service qui l'a annoncée (sa liste remplace la précédente).
    pub svc: Service,
    pub sorte: SorteStory,
    /// L'heure (ms) de la plus récente ; 0 : inconnue.
    pub latest: i64,
    /// L'échéance (ms).
    pub until: i64,
    /// Déjà vue (dans l'app officielle ou ailleurs).
    pub seen: bool,
    pub link: String,
}

#[derive(Default)]
pub(crate) struct Stories {
    /// Par id de conversation (avec le préfixe du service).
    pub par_conv: HashMap<String, StoryContact>,
    /// Pourquoi un service ne donne rien (`indispo`), pour les Réglages.
    pub indispo: HashMap<Service, String>,
    dernier_tic: Option<Instant>,
}

/// Le cercle se montre-t-il ? Story à venir, pas vue, et plus récente que
/// celle ouverte depuis Jasette (`ouverte`, marque `stories_ouvertes`).
pub(crate) fn story_visible(s: &StoryContact, ouverte: Option<i64>, maintenant: i64) -> bool {
    s.until > maintenant && !s.seen && ouverte.is_none_or(|t| s.latest > t)
}

/// Un lien de story qu'on accepte d'ouvrir : Instagram ou Facebook, en https.
fn lien_sur(l: &str) -> bool {
    l.starts_with("https://www.instagram.com/stories/") || l.starts_with("https://www.facebook.com/stories/")
}

/// Lit un événement `stories` (ids déjà préfixés par `prefixer_meta`).
pub(crate) fn lire_stories(svc: Service, v: &Value) -> Vec<(String, StoryContact)> {
    let mut r = Vec::new();
    for it in v.get("items").and_then(Value::as_array).into_iter().flatten() {
        let (Some(conv), Some(link)) = (it.get("conv").and_then(Value::as_str), it.get("link").and_then(Value::as_str)) else {
            continue;
        };
        if conv.is_empty() || !lien_sur(link) {
            continue;
        }
        let sorte = match it.get("service").and_then(Value::as_str) {
            Some("facebook") => SorteStory::Facebook,
            Some("instagram") => SorteStory::Instagram,
            _ if svc == Service::Messenger => SorteStory::Facebook,
            _ => SorteStory::Instagram,
        };
        r.push((
            conv.to_string(),
            StoryContact {
                svc,
                sorte,
                latest: it.get("latest").and_then(Value::as_i64).unwrap_or(0),
                until: it.get("until").and_then(Value::as_i64).unwrap_or(0),
                seen: it.get("seen").and_then(Value::as_bool).unwrap_or(false),
                link: link.to_string(),
            },
        ));
    }
    r
}

impl Jasette {
    /// `stories` (Instagram ou Messenger) : la liste complète de ce service.
    pub(crate) fn stories_recues(&mut self, svc: Service, v: &Value) {
        if !self.reglages.stories {
            return;
        }
        self.stories.par_conv.retain(|_, s| s.svc != svc);
        for (conv, s) in lire_stories(svc, v) {
            self.stories.par_conv.insert(conv, s);
        }
        match v.get("indispo").and_then(Value::as_str).filter(|t| !t.trim().is_empty()) {
            Some(t) => {
                self.stories.indispo.insert(svc, t.trim().to_string());
            }
            None => {
                self.stories.indispo.remove(&svc);
            }
        }
    }

    /// La marque `stories` vient de changer (ici ou ailleurs) : les ponts
    /// locaux le reçoivent (le serveur prévient les siens).
    pub(crate) fn stories_reglees(&mut self) {
        let oui = self.reglages.stories;
        pont::regler_stories(oui);
        if !oui {
            self.stories.par_conv.clear();
            self.stories.indispo.clear();
        }
        for svc in [Service::Instagram, Service::Messenger] {
            if let Some(p) = self.meta(svc).pont.as_ref()
                && p.local()
            {
                p.envoyer(&json!({"cmd": "stories", "on": oui}));
            }
        }
    }

    /// La story à montrer pour cette conversation (ou sa personne), et l'id
    /// de la conversation qui l'a : Instagram d'abord. Jamais pour un groupe.
    pub(crate) fn story_de(&self, conv: &Conv) -> Option<(&str, &StoryContact)> {
        if !self.reglages.stories || self.stories.par_conv.is_empty() || conv.group || conv.large {
            return None;
        }
        let maintenant = maintenant_ms();
        let visible = |id: &str| {
            let (k, s) = self.stories.par_conv.get_key_value(id)?;
            story_visible(s, self.reglages.stories_ouvertes.get(id).copied(), maintenant).then_some((k.as_str(), s))
        };
        let mut trouvees: Vec<(&str, &StoryContact)> = visible(&conv.id).into_iter().collect();
        if let Some(p) = self.reglages.personnes.iter().find(|p| p.convs.iter().any(|c| *c == conv.id)) {
            trouvees.extend(p.convs.iter().filter(|c| **c != conv.id).filter_map(|c| visible(c)));
        }
        trouvees.into_iter().min_by_key(|(_, s)| s.sorte != SorteStory::Instagram)
    }

    /// Ouvre la story de cette conversation dans le navigateur, et retient
    /// qu'elle l'a été (le cercle s'en va, sur tous les appareils).
    pub(crate) fn ouvrir_story(&mut self, conv_id: &str) {
        let Some(conv) = self.conv(conv_id).cloned() else { return };
        let Some((id, s)) = self.story_de(&conv).map(|(id, s)| (id.to_string(), s.clone())) else { return };
        ouvrir_adresse(&s.link);
        let ouverte = self.reglages.stories_ouvertes.entry(id).or_insert(0);
        *ouverte = (*ouverte).max(s.latest.max(1));
        self.marques_changees();
    }

    /// Appelé par la boucle des nouvelles : toutes les 30 s, une story échue
    /// s'en va. Vrai si l'écran doit être redessiné.
    pub(crate) fn stories_tic(&mut self) -> bool {
        if self.stories.par_conv.is_empty() || self.stories.dernier_tic.is_some_and(|t| t.elapsed() < Duration::from_secs(30)) {
            return false;
        }
        self.stories.dernier_tic = Some(Instant::now());
        let maintenant = maintenant_ms();
        let avant = self.stories.par_conv.len();
        self.stories.par_conv.retain(|_, s| s.until > maintenant);
        self.stories.par_conv.len() != avant
    }

    /// L'avatar d'une conversation, cerclé si son contact a une story.
    pub(crate) fn avatar_story(&self, conv: &Conv, taille: f32, contenu: impl FnOnce(f32) -> AnyElement) -> AnyElement {
        match self.story_de(conv) {
            Some((_, s)) => anneau_story(s.sorte, taille, contenu),
            None => contenu(taille),
        }
    }

    /// Un avatar cerclé qu'on peut cliquer pour ouvrir la story (dans une
    /// rangée de liste : le reste de la rangée ouvre la conversation). Sans
    /// story, l'avatar tel quel, sans clic propre.
    pub(crate) fn avatar_story_cliquable(
        &self,
        id_element: impl Into<SharedString>,
        conv: &Conv,
        avatar: AnyElement,
        cx: &mut Context<Self>,
    ) -> AnyElement {
        if self.story_de(conv).is_none() {
            return avatar;
        }
        let id = conv.id.clone();
        div()
            .id(gpui::ElementId::Name(id_element.into()))
            .flex_none()
            .cursor_pointer()
            .child(avatar)
            .on_click(cx.listener(move |this, _, _, cx| {
                cx.stop_propagation();
                this.ouvrir_story(&id);
                cx.notify();
            }))
            .tooltip(|w, cx| gpui_component::tooltip::Tooltip::new(notes::tr("Voir la story", "View story")).build(w, cx))
            .into_any_element()
    }
}

impl Jasette {
    /// Réglages > Comptes : « Afficher les stories Facebook et Instagram »
    /// (marque partagée `stories`), et ce qui manque à un service (`indispo`).
    pub(crate) fn stories_reglages(&self, cx: &mut Context<Self>) -> Div {
        let mut bloc = div().mt(px(22.)).flex().flex_col().gap(px(8.)).child(
            groupe().child(ligne_interrupteur(
                "reglage-stories",
                notes::tr("Afficher les stories Facebook et Instagram", "Show Facebook and Instagram stories"),
                notes::tr(
                    "Un cercle autour de l'avatar d'un contact qui a une story ; cliquez-le pour l'ouvrir dans le navigateur. Jasette ne la marque jamais comme vue. Vérifié au plus toutes les 20 min.",
                    "A ring around the avatar of a contact who has a story; click it to open the story in your browser. Jasette never marks it as seen. Checked at most every 20 min.",
                ),
                self.reglages.stories,
                cx.listener(|this, _, _, cx| {
                    this.reglages.stories = !this.reglages.stories;
                    this.marques_changees();
                    this.stories_reglees();
                    cx.notify();
                }),
            )),
        );
        if self.reglages.stories {
            let mut indispo: Vec<(Service, &String)> = self.stories.indispo.iter().map(|(s, t)| (*s, t)).collect();
            indispo.sort_by_key(|(s, _)| s.cle());
            for (svc, texte) in indispo {
                bloc = bloc.child(
                    div()
                        .px(px(4.))
                        .flex()
                        .flex_row()
                        .items_center()
                        .gap(px(6.))
                        .text_size(px(12.))
                        .text_color(th().texte_pale)
                        .child(logo(svc, 14.))
                        .child(crate::ponts_langue::affiche(texte, false).into_owned()),
                );
            }
        }
        bloc
    }

    /// L'avatar d'une rangée de liste (boîte, archives, groupe ouvert,
    /// recherche) : cliquer le cercle ouvre la story, le reste de la rangée
    /// garde son geste. L'icône d'un groupe automatique n'a pas de cercle.
    pub(crate) fn avatar_liste_story(&self, id_element: String, conv: &Conv, taille: f32, cx: &mut Context<Self>) -> AnyElement {
        let a = self.avatar_liste(conv, taille);
        if self.groupe_detecte(conv).is_some() {
            return a;
        }
        self.avatar_story_cliquable(id_element, conv, a, cx)
    }
}

/// Les couleurs du dégradé, du coin bas gauche au coin haut droit.
fn couleurs_story(sorte: SorteStory) -> &'static [u32] {
    match sorte {
        SorteStory::Instagram => &[0xfeda75, 0xfa7e1e, 0xd62976, 0x962fbf, 0x4f5bd5],
        SorteStory::Facebook => &[0x1877f2, 0x00c6ff],
    }
}

/// La couleur du dégradé à `t` (0 à 1).
fn couleur_degrade(stops: &[u32], t: f32) -> Hsla {
    let t = t.clamp(0., 1.) * (stops.len() - 1) as f32;
    let i = (t.floor() as usize).min(stops.len() - 2);
    let f = t - i as f32;
    let (a, b) = (stops[i], stops[i + 1]);
    let canal = |d: u32| ((((a >> d) & 0xff) as f32 * (1. - f) + ((b >> d) & 0xff) as f32 * f).round() as u32) << d;
    c(canal(16) | canal(8) | canal(0))
}

/// L'épaisseur de l'anneau et l'espace entre l'anneau et la photo.
fn mesures_anneau(taille: f32) -> (f32, f32) {
    ((taille * 0.05).clamp(1.5, 4.), (taille * 0.045).clamp(1.5, 4.))
}

/// Un avatar dans un anneau de story, à la même taille extérieure (la photo
/// rapetisse) : le dégradé Instagram ou Facebook, avec un espace transparent
/// entre l'anneau et la photo, comme Instagram. EGA : un anneau plein en
/// couleurs EGA ; Live et Sirène : un cadre plein, simple.
pub(crate) fn anneau_story(sorte: SorteStory, taille: f32, contenu: impl FnOnce(f32) -> AnyElement) -> AnyElement {
    let (epais, espace) = mesures_anneau(taille);
    let dedans = (taille - 2. * (epais + espace)).max(8.);
    if retro::retro() {
        let couleur = c(match sorte {
            SorteStory::Instagram => 0xd62976,
            SorteStory::Facebook => 0x1877f2,
        });
        return div()
            .flex_none()
            .size(px(taille))
            .rounded(rayon(taille * 0.14))
            .border(px(epais))
            .border_color(couleur)
            .flex()
            .items_center()
            .justify_center()
            .child(contenu(dedans))
            .into_any_element();
    }
    let plein = th().ega.then(|| {
        c(match sorte {
            SorteStory::Instagram => EGA_MAGENTA_VIF,
            SorteStory::Facebook => 0x5555ff,
        })
    });
    div()
        .flex_none()
        .relative()
        .size(px(taille))
        .flex()
        .items_center()
        .justify_center()
        .child(contenu(dedans))
        .child(
            gpui::canvas(
                |_, _, _| {},
                move |bornes, _, fenetre, _| peindre_anneau(fenetre, bornes, sorte, plein, epais),
            )
            .absolute()
            .top_0()
            .left_0()
            .size_full(),
        )
        .into_any_element()
}

/// Peint l'anneau : des secteurs fins, chacun de la couleur du dégradé à sa
/// place sur la diagonale (bas gauche → haut droit), un peu chevauchés pour
/// qu'aucun joint ne se voie.
fn peindre_anneau(fenetre: &mut Window, bornes: Bounds<Pixels>, sorte: SorteStory, plein: Option<Hsla>, epais: f32) {
    let l = f32::from(bornes.size.width).min(f32::from(bornes.size.height));
    let cx0 = f32::from(bornes.origin.x) + f32::from(bornes.size.width) / 2.;
    let cy0 = f32::from(bornes.origin.y) + f32::from(bornes.size.height) / 2.;
    let r_ext = l / 2.;
    let r_int = (r_ext - epais).max(0.);
    let point_a = |r: f32, a: f32| point(px(cx0 + r * a.cos()), px(cy0 + r * a.sin()));
    let n: usize = if plein.is_some() { 1 } else { 48 };
    let pas = std::f32::consts::TAU / n as f32;
    let stops = couleurs_story(sorte);
    for i in 0..n {
        let (a0, a1) = if n == 1 { (0., std::f32::consts::TAU) } else { (i as f32 * pas - 0.01, (i + 1) as f32 * pas + 0.01) };
        let mut b = gpui::PathBuilder::fill();
        let sous = if n == 1 { 64 } else { 3 };
        let mut pts = Vec::with_capacity(2 * (sous + 1));
        for k in 0..=sous {
            pts.push(point_a(r_ext, a0 + (a1 - a0) * k as f32 / sous as f32));
        }
        for k in (0..=sous).rev() {
            pts.push(point_a(r_int, a0 + (a1 - a0) * k as f32 / sous as f32));
        }
        b.add_polygon(&pts, true);
        let Ok(chemin) = b.build() else { continue };
        let couleur = plein.unwrap_or_else(|| {
            let milieu = (a0 + a1) / 2.;
            // y vers le bas : le haut droit est cos > 0, sin < 0.
            let d = (milieu.cos() - milieu.sin()) / std::f32::consts::SQRT_2;
            couleur_degrade(stops, 0.5 + d / 2.)
        });
        fenetre.paint_path(chemin, couleur);
    }
}

#[cfg(test)]
mod essais {
    use super::*;

    fn s(latest: i64, until: i64, seen: bool) -> StoryContact {
        StoryContact {
            svc: Service::Instagram,
            sorte: SorteStory::Instagram,
            latest,
            until,
            seen,
            link: "https://www.instagram.com/stories/a/".into(),
        }
    }

    #[test]
    fn visible_seulement_active_pas_vue_pas_ouverte() {
        assert!(story_visible(&s(100, 1000, false), None, 500));
        assert!(!story_visible(&s(100, 1000, false), None, 1000), "échue");
        assert!(!story_visible(&s(100, 1000, true), None, 500), "vue ailleurs : pas d'anneau gris, rien");
        assert!(!story_visible(&s(100, 1000, false), Some(100), 500), "ouverte depuis Jasette");
        assert!(story_visible(&s(200, 1000, false), Some(100), 500), "une story plus récente : le cercle revient");
        // Heure inconnue (0) : ouverte une fois (1), cachée jusqu'à l'échéance.
        assert!(!story_visible(&s(0, 1000, false), Some(1), 500));
    }

    #[test]
    fn lecture_de_l_evenement() {
        let v = json!({"ev": "stories", "items": [
            {"conv": "i:1", "latest": 5, "until": 9, "seen": false, "link": "https://www.instagram.com/stories/a/", "service": "instagram"},
            {"conv": "i:2", "latest": 5, "until": 9, "link": "https://exemple.com/piege"},
            {"conv": "", "latest": 5, "until": 9, "link": "https://www.instagram.com/stories/b/"}
        ]});
        let r = lire_stories(Service::Instagram, &v);
        assert_eq!(r.len(), 1);
        assert_eq!(r[0].0, "i:1");
        let v = json!({"items": [{"conv": "m:7", "latest": 0, "until": 9, "link": "https://www.facebook.com/stories/7"}]});
        assert_eq!(lire_stories(Service::Messenger, &v)[0].1.sorte, SorteStory::Facebook);
    }

    #[test]
    fn degrade_aux_bouts() {
        let stops = couleurs_story(SorteStory::Instagram);
        assert_eq!(couleur_degrade(stops, 0.), c(0xfeda75));
        assert_eq!(couleur_degrade(stops, 1.), c(0x4f5bd5));
    }
}
