// « Ce que j'écoute » (serveur/PROTOCOLE.md, §5.12) : la note musicale.
//
// Aucune API Spotify : le lecteur local, par MPRIS sur D-Bus
// (`org.mpris.MediaPlayer2.spotify`), sondé toutes les 30 s par un fil à part,
// seulement si le partage est actif sur cet appareil. Le moteur (pur, testé)
// décide quand publier : au plus une fois toutes les 15 minutes, seulement si
// le texte change ; l'artiste (ou le genre) retenu est celui qui a joué le
// plus longtemps pendant la période ; effacement une seule fois, après
// 20 minutes sans lecture. Le genre vient de MusicBrainz (format « genre »
// seulement), une requête par seconde au plus, avec un cache local.
use std::collections::HashMap;
use std::path::PathBuf;
use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::{Arc, Mutex};
use std::time::{Duration, Instant};

use serde_json::Value;

/// Au plus une publication musicale par période de 15 minutes.
pub const PERIODE_MS: i64 = 15 * 60_000;
/// La note musicale s'efface après 20 minutes sans lecture.
pub const SILENCE_MS: i64 = 20 * 60_000;
/// Un écart entre deux sondages plus long ne compte pas en entier.
const ECART_MAX_MS: i64 = 90_000;
/// On attend le genre au plus autant avant de publier sans lui.
const ATTENTE_GENRE_MS: i64 = 30_000;

/// Ce que joue Spotify.
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct Lecture {
    pub titre: String,
    pub artiste: String,
    /// Un balado : le nom de l'émission (sinon vide).
    pub emission: String,
    pub balado: bool,
    pub joue: bool,
}

/// Le genre d'un artiste : pas encore connu (requête en cours), ou connu (peut-être aucun).
#[derive(Clone, Debug, PartialEq, Eq)]
pub enum Genre {
    Attente,
    Connu(Option<String>),
}

/// Ce que le moteur demande.
#[derive(Clone, Debug, PartialEq, Eq)]
pub enum Action {
    Publier(String),
    Effacer,
}

/// Ce qui a joué pendant la période : une clé (artiste, émission ou titre) et sa durée.
#[derive(Clone, Debug, PartialEq, Eq)]
struct Joue {
    lecture: Lecture,
    ms: i64,
}

#[derive(Debug, Default)]
pub struct Moteur {
    /// Début de la période en cours (ms) ; 0 : aucune.
    debut: i64,
    /// La dernière publication musicale (ms) et son texte.
    publie_a: i64,
    publie: Option<String>,
    /// Dernier sondage, dernière lecture entendue (ms).
    dernier_sondage: i64,
    derniere_lecture: i64,
    /// La note musicale a déjà été effacée après le silence.
    efface: bool,
    periode: Vec<(String, Joue)>,
}

impl Moteur {
    /// Un sondage, à l'heure `maintenant` (ms). `musicale` : la note actuelle
    /// (sur un des services) vient de la musique. `format` : "artiste" (ou
    /// vide), "genre" ou "titre". `limite` : la longueur maximale du texte.
    pub fn sondage(
        &mut self,
        maintenant: i64,
        lecture: Option<&Lecture>,
        musicale: bool,
        format: &str,
        anglais: bool,
        limite: usize,
        genre: &dyn Fn(&str) -> Genre,
    ) -> Option<Action> {
        let ecart = if self.dernier_sondage > 0 { (maintenant - self.dernier_sondage).clamp(0, ECART_MAX_MS) } else { 0 };
        self.dernier_sondage = maintenant;
        if let Some(l) = lecture.filter(|l| l.joue && !(l.artiste.is_empty() && l.titre.is_empty() && l.emission.is_empty())) {
            self.derniere_lecture = maintenant;
            if self.debut == 0 {
                self.debut = maintenant;
            }
            let cle = cle_de(l, format);
            match self.periode.iter_mut().find(|(c, _)| *c == cle) {
                Some((_, j)) => {
                    j.ms += ecart;
                    j.lecture = l.clone();
                }
                None => self.periode.push((cle, Joue { lecture: l.clone(), ms: ecart })),
            }
            // Première publication tout de suite ; ensuite, à la fin de chaque période.
            // (Après un effacement, la musique qui reprend se publie aussi tout de suite.)
            let due = self.publie_a == 0 || maintenant - self.publie_a >= PERIODE_MS;
            if !due || (self.publie.is_some() && maintenant - self.debut < PERIODE_MS) {
                return None;
            }
            // Ce qui a joué le plus longtemps (à égalité : le plus récent).
            let gagnant = self.periode.iter().rev().max_by_key(|(_, j)| j.ms).map(|(_, j)| j.lecture.clone())?;
            let g = if format == "genre" && !gagnant.balado {
                match genre(&gagnant.artiste) {
                    Genre::Attente if maintenant - self.debut < ATTENTE_GENRE_MS => return None,
                    Genre::Attente => None,
                    Genre::Connu(g) => g,
                }
            } else {
                None
            };
            let texte = tronquer(&texte_note(&gagnant, format, g.as_deref(), anglais), limite);
            // La période est jugée : la suivante commence.
            self.periode.clear();
            self.debut = maintenant;
            if musicale && self.publie.as_deref() == Some(texte.as_str()) {
                return None;
            }
            self.publie_a = maintenant;
            self.publie = Some(texte.clone());
            self.efface = false;
            return Some(Action::Publier(texte));
        }
        // Rien ne joue : une pause n'efface rien, 20 minutes de silence si.
        if self.publie.is_some() && !self.efface && self.derniere_lecture > 0 && maintenant - self.derniere_lecture >= SILENCE_MS {
            self.efface = true;
            self.publie = None;
            self.periode.clear();
            self.debut = 0;
            return Some(Action::Effacer);
        }
        None
    }
}

fn cle_de(l: &Lecture, format: &str) -> String {
    match format {
        "titre" => format!("{}\u{1}{}", l.titre, if l.balado { &l.emission } else { &l.artiste }),
        _ if l.balado => format!("\u{2}{}", if l.emission.is_empty() { &l.artiste } else { &l.emission }),
        _ => l.artiste.clone(),
    }
}

/// Le texte de la note, avant troncature.
pub fn texte_note(l: &Lecture, format: &str, genre: Option<&str>, anglais: bool) -> String {
    let emission = if l.emission.is_empty() { &l.artiste } else { &l.emission };
    let ecoute = if anglais { "Listening to" } else { "Écoute" };
    match (format, l.balado) {
        ("titre", true) => format!("🎙️ {} — {emission}", l.titre),
        ("titre", false) => format!("🎵 {} — {}", l.titre, l.artiste),
        ("genre", true) => if anglais { "🎙️ Listening to a podcast".into() } else { "🎙️ Écoute un podcast".into() },
        ("genre", false) if genre.is_some() => {
            let g = genre.unwrap_or_default();
            if anglais { format!("🎵 Listening to {}!", genre_anglais(g)) } else { format!("🎵 Écoute {} !", genre_francais(g)) }
        }
        (_, true) => format!("🎙️ {ecoute} {emission}"),
        _ => format!("🎵 {ecoute} {}", l.artiste),
    }
}

/// Tronque à `max` caractères (points de code), avec « … ».
pub fn tronquer(s: &str, max: usize) -> String {
    if s.chars().count() <= max || max == 0 {
        return s.to_string();
    }
    let mut r: String = s.chars().take(max.saturating_sub(1)).collect::<String>().trim_end().to_string();
    r.push('…');
    r
}

/// Les genres courants (étiquettes MusicBrainz) : clé, en français avec
/// l'article, en anglais.
const GENRES: [(&str, &str, &str); 24] = [
    ("rock", "du rock", "rock"),
    ("pop", "de la pop", "pop"),
    ("hip hop", "du hip-hop", "hip-hop"),
    ("hip-hop", "du hip-hop", "hip-hop"),
    ("rap", "du rap", "rap"),
    ("jazz", "du jazz", "jazz"),
    ("electronic", "de l'électro", "electronic music"),
    ("electro", "de l'électro", "electro"),
    ("techno", "de la techno", "techno"),
    ("house", "de la house", "house"),
    ("classical", "de la musique classique", "classical music"),
    ("metal", "du métal", "metal"),
    ("heavy metal", "du métal", "heavy metal"),
    ("country", "du country", "country"),
    ("rnb", "du R&B", "R&B"),
    ("r&b", "du R&B", "R&B"),
    ("folk", "du folk", "folk"),
    ("punk", "du punk", "punk"),
    ("blues", "du blues", "blues"),
    ("reggae", "du reggae", "reggae"),
    ("soul", "du soul", "soul"),
    ("funk", "du funk", "funk"),
    ("indie", "de l'indie", "indie"),
    ("alternative rock", "du rock alternatif", "alternative rock"),
];

fn genre_francais(g: &str) -> String {
    let b = g.to_lowercase();
    if let Some((_, fr, _)) = GENRES.iter().find(|(k, _, _)| *k == b) {
        return fr.to_string();
    }
    // Un genre rare : tel quel, avec l'article.
    if b.starts_with(['a', 'e', 'é', 'i', 'o', 'u', 'h']) { format!("de l'{g}") } else { format!("du {g}") }
}

fn genre_anglais(g: &str) -> String {
    let b = g.to_lowercase();
    GENRES.iter().find(|(k, _, _)| *k == b).map(|(_, _, en)| en.to_string()).unwrap_or_else(|| g.to_string())
}

/// Le genre d'une réponse MusicBrainz (`/ws/2/artist/?query=…&fmt=json`) :
/// l'étiquette la plus votée du premier artiste.
pub fn genre_musicbrainz(v: &Value) -> Option<String> {
    let tags = v.get("artists")?.as_array()?.first()?.get("tags")?.as_array()?;
    tags.iter()
        .filter_map(|t| Some((t.get("count").and_then(Value::as_i64).unwrap_or(0), t.get("name")?.as_str()?)))
        .filter(|(_, n)| !n.trim().is_empty())
        .max_by_key(|(c, _)| *c)
        .map(|(_, n)| n.trim().to_string())
}

// ---------------------------------------------------------------------------
// Le lecteur (MPRIS) et MusicBrainz, hors du fil de l'interface
// ---------------------------------------------------------------------------

/// Ce que le fil de la musique partage avec l'interface.
#[derive(Default)]
pub struct Lecteur {
    /// Sonder MPRIS (le partage est actif sur cet appareil).
    pub actif: AtomicBool,
    /// Le dernier sondage : rien (Spotify absent ou arrêté), ou ce qui joue.
    pub derniere: Mutex<Option<Option<Lecture>>>,
    /// Les genres connus (artiste → genre) et les artistes à chercher.
    genres: Mutex<HashMap<String, Option<String>>>,
    a_chercher: Mutex<Vec<String>>,
    lance: AtomicBool,
}

impl Lecteur {
    /// Lance le fil (une seule fois) ; il ne fait rien tant que `actif` est faux.
    pub fn demarrer(self: &Arc<Self>, cache_genres: PathBuf) {
        if self.lance.swap(true, Ordering::SeqCst) {
            return;
        }
        if let Ok(t) = std::fs::read_to_string(&cache_genres)
            && let Ok(m) = serde_json::from_str::<HashMap<String, Option<String>>>(&t)
            && let Ok(mut g) = self.genres.lock()
        {
            *g = m;
        }
        let moi = self.clone();
        let _ = std::thread::Builder::new().name("musique".into()).spawn(move || moi.boucle(cache_genres));
    }

    /// Le genre d'un artiste ; inconnu, il est mis en file pour MusicBrainz.
    pub fn genre(&self, artiste: &str) -> Genre {
        let cle = artiste.trim().to_lowercase();
        if let Some(g) = self.genres.lock().ok().and_then(|g| g.get(&cle).cloned()) {
            return Genre::Connu(g);
        }
        if let Ok(mut f) = self.a_chercher.lock()
            && !f.contains(&cle)
        {
            f.push(cle);
        }
        Genre::Attente
    }

    fn boucle(&self, cache_genres: PathBuf) {
        let mut connexion: Option<zbus::blocking::Connection> = None;
        let mut prochain_sondage = Instant::now();
        let mut derniere_requete: Option<Instant> = None;
        loop {
            std::thread::sleep(Duration::from_millis(500));
            if !self.actif.load(Ordering::Relaxed) {
                continue;
            }
            // MusicBrainz : un artiste à la fois, une requête par seconde au plus.
            let artiste = self.a_chercher.lock().ok().and_then(|mut f| (!f.is_empty()).then(|| f.remove(0)));
            if let Some(a) = artiste {
                if let Some(t) = derniere_requete {
                    let ecoule = t.elapsed();
                    if ecoule < Duration::from_secs(1) {
                        std::thread::sleep(Duration::from_secs(1) - ecoule);
                    }
                }
                derniere_requete = Some(Instant::now());
                let g = chercher_genre(&a);
                if let Ok(mut m) = self.genres.lock() {
                    m.insert(a, g);
                    if let Ok(t) = serde_json::to_string(&*m) {
                        let _ = std::fs::write(&cache_genres, t);
                    }
                }
            }
            if Instant::now() < prochain_sondage {
                continue;
            }
            prochain_sondage = Instant::now() + Duration::from_secs(30);
            if connexion.is_none() {
                connexion = zbus::blocking::Connection::session().ok();
            }
            let lu = match connexion.as_ref().map(lire_mpris) {
                Some(Ok(l)) => l,
                Some(Err(Absent::Bus)) => {
                    connexion = None;
                    None
                }
                _ => None,
            };
            if let Ok(mut d) = self.derniere.lock() {
                *d = Some(lu);
            }
        }
    }
}

enum Absent {
    /// Spotify n'est pas lancé.
    Lecteur,
    /// Le bus lui-même ne répond plus.
    Bus,
}

/// Ce que joue Spotify, par MPRIS ; Ok(None) : rien.
fn lire_mpris(c: &zbus::blocking::Connection) -> Result<Option<Lecture>, Absent> {
    use zbus::zvariant::OwnedValue;
    let p = zbus::blocking::Proxy::new(c, "org.mpris.MediaPlayer2.spotify", "/org/mpris/MediaPlayer2", "org.mpris.MediaPlayer2.Player")
        .map_err(|_| Absent::Bus)?;
    let statut: String = match p.get_property("PlaybackStatus") {
        Ok(s) => s,
        Err(zbus::Error::MethodError(..)) | Err(zbus::Error::FDO(_)) => return Err(Absent::Lecteur),
        Err(_) => return Err(Absent::Bus),
    };
    let meta: HashMap<String, OwnedValue> = p.get_property("Metadata").map_err(|_| Absent::Lecteur)?;
    let texte = |k: &str| -> String {
        meta.get(k).and_then(|v| String::try_from(v.try_clone().ok()?).ok()).unwrap_or_default()
    };
    let artistes: Vec<String> =
        meta.get("xesam:artist").and_then(|v| Vec::<String>::try_from(v.try_clone().ok()?).ok()).unwrap_or_default();
    // mpris:trackid est un chemin d'objet (parfois une chaîne).
    let piste = meta
        .get("mpris:trackid")
        .and_then(|v| {
            let v = v.try_clone().ok()?;
            zbus::zvariant::OwnedObjectPath::try_from(v.try_clone().ok()?)
                .map(|o| o.as_str().to_string())
                .ok()
                .or_else(|| String::try_from(v).ok())
        })
        .unwrap_or_default();
    Ok(Some(lecture_de(&statut, &texte("xesam:title"), &artistes, &texte("xesam:album"), &piste)))
}

/// Une lecture à partir des champs MPRIS.
pub fn lecture_de(statut: &str, titre: &str, artistes: &[String], album: &str, piste: &str) -> Lecture {
    let balado = piste.contains("spotify:episode:") || piste.contains("/episode/");
    let artiste = artistes.iter().filter(|a| !a.trim().is_empty()).cloned().collect::<Vec<_>>().join(", ");
    Lecture {
        titre: titre.trim().to_string(),
        emission: if balado { if album.trim().is_empty() { artiste.clone() } else { album.trim().to_string() } } else { String::new() },
        artiste,
        balado,
        joue: statut == "Playing",
    }
}

/// MusicBrainz : le genre d'un artiste (None : aucun, ou erreur).
fn chercher_genre(artiste: &str) -> Option<String> {
    let nom = artiste.replace('"', " ");
    let requete = format!("artist:\"{nom}\"");
    let r = ureq::AgentBuilder::new()
        .timeout(Duration::from_secs(10))
        .user_agent("Jasette/0.1 (https://jonathanguernon.net/jasette)")
        .build()
        .get("https://musicbrainz.org/ws/2/artist/")
        .query("query", &requete)
        .query("fmt", "json")
        .call()
        .ok()?;
    let v: Value = serde_json::from_reader(r.into_reader().take(1 << 20)).ok()?;
    genre_musicbrainz(&v)
}

use std::io::Read as _;

#[cfg(test)]
mod essais {
    use super::*;
    use serde_json::json;

    const MIN: i64 = 60_000;

    fn chanson(artiste: &str, titre: &str) -> Lecture {
        Lecture { titre: titre.into(), artiste: artiste.into(), emission: String::new(), balado: false, joue: true }
    }

    fn sans_genre(_: &str) -> Genre {
        Genre::Connu(None)
    }

    #[test]
    fn textes() {
        let l = chanson("Daft Punk", "One More Time");
        assert_eq!(texte_note(&l, "artiste", None, false), "🎵 Écoute Daft Punk");
        assert_eq!(texte_note(&l, "", None, true), "🎵 Listening to Daft Punk");
        assert_eq!(texte_note(&l, "genre", Some("rock"), false), "🎵 Écoute du rock !");
        assert_eq!(texte_note(&l, "genre", Some("electronic"), false), "🎵 Écoute de l'électro !");
        assert_eq!(texte_note(&l, "genre", Some("rock"), true), "🎵 Listening to rock!");
        assert_eq!(texte_note(&l, "genre", None, false), "🎵 Écoute Daft Punk");
        assert_eq!(texte_note(&l, "titre", None, false), "🎵 One More Time — Daft Punk");
        let b = lecture_de("Playing", "Épisode 2100", &["Joe Rogan".into()], "The Joe Rogan Experience", "spotify:episode:abc");
        assert!(b.balado);
        assert_eq!(texte_note(&b, "artiste", None, false), "🎙️ Écoute The Joe Rogan Experience");
        assert_eq!(texte_note(&b, "genre", None, true), "🎙️ Listening to a podcast");
        let c = lecture_de("Paused", "Get Lucky", &["Daft Punk".into(), "Pharrell Williams".into()], "RAM", "/com/spotify/track/123");
        assert!(!c.balado && !c.joue);
        assert_eq!(c.artiste, "Daft Punk, Pharrell Williams");
        let t = tronquer("🎵 Écoute Godspeed You! Black Emperor", 20);
        assert!(t.chars().count() <= 20 && t.ends_with('…'), "{t}");
        assert_eq!(tronquer("court", 60), "court");
        assert_eq!(tronquer(&"é".repeat(80), 60).chars().count(), 60);
    }

    #[test]
    fn musicbrainz() {
        // Une réponse comme celles de /ws/2/artist/?query=…&fmt=json (abrégée).
        let v = json!({"created": "2026-11-26T00:00:00.000Z", "count": 2, "offset": 0, "artists": [
            {"id": "056e4f3e-d505-4dad-8ec1-d04f521cbb56", "name": "Daft Punk", "score": 100,
             "tags": [{"count": 3, "name": "house"}, {"count": 12, "name": "electronic"}, {"count": 5, "name": "french"}]},
            {"id": "x", "name": "Daft Punk Tribute", "tags": [{"count": 40, "name": "rock"}]}]});
        assert_eq!(genre_musicbrainz(&v).as_deref(), Some("electronic"));
        assert_eq!(genre_musicbrainz(&json!({"artists": [{"name": "Inconnu"}]})), None);
        assert_eq!(genre_musicbrainz(&json!({"artists": []})), None);
        assert_eq!(genre_musicbrainz(&json!({"error": "rate limited"})), None);
    }

    #[test]
    fn anti_rafale() {
        let mut m = Moteur::default();
        let t0 = 1_000_000_000_000;
        let a = chanson("Daft Punk", "One More Time");
        // Premier sondage avec musique : publication tout de suite.
        assert_eq!(m.sondage(t0, Some(&a), false, "", false, 60, &sans_genre), Some(Action::Publier("🎵 Écoute Daft Punk".into())));
        // Une autre chanson, 30 s plus tard : rien avant 15 minutes.
        let b = chanson("Justice", "D.A.N.C.E.");
        let mut t = t0;
        let mut publications = 0;
        for _ in 0..60 {
            t += MIN / 2;
            // Justice joue 20 min, Daft Punk (de nouveau) les 10 dernières.
            let l = if t - t0 <= 20 * MIN { &b } else { &a };
            if let Some(Action::Publier(_)) = m.sondage(t, Some(l), true, "", false, 60, &sans_genre) {
                publications += 1;
            }
        }
        // 30 minutes : deux périodes jugées au plus.
        assert!(publications <= 2, "{publications}");
        // Pause de 19 min : rien ; à 20 min : effacement, une seule fois.
        let pause = t;
        let arret = Lecture { joue: false, ..a.clone() };
        for k in 1..=38 {
            let r = m.sondage(pause + k * MIN / 2, Some(&arret), true, "", false, 60, &sans_genre);
            assert_eq!(r, None, "pause {k}");
        }
        assert_eq!(m.sondage(pause + 20 * MIN, None, true, "", false, 60, &sans_genre), Some(Action::Effacer));
        assert_eq!(m.sondage(pause + 25 * MIN, None, false, "", false, 60, &sans_genre), None);
    }

    #[test]
    fn le_plus_long_gagne() {
        let mut m = Moteur::default();
        let t0 = 2_000_000_000_000;
        let a = chanson("Daft Punk", "x");
        let b = chanson("Justice", "y");
        assert!(m.sondage(t0, Some(&a), false, "", false, 60, &sans_genre).is_some());
        let mut t = t0;
        let mut r = None;
        // 15 minutes : Justice 11 min, puis Daft Punk 4 min (le dernier).
        while t < t0 + 15 * MIN {
            t += MIN / 2;
            let l = if t - t0 <= 11 * MIN { &b } else { &a };
            if let Some(x) = m.sondage(t, Some(l), true, "", false, 60, &sans_genre) {
                r = Some(x);
            }
        }
        assert_eq!(r, Some(Action::Publier("🎵 Écoute Justice".into())));
        // Même texte, note encore musicale : pas de nouvelle publication.
        let mut t2 = t;
        let mut encore = None;
        while t2 < t + 16 * MIN {
            t2 += MIN / 2;
            if let Some(x) = m.sondage(t2, Some(&b), true, "", false, 60, &sans_genre) {
                encore = Some(x);
            }
        }
        assert_eq!(encore, None);
        // La note n'est plus musicale (écrite à la main) : la musique passe par-dessus.
        let mut t3 = t2;
        let mut apres = None;
        while t3 < t2 + 16 * MIN && apres.is_none() {
            t3 += MIN / 2;
            apres = m.sondage(t3, Some(&b), false, "", false, 60, &sans_genre);
        }
        assert_eq!(apres, Some(Action::Publier("🎵 Écoute Justice".into())));
    }

    #[test]
    fn attend_le_genre() {
        let mut m = Moteur::default();
        let t0 = 3_000_000_000_000;
        let a = chanson("Daft Punk", "x");
        let attente = |_: &str| Genre::Attente;
        assert_eq!(m.sondage(t0, Some(&a), false, "genre", false, 60, &attente), None);
        let connu = |_: &str| Genre::Connu(Some("electronic".into()));
        assert_eq!(
            m.sondage(t0 + 2_000, Some(&a), false, "genre", false, 60, &connu),
            Some(Action::Publier("🎵 Écoute de l'électro !".into()))
        );
    }
}
