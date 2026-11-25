//! Les liens des messages : les trouver dans le texte (pour les rendre
//! cliquables) et en faire un aperçu (titre, description, image, site).
//!
//! L'aperçu fourni par le service (`link_preview` du message, PROTOCOLE.md
//! §5.9) passe en premier. Sinon on le demande au serveur Jasette (trame
//! `apercu`), ou, sans serveur, on lit la page nous-mêmes, avec les mêmes
//! garde-fous que le serveur : 5 s, 2 Mio de HTML, 5 redirections, jamais
//! d'adresse privée ou locale (vérifiée après la résolution du nom).

use std::io::Read;
use std::net::{IpAddr, SocketAddr, ToSocketAddrs};
use std::ops::Range;
use std::path::{Path, PathBuf};
use std::time::{Duration, SystemTime, UNIX_EPOCH};

use serde::{Deserialize, Serialize};

const DELAI: Duration = Duration::from_secs(5);
const HTML_MAX: u64 = 2 << 20;
const IMAGE_MAX: u64 = 8 << 20;
const LARGEUR: u32 = 640;
const DUREE_OK: i64 = 7 * 24 * 3600 * 1000;
const DUREE_ECHEC: i64 = 6 * 3600 * 1000;
const UA: &str = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36";

/// L'aperçu d'un lien, tel que le pont l'envoie (`link_preview`) ou tel
/// qu'on l'a fait. `image` : un fichier local ; `image_url` : une image à
/// aller chercher ; `image_media` : une pièce à demander au pont.
#[derive(Clone, Debug, Default, Deserialize, Serialize, PartialEq)]
pub struct Apercu {
    #[serde(default)]
    pub url: String,
    #[serde(default)]
    pub title: String,
    #[serde(default)]
    pub description: String,
    #[serde(default)]
    pub site: String,
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub image: String,
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub image_url: String,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub image_media: Option<serde_json::Value>,
}

impl Apercu {
    /// Le domaine à afficher sous la carte : le site, sinon l'hôte de l'adresse.
    pub fn domaine(&self) -> String {
        hote(&self.url).map(|h| h.trim_start_matches("www.").to_string()).unwrap_or_default()
    }
}

// ---------------------------------------------------------------------------
// Les liens dans le texte
// ---------------------------------------------------------------------------

/// Un caractère qui ne peut pas faire partie d'une adresse écrite dans un
/// message (espaces, guillemets, emojis…).
fn hors_lien(c: char) -> bool {
    c.is_whitespace()
        || c.is_control()
        || matches!(c, '<' | '>' | '"' | '`' | '«' | '»' | '“' | '”' | '‘' | '’' | '，' | '。' | '、' | '「' | '」')
        || matches!(c as u32, 0x1F000..=0x1FAFF | 0x2600..=0x27BF | 0x2B00..=0x2BFF | 0xFE0F | 0x200D | 0x200B | 0x2060)
}

/// Les adresses du texte : leur plage (octets) et l'adresse à ouvrir
/// (`www.…` devient `https://www.…`). La ponctuation finale (`.`, `,`, `!`,
/// `?`, `:`, `;`, une parenthèse fermante sans ouvrante…) n'en fait pas partie.
pub fn liens(texte: &str) -> Vec<(Range<usize>, String)> {
    let mut r = Vec::new();
    let bas = texte.to_ascii_lowercase();
    let mut i = 0;
    while i < texte.len() {
        let reste = &bas[i..];
        let debut = if reste.starts_with("https://") || reste.starts_with("http://") {
            Some(false)
        } else if reste.starts_with("www.") {
            Some(true)
        } else {
            None
        };
        // Le lien commence à une frontière de mot (pas « abcwww. », pas « x@www. »).
        let frontiere = texte[..i].chars().next_back().is_none_or(|p| !(p.is_alphanumeric() || matches!(p, '.' | '@' | '/' | '-' | '_')));
        let Some(www) = debut.filter(|_| frontiere) else {
            i += texte[i..].chars().next().map_or(1, char::len_utf8);
            continue;
        };
        let mut fin = i;
        for (k, c) in texte[i..].char_indices() {
            if hors_lien(c) {
                break;
            }
            fin = i + k + c.len_utf8();
        }
        // La ponctuation finale, et les fermantes sans ouvrante.
        loop {
            let Some(c) = texte[i..fin].chars().next_back() else { break };
            let enlever = match c {
                '.' | ',' | ';' | ':' | '!' | '?' | '\'' | '…' | '*' | '_' => true,
                ')' => texte[i..fin].matches('(').count() < texte[i..fin].matches(')').count(),
                ']' => texte[i..fin].matches('[').count() < texte[i..fin].matches(']').count(),
                '}' => texte[i..fin].matches('{').count() < texte[i..fin].matches('}').count(),
                _ => false,
            };
            if !enlever {
                break;
            }
            fin -= c.len_utf8();
        }
        let brut = &texte[i..fin];
        // Il faut un hôte : « https:// » seul, ou « www.x » sans point, ne comptent pas.
        let sans_schema = if www { brut } else { brut.split_once("://").map_or("", |(_, b)| b) };
        let hote_brut = sans_schema.split(['/', '?', '#']).next().unwrap_or("");
        let valide = if www {
            hote_brut.len() > 4 && hote_brut[4..].contains('.') && !hote_brut.ends_with('.')
        } else {
            !hote_brut.is_empty() && hote_brut.chars().any(char::is_alphanumeric)
        };
        if valide {
            let url = if www { format!("https://{brut}") } else { brut.to_string() };
            r.push((i..fin, url));
            i = fin.max(i + 1);
        } else {
            i += 4;
        }
        while i < texte.len() && !texte.is_char_boundary(i) {
            i += 1;
        }
    }
    r
}

/// La première adresse du texte.
pub fn premier_lien(texte: &str) -> Option<String> {
    if !texte.contains("://") && !texte.to_ascii_lowercase().contains("www.") {
        return None;
    }
    liens(texte).into_iter().next().map(|(_, u)| u)
}

/// L'hôte d'une adresse (minuscules, sans port ni identifiants).
pub fn hote(url: &str) -> Option<String> {
    let apres = url.split_once("://").map_or(url, |(_, b)| b);
    let autorite = apres.split(['/', '?', '#']).next()?;
    let autorite = autorite.rsplit_once('@').map_or(autorite, |(_, h)| h);
    let h = if let Some(r) = autorite.strip_prefix('[') {
        r.split(']').next()?.to_string()
    } else {
        autorite.split(':').next()?.to_string()
    };
    (!h.is_empty()).then(|| h.to_lowercase())
}

// ---------------------------------------------------------------------------
// Les garde-fous du réseau
// ---------------------------------------------------------------------------

/// Les adresses qu'un aperçu ne joint jamais (réseau local, boucle, lien
/// local, plages réservées).
pub fn ip_interdite(ip: IpAddr) -> bool {
    match ip {
        IpAddr::V4(v) => {
            let o = v.octets();
            v.is_loopback()
                || v.is_private()
                || v.is_link_local()
                || v.is_unspecified()
                || v.is_broadcast()
                || v.is_multicast()
                || v.is_documentation()
                || o[0] == 0
                || (o[0] == 100 && (64..128).contains(&o[1]))
                || (o[0] == 192 && o[1] == 0 && o[2] == 0)
                || (o[0] == 198 && (18..20).contains(&o[1]))
                || o[0] >= 240
        }
        IpAddr::V6(v) => {
            if let Some(v4) = v.to_ipv4_mapped() {
                return ip_interdite(IpAddr::V4(v4));
            }
            let s = v.segments();
            v.is_loopback()
                || v.is_unspecified()
                || v.is_multicast()
                || (s[0] & 0xfe00) == 0xfc00 // fc00::/7
                || (s[0] & 0xffc0) == 0xfe80 // fe80::/10
                || (s[0] & 0xffc0) == 0xfec0 // fec0::/10
                || (s[0] == 0x64 && s[1] == 0xff9b) // NAT64
                || (s[0] == 0x2001 && s[1] == 0x0db8) // documentation
        }
    }
}

/// Un nom d'hôte qu'on refuse sans même le résoudre.
fn nom_interdit(h: &str) -> bool {
    let h = h.trim_end_matches('.');
    h == "localhost"
        || h.ends_with(".localhost")
        || h.ends_with(".local")
        || h.ends_with(".internal")
        || h.parse::<IpAddr>().is_ok_and(ip_interdite)
        || (!h.contains('.') && !h.contains(':'))
}

/// Résout un nom et refuse s'il mène, même en partie, à une adresse interdite.
struct ResolveurSur;

impl ureq::Resolver for ResolveurSur {
    fn resolve(&self, netloc: &str) -> std::io::Result<Vec<SocketAddr>> {
        let refus = || std::io::Error::new(std::io::ErrorKind::PermissionDenied, "adresse privée ou locale refusée");
        let h = netloc.rsplit_once(':').map_or(netloc, |(h, _)| h).trim_start_matches('[').trim_end_matches(']');
        if nom_interdit(&h.to_lowercase()) {
            return Err(refus());
        }
        let adresses: Vec<SocketAddr> = netloc.to_socket_addrs()?.collect();
        if adresses.is_empty() || adresses.iter().any(|a| ip_interdite(a.ip())) {
            return Err(refus());
        }
        Ok(adresses)
    }
}

fn agent() -> ureq::Agent {
    ureq::AgentBuilder::new()
        .resolver(ResolveurSur)
        .timeout(DELAI)
        .redirects(5)
        .user_agent(UA)
        .try_proxy_from_env(false)
        .build()
}

/// http ou https, un vrai nom d'hôte, pas d'identifiants.
fn adresse_acceptable(url: &str) -> bool {
    let bas = url.to_ascii_lowercase();
    if !(bas.starts_with("http://") || bas.starts_with("https://")) || url.len() > 2048 {
        return false;
    }
    let apres = &url[url.find("://").unwrap_or(0) + 3..];
    let autorite = apres.split(['/', '?', '#']).next().unwrap_or("");
    !autorite.contains('@') && hote(url).is_some_and(|h| !nom_interdit(&h))
}

// ---------------------------------------------------------------------------
// Faire l'aperçu nous-mêmes (sans serveur Jasette)
// ---------------------------------------------------------------------------

#[derive(Serialize, Deserialize)]
struct EnCache {
    quand: i64,
    #[serde(default)]
    apercu: Option<Apercu>,
}

fn maintenant() -> i64 {
    SystemTime::now().duration_since(UNIX_EPOCH).map_or(0, |d| d.as_millis() as i64)
}

/// Une clé de fichier pour une adresse (SHA-256, 16 octets en hexadécimal).
pub fn cle(url: &str) -> String {
    let d = ring::digest::digest(&ring::digest::SHA256, url.as_bytes());
    d.as_ref()[..16].iter().map(|o| format!("{o:02x}")).collect()
}

/// L'aperçu d'une adresse : du cache (`dossier`), sinon du réseau. None :
/// pas d'aperçu (site qui refuse, pas de balises, adresse interdite…).
/// Bloquant (quelques secondes au plus) : hors du fil de l'interface.
pub fn chercher(url: &str, dossier: &Path) -> Option<Apercu> {
    let k = cle(url);
    let fichier = dossier.join(format!("{k}.json"));
    if let Some(c) = std::fs::read(&fichier).ok().and_then(|b| serde_json::from_slice::<EnCache>(&b).ok()) {
        let duree = if c.apercu.is_some() { DUREE_OK } else { DUREE_ECHEC };
        let image_ok = c.apercu.as_ref().is_none_or(|a| a.image.is_empty() || Path::new(&a.image).is_file());
        if maintenant() - c.quand < duree && image_ok {
            return c.apercu;
        }
    }
    let apercu = lire_page(url, dossier, &k);
    let _ = std::fs::create_dir_all(dossier);
    if let Ok(b) = serde_json::to_vec(&EnCache { quand: maintenant(), apercu: apercu.clone() }) {
        let tmp = fichier.with_extension("tmp");
        if std::fs::write(&tmp, b).is_ok() {
            let _ = std::fs::rename(&tmp, &fichier);
        }
    }
    apercu
}

fn lire_page(url: &str, dossier: &Path, k: &str) -> Option<Apercu> {
    if !adresse_acceptable(url) {
        return None;
    }
    let rep = agent()
        .get(url)
        .set("Accept", "text/html,application/xhtml+xml;q=0.9,*/*;q=0.5")
        .set("Accept-Language", "fr-CA,fr;q=0.9,en;q=0.8")
        .call()
        .ok()?;
    let ct = rep.content_type().to_ascii_lowercase();
    if !ct.is_empty() && ct != "text/html" && ct != "application/xhtml+xml" {
        return None;
    }
    let finale = rep.get_url().to_string();
    let mut corps = Vec::new();
    rep.into_reader().take(HTML_MAX).read_to_end(&mut corps).ok()?;
    let html = String::from_utf8_lossy(&corps);
    let mut a = balises(&html, &finale);
    if a.title.is_empty() && a.description.is_empty() {
        return None;
    }
    a.url = url.to_string();
    if !a.image_url.is_empty() {
        a.image = image(&a.image_url, &dossier.join(format!("{k}.jpg"))).map(|p| p.to_string_lossy().into_owned()).unwrap_or_default();
        a.image_url.clear();
    }
    Some(a)
}

/// Télécharge une image (8 Mio au plus) et la garde réduite à 640 px de
/// large, en JPEG, dans `sortie`. Pour `image_url` (fourni par un service)
/// comme pour l'image d'une page.
pub fn image(url: &str, sortie: &Path) -> Option<PathBuf> {
    if sortie.is_file() {
        return Some(sortie.to_path_buf());
    }
    if !adresse_acceptable(url) {
        return None;
    }
    let rep = agent().get(url).set("Accept", "image/webp,image/png,image/jpeg,image/*;q=0.8").call().ok()?;
    let mut brut = Vec::new();
    rep.into_reader().take(IMAGE_MAX + 1).read_to_end(&mut brut).ok()?;
    if brut.len() as u64 > IMAGE_MAX {
        return None;
    }
    let img = image::load_from_memory(&brut).ok()?;
    if img.width() == 0 || img.height() == 0 || (img.width() as u64 * img.height() as u64) > 40_000_000 {
        return None;
    }
    let img = if img.width() > LARGEUR {
        let h = (img.height() as u64 * LARGEUR as u64 / img.width() as u64).max(1) as u32;
        img.resize_exact(LARGEUR, h, image::imageops::FilterType::Triangle)
    } else {
        img
    };
    let rgb = image::DynamicImage::ImageRgb8(img.to_rgb8());
    if let Some(d) = sortie.parent() {
        let _ = std::fs::create_dir_all(d);
    }
    let tmp = sortie.with_extension("tmp");
    let mut f = std::fs::File::create(&tmp).ok()?;
    rgb.write_with_encoder(image::codecs::jpeg::JpegEncoder::new_with_quality(&mut f, 82)).ok()?;
    drop(f);
    std::fs::rename(&tmp, sortie).ok()?;
    Some(sortie.to_path_buf())
}

// ---------------------------------------------------------------------------
// Les recherches en arrière-plan (trois fils au plus)
// ---------------------------------------------------------------------------

/// Une recherche : l'aperçu d'une page, ou une image `image_url` d'un service.
pub enum Tache {
    Page(String),
    Image(String),
}

/// Un résultat, que l'interface ramasse.
pub enum Pret {
    Page(String, Option<Apercu>),
    Image(String, Option<PathBuf>),
}

/// La file des recherches, ses fils, et les résultats en attente.
#[derive(Default)]
pub struct Travaux {
    file: std::sync::Mutex<(std::collections::VecDeque<Tache>, usize)>,
    pub prets: std::sync::Mutex<Vec<Pret>>,
}

const FILS: usize = 3;

impl Travaux {
    /// Ajoute une recherche. `serveur` : le serveur Jasette à qui demander
    /// les pages (sinon, ou s'il ne répond pas, on les lit nous-mêmes) ;
    /// `dossier` : le cache sur le disque.
    pub fn lancer(self: &std::sync::Arc<Self>, t: Tache, serveur: Option<crate::pont::ConfigServeur>, dossier: PathBuf) {
        {
            let Ok(mut f) = self.file.lock() else { return };
            f.0.push_back(t);
            if f.1 >= FILS {
                return;
            }
            f.1 += 1;
        }
        let moi = self.clone();
        let _ = std::thread::Builder::new().name("apercus".into()).spawn(move || {
            loop {
                let t = {
                    let Ok(mut f) = moi.file.lock() else { return };
                    match f.0.pop_front() {
                        Some(t) => t,
                        None => {
                            f.1 -= 1;
                            return;
                        }
                    }
                };
                let r = match t {
                    Tache::Page(u) => {
                        let a = page(&u, serveur.as_ref(), &dossier);
                        Pret::Page(u, a)
                    }
                    Tache::Image(u) => {
                        let p = image(&u, &dossier.join(format!("i-{}.jpg", cle(&u))));
                        Pret::Image(u, p)
                    }
                };
                if let Ok(mut p) = moi.prets.lock() {
                    p.push(r);
                }
            }
        });
    }
}

/// L'aperçu d'une page : par le serveur Jasette s'il y en a un (et qu'il
/// répond), sinon nous-mêmes.
fn page(url: &str, serveur: Option<&crate::pont::ConfigServeur>, dossier: &Path) -> Option<Apercu> {
    if let Some(cfg) = serveur {
        match crate::pont::apercu_lien(cfg, url) {
            Ok(Some(v)) => {
                return serde_json::from_value::<Apercu>(v).ok().map(|mut a| {
                    a.url = url.to_string();
                    a
                });
            }
            Ok(None) => return None,
            Err(()) => {}
        }
    }
    chercher(url, dossier)
}

// ---------------------------------------------------------------------------
// Les balises OpenGraph et Twitter card
// ---------------------------------------------------------------------------

/// Décode les entités HTML courantes.
fn entites(s: &str) -> String {
    if !s.contains('&') {
        return s.to_string();
    }
    let mut r = String::with_capacity(s.len());
    let mut reste = s;
    while let Some(p) = reste.find('&') {
        r.push_str(&reste[..p]);
        reste = &reste[p..];
        let Some(fin) = reste[..reste.len().min(12)].find(';') else {
            r.push('&');
            reste = &reste[1..];
            continue;
        };
        let nom = &reste[1..fin];
        let c = match nom {
            "amp" => Some('&'),
            "lt" => Some('<'),
            "gt" => Some('>'),
            "quot" => Some('"'),
            "apos" => Some('\''),
            "nbsp" => Some(' '),
            _ if nom.starts_with("#x") || nom.starts_with("#X") => u32::from_str_radix(&nom[2..], 16).ok().and_then(char::from_u32),
            _ if nom.starts_with('#') => nom[1..].parse().ok().and_then(char::from_u32),
            _ => None,
        };
        match c {
            Some(c) => {
                r.push(c);
                reste = &reste[fin + 1..];
            }
            None => {
                r.push('&');
                reste = &reste[1..];
            }
        }
    }
    r.push_str(reste);
    r
}

/// Les attributs d'une balise (`<meta …>`), noms en minuscules.
fn attributs(balise: &str) -> Vec<(String, String)> {
    let mut r = Vec::new();
    let b = balise.as_bytes();
    let mut i = 0;
    while i < b.len() {
        while i < b.len() && (b[i].is_ascii_whitespace() || b[i] == b'/') {
            i += 1;
        }
        let d = i;
        while i < b.len() && !b[i].is_ascii_whitespace() && b[i] != b'=' && b[i] != b'/' {
            i += 1;
        }
        let nom = balise[d..i].to_ascii_lowercase();
        while i < b.len() && b[i].is_ascii_whitespace() {
            i += 1;
        }
        let mut valeur = String::new();
        if i < b.len() && b[i] == b'=' {
            i += 1;
            while i < b.len() && b[i].is_ascii_whitespace() {
                i += 1;
            }
            if i < b.len() && (b[i] == b'"' || b[i] == b'\'') {
                let q = b[i];
                i += 1;
                let d = i;
                while i < b.len() && b[i] != q {
                    i += 1;
                }
                valeur = balise[d..i].to_string();
                i += 1;
            } else {
                let d = i;
                while i < b.len() && !b[i].is_ascii_whitespace() {
                    i += 1;
                }
                valeur = balise[d..i].to_string();
            }
        }
        if nom.is_empty() {
            i += 1;
            continue;
        }
        r.push((nom, entites(&valeur)));
    }
    r
}

fn couper(s: &str, n: usize) -> String {
    let s = s.split_whitespace().collect::<Vec<_>>().join(" ");
    if s.chars().count() <= n {
        return s;
    }
    let mut c: String = s.chars().take(n - 1).collect();
    c = c.trim_end().to_string();
    c.push('…');
    c
}

/// Lit les balises d'une page. `base` : l'adresse finale (pour une image relative).
pub fn balises(html: &str, base: &str) -> Apercu {
    let bas = html.to_ascii_lowercase();
    let mut metas: Vec<(String, String)> = Vec::new();
    let mut i = 0;
    while let Some(p) = bas[i..].find("<meta") {
        let d = i + p + 5;
        let Some(f) = bas[d..].find('>') else { break };
        let attrs = attributs(&html[d..d + f]);
        let prop = attrs
            .iter()
            .find(|(k, _)| k == "property" || k == "name" || k == "itemprop")
            .map(|(_, v)| v.trim().to_ascii_lowercase());
        let contenu = attrs.iter().find(|(k, _)| k == "content").map(|(_, v)| v.trim().to_string());
        if let (Some(p), Some(c)) = (prop, contenu)
            && !c.is_empty()
            && !metas.iter().any(|(k, _)| *k == p)
        {
            metas.push((p, c));
        }
        i = d + f;
    }
    let premier = |cles: &[&str]| {
        cles.iter().find_map(|k| metas.iter().find(|(p, _)| p == k).map(|(_, v)| v.clone())).unwrap_or_default()
    };
    let mut titre = premier(&["og:title", "twitter:title"]);
    if titre.is_empty()
        && let Some(p) = bas.find("<title")
        && let Some(d) = bas[p..].find('>')
        && let Some(f) = bas[p + d..].find("</title")
    {
        titre = entites(&html[p + d + 1..p + d + f]);
    }
    let mut image = premier(&["og:image:secure_url", "og:image", "og:image:url", "twitter:image", "twitter:image:src", "image"]);
    if !image.is_empty() {
        image = absolue(&image, base);
    }
    let mut site = premier(&["og:site_name", "application-name"]);
    if site.is_empty() {
        site = hote(base).unwrap_or_default().trim_start_matches("www.").to_string();
    }
    Apercu {
        url: base.to_string(),
        title: couper(&titre, 300),
        description: couper(&premier(&["og:description", "twitter:description", "description"]), 500),
        site: couper(&site, 100),
        image: String::new(),
        image_url: image,
        image_media: None,
    }
}

/// Une adresse relative rendue absolue par rapport à `base`.
fn absolue(lien: &str, base: &str) -> String {
    let l = lien.trim();
    if l.starts_with("http://") || l.starts_with("https://") {
        return l.to_string();
    }
    let schema = base.split_once("://").map_or("https", |(s, _)| s);
    if let Some(r) = l.strip_prefix("//") {
        return format!("{schema}://{r}");
    }
    let apres = base.split_once("://").map_or(base, |(_, b)| b);
    let autorite = apres.split(['/', '?', '#']).next().unwrap_or("");
    if l.starts_with('/') {
        return format!("{schema}://{autorite}{l}");
    }
    if l.contains(':') {
        return String::new(); // data:, javascript:…
    }
    let chemin = &apres[autorite.len()..];
    let chemin = chemin.split(['?', '#']).next().unwrap_or("");
    let dossier = chemin.rsplit_once('/').map_or("", |(d, _)| d);
    format!("{schema}://{autorite}{dossier}/{l}")
}

#[cfg(test)]
mod essais {
    use super::*;
    use ureq::Resolver;

    fn urls(t: &str) -> Vec<String> {
        liens(t).into_iter().map(|(r, _)| t[r].to_string()).collect()
    }

    #[test]
    fn trouve_les_liens() {
        assert_eq!(
            urls("I love pickles✨🥒 https://www.facebook.com/share/r/18fH8X7YXK/?mibextid=wwXIfr"),
            vec!["https://www.facebook.com/share/r/18fH8X7YXK/?mibextid=wwXIfr"]
        );
        assert_eq!(urls("Va voir www.lapresse.ca."), vec!["www.lapresse.ca"]);
        assert_eq!(liens("Va voir www.lapresse.ca.")[0].1, "https://www.lapresse.ca");
        assert_eq!(urls("(voir https://exemple.com/a)"), vec!["https://exemple.com/a"]);
        assert_eq!(urls("https://fr.wikipedia.org/wiki/Python_(langage)!"), vec!["https://fr.wikipedia.org/wiki/Python_(langage)"]);
        assert_eq!(urls("Quoi? http://a.ca/x?y=1, et https://b.ca!"), vec!["http://a.ca/x?y=1", "https://b.ca"]);
        assert_eq!(urls("https://fr.wikipedia.org/wiki/Québec?"), vec!["https://fr.wikipedia.org/wiki/Québec"]);
        assert!(urls("allo www. rien, https:// et abcwww.x.ca, moi@www.x.ca").is_empty());
        assert_eq!(urls("«https://x.ca/é»"), vec!["https://x.ca/é"]);
        assert_eq!(urls("HTTPS://X.CA/A"), vec!["HTTPS://X.CA/A"]);
    }

    #[test]
    fn adresses_interdites() {
        for u in [
            "http://127.0.0.1/",
            "http://localhost:8080/",
            "http://10.0.0.1/",
            "http://192.168.1.1/",
            "http://172.16.0.1/",
            "http://169.254.169.254/",
            "http://[::1]/",
            "http://[fc00::1]/",
            "http://intranet/",
            "ftp://exemple.com/",
            "https://moi:secret@exemple.com/",
        ] {
            assert!(!adresse_acceptable(u), "{u}");
        }
        assert!(adresse_acceptable("https://fr.wikipedia.org/wiki/Cornichon"));
        assert!(ResolveurSur.resolve("localtest.me:80").is_err());
    }

    #[test]
    fn lit_les_balises() {
        let html = r#"<html><head><title>Titre &amp; page</title>
            <meta property="og:title" content="Le titre OG">
            <meta name="description" content='Une description'>
            <meta property=og:image content="/img/a.png" />
            </head><body></body></html>"#;
        let a = balises(html, "https://www.exemple.com/x/y.html");
        assert_eq!(a.title, "Le titre OG");
        assert_eq!(a.description, "Une description");
        assert_eq!(a.site, "exemple.com");
        assert_eq!(a.image_url, "https://www.exemple.com/img/a.png");
        let b = balises("<title>Seul &#233;t&eacute;</title>", "https://a.ca/");
        assert_eq!(b.title, "Seul ét&eacute;");
    }

    /// Avec le réseau : JASETTE_ESSAI_APERCUS=<dossier> cargo test essai_reseau -- --ignored --nocapture
    #[test]
    #[ignore]
    fn essai_reseau() {
        let d = PathBuf::from(std::env::var("JASETTE_ESSAI_APERCUS").expect("JASETTE_ESSAI_APERCUS"));
        for u in ["https://fr.wikipedia.org/wiki/Cornichon", "https://www.youtube.com/watch?v=dQw4w9WgXcQ", "https://www.lapresse.ca"] {
            let a = chercher(u, &d).unwrap_or_else(|| panic!("pas d'aperçu : {u}"));
            println!("{a:#?}");
            assert!(!a.title.is_empty() && Path::new(&a.image).is_file(), "{u}");
        }
        for u in ["https://www.facebook.com/share/r/18fH8X7YXK/?mibextid=wwXIfr", "http://127.0.0.1:47800/", "http://localtest.me/"] {
            println!("{u} : {:?}", chercher(u, &d).map(|a| a.title));
        }
        assert!(chercher("http://localtest.me/", &d).is_none());
    }
}
