//! Le spam par conversation : « Marquer comme spam » (menu « … » de la
//! conversation ouverte et clic droit dans la liste, juste sous « Classer… »)
//! range la conversation dans le dossier Spam et la met en sourdine sans fin.
//! Elle quitte la boîte de réception, les groupes automatiques, le Head, les
//! favoris, les épinglées, les archives et la recherche ; ses nouveaux
//! messages n'y reviennent pas, ne notifient jamais et ne comptent pas dans
//! les non-lus (boîte, icône).
//!
//! Gardé dans les réglages (`spam` : id → heure du marquage en ms) et partagé
//! par le serveur Jasette s'il y en a un (marque `spam`, serveur/PROTOCOLE.md
//! §5.5). « Ce n'est pas du spam » retire la marque et la sourdine sans fin
//! posée avec elle.
//!
//! Le dossier Spam est dans Réglages > Spam : la liste des conversations
//! marquées (« Ce n'est pas du spam », « Supprimer » là où le service sait
//! supprimer une conversation, « Tout vider ») ; un clic sur l'une l'ouvre
//! dans la vue Spam de la liste. Une conversation supprimée garde sa marque :
//! si l'expéditeur réécrit, la conversation revient directement dans Spam.
//!
//! Pour une conversation Google Messages à deux (SMS/RCS), la fenêtre de
//! confirmation offre aussi « Marquer comme spam et envoyer STOP » : le texte
//! exact `STOP` part par la commande `send` ordinaire, seulement sur ce clic.

use super::*;
use std::collections::BTreeMap;

/// Le texte envoyé pour se désinscrire des envois massifs par SMS.
pub(crate) const TEXTE_STOP: &str = "STOP";

/// « Envoyer STOP » n'est offert que pour une conversation Google Messages
/// (SMS/RCS) à deux.
pub(crate) fn stop_offert(id: &str, groupe: bool, grand: bool) -> bool {
    service_de(id) == Service::GoogleMessages && !groupe && !grand
}

/// La conversation `id` est-elle marquée comme spam ?
pub(crate) fn marquee(spam: &BTreeMap<String, i64>, id: &str) -> bool {
    spam.contains_key(id)
}

/// Une notification système est-elle permise pour un nouveau message de
/// `id` ? Jamais pour le spam (même si sa sourdine a été levée), ni pour une
/// conversation en sourdine (src/sourdine.rs).
pub(crate) fn notification_permise(spam: &BTreeMap<String, i64>, sourdine: &BTreeMap<String, i64>, id: &str, maintenant: i64) -> bool {
    !marquee(spam, id) && !sourdine::active(sourdine, id, maintenant)
}

/// Les conversations du dossier Spam qu'on peut supprimer (le service sait
/// le faire) et celles qui restent.
pub(crate) fn a_vider(ids: &[String]) -> (Vec<String>, Vec<String>) {
    ids.iter().cloned().partition(|id| service_de(id).supprimable())
}

/// La fenêtre « Marquer comme spam » : la conversation, son nom, et si
/// « envoyer STOP » est offert.
#[derive(Clone)]
pub(crate) struct ModaleSpam {
    pub id: String,
    pub nom: String,
    pub stop: bool,
}

/// L'état du spam à l'écran : la fenêtre de confirmation et les
/// confirmations du dossier Spam (une suppression, « Tout vider »).
#[derive(Default)]
pub(crate) struct EtatSpam {
    pub modale: Option<ModaleSpam>,
    /// « Supprimer » cliqué une fois sur cette conversation du dossier.
    pub confirmer: Option<String>,
    /// « Tout vider » cliqué une fois.
    pub vider: bool,
}

impl Jasette {
    /// La conversation `id` est marquée comme spam.
    pub(crate) fn en_spam(&self, id: &str) -> bool {
        marquee(&self.reglages.spam, id)
    }

    /// Le dossier Spam : chargées, archivées ou en cache (pas les privées),
    /// les plus récemment marquées d'abord.
    pub(crate) fn liste_spam(&self) -> Vec<&Conv> {
        let mut vues = HashSet::new();
        let mut l: Vec<&Conv> = self
            .convs
            .iter()
            .chain(self.archives.iter())
            .chain(self.fantomes.iter())
            .filter(|c| self.en_spam(&c.id) && !self.privee(&c.id) && vues.insert(c.id.as_str()))
            .collect();
        let quand = |c: &Conv| self.reglages.spam.get(&c.id).copied().unwrap_or(0);
        l.sort_by(|a, b| quand(b).cmp(&quand(a)).then(b.time.cmp(&a.time)));
        l
    }

    /// Marque `id` comme spam : sourdine sans fin, hors du Head, des favoris
    /// et des épinglées ; `stop` : envoie aussi « STOP » (Google à deux
    /// seulement, sur clic explicite).
    pub(crate) fn marquer_spam(&mut self, id: &str, stop: bool) {
        let conv = self.conv(id).map(|c| (c.group, c.large));
        if stop
            && let Some((groupe, grand)) = conv
            && stop_offert(id, groupe, grand)
            && self.etat_de(Service::GoogleMessages) == Etat::Pret
        {
            let tmp = self.en_attente(id, TEXTE_STOP.to_string(), String::new(), Vec::new());
            self.commande(json!({"cmd": "send", "conv": id, "text": TEXTE_STOP, "tmp_id": tmp}));
        }
        let r = &mut self.reglages;
        r.spam.insert(id.to_string(), maintenant_ms());
        r.sourdine.insert(id.to_string(), 0);
        r.head.retain(|x| x != id);
        r.etoiles.retain(|x| x != id);
        r.epingles.retain(|x| x != id);
        if self.choisie.as_deref() == Some(id) && self.vue != Vue::Spam {
            self.choisie = None;
            self.menu_conv = false;
            self.fil_cherche_ouvert = false;
        }
        self.resultats_msgs.retain(|m| m.conv != id);
        self.marques_changees();
    }

    /// « Ce n'est pas du spam » : la marque s'en va, et la sourdine sans fin
    /// posée avec elle.
    pub(crate) fn pas_spam(&mut self, id: &str) {
        let r = &mut self.reglages;
        if r.spam.remove(id).is_none() {
            return;
        }
        if r.sourdine.get(id) == Some(&0) {
            r.sourdine.remove(id);
        }
        self.marques_changees();
    }

    /// L'entrée de menu : « Ce n'est pas du spam » pour une conversation
    /// marquée, sinon « Marquer comme spam » (qui ouvre la confirmation).
    pub(crate) fn entree_spam(&self, id: &str) -> (&'static str, &'static str) {
        if self.en_spam(id) {
            ("shield-check", t("Ce n'est pas du spam"))
        } else {
            ("shield-alert", t("Marquer comme spam"))
        }
    }

    /// Le geste de l'entrée de menu (voir `entree_spam`).
    pub(crate) fn basculer_spam(&mut self, id: String) {
        if self.en_spam(&id) {
            self.pas_spam(&id);
            return;
        }
        let Some(c) = self.conv(&id) else { return };
        let nom = self.nom_affiche(c);
        let stop = stop_offert(&id, c.group, c.large);
        self.spam.modale = Some(ModaleSpam { id, nom, stop });
    }

    /// Ouvre une conversation du dossier Spam dans la vue Spam de la liste.
    pub(crate) fn ouvrir_spam(&mut self, id: String) {
        self.section = None;
        self.galerie = None;
        self.vue = Vue::Spam;
        self.spam.confirmer = None;
        self.spam.vider = false;
        self.choisir(id);
    }

    /// « Tout vider » : supprime les conversations du dossier dont le service
    /// sait le faire (la même commande que « Supprimer la conversation ») ;
    /// les autres restent. Les marques restent : si l'expéditeur réécrit,
    /// la conversation revient droit dans Spam.
    pub(crate) fn vider_spam(&mut self) {
        let ids: Vec<String> = self.liste_spam().iter().map(|c| c.id.clone()).collect();
        let (oui, _) = a_vider(&ids);
        for id in oui {
            self.supprimer_conversation(&id);
        }
        self.spam.vider = false;
        self.spam.confirmer = None;
    }

    /// La fenêtre « Marquer comme spam » : « Marquer comme spam », pour un
    /// SMS « Marquer comme spam et envoyer STOP », et « Annuler ». Échap ou
    /// un clic dehors : fermée sans rien changer.
    pub(crate) fn modale_spam_vue(&self, cx: &mut Context<Self>) -> Option<AnyElement> {
        let m = self.spam.modale.clone()?;
        let rangee = |cle: &'static str, ic: &'static str, titre: &'static str, detail: Option<&'static str>, couleur: Hsla| {
            div()
                .id(cle)
                .flex()
                .flex_row()
                .items_center()
                .gap(px(12.))
                .px(px(10.))
                .py(px(9.))
                .rounded(rayon(12.))
                .cursor_pointer()
                .hover(|s| s.bg(th().survol))
                .child(
                    div()
                        .size(px(34.))
                        .flex_none()
                        .rounded(rayon(999.))
                        .border_1()
                        .border_color(th().trait_fin)
                        .flex()
                        .items_center()
                        .justify_center()
                        .child(icone(ic, 17., couleur)),
                )
                .child(
                    div()
                        .flex_1()
                        .min_w(px(0.))
                        .flex()
                        .flex_col()
                        .gap(px(1.))
                        .child(div().text_size(px(14.5)).font_weight(FontWeight::SEMIBOLD).text_color(couleur).child(titre))
                        .children(detail.map(|d| div().text_size(px(12.)).text_color(th().texte_pale).child(d))),
                )
        };
        let id = m.id.clone();
        let mut liste = div().flex().flex_col().gap(px(2.)).child(
            rangee("spam-marquer", "shield-alert", t("Marquer comme spam"), None, th().texte_fort).on_click(cx.listener(
                move |this, _, _, cx| {
                    cx.stop_propagation();
                    this.spam.modale = None;
                    this.marquer_spam(&id, false);
                    cx.notify();
                },
            )),
        );
        if m.stop {
            let id = m.id.clone();
            liste = liste.child(
                rangee(
                    "spam-stop",
                    "message-square-x",
                    t("Marquer comme spam et envoyer STOP"),
                    Some(t("Envoie le message STOP à ce numéro pour te désinscrire.")),
                    th().texte_fort,
                )
                .on_click(cx.listener(move |this, _, _, cx| {
                    cx.stop_propagation();
                    this.spam.modale = None;
                    this.marquer_spam(&id, true);
                    cx.notify();
                })),
            );
        }
        liste = liste.child(rangee("spam-annuler", "x", t("Annuler"), None, th().texte_pale).on_click(cx.listener(|this, _, _, cx| {
            cx.stop_propagation();
            this.spam.modale = None;
            cx.notify();
        })));
        Some(
            div()
                .id("modale-spam")
                .absolute()
                .inset_0()
                .occlude()
                .flex()
                .items_center()
                .justify_center()
                .bg(hsla(0., 0., 0., 0.35))
                .on_click(cx.listener(|this, _, _, cx| {
                    this.spam.modale = None;
                    cx.notify();
                }))
                .child(
                    groupe()
                        .id("modale-spam-carte")
                        .when(!th().sombre && !th().ega, |d| d.bg(blanc(0.97)))
                        .w(px(400.))
                        .px(px(14.))
                        .py(px(14.))
                        .gap(px(8.))
                        .on_click(|_, _, cx| cx.stop_propagation())
                        .child(
                            div()
                                .px(px(10.))
                                .flex()
                                .flex_col()
                                .gap(px(2.))
                                .child(div().text_size(px(16.)).font_weight(FontWeight::BOLD).text_color(th().texte_fort).child(t("Marquer comme spam")))
                                .child(div().truncate().text_size(px(12.5)).text_color(th().texte_pale).child(m.nom))
                                .child(div().pt(px(4.)).text_size(px(12.5)).text_color(th().texte_pale).child(t(
                                    "Elle quitte la boîte de réception, ne notifie plus et va dans Réglages > Spam.",
                                ))),
                        )
                        .child(liste),
                )
                .into_any_element(),
        )
    }

    /// Réglages > Spam : le dossier. Chaque conversation (avatar, nom, aperçu,
    /// date du marquage) s'ouvre d'un clic ; « Ce n'est pas du spam »,
    /// « Supprimer » (deux clics, là où le service le permet) ; « Tout vider ».
    pub(crate) fn reglages_spam(&self, cx: &mut Context<Self>) -> AnyElement {
        let l = self.liste_spam();
        let ids: Vec<String> = l.iter().map(|c| c.id.clone()).collect();
        let (oui, non) = a_vider(&ids);
        let mut carte = carte_section("Spam");
        if l.is_empty() {
            return carte
                .child(groupe().child(div().px(px(16.)).py(px(16.)).text_color(th().texte_pale).child(t(
                    "Aucune conversation marquée comme spam. « Marquer comme spam » est dans le menu d'une conversation, sous « Classer… ».",
                ))))
                .into_any_element();
        }
        // « Tout vider » : deux clics, avec le nombre.
        let texte_vider = if self.spam.vider {
            if anglais() {
                format!("Delete {} conversation(s)?", oui.len())
            } else {
                format!("Supprimer {} conversation(s) ?", oui.len())
            }
        } else {
            t("Tout vider").to_string()
        };
        let mut haut = div().flex().flex_row().items_center().gap(px(8.)).pb(px(10.)).child(
            div().flex_1().text_size(px(12.5)).text_color(th().texte_pale).child(if anglais() {
                format!("{} conversation(s) · no notifications, out of the inbox", l.len())
            } else {
                format!("{} conversation(s) · sans notification, hors de la boîte", l.len())
            }),
        );
        if !oui.is_empty() {
            haut = haut.child(
                div()
                    .id("spam-vider")
                    .px(px(14.))
                    .py(px(6.))
                    .rounded(rayon(999.))
                    .text_size(px(13.))
                    .font_weight(FontWeight::SEMIBOLD)
                    .cursor_pointer()
                    .bg(alpha(th().danger, if self.spam.vider { 0.18 } else { 0.1 }))
                    .text_color(th().danger)
                    .hover(|s| s.bg(alpha(th().danger, 0.22)))
                    .child(texte_vider)
                    .on_click(cx.listener(|this, _, _, cx| {
                        if this.spam.vider {
                            this.vider_spam();
                        } else {
                            this.spam.vider = true;
                            this.spam.confirmer = None;
                        }
                        cx.notify();
                    })),
            );
            if self.spam.vider {
                haut = haut.child(bouton_texte("spam-vider-annuler", t("Annuler"), 0x64748b).on_click(cx.listener(|this, _, _, cx| {
                    this.spam.vider = false;
                    cx.notify();
                })));
            }
        }
        carte = carte.child(haut);
        let mut g = groupe();
        let n = l.len();
        for (i, c) in l.into_iter().enumerate() {
            let nom = self.nom_affiche(c);
            let quand = self.reglages.spam.get(&c.id).copied().unwrap_or(0);
            let detail = if quand > 0 {
                format!("{} · {}", if anglais() { format!("Marked {}", heure(quand, true)) } else { format!("Marquée {}", heure(quand, true)) }, c.preview)
            } else {
                c.preview.clone()
            };
            let supprimable = service_de(&c.id).supprimable();
            let confirmer = self.spam.confirmer.as_deref() == Some(c.id.as_str());
            let (id_o, id_p, id_s) = (c.id.clone(), c.id.clone(), c.id.clone());
            let mut rang = div()
                .id(SharedString::from(format!("spam-{}", c.id)))
                .flex()
                .flex_row()
                .items_center()
                .gap(px(12.))
                .px(px(14.))
                .py(px(10.))
                .child(
                    div()
                        .id(SharedString::from(format!("spam-ouvrir-{}", c.id)))
                        .flex_1()
                        .min_w(px(0.))
                        .flex()
                        .flex_row()
                        .items_center()
                        .gap(px(12.))
                        .cursor_pointer()
                        .hover(|s| s.opacity(0.8))
                        .on_click(cx.listener(move |this, _, _, cx| {
                            this.ouvrir_spam(id_o.clone());
                            cx.notify();
                        }))
                        .child(self.avatar_conv(c, &nom, 40.))
                        .child(
                            div()
                                .flex_1()
                                .min_w(px(0.))
                                .flex()
                                .flex_col()
                                .gap(px(2.))
                                .child(div().truncate().font_weight(FontWeight::SEMIBOLD).text_color(th().texte_fort).child(texte_emoji(
                                    nom.clone(),
                                    th().texte_fort,
                                    Some(FontWeight::SEMIBOLD),
                                )))
                                .child(div().truncate().text_size(px(12.)).text_color(th().texte_pale).child(detail)),
                        ),
                )
                .child(bouton_texte(SharedString::from(format!("spam-pas-{}", c.id)), t("Ce n'est pas du spam"), ACCENT_2).on_click(
                    cx.listener(move |this, _, _, cx| {
                        this.pas_spam(&id_p);
                        cx.notify();
                    }),
                ));
            if supprimable {
                rang = rang.child(
                    div()
                        .id(SharedString::from(format!("spam-suppr-{}", c.id)))
                        .px(px(12.))
                        .py(px(6.))
                        .rounded(rayon(999.))
                        .text_size(px(13.))
                        .font_weight(FontWeight::SEMIBOLD)
                        .cursor_pointer()
                        .text_color(th().danger)
                        .bg(alpha(th().danger, if confirmer { 0.18 } else { 0.08 }))
                        .hover(|s| s.bg(alpha(th().danger, 0.22)))
                        .child(if confirmer { t("Confirmer la suppression") } else { t("Supprimer") })
                        .on_click(cx.listener(move |this, _, _, cx| {
                            if this.spam.confirmer.as_deref() == Some(id_s.as_str()) {
                                this.supprimer_conversation(&id_s);
                                this.spam.confirmer = None;
                            } else {
                                this.spam.confirmer = Some(id_s.clone());
                                this.spam.vider = false;
                            }
                            cx.notify();
                        })),
                );
            }
            g = g.child(rang);
            if i + 1 < n {
                g = g.child(separateur());
            }
        }
        carte = carte.child(g);
        if !non.is_empty() {
            let note = if anglais() {
                format!(
                    "{} conversation(s) can't be deleted from Jasette for their service: they stay here, silent. Delete them in the service's app if needed.",
                    non.len()
                )
            } else {
                format!(
                    "{} conversation(s) ne peuvent pas être supprimée(s) depuis Jasette pour leur service : elles restent ici, sans bruit. Supprime-les dans l'app du service au besoin.",
                    non.len()
                )
            };
            carte = carte.child(div().px(px(6.)).pt(px(12.)).text_size(px(12.)).text_color(th().texte_pale).child(note));
        }
        carte.into_any_element()
    }
}

#[cfg(test)]
mod essais {
    use super::*;

    /// « Envoyer STOP » : seulement une conversation Google à deux.
    #[test]
    fn stop_seulement_google_a_deux() {
        assert!(stop_offert("15145551234", false, false));
        assert!(!stop_offert("15145551234", true, false));
        assert!(!stop_offert("m:42", false, false));
        assert!(!stop_offert("w:5551@s.whatsapp.net", false, false));
        assert!(!stop_offert("s:+15145551234", false, false));
        assert!(!stop_offert("d:9", false, true));
    }

    /// « Tout vider » : seulement les services qui savent supprimer.
    #[test]
    fn vider_selon_service() {
        let ids: Vec<String> = ["123", "m:1", "i:2", "w:3", "t:4"].iter().map(|s| s.to_string()).collect();
        let (oui, non) = a_vider(&ids);
        assert_eq!(oui, vec!["123", "m:1", "i:2"]);
        assert_eq!(non, vec!["w:3", "t:4"]);
    }

    #[test]
    fn marque() {
        let s = BTreeMap::from([("m:1".to_string(), 5)]);
        assert!(marquee(&s, "m:1"));
        assert!(!marquee(&s, "m:2"));
    }

    /// Le filtre de notification : le spam ne notifie jamais, même pour un
    /// nouveau message et même sans sourdine ; le reste suit la sourdine.
    #[test]
    fn spam_jamais_notifie() {
        let maint = 1_000_000;
        let spam = BTreeMap::from([("15145550000".to_string(), 10)]);
        let avec_sourdine = BTreeMap::from([("15145550000".to_string(), 0)]);
        let vide = BTreeMap::new();
        assert!(!notification_permise(&spam, &avec_sourdine, "15145550000", maint));
        // Sourdine levée ailleurs (ou échue) : toujours rien.
        assert!(!notification_permise(&spam, &vide, "15145550000", maint));
        assert!(!notification_permise(&spam, &vide, "15145550000", i64::MAX));
        // Les autres conversations notifient comme avant.
        assert!(notification_permise(&spam, &vide, "m:1", maint));
        let sourdine = BTreeMap::from([("m:1".to_string(), maint + 1)]);
        assert!(!notification_permise(&vide, &sourdine, "m:1", maint));
        assert!(notification_permise(&vide, &sourdine, "m:1", maint + 1));
    }
}
