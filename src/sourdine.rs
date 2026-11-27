//! La sourdine par conversation : « Mettre en sourdine » coupe seulement les
//! notifications système de cette conversation (aucune bulle, aucun son de
//! notification) pour 1 h, 8 h, 24 h, 1 semaine ou « jusqu'à ce que je la
//! réactive ». Dans l'app, rien ne change (messages, non-lus, ordre de la
//! liste, sons du fil ouvert), sauf une cloche barrée (Lucide `bell-off`)
//! dans la liste, l'en-tête du fil et la fiche.
//!
//! Gardée dans les réglages (`sourdine` : id → échéance en ms, 0 = sans
//! fin) et partagée par le serveur Jasette s'il y en a un (marque
//! `sourdine`, serveur/PROTOCOLE.md §5.5). Une échéance passée ne compte
//! plus : les notifications reviennent et l'icône s'en va d'elles-mêmes
//! (`sourdine_tic`).
//!
//! Offerte par le menu « … » de la conversation ouverte, le clic droit dans
//! la liste et la fiche de la personne (alors pour toutes ses conversations
//! liées, src/personne.rs). Elle ouvre la modale des durées.

use super::*;
use chrono::{Datelike, NaiveDateTime, Timelike};
use std::collections::BTreeMap;

/// Les durées offertes par la modale.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub(crate) enum Duree {
    Heure,
    HuitHeures,
    Jour,
    Semaine,
    Toujours,
}

impl Duree {
    pub const TOUTES: [Duree; 5] = [Duree::Heure, Duree::HuitHeures, Duree::Jour, Duree::Semaine, Duree::Toujours];

    fn cle(self) -> &'static str {
        match self {
            Duree::Heure => "1h",
            Duree::HuitHeures => "8h",
            Duree::Jour => "24h",
            Duree::Semaine => "7j",
            Duree::Toujours => "toujours",
        }
    }

    fn libelle(self) -> &'static str {
        match self {
            Duree::Heure => t("Pendant 1 heure"),
            Duree::HuitHeures => t("Pendant 8 heures"),
            Duree::Jour => t("Pendant 24 heures"),
            Duree::Semaine => t("Pendant 1 semaine"),
            Duree::Toujours => t("Jusqu'à ce que je la réactive"),
        }
    }

    /// L'échéance (ms) d'une sourdine posée à `maintenant` ; 0 : sans fin.
    pub fn echeance(self, maintenant: i64) -> i64 {
        const HEURE: i64 = 3_600_000;
        match self {
            Duree::Heure => maintenant + HEURE,
            Duree::HuitHeures => maintenant + 8 * HEURE,
            Duree::Jour => maintenant + 24 * HEURE,
            Duree::Semaine => maintenant + 7 * 24 * HEURE,
            Duree::Toujours => 0,
        }
    }
}

/// `id` est-elle en sourdine à `maintenant` (ms) ? Une échéance passée ne compte plus.
pub(crate) fn active(sourdine: &BTreeMap<String, i64>, id: &str, maintenant: i64) -> bool {
    sourdine.get(id).is_some_and(|t| *t == 0 || *t > maintenant)
}

/// L'échéance commune de plusieurs conversations (la fiche) : 0 si l'une est
/// sans fin, sinon la plus lointaine ; None si aucune n'est en sourdine.
pub(crate) fn echeance_de(sourdine: &BTreeMap<String, i64>, ids: &[String], maintenant: i64) -> Option<i64> {
    let l: Vec<i64> = ids.iter().filter(|id| active(sourdine, id, maintenant)).filter_map(|id| sourdine.get(id).copied()).collect();
    if l.is_empty() {
        None
    } else if l.contains(&0) {
        Some(0)
    } else {
        l.into_iter().max()
    }
}

const JOURS_EN: [&str; 7] = ["Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday", "Sunday"];
const MOIS_EN: [&str; 12] = ["Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"];

/// « En sourdine jusqu'à demain 14 h 00 » (heures locales) ; `fin` None :
/// « En sourdine » (jusqu'à réactivation).
pub(crate) fn libelle_a(fin: Option<NaiveDateTime>, maintenant: NaiveDateTime, en: bool) -> String {
    let Some(fin) = fin else {
        return if en { "Muted".into() } else { "En sourdine".into() };
    };
    let (h, m) = (fin.hour(), fin.minute());
    let jours = (fin.date() - maintenant.date()).num_days();
    if en {
        let heure = format!("{}:{m:02} {}", if h % 12 == 0 { 12 } else { h % 12 }, if h < 12 { "AM" } else { "PM" });
        match jours {
            0 => format!("Muted until {heure}"),
            1 => format!("Muted until tomorrow {heure}"),
            2..=6 => format!("Muted until {} {heure}", JOURS_EN[fin.weekday().num_days_from_monday() as usize]),
            _ => format!("Muted until {} {}, {heure}", MOIS_EN[fin.month0() as usize], fin.day()),
        }
    } else {
        let heure = format!("{h} h {m:02}");
        match jours {
            0 => format!("En sourdine jusqu'à {heure}"),
            1 => format!("En sourdine jusqu'à demain {heure}"),
            2..=6 => format!("En sourdine jusqu'à {} {heure}", JOURS[fin.weekday().num_days_from_monday() as usize]),
            _ => {
                let jour = if fin.day() == 1 { "1er".to_string() } else { fin.day().to_string() };
                format!("En sourdine jusqu'au {jour} {} {heure}", MOIS[fin.month0() as usize])
            }
        }
    }
}

/// Le libellé d'une échéance (ms ; 0 : sans fin), à l'heure locale.
pub(crate) fn libelle(echeance: i64) -> String {
    let maintenant = chrono::Local::now().naive_local();
    let fin = (echeance > 0).then(|| local(echeance).map(|d| d.naive_local())).flatten();
    if echeance > 0 && fin.is_none() {
        return libelle_a(None, maintenant, anglais());
    }
    libelle_a(fin, maintenant, anglais())
}

/// La modale des durées : les conversations à mettre en sourdine et le nom montré.
#[derive(Clone)]
pub(crate) struct ModaleSourdine {
    pub ids: Vec<String>,
    pub nom: String,
}

impl Jasette {
    /// La conversation `id` est en sourdine (sa notification système est coupée).
    pub(crate) fn en_sourdine(&self, id: &str) -> bool {
        active(&self.reglages.sourdine, id, maintenant_ms())
    }

    /// Pose (`Some(échéance)`) ou lève (None) la sourdine de `ids`, gardée
    /// et partagée (marque `sourdine`). Les échues s'en vont au passage.
    pub(crate) fn poser_sourdine(&mut self, ids: &[String], echeance: Option<i64>) {
        let maintenant = maintenant_ms();
        let s = &mut self.reglages.sourdine;
        s.retain(|_, t| *t == 0 || *t > maintenant);
        for id in ids {
            match echeance {
                Some(e) => {
                    s.insert(id.clone(), e);
                }
                None => {
                    s.remove(id);
                }
            }
        }
        self.marques_changees();
    }

    /// L'entrée de menu : « Réactiver les notifications » si l'une de `ids`
    /// est en sourdine (les lève toutes), sinon « Mettre en sourdine… » (la modale).
    pub(crate) fn entree_sourdine(&self, ids: &[String]) -> (&'static str, &'static str) {
        if ids.iter().any(|id| self.en_sourdine(id)) {
            ("bell", t("Réactiver les notifications"))
        } else {
            ("bell-off", t("Mettre en sourdine…"))
        }
    }

    /// Le geste de l'entrée de menu (voir `entree_sourdine`).
    pub(crate) fn basculer_sourdine(&mut self, ids: Vec<String>, nom: String) {
        if ids.iter().any(|id| self.en_sourdine(id)) {
            self.poser_sourdine(&ids, None);
        } else if !ids.is_empty() {
            self.modale_sourdine = Some(ModaleSourdine { ids, nom });
        }
    }

    /// Toutes les 15 s : une sourdine échue s'en va (l'icône disparaît ; les
    /// notifications, elles, reviennent dès l'échéance). Gardé ici seulement :
    /// le serveur l'élague de son côté.
    pub(crate) fn sourdine_tic(&mut self) -> bool {
        if self.reglages.sourdine.is_empty() || self.dernier_tic_sourdine.is_some_and(|t| t.elapsed() < Duration::from_secs(15)) {
            return false;
        }
        self.dernier_tic_sourdine = Some(Instant::now());
        let maintenant = maintenant_ms();
        let avant = self.reglages.sourdine.len();
        self.reglages.sourdine.retain(|_, t| *t == 0 || *t > maintenant);
        self.reglages.sourdine.len() != avant
    }

    /// La cloche barrée, à côté de l'heure ou du nom.
    pub(crate) fn icone_sourdine(taille: f32, couleur: Hsla) -> Svg {
        icone("bell-off", taille, couleur)
    }

    /// La modale « Mettre en sourdine » : les cinq durées ; un clic pose la
    /// sourdine et ferme. Échap ou un clic dehors : fermée sans rien changer.
    pub(crate) fn modale_sourdine_vue(&self, cx: &mut Context<Self>) -> Option<AnyElement> {
        let m = self.modale_sourdine.clone()?;
        let mut liste = div().flex().flex_col().gap(px(2.));
        for d in Duree::TOUTES {
            let ids = m.ids.clone();
            liste = liste.child(
                div()
                    .id(SharedString::from(format!("sourdine-{}", d.cle())))
                    .flex()
                    .flex_row()
                    .items_center()
                    .gap(px(12.))
                    .px(px(10.))
                    .py(px(10.))
                    .rounded(rayon(12.))
                    .cursor_pointer()
                    .hover(|s| s.bg(th().survol))
                    .on_click(cx.listener(move |this, _, _, cx| {
                        cx.stop_propagation();
                        this.poser_sourdine(&ids, Some(d.echeance(maintenant_ms())));
                        this.modale_sourdine = None;
                        cx.notify();
                    }))
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
                            .child(icone(if d == Duree::Toujours { "bell-off" } else { "clock" }, 17., th().texte)),
                    )
                    .child(div().flex_1().text_size(px(14.5)).font_weight(FontWeight::SEMIBOLD).text_color(th().texte_fort).child(d.libelle())),
            );
        }
        Some(
            div()
                .id("modale-sourdine")
                .absolute()
                .inset_0()
                .occlude()
                .flex()
                .items_center()
                .justify_center()
                .bg(hsla(0., 0., 0., 0.35))
                .on_click(cx.listener(|this, _, _, cx| {
                    this.modale_sourdine = None;
                    cx.notify();
                }))
                .child(
                    groupe()
                        .id("modale-sourdine-carte")
                        .when(!th().sombre && !th().ega, |d| d.bg(blanc(0.97)))
                        .w(px(380.))
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
                                .child(
                                    div()
                                        .text_size(px(16.))
                                        .font_weight(FontWeight::BOLD)
                                        .text_color(th().texte_fort)
                                        .child(t("Mettre en sourdine")),
                                )
                                .child(div().truncate().text_size(px(12.5)).text_color(th().texte_pale).child(m.nom))
                                .child(
                                    div()
                                        .pt(px(4.))
                                        .text_size(px(12.5))
                                        .text_color(th().texte_pale)
                                        .child(t("Les messages arrivent comme d'habitude, sans notification.")),
                                ),
                        )
                        .child(liste),
                )
                .into_any_element(),
        )
    }
}

#[cfg(test)]
mod essais {
    use super::*;
    use chrono::NaiveDate;

    fn a(j: u32, h: u32, m: u32) -> NaiveDateTime {
        NaiveDate::from_ymd_opt(2026, 11, j).unwrap().and_hms_opt(h, m, 0).unwrap()
    }

    #[test]
    fn durees() {
        let m = 1_000_000;
        assert_eq!(Duree::Heure.echeance(m), m + 3_600_000);
        assert_eq!(Duree::HuitHeures.echeance(m), m + 8 * 3_600_000);
        assert_eq!(Duree::Jour.echeance(m), m + 24 * 3_600_000);
        assert_eq!(Duree::Semaine.echeance(m), m + 7 * 24 * 3_600_000);
        assert_eq!(Duree::Toujours.echeance(m), 0);
    }

    /// Le filtre : en sourdine jusqu'à l'échéance (exclue), toujours avec 0.
    #[test]
    fn active_et_echeance() {
        let s = BTreeMap::from([("m:1".to_string(), 0), ("w:2".to_string(), 5_000), ("i:3".to_string(), 100)]);
        assert!(active(&s, "m:1", i64::MAX));
        assert!(active(&s, "w:2", 4_999));
        assert!(!active(&s, "w:2", 5_000));
        assert!(!active(&s, "i:3", 200));
        assert!(!active(&s, "x:9", 0));
        let ids = |l: &[&str]| l.iter().map(|x| x.to_string()).collect::<Vec<_>>();
        assert_eq!(echeance_de(&s, &ids(&["w:2", "i:3"]), 200), Some(5_000));
        assert_eq!(echeance_de(&s, &ids(&["w:2", "m:1"]), 200), Some(0));
        assert_eq!(echeance_de(&s, &ids(&["i:3", "x:9"]), 200), None);
    }

    #[test]
    fn libelles() {
        let m = a(27, 9, 30); // un vendredi
        assert_eq!(libelle_a(None, m, false), "En sourdine");
        assert_eq!(libelle_a(Some(a(27, 10, 30)), m, false), "En sourdine jusqu'à 10 h 30");
        assert_eq!(libelle_a(Some(a(28, 14, 0)), m, false), "En sourdine jusqu'à demain 14 h 00");
        assert_eq!(libelle_a(Some(a(30, 8, 5)), m, false), "En sourdine jusqu'à lundi 8 h 05");
        let loin = NaiveDate::from_ymd_opt(2026, 12, 4).unwrap().and_hms_opt(9, 30, 0).unwrap();
        assert_eq!(libelle_a(Some(loin), m, false), "En sourdine jusqu'au 4 déc. 9 h 30");
        assert_eq!(libelle_a(Some(a(28, 14, 0)), m, true), "Muted until tomorrow 2:00 PM");
        assert_eq!(libelle_a(None, m, true), "Muted");
        assert_eq!(libelle_a(Some(loin), m, true), "Muted until Dec 4, 9:30 AM");
    }
}
