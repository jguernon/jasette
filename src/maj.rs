//! La mise à jour dans l'app, pour le bureau Linux (comme MiseAJour.kt d'Android).
//!
//! 1. `chercher` lit `version-linux.json` sur le site (HTTPS, seulement
//!    https://jonathanguernon.net, sans redirection) au démarrage (au plus une
//!    fois toutes les 6 heures) ou par Réglages > À propos.
//! 2. Selon `Installation` (le chemin de l'exécutable et `dpkg -S`) :
//!    - paquet .deb (/usr/…) : `telecharger` le .deb dans `<données>/maj/`
//!      (0600), SHA-256 vérifié, puis `installer_deb` : `pkexec apt-get install`
//!      (ou `pkexec dpkg -i`) ; c'est la fenêtre du système qui demande le mot
//!      de passe, jamais Jasette ;
//!    - archive .tar.gz (dossier de l'utilisateur, comme ~/.local/lib/jasette) :
//!      `telecharger` l'archive, SHA-256 vérifié, `extraire_tgz` dans un dossier
//!      temporaire à côté (sans chemin absolu, sans `..`, sans lien), puis
//!      `remplacer` : chaque exécutable remplacé par renommage atomique,
//!      l'ancien gardé en `<nom>.ancien` ;
//!    - version de développement (dossier target de cargo) : rien d'automatique.
//!
//! Rien de téléchargé n'est exécuté ni installé avant la vérification du SHA-256.
//!
//! Pour les essais (version debug seulement, ignoré en release) :
//! `JASETTE_MAJ_URL` remplace l'adresse de version-linux.json et permet aussi
//! un serveur local en HTTP sur 127.0.0.1 ; `JASETTE_MAJ_INSTALL=deb` force le
//! cas .deb et `JASETTE_MAJ_PKEXEC` remplace pkexec (un faux, pour les essais).

use std::io::{Read, Write};
use std::os::unix::fs::{DirBuilderExt, OpenOptionsExt, PermissionsExt};
use std::path::{Component, Path, PathBuf};
use std::process::{Command, Stdio};
use std::sync::OnceLock;
use std::sync::atomic::{AtomicU64, Ordering};
use std::time::Duration;

use serde::{Deserialize, Serialize};
use serde_json::Value;

/// Le seul hôte d'où Jasette accepte une mise à jour (en HTTPS).
pub const HOTE: &str = "jonathanguernon.net";
pub const ADRESSE_VERSION: &str = "https://jonathanguernon.net/jasette/telecharger/version-linux.json";
/// La page de téléchargement, pour une installation que Jasette ne sait pas mettre à jour.
pub const PAGE: &str = "https://jonathanguernon.net/jasette/";
pub const INTERVALLE_MS: i64 = 6 * 3600 * 1000;
const TAILLE_MAX: u64 = 400 << 20;
const EXTRAIT_MAX: u64 = 1 << 30;

/// Le numéro de cette version (10 000 + le nombre de commits, voir build.rs).
pub fn code_installe() -> u64 {
    env!("JASETTE_CODE").parse().unwrap_or(0)
}

/// « 1.0.0 (123) ».
pub fn nom_installe() -> &'static str {
    env!("JASETTE_NOM")
}

/// Les réglages d'essai, lus seulement dans une version debug.
fn essai(var: &str) -> Option<String> {
    if cfg!(debug_assertions) { std::env::var(var).ok().filter(|v| !v.is_empty()) } else { None }
}

/// Un serveur local (http://127.0.0.1) est-il permis ? Seulement en debug, avec JASETTE_MAJ_URL.
pub fn local_permis() -> bool {
    essai("JASETTE_MAJ_URL").is_some()
}

pub fn adresse_version() -> String {
    essai("JASETTE_MAJ_URL").unwrap_or_else(|| ADRESSE_VERSION.to_string())
}

// ---------------------------------------------------------------------------
// version-linux.json
// ---------------------------------------------------------------------------

/// Un paquet publié : son adresse, son SHA-256 et sa taille (octets).
#[derive(Clone, Debug, PartialEq)]
pub struct Paquet {
    pub url: String,
    pub sha256: String,
    pub taille: u64,
}

/// La version publiée (`version-linux.json`, écrit par paquets/empaqueter.sh) :
///
/// ```json
/// {"versionCode": 10123, "versionName": "1.0.0 (123)",
///  "notes": {"fr": "…", "en": "…"},
///  "deb":   {"url": "https://jonathanguernon.net/jasette/telecharger/jasette_1.0.0_amd64.deb?v=10123",
///            "sha256": "…", "taille": 41234567},
///  "targz": {"url": "https://jonathanguernon.net/jasette/telecharger/jasette-1.0.0-linux-x86_64.tar.gz?v=10123",
///            "sha256": "…", "taille": 40123456}}
/// ```
#[derive(Clone, Debug, PartialEq)]
pub struct VersionDispo {
    pub code: u64,
    pub nom: String,
    pub notes_fr: String,
    pub notes_en: String,
    pub deb: Option<Paquet>,
    pub targz: Option<Paquet>,
}

impl VersionDispo {
    /// Les notes dans la langue voulue (l'autre si celle-là manque).
    pub fn notes(&self, anglais: bool) -> &str {
        let (a, b) = if anglais { (&self.notes_en, &self.notes_fr) } else { (&self.notes_fr, &self.notes_en) };
        if a.trim().is_empty() { b } else { a }
    }
}

fn hex64(s: &str) -> bool {
    s.len() == 64 && s.bytes().all(|b| b.is_ascii_digit() || (b'a'..=b'f').contains(&b))
}

/// HTTPS seulement, et seulement sur jonathanguernon.net (pas de sous-domaine,
/// de port ni d'identifiants). `local` : accepte aussi http://127.0.0.1:<port>/
/// (les essais d'une version debug).
pub fn adresse_sure(url: &str, local: bool) -> bool {
    if url.len() > 2048 || url.chars().any(|c| c.is_whitespace() || c.is_control() || c == '\\') {
        return false;
    }
    let bas = url.to_ascii_lowercase();
    if let Some(chemin) = bas.strip_prefix(&format!("https://{HOTE}")) {
        return chemin.starts_with('/') && chemin.len() > 1;
    }
    if local && let Some(reste) = bas.strip_prefix("http://127.0.0.1:") {
        let port: String = reste.chars().take_while(char::is_ascii_digit).collect();
        return !port.is_empty() && port.len() <= 5 && reste[port.len()..].starts_with('/');
    }
    false
}

fn lire_paquet(v: Option<&Value>, local: bool) -> Option<Paquet> {
    let o = v?.as_object()?;
    let url = o.get("url")?.as_str()?.trim().to_string();
    let sha256 = o.get("sha256")?.as_str()?.trim().to_ascii_lowercase();
    let taille = o.get("taille").and_then(Value::as_u64).unwrap_or(0);
    (adresse_sure(&url, local) && hex64(&sha256) && taille <= TAILLE_MAX).then_some(Paquet { url, sha256, taille })
}

/// Lit `version-linux.json` ; None s'il est illisible ou douteux (pas de
/// versionCode positif, aucun paquet valable : adresse hors du site, SHA-256
/// qui n'a pas 64 chiffres hexadécimaux, taille démesurée).
pub fn lire(json: &str, local: bool) -> Option<VersionDispo> {
    let v: Value = serde_json::from_str(json).ok()?;
    let code = v.get("versionCode")?.as_u64().filter(|c| *c > 0)?;
    let deb = lire_paquet(v.get("deb"), local);
    let targz = lire_paquet(v.get("targz"), local);
    if deb.is_none() && targz.is_none() {
        return None;
    }
    let notes = |l: &str| v.get("notes").and_then(|n| n.get(l)).and_then(Value::as_str).unwrap_or("").trim().to_string();
    let nom = v.get("versionName").and_then(Value::as_str).unwrap_or("").trim();
    Some(VersionDispo {
        code,
        nom: if nom.is_empty() { code.to_string() } else { nom.to_string() },
        notes_fr: notes("fr"),
        notes_en: notes("en"),
        deb,
        targz,
    })
}

/// La version publiée est-elle plus récente que celle installée ? (le versionCode seul compte)
pub fn plus_recente(publiee: Option<&VersionDispo>, installee: u64) -> bool {
    publiee.is_some_and(|p| p.code > installee)
}

// ---------------------------------------------------------------------------
// Ce qui est gardé entre deux lancements : <données>/maj/etat.json
// ---------------------------------------------------------------------------

#[derive(Clone, Debug, Default, Serialize, Deserialize)]
pub struct Memoire {
    /// La dernière vérification réussie (ms).
    #[serde(default)]
    pub derniere_verif: i64,
    /// Le versionCode remis à plus tard : sa carte reste cachée.
    #[serde(default)]
    pub ignoree: u64,
    /// Le dernier version-linux.json lu.
    #[serde(default)]
    pub json: String,
}

pub fn creer_dossier(d: &Path) -> std::io::Result<()> {
    std::fs::DirBuilder::new().recursive(true).mode(0o700).create(d)
}

impl Memoire {
    pub fn charger(dossier: &Path) -> Self {
        std::fs::read_to_string(dossier.join("etat.json")).ok().and_then(|t| serde_json::from_str(&t).ok()).unwrap_or_default()
    }

    pub fn garder(&self, dossier: &Path) {
        if creer_dossier(dossier).is_err() {
            return;
        }
        let tmp = dossier.join("etat.json.tmp");
        if let Ok(t) = serde_json::to_string_pretty(self)
            && std::fs::write(&tmp, t).is_ok()
        {
            let _ = std::fs::rename(&tmp, dossier.join("etat.json"));
        }
    }
}

/// Vide `<données>/maj/` de ses téléchargements (etat.json reste).
pub fn nettoyer(dossier: &Path) {
    if let Ok(l) = std::fs::read_dir(dossier) {
        for e in l.flatten() {
            if e.file_name() != "etat.json" {
                let p = e.path();
                let _ = if p.is_dir() { std::fs::remove_dir_all(&p) } else { std::fs::remove_file(&p) };
            }
        }
    }
}

// ---------------------------------------------------------------------------
// Comment Jasette est installé
// ---------------------------------------------------------------------------

#[derive(Clone, Debug, PartialEq)]
pub enum Installation {
    /// Le paquet .deb (dpkg connaît l'exécutable).
    Deb,
    /// L'archive .tar.gz, dans un dossier de l'utilisateur (ses binaires à côté).
    Archive(PathBuf),
    /// Compilé par cargo (un dossier target) : mise à jour par git et cargo.
    Dev,
    /// Ailleurs (dossier non modifiable, /usr hors paquet) : le site seulement.
    Autre,
}

/// Le chemin de l'exécutable, lu une seule fois (au démarrage) : après un
/// remplacement, /proc/self/exe se termine par « (deleted) ».
pub fn chemin_exe() -> Option<PathBuf> {
    static EXE: OnceLock<Option<PathBuf>> = OnceLock::new();
    EXE.get_or_init(|| {
        let p = std::env::current_exe().ok()?;
        let s = p.to_string_lossy();
        Some(match s.strip_suffix(" (deleted)") {
            Some(v) => PathBuf::from(v),
            None => p,
        })
    })
    .clone()
}

fn dpkg_possede(exe: &Path) -> bool {
    Command::new("dpkg")
        .arg("-S")
        .arg(exe)
        .stdin(Stdio::null())
        .stderr(Stdio::null())
        .output()
        .is_ok_and(|o| o.status.success() && String::from_utf8_lossy(&o.stdout).starts_with("jasette:"))
}

fn inscriptible(dossier: &Path) -> bool {
    let essai = dossier.join(format!(".jasette-ecriture-{}", std::process::id()));
    let ok = std::fs::OpenOptions::new().write(true).create_new(true).mode(0o600).open(&essai).is_ok();
    let _ = std::fs::remove_file(&essai);
    ok
}

/// Un exécutable compilé par cargo : sous un dossier « target… », ou à côté
/// des dossiers de cargo (.fingerprint, deps).
fn dans_cargo(exe: &Path) -> bool {
    let dir = exe.parent().unwrap_or(Path::new("/"));
    exe.components().any(|c| matches!(c, Component::Normal(n) if n.to_string_lossy().starts_with("target")))
        || (dir.join(".fingerprint").is_dir() && dir.join("deps").is_dir())
}

/// Le cas d'installation d'un exécutable (`dpkg` : dpkg -S le connaît-il ?).
pub fn detecter_chemin(exe: &Path, dpkg: impl Fn(&Path) -> bool) -> Installation {
    if dans_cargo(exe) {
        return Installation::Dev;
    }
    if exe.starts_with("/usr") {
        return if dpkg(exe) { Installation::Deb } else { Installation::Autre };
    }
    match exe.parent() {
        Some(d) if inscriptible(d) => Installation::Archive(d.to_path_buf()),
        _ => Installation::Autre,
    }
}

pub fn detecter() -> Installation {
    if essai("JASETTE_MAJ_INSTALL").as_deref() == Some("deb") {
        return Installation::Deb;
    }
    match chemin_exe() {
        Some(exe) => detecter_chemin(&exe, dpkg_possede),
        None => Installation::Autre,
    }
}

// ---------------------------------------------------------------------------
// Le réseau
// ---------------------------------------------------------------------------

fn agent() -> ureq::Agent {
    ureq::AgentBuilder::new()
        .timeout_connect(Duration::from_secs(8))
        .timeout_read(Duration::from_secs(30))
        // Aucune redirection : rien ne quitte https://jonathanguernon.net.
        .redirects(0)
        .user_agent(&format!("Jasette-Linux/{}", nom_installe()))
        .try_proxy_from_env(false)
        .build()
}

/// Lit version-linux.json : la version et son texte (gardé dans etat.json).
pub fn chercher() -> Result<(VersionDispo, String), String> {
    let local = local_permis();
    let adresse = adresse_version();
    if !adresse_sure(&adresse, local) {
        return Err("adresse refusée".into());
    }
    let minutes = chrono::Utc::now().timestamp() / 60;
    let sep = if adresse.contains('?') { '&' } else { '?' };
    let r = agent()
        .get(&format!("{adresse}{sep}t={minutes}"))
        .set("Cache-Control", "no-cache")
        .call()
        .map_err(|e| e.to_string())?;
    if r.status() != 200 {
        return Err(format!("HTTP {}", r.status()));
    }
    let mut texte = String::new();
    r.into_reader().take(64 * 1024).read_to_string(&mut texte).map_err(|e| e.to_string())?;
    let v = lire(&texte, local).ok_or("version-linux.json illisible")?;
    Ok((v, texte))
}

fn hex(octets: &[u8]) -> String {
    octets.iter().map(|b| format!("{b:02x}")).collect()
}

pub fn sha256_fichier(p: &Path) -> Result<String, String> {
    let mut f = std::fs::File::open(p).map_err(|e| e.to_string())?;
    let mut ctx = ring::digest::Context::new(&ring::digest::SHA256);
    let mut tampon = vec![0u8; 64 * 1024];
    loop {
        let n = f.read(&mut tampon).map_err(|e| e.to_string())?;
        if n == 0 {
            break;
        }
        ctx.update(&tampon[..n]);
    }
    Ok(hex(ctx.finish().as_ref()))
}

/// Télécharge `p` dans `dest` (fichier 0600, dossier 0700) en calculant son
/// SHA-256 ; `recu` suit les octets reçus. Le fichier n'existe sous son nom
/// qu'une fois le SHA-256 (et la taille) vérifiés ; sinon il est effacé.
pub fn telecharger(p: &Paquet, dest: &Path, recu: &AtomicU64) -> Result<(), String> {
    if !adresse_sure(&p.url, local_permis()) {
        return Err("adresse refusée".into());
    }
    if let Some(d) = dest.parent() {
        creer_dossier(d).map_err(|e| e.to_string())?;
    }
    let part = dest.with_extension("part");
    let _ = std::fs::remove_file(&part);
    let resultat = (|| {
        let r = agent().get(&p.url).call().map_err(|e| e.to_string())?;
        if r.status() != 200 {
            return Err(format!("HTTP {}", r.status()));
        }
        let limite = if p.taille > 0 { p.taille } else { TAILLE_MAX };
        let mut lecteur = r.into_reader().take(limite + 1);
        let mut f = std::fs::OpenOptions::new()
            .write(true)
            .create_new(true)
            .mode(0o600)
            .open(&part)
            .map_err(|e| e.to_string())?;
        let mut ctx = ring::digest::Context::new(&ring::digest::SHA256);
        let mut tampon = vec![0u8; 64 * 1024];
        let mut total = 0u64;
        loop {
            let n = lecteur.read(&mut tampon).map_err(|e| e.to_string())?;
            if n == 0 {
                break;
            }
            total += n as u64;
            if total > limite {
                return Err("fichier plus gros que prévu".into());
            }
            ctx.update(&tampon[..n]);
            f.write_all(&tampon[..n]).map_err(|e| e.to_string())?;
            recu.store(total, Ordering::Relaxed);
        }
        f.sync_all().map_err(|e| e.to_string())?;
        if p.taille > 0 && total != p.taille {
            return Err("téléchargement incomplet".into());
        }
        if hex(ctx.finish().as_ref()) != p.sha256 {
            return Err("SHA-256 différent : fichier refusé".into());
        }
        std::fs::rename(&part, dest).map_err(|e| e.to_string())?;
        std::fs::set_permissions(dest, std::fs::Permissions::from_mode(0o600)).map_err(|e| e.to_string())
    })();
    if resultat.is_err() {
        let _ = std::fs::remove_file(&part);
    }
    resultat
}

// ---------------------------------------------------------------------------
// L'archive .tar.gz
// ---------------------------------------------------------------------------

/// Un chemin d'archive sûr : relatif, sans `..`, sans racine. Le premier
/// élément (le dossier « jasette-1.0.0-linux-x86_64 ») est retiré ; None pour
/// ce dossier lui-même ou un chemin refusé (voir `extraire_tgz`).
fn chemin_sur(nom: &str) -> Result<Option<PathBuf>, String> {
    if nom.is_empty() || nom.starts_with('/') || nom.contains('\\') || nom.contains('\0') {
        return Err(format!("chemin refusé dans l'archive : {nom}"));
    }
    let mut r = PathBuf::new();
    let mut premier = true;
    for c in Path::new(nom).components() {
        match c {
            Component::Normal(n) => {
                if premier {
                    premier = false;
                } else {
                    r.push(n);
                }
            }
            Component::CurDir => {}
            _ => return Err(format!("chemin refusé dans l'archive : {nom}")),
        }
    }
    Ok((!r.as_os_str().is_empty()).then_some(r))
}

fn octal(champ: &[u8]) -> Result<u64, String> {
    let s = String::from_utf8_lossy(champ);
    let s = s.trim_matches(|c: char| c == '\0' || c == ' ');
    if s.is_empty() {
        return Ok(0);
    }
    u64::from_str_radix(s, 8).map_err(|_| "en-tête d'archive illisible".to_string())
}

fn texte_nul(champ: &[u8]) -> String {
    let fin = champ.iter().position(|&b| b == 0).unwrap_or(champ.len());
    String::from_utf8_lossy(&champ[..fin]).into_owned()
}

/// Extrait l'archive dans `dest` (recréé vide, 0700), sans le dossier de tête.
/// Seulement des fichiers et des dossiers : liens, périphériques, en-têtes
/// étendus, chemins absolus ou avec `..` font échouer toute l'extraction.
/// Rend les fichiers extraits (chemins relatifs à `dest`).
pub fn extraire_tgz(archive: &Path, dest: &Path) -> Result<Vec<PathBuf>, String> {
    if dest.exists() {
        std::fs::remove_dir_all(dest).map_err(|e| e.to_string())?;
    }
    creer_dossier(dest).map_err(|e| e.to_string())?;
    let f = std::fs::File::open(archive).map_err(|e| e.to_string())?;
    let mut gz = flate2::read::GzDecoder::new(std::io::BufReader::new(f));
    let mut fichiers = Vec::new();
    let mut total = 0u64;
    let mut entete = [0u8; 512];
    loop {
        if let Err(e) = gz.read_exact(&mut entete) {
            // Une archive sans les blocs nuls de fin s'arrête ici.
            if e.kind() == std::io::ErrorKind::UnexpectedEof && !fichiers.is_empty() {
                break;
            }
            return Err(format!("archive illisible : {e}"));
        }
        if entete.iter().all(|&b| b == 0) {
            break;
        }
        // La somme de contrôle de l'en-tête (le champ compté comme des espaces).
        let attendu = octal(&entete[148..156])?;
        let somme: u64 = entete.iter().enumerate().map(|(i, &b)| if (148..156).contains(&i) { 32 } else { b as u64 }).sum();
        if somme != attendu {
            return Err("archive abîmée (en-tête)".into());
        }
        let mut nom = texte_nul(&entete[0..100]);
        if &entete[257..262] == b"ustar" {
            let prefixe = texte_nul(&entete[345..500]);
            if !prefixe.is_empty() {
                nom = format!("{prefixe}/{nom}");
            }
        }
        let mode = octal(&entete[100..108])? as u32;
        let taille = octal(&entete[124..136])?;
        let type_ = entete[156];
        total += taille;
        if total > EXTRAIT_MAX {
            return Err("archive trop grosse".into());
        }
        let chemin = chemin_sur(&nom)?;
        match type_ {
            b'5' => {
                if let Some(c) = chemin {
                    creer_dossier(&dest.join(c)).map_err(|e| e.to_string())?;
                }
            }
            b'0' | 0 => {
                let c = chemin.ok_or_else(|| format!("chemin refusé dans l'archive : {nom}"))?;
                let cible = dest.join(&c);
                if let Some(p) = cible.parent() {
                    creer_dossier(p).map_err(|e| e.to_string())?;
                }
                // Les droits de l'archive, sans setuid ni écriture pour les autres.
                let droits = (mode & 0o755) | 0o600;
                let mut sortie = std::fs::OpenOptions::new()
                    .write(true)
                    .create_new(true)
                    .mode(0o600)
                    .open(&cible)
                    .map_err(|e| format!("{}: {e}", c.display()))?;
                let copie = std::io::copy(&mut (&mut gz).take(taille), &mut sortie).map_err(|e| e.to_string())?;
                if copie != taille {
                    return Err("archive tronquée".into());
                }
                std::fs::set_permissions(&cible, std::fs::Permissions::from_mode(droits)).map_err(|e| e.to_string())?;
                fichiers.push(c);
            }
            t => return Err(format!("entrée refusée dans l'archive ({}) : {nom}", t as char)),
        }
        // Le contenu est complété à 512 octets.
        let reste = (512 - taille % 512) % 512;
        if type_ != b'0' && type_ != 0 && taille > 0 {
            return Err("archive douteuse (dossier avec contenu)".into());
        }
        std::io::copy(&mut (&mut gz).take(reste), &mut std::io::sink()).map_err(|e| e.to_string())?;
    }
    Ok(fichiers)
}

/// Les fichiers à remplacer : les exécutables du premier niveau de `source`
/// (jasette, jasette-serveur, les ponts, les scripts de connexion) qui
/// existent déjà dans `install` ou dont le nom commence par « jasette ».
fn a_remplacer(source: &Path, install: &Path) -> Result<Vec<String>, String> {
    let mut noms = Vec::new();
    for e in std::fs::read_dir(source).map_err(|e| e.to_string())?.flatten() {
        let Ok(m) = e.metadata() else { continue };
        let nom = e.file_name().to_string_lossy().into_owned();
        if m.is_file() && m.permissions().mode() & 0o111 != 0 && (nom.starts_with("jasette") || install.join(&nom).is_file()) {
            noms.push(nom);
        }
    }
    noms.sort();
    if !noms.iter().any(|n| n == "jasette") {
        return Err("l'archive ne contient pas jasette".into());
    }
    Ok(noms)
}

/// Remplace les exécutables de `install` par ceux de `source` (même système de
/// fichiers) : l'ancien est d'abord lié en `<nom>.ancien`, puis le nouveau
/// prend sa place par un renommage (atomique). Au moindre échec, tout revient
/// comme avant. Rend les noms remplacés.
pub fn remplacer(source: &Path, install: &Path) -> Result<Vec<String>, String> {
    let noms = a_remplacer(source, install)?;
    // 1. Les copies .ancien.
    for n in &noms {
        let actuel = install.join(n);
        if actuel.is_file() {
            let ancien = install.join(format!("{n}.ancien"));
            let _ = std::fs::remove_file(&ancien);
            if std::fs::hard_link(&actuel, &ancien).is_err() {
                std::fs::copy(&actuel, &ancien).map_err(|e| format!("{n}.ancien : {e}"))?;
            }
        }
    }
    // 2. Les renommages ; en cas d'échec, retour en arrière.
    let mut faits: Vec<(String, bool)> = Vec::new();
    for n in &noms {
        let cible = install.join(n);
        let existait = cible.is_file();
        if let Err(e) = std::fs::rename(source.join(n), &cible) {
            for (f, ex) in faits.iter().rev() {
                if *ex {
                    let _ = std::fs::rename(install.join(format!("{f}.ancien")), install.join(f));
                } else {
                    let _ = std::fs::remove_file(install.join(f));
                }
            }
            return Err(format!("{n} : {e}"));
        }
        faits.push((n.clone(), existait));
    }
    Ok(noms)
}

/// Le dossier temporaire d'extraction : à côté du dossier d'installation (même
/// système de fichiers, pour des renommages atomiques), sinon dedans.
pub fn dossier_extraction(install: &Path, code: u64) -> PathBuf {
    match install.parent() {
        Some(p) if inscriptible(p) => p.join(format!(".jasette-maj-{code}")),
        _ => install.join(format!(".maj-{code}")),
    }
}

// ---------------------------------------------------------------------------
// Le paquet .deb
// ---------------------------------------------------------------------------

/// La commande d'installation, devant `pkexec` : apt-get (qui règle les
/// dépendances), sinon dpkg -i.
pub fn commande_deb(fichier: &Path) -> Vec<String> {
    let f = fichier.display().to_string();
    if Path::new("/usr/bin/apt-get").is_file() {
        vec!["/usr/bin/apt-get".into(), "install".into(), "-y".into(), f]
    } else {
        vec!["/usr/bin/dpkg".into(), "-i".into(), f]
    }
}

/// Installe le .deb vérifié par `pkexec` : la fenêtre d'authentification du
/// système demande le mot de passe (jamais Jasette). Le SHA-256 est revérifié
/// juste avant, et le paquet doit s'appeler « jasette ».
pub fn installer_deb(fichier: &Path, sha256: &str) -> Result<(), String> {
    if sha256_fichier(fichier)? != sha256 {
        return Err("SHA-256 différent : fichier refusé".into());
    }
    if let Ok(o) = Command::new("dpkg-deb").arg("-f").arg(fichier).arg("Package").stdin(Stdio::null()).output()
        && o.status.success()
        && String::from_utf8_lossy(&o.stdout).trim() != "jasette"
    {
        return Err("ce paquet n'est pas Jasette".into());
    }
    let pkexec = essai("JASETTE_MAJ_PKEXEC").unwrap_or_else(|| "pkexec".into());
    let sortie = Command::new(pkexec)
        .args(commande_deb(fichier))
        .stdin(Stdio::null())
        .output()
        .map_err(|e| format!("pkexec : {e}"))?;
    match sortie.status.code() {
        Some(0) => Ok(()),
        Some(126) => Err("installation annulée".into()),
        Some(127) => Err("authentification refusée (ou pkexec absent)".into()),
        c => {
            let err = String::from_utf8_lossy(&sortie.stderr);
            let fin: Vec<&str> = err.lines().filter(|l| !l.trim().is_empty()).collect();
            let fin = fin[fin.len().saturating_sub(2)..].join(" ");
            Err(format!("échec de l'installation (code {}) {fin}", c.unwrap_or(-1)))
        }
    }
}

// ---------------------------------------------------------------------------
// Après l'installation
// ---------------------------------------------------------------------------

/// Relance Jasette (le nouvel exécutable, au même chemin) dès que ce
/// processus-ci est terminé ; à l'appelant de quitter ensuite.
pub fn relancer() -> Result<(), String> {
    use std::os::unix::process::CommandExt;
    let exe = chemin_exe().ok_or("exécutable introuvable")?;
    Command::new("/bin/sh")
        .arg("-c")
        .arg("while kill -0 \"$1\" 2>/dev/null; do sleep 0.2; done; shift; exec \"$@\"")
        .arg("sh")
        .arg(std::process::id().to_string())
        .arg(exe)
        .args(std::env::args_os().skip(1))
        .stdin(Stdio::null())
        .stdout(Stdio::null())
        .stderr(Stdio::null())
        .process_group(0)
        .spawn()
        .map(|_| ())
        .map_err(|e| e.to_string())
}

/// Le serveur Jasette tourne-t-il comme service systemd de l'utilisateur ?
pub fn serveur_systemd_actif() -> bool {
    Command::new("systemctl")
        .args(["--user", "is-active", "--quiet", "jasette-serveur"])
        .stdin(Stdio::null())
        .stdout(Stdio::null())
        .stderr(Stdio::null())
        .status()
        .is_ok_and(|s| s.success())
}

/// Un processus jasette-serveur tourne-t-il (service ou lancé à la main) ?
pub fn serveur_tourne() -> bool {
    Command::new("pgrep")
        .args(["-x", "jasette-serveur"])
        .stdin(Stdio::null())
        .stdout(Stdio::null())
        .stderr(Stdio::null())
        .status()
        .is_ok_and(|s| s.success())
}

pub fn redemarrer_serveur() -> Result<(), String> {
    let s = Command::new("systemctl")
        .args(["--user", "restart", "jasette-serveur"])
        .stdin(Stdio::null())
        .output()
        .map_err(|e| e.to_string())?;
    if s.status.success() { Ok(()) } else { Err(String::from_utf8_lossy(&s.stderr).trim().to_string()) }
}

#[cfg(test)]
mod essais {
    use super::*;

    const SHA: &str = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef";

    fn json(url_deb: &str) -> String {
        format!(
            r#"{{"versionCode": 10150, "versionName": "1.0.0 (150)",
                "notes": {{"fr": "Nouveautés", "en": "What's new"}},
                "deb": {{"url": "{url_deb}", "sha256": "{SHA}", "taille": 1234}},
                "targz": {{"url": "https://jonathanguernon.net/jasette/telecharger/jasette-1.0.0-linux-x86_64.tar.gz?v=10150", "sha256": "{}", "taille": 99}}}}"#,
            SHA.to_uppercase()
        )
    }

    /// Un dossier d'essai dans .cache du projet (jamais /tmp).
    fn dossier(nom: &str) -> PathBuf {
        let d = PathBuf::from(env!("CARGO_MANIFEST_DIR")).join(".cache/essais-maj").join(nom);
        let _ = std::fs::remove_dir_all(&d);
        std::fs::create_dir_all(&d).unwrap();
        d
    }

    #[test]
    fn comparaison_des_versions() {
        let v = lire(&json("https://jonathanguernon.net/jasette/telecharger/jasette_1.0.0_amd64.deb?v=10150"), false).unwrap();
        assert!(plus_recente(Some(&v), 10149));
        assert!(!plus_recente(Some(&v), 10150));
        assert!(!plus_recente(Some(&v), 10200));
        assert!(!plus_recente(None, 1));
        assert!(code_installe() >= 10_000);
    }

    #[test]
    fn lecture_de_version_linux() {
        let v = lire(&json("https://jonathanguernon.net/jasette/telecharger/jasette_1.0.0_amd64.deb?v=10150"), false).unwrap();
        assert_eq!(v.code, 10150);
        assert_eq!(v.nom, "1.0.0 (150)");
        assert_eq!(v.notes(false), "Nouveautés");
        assert_eq!(v.notes(true), "What's new");
        let deb = v.deb.unwrap();
        assert_eq!(deb.taille, 1234);
        assert_eq!(deb.sha256, SHA);
        // Le SHA-256 en majuscules est ramené en minuscules.
        assert_eq!(v.targz.unwrap().sha256, SHA);
        assert!(lire("pas du json", false).is_none());
        assert!(lire(r#"{"versionCode": 0, "deb": {}}"#, false).is_none());
        // Un SHA-256 trop court : le paquet est refusé, et sans paquet, la version aussi.
        assert!(lire(r#"{"versionCode": 5, "deb": {"url": "https://jonathanguernon.net/a.deb", "sha256": "abc"}}"#, false).is_none());
        // Le version.json d'Android (url, pas de deb ni targz) n'est pas une version Linux.
        assert!(lire(&format!(r#"{{"versionCode": 5, "url": "https://jonathanguernon.net/a.apk", "sha256": "{SHA}"}}"#), false).is_none());
    }

    #[test]
    fn refus_des_autres_hotes() {
        let refusees = [
            "http://jonathanguernon.net/jasette/a.deb",
            "https://jonathanguernon.net.evil.com/a.deb",
            "https://evil.com/jonathanguernon.net/a.deb",
            "https://www.jonathanguernon.net/a.deb",
            "https://jonathanguernon.net:8443/a.deb",
            "https://user@jonathanguernon.net/a.deb",
            "https://jonathanguernon.net@evil.com/a.deb",
            "https://jonathanguernon.net",
            "https://jonathanguernon.net/",
            "https://jonathanguernon.net/a b.deb",
            "ftp://jonathanguernon.net/a.deb",
            "http://127.0.0.1:8000/version-linux.json",
        ];
        for u in refusees {
            assert!(!adresse_sure(u, false), "{u}");
        }
        assert!(adresse_sure("https://jonathanguernon.net/jasette/telecharger/a.deb?v=3", false));
        assert!(adresse_sure("HTTPS://JonathanGuernon.net/jasette/a.deb", false));
        // Les essais (debug) : 127.0.0.1 en HTTP, rien d'autre.
        assert!(adresse_sure("http://127.0.0.1:8000/version-linux.json", true));
        assert!(!adresse_sure("http://127.0.0.1.evil.com:8000/x", true));
        assert!(!adresse_sure("http://127.0.0.1:80@evil.com/x", true));
        assert!(!adresse_sure("http://localhost:8000/x", true));
        // Un version-linux.json qui pointe ailleurs : le paquet est refusé.
        let v = lire(&json("https://evil.com/jasette_1.0.0_amd64.deb"), false).unwrap();
        assert!(v.deb.is_none() && v.targz.is_some());
        assert!(lire(&json("http://127.0.0.1:8000/a.deb"), false).unwrap().deb.is_none());
    }

    #[test]
    fn detection_de_l_installation() {
        let oui = |_: &Path| true;
        let non = |_: &Path| false;
        assert_eq!(detecter_chemin(Path::new("/x/Jasette/.cache/target/release/jasette"), oui), Installation::Dev);
        assert_eq!(detecter_chemin(Path::new("/x/Jasette/.cache/target-agent/debug/jasette"), oui), Installation::Dev);
        assert_eq!(detecter_chemin(Path::new("/usr/lib/jasette/jasette"), oui), Installation::Deb);
        assert_eq!(detecter_chemin(Path::new("/usr/lib/jasette/jasette"), non), Installation::Autre);
        let d = dossier("detection");
        assert_eq!(detecter_chemin(&d.join("jasette"), non), Installation::Archive(d.clone()));
    }

    /// Un en-tête tar (ustar) à la main, pour fabriquer des archives piégées.
    fn entete(nom: &str, taille: usize, type_: u8, mode: u32) -> [u8; 512] {
        let mut h = [0u8; 512];
        h[..nom.len()].copy_from_slice(nom.as_bytes());
        h[100..107].copy_from_slice(format!("{mode:07o}").as_bytes());
        h[124..135].copy_from_slice(format!("{taille:011o}").as_bytes());
        h[156] = type_;
        h[257..263].copy_from_slice(b"ustar\0");
        h[263..265].copy_from_slice(b"00");
        h[148..156].fill(b' ');
        let s: u32 = h.iter().map(|&b| b as u32).sum();
        h[148..155].copy_from_slice(format!("{s:06o}\0").as_bytes());
        h
    }

    fn archive(chemin: &Path, entrees: &[(&str, &[u8], u8, u32)]) {
        let mut brut = Vec::new();
        for (nom, contenu, t, mode) in entrees {
            brut.extend_from_slice(&entete(nom, contenu.len(), *t, *mode));
            brut.extend_from_slice(contenu);
            brut.resize(brut.len().div_ceil(512) * 512, 0);
        }
        brut.extend_from_slice(&[0u8; 1024]);
        let mut gz = flate2::write::GzEncoder::new(std::fs::File::create(chemin).unwrap(), flate2::Compression::fast());
        gz.write_all(&brut).unwrap();
        gz.finish().unwrap();
    }

    #[test]
    fn extraction_sure_de_l_archive() {
        let d = dossier("extraction");
        let bonne = d.join("bonne.tar.gz");
        archive(
            &bonne,
            &[
                ("jasette-1.0.0-linux-x86_64/", b"", b'5', 0o755),
                ("jasette-1.0.0-linux-x86_64/jasette", b"#!nouveau", b'0', 0o4755),
                ("jasette-1.0.0-linux-x86_64/jasette-serveur", b"serveur", b'0', 0o755),
                ("jasette-1.0.0-linux-x86_64/LICENSE", b"AGPL", b'0', 0o666),
                ("jasette-1.0.0-linux-x86_64/icons/16x16/apps/jasette.png", b"png", b'0', 0o644),
            ],
        );
        let sortie = d.join("sortie");
        let f = extraire_tgz(&bonne, &sortie).unwrap();
        assert_eq!(f.len(), 4);
        assert_eq!(std::fs::read(sortie.join("jasette")).unwrap(), b"#!nouveau");
        // Ni setuid ni écriture pour les autres.
        assert_eq!(std::fs::metadata(sortie.join("jasette")).unwrap().permissions().mode() & 0o7777, 0o755);
        assert_eq!(std::fs::metadata(sortie.join("LICENSE")).unwrap().permissions().mode() & 0o7777, 0o644);
        assert!(sortie.join("icons/16x16/apps/jasette.png").is_file());

        let piegees: [(&str, u8); 5] = [
            ("/etc/passwd", b'0'),
            ("jasette/../../evil", b'0'),
            ("jasette/a/../../../evil", b'0'),
            ("jasette/lien", b'2'),
            ("jasette/dur", b'1'),
        ];
        for (i, (nom, t)) in piegees.iter().enumerate() {
            let a = d.join(format!("piege{i}.tar.gz"));
            archive(&a, &[("jasette/ok", b"ok", b'0', 0o755), (nom, b"x", *t, 0o755)]);
            assert!(extraire_tgz(&a, &d.join(format!("p{i}"))).is_err(), "{nom}");
        }
        assert!(!d.join("evil").exists() && !Path::new("/etc/passwd-jasette").exists());
        let _ = std::fs::remove_dir_all(&d);
    }

    #[test]
    fn remplacement_atomique() {
        let d = dossier("remplacement");
        let install = d.join("lib/jasette");
        std::fs::create_dir_all(&install).unwrap();
        for (n, c) in [("jasette", "ancien"), ("jasette-pont", "ancien pont"), ("autre", "garde")] {
            std::fs::write(install.join(n), c).unwrap();
            std::fs::set_permissions(install.join(n), std::fs::Permissions::from_mode(0o755)).unwrap();
        }
        let a = d.join("maj.tar.gz");
        archive(
            &a,
            &[
                ("jasette-1.0.0-linux-x86_64/jasette", b"nouveau", b'0', 0o755),
                ("jasette-1.0.0-linux-x86_64/jasette-pont", b"nouveau pont", b'0', 0o755),
                ("jasette-1.0.0-linux-x86_64/jasette-x", b"nouveau x", b'0', 0o755),
                ("jasette-1.0.0-linux-x86_64/installer.sh", b"sh", b'0', 0o755),
                ("jasette-1.0.0-linux-x86_64/LICENSE", b"AGPL", b'0', 0o644),
            ],
        );
        let tmp = dossier_extraction(&install, 10150);
        extraire_tgz(&a, &tmp).unwrap();
        let noms = remplacer(&tmp, &install).unwrap();
        assert_eq!(noms, ["jasette", "jasette-pont", "jasette-x"]);
        assert_eq!(std::fs::read_to_string(install.join("jasette")).unwrap(), "nouveau");
        assert_eq!(std::fs::read_to_string(install.join("jasette.ancien")).unwrap(), "ancien");
        assert_eq!(std::fs::read_to_string(install.join("jasette-pont.ancien")).unwrap(), "ancien pont");
        assert_eq!(std::fs::read_to_string(install.join("autre")).unwrap(), "garde");
        assert!(!install.join("installer.sh").exists() && !install.join("LICENSE").exists());
        let _ = std::fs::remove_dir_all(&d);
    }
}
