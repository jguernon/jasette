//! La fiche de la personne : toucher le nom ou l'avatar dans l'en-tête d'une
//! conversation à deux ouvre une feuille avec son grand avatar, sa pensée
//! (la note de « Quoi de neuf ? »), et les autres services où cette même
//! personne existe ; toucher une ligne ouvre la conversation de ce service.
//!
//! La correspondance se fait ici, localement, sans aucune requête aux
//! services (serveur/PROTOCOLE.md §5.5, « Règle des personnes ») :
//! - **sûre** : le même numéro de téléphone (Google, WhatsApp, Signal,
//!   Telegram) ;
//! - **suggérée** : un nom très proche, ou le nom exact d'un contact du
//!   téléphone qui porte le numéro d'une conversation de la personne.
//!
//! Les liens faits à la main (marque `personnes`) et les refus (marque
//! `personnes_refus`) sont partagés par le serveur Jasette.

use super::*;
use pont::{ContactPerso, Personne};
use std::collections::BTreeMap;

fn tr(fr: &'static str, en: &'static str) -> &'static str {
    if anglais() { en } else { fr }
}

// ---------------------------------------------------------------------------
// La correspondance (sans interface : testée plus bas)
// ---------------------------------------------------------------------------

/// Un numéro en E.164, à la nord-américaine pour les numéros sans indicatif :
/// `(418) 555-1234`, `418-555-1234` → `+14185551234` ; `1-418-555-1234`,
/// `+1 418 555 1234` → `+14185551234` ; `+33 6 12 34 56 78` → `+33612345678`.
/// None sous 7 chiffres (numéros courts, adresses) ou au-delà de 15.
pub(crate) fn e164(s: &str) -> Option<String> {
    let s = s.trim();
    // Pas un numéro : des lettres (hors « @… » d'un id WhatsApp, déjà retiré).
    if s.chars().any(|c| c.is_alphabetic()) {
        return None;
    }
    let chiffres: String = s.chars().filter(char::is_ascii_digit).collect();
    let plus = s.starts_with('+') || s.starts_with("00");
    let chiffres = if s.starts_with("00") { chiffres[2..].to_string() } else { chiffres };
    if chiffres.len() < 7 || chiffres.len() > 15 {
        return None;
    }
    Some(if plus {
        format!("+{chiffres}")
    } else if chiffres.len() == 10 {
        format!("+1{chiffres}")
    } else if chiffres.len() == 11 && chiffres.starts_with('1') {
        format!("+{chiffres}")
    } else {
        format!("+{chiffres}")
    })
}

/// Un numéro E.164 à lire : `+14185551234` → `+1 418-555-1234` (Amérique
/// du Nord) ; les autres pays restent tels quels.
pub(crate) fn numero_lisible(e164: &str) -> String {
    let c: String = e164.chars().filter(char::is_ascii_digit).collect();
    if c.len() == 11 && c.starts_with('1') {
        format!("+1 {}-{}-{}", &c[1..4], &c[4..7], &c[7..])
    } else {
        e164.to_string()
    }
}

/// La clé de comparaison d'un numéro : son E.164.
pub(crate) fn cle_tel(s: &str) -> Option<String> {
    e164(s)
}

/// Le numéro (E.164) d'une conversation à deux, s'il est connu sans rien
/// demander : Google (`number`, sinon le membre ou le nom s'ils sont un
/// numéro), WhatsApp (l'id `<numéro>@s.whatsapp.net`), Signal et Telegram
/// (`number` du pont ; Signal : un id `+…`). Les autres services n'en ont pas.
pub(crate) fn numero_de(id: &str, numero: &str, membre: Option<&str>, nom: &str) -> Option<String> {
    let svc = service_de(id);
    let brut = &id[svc.prefixe().len()..];
    match svc {
        Service::GoogleMessages => cle_tel(numero)
            .or_else(|| membre.filter(|m| est_numero(m)).and_then(cle_tel))
            .or_else(|| est_numero(nom).then(|| cle_tel(nom)).flatten()),
        Service::WhatsApp => brut.strip_suffix("@s.whatsapp.net").and_then(|n| cle_tel(&format!("+{n}"))),
        Service::Signal => cle_tel(numero).or_else(|| brut.starts_with('+').then(|| cle_tel(brut)).flatten()),
        Service::Telegram => cle_tel(numero),
        _ => None,
    }
}

/// Les mots d'un nom, pliés (sans accents ni casse), sans ponctuation,
/// rangés (l'ordre prénom/nom ne compte pas).
pub(crate) fn mots(nom: &str) -> Vec<String> {
    let plie: String = plier(nom).chars().map(|c| if c.is_alphanumeric() { c } else { ' ' }).collect();
    let mut m: Vec<String> = plie.split_whitespace().map(str::to_string).collect();
    m.sort();
    m
}

/// La distance d'édition (Levenshtein), en caractères.
fn distance(a: &str, b: &str) -> usize {
    let b: Vec<char> = b.chars().collect();
    let mut ligne: Vec<usize> = (0..=b.len()).collect();
    for (i, ca) in a.chars().enumerate() {
        let mut prec = ligne[0];
        ligne[0] = i + 1;
        for j in 0..b.len() {
            let tmp = ligne[j + 1];
            ligne[j + 1] = if ca == b[j] { prec } else { 1 + prec.min(ligne[j]).min(ligne[j + 1]) };
            prec = tmp;
        }
    }
    ligne[b.len()]
}

/// Deux noms très proches ? Mêmes mots (accents, casse, ponctuation et
/// ordre ignorés) ; ou, pour deux noms d'au moins deux mots, un mot abrégé
/// (« Steph » pour « Stephanie », 4 lettres au moins) ou une faute de frappe
/// (une lettre par tranche de 8). Un nom trop court (moins de 4 lettres) ou
/// un numéro ne se compare pas.
pub(crate) fn noms_proches(a: &str, b: &str) -> bool {
    if est_numero(a) || est_numero(b) {
        return false;
    }
    let (ma, mb) = (mots(a), mots(b));
    let long = |m: &[String]| m.iter().map(String::len).sum::<usize>();
    if ma.is_empty() || mb.is_empty() || long(&ma) < 4 || long(&mb) < 4 {
        return false;
    }
    if ma == mb {
        return true;
    }
    if ma.len() < 2 || ma.len() != mb.len() {
        return false;
    }
    // Un mot abrégé : chaque mot de l'un est égal à un mot de l'autre, ou
    // en est le début (4 lettres au moins), et au moins un est égal.
    let abrege = |x: &[String], y: &[String]| {
        let mut restants: Vec<&String> = y.iter().collect();
        let mut egaux = 0;
        for m in x {
            if let Some(i) = restants.iter().position(|r| *r == m) {
                restants.remove(i);
                egaux += 1;
            } else if let Some(i) = restants.iter().position(|r| m.len() >= 4 && (r.starts_with(m.as_str()) || m.starts_with(r.as_str())) && r.len().min(m.len()) >= 4) {
                restants.remove(i);
            } else {
                return false;
            }
        }
        egaux >= 1
    };
    if abrege(&ma, &mb) {
        return true;
    }
    let (ja, jb) = (ma.join(" "), mb.join(" "));
    let tolere = ja.chars().count().min(jb.chars().count()) / 8;
    tolere >= 1 && distance(&ja, &jb) <= tolere
}

/// Une conversation candidate : à deux, pas un grand espace.
#[derive(Clone, Debug)]
pub(crate) struct Candidat {
    pub id: String,
    pub nom: String,
    /// Son numéro (E.164), s'il est connu.
    pub numero: Option<String>,
}

/// Pourquoi une conversation est sur la fiche.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub(crate) enum Lien {
    /// La conversation d'où la fiche est ouverte.
    Ancre,
    /// Reliée à la main (marque `personnes`).
    Main,
    /// Le même numéro de téléphone : sûr.
    Numero,
}

/// Pourquoi une conversation est suggérée.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub(crate) enum Raison {
    /// Le nom exact d'un contact du téléphone qui porte le numéro d'une
    /// conversation de la personne.
    Contact,
    /// Un nom très proche.
    Nom,
}

/// La personne autour d'une conversation : ses conversations (l'ancre
/// d'abord) et les suggestions.
#[derive(Clone, Debug, Default)]
pub(crate) struct Groupe {
    pub membres: Vec<(String, Lien)>,
    pub suggestions: Vec<(String, Raison)>,
}

impl Groupe {
    pub fn ids(&self) -> Vec<String> {
        self.membres.iter().map(|(id, _)| id.clone()).collect()
    }
}

fn refuse(refus: &[(String, String)], a: &str, b: &str) -> bool {
    refus.iter().any(|(x, y)| (x == a && y == b) || (x == b && y == a))
}

/// Au plus tant de suggestions.
const SUGGESTIONS_MAX: usize = 6;

/// La personne de `ancre`, parmi `cands` : les liens à la main d'abord, puis
/// le même numéro, de proche en proche ; une conversation refusée avec une
/// des conversations déjà prises n'entre pas. Puis les suggestions : les
/// autres conversations (hors de toute autre personne faite à la main) au
/// nom très proche d'une conversation de la personne, ou au nom exact d'un
/// contact du téléphone (`contacts` : nom, numéro E.164) qui porte un de ses
/// numéros.
pub(crate) fn regrouper(
    ancre: &str,
    cands: &[Candidat],
    personnes: &[Personne],
    refus: &[(String, String)],
    contacts: &[(String, String)],
) -> Groupe {
    let cand = |id: &str| cands.iter().find(|c| c.id == id);
    let mut g = Groupe::default();
    if cand(ancre).is_none() {
        return g;
    }
    g.membres.push((ancre.to_string(), Lien::Ancre));
    let mut i = 0;
    while i < g.membres.len() {
        let x = g.membres[i].0.clone();
        i += 1;
        let mut voisins: Vec<(String, Lien)> = Vec::new();
        for p in personnes.iter().filter(|p| p.convs.contains(&x)) {
            voisins.extend(p.convs.iter().filter(|c| **c != x).map(|c| (c.clone(), Lien::Main)));
        }
        if let Some(n) = cand(&x).and_then(|c| c.numero.as_ref()) {
            voisins.extend(cands.iter().filter(|c| c.id != x && c.numero.as_ref() == Some(n)).map(|c| (c.id.clone(), Lien::Numero)));
        }
        for (y, lien) in voisins {
            if cand(&y).is_none() || g.membres.iter().any(|(m, _)| *m == y) || g.membres.iter().any(|(m, _)| refuse(refus, m, &y)) {
                continue;
            }
            g.membres.push((y, lien));
        }
    }
    // Les suggestions.
    let pris = |id: &str| g.membres.iter().any(|(m, _)| m == id);
    let numeros: Vec<&String> = g.membres.iter().filter_map(|(m, _)| cand(m)?.numero.as_ref()).collect();
    let noms: Vec<&str> = g.membres.iter().filter_map(|(m, _)| cand(m).map(|c| c.nom.as_str())).collect();
    // Les noms des contacts du téléphone qui portent un numéro de la personne.
    let noms_contacts: Vec<Vec<String>> =
        contacts.iter().filter(|(_, n)| numeros.contains(&n)).map(|(nom, _)| mots(nom)).filter(|m| !m.is_empty()).collect();
    let mut sugg: Vec<(String, Raison)> = Vec::new();
    for c in cands {
        if pris(&c.id)
            || g.membres.iter().any(|(m, _)| refuse(refus, m, &c.id))
            || personnes.iter().any(|p| p.convs.contains(&c.id))
        {
            continue;
        }
        let m = mots(&c.nom);
        if !est_numero(&c.nom) && !m.is_empty() && noms_contacts.contains(&m) {
            sugg.push((c.id.clone(), Raison::Contact));
        } else if noms.iter().any(|n| noms_proches(n, &c.nom)) {
            sugg.push((c.id.clone(), Raison::Nom));
        }
    }
    sugg.sort_by_key(|(_, r)| *r == Raison::Nom);
    sugg.truncate(SUGGESTIONS_MAX);
    g.suggestions = sugg;
    g
}

/// Un id neuf de personne : « p », l'heure en ms, et un peu de hasard.
pub(crate) fn nouvel_id() -> String {
    use std::hash::{BuildHasher, Hasher};
    let mut h = std::collections::hash_map::RandomState::new().build_hasher();
    h.write_i64(maintenant_ms());
    format!("p{}{:04x}", maintenant_ms(), h.finish() & 0xffff)
}

fn ranger(a: &str, b: &str) -> (String, String) {
    if b < a { (b.to_string(), a.to_string()) } else { (a.to_string(), b.to_string()) }
}

/// « Lier » (ou « Oui » sur une suggestion) : `x` rejoint la personne de la
/// fiche (`groupe` : ses conversations, l'ancre d'abord). Retirée de toute
/// autre personne ; la personne est créée au besoin ; les refus entre `x` et
/// la fiche sont effacés.
pub(crate) fn lier(personnes: &mut Vec<Personne>, refus: &mut Vec<(String, String)>, groupe: &[String], x: &str, id_neuf: String) {
    let Some(ancre) = groupe.first() else { return };
    if groupe.iter().any(|g| g == x) {
        return;
    }
    for p in personnes.iter_mut() {
        p.convs.retain(|c| c != x);
    }
    match personnes.iter().position(|p| p.convs.contains(ancre)).or_else(|| personnes.iter().position(|p| p.convs.iter().any(|c| groupe.contains(c)))) {
        Some(i) => personnes[i].convs.push(x.to_string()),
        None => personnes.push(Personne { id: id_neuf, convs: vec![ancre.clone(), x.to_string()], principal: String::new() }),
    }
    refus.retain(|(a, b)| !((a == x && groupe.contains(b)) || (b == x && groupe.contains(a))));
    *personnes = pont::nettoyer_personnes(std::mem::take(personnes));
}

/// « Délier » `x` de la fiche : retirée de sa personne, et un refus entre
/// elle et chaque autre conversation de la fiche (un même numéro ne la
/// ramène pas).
pub(crate) fn delier(personnes: &mut Vec<Personne>, refus: &mut Vec<(String, String)>, groupe: &[String], x: &str) {
    for p in personnes.iter_mut() {
        p.convs.retain(|c| c != x);
    }
    *personnes = pont::nettoyer_personnes(std::mem::take(personnes));
    refuser(refus, groupe, x);
}

/// « Pas la même personne » : un refus entre `x` et chaque conversation de la fiche.
pub(crate) fn refuser(refus: &mut Vec<(String, String)>, groupe: &[String], x: &str) {
    for g in groupe.iter().filter(|g| *g != x) {
        refus.push(ranger(g, x));
    }
    *refus = pont::nettoyer_refus(std::mem::take(refus));
}

/// La conversation principale de la personne de `id`, si un principal est
/// choisi (et toujours lié) : son nom et sa photo valent pour toutes.
pub(crate) fn principal_de<'a>(personnes: &'a [Personne], id: &str) -> Option<&'a str> {
    let p = personnes.iter().find(|p| p.convs.iter().any(|c| c == id))?;
    let x = p.principal.as_str();
    (!x.is_empty() && x != id && p.convs.iter().any(|c| c == x)).then_some(x)
}

/// « Utiliser ce nom et cette photo » : `x` devient le nom principal de la personne (créée
/// au besoin avec les conversations de la fiche qui ne sont à personne d'autre).
pub(crate) fn choisir_principal(personnes: &mut Vec<Personne>, groupe: &[String], x: &str, id_neuf: String) {
    let Some(ancre) = groupe.first() else { return };
    let i = match personnes.iter().position(|p| p.convs.contains(ancre)).or_else(|| personnes.iter().position(|p| p.convs.iter().any(|c| groupe.contains(c)))) {
        Some(i) => i,
        None => {
            let libres: Vec<String> = groupe.iter().filter(|g| !personnes.iter().any(|p| p.convs.contains(g))).cloned().collect();
            personnes.push(Personne { id: id_neuf, convs: libres, principal: String::new() });
            personnes.len() - 1
        }
    };
    if !personnes[i].convs.iter().any(|c| c == x) {
        personnes[i].convs.push(x.to_string());
    }
    personnes[i].principal = x.to_string();
    *personnes = pont::nettoyer_personnes(std::mem::take(personnes));
}

// ---------------------------------------------------------------------------
// Le nom et la photo choisis à la main (marque `contacts_perso`)
// ---------------------------------------------------------------------------

/// L'entrée de `contacts_perso` qui vaut pour `id` (PROTOCOLE.md §5.5,
/// « Règle des contacts perso ») : la sienne, sinon celle de la principale de
/// sa personne, sinon celle d'une autre de ses conversations. Rend sa clé.
pub(crate) fn contact_perso_de<'a>(
    perso: &'a BTreeMap<String, ContactPerso>,
    personnes: &[Personne],
    id: &str,
) -> Option<(&'a str, &'a ContactPerso)> {
    if let Some((k, c)) = perso.get_key_value(id) {
        return Some((k.as_str(), c));
    }
    let p = personnes.iter().find(|p| p.convs.iter().any(|c| c == id))?;
    std::iter::once(&p.principal)
        .filter(|x| !x.is_empty())
        .chain(p.convs.iter())
        .find_map(|x| perso.get_key_value(x.as_str()))
        .map(|(k, c)| (k.as_str(), c))
}

/// La clé où enregistrer le nom et la photo de la fiche de `groupe` (l'ancre
/// d'abord) : celle de l'entrée qui vaut déjà pour l'une de ses conversations,
/// sinon la principale de sa personne, sinon l'ancre.
pub(crate) fn cle_contact_perso(perso: &BTreeMap<String, ContactPerso>, personnes: &[Personne], groupe: &[String]) -> String {
    let Some(ancre) = groupe.first() else { return String::new() };
    if let Some((k, _)) = groupe.iter().find_map(|id| contact_perso_de(perso, personnes, id)) {
        return k.to_string();
    }
    personnes
        .iter()
        .find(|p| p.convs.contains(ancre))
        .map(|p| p.principal.clone())
        .filter(|x| !x.is_empty())
        .unwrap_or_else(|| ancre.clone())
}

/// Les conversations de la fiche (même numéro compris) reliées à la main dans
/// une personne, créée au besoin, sans toucher à sa principale : le nom choisi
/// vaut alors pour toutes. Une conversation déjà à une autre personne y reste.
pub(crate) fn relier_fiche(personnes: &mut Vec<Personne>, groupe: &[String], id_neuf: String) {
    let Some(ancre) = groupe.first() else { return };
    if groupe.len() < 2 {
        return;
    }
    let libres: Vec<String> = groupe.iter().filter(|g| !personnes.iter().any(|p| p.convs.contains(g))).cloned().collect();
    match personnes.iter().position(|p| p.convs.contains(ancre)).or_else(|| personnes.iter().position(|p| p.convs.iter().any(|c| groupe.contains(c)))) {
        Some(i) => personnes[i].convs.extend(libres),
        None => personnes.push(Personne { id: id_neuf, convs: libres, principal: String::new() }),
    }
    *personnes = pont::nettoyer_personnes(std::mem::take(personnes));
}

/// Enregistre le nom (vide : celui du service) sous `cle` ; la photo et la
/// case Google restent celles de l'entrée, sauf `photo` donnée. Une entrée
/// sans nom ni photo disparaît (comme « Rétablir »).
pub(crate) fn poser_contact_perso(perso: &mut BTreeMap<String, ContactPerso>, cle: &str, nom: &str, photo: Option<String>, google: bool) {
    let mut e = perso.remove(cle).unwrap_or_default();
    e.nom = nom.trim().to_string();
    if let Some(p) = photo {
        e.photo = p;
    }
    e.google = google;
    perso.insert(cle.to_string(), e);
    *perso = pont::nettoyer_contacts_perso(std::mem::take(perso));
}

/// « Changer la photo » : `src` recadrée en carré au centre, réduite à 512 px
/// (au plus), en JPEG, dans `dossier` (nommée par son contenu). Rend son chemin.
pub(crate) fn photo_carree(src: &std::path::Path, dossier: &std::path::Path) -> Result<PathBuf, String> {
    let img = image::open(src).map_err(|e| e.to_string())?;
    let (l, h) = (img.width(), img.height());
    let cote = l.min(h);
    if cote == 0 {
        return Err("image vide".into());
    }
    let carre = img.crop_imm((l - cote) / 2, (h - cote) / 2, cote, cote);
    let cible = cote.min(512);
    let petite = if cible == cote { carre } else { carre.resize_exact(cible, cible, image::imageops::FilterType::Lanczos3) };
    let mut octets = Vec::new();
    image::codecs::jpeg::JpegEncoder::new_with_quality(&mut octets, 88)
        .encode_image(&petite.to_rgb8())
        .map_err(|e| e.to_string())?;
    use std::hash::{Hash, Hasher};
    let mut h = std::collections::hash_map::DefaultHasher::new();
    octets.hash(&mut h);
    std::fs::create_dir_all(dossier).map_err(|e| e.to_string())?;
    let chemin = dossier.join(format!("{:016x}.jpg", h.finish()));
    std::fs::write(&chemin, &octets).map_err(|e| e.to_string())?;
    Ok(chemin)
}

// ---------------------------------------------------------------------------
// L'interface
// ---------------------------------------------------------------------------

/// La fiche ouverte : la conversation d'où on l'a ouverte, et le choix
/// « Lier une autre conversation… » (ouvert, puce de service).
#[derive(Clone, Default)]
pub(crate) struct Fiche {
    pub ancre: String,
    pub lier: bool,
    pub filtre: Option<Service>,
    /// La fenêtre du crayon (nom et photo), si elle est ouverte.
    pub edition: Option<EditionPerso>,
}

/// La fenêtre du crayon : la photo choisie (déjà recadrée, pas encore
/// enregistrée), la case « Aussi dans les Contacts Google » et une erreur.
#[derive(Clone, Default)]
pub(crate) struct EditionPerso {
    pub photo: Option<PathBuf>,
    pub google: bool,
    pub erreur: Option<String>,
}

/// La taille du grand avatar.
const GRAND: f32 = 140.;

impl Jasette {
    /// La conversation principale de la personne de `conv` (jamais pour un groupe).
    pub(crate) fn principale_de(&self, conv: &Conv) -> Option<&Conv> {
        if conv.group || conv.large {
            return None;
        }
        let x = principal_de(&self.reglages.personnes, &conv.id)?;
        self.conv(x).filter(|c| !c.group)
    }

    /// L'entrée `contacts_perso` qui vaut pour `conv` (jamais pour un groupe), et sa clé.
    pub(crate) fn contact_perso(&self, conv: &Conv) -> Option<(&str, &ContactPerso)> {
        if conv.group || conv.large {
            return None;
        }
        contact_perso_de(&self.reglages.contacts_perso, &self.reglages.personnes, &conv.id)
    }

    /// La photo choisie à la main pour `conv` (sa copie locale), s'il y en a une.
    pub(crate) fn photo_perso(&self, conv: &Conv) -> Option<&PathBuf> {
        let (cle, _) = self.contact_perso(conv)?;
        self.photos_perso.get(cle)
    }

    /// Le pont qui passe par le serveur Jasette (mode client), s'il y en a un.
    pub(crate) fn pont_distant(&self) -> Option<&pont::Pont> {
        self.pont
            .as_ref()
            .filter(|p| !p.local())
            .or_else(|| SERVICES_META.iter().find_map(|s| self.meta(*s).pont.as_ref().filter(|p| !p.local())))
    }

    /// Les copies locales des photos de `contacts_perso` : le fichier lui-même
    /// (mode local, ou photo choisie ici), sinon la copie reçue du serveur.
    pub(crate) fn maj_photos_perso(&mut self) {
        let mut r = HashMap::new();
        for (cle, c) in &self.reglages.contacts_perso {
            if c.photo.is_empty() {
                continue;
            }
            let p = PathBuf::from(&c.photo);
            let local = if p.is_file() { Some(p) } else { self.pont_distant().and_then(|x| x.photo_locale(&c.photo)) };
            if let Some(l) = local {
                r.insert(cle.clone(), l);
            }
        }
        self.photos_perso = r;
    }

    /// Le nom montré partout pour une conversation : celui choisi à la main
    /// (crayon de la fiche), sinon celui de la principale de sa personne,
    /// sinon le sien. La recherche trouve aussi le nom d'origine.
    pub(crate) fn nom_affiche(&self, conv: &Conv) -> String {
        if let Some((_, c)) = self.contact_perso(conv).filter(|(_, c)| !c.nom.is_empty()) {
            return c.nom.clone();
        }
        let principal = self.principale_de(conv).map(|p| p.name.trim()).filter(|n| !n.is_empty());
        let nom = principal.unwrap_or(conv.name.trim());
        if nom.is_empty() { t("Sans nom").to_string() } else { nom.to_string() }
    }

    /// L'avatar montré partout pour une conversation (voir `photo_conv`).
    /// Cerclé si le contact a une story (src/stories.rs).
    pub(crate) fn avatar_affiche(&self, conv: &Conv, taille: f32) -> AnyElement {
        let nom = self.nom_affiche(conv);
        self.avatar_story(conv, taille, |t| self.avatar_conv(conv, &nom, t))
    }

    /// Les conversations à deux connues (chargées, archivées ou en cache),
    /// sans doublon ; les privées seulement dans la vue Privées.
    pub(crate) fn candidats_personne(&self) -> Vec<Candidat> {
        let prive = self.vue_privee();
        let mut vus = HashSet::new();
        self.convs
            .iter()
            .chain(self.archives.iter())
            .chain(self.fantomes.iter())
            .filter(|c| !c.group && !c.large && vus.insert(c.id.as_str()))
            .filter(|c| prive || !self.privee(&c.id))
            .map(|c| Candidat {
                id: c.id.clone(),
                nom: c.name.clone(),
                numero: numero_de(&c.id, &c.number, c.members.first().map(String::as_str), &c.name),
            })
            .collect()
    }

    /// La personne de cette conversation.
    pub(crate) fn groupe_personne(&self, ancre: &str) -> Groupe {
        regrouper(ancre, &self.candidats_personne(), &self.reglages.personnes, &self.reglages.personnes_refus, &self.contacts_tel)
    }

    /// Ouvre la fiche (conversation à deux seulement).
    pub(crate) fn ouvrir_fiche(&mut self, id: &str) {
        if self.conv(id).is_some_and(|c| !c.group && !c.large) {
            self.menu_conv = false;
            self.fiche = Some(Fiche { ancre: id.to_string(), ..Default::default() });
        }
    }

    /// Les marques des personnes ont changé ici : gardées et partagées.
    fn personnes_changees(&mut self) {
        self.marques_changees();
    }

    fn fiche_lier(&mut self, x: &str) {
        let Some(f) = self.fiche.as_ref() else { return };
        let groupe = self.groupe_personne(&f.ancre).ids();
        lier(&mut self.reglages.personnes, &mut self.reglages.personnes_refus, &groupe, x, nouvel_id());
        if let Some(f) = self.fiche.as_mut() {
            f.lier = false;
        }
        self.personnes_changees();
    }

    fn fiche_delier(&mut self, x: &str) {
        let Some(f) = self.fiche.as_ref() else { return };
        let groupe = self.groupe_personne(&f.ancre).ids();
        delier(&mut self.reglages.personnes, &mut self.reglages.personnes_refus, &groupe, x);
        self.personnes_changees();
    }

    fn fiche_refuser(&mut self, x: &str) {
        let Some(f) = self.fiche.as_ref() else { return };
        let groupe = self.groupe_personne(&f.ancre).ids();
        refuser(&mut self.reglages.personnes_refus, &groupe, x);
        self.personnes_changees();
    }

    fn fiche_principal(&mut self, x: &str) {
        let Some(f) = self.fiche.as_ref() else { return };
        let groupe = self.groupe_personne(&f.ancre).ids();
        choisir_principal(&mut self.reglages.personnes, &groupe, x, nouvel_id());
        self.personnes_changees();
    }

    /// Le numéro de la personne de la fiche (E.164), s'il est connu.
    fn numero_fiche(&self, groupe: &Groupe) -> Option<String> {
        let ids = groupe.ids();
        self.candidats_personne().into_iter().filter(|c| ids.contains(&c.id)).find_map(|c| c.numero)
    }

    /// Le crayon du grand avatar : ouvre la fenêtre, le nom choisi dans le champ.
    fn ouvrir_edition(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        let Some(f) = self.fiche.as_ref() else { return };
        let ids = self.groupe_personne(&f.ancre).ids();
        let (nom, google) = self.perso_fiche(&ids).map(|(_, c)| (c.nom.clone(), c.google)).unwrap_or_default();
        if let Some(f) = self.fiche.as_mut() {
            f.edition = Some(EditionPerso { photo: None, google, erreur: None });
        }
        self.fiche_nom.update(cx, |s, cx| {
            s.set_value(nom, window, cx);
            s.focus(window, cx);
        });
        cx.notify();
    }

    /// « Changer la photo » : le sélecteur de fichiers (comme les pièces
    /// jointes), puis le recadrage carré de 512 px, hors du fil de l'interface.
    fn choisir_photo_perso(&mut self, cx: &mut Context<Self>) {
        let choix = cx.prompt_for_paths(PathPromptOptions {
            files: true,
            directories: false,
            multiple: false,
            prompt: Some(t("Choisir").into()),
        });
        cx.spawn(async move |this, cx| {
            let Ok(Ok(Some(chemins))) = choix.await else { return };
            let Some(src) = chemins.into_iter().next() else { return };
            let dossier = dossier_medias().join("contacts");
            let r = cx.background_executor().spawn(async move { photo_carree(&src, &dossier) }).await;
            let _ = this.update(cx, |this, cx| {
                if let Some(e) = this.fiche.as_mut().and_then(|f| f.edition.as_mut()) {
                    match r {
                        Ok(p) => {
                            e.photo = Some(p);
                            e.erreur = None;
                        }
                        Err(err) => e.erreur = Some(format!("{} : {err}", t("Image illisible"))),
                    }
                }
                cx.notify();
            });
        })
        .detach();
    }

    /// « Enregistrer » : la marque `contacts_perso` (le nom tout de suite ; la
    /// photo aussi en mode local, sinon une fois téléversée au serveur).
    pub(crate) fn enregistrer_perso(&mut self, cx: &mut Context<Self>) {
        let Some(f) = self.fiche.as_ref() else { return };
        let Some(e) = f.edition.clone() else { return };
        let ancre = f.ancre.clone();
        let nom = self.fiche_nom.read(cx).value().trim().to_string();
        let groupe = self.groupe_personne(&ancre).ids();
        relier_fiche(&mut self.reglages.personnes, &groupe, nouvel_id());
        let cle = cle_contact_perso(&self.reglages.contacts_perso, &self.reglages.personnes, &groupe);
        if cle.is_empty() {
            return;
        }
        let mut photo = None;
        if let Some(local) = e.photo {
            let sortie = self.photos_televersees.clone();
            let (c, google) = (cle.clone(), e.google);
            let distant = self.pont_distant().is_some_and(|p| {
                p.televerser_photo(local.clone(), move |r| {
                    if let Ok(mut v) = sortie.lock() {
                        v.push((c, google, r));
                    }
                })
            });
            if !distant {
                photo = Some(local.to_string_lossy().into_owned());
            } else {
                // En attendant le serveur : déjà montrée ici.
                self.photos_perso.insert(cle.clone(), local);
            }
        }
        // Une photo en route vers le serveur entrera dans la marque à son arrivée
        // (photo_perso_televersee) ; une entrée sans nom ni photo n'y est jamais.
        poser_contact_perso(&mut self.reglages.contacts_perso, &cle, &nom, photo, e.google);
        if let Some(f) = self.fiche.as_mut() {
            f.edition = None;
        }
        self.marques_changees();
        // La photo en route reste montrée jusqu'au retour du serveur.
        self.photos_perso_a_faire = false;
        cx.notify();
    }

    /// La photo d'un contact perso est arrivée au serveur : son chemin va dans la marque.
    pub(crate) fn photo_perso_televersee(&mut self, cle: &str, google: bool, r: Result<String, String>) {
        match r {
            Ok(distant) => {
                let e = self.reglages.contacts_perso.get(cle).cloned().unwrap_or_default();
                poser_contact_perso(&mut self.reglages.contacts_perso, cle, &e.nom, Some(distant), e.google || google);
                self.marques_changees();
            }
            Err(err) => {
                self.erreur = Some(format!("{} : {err}", t("La photo n'a pas pu être envoyée au serveur Jasette")));
                self.photos_perso_a_faire = true;
            }
        }
    }

    /// « Rétablir » : plus de nom ni de photo choisis à la main.
    fn retablir_perso(&mut self, cx: &mut Context<Self>) {
        let Some(f) = self.fiche.as_ref() else { return };
        let ids = self.groupe_personne(&f.ancre).ids();
        while let Some(cle) = self.perso_fiche(&ids).map(|(k, _)| k.to_string()) {
            self.reglages.contacts_perso.remove(&cle);
        }
        if let Some(f) = self.fiche.as_mut() {
            f.edition = None;
        }
        self.marques_changees();
        cx.notify();
    }

    /// Le crayon : un petit badge rond (carré en EGA, Live et Sirène) sur le bas du grand avatar.
    fn crayon_avatar(&self, taille: f32, fond: Hsla, cx: &mut Context<Self>) -> impl IntoElement {
        let carre = th().ega || retro::retro();
        let d = (taille * 0.25).max(26.);
        div()
            .id("fiche-crayon")
            .absolute()
            .right(px(if carre { -4. } else { taille * 0.02 }))
            .bottom(px(if carre { -4. } else { taille * 0.02 }))
            .size(px(d))
            .when(!carre, |b| b.rounded(px(d)))
            .bg(th().accent)
            .border_3()
            .border_color(fond)
            .when(th().ega, |b| b.shadow(ombre_dure(2.)))
            .flex()
            .items_center()
            .justify_center()
            .cursor_pointer()
            .hover(|s| s.opacity(0.85))
            .tooltip(|w, cx| gpui_component::tooltip::Tooltip::new(t("Changer le nom et la photo")).build(w, cx))
            .on_click(cx.listener(|this, _, window, cx| {
                cx.stop_propagation();
                this.ouvrir_edition(window, cx);
            }))
            .child(icone("pencil", d * 0.46, if th().ega { c(EGA_NOIR) } else { blanc(1.) }))
    }

    /// La fenêtre du crayon : l'aperçu, « Changer la photo », le champ Nom, la
    /// case Google (mode serveur, personne avec un numéro) et les boutons.
    fn fenetre_edition(&self, groupe: &Groupe, ancre: &Conv, e: &EditionPerso, cx: &mut Context<Self>) -> AnyElement {
        let ids = groupe.ids();
        let existe = self.perso_fiche(&ids).is_some();
        let saisi = self.fiche_nom.read(cx).value().trim().to_string();
        let nom = if saisi.is_empty() { self.nom_service_personne(groupe) } else { saisi };
        let photo = e.photo.clone().or_else(|| self.meilleure_photo(groupe));
        let offrir_google = self.pont_distant().is_some() && self.numero_fiche(groupe).is_some();
        let mut boite = div()
            .id("fiche-edition")
            .w(px(380.))
            .max_w_full()
            .p(px(20.))
            .flex()
            .flex_col()
            .gap(px(14.))
            .bg(th().menu)
            .border_1()
            .border_color(th().trait_fin)
            .rounded(rayon(20.))
            .when(th().ega, |d| d.shadow(ombre_dure(6.)))
            .when(!th().ega && !retro::retro(), |d| d.shadow(ombre_verre()))
            .on_click(|_, _, cx| cx.stop_propagation())
            .child(div().text_size(px(17.)).font_weight(FontWeight::BOLD).text_color(th().texte_fort).child(t("Changer le nom et la photo")))
            .child(
                div()
                    .flex()
                    .flex_row()
                    .items_center()
                    .gap(px(16.))
                    .child(avatar(&nom, &ancre.color, false, photo.as_ref(), 84.))
                    .child(
                        div().flex().flex_col().gap(px(6.)).child(
                            Self::bouton_fiche("edition-photo", t("Changer la photo"), false).on_click(cx.listener(|this, _, _, cx| {
                                this.choisir_photo_perso(cx);
                            })),
                        ),
                    ),
            )
            .child(
                div()
                    .flex()
                    .flex_col()
                    .gap(px(6.))
                    .child(div().text_size(px(12.)).font_weight(FontWeight::BOLD).text_color(th().texte_pale).child(t("Nom")))
                    .child(champ_surface().px(px(10.)).py(px(1.)).child(Input::new(&self.fiche_nom).appearance(false).cleanable(true))),
            );
        if offrir_google {
            boite = boite.child(div().mx(px(-16.)).child(ligne_interrupteur(
                "edition-google",
                t("Aussi dans les Contacts Google"),
                t("Le téléphone jumelé l'enregistre dans son carnet, s'il en a la permission."),
                e.google,
                cx.listener(|this, _, _, cx| {
                    if let Some(e) = this.fiche.as_mut().and_then(|f| f.edition.as_mut()) {
                        e.google = !e.google;
                    }
                    cx.notify();
                }),
            )));
        }
        if let Some(err) = &e.erreur {
            boite = boite.child(div().text_size(px(12.)).text_color(th().danger).child(err.clone()));
        }
        let mut pied = div().flex().flex_row().items_center().gap(px(8.)).pt(px(4.));
        if existe {
            pied = pied.child(Self::bouton_fiche("edition-retablir", t("Rétablir"), false).on_click(cx.listener(|this, _, _, cx| {
                this.retablir_perso(cx);
            })));
        }
        pied = pied
            .child(div().flex_1())
            .child(Self::bouton_fiche("edition-annuler", t("Annuler"), false).on_click(cx.listener(|this, _, _, cx| {
                if let Some(f) = this.fiche.as_mut() {
                    f.edition = None;
                }
                cx.notify();
            })))
            .child(Self::bouton_fiche("edition-enregistrer", t("Enregistrer"), true).on_click(cx.listener(|this, _, _, cx| {
                this.enregistrer_perso(cx);
            })));
        boite = boite.child(pied);
        div()
            .id("fiche-edition-voile")
            .absolute()
            .top_0()
            .left_0()
            .right_0()
            .bottom_0()
            .bg(hsla(0., 0., 0., if th().sombre || th().ega { 0.45 } else { 0.2 }))
            .flex()
            .items_center()
            .justify_center()
            .on_click(cx.listener(|this, _, _, cx| {
                cx.stop_propagation();
                if let Some(f) = this.fiche.as_mut() {
                    f.edition = None;
                }
                cx.notify();
            }))
            .child(boite)
            .into_any_element()
    }

    /// Le nom de la personne : celui de la conversation principale, sinon
    /// celui de la conversation d'où la fiche est ouverte.
    pub(crate) fn nom_personne(&self, groupe: &Groupe) -> String {
        let ids = groupe.ids();
        if let Some(n) = self.perso_fiche(&ids).map(|(_, c)| c.nom.clone()).filter(|n| !n.is_empty()) {
            return n;
        }
        self.nom_service_personne(groupe)
    }

    /// L'entrée `contacts_perso` de la fiche (l'une de ses conversations), et sa clé.
    pub(crate) fn perso_fiche(&self, ids: &[String]) -> Option<(&str, &ContactPerso)> {
        ids.iter().find_map(|id| contact_perso_de(&self.reglages.contacts_perso, &self.reglages.personnes, id))
    }

    /// Le nom de la personne selon les services (sans le nom choisi à la main).
    fn nom_service_personne(&self, groupe: &Groupe) -> String {
        let ids = groupe.ids();
        let principal = self
            .reglages
            .personnes
            .iter()
            .find(|p| !p.principal.is_empty() && p.convs.iter().any(|c| ids.contains(c)))
            .map(|p| p.principal.clone())
            .filter(|p| ids.contains(p));
        let id = principal.or_else(|| ids.first().cloned()).unwrap_or_default();
        self.conv(&id).map(|c| c.name.clone()).filter(|n| !n.is_empty()).unwrap_or_else(|| t("Sans nom").to_string())
    }

    /// La meilleure photo parmi les conversations de la personne : celle du
    /// plus gros fichier (la plus grande, d'habitude).
    fn meilleure_photo(&self, groupe: &Groupe) -> Option<PathBuf> {
        let ids = groupe.ids();
        // La photo choisie à la main l'emporte.
        if let Some(p) = self.perso_fiche(&ids).and_then(|(cle, _)| self.photos_perso.get(cle)) {
            return Some(p.clone());
        }
        self.meilleure_photo_service(groupe)
    }

    /// La meilleure photo selon les services (sans celle choisie à la main).
    fn meilleure_photo_service(&self, groupe: &Groupe) -> Option<PathBuf> {
        // La photo de la principale, si elle en a une, l'emporte.
        let ids = groupe.ids();
        if let Some(p) = ids.iter().filter_map(|id| self.conv(id)).find_map(|c| self.principale_de(c)).and_then(|p| self.photo_propre(p)) {
            return Some(p.clone());
        }
        groupe
            .membres
            .iter()
            .filter_map(|(id, _)| self.photo_conv(self.conv(id)?))
            .max_by_key(|p| std::fs::metadata(p).map(|m| m.len()).unwrap_or(0))
            .cloned()
    }

    /// La pensée de la personne : sa note (le service, le texte, le temps restant).
    fn pensee(&self, groupe: &Groupe) -> Option<(Service, String, Option<i64>)> {
        if !self.reglages.notes_contacts {
            return None;
        }
        groupe.membres.iter().find_map(|(id, _)| {
            let n = self.notes.contacts.get(id)?;
            let reste = (n.time > 0).then(|| n.time + 24 * 3600 * 1000 - maintenant_ms()).filter(|r| *r > 0);
            Some((service_de(id), n.text.clone(), reste))
        })
    }

    /// Un petit bouton texte de la fiche.
    fn bouton_fiche(id: impl Into<gpui::ElementId>, texte: impl Into<SharedString>, plein: bool) -> gpui::Stateful<Div> {
        div()
            .id(id)
            .flex_none()
            .px(px(12.))
            .py(px(5.))
            .rounded(rayon(999.))
            .text_size(px(12.5))
            .font_weight(FontWeight::SEMIBOLD)
            .cursor_pointer()
            .when(plein, |d| d.bg(th().accent).text_color(if th().ega { c(EGA_NOIR) } else { blanc(1.) }))
            .when(!plein, |d| d.bg(th().menu_champ).text_color(th().texte_fort).border_1().border_color(th().trait_fin))
            .hover(|s| s.opacity(0.85))
            .child(texte.into())
    }

    /// La bulle de pensée au-dessus du grand avatar : le texte en grand, et
    /// trois petits ronds qui descendent vers la tête.
    fn bulle_pensee(svc: Service, texte: String, reste: Option<i64>) -> Div {
        let carre = th().ega || retro::retro();
        let fond = if th().ega { c(EGA_NOIR) } else { ton(blanc(1.), th().menu_champ) };
        let bord = if th().ega { th().texte_pale } else { ton(blanc(1.), th().trait_fin) };
        let rond = |t: f32| {
            div().size(px(t)).flex_none().when(!carre, |d| d.rounded(px(t))).bg(fond).border_1().border_color(bord).when(!carre, |d| d.shadow(ombre_flottante()))
        };
        let legende = match reste {
            Some(r) => format!("{} · {}", svc.nom(), notes::restant_court(r)),
            None => svc.nom().to_string(),
        };
        div()
            .flex()
            .flex_col()
            .items_center()
            .child(
                div()
                    .max_w(px(320.))
                    .px(px(18.))
                    .py(px(10.))
                    .when(!carre, |d| d.rounded(px(22.)).shadow(ombre_flottante()))
                    .bg(fond)
                    .border_1()
                    .border_color(bord)
                    .flex()
                    .flex_col()
                    .items_center()
                    .gap(px(2.))
                    .child(div().text_size(px(19.)).line_height(px(25.)).text_color(th().texte_fort).text_center().child(texto_emoji(texte)))
                    .child(div().text_size(px(11.)).text_color(th().texte_pale).child(legende)),
            )
            .child(div().pt(px(3.)).pl(px(34.)).child(rond(11.)))
            .child(div().pt(px(1.)).pl(px(54.)).child(rond(6.)))
    }

    /// La fiche de la personne (feuille par-dessus l'app), si elle est ouverte.
    pub(crate) fn fiche_vue(&self, cx: &mut Context<Self>) -> Option<AnyElement> {
        let f = self.fiche.clone()?;
        let ancre = self.conv(&f.ancre)?.clone();
        let groupe = self.groupe_personne(&f.ancre);
        let nom = self.nom_personne(&groupe);
        let photo = self.meilleure_photo(&groupe);
        let en_ligne = groupe.membres.iter().any(|(id, _)| self.conv(id).is_some_and(|c| self.en_ligne(c)));
        let fond_feuille = if th().sombre || retro::retro() || th().ega { th().menu } else { blanc(0.97) };

        let fermer = div()
            .id("fiche-fermer")
            .absolute()
            .top(px(12.))
            .right(px(12.))
            .size(px(30.))
            .rounded(rayon(999.))
            .flex()
            .items_center()
            .justify_center()
            .cursor_pointer()
            .hover(|s| s.bg(th().survol))
            .on_click(cx.listener(|this, _, _, cx| {
                this.fiche = None;
                cx.notify();
            }))
            .child(icone("x", 16., th().texte_pale));

        // Le haut : la pensée, le grand avatar, le nom et les noms par service.
        // Pendant « Lier une autre conversation… », l'avatar se fait plus petit.
        let grand_t = if f.lier { 72. } else { GRAND };
        let crayon = (!f.lier).then(|| self.crayon_avatar(grand_t, fond_feuille, cx));
        // La story d'un des comptes de la personne (src/stories.rs) : un cercle
        // autour du grand avatar (le crayon garde son geste) et « Voir la story ».
        let story = groupe
            .membres
            .iter()
            .filter_map(|(id, _)| self.conv(id))
            .filter_map(|c| self.story_de(c))
            .min_by_key(|(_, s)| s.sorte != stories::SorteStory::Instagram)
            .map(|(id, s)| (id.to_string(), s.sorte));
        let grand_avatar = match &story {
            Some((_, sorte)) => stories::anneau_story(*sorte, grand_t, |t| avatar(&nom, &ancre.color, false, photo.as_ref(), t)),
            None => avatar(&nom, &ancre.color, false, photo.as_ref(), grand_t),
        };
        let grand = div()
            .relative()
            .child(grand_avatar)
            .children(crayon)
            .when(en_ligne, |d| {
                // Le point vert, à la taille du grand avatar (carré en EGA, Live et Sirène).
                let carre = th().ega || retro::retro();
                d.child(
                    div()
                        .absolute()
                        .left(px(if carre { -2. } else { grand_t * 0.08 }))
                        .bottom(px(if carre { -2. } else { grand_t * 0.08 }))
                        .size(px(grand_t * 0.19))
                        .when(!carre, |p| p.rounded(px(grand_t * 0.1)))
                        .bg(if th().ega { c(0x55ff55) } else if carre { c(0x2fb52a) } else { c(0x31c45a) })
                        .border_4()
                        .border_color(fond_feuille),
                )
            });
        let mut haut = div().flex().flex_col().items_center().gap(px(4.)).pt(px(8.));
        if let Some((svc, texte, reste)) = self.pensee(&groupe).filter(|_| !f.lier) {
            haut = haut.child(Self::bulle_pensee(svc, texte, reste));
        }
        haut = haut.child(grand).child(
            div()
                .pt(px(10.))
                .text_size(px(24.))
                .font_weight(FontWeight::BOLD)
                .text_color(th().texte_fort)
                .text_center()
                .child(texte_emoji(nom.clone(), th().texte_fort, Some(FontWeight::BOLD))),
        );
        // En sourdine (src/sourdine.rs) : la cloche barrée et jusqu'à quand.
        if let Some(fin) = sourdine::echeance_de(&self.reglages.sourdine, &groupe.ids(), maintenant_ms()).filter(|_| !f.lier) {
            haut = haut.child(
                div()
                    .flex()
                    .flex_row()
                    .items_center()
                    .gap(px(6.))
                    .text_size(px(13.))
                    .text_color(th().texte_pale)
                    .child(Self::icone_sourdine(14., th().texte_pale))
                    .child(sourdine::libelle(fin)),
            );
        }
        if let Some((id_story, sorte)) = story.filter(|_| !f.lier) {
            let titre = match sorte {
                stories::SorteStory::Instagram => notes::tr("Voir la story Instagram", "View Instagram story"),
                stories::SorteStory::Facebook => notes::tr("Voir la story Facebook", "View Facebook story"),
            };
            haut = haut.child(div().pt(px(6.)).child(Self::bouton_fiche("fiche-story", titre, true).on_click(cx.listener(
                move |this, _, _, cx| {
                    this.ouvrir_story(&id_story);
                    cx.notify();
                },
            ))));
        }
        let nom_plie = mots(&nom);
        let mut autres_noms: Vec<(Service, String)> = Vec::new();
        for (id, _) in &groupe.membres {
            if let Some(c) = self.conv(id)
                && !c.name.is_empty()
                && mots(&c.name) != nom_plie
                && !autres_noms.iter().any(|(_, n)| mots(n) == mots(&c.name))
            {
                autres_noms.push((service_de(id), c.name.clone()));
            }
        }
        for (svc, n) in autres_noms {
            haut = haut.child(
                div()
                    .flex()
                    .flex_row()
                    .items_center()
                    .gap(px(6.))
                    .text_size(px(13.))
                    .text_color(th().texte_pale)
                    .child(logo(svc, 14.))
                    .child(texte_emoji(
                        if anglais() { format!("“{n}” on {}", svc.nom()) } else { format!("« {n} » sur {}", svc.nom()) },
                        th().texte_pale,
                        None,
                    )),
            );
        }

        // Les numéros de téléphone (très utile) : « +1 418-555-1234 sur Google
        // Messages, WhatsApp », un par numéro distinct.
        let mut numeros: Vec<(String, Vec<Service>)> = Vec::new();
        for (id, _) in &groupe.membres {
            let Some(c) = self.conv(id) else { continue };
            if c.group {
                continue;
            }
            let Some(n) = numero_de(&c.id, &c.number, c.members.first().map(String::as_str), &c.name) else { continue };
            let svc = service_de(id);
            match numeros.iter_mut().find(|(x, _)| *x == n) {
                Some((_, l)) => {
                    if !l.contains(&svc) {
                        l.push(svc);
                    }
                }
                None => numeros.push((n, vec![svc])),
            }
        }
        for (n, svcs) in numeros {
            let noms = svcs.iter().map(|s| s.nom()).collect::<Vec<_>>().join(", ");
            let joli = numero_lisible(&n);
            haut = haut.child(
                div()
                    .flex()
                    .flex_row()
                    .items_center()
                    .gap(px(6.))
                    .text_size(px(13.))
                    .text_color(th().texte_pale)
                    .child(icone("phone", 14., th().texte_pale))
                    .child(div().text_color(th().texte).font_weight(FontWeight::SEMIBOLD).child(joli))
                    .child(if anglais() { format!("on {noms}") } else { format!("sur {noms}") }),
            );
        }

        let corps = if f.lier { self.fiche_choix_lier(&groupe, cx) } else { self.fiche_lignes(&groupe, &nom, cx) };

        let feuille = verre(0.97, 26.)
            .when(th().sombre || th().ega, |d| d.bg(th().menu).border_1().border_color(th().trait_fin))
            .when(!th().ega && !retro::retro(), |d| d.shadow(ombre_verre()))
            .when(th().ega, |d| d.shadow(ombre_dure(6.)))
            .id("fiche-feuille")
            .relative()
            .w(px(480.))
            .max_h_full()
            .overflow_y_scroll()
            .p(px(22.))
            .flex()
            .flex_col()
            .gap(px(16.))
            .on_click(|_, _, cx| cx.stop_propagation())
            .child(haut.flex_none())
            .child(corps.flex_none())
            .child(fermer);
        Some(
            div()
                .id("fiche-voile")
                // Bloque la souris (et la molette) pour ce qui est derrière :
                // seule la fiche défile.
                .occlude()
                .absolute()
                .top_0()
                .left_0()
                .right_0()
                .bottom_0()
                .bg(hsla(0., 0., 0., if th().sombre || th().ega { 0.55 } else { 0.25 }))
                .p(px(24.))
                .flex()
                .items_center()
                .justify_center()
                .on_click(cx.listener(|this, _, _, cx| {
                    this.fiche = None;
                    cx.notify();
                }))
                .child(feuille)
                .children(f.edition.as_ref().map(|e| self.fenetre_edition(&groupe, &ancre, e, cx)))
                .into_any_element(),
        )
    }

    /// Un titre de section de la fiche.
    fn titre_fiche(texte: &'static str) -> Div {
        div().pt(px(4.)).text_size(px(12.)).font_weight(FontWeight::BOLD).text_color(th().texte_pale).child(texte)
    }

    /// La ligne d'une conversation (sur la fiche ou dans le choix à lier) :
    /// avatar et badge du service, nom, dernier message et date, non-lu, présence.
    fn ligne_conv_fiche(&self, conv: &Conv, active: bool, detail: Option<String>, boutons: Option<Div>) -> Div {
        let svc = service_de(&conv.id);
        let nom = if conv.name.is_empty() { t("Sans nom").to_string() } else { conv.name.clone() };
        let fond = if active { th().choisi } else if th().sombre || retro::retro() || th().ega { th().menu } else { blanc(0.97) };
        let apercu = match self.activite(conv) {
            Some(a) => a,
            None if conv.preview.is_empty() => svc.nom().to_string(),
            None => crate::ponts_langue::affiche(&conv.preview, false).replace('\n', " "),
        };
        div()
            .flex()
            .flex_row()
            .items_center()
            .gap(px(12.))
            .px(px(10.))
            .py(px(8.))
            .rounded(rayon(14.))
            .when(active, |d| d.bg(th().choisi).border_1().border_color(th().accent))
            .child(
                div()
                    .relative()
                    .flex_none()
                    // Chaque ligne de la fiche garde le nom et la photo propres à son service.
                    .child(match self.story_de(conv).filter(|(id, _)| *id == conv.id) {
                        Some((_, s)) => stories::anneau_story(s.sorte, 40., |t| avatar(&nom, &conv.color, false, self.photo_propre(conv), t)),
                        None => avatar(&nom, &conv.color, false, self.photo_propre(conv), 40.),
                    })
                    .when(self.en_ligne(conv), |d| d.child(presence::point_presence(40., fond)))
                    .child(div().absolute().right(px(-3.)).bottom(px(-2.)).child(logo(svc, 16.))),
            )
            .child(
                div()
                    .flex()
                    .flex_col()
                    .flex_1()
                    .min_w(px(0.))
                    .child(
                        div()
                            .flex()
                            .flex_row()
                            .items_center()
                            .gap(px(6.))
                            .child(
                                div()
                                    .truncate()
                                    .text_size(px(14.5))
                                    .font_weight(if conv.unread { FontWeight::BOLD } else { FontWeight::SEMIBOLD })
                                    .text_color(th().texte_fort)
                                    .child(texte_emoji(nom, th().texte_fort, None)),
                            )
                            .child(div().flex_none().text_size(px(11.5)).text_color(th().texte_pale).child(svc.court()))
                            .when(active, |d| {
                                d.child(
                                    div()
                                        .flex_none()
                                        .px(px(6.))
                                        .rounded(rayon(999.))
                                        .bg(alpha(th().accent, 0.15))
                                        .text_size(px(11.))
                                        .font_weight(FontWeight::SEMIBOLD)
                                        .text_color(th().accent)
                                        .child(tr("Ici", "Here")),
                                )
                            }),
                    )
                    .child(
                        div()
                            .flex()
                            .flex_row()
                            .gap(px(6.))
                            .text_size(px(12.5))
                            .text_color(if conv.unread { th().texte_fort } else { th().texte_pale })
                            .child(div().flex_1().min_w(px(0.)).truncate().child(texte_emoji(apercu, if conv.unread { th().texte_fort } else { th().texte_pale }, None)))
                            .when(self.en_sourdine(&conv.id), |d| d.child(Self::icone_sourdine(12., th().texte_pale)))
                            .when(conv.time > 0, |d| d.child(div().flex_none().child(heure(conv.time, true)))),
                    )
                    .when(detail.is_some() || boutons.is_some(), |d| {
                        d.child(
                            div()
                                .flex()
                                .flex_row()
                                .items_center()
                                .gap(px(6.))
                                .pt(px(2.))
                                .child(div().flex_1().min_w(px(0.)).truncate().text_size(px(11.)).text_color(th().texte_pale).children(detail))
                                .children(boutons),
                        )
                    }),
            )
            .when(conv.unread, |d| d.child(div().flex_none().size(px(10.)).when(!th().ega, |d| d.rounded(px(5.))).bg(th().accent)))
    }

    /// Les lignes de la fiche : « Même personne sur », les suggestions, « Lier… ».
    fn fiche_lignes(&self, groupe: &Groupe, nom: &str, cx: &mut Context<Self>) -> Div {
        let choisie = self.choisie.clone().unwrap_or_default();
        let mut col = div().flex().flex_col().gap(px(4.)).child(Self::titre_fiche(tr("Même personne sur", "Same person on")));
        let seule = groupe.membres.len() == 1;
        for (i, (id, lien)) in groupe.membres.iter().enumerate() {
            let Some(conv) = self.conv(id) else { continue };
            let detail = match lien {
                Lien::Ancre => None,
                Lien::Main => Some(tr("Lié à la main", "Linked by hand").to_string()),
                Lien::Numero => Some(tr("Même numéro", "Same number").to_string()),
            };
            let active = *id == choisie;
            let (a, b, c) = (id.clone(), id.clone(), id.clone());
            let mut boutons = div().flex_none().flex().flex_row().gap(px(6.));
            let mut des_boutons = false;
            // Offert sur toute ligne qui n'est pas déjà la principale : même avec un
            // nom pareil (« Genevieve » / « Geneviève »), la photo peut différer.
            let est_principale = self.reglages.personnes.iter().any(|p| p.principal == *id && p.convs.iter().any(|c| c == id));
            if !seule && !est_principale {
                des_boutons = true;
                boutons = boutons.child(Self::bouton_fiche(SharedString::from(format!("fiche-nom-{i}")), tr("Utiliser ce nom et cette photo", "Use this name & picture"), false).on_click(
                    cx.listener(move |this, _, _, cx| {
                        cx.stop_propagation();
                        this.fiche_principal(&b);
                        cx.notify();
                    }),
                ));
            }
            if *lien != Lien::Ancre {
                des_boutons = true;
                boutons = boutons.child(Self::bouton_fiche(SharedString::from(format!("fiche-delier-{i}")), tr("Délier", "Unlink"), false).on_click(
                    cx.listener(move |this, _, _, cx| {
                        cx.stop_propagation();
                        this.fiche_delier(&c);
                        cx.notify();
                    }),
                ));
            }
            col = col.child(
                div()
                    .id(SharedString::from(format!("fiche-ligne-{i}")))
                    .rounded(rayon(14.))
                    .cursor_pointer()
                    .hover(|s| s.bg(th().survol))
                    // « Changer de service » : ouvrir cette conversation.
                    .on_click(cx.listener(move |this, _, _, cx| {
                        this.fiche = None;
                        this.choisir(a.clone());
                        cx.notify();
                    }))
                    .child(self.ligne_conv_fiche(conv, active, detail, des_boutons.then_some(boutons))),
            );
        }
        if seule {
            col = col.child(
                div()
                    .px(px(10.))
                    .text_size(px(12.5))
                    .text_color(th().texte_pale)
                    .child(tr("Aucune autre conversation trouvée pour cette personne.", "No other conversation found for this person.")),
            );
        }
        // Les suggestions : « Est-ce la même personne ? Oui / Non ».
        if !groupe.suggestions.is_empty() {
            col = col.child(div().h(px(8.))).child(Self::titre_fiche(tr("Est-ce la même personne ?", "Is this the same person?")));
            for (i, (id, raison)) in groupe.suggestions.iter().enumerate() {
                let Some(conv) = self.conv(id) else { continue };
                let pourquoi = match raison {
                    Raison::Contact => tr("Nom d'un contact du téléphone qui a ce numéro", "Name of a phone contact with this number"),
                    Raison::Nom => tr("Nom très proche", "Very similar name"),
                };
                let (oui, non) = (id.clone(), id.clone());
                col = col.child(
                    div()
                        .flex()
                        .flex_col()
                        .gap(px(2.))
                        .rounded(rayon(14.))
                        .border_1()
                        .border_color(th().trait_fin)
                        .child(self.ligne_conv_fiche(
                            conv,
                            false,
                            Some(pourquoi.to_string()),
                            Some(
                            div()
                                .flex_none()
                                .flex()
                                .flex_row()
                                .gap(px(6.))
                                .child(Self::bouton_fiche(SharedString::from(format!("fiche-oui-{i}")), tr("Oui", "Yes"), true).on_click(cx.listener(
                                    move |this, _, _, cx| {
                                        this.fiche_lier(&oui);
                                        cx.notify();
                                    },
                                )))
                                .child(
                                    Self::bouton_fiche(SharedString::from(format!("fiche-non-{i}")), tr("Pas la même personne", "Not the same person"), false)
                                        .on_click(cx.listener(move |this, _, _, cx| {
                                            this.fiche_refuser(&non);
                                            cx.notify();
                                        })),
                                ),
                            ),
                        )),
                );
            }
        }
        col.child(div().h(px(8.))).child(
            div()
                .id("fiche-lier")
                .flex()
                .flex_row()
                .items_center()
                .gap(px(10.))
                .px(px(10.))
                .py(px(9.))
                .rounded(rayon(14.))
                .cursor_pointer()
                .text_color(th().accent)
                .font_weight(FontWeight::SEMIBOLD)
                .hover(|s| s.bg(th().survol))
                .on_click(cx.listener(|this, _, window, cx| {
                    if let Some(f) = this.fiche.as_mut() {
                        f.lier = true;
                        f.filtre = None;
                    }
                    this.fiche_recherche.update(cx, |s, cx| {
                        s.set_value("", window, cx);
                        s.focus(window, cx);
                    });
                    cx.notify();
                }))
                .child(icone("link", 17., th().accent))
                .child(tr("Lier une autre conversation…", "Link another conversation…")),
        )
        .child(self.fiche_sourdine(groupe, nom, cx))
    }

    /// « Mettre en sourdine… » / « Réactiver les notifications » : pour
    /// toutes les conversations de la personne (src/sourdine.rs).
    fn fiche_sourdine(&self, groupe: &Groupe, nom: &str, cx: &mut Context<Self>) -> impl IntoElement {
        let ids = groupe.ids();
        let (ic, texte) = self.entree_sourdine(&ids);
        let nom = nom.to_string();
        div()
            .id("fiche-sourdine")
            .flex()
            .flex_row()
            .items_center()
            .gap(px(10.))
            .px(px(10.))
            .py(px(9.))
            .rounded(rayon(14.))
            .cursor_pointer()
            .text_color(th().accent)
            .font_weight(FontWeight::SEMIBOLD)
            .hover(|s| s.bg(th().survol))
            .on_click(cx.listener(move |this, _, _, cx| {
                this.basculer_sourdine(ids.clone(), nom.clone());
                cx.notify();
            }))
            .child(icone(ic, 17., th().accent))
            .child(texte)
    }

    /// « Lier une autre conversation… » : une recherche, des puces par
    /// service, et les conversations à deux qui ne sont pas déjà sur la fiche.
    fn fiche_choix_lier(&self, groupe: &Groupe, cx: &mut Context<Self>) -> Div {
        let filtre = self.fiche.as_ref().and_then(|f| f.filtre);
        let q = plier(self.fiche_recherche.read(cx).value().trim());
        let ids = groupe.ids();
        let mut cands: Vec<&Conv> = Vec::new();
        let mut vus = HashSet::new();
        let prive = self.vue_privee();
        for c in self.convs.iter().chain(self.archives.iter()).chain(self.fantomes.iter()) {
            if c.group || c.large || ids.contains(&c.id) || !vus.insert(c.id.as_str()) || (!prive && self.privee(&c.id)) {
                continue;
            }
            cands.push(c);
        }
        let services: Vec<Service> = self.reglages.services_ordonnes().into_iter().filter(|s| cands.iter().any(|c| service_de(&c.id) == *s)).collect();
        let chiffres: String = q.chars().filter(char::is_ascii_digit).collect();
        let mut liste: Vec<&Conv> = cands
            .into_iter()
            .filter(|c| filtre.is_none_or(|s| service_de(&c.id) == s))
            .filter(|c| {
                q.is_empty()
                    || plier(&c.name).contains(&q)
                    || (chiffres.len() >= 3 && (c.number.contains(&chiffres) || c.id.contains(&chiffres)))
            })
            .collect();
        liste.sort_by(|a, b| b.time.cmp(&a.time));
        liste.truncate(40);

        let mut puces = div().flex().flex_row().flex_wrap().gap(px(4.)).child(
            nouveau::puce("fiche-tout", filtre.is_none(), true).child(t("Tout")).on_click(cx.listener(|this, _, _, cx| {
                if let Some(f) = this.fiche.as_mut() {
                    f.filtre = None;
                }
                cx.notify();
            })),
        );
        for s in services {
            let choisie = filtre == Some(s);
            let nom = s.court();
            puces = puces.child(
                nouveau::puce(SharedString::from(format!("fiche-{}", s.cle())), choisie, true)
                    .child(logo(s, 18.))
                    .when(choisie, |d| d.child(nom))
                    .when(!choisie, |d| {
                        d.w(px(28.)).px(px(0.)).justify_center().tooltip(move |w, cx| gpui_component::tooltip::Tooltip::new(nom).build(w, cx))
                    })
                    .on_click(cx.listener(move |this, _, _, cx| {
                        if let Some(f) = this.fiche.as_mut() {
                            f.filtre = Some(s);
                        }
                        cx.notify();
                    })),
            );
        }
        let mut col = div()
            .flex()
            .flex_col()
            .gap(px(8.))
            .child(
                div()
                    .flex()
                    .flex_row()
                    .items_center()
                    .gap(px(8.))
                    .child(
                        div()
                            .id("fiche-retour")
                            .size(px(28.))
                            .rounded(rayon(999.))
                            .flex()
                            .items_center()
                            .justify_center()
                            .cursor_pointer()
                            .hover(|s| s.bg(th().survol))
                            .on_click(cx.listener(|this, _, _, cx| {
                                if let Some(f) = this.fiche.as_mut() {
                                    f.lier = false;
                                }
                                cx.notify();
                            }))
                            .child(icone("chevron-left", 17., th().texte_fort)),
                    )
                    .child(div().font_weight(FontWeight::BOLD).text_color(th().texte_fort).child(tr("Lier une autre conversation…", "Link another conversation…"))),
            )
            .child(
                champ_surface()
                    .flex()
                    .flex_row()
                    .items_center()
                    .gap(px(8.))
                    .pl(px(12.))
                    .pr(px(6.))
                    .py(px(1.))
                    .child(icone("search", 15., th().texte_pale))
                    .child(div().flex_1().child(Input::new(&self.fiche_recherche).appearance(false).cleanable(true))),
            )
            .child(puces);
        if liste.is_empty() {
            col = col.child(div().px(px(10.)).py(px(8.)).text_size(px(13.)).text_color(th().texte_pale).child(tr("Aucune conversation", "No conversation")));
        }
        for (i, conv) in liste.into_iter().enumerate() {
            let id = conv.id.clone();
            col = col.child(
                div()
                    .id(SharedString::from(format!("fiche-choix-{i}")))
                    .rounded(rayon(14.))
                    .cursor_pointer()
                    .hover(|s| s.bg(th().survol))
                    .on_click(cx.listener(move |this, _, _, cx| {
                        this.fiche_lier(&id);
                        cx.notify();
                    }))
                    .child(self.ligne_conv_fiche(conv, false, None, None)),
            );
        }
        col
    }
}

/// Le texte de la pensée, émojis compris (police des émojis).
fn texto_emoji(texte: String) -> gpui::StyledText {
    texte_emoji(texte, th().texte_fort, None)
}

#[cfg(test)]
mod essais {
    use super::*;

    #[test]
    fn numeros_quebecois() {
        for n in ["(418) 555-1234", "418-555-1234", "418 555 1234", "+1 418 555 1234", "1-418-555-1234", "+14185551234", "1 (418) 555-1234", "001 418 555 1234"] {
            assert_eq!(e164(n).as_deref(), Some("+14185551234"), "{n}");
        }
        assert_eq!(e164("+33 6 12 34 56 78").as_deref(), Some("+33612345678"));
        assert_eq!(e164("555-12"), None);
        assert_eq!(numero_lisible("+14185551234"), "+1 418-555-1234");
        assert_eq!(numero_lisible("+33612345678"), "+33612345678");
        assert_eq!(e164("Mon Amour"), None);
        assert_eq!(e164("32665"), None);
    }

    #[test]
    fn numeros_des_services() {
        assert_eq!(numero_de("123", "(418) 555-1234", None, "Mon Amour").as_deref(), Some("+14185551234"));
        assert_eq!(numero_de("123", "", Some("418-555-1234"), "Mon Amour").as_deref(), Some("+14185551234"));
        assert_eq!(numero_de("123", "", None, "+1 418 555 1234").as_deref(), Some("+14185551234"));
        assert_eq!(numero_de("w:14185551234@s.whatsapp.net", "", None, "Steph").as_deref(), Some("+14185551234"));
        assert_eq!(numero_de("w:123456789012345@lid", "", None, "Steph"), None);
        assert_eq!(numero_de("w:1203630@g.us", "", None, "Groupe"), None);
        assert_eq!(numero_de("s:8f0c-uuid", "+14185551234", None, "S").as_deref(), Some("+14185551234"));
        assert_eq!(numero_de("t:u42", "+14185551234", None, "T").as_deref(), Some("+14185551234"));
        assert_eq!(numero_de("m:99", "+14185551234", None, "M"), None);
    }

    #[test]
    fn noms() {
        assert!(noms_proches("Stephanie Legendre", "Stéphanie Legendre"));
        assert!(noms_proches("Stephanie Legendre", "LEGENDRE, Stéphanie"));
        assert!(noms_proches("Steph Legendre", "Stéphanie Legendre"));
        assert!(noms_proches("Stephanie Legendre", "Stephanie Legendree"));
        assert!(!noms_proches("Stephanie Legendre", "Stephanie Tremblay"));
        assert!(!noms_proches("Mon Amour", "Stephanie Legendre"));
        assert!(!noms_proches("Al", "Al"));
        assert!(!noms_proches("418-555-1234", "418-555-1234"));
        assert!(noms_proches("Camille", "camille"));
        assert!(!noms_proches("Marc Roy", "Marc Rioux"));
    }

    fn cand(id: &str, nom: &str, numero: Option<&str>) -> Candidat {
        Candidat { id: id.into(), nom: nom.into(), numero: numero.map(Into::into) }
    }

    fn base() -> Vec<Candidat> {
        vec![
            cand("123", "Mon Amour", Some("+14185551234")),
            cand("w:14185551234@s.whatsapp.net", "Steph 💕", Some("+14185551234")),
            cand("m:99", "Stephanie Legendre", None),
            cand("i:42", "Stéphanie Legendre", None),
            cand("m:7", "Marc Tremblay", None),
        ]
    }

    #[test]
    fn regrouper_par_numero_et_suggestions() {
        let g = regrouper("123", &base(), &[], &[], &[]);
        assert_eq!(g.ids(), vec!["123".to_string(), "w:14185551234@s.whatsapp.net".into()]);
        assert_eq!(g.membres[1].1, Lien::Numero);
        // Aucun nom proche de « Mon Amour » ou « Steph 💕 » : pas de suggestion.
        assert!(g.suggestions.is_empty());
        // Un contact du téléphone « Stephanie Legendre » porte ce numéro : suggestion.
        let contacts = vec![("Stephanie Legendre".to_string(), "+14185551234".to_string())];
        let g = regrouper("123", &base(), &[], &[], &contacts);
        assert_eq!(g.suggestions, vec![("m:99".to_string(), Raison::Contact), ("i:42".to_string(), Raison::Contact)]);
        // Depuis Messenger : Instagram au nom très proche est suggéré.
        let g = regrouper("m:99", &base(), &[], &[], &[]);
        assert_eq!(g.ids(), vec!["m:99".to_string()]);
        assert_eq!(g.suggestions, vec![("i:42".to_string(), Raison::Nom)]);
    }

    #[test]
    fn lier_delier_refuser() {
        let cands = base();
        let (mut p, mut r) = (Vec::new(), Vec::new());
        // Lier Messenger à « Mon Amour » à la main.
        let g = regrouper("123", &cands, &p, &r, &[]).ids();
        lier(&mut p, &mut r, &g, "m:99", "p1".into());
        assert_eq!(p, vec![Personne { id: "p1".into(), convs: vec!["123".into(), "m:99".into()], principal: String::new() }]);
        let g = regrouper("m:99", &cands, &p, &r, &[]);
        // Depuis Messenger : Google (à la main), WhatsApp (même numéro que Google).
        assert_eq!(g.ids(), vec!["m:99".to_string(), "123".into(), "w:14185551234@s.whatsapp.net".into()]);
        assert_eq!(g.membres[1].1, Lien::Main);
        assert_eq!(g.membres[2].1, Lien::Numero);
        // Instagram au nom proche de Messenger : suggéré ; « Pas la même personne ».
        assert_eq!(g.suggestions, vec![("i:42".to_string(), Raison::Nom)]);
        refuser(&mut r, &g.ids(), "i:42");
        assert_eq!(r.len(), 3);
        assert!(regrouper("m:99", &cands, &p, &r, &[]).suggestions.is_empty());
        // Délier WhatsApp : le même numéro ne le ramène plus.
        let g = regrouper("123", &cands, &p, &r, &[]).ids();
        delier(&mut p, &mut r, &g, "w:14185551234@s.whatsapp.net");
        assert_eq!(regrouper("123", &cands, &p, &r, &[]).ids(), vec!["123".to_string(), "m:99".into()]);
        // Le relier à la main efface ses refus.
        let g = regrouper("123", &cands, &p, &r, &[]).ids();
        lier(&mut p, &mut r, &g, "w:14185551234@s.whatsapp.net", "p2".into());
        assert_eq!(p.len(), 1);
        assert_eq!(p[0].convs.len(), 3);
        assert_eq!(regrouper("w:14185551234@s.whatsapp.net", &cands, &p, &r, &[]).ids().len(), 3);
        // Délier Messenger, la personne garde Google et WhatsApp.
        let g = regrouper("123", &cands, &p, &r, &[]).ids();
        delier(&mut p, &mut r, &g, "m:99");
        assert_eq!(p[0].convs, vec!["123".to_string(), "w:14185551234@s.whatsapp.net".into()]);
        assert!(!regrouper("123", &cands, &p, &r, &[]).ids().contains(&"m:99".to_string()));
    }

    #[test]
    fn principal_affiche() {
        let w = "w:14185551234@s.whatsapp.net";
        let mut p = vec![Personne { id: "p1".into(), convs: vec!["105".into(), w.into(), "m:2005".into()], principal: String::new() }];
        // Sans principal : chacune garde le sien.
        assert_eq!(principal_de(&p, w), None);
        p[0].principal = "105".into();
        assert_eq!(principal_de(&p, w), Some("105"));
        assert_eq!(principal_de(&p, "m:2005"), Some("105"));
        assert_eq!(principal_de(&p, "105"), None);
        assert_eq!(principal_de(&p, "i:3005"), None);
        // Délier rend à la conversation son nom et sa photo.
        let mut refus = Vec::new();
        let g: Vec<String> = p[0].convs.clone();
        delier(&mut p, &mut refus, &g, "m:2005");
        assert_eq!(principal_de(&p, "m:2005"), None);
        assert_eq!(principal_de(&p, w), Some("105"));
        // La principale déliée : plus de principal pour les autres.
        delier(&mut p, &mut refus, &g, "105");
        assert_eq!(principal_de(&p, w), None);
    }

    #[test]
    fn nom_principal() {
        let cands = base();
        let mut p = Vec::new();
        // Pas encore de personne : elle est créée avec la fiche (Google + WhatsApp).
        let g = regrouper("123", &cands, &p, &[], &[]).ids();
        choisir_principal(&mut p, &g, "w:14185551234@s.whatsapp.net", "p1".into());
        assert_eq!(p[0].principal, "w:14185551234@s.whatsapp.net");
        assert_eq!(p[0].convs.len(), 2);
    }

    #[test]
    fn contacts_perso_cle_et_application() {
        let w = "w:14185551234@s.whatsapp.net";
        let mut perso: BTreeMap<String, ContactPerso> = BTreeMap::new();
        let mut p = vec![Personne { id: "p1".into(), convs: vec!["123".into(), w.into(), "m:9".into()], principal: w.into() }];
        let g: Vec<String> = vec!["123".into(), w.into()];
        // Rien encore : la clé est la principale.
        assert!(contact_perso_de(&perso, &p, "123").is_none());
        assert_eq!(cle_contact_perso(&perso, &p, &g), w);
        // Posée sur la principale : vaut pour toutes les conversations de la personne.
        poser_contact_perso(&mut perso, w, "  Maman ", None, false);
        assert_eq!(perso[w].nom, "Maman");
        for id in ["123", w, "m:9"] {
            assert_eq!(contact_perso_de(&perso, &p, id).map(|(k, _)| k), Some(w), "{id}");
        }
        // Pas pour une conversation d'une autre personne.
        assert!(contact_perso_de(&perso, &p, "i:4").is_none());
        // Une entrée sur la conversation elle-même l'emporte.
        perso.insert("m:9".into(), ContactPerso { nom: "Steph".into(), ..Default::default() });
        assert_eq!(contact_perso_de(&perso, &p, "m:9").map(|(_, c)| c.nom.as_str()), Some("Steph"));
        perso.remove("m:9");
        // La clé existante est gardée, même ouverte depuis une autre conversation.
        assert_eq!(cle_contact_perso(&perso, &p, &["m:9".to_string()]), w);
        // Une photo : le nom reste ; vider le nom garde la photo ; sans rien, l'entrée part.
        poser_contact_perso(&mut perso, w, "Maman", Some("/m/contacts/a.jpg".into()), true);
        poser_contact_perso(&mut perso, w, "", None, true);
        assert_eq!((perso[w].nom.as_str(), perso[w].photo.as_str(), perso[w].google), ("", "/m/contacts/a.jpg", true));
        poser_contact_perso(&mut perso, w, "", Some(String::new()), false);
        assert!(perso.is_empty());
        // Sans personne ni principale : l'ancre.
        p.clear();
        assert_eq!(cle_contact_perso(&perso, &p, &g), "123");
    }

    #[test]
    fn relier_la_fiche() {
        let cands = base();
        let mut p = Vec::new();
        // Même numéro (Google + WhatsApp) : reliées à la main, sans principale.
        let g = regrouper("123", &cands, &p, &[], &[]).ids();
        relier_fiche(&mut p, &g, "p1".into());
        assert_eq!(p.len(), 1);
        assert_eq!(p[0].convs, g);
        assert_eq!(p[0].principal, "");
        // Seule : rien à relier.
        let mut q = Vec::new();
        relier_fiche(&mut q, &["m:9".to_string()], "p2".into());
        assert!(q.is_empty());
        // Une personne existante gagne les conversations libres de la fiche.
        let mut r = vec![Personne { id: "p3".into(), convs: vec!["123".into(), "m:7".into()], principal: "m:7".into() }];
        relier_fiche(&mut r, &g, "p4".into());
        assert_eq!(r.len(), 1);
        assert_eq!(r[0].convs.len(), 3);
        assert_eq!(r[0].principal, "m:7");
    }

    #[test]
    fn photo_recadree() {
        let dir = std::path::Path::new(env!("CARGO_MANIFEST_DIR")).join(format!(".cache/essai-photo-{}", std::process::id()));
        let _ = std::fs::create_dir_all(&dir);
        let src = dir.join("large.png");
        image::RgbImage::from_fn(900, 600, |x, _| if x < 150 || x >= 750 { image::Rgb([255, 0, 0]) } else { image::Rgb([0, 0, 255]) })
            .save(&src)
            .unwrap();
        let p = photo_carree(&src, &dir.join("contacts")).expect("photo");
        let img = image::open(&p).unwrap();
        assert_eq!((img.width(), img.height()), (512, 512));
        // Le centre : les bandes rouges des côtés sont coupées.
        let px = img.to_rgb8();
        assert!(px.get_pixel(2, 256)[2] > 200 && px.get_pixel(509, 256)[2] > 200);
        // Petite : gardée à sa taille, carrée.
        let petite = dir.join("petite.png");
        image::RgbImage::new(100, 300).save(&petite).unwrap();
        let q = image::open(photo_carree(&petite, &dir.join("contacts")).unwrap()).unwrap();
        assert_eq!((q.width(), q.height()), (100, 100));
        assert!(photo_carree(&dir.join("absente.png"), &dir).is_err());
        let _ = std::fs::remove_dir_all(&dir);
    }

    #[test]
    fn refus_l_emporte_sur_le_numero() {
        let cands = base();
        let r = vec![("123".to_string(), "w:14185551234@s.whatsapp.net".to_string())];
        assert_eq!(regrouper("123", &cands, &[], &r, &[]).ids(), vec!["123".to_string()]);
    }
}
