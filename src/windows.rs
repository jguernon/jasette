//! Ce qui est propre à Windows (compilé seulement pour Windows, voir main.rs).
//!
//! Le reste de Jasette lit les dossiers de la norme XDG (XDG_DATA_HOME,
//! XDG_CONFIG_HOME, HOME), comme ses ponts Go : `preparer` les fait pointer,
//! au tout début du programme, vers les dossiers standards de Windows. Les
//! ponts, lancés ensuite, en héritent. Ainsi :
//!   données (réglages, sessions des ponts, médias) : %LOCALAPPDATA%\jasette
//!   configuration                                  : %APPDATA%
//!
//! Le reste : ouvrir une adresse ou un fichier (ShellExecuteW), les
//! notifications (une « toast » par PowerShell), le démarrage avec Windows
//! (clé Run du registre), le nom de la machine, et la fenêtre de connexion
//! (jasette-connexion.exe, WebView2) à la place des scripts Python GTK.

use std::ffi::{OsStr, OsString};
use std::os::windows::ffi::OsStrExt;
use std::os::windows::process::CommandExt;
use std::path::PathBuf;
use std::process::{Command, Stdio};

/// Pas de fenêtre de console pour un programme lancé (les ponts sont des
/// programmes « console » ; Jasette, elle, n'en a pas).
pub const CREATE_NO_WINDOW: u32 = 0x0800_0000;

/// L'identifiant d'app (AppUserModelID) : celui du processus, du raccourci
/// du menu Démarrer posé par l'installateur (paquets/windows/jasette.iss) et
/// de la clé HKCU\Software\Classes\AppUserModelId\<AUMID> (nom et icône des
/// notifications, aussi pour la version zip, sans raccourci).
pub const AUMID: &str = "JonathanGuernon.Jasette";

/// L'AUMID est-il inscrit dans le registre ? (sinon, les notifications passent
/// par celui de PowerShell, toujours connu de Windows)
static AUMID_INSCRIT: std::sync::atomic::AtomicBool = std::sync::atomic::AtomicBool::new(false);

fn large(s: &OsStr) -> Vec<u16> {
    s.encode_wide().chain(std::iter::once(0)).collect()
}

fn var(nom: &str) -> Option<OsString> {
    std::env::var_os(nom).filter(|v| !v.is_empty())
}

/// À appeler au tout début de main(), avant tout fil : les dossiers, le
/// journal (version sans console) et l'identifiant d'app.
pub fn preparer() {
    let local = var("LOCALAPPDATA").or_else(|| var("USERPROFILE").map(|h| PathBuf::from(h).join("AppData\\Local").into()));
    let itinerant = var("APPDATA").or_else(|| var("USERPROFILE").map(|h| PathBuf::from(h).join("AppData\\Roaming").into()));
    // SAFETY : appelé en premier dans main(), quand aucun autre fil ne tourne.
    unsafe {
        if var("XDG_DATA_HOME").is_none()
            && let Some(l) = &local
        {
            std::env::set_var("XDG_DATA_HOME", l);
        }
        if var("XDG_CONFIG_HOME").is_none()
            && let Some(a) = &itinerant
        {
            std::env::set_var("XDG_CONFIG_HOME", a);
        }
        if var("HOME").is_none()
            && let Some(h) = var("USERPROFILE")
        {
            std::env::set_var("HOME", h);
        }
        if var("USER").is_none()
            && let Some(u) = var("USERNAME")
        {
            std::env::set_var("USER", u);
        }
        let id = large(OsStr::new(AUMID));
        let _ = windows_sys::Win32::UI::Shell::SetCurrentProcessExplicitAppUserModelID(id.as_ptr());
    }
    // La version publiée n'a pas de console : ses journaux (et ceux des ponts,
    // qui héritent de la sortie d'erreur) vont dans %LOCALAPPDATA%\jasette\jasette.log.
    if !cfg!(debug_assertions) || var("JASETTE_JOURNAL").is_some() {
        journal();
    }
    std::thread::spawn(inscrire_aumid);
}

/// Inscrit l'AUMID (nom « Jasette » et icône) pour les notifications.
fn inscrire_aumid() {
    let Some(base) = var("XDG_DATA_HOME") else { return };
    let dossier = PathBuf::from(base).join("jasette");
    let icone = dossier.join("jasette-notification.png");
    if std::fs::create_dir_all(&dossier).is_err()
        || std::fs::write(&icone, include_bytes!("../assets/png/jasette-256.png")).is_err()
    {
        return;
    }
    let cle = format!(r"HKCU\Software\Classes\AppUserModelId\{AUMID}");
    let ok = [("DisplayName", "Jasette".to_string()), ("IconUri", icone.display().to_string())].into_iter().all(|(nom, valeur)| {
        commande("reg.exe")
            .args(["add", &cle, "/v", nom, "/t", "REG_SZ", "/f", "/d", &valeur])
            .stdin(Stdio::null())
            .stdout(Stdio::null())
            .stderr(Stdio::null())
            .status()
            .is_ok_and(|s| s.success())
    });
    AUMID_INSCRIT.store(ok, std::sync::atomic::Ordering::Relaxed);
}

fn journal() {
    use std::os::windows::io::IntoRawHandle;
    let Some(base) = var("XDG_DATA_HOME") else { return };
    let dossier = PathBuf::from(base).join("jasette");
    if std::fs::create_dir_all(&dossier).is_err() {
        return;
    }
    let f = dossier.join("jasette.log");
    // Au-delà de 4 Mo, l'ancien journal devient jasette.log.1.
    if std::fs::metadata(&f).is_ok_and(|m| m.len() > 4 << 20) {
        let _ = std::fs::rename(&f, dossier.join("jasette.log.1"));
    }
    let Ok(fichier) = std::fs::OpenOptions::new().create(true).append(true).open(&f) else { return };
    let h = fichier.into_raw_handle();
    // SAFETY : un handle de fichier valide, gardé ouvert jusqu'à la fin.
    unsafe {
        windows_sys::Win32::System::Console::SetStdHandle(windows_sys::Win32::System::Console::STD_ERROR_HANDLE, h as _);
    }
    eprintln!("---- jasette {} ({})", env!("JASETTE_NOM"), chrono::Local::now().format("%Y-%m-%d %H:%M:%S"));
}

/// Une commande sans fenêtre de console.
pub fn commande(programme: impl AsRef<OsStr>) -> Command {
    let mut c = Command::new(programme);
    c.creation_flags(CREATE_NO_WINDOW);
    c
}

/// Ouvre une adresse (https:, tel:, mailto:) ou un fichier avec l'application par défaut.
pub fn ouvrir(cible: &OsStr) {
    let op = large(OsStr::new("open"));
    let f = large(cible);
    // SAFETY : chaînes terminées par un zéro, gardées vivantes pendant l'appel.
    unsafe {
        windows_sys::Win32::UI::Shell::ShellExecuteW(
            std::ptr::null_mut(),
            op.as_ptr(),
            f.as_ptr(),
            std::ptr::null(),
            std::ptr::null(),
            windows_sys::Win32::UI::WindowsAndMessaging::SW_SHOWNORMAL,
        );
    }
}

/// L'AppUserModelID de PowerShell : toujours connu de Windows (version portable).
const AUMID_POWERSHELL: &str = "{1AC14E77-02E7-4E5D-B744-2EB1AE5198B7}\\WindowsPowerShell\\v1.0\\powershell.exe";

/// Une notification (« toast ») de Windows, par PowerShell (aucune dépendance).
/// Le titre et le texte passent par l'environnement, jamais dans le script.
/// Bloque le temps de l'afficher : à appeler depuis un fil à part.
pub fn notifier(titre: &str, corps: &str, image: Option<&str>) {
    const SCRIPT: &str = r#"
$ErrorActionPreference = 'Stop'
[void][Windows.UI.Notifications.ToastNotificationManager, Windows.UI.Notifications, ContentType = WindowsRuntime]
[void][Windows.Data.Xml.Dom.XmlDocument, Windows.Data.Xml.Dom.XmlDocument, ContentType = WindowsRuntime]
$e = [Security.SecurityElement]
$t = $e::Escape($env:JASETTE_TITRE); $c = $e::Escape($env:JASETTE_CORPS)
$img = ''
if ($env:JASETTE_IMAGE) { $img = '<image placement="appLogoOverride" hint-crop="circle" src="' + $e::Escape($env:JASETTE_IMAGE) + '"/>' }
$x = New-Object Windows.Data.Xml.Dom.XmlDocument
$x.LoadXml('<toast><visual><binding template="ToastGeneric"><text>' + $t + '</text><text>' + $c + '</text>' + $img + '</binding></visual></toast>')
[Windows.UI.Notifications.ToastNotificationManager]::CreateToastNotifier($env:JASETTE_AUMID).Show([Windows.UI.Notifications.ToastNotification]::new($x))
"#;
    let encode: Vec<u8> = SCRIPT.encode_utf16().flat_map(u16::to_le_bytes).collect();
    use base64::Engine as _;
    let b64 = base64::engine::general_purpose::STANDARD.encode(encode);
    let aumid = if AUMID_INSCRIT.load(std::sync::atomic::Ordering::Relaxed) { AUMID } else { AUMID_POWERSHELL };
    let _ = commande("powershell.exe")
        .args(["-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-EncodedCommand", &b64])
        .env("JASETTE_TITRE", titre)
        .env("JASETTE_CORPS", corps)
        .env("JASETTE_IMAGE", image.unwrap_or(""))
        .env("JASETTE_AUMID", aumid)
        .stdin(Stdio::null())
        .stdout(Stdio::null())
        .stderr(Stdio::null())
        .status();
}

const CLE_RUN: &str = r"HKCU\Software\Microsoft\Windows\CurrentVersion\Run";

/// Jasette s'ouvre-t-elle avec Windows ? (la valeur « Jasette » de la clé Run)
pub fn demarrage_auto() -> bool {
    commande("reg.exe")
        .args(["query", CLE_RUN, "/v", "Jasette"])
        .stdin(Stdio::null())
        .stdout(Stdio::null())
        .stderr(Stdio::null())
        .status()
        .is_ok_and(|s| s.success())
}

pub fn regler_demarrage_auto(actif: bool) -> Result<(), String> {
    let mut c = commande("reg.exe");
    if actif {
        let exe = std::env::current_exe().map_err(|e| e.to_string())?;
        c.args(["add", CLE_RUN, "/v", "Jasette", "/t", "REG_SZ", "/f", "/d"]).arg(format!("\"{}\"", exe.display()));
    } else {
        if !demarrage_auto() {
            return Ok(());
        }
        c.args(["delete", CLE_RUN, "/v", "Jasette", "/f"]);
    }
    let s = c.stdin(Stdio::null()).output().map_err(|e| e.to_string())?;
    if s.status.success() { Ok(()) } else { Err(String::from_utf8_lossy(&s.stderr).trim().to_string()) }
}

pub fn nom_machine() -> String {
    std::env::var("COMPUTERNAME").ok().map(|s| s.trim().to_string()).filter(|s| !s.is_empty()).unwrap_or_else(|| "Jasette".into())
}

/// La fenêtre de connexion de Windows : jasette-connexion.exe (WebView2), à
/// côté de l'exécutable. Elle prend les mêmes arguments que les scripts
/// Python (le nom du script, puis l'option) et répond de la même façon : une
/// ligne JSON {"cookies": …} sur sa sortie standard.
pub fn fenetre_connexion() -> Option<PathBuf> {
    if let Some(p) = var("JASETTE_CONNEXION_EXE") {
        return Some(PathBuf::from(p));
    }
    let exe = std::env::current_exe().ok()?;
    Some(exe.parent()?.join("jasette-connexion.exe")).filter(|p| p.is_file())
}

/// Les textes qui changent sous Windows (le reste passe par `t` comme d'habitude).
pub fn traduire(fr: &str, anglais: bool) -> Option<&'static str> {
    Some(match (fr, anglais) {
        ("archive .tar.gz", false) => "archive .zip",
        ("archive .tar.gz", true) => ".zip archive",
        ("Le serveur Jasette n'est pas offert sous Windows : installez-le sur une machine Linux, puis branchez-vous-y.", true) => {
            "The Jasette server isn't available on Windows: install it on a Linux machine, then connect to it."
        }
        _ => return None,
    })
}
