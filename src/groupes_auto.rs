//! Le regroupement automatique de la boîte de réception (Réglages >
//! « Regrouper automatiquement », désactivé par défaut) : les expéditeurs de
//! codes de vérification (🔐 Codes), de publicités (🏷️ Promotions) et d'avis
//! (📦 Avis : livraisons, rendez-vous, banque, factures, alertes) deviennent
//! chacun une seule rangée repliable de la boîte.
//!
//! Tout se décide ici, localement, sans IA ni service externe, par
//! EXPÉDITEUR (conversation) et pas message par message :
//!
//! 1. chaque message reçu reçoit une sorte (`sorte_message`) : un code de
//!    vérification (src/codes.rs), une promo, un avis, ou rien ;
//! 2. l'expéditeur est-il un numéro court, un nom alphanumérique
//!    (« Desjardins ») ou un expéditeur ordinaire (`sorte_expediteur`) ;
//! 3. `classer` décide du groupe d'après les derniers messages reçus : il en
//!    faut au moins deux qui concordent (trois hors SMS pour un expéditeur
//!    ordinaire), et qu'ils soient au moins la moitié ; un seul code suffit
//!    d'un numéro court ou d'un nom alphanumérique.
//!
//! Jamais classés (l'appelant le dit par `exclue`) : un contact (contacts du
//! téléphone, fiche de la personne, nom choisi à la main), un favori, le Head,
//! une épinglée, un groupe, un grand espace, une conversation privée. Ni une
//! conversation où j'ai déjà écrit (`j_ai_ecrit`).
//!
//! La correction à la main (marque `groupes_auto`, PROTOCOLE.md §5.5) gagne
//! toujours sur la détection (`groupe_final`).
//!
//! La même logique, pas à pas, est dans android/…/GroupesAuto.kt ; les deux
//! côtés lisent les mêmes essais : assets/essais/groupes-auto.txt.

use crate::codes::code_verification;

/// Un groupe de la boîte.
#[derive(Clone, Copy, Debug, PartialEq, Eq, Hash)]
pub enum Groupe {
    Codes,
    Promos,
    Avis,
}

impl Groupe {
    pub const TOUS: [Groupe; 3] = [Groupe::Codes, Groupe::Promos, Groupe::Avis];

    /// Sa valeur dans la marque `groupes_auto`.
    pub fn cle(self) -> &'static str {
        match self {
            Groupe::Codes => "codes",
            Groupe::Promos => "promos",
            Groupe::Avis => "avis",
        }
    }

    /// Son icône Lucide, blanche sur sa couleur, comme les tuiles des Réglages.
    pub fn icone(self) -> &'static str {
        match self {
            Groupe::Codes => "key-round",
            Groupe::Promos => "tag",
            Groupe::Avis => "package",
        }
    }

    /// La couleur de son rond (RVB), prise dans la palette des Réglages.
    pub fn fond(self) -> u32 {
        match self {
            Groupe::Codes => 0x8b5cf6,
            Groupe::Promos => 0xec4899,
            Groupe::Avis => 0xf59e0b,
        }
    }
}

/// Une valeur de la marque `groupes_auto` : Some(Some(g)) « mettre dans g »,
/// Some(None) « aucun » (jamais regroupée), None valeur inconnue.
pub fn lire_correction(v: &str) -> Option<Option<Groupe>> {
    match v {
        "codes" => Some(Some(Groupe::Codes)),
        "promos" => Some(Some(Groupe::Promos)),
        "avis" => Some(Some(Groupe::Avis)),
        "aucun" => Some(None),
        _ => None,
    }
}

/// Le groupe d'une conversation : la correction à la main d'abord (une
/// valeur inconnue ne compte pas), sinon la détection.
pub fn groupe_final(correction: Option<&str>, detecte: Option<Groupe>) -> Option<Groupe> {
    match correction.and_then(lire_correction) {
        Some(c) => c,
        None => detecte,
    }
}

/// La sorte d'un message reçu.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Sorte {
    Code,
    Promo,
    Avis,
    Autre,
}

/// La sorte d'un expéditeur.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Expediteur {
    /// Un numéro court (3 à 6 chiffres) : 72345, 2222…
    Court,
    /// Un nom au lieu d'un numéro (« Google », « Desjardins », « AMAZON »).
    Alphanumerique,
    /// Un numéro ordinaire, ou une personne d'un autre service.
    Ordinaire,
}

/// Les mots forts d'une promo (2 points chacun).
const PROMO_FORTS: [&str; 16] = [
    "rabais", "discount", "code promo", "promo code", "coupon", "en solde", "soldes", "économisez", "economisez",
    "% off", "% de rabais", "black friday", "cyber monday", "liquidation", "clearance", "save up to",
];

/// Les mots faibles d'une promo (1 point chacun).
const PROMO_FAIBLES: [&str; 20] = [
    "promo", "promotion", "promotions", "offre", "offres", "offer", "offers", "deal", "deals", "sale", "gratuit",
    "gratuite", "free", "exclusif", "exclusive", "limited time", "durée limitée", "aujourd'hui seulement", "today only",
    "magasinez",
];

/// Se désabonner : 1 point chacun (un rappel de rendez-vous en parle aussi).
const DESABONNER: [&str; 8] =
    ["désabonner", "désabonnement", "désinscrire", "unsubscribe", "opt out", "opt-out", "se retirer", "unsub"];

/// Les mots en majuscules d'un texto commercial (« Répondez STOP »).
const STOP: [&str; 3] = ["STOP", "ARRET", "ARRÊT"];

/// Les liens raccourcis.
const LIENS_COURTS: [&str; 16] = [
    "bit.ly/", "tinyurl.com/", "t.co/", "goo.gl/", "ow.ly/", "amzn.to/", "rb.gy/", "cutt.ly/", "is.gd/", "buff.ly/",
    "shorturl.at/", "tiny.cc/", "lnkd.in/", "qrco.de/", "bit.do/", "s.id/",
];

/// Les mots d'un avis (1 point chacun).
const AVIS: [&str; 44] = [
    "colis", "package", "parcel", "livraison", "livré", "livrée", "delivered", "delivery", "expédié", "expédiée",
    "shipped", "suivi", "tracking", "rendez-vous", "rdv", "appointment", "rappel", "reminder", "facture", "invoice",
    "bill", "solde", "balance", "paiement", "payment", "dépôt", "deposit", "transaction", "retrait", "withdrawal",
    "prélèvement", "virement", "alerte", "alert", "commande", "order", "réservation", "reservation", "booking",
    "ramassage", "pickup", "échéance", "relevé", "statement",
];

/// Un mot (ou une expression) entier dans `t` : ni lettre ni chiffre juste
/// avant ni juste après. Une expression qui commence ou finit par un signe
/// (« % off », « bit.ly/ ») n'a pas besoin de limite de ce côté.
fn mot(t: &[char], m: &str) -> bool {
    let m: Vec<char> = m.chars().collect();
    if m.is_empty() || m.len() > t.len() {
        return false;
    }
    let limite_avant = m[0].is_alphanumeric();
    let limite_apres = m[m.len() - 1].is_alphanumeric();
    (0..=t.len() - m.len()).any(|i| {
        t[i..i + m.len()] == m[..]
            && (!limite_avant || i == 0 || !t[i - 1].is_alphanumeric())
            && (!limite_apres || i + m.len() == t.len() || !t[i + m.len()].is_alphanumeric())
    })
}

/// Un pourcentage : un chiffre, une espace au plus, puis « % ».
fn pourcentage(t: &[char]) -> bool {
    (0..t.len()).any(|i| {
        t[i] == '%'
            && i >= 1
            && (t[i - 1].is_ascii_digit() || (i >= 2 && t[i - 1].is_whitespace() && t[i - 2].is_ascii_digit()))
    })
}

/// La sorte d'un message reçu. Un code de vérification d'abord ; sinon les
/// points de promo (forts 2, faibles 1, pourcentage 1, lien raccourci 1,
/// « STOP » en majuscules 1, se désabonner 1) contre ceux d'avis (1 par mot) :
/// promo s'il y en a au moins 2 et plus que d'avis ; avis s'il y a au moins
/// un mot d'avis ; sinon rien.
pub fn sorte_message(texte: &str) -> Sorte {
    if code_verification(texte).is_some() {
        return Sorte::Code;
    }
    let original: Vec<char> = texte.chars().map(|c| if c == '’' { '\'' } else { c }).collect();
    let t: Vec<char> = original.iter().map(|c| c.to_lowercase().next().unwrap_or(*c)).collect();
    let mut promo = 0;
    promo += 2 * PROMO_FORTS.iter().filter(|m| mot(&t, m)).count();
    promo += PROMO_FAIBLES.iter().filter(|m| mot(&t, m)).count();
    promo += DESABONNER.iter().filter(|m| mot(&t, m)).count().min(1);
    promo += usize::from(STOP.iter().any(|m| mot(&original, m)));
    promo += usize::from(pourcentage(&t));
    promo += usize::from(LIENS_COURTS.iter().any(|m| mot(&t, m)));
    let avis = AVIS.iter().filter(|m| mot(&t, m)).count();
    if promo >= 2 && promo > avis {
        Sorte::Promo
    } else if avis >= 1 {
        Sorte::Avis
    } else {
        Sorte::Autre
    }
}

/// La sorte d'un expéditeur, d'après son numéro (Google) ou, sans numéro, son
/// nom. Numéro court : 3 à 6 chiffres (espaces, tirets et « + » ignorés), sans
/// lettre. Alphanumérique : un numéro qui contient une lettre. Sans numéro,
/// le nom tient lieu de numéro seulement s'il n'a que des chiffres.
pub fn sorte_expediteur(nom: &str, numero: &str) -> Expediteur {
    let source = if numero.trim().is_empty() { nom } else { numero };
    let utiles: Vec<char> = source.chars().filter(|c| !c.is_whitespace() && !matches!(c, '-' | '+' | '(' | ')' | '.')).collect();
    if utiles.is_empty() {
        return Expediteur::Ordinaire;
    }
    if utiles.iter().all(char::is_ascii_digit) {
        return if (3..=6).contains(&utiles.len()) { Expediteur::Court } else { Expediteur::Ordinaire };
    }
    if !numero.trim().is_empty() && utiles.iter().any(|c| c.is_alphabetic()) {
        return Expediteur::Alphanumerique;
    }
    Expediteur::Ordinaire
}

/// Les messages reçus qui comptent : les 10 derniers.
pub const MESSAGES_MAX: usize = 10;

/// Ce que `classer` sait d'une conversation.
#[derive(Clone, Debug)]
pub struct Entree {
    /// Un SMS / RCS (Google Messages).
    pub sms: bool,
    pub expediteur: Expediteur,
    /// Un contact, un favori, le Head, une épinglée, un groupe, un grand
    /// espace, une privée : jamais classée.
    pub exclue: bool,
    /// J'y ai déjà écrit : jamais classée.
    pub j_ai_ecrit: bool,
    /// Les sortes des messages reçus, du plus vieux au plus récent.
    pub sortes: Vec<Sorte>,
}

/// Le groupe détecté d'une conversation (None : elle reste dans la boîte).
pub fn classer(e: &Entree) -> Option<Groupe> {
    if e.exclue || e.j_ai_ecrit || e.sortes.is_empty() {
        return None;
    }
    let s = &e.sortes[e.sortes.len().saturating_sub(MESSAGES_MAX)..];
    let n = s.len();
    let compte = |x: Sorte| s.iter().filter(|y| **y == x).count();
    let commercial = e.expediteur != Expediteur::Ordinaire;
    // Un code évident d'un numéro court ou d'un nom : tout de suite.
    if commercial && s[n - 1] == Sorte::Code {
        return Some(Groupe::Codes);
    }
    let codes = compte(Sorte::Code);
    if codes >= 2 && codes * 2 >= n {
        return Some(Groupe::Codes);
    }
    let seuil = if commercial || e.sms { 2 } else { 3 };
    let (avis, promos) = (compte(Sorte::Avis), compte(Sorte::Promo));
    // À égalité, Avis (le plus utile des deux).
    let (g, c) = if promos > avis { (Groupe::Promos, promos) } else { (Groupe::Avis, avis) };
    (c >= seuil && c * 2 >= n).then_some(g)
}

/// La rangée Codes monte en haut 15 minutes après l'arrivée d'un code.
pub const CODES_EN_HAUT_MS: i64 = 15 * 60 * 1000;

/// La rangée Codes est-elle en haut ? (dernier code reçu à `code_ms`)
pub fn codes_en_haut(code_ms: i64, maintenant_ms: i64) -> bool {
    code_ms > 0 && maintenant_ms >= code_ms && maintenant_ms - code_ms < CODES_EN_HAUT_MS
}

#[cfg(test)]
mod essais {
    use super::*;

    /// Les essais partagés avec Android (GroupesAutoTest.kt).
    const ESSAIS: &str = include_str!("../assets/essais/groupes-auto.txt");

    fn champs(l: &str) -> Vec<String> {
        l.split('\t').map(|c| c.replace("\\n", "\n")).collect()
    }

    fn sorte(c: char) -> Sorte {
        match c {
            'c' => Sorte::Code,
            'p' => Sorte::Promo,
            'a' => Sorte::Avis,
            _ => Sorte::Autre,
        }
    }

    #[test]
    fn essais_partages() {
        let mut n = 0;
        for l in ESSAIS.lines().filter(|l| !l.trim().is_empty() && !l.starts_with('#')) {
            let c = champs(l);
            match c[0].as_str() {
                "M" => {
                    let attendu = match c[1].as_str() {
                        "code" => Sorte::Code,
                        "promo" => Sorte::Promo,
                        "avis" => Sorte::Avis,
                        _ => Sorte::Autre,
                    };
                    assert_eq!(sorte_message(&c[2]), attendu, "message : {}", c[2]);
                }
                "X" => {
                    let attendu = match c[1].as_str() {
                        "court" => Expediteur::Court,
                        "alpha" => Expediteur::Alphanumerique,
                        _ => Expediteur::Ordinaire,
                    };
                    assert_eq!(sorte_expediteur(&c[2], c.get(3).map(String::as_str).unwrap_or("")), attendu, "expéditeur : {l}");
                }
                "G" => {
                    let e = Entree {
                        sms: c[2] == "sms",
                        expediteur: match c[3].as_str() {
                            "court" => Expediteur::Court,
                            "alpha" => Expediteur::Alphanumerique,
                            _ => Expediteur::Ordinaire,
                        },
                        exclue: c[4].split(',').any(|d| d == "exclue"),
                        j_ai_ecrit: c[4].split(',').any(|d| d == "ecrit"),
                        sortes: c[5].chars().map(sorte).collect(),
                    };
                    let attendu = lire_correction(&c[1]).expect("groupe attendu");
                    assert_eq!(classer(&e), attendu, "classement : {l}");
                }
                "F" => {
                    let correction = (c[2] != "-").then(|| c[2].as_str());
                    let detecte = lire_correction(&c[3]).expect("détecté");
                    assert_eq!(groupe_final(correction, detecte), lire_correction(&c[1]).expect("attendu"), "final : {l}");
                }
                autre => panic!("ligne inconnue : {autre}"),
            }
            n += 1;
        }
        assert!(n > 40, "trop peu d'essais ({n})");
    }

    #[test]
    fn codes_quinze_minutes() {
        assert!(codes_en_haut(1_000_000, 1_000_000 + 60_000));
        assert!(!codes_en_haut(1_000_000, 1_000_000 + CODES_EN_HAUT_MS));
        assert!(!codes_en_haut(0, 5));
        assert!(!codes_en_haut(2_000, 1_000));
    }
}
