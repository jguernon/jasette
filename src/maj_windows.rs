//! La mise à jour dans l'app, pour Windows (à la place de maj.rs, voir main.rs).
//!
//! Même interface que maj.rs (main.rs ne change pas) :
//! 1. `chercher` lit `version-windows.json` sur le site (HTTPS, seulement
//!    https://jonathanguernon.net, sans redirection).
//! 2. Selon `Installation` :
//!    - dossier modifiable par l'utilisateur (le zip portable, ou l'installateur,
//!      qui installe dans %LOCALAPPDATA%\Programs\Jasette) : `telecharger` le zip
//!      dans `<données>\maj\`, SHA-256 vérifié, `extraire_tgz` (ici : le zip)
//!      dans un dossier temporaire à côté (sans chemin absolu, sans `..`), puis
//!      `remplacer` : chaque fichier en cours d'usage est renommé en
//!      `<nom>.ancien` (Windows permet de renommer un .exe qui tourne, pas de
//!      l'écraser), puis le nouveau prend sa place ; les `.ancien` sont effacés
//!      au lancement suivant ;
//!    - version de développement (dossier target de cargo) : rien d'automatique ;
//!    - ailleurs (Program Files, sans droit d'écriture) : la page de téléchargement.
//!
//! Dans version-windows.json, le paquet est sous la clé « zip » ; il est rangé
//! dans le champ `targz` de `VersionDispo` (le paquet de l'« archive »).
//! Rien de téléchargé n'est exécuté avant la vérification du SHA-256.

use std::io::{Read, Seek, SeekFrom, Write};
use std::path::{Component, Path, PathBuf};
use std::process::Stdio;
use std::sync::OnceLock;
use std::sync::atomic::{AtomicU64, Ordering};
use std::time::Duration;

use serde::{Deserialize, Serialize};
use serde_json::Value;

/// Le seul hôte d'où Jasette accepte une mise à jour (en HTTPS).
pub const HOTE: &str = "jonathanguernon.net";
pub const ADRESSE_VERSION: &str = "https://jonathanguernon.net/jasette/telecharger/version-windows.json";
/// La page de téléchargement, pour une installation que Jasette ne sait pas mettre à jour.
pub const PAGE: &str = "https://jonathanguernon.net/jasette/";
pub const INTERVALLE_MS: i64 = 6 * 3600 * 1000;
const TAILLE_MAX: u64 = 400 << 20;
const EXTRAIT_MAX: u64 = 1 << 30;

pub fn code_installe() -> u64 {
    env!("JASETTE_CODE").parse().unwrap_or(0)
}

pub fn nom_installe() -> &'static str {
    env!("JASETTE_NOM")
}

fn essai(var: &str) -> Option<String> {
    if cfg!(debug_assertions) { std::env::var(var).ok().filter(|v| !v.is_empty()) } else { None }
}

pub fn local_permis() -> bool {
    essai("JASETTE_MAJ_URL").is_some()
}

pub fn adresse_version() -> String {
    essai("JASETTE_MAJ_URL").unwrap_or_else(|| ADRESSE_VERSION.to_string())
}

#[derive(Clone, Debug, PartialEq)]
pub struct Paquet {
    pub url: String,
    pub sha256: String,
    pub taille: u64,
}

/// La version publiée (`version-windows.json`, écrit par paquets/empaqueter-windows.sh) :
///
/// ```json
/// {"versionCode": 10123, "versionName": "1.0.1 (123)",
///  "notes": {"fr": "…", "en": "…"},
///  "zip":   {"url": "https://jonathanguernon.net/jasette/telecharger/jasette-1.0.1-windows-x86_64.zip?v=10123",
///            "sha256": "…", "taille": 41234567},
///  "setup": {"url": "…/jasette-1.0.1-windows-x86_64-setup.exe?v=10123", "sha256": "…", "taille": 40123456}}
/// ```
#[derive(Clone, Debug, PartialEq)]
pub struct VersionDispo {
    pub code: u64,
    pub nom: String,
    pub notes_fr: String,
    pub notes_en: String,
    /// Toujours None sous Windows (gardé pour l'interface commune).
    pub deb: Option<Paquet>,
    /// Le zip de Windows.
    pub targz: Option<Paquet>,
}

impl VersionDispo {
    pub fn notes(&self, anglais: bool) -> &str {
        let (a, b) = if anglais { (&self.notes_en, &self.notes_fr) } else { (&self.notes_fr, &self.notes_en) };
        if a.trim().is_empty() { b } else { a }
    }
}

fn hex64(s: &str) -> bool {
    s.len() == 64 && s.bytes().all(|b| b.is_ascii_digit() || (b'a'..=b'f').contains(&b))
}

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

pub fn lire(json: &str, local: bool) -> Option<VersionDispo> {
    let v: Value = serde_json::from_str(json).ok()?;
    let code = v.get("versionCode")?.as_u64().filter(|c| *c > 0)?;
    let zip = lire_paquet(v.get("zip"), local)?;
    let notes = |l: &str| v.get("notes").and_then(|n| n.get(l)).and_then(Value::as_str).unwrap_or("").trim().to_string();
    let nom = v.get("versionName").and_then(Value::as_str).unwrap_or("").trim();
    Some(VersionDispo {
        code,
        nom: if nom.is_empty() { code.to_string() } else { nom.to_string() },
        notes_fr: notes("fr"),
        notes_en: notes("en"),
        deb: None,
        targz: Some(zip),
    })
}

pub fn plus_recente(publiee: Option<&VersionDispo>, installee: u64) -> bool {
    publiee.is_some_and(|p| p.code > installee)
}

#[derive(Clone, Debug, Default, Serialize, Deserialize)]
pub struct Memoire {
    #[serde(default)]
    pub derniere_verif: i64,
    #[serde(default)]
    pub ignoree: u64,
    #[serde(default)]
    pub json: String,
}

pub fn creer_dossier(d: &Path) -> std::io::Result<()> {
    std::fs::create_dir_all(d)
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

/// Vide `<données>\maj\` de ses téléchargements (etat.json reste), et efface
/// les `.ancien` laissés à côté de l'exécutable par la dernière mise à jour.
pub fn nettoyer(dossier: &Path) {
    if let Ok(l) = std::fs::read_dir(dossier) {
        for e in l.flatten() {
            if e.file_name() != "etat.json" {
                let p = e.path();
                let _ = if p.is_dir() { std::fs::remove_dir_all(&p) } else { std::fs::remove_file(&p) };
            }
        }
    }
    if let Some(dir) = chemin_exe().as_deref().and_then(Path::parent)
        && let Ok(l) = std::fs::read_dir(dir)
    {
        for e in l.flatten() {
            if e.file_name().to_string_lossy().contains(".ancien") {
                let _ = std::fs::remove_file(e.path());
            }
        }
    }
}

#[derive(Clone, Debug, PartialEq)]
pub enum Installation {
    /// Jamais sous Windows (gardé pour l'interface commune).
    Deb,
    /// Un dossier de l'utilisateur (zip ou installateur), ses binaires à côté.
    Archive(PathBuf),
    Dev,
    Autre,
}

pub fn chemin_exe() -> Option<PathBuf> {
    static EXE: OnceLock<Option<PathBuf>> = OnceLock::new();
    EXE.get_or_init(|| std::env::current_exe().ok()).clone()
}

fn inscriptible(dossier: &Path) -> bool {
    let essai = dossier.join(format!(".jasette-ecriture-{}", std::process::id()));
    let ok = std::fs::OpenOptions::new().write(true).create_new(true).open(&essai).is_ok();
    let _ = std::fs::remove_file(&essai);
    ok
}

fn dans_cargo(exe: &Path) -> bool {
    let dir = exe.parent().unwrap_or(Path::new("\\"));
    exe.components().any(|c| matches!(c, Component::Normal(n) if n.to_string_lossy().starts_with("target")))
        || (dir.join(".fingerprint").is_dir() && dir.join("deps").is_dir())
}

pub fn detecter_chemin(exe: &Path) -> Installation {
    if dans_cargo(exe) {
        return Installation::Dev;
    }
    match exe.parent() {
        Some(d) if inscriptible(d) => Installation::Archive(d.to_path_buf()),
        _ => Installation::Autre,
    }
}

pub fn detecter() -> Installation {
    match chemin_exe() {
        Some(exe) => detecter_chemin(&exe),
        None => Installation::Autre,
    }
}

fn agent() -> ureq::Agent {
    ureq::AgentBuilder::new()
        .timeout_connect(Duration::from_secs(8))
        .timeout_read(Duration::from_secs(30))
        .redirects(0)
        .user_agent(&format!("Jasette-Windows/{}", nom_installe()))
        .try_proxy_from_env(false)
        .build()
}

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
    let v = lire(&texte, local).ok_or("version-windows.json illisible")?;
    Ok((v, texte))
}

fn hex(octets: &[u8]) -> String {
    octets.iter().map(|b| format!("{b:02x}")).collect()
}

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
        let mut f = std::fs::OpenOptions::new().write(true).create_new(true).open(&part).map_err(|e| e.to_string())?;
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
        drop(f);
        if p.taille > 0 && total != p.taille {
            return Err("téléchargement incomplet".into());
        }
        if hex(ctx.finish().as_ref()) != p.sha256 {
            return Err("SHA-256 différent : fichier refusé".into());
        }
        let _ = std::fs::remove_file(dest);
        std::fs::rename(&part, dest).map_err(|e| e.to_string())
    })();
    if resultat.is_err() {
        let _ = std::fs::remove_file(&part);
    }
    resultat
}

// ---------------------------------------------------------------------------
// Le zip
// ---------------------------------------------------------------------------

/// Un chemin d'archive sûr : relatif, sans `..`, sans lecteur ni racine. Le
/// premier élément (le dossier « jasette-1.0.1-windows-x86_64 ») est retiré.
fn chemin_sur(nom: &str) -> Result<Option<PathBuf>, String> {
    let refuse = || format!("chemin refusé dans l'archive : {nom}");
    if nom.is_empty() || nom.starts_with('/') || nom.contains('\\') || nom.contains('\0') || nom.contains(':') {
        return Err(refuse());
    }
    let mut r = PathBuf::new();
    let mut premier = true;
    for morceau in nom.split('/') {
        match morceau {
            "" | "." => {}
            ".." => return Err(refuse()),
            m => {
                if premier {
                    premier = false;
                } else {
                    r.push(m);
                }
            }
        }
    }
    Ok((!r.as_os_str().is_empty()).then_some(r))
}

fn u16le(b: &[u8], i: usize) -> u16 {
    u16::from_le_bytes([b[i], b[i + 1]])
}

fn u32le(b: &[u8], i: usize) -> u32 {
    u32::from_le_bytes([b[i], b[i + 1], b[i + 2], b[i + 3]])
}

/// Extrait le zip dans `dest` (recréé vide), sans le dossier de tête. Lit le
/// répertoire central ; seulement « stocké » ou « deflate », sans chiffrement
/// ni zip64 ; chaque CRC-32 est vérifié. Rend les fichiers extraits.
/// (Le nom vient de maj.rs : main.rs l'appelle ainsi pour l'archive.)
pub fn extraire_tgz(archive: &Path, dest: &Path) -> Result<Vec<PathBuf>, String> {
    if dest.exists() {
        std::fs::remove_dir_all(dest).map_err(|e| e.to_string())?;
    }
    creer_dossier(dest).map_err(|e| e.to_string())?;
    let mut f = std::fs::File::open(archive).map_err(|e| e.to_string())?;
    let longueur = f.metadata().map_err(|e| e.to_string())?.len();
    // La fin du répertoire central (22 octets, plus un commentaire de 64 Ko au plus).
    let queue = longueur.min(22 + 65535);
    f.seek(SeekFrom::Start(longueur - queue)).map_err(|e| e.to_string())?;
    let mut fin = vec![0u8; queue as usize];
    f.read_exact(&mut fin).map_err(|e| e.to_string())?;
    let pos = (0..fin.len().saturating_sub(21)).rev().find(|&i| u32le(&fin, i) == 0x0605_4b50).ok_or("zip illisible")?;
    let nombre = u16le(&fin, pos + 10) as usize;
    let taille_rep = u32le(&fin, pos + 12) as u64;
    let debut_rep = u32le(&fin, pos + 16) as u64;
    if debut_rep + taille_rep > longueur || nombre == 0xffff {
        return Err("zip illisible (zip64 ?)".into());
    }
    f.seek(SeekFrom::Start(debut_rep)).map_err(|e| e.to_string())?;
    let mut rep = vec![0u8; taille_rep as usize];
    f.read_exact(&mut rep).map_err(|e| e.to_string())?;
    let mut fichiers = Vec::new();
    let mut total = 0u64;
    let mut i = 0usize;
    for _ in 0..nombre {
        if i + 46 > rep.len() || u32le(&rep, i) != 0x0201_4b50 {
            return Err("zip abîmé (répertoire)".into());
        }
        let drapeaux = u16le(&rep, i + 8);
        let methode = u16le(&rep, i + 10);
        let crc = u32le(&rep, i + 16);
        let comprime = u32le(&rep, i + 20) as u64;
        let taille = u32le(&rep, i + 24) as u64;
        let (n, x, c) = (u16le(&rep, i + 28) as usize, u16le(&rep, i + 30) as usize, u16le(&rep, i + 32) as usize);
        let local = u32le(&rep, i + 42) as u64;
        let nom = String::from_utf8_lossy(rep.get(i + 46..i + 46 + n).ok_or("zip abîmé")?).into_owned();
        i += 46 + n + x + c;
        if drapeaux & 1 != 0 {
            return Err("zip chiffré refusé".into());
        }
        total += taille;
        if total > EXTRAIT_MAX {
            return Err("archive trop grosse".into());
        }
        let chemin = chemin_sur(&nom)?;
        if nom.ends_with('/') {
            if let Some(c) = chemin {
                creer_dossier(&dest.join(c)).map_err(|e| e.to_string())?;
            }
            continue;
        }
        let c = chemin.ok_or_else(|| format!("chemin refusé dans l'archive : {nom}"))?;
        // L'en-tête local : 30 octets, son nom et son champ supplémentaire.
        let mut entete = [0u8; 30];
        f.seek(SeekFrom::Start(local)).map_err(|e| e.to_string())?;
        f.read_exact(&mut entete).map_err(|e| e.to_string())?;
        if u32le(&entete, 0) != 0x0403_4b50 {
            return Err("zip abîmé (en-tête)".into());
        }
        let saut = 30 + u16le(&entete, 26) as u64 + u16le(&entete, 28) as u64;
        f.seek(SeekFrom::Start(local + saut)).map_err(|e| e.to_string())?;
        let brut = (&mut f).take(comprime);
        let mut lecteur: Box<dyn Read> = match methode {
            0 => Box::new(brut),
            8 => Box::new(flate2::read::DeflateDecoder::new(brut)),
            m => return Err(format!("méthode de compression {m} refusée")),
        };
        let cible = dest.join(&c);
        if let Some(p) = cible.parent() {
            creer_dossier(p).map_err(|e| e.to_string())?;
        }
        let mut sortie = std::fs::OpenOptions::new()
            .write(true)
            .create_new(true)
            .open(&cible)
            .map_err(|e| format!("{}: {e}", c.display()))?;
        let mut crc_lu = flate2::Crc::new();
        let mut tampon = vec![0u8; 64 * 1024];
        let mut ecrit = 0u64;
        loop {
            let k = lecteur.read(&mut tampon).map_err(|e| e.to_string())?;
            if k == 0 {
                break;
            }
            ecrit += k as u64;
            if ecrit > taille {
                return Err("zip douteux (taille)".into());
            }
            crc_lu.update(&tampon[..k]);
            sortie.write_all(&tampon[..k]).map_err(|e| e.to_string())?;
        }
        if ecrit != taille || crc_lu.sum() != crc {
            return Err(format!("zip abîmé : {}", c.display()));
        }
        fichiers.push(c);
    }
    Ok(fichiers)
}

/// Les fichiers à remplacer : ceux du premier niveau de `source` (jasette.exe,
/// les ponts, jasette-connexion.exe, les notices).
fn a_remplacer(source: &Path) -> Result<Vec<String>, String> {
    let mut noms = Vec::new();
    for e in std::fs::read_dir(source).map_err(|e| e.to_string())?.flatten() {
        if e.metadata().is_ok_and(|m| m.is_file()) {
            noms.push(e.file_name().to_string_lossy().into_owned());
        }
    }
    noms.sort();
    if !noms.iter().any(|n| n.eq_ignore_ascii_case("jasette.exe")) {
        return Err("l'archive ne contient pas jasette.exe".into());
    }
    Ok(noms)
}

/// Un nom libre pour mettre de côté `n` (un ancien .ancien peut être encore
/// ouvert, s'il tourne toujours : on prend alors .ancien2, .ancien3…).
fn nom_ancien(install: &Path, n: &str) -> PathBuf {
    for k in 0..20 {
        let p = install.join(if k == 0 { format!("{n}.ancien") } else { format!("{n}.ancien{k}") });
        let _ = std::fs::remove_file(&p);
        if !p.exists() {
            return p;
        }
    }
    install.join(format!("{n}.ancien-{}", std::process::id()))
}

/// Remplace les fichiers de `install` par ceux de `source` : l'actuel est
/// renommé en `.ancien` (permis même s'il tourne), puis le nouveau prend sa
/// place. Au moindre échec, tout revient comme avant.
pub fn remplacer(source: &Path, install: &Path) -> Result<Vec<String>, String> {
    let noms = a_remplacer(source)?;
    let mut faits: Vec<(String, Option<PathBuf>)> = Vec::new();
    let defaire = |faits: &[(String, Option<PathBuf>)]| {
        for (f, ancien) in faits.iter().rev() {
            let _ = std::fs::remove_file(install.join(f));
            if let Some(a) = ancien {
                let _ = std::fs::rename(a, install.join(f));
            }
        }
    };
    for n in &noms {
        let cible = install.join(n);
        let ancien = if cible.is_file() {
            let a = nom_ancien(install, n);
            if let Err(e) = std::fs::rename(&cible, &a) {
                defaire(&faits);
                return Err(format!("{n} : {e}"));
            }
            Some(a)
        } else {
            None
        };
        if let Err(e) = std::fs::rename(source.join(n), &cible) {
            if let Some(a) = &ancien {
                let _ = std::fs::rename(a, &cible);
            }
            defaire(&faits);
            return Err(format!("{n} : {e}"));
        }
        faits.push((n.clone(), ancien));
    }
    Ok(noms)
}

pub fn dossier_extraction(install: &Path, code: u64) -> PathBuf {
    match install.parent() {
        Some(p) if inscriptible(p) => p.join(format!(".jasette-maj-{code}")),
        _ => install.join(format!(".maj-{code}")),
    }
}

pub fn installer_deb(_fichier: &Path, _sha256: &str) -> Result<(), String> {
    Err("pas de paquet .deb sous Windows".into())
}

/// Relance Jasette (le nouvel exécutable, au même chemin) dès que ce
/// processus-ci est terminé : PowerShell attend sa fin, puis le lance.
pub fn relancer() -> Result<(), String> {
    let exe = chemin_exe().ok_or("exécutable introuvable")?;
    let args: Vec<String> = std::env::args().skip(1).collect();
    crate::windows::commande("powershell.exe")
        .args([
            "-NoProfile",
            "-NonInteractive",
            "-WindowStyle",
            "Hidden",
            "-Command",
            "Wait-Process -Id $env:JASETTE_PID -ErrorAction SilentlyContinue; \
             if ($env:JASETTE_ARGS) { Start-Process -FilePath $env:JASETTE_EXE -ArgumentList $env:JASETTE_ARGS } \
             else { Start-Process -FilePath $env:JASETTE_EXE }",
        ])
        .env("JASETTE_PID", std::process::id().to_string())
        .env("JASETTE_EXE", &exe)
        .env("JASETTE_ARGS", args.join(" "))
        .stdin(Stdio::null())
        .stdout(Stdio::null())
        .stderr(Stdio::null())
        .spawn()
        .map(|_| ())
        .map_err(|e| e.to_string())
}

/// Pas de serveur Jasette sous Windows.
pub fn serveur_systemd_actif() -> bool {
    false
}

pub fn serveur_tourne() -> bool {
    false
}

pub fn redemarrer_serveur() -> Result<(), String> {
    Err("pas de serveur Jasette sous Windows".into())
}
