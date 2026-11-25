//! Le cache local du bureau : de quoi tout montrer avant que les ponts (ou
//! le serveur Jasette) aient répondu.
//!
//! Un seul fichier, `<données>/cache-bureau.json` (0600, écrit par
//! renommage, hors du fil de l'interface) : la dernière liste de
//! conversations de chaque service (et des archives), les derniers messages
//! des conversations ouvertes ou reçues, et où sont déjà les pièces jointes
//! et les photos téléchargées (seulement les fichiers qui existent encore).
//! Borné : CONVS_MAX conversations par service dans chaque liste, FILS_MAX
//! fils de MSGS_MAX messages. Le même cache sert au mode autonome (ponts
//! locaux) et au mode client ; `source` dit d'où il vient (un cache d'une
//! autre source est ignoré).

use std::collections::{HashMap, HashSet};
use std::path::PathBuf;
use std::sync::Mutex;

use serde::{Deserialize, Serialize};

use super::{Conv, Msg, Service, dossier_donnees, ecrire_atomique, service_de};

/// Conversations gardées par service, dans chaque liste.
pub const CONVS_MAX: usize = 300;
/// Fils (conversations) dont on garde les messages.
pub const FILS_MAX: usize = 200;
/// Messages gardés par fil.
pub const MSGS_MAX: usize = 60;

#[derive(Default, Serialize, Deserialize)]
pub struct CacheLocal {
    #[serde(default)]
    pub source: String,
    #[serde(default)]
    pub convs: Vec<Conv>,
    #[serde(default)]
    pub archives: Vec<Conv>,
    #[serde(default)]
    pub fils: HashMap<String, Fil>,
    #[serde(default)]
    pub medias: HashMap<String, PathBuf>,
    #[serde(default)]
    pub avatars: HashMap<String, PathBuf>,
}

#[derive(Default, Serialize, Deserialize)]
pub struct Fil {
    #[serde(default)]
    pub more: bool,
    #[serde(default)]
    pub items: Vec<Msg>,
}

fn chemin() -> PathBuf {
    dossier_donnees().join("cache-bureau.json")
}

impl CacheLocal {
    /// Le cache de cette source (vide s'il manque, est illisible ou vient
    /// d'ailleurs). Les fichiers disparus sont oubliés.
    pub fn charger(source: &str) -> Self {
        let Ok(texte) = std::fs::read_to_string(chemin()) else { return Self::default() };
        let mut c: Self = match serde_json::from_str(&texte) {
            Ok(c) => c,
            Err(e) => {
                eprintln!("jasette : cache-bureau.json illisible ({e}) : ignoré");
                return Self::default();
            }
        };
        if c.source != source {
            return Self::default();
        }
        c.medias.retain(|_, p| p.is_file());
        c.avatars.retain(|_, p| p.is_file());
        c
    }

    /// Fait le cache à partir de l'état de l'interface, borné.
    pub fn faire(
        source: &str,
        convs: &[Conv],
        archives: &[Conv],
        messages: &HashMap<String, Vec<Msg>>,
        plus_anciens: &HashMap<String, bool>,
        medias: &HashMap<String, PathBuf>,
        avatars: &HashMap<String, PathBuf>,
    ) -> Self {
        let borner = |l: &[Conv]| -> Vec<Conv> {
            let mut par_service: HashMap<Service, usize> = HashMap::new();
            l.iter()
                .filter(|c| {
                    let n = par_service.entry(service_de(&c.id)).or_default();
                    *n += 1;
                    *n <= CONVS_MAX
                })
                .cloned()
                .collect()
        };
        // Les fils les plus récents (dernier message) d'abord.
        let mut ids: Vec<(&String, i64)> = messages
            .iter()
            .filter(|(_, l)| l.iter().any(|m| !m.local()))
            .map(|(k, l)| (k, l.iter().map(|m| m.time).max().unwrap_or(0)))
            .collect();
        ids.sort_by(|a, b| b.1.cmp(&a.1));
        ids.truncate(FILS_MAX);
        let mut fils = HashMap::new();
        let mut pieces = HashSet::new();
        for (id, _) in ids {
            let l: Vec<Msg> = messages[id].iter().filter(|m| !m.local()).cloned().collect();
            let coupe = l.len() > MSGS_MAX;
            let items: Vec<Msg> = l[l.len().saturating_sub(MSGS_MAX)..].to_vec();
            for p in items.iter().flat_map(|m| m.attachments.iter()) {
                pieces.insert(p.media_id.clone());
            }
            let more = coupe || plus_anciens.get(id).copied().unwrap_or(false);
            fils.insert(id.clone(), Fil { more, items });
        }
        let convs = borner(convs);
        let archives = borner(archives);
        let ids_convs: HashSet<&str> = convs.iter().chain(archives.iter()).map(|c| c.id.as_str()).collect();
        Self {
            source: source.to_string(),
            medias: medias.iter().filter(|(k, _)| pieces.contains(*k)).map(|(k, v)| (k.clone(), v.clone())).collect(),
            avatars: avatars
                .iter()
                .filter(|(k, _)| ids_convs.contains(k.as_str()))
                .map(|(k, v)| (k.clone(), v.clone()))
                .collect(),
            convs,
            archives,
            fils,
        }
    }

    /// Écrit le cache dans un fil à part (une écriture à la fois).
    pub fn garder(self) {
        let Ok(texte) = serde_json::to_vec(&self) else { return };
        static ECRITURE: Mutex<()> = Mutex::new(());
        let _ = std::thread::Builder::new().name("cache".into()).spawn(move || {
            let _verrou = ECRITURE.lock();
            let dir = dossier_donnees();
            let _ = std::fs::create_dir_all(&dir);
            if let Err(e) = ecrire_atomique(&chemin(), &texte) {
                eprintln!("jasette : cache-bureau.json non écrit : {e}");
            }
        });
    }
}

impl Msg {
    /// Un envoi local pas encore confirmé par le pont.
    pub fn local(&self) -> bool {
        !self.tmp_id.is_empty() && self.id == self.tmp_id
    }
}

/// La page la plus récente d'un fil (`page`, réponse fraîche du pont) fusionnée
/// avec ce qu'on avait (`anc`) : la page fait foi dans sa plage (un message
/// qui n'y est plus a été retiré) ; on garde de `anc` les envois locaux pas
/// encore confirmés, ce qui est plus récent que la page (arrivé entre-temps)
/// et, si la page touche `anc` (son plus vieux message y est), ce qui est plus
/// vieux qu'elle (les pages déjà remontées). Sans doublon d'id.
pub fn fusionner_page(anc: &[Msg], page: Vec<Msg>) -> Vec<Msg> {
    let ids: HashSet<&str> = page.iter().map(|m| m.id.as_str()).collect();
    let tmps: HashSet<&str> = page.iter().map(|m| m.tmp_id.as_str()).filter(|t| !t.is_empty()).collect();
    let premier = page.iter().map(|m| m.time).min();
    let dernier = page.iter().map(|m| m.time).max();
    let touche = page.iter().min_by_key(|m| m.time).is_some_and(|p| anc.iter().any(|m| m.id == p.id));
    let mut garde: Vec<Msg> = anc
        .iter()
        .filter(|m| !ids.contains(m.id.as_str()) && !(!m.tmp_id.is_empty() && tmps.contains(m.tmp_id.as_str())))
        .filter(|m| {
            m.local()
                || dernier.is_some_and(|d| m.time > d)
                || (touche && premier.is_some_and(|p| m.time < p))
        })
        .cloned()
        .collect();
    let mut r = page;
    let mut vus: HashSet<String> = HashSet::new();
    r.retain(|m| vus.insert(m.id.clone()));
    r.append(&mut garde);
    r.sort_by_key(|m| m.time);
    r
}

/// Une copie (la réponse `"cache":true` du serveur) ajoutée à ce qu'on a :
/// par id, la copie remplace le message qu'on avait ; rien n'est retiré.
pub fn fusionner_copie(anc: &[Msg], copie: Vec<Msg>) -> Vec<Msg> {
    let mut r: Vec<Msg> = anc.to_vec();
    for m in copie {
        match r.iter().position(|x| x.id == m.id || (!m.tmp_id.is_empty() && x.tmp_id == m.tmp_id)) {
            Some(i) => r[i] = m,
            None => r.push(m),
        }
    }
    r.sort_by_key(|m| m.time);
    r
}

#[cfg(test)]
mod essais {
    use super::*;
    use serde_json::json;

    fn m(id: &str, t: i64) -> Msg {
        serde_json::from_value(json!({"id": id, "conv": "c", "time": t, "text": id})).unwrap()
    }

    fn ids(l: &[Msg]) -> Vec<&str> {
        l.iter().map(|m| m.id.as_str()).collect()
    }

    #[test]
    fn page_puis_copie_sans_doublon() {
        // Le cache du serveur arrive d'abord, puis la réponse du pont.
        let copie = vec![m("a", 1), m("b", 2), m("c", 3)];
        let l = fusionner_copie(&[], copie);
        let l = fusionner_page(&l, vec![m("b", 2), m("c", 3), m("d", 4)]);
        assert_eq!(ids(&l), ["a", "b", "c", "d"]);
        // La copie, de nouveau : rien ne double.
        let l = fusionner_copie(&l, vec![m("c", 3), m("d", 4)]);
        assert_eq!(ids(&l), ["a", "b", "c", "d"]);
    }

    #[test]
    fn page_fait_foi_dans_sa_plage() {
        // « b » a été retiré ; « e » est arrivé pendant la requête ; un envoi local attend.
        let mut local = m("tmp1", 9);
        local.tmp_id = "tmp1".into();
        let anc = vec![m("a", 1), m("b", 2), m("c", 3), m("e", 8), local];
        let l = fusionner_page(&anc, vec![m("a", 1), m("c", 3), m("d", 4)]);
        assert_eq!(ids(&l), ["a", "c", "d", "e", "tmp1"]);
    }

    #[test]
    fn page_sans_lien_remplace() {
        // Un vieux cache (disque) qui ne touche pas la page : pas de trou.
        let anc = vec![m("v1", 1), m("v2", 2)];
        let l = fusionner_page(&anc, vec![m("n1", 100), m("n2", 101)]);
        assert_eq!(ids(&l), ["n1", "n2"]);
        // Une page qui touche : les messages plus vieux remontés restent.
        let anc = vec![m("v1", 1), m("v2", 2), m("n1", 100)];
        let l = fusionner_page(&anc, vec![m("n1", 100), m("n2", 101)]);
        assert_eq!(ids(&l), ["v1", "v2", "n1", "n2"]);
    }

    #[test]
    fn envoi_confirme_remplace_le_local() {
        let mut local = m("tmp1", 5);
        local.tmp_id = "tmp1".into();
        let mut confirme = m("vrai", 5);
        confirme.tmp_id = "tmp1".into();
        let l = fusionner_page(&[m("a", 1), local], vec![m("a", 1), confirme]);
        assert_eq!(ids(&l), ["a", "vrai"]);
    }

    #[test]
    fn faire_borne_et_garde_les_pieces() {
        let mut messages = HashMap::new();
        for f in 0..(FILS_MAX + 10) {
            let l: Vec<Msg> = (0..(MSGS_MAX as i64 + 5)).map(|i| m(&format!("{f}-{i}"), f as i64 * 1000 + i)).collect();
            messages.insert(format!("f{f}"), l);
        }
        let c = CacheLocal::faire("local", &[], &[], &messages, &HashMap::new(), &HashMap::new(), &HashMap::new());
        assert_eq!(c.fils.len(), FILS_MAX);
        // Les plus récents sont gardés, chacun borné, avec « plus anciens ».
        assert!(c.fils.contains_key(&format!("f{}", FILS_MAX + 9)));
        assert!(!c.fils.contains_key("f0"));
        let f = &c.fils[&format!("f{}", FILS_MAX + 9)];
        assert_eq!(f.items.len(), MSGS_MAX);
        assert!(f.more);
        let texte = serde_json::to_string(&c).unwrap();
        let relu: CacheLocal = serde_json::from_str(&texte).unwrap();
        assert_eq!(relu.fils.len(), FILS_MAX);
    }
}
