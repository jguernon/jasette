//! Le regroupement automatique dans la boîte de réception, côté interface du
//! bureau : quelles conversations vont dans quel groupe (la détection est
//! dans src/groupes_auto.rs), les rangées Codes, Promotions et Avis,
//! la vue d'un groupe ouvert, le menu du clic droit (« Ce n'est pas une
//! promo », « Mettre dans Codes »…) et la section « Regrouper
//! automatiquement » des réglages.

use super::*;
use crate::groupes_auto::{self as ga, Groupe};

/// Le menu du clic droit sur une conversation : son id, et où il s'ouvre.
#[derive(Clone)]
pub(crate) struct MenuGroupe {
    pub id: String,
    pub pos: gpui::Point<Pixels>,
}

/// Une rangée de la boîte : une conversation, ou un groupe replié.
pub(crate) enum Rangee<'a> {
    Conv(&'a Conv),
    Groupe(Groupe),
}

/// Ce qu'un groupe montre dans sa rangée.
pub(crate) struct Resume {
    pub membres: usize,
    pub non_lus: usize,
    /// L'heure du message le plus récent du groupe.
    pub time: i64,
    /// Le plus récent : son nom et son aperçu.
    pub dernier_nom: String,
    pub dernier_apercu: String,
    /// Codes : le dernier code reçu, son expéditeur et son heure.
    pub code: Option<(String, String, i64)>,
    /// Promotions : les noms des expéditeurs, du plus récent au plus vieux.
    pub noms: Vec<String>,
}

/// La détection gardée par conversation, et ce qu'elle a vu (heure, nombre
/// de messages, longueur de l'aperçu) : refaite seulement si ça change.
pub(crate) type CacheDetection = RefCell<HashMap<String, ((i64, usize, usize), Option<Groupe>)>>;

fn titre(g: Groupe) -> &'static str {
    match g {
        Groupe::Codes => t("Codes"),
        Groupe::Promos => t("Promotions"),
        Groupe::Avis => t("Avis"),
    }
}

/// « Codes · 3 nouveaux », « Promotions · 12 », « Avis ».
pub(crate) fn titre_rangee(g: Groupe, r: &Resume) -> String {
    match g {
        Groupe::Promos => format!("{} · {}", titre(g), r.membres),
        _ if r.non_lus == 0 => titre(g).to_string(),
        _ => format!("{} · {} {}", titre(g), r.non_lus, if r.non_lus == 1 { t("nouveau") } else { t("nouveaux") }),
    }
}

impl Jasette {
    pub(crate) fn groupe_actif(&self, g: Groupe) -> bool {
        self.reglages.groupes_actifs.iter().any(|k| k == g.cle())
    }

    pub(crate) fn regroupement_actif(&self) -> bool {
        Groupe::TOUS.iter().any(|g| self.groupe_actif(*g))
    }

    /// Un contact : nom ou photo choisis à la main, fiche de la personne, ou
    /// (Google) un vrai numéro que le téléphone nomme d'après ses contacts.
    fn est_contact(&self, c: &Conv) -> bool {
        if self.contact_perso(c).is_some() || personne::principal_de(&self.reglages.personnes, &c.id).is_some() {
            return true;
        }
        if service_de(&c.id) == Service::GoogleMessages
            && let Some(k) = cle_numero(&c.number)
        {
            return self.photos_numeros.contains_key(&k) || c.name.chars().any(char::is_alphabetic);
        }
        false
    }

    /// La détection seule (sans les exclusions), gardée tant que la
    /// conversation ne bouge pas.
    fn detection(&self, c: &Conv) -> Option<Groupe> {
        let msgs = self.messages.get(&c.id);
        let cle = (c.time, msgs.map_or(0, Vec::len), c.preview.len());
        if let Some((k, g)) = self.cache_groupes.borrow().get(&c.id)
            && *k == cle
        {
            return *g;
        }
        let mut j_ai_ecrit = c.from_me;
        let mut textes: Vec<&str> = Vec::new();
        let mut dernier = 0;
        for m in msgs.into_iter().flatten() {
            dernier = dernier.max(m.time);
            if m.from_me {
                j_ai_ecrit = true;
            } else if !m.texte_fr().trim().is_empty() {
                textes.push(m.texte_fr());
            }
        }
        // L'aperçu : le dernier message, s'il est plus récent que ceux qu'on a.
        if !c.from_me && !c.preview.trim().is_empty() && (textes.is_empty() || c.time > dernier) {
            textes.push(&c.preview);
        }
        let debut = textes.len().saturating_sub(ga::MESSAGES_MAX);
        let sms = service_de(&c.id) == Service::GoogleMessages;
        let e = ga::Entree {
            sms,
            expediteur: ga::sorte_expediteur(&c.name, if sms { &c.number } else { "" }),
            exclue: false,
            j_ai_ecrit,
            sortes: textes[debut..].iter().map(|t| ga::sorte_message(t)).collect(),
        };
        let g = ga::classer(&e);
        self.cache_groupes.borrow_mut().insert(c.id.clone(), (cle, g));
        g
    }

    /// Le groupe (actif) d'une conversation de la boîte, s'il y en a un. Le
    /// Head, les favoris, les épinglées, les groupes, les grands espaces et
    /// les privées n'y vont jamais ; la correction à la main gagne sur la
    /// détection (qui laisse aussi de côté les contacts).
    pub(crate) fn groupe_de(&self, c: &Conv) -> Option<Groupe> {
        if !self.regroupement_actif() {
            return None;
        }
        let r = &self.reglages;
        if c.group || c.large || self.privee(&c.id) || r.head.contains(&c.id) || r.etoiles.contains(&c.id) || r.epingles.contains(&c.id) {
            return None;
        }
        let correction = r.groupes_auto.get(&c.id).map(String::as_str);
        let detecte = if correction.and_then(ga::lire_correction).is_some() || self.est_contact(c) { None } else { self.detection(c) };
        ga::groupe_final(correction, detecte).filter(|g| self.groupe_actif(*g))
    }

    /// Une promotion (groupe actif) : ni notification, ni non-lu compté.
    pub(crate) fn en_promotions(&self, c: &Conv) -> bool {
        self.groupe_de(c) == Some(Groupe::Promos)
    }

    /// La vue Tout, sans recherche ni thème rétro : là seulement on regroupe.
    pub(crate) fn regroupe_ici(&self) -> bool {
        self.vue == Vue::Tout && self.filtre.is_empty() && !retro::retro() && self.regroupement_actif()
    }

    /// Le résumé d'un groupe (`membres` : ses conversations).
    pub(crate) fn resume_groupe(&self, g: Groupe, membres: &[&Conv]) -> Resume {
        let mut tries: Vec<&Conv> = membres.to_vec();
        tries.sort_by(|a, b| b.time.cmp(&a.time));
        let dernier = tries.first();
        let code = if g == Groupe::Codes {
            tries.iter().find_map(|c| {
                let texte = if c.from_me {
                    self.messages.get(&c.id)?.iter().rev().find(|m| !m.from_me)?.texte_fr().to_string()
                } else {
                    c.preview.clone()
                };
                Some((codes::code_verification(&texte)?, self.nom_affiche(c), c.time))
            })
        } else {
            None
        };
        Resume {
            membres: membres.len(),
            non_lus: membres.iter().filter(|c| c.unread).count(),
            time: dernier.map_or(0, |c| c.time),
            dernier_nom: dernier.map(|c| self.nom_affiche(c)).unwrap_or_default(),
            dernier_apercu: dernier.map(|c| une_ligne(&ponts_langue::affiche(&c.preview, false))).unwrap_or_default(),
            code,
            noms: tries.iter().map(|c| self.nom_affiche(c)).collect(),
        }
    }

    /// Les rangées de la boîte : les conversations des groupes actifs quittent
    /// `visibles` (déjà triées : épinglées d'abord, puis par heure) et chaque
    /// groupe non vide prend une place, à l'heure de son plus récent ; Codes
    /// monte sous les épinglées 15 minutes après un code. Rend aussi les
    /// membres de chaque groupe.
    pub(crate) fn rangees<'a>(&self, visibles: Vec<&'a Conv>, nb_epingles: usize) -> (Vec<Rangee<'a>>, Vec<(Groupe, Vec<&'a Conv>)>) {
        let mut membres: Vec<(Groupe, Vec<&'a Conv>)> = Groupe::TOUS.iter().map(|g| (*g, Vec::new())).collect();
        let mut restes: Vec<&'a Conv> = Vec::with_capacity(visibles.len());
        for (i, c) in visibles.into_iter().enumerate() {
            match (i >= nb_epingles).then(|| self.groupe_de(c)).flatten() {
                Some(g) => membres.iter_mut().find(|(x, _)| *x == g).map(|(_, l)| l.push(c)).unwrap_or(()),
                None => restes.push(c),
            }
        }
        membres.retain(|(_, l)| !l.is_empty());
        let mut a_placer: Vec<(Groupe, i64)> = membres.iter().map(|(g, l)| (*g, l.iter().map(|c| c.time).max().unwrap_or(0))).collect();
        let maintenant = maintenant_ms();
        self.codes_en_haut.set(false);
        let mut r: Vec<Rangee<'a>> = Vec::with_capacity(restes.len() + a_placer.len());
        for (i, c) in restes.into_iter().enumerate() {
            if i == nb_epingles {
                // Codes en haut : un code est arrivé il y a moins de 15 minutes.
                if let Some(k) = a_placer.iter().position(|(g, _)| *g == Groupe::Codes) {
                    let l = &membres.iter().find(|(g, _)| *g == Groupe::Codes).expect("membres").1;
                    if self.resume_groupe(Groupe::Codes, l).code.is_some_and(|(_, _, t)| ga::codes_en_haut(t, maintenant)) {
                        a_placer.remove(k);
                        r.push(Rangee::Groupe(Groupe::Codes));
                        self.codes_en_haut.set(true);
                    }
                }
            }
            if i >= nb_epingles {
                a_placer.sort_by(|a, b| b.1.cmp(&a.1));
                while a_placer.first().is_some_and(|(_, t)| *t >= c.time) {
                    r.push(Rangee::Groupe(a_placer.remove(0).0));
                }
            }
            r.push(Rangee::Conv(c));
        }
        // Rien que des épinglées (ou rien du tout) : les groupes après elles.
        a_placer.sort_by(|a, b| b.1.cmp(&a.1));
        r.extend(a_placer.into_iter().map(|(g, _)| Rangee::Groupe(g)));
        (r, membres)
    }

    /// Ouvre ou ferme un groupe (la liste de ses conversations).
    pub(crate) fn ouvrir_groupe(&mut self, g: Option<Groupe>) {
        self.groupe_ouvert = g;
        self.defil_liste.set_offset(gpui::Point::default());
    }

    /// « Tout marquer comme lu » d'un groupe ouvert (action de l'utilisateur).
    pub(crate) fn groupe_tout_lu(&mut self, g: Groupe) {
        let ids: Vec<String> = self.boite().into_iter().filter(|c| c.unread && self.groupe_de(c) == Some(g)).map(|c| c.id.clone()).collect();
        for id in ids {
            self.marquer_lu(&id);
        }
    }

    /// Corrige à la main le groupe d'une conversation ("aucun" : jamais
    /// regroupée), partagé par le serveur Jasette (marque `groupes_auto`).
    pub(crate) fn corriger_groupe(&mut self, id: &str, valeur: &str) {
        if self.reglages.groupes_auto.get(id).map(String::as_str) == Some(valeur) {
            return;
        }
        self.reglages.groupes_auto.insert(id.to_string(), valeur.to_string());
        self.marques_changees();
    }

    /// Le rond de couleur d'un groupe, avec son icône blanche, à la place de l'avatar.
    pub(crate) fn avatar_groupe(g: Groupe, taille: f32) -> Div {
        div()
            .size(px(taille))
            .flex_none()
            .rounded(rayon(999.))
            .bg(if th().ega { ega_fond(c(g.fond())) } else { c(g.fond()) })
            .flex()
            .items_center()
            .justify_center()
            .child(icone(g.icone(), (taille * 0.5).round(), blanc(1.)))
    }

    /// La rangée d'un groupe dans la boîte.
    pub(crate) fn rangee_groupe(&self, g: Groupe, membres: &[&Conv], derniere: bool, cx: &mut Context<Self>) -> AnyElement {
        let r = self.resume_groupe(g, membres);
        let non_lu = r.non_lus > 0 && g != Groupe::Promos;
        let poids = if non_lu { FontWeight::BOLD } else { FontWeight::SEMIBOLD };
        // « Google : 482913 » (en anglais, sans espace avant les deux-points).
        let deux_points = if anglais() { ": " } else { " : " };
        let apercu = match g {
            Groupe::Codes => match &r.code {
                Some((code, nom, _)) => format!("{nom}{deux_points}{code}"),
                None => format!("{}{deux_points}{}", r.dernier_nom, r.dernier_apercu),
            },
            Groupe::Promos => r.noms.join(", "),
            Groupe::Avis => format!("{}{deux_points}{}", r.dernier_nom, r.dernier_apercu),
        };
        let copie = (g == Groupe::Codes).then(|| r.code.clone()).flatten().map(|(code, _, _)| {
            let cle = format!("groupe-codes-{code}");
            let fait = self.code_copie.as_deref() == Some(cle.as_str());
            let teinte = if fait { th().succes } else { th().accent };
            div()
                .id("groupe-copier")
                .flex_none()
                .flex()
                .flex_row()
                .items_center()
                .gap(px(5.))
                .px(px(10.))
                .py(px(3.))
                .rounded(rayon(999.))
                .border_1()
                .border_color(th().trait_fin)
                .bg(ton(blanc(0.8), th().surface))
                .text_size(px(12.))
                .font_weight(FontWeight::SEMIBOLD)
                .text_color(teinte)
                .cursor_pointer()
                .hover(|s| s.bg(th().survol))
                .child(icone(if fait { "check" } else { "copy" }, 13., teinte))
                .child(if fait { t("Copié") } else { t("Copier") })
                .on_click(cx.listener(move |this, _, _, cx| {
                    cx.stop_propagation();
                    cx.write_to_clipboard(gpui::ClipboardItem::new_string(code.clone()));
                    this.code_copie = Some(cle.clone());
                    cx.notify();
                    let cle = cle.clone();
                    cx.spawn(async move |this, cx| {
                        cx.background_executor().timer(Duration::from_millis(1600)).await;
                        let _ = this.update(cx, |this, cx| {
                            if this.code_copie.as_deref() == Some(cle.as_str()) {
                                this.code_copie = None;
                                cx.notify();
                            }
                        });
                    })
                    .detach();
                }))
        });
        div()
            .id(SharedString::from(format!("groupe-{}", g.cle())))
            .flex()
            .flex_row()
            .items_center()
            .gap(px(12.))
            .pl(px(10.))
            .rounded(rayon(mesure(16., 10.)))
            .cursor_pointer()
            .hover(|s| s.bg(th().survol))
            .on_click(cx.listener(move |this, _, _, cx| {
                this.ouvrir_groupe(Some(g));
                cx.notify();
            }))
            .child(div().flex_none().py(px(10.)).child(Self::avatar_groupe(g, 50.)))
            .child(
                div()
                    .flex()
                    .flex_col()
                    .flex_1()
                    .min_w(px(0.))
                    .gap(px(2.))
                    .py(px(10.))
                    .pr(px(10.))
                    .when(!derniere, |d| d.border_b_1().border_color(th().trait_fin))
                    .child(
                        div()
                            .flex()
                            .flex_row()
                            .justify_between()
                            .gap(px(8.))
                            .child(
                                div()
                                    .truncate()
                                    .text_size(px(15. + self.plus_liste()))
                                    .text_color(th().texte_fort)
                                    .font_weight(poids)
                                    .child(titre_rangee(g, &r)),
                            )
                            .child(
                                div()
                                    .flex_none()
                                    .text_size(px(12. + self.plus_liste()))
                                    .text_color(if non_lu { th().accent } else { th().texte_pale })
                                    .child(heure(r.time, true)),
                            ),
                    )
                    .child(
                        div()
                            .flex()
                            .flex_row()
                            .items_center()
                            .gap(px(6.))
                            .child(div().flex_1().min_w(px(0.)).truncate().text_size(px(13. + self.plus_liste())).child(texte_emoji(
                                apercu,
                                if non_lu { th().texte_fort } else { th().apercu },
                                None,
                            )))
                            .children(copie)
                            .when(non_lu, |d| d.child(div().flex_none().size(px(9.)).rounded(rayon(999.)).bg(degrade_accent()))),
                    ),
            )
            .into_any_element()
    }

    /// L'en-tête d'un groupe ouvert : retour, son icône, son nom, et « Tout
    /// marquer comme lu ».
    pub(crate) fn entete_groupe(&self, g: Groupe, non_lus: usize, cx: &mut Context<Self>) -> AnyElement {
        div()
            .flex()
            .flex_row()
            .items_center()
            .gap(px(10.))
            .px(px(6.))
            .pb(px(8.))
            .mb(px(4.))
            .border_b_1()
            .border_color(th().trait_fin)
            .child(Self::bouton_icone("groupe-retour", "arrow-left", false).on_click(cx.listener(|this, _, _, cx| {
                this.ouvrir_groupe(None);
                cx.notify();
            })))
            .child(Self::avatar_groupe(g, 34.))
            .child(div().flex_1().min_w(px(60.)).truncate().font_weight(FontWeight::BOLD).text_color(th().texte_fort).child(titre(g)))
            .when(non_lus > 0, |d| {
                d.child(
                    div()
                        .id("groupe-tout-lu")
                        .flex_none()
                        .px(px(8.))
                        .py(px(4.))
                        .rounded(rayon(8.))
                        .text_size(px(12.))
                        .font_weight(FontWeight::SEMIBOLD)
                        .text_color(th().accent)
                        .cursor_pointer()
                        .hover(|s| s.bg(th().survol))
                        .child(t("Tout marquer comme lu"))
                        .on_click(cx.listener(move |this, _, _, cx| {
                            this.groupe_tout_lu(g);
                            cx.notify();
                        })),
                )
            })
            .into_any_element()
    }

    /// Le menu du clic droit sur une conversation : « Ce n'est pas … » et
    /// « Mettre dans … » (seulement les groupes actifs).
    pub(crate) fn menu_groupe_vue(&self, cx: &mut Context<Self>) -> Option<gpui::Deferred> {
        let m = self.menu_groupe.clone()?;
        let conv = self.conv(&m.id)?;
        let actuel = self.groupe_de(conv);
        let mut choix: Vec<(&'static str, &'static str)> = Vec::new();
        match actuel {
            Some(Groupe::Codes) => choix.push(("aucun", t("Ce n'est pas un code"))),
            Some(Groupe::Promos) => choix.push(("aucun", t("Ce n'est pas une promo"))),
            Some(Groupe::Avis) => choix.push(("aucun", t("Ce n'est pas un avis"))),
            None => {}
        }
        for g in Groupe::TOUS {
            if Some(g) != actuel && self.groupe_actif(g) {
                choix.push((
                    g.cle(),
                    match g {
                        Groupe::Codes => t("Mettre dans Codes"),
                        Groupe::Promos => t("Mettre dans Promotions"),
                        Groupe::Avis => t("Mettre dans Avis"),
                    },
                ));
            }
        }
        if choix.is_empty() {
            return None;
        }
        let mut menu = verre(0.96, 14.)
            .id("menu-groupe")
            .w(px(260.))
            .p(px(6.))
            .shadow(ombre_verre())
            .when(th().sombre, |d| d.bg(th().menu).border_1().border_color(th().trait_fin))
            .on_mouse_down_out(cx.listener(|this, _, _, cx| {
                this.menu_groupe = None;
                cx.notify();
            }));
        for (valeur, texte) in choix {
            let id = m.id.clone();
            let ic = ga::lire_correction(valeur).flatten();
            menu = menu.child(
                div()
                    .id(SharedString::from(format!("menu-groupe-{valeur}")))
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
                    .on_mouse_down(MouseButton::Left, |_, _, cx| cx.stop_propagation())
                    .on_click(cx.listener(move |this, _, _, cx| {
                        cx.stop_propagation();
                        this.corriger_groupe(&id, valeur);
                        this.menu_groupe = None;
                        cx.notify();
                    }))
                    .child(match ic {
                        Some(g) => Self::avatar_groupe(g, 22.),
                        None => div().size(px(22.)).flex_none().flex().items_center().justify_center().child(icone("undo-2", 16., th().texte_pale)),
                    })
                    .child(texte),
            );
        }
        Some(gpui::deferred(gpui::anchored().position(m.pos).snap_to_window_with_margin(px(8.)).child(menu)).with_priority(1))
    }

    /// Réglages : « Regrouper automatiquement », une case par groupe, sur cet appareil.
    pub(crate) fn regrouper_reglages(&self, cx: &mut Context<Self>) -> Div {
        let mut lignes = groupe();
        for (i, g) in Groupe::TOUS.into_iter().enumerate() {
            if i > 0 {
                lignes = lignes.child(separateur());
            }
            let actif = self.groupe_actif(g);
            let texte = match g {
                Groupe::Codes => t("Codes de vérification"),
                Groupe::Promos => t("Promotions"),
                Groupe::Avis => t("Avis (livraisons, rendez-vous, banque, factures)"),
            };
            lignes = lignes.child(
                div()
                    .id(SharedString::from(format!("regrouper-{}", g.cle())))
                    .flex()
                    .flex_row()
                    .items_center()
                    .gap(px(12.))
                    .px(px(16.))
                    .py(px(12.))
                    .cursor_pointer()
                    .on_click(cx.listener(move |this, _, _, cx| {
                        let l = &mut this.reglages.groupes_actifs;
                        l.retain(|k| k != g.cle());
                        if !actif {
                            l.push(g.cle().to_string());
                        }
                        if this.groupe_ouvert == Some(g) && actif {
                            this.groupe_ouvert = None;
                        }
                        this.reglages.garder();
                        cx.notify();
                    }))
                    .child(Self::avatar_groupe(g, 26.))
                    .child(div().flex_1().text_color(if actif { th().texte_fort } else { th().texte }).child(texte))
                    .child(
                        div()
                            .size(px(20.))
                            .flex_none()
                            .rounded(rayon(5.))
                            .border_2()
                            .border_color(if actif { th().accent } else { th().texte_pale })
                            .when(actif, |d| d.bg(th().accent))
                            .flex()
                            .items_center()
                            .justify_center()
                            .when(actif, |d| d.child(icone("check", 14., blanc(1.)))),
                    ),
            );
        }
        div()
            .mt(px(22.))
            .flex()
            .flex_col()
            .gap(px(8.))
            .child(div().px(px(4.)).font_weight(FontWeight::SEMIBOLD).text_color(th().texte_fort).child(t("Regrouper automatiquement")))
            .child(lignes)
            .child(div().px(px(4.)).text_size(px(12.)).text_color(th().texte_pale).child(t(
                "Jasette range les expéditeurs automatiques dans une seule rangée, sur cet appareil, sans rien envoyer ailleurs. Les contacts, les favoris, les épinglées, les groupes et les conversations où vous avez écrit ne sont jamais regroupés.",
            )))
    }
}
