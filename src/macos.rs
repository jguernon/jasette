//! Ce qui est propre à macOS (compilé seulement pour macOS).
//!
//! La version Linux n'en dépend pas : chaque appel vient d'un bloc
//! `#[cfg(target_os = "macos")]` de main.rs, maj.rs ou prive.rs, qui remplace
//! l'outil Linux correspondant :
//!   - données : ~/Library/Application Support/Jasette (JASETTE_DATA, que les
//!     ponts reçoivent aussi), au lieu de ~/.local/share/jasette ;
//!   - liens et fichiers : `open` au lieu de xdg-open ;
//!   - notifications : `osascript` (display notification) au lieu de notify-send ;
//!   - vocaux : ffplay/ffmpeg s'ils sont installés (Homebrew), sinon afplay ;
//!   - démarrage avec l'ordinateur : un LaunchAgent au lieu du lanceur XDG ;
//!   - fenêtres de connexion : jasette-connexion (WKWebView, macos/connexion)
//!     au lieu des scripts Python GTK ;
//!   - serveur Jasette (systemd) : non offert, le mode client marche.

use std::path::{Path, PathBuf};
use std::process::{Command, Stdio};

/// Le dossier de données de Jasette sur macOS.
pub fn dossier_donnees() -> PathBuf {
    let home = std::env::var_os("HOME").map(PathBuf::from).unwrap_or_else(|| PathBuf::from("."));
    home.join("Library/Application Support/Jasette")
}

/// Au tout début de main(), avant tout fil : les variables d'environnement
/// que Jasette et ses ponts (processus enfants) liront.
pub fn preparer() {
    // Les ponts Go cherchent leurs données dans JASETTE_DATA (sinon
    // ~/.local/share/jasette) : tout le monde dans le même dossier macOS.
    if std::env::var_os("JASETTE_DATA").is_none() {
        let d = dossier_donnees();
        let _ = std::fs::create_dir_all(&d);
        // SAFETY : appelé au début de main(), avant la création de tout fil.
        unsafe { std::env::set_var("JASETTE_DATA", &d) };
    }
    // Une app lancée du Finder n'a que /usr/bin:/bin:/usr/sbin:/sbin : on
    // ajoute les dossiers de Homebrew (ffmpeg, ffplay, s'ils y sont).
    let chemin = std::env::var("PATH").unwrap_or_default();
    let mut morceaux: Vec<&str> = chemin.split(':').filter(|s| !s.is_empty()).collect();
    for d in ["/opt/homebrew/bin", "/usr/local/bin"] {
        if !morceaux.contains(&d) {
            morceaux.push(d);
        }
    }
    if morceaux.is_empty() {
        morceaux = vec!["/usr/bin", "/bin", "/usr/sbin", "/sbin"];
    }
    // SAFETY : idem.
    unsafe { std::env::set_var("PATH", morceaux.join(":")) };
}

/// Un programme est-il dans le PATH ?
pub fn trouve(nom: &str) -> bool {
    std::env::var_os("PATH").is_some_and(|p| std::env::split_paths(&p).any(|d| d.join(nom).is_file()))
}

/// Ouvre une adresse ou un fichier avec l'application par défaut.
pub fn ouvrir(cible: &std::ffi::OsStr) {
    let _ = Command::new("/usr/bin/open")
        .arg(cible)
        .stdin(Stdio::null())
        .stdout(Stdio::null())
        .stderr(Stdio::null())
        .spawn();
}

/// Une chaîne AppleScript entre guillemets.
fn chaine_applescript(s: &str) -> String {
    let mut r = String::with_capacity(s.len() + 2);
    r.push('"');
    for c in s.chars() {
        match c {
            '"' => r.push_str("\\\""),
            '\\' => r.push_str("\\\\"),
            '\n' | '\r' => r.push(' '),
            c if c.is_control() => {}
            c => r.push(c),
        }
    }
    r.push('"');
    r
}

/// Une notification (Centre de notifications). Sans signature Developer ID,
/// macOS l'attribue à « Éditeur de script » et un clic n'ouvre pas la conversation.
pub fn notifier(titre: &str, corps: &str) {
    let tronque = |s: &str, n: usize| s.chars().take(n).collect::<String>();
    let script = format!(
        "display notification {} with title {}",
        chaine_applescript(&tronque(corps, 400)),
        chaine_applescript(&tronque(titre, 120))
    );
    let _ = Command::new("/usr/bin/osascript")
        .arg("-e")
        .arg(script)
        .stdin(Stdio::null())
        .stdout(Stdio::null())
        .stderr(Stdio::null())
        .spawn()
        .and_then(|mut e| e.wait());
}

/// Lire un vocal : ffplay s'il est là (comme sous Linux), sinon afplay
/// (sans reprise au milieu, et sans Ogg/Opus).
pub fn lecture_vocal(chemin: &Path, depuis: f32) -> std::io::Result<std::process::Child> {
    let mut c = if trouve("ffplay") {
        let mut c = Command::new("ffplay");
        c.args(["-nodisp", "-autoexit", "-loglevel", "quiet", "-ss", &format!("{depuis:.2}")]);
        c
    } else {
        Command::new("/usr/bin/afplay")
    };
    c.arg(chemin).stdin(Stdio::null()).stdout(Stdio::null()).stderr(Stdio::null()).spawn()
}

/// Enregistrer un vocal (Ogg/Opus, comme pw-record sous Linux) : ffmpeg
/// (Homebrew) par AVFoundation ; SIGINT termine proprement le fichier.
pub fn enregistrer_vocal(fichier: &Path) -> Result<std::process::Child, String> {
    if !trouve("ffmpeg") {
        return Err("ffmpeg est nécessaire pour enregistrer un vocal sur macOS (brew install ffmpeg)".into());
    }
    Command::new("ffmpeg")
        .args(["-loglevel", "quiet", "-nostdin", "-f", "avfoundation", "-i", ":0", "-ac", "1", "-ar", "48000", "-c:a", "libopus"])
        .arg(fichier)
        .stdin(Stdio::null())
        .stdout(Stdio::null())
        .stderr(Stdio::null())
        .spawn()
        .map_err(|e| e.to_string())
}

/// Le programme des fenêtres de connexion (WKWebView), à côté de l'exécutable.
pub fn programme_connexion() -> Option<PathBuf> {
    if let Some(p) = std::env::var_os("JASETTE_CONNEXION_MAC") {
        return Some(PathBuf::from(p));
    }
    let exe = std::env::current_exe().ok()?;
    let p = exe.parent()?.join("jasette-connexion");
    p.is_file().then_some(p)
}

/// La commande d'une fenêtre de connexion : jasette-connexion reçoit le nom
/// du script Linux qu'il remplace (voir macos/connexion/main.swift).
pub fn commande_connexion() -> Command {
    Command::new(programme_connexion().unwrap_or_else(|| PathBuf::from("jasette-connexion")))
}

/// Le « script » de connexion : son nom seul, que jasette-connexion reconnaît.
pub fn script_connexion(nom: &str) -> Option<PathBuf> {
    programme_connexion().map(|_| PathBuf::from(nom))
}

/// Le LaunchAgent qui ouvre Jasette avec la session.
pub fn fichier_demarrage() -> Option<PathBuf> {
    let home = std::env::var_os("HOME")?;
    Some(PathBuf::from(home).join("Library/LaunchAgents/ca.jasette.Jasette.plist"))
}

/// Le dossier Jasette.app qui contient cet exécutable, s'il y en a un.
fn paquet_app() -> Option<PathBuf> {
    let exe = std::env::current_exe().ok()?;
    exe.ancestors().find(|a| a.extension().is_some_and(|e| e == "app")).map(Path::to_path_buf)
}

fn xml(s: &str) -> String {
    s.replace('&', "&amp;").replace('<', "&lt;").replace('>', "&gt;").replace('"', "&quot;")
}

pub fn regler_demarrage_auto(actif: bool) -> Result<(), String> {
    let fichier = fichier_demarrage().ok_or("Dossier personnel introuvable")?;
    if !actif {
        return match std::fs::remove_file(&fichier) {
            Err(e) if e.kind() != std::io::ErrorKind::NotFound => Err(e.to_string()),
            _ => Ok(()),
        };
    }
    let arguments: Vec<String> = match paquet_app() {
        Some(app) => vec!["/usr/bin/open".into(), "-a".into(), app.display().to_string()],
        None => vec![std::env::current_exe().map_err(|e| e.to_string())?.display().to_string()],
    };
    let args: String = arguments.iter().map(|a| format!("\t\t<string>{}</string>\n", xml(a))).collect();
    let contenu = format!(
        "<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n\
         <!DOCTYPE plist PUBLIC \"-//Apple//DTD PLIST 1.0//EN\" \"http://www.apple.com/DTDs/PropertyList-1.0.dtd\">\n\
         <plist version=\"1.0\">\n<dict>\n\
         \t<key>Label</key>\n\t<string>ca.jasette.Jasette</string>\n\
         \t<key>ProgramArguments</key>\n\t<array>\n{args}\t</array>\n\
         \t<key>RunAtLoad</key>\n\t<true/>\n\
         </dict>\n</plist>\n"
    );
    if let Some(d) = fichier.parent() {
        std::fs::create_dir_all(d).map_err(|e| e.to_string())?;
    }
    std::fs::write(&fichier, contenu).map_err(|e| e.to_string())
}

/// Le nom de cette machine (Réglages > Général > Informations > Nom).
pub fn nom_machine() -> String {
    Command::new("/usr/sbin/scutil")
        .args(["--get", "ComputerName"])
        .stdin(Stdio::null())
        .stderr(Stdio::null())
        .output()
        .ok()
        .map(|o| String::from_utf8_lossy(&o.stdout).trim().to_string())
        .filter(|s| !s.is_empty())
        .unwrap_or_else(|| "Jasette".into())
}

/// Le mode « Je suis le serveur Jasette » (service systemd) n'existe pas sur macOS.
pub const SERVEUR_NON_OFFERT: &str =
    "Le mode serveur n'est pas offert sur macOS. Choisissez « Utiliser mon serveur Jasette » pour vous brancher à un serveur Linux.";

gpui::actions!(jasette_macos, [Quitter, Masquer, MasquerAutres]);

/// Le menu de l'app (barre des menus de macOS) et ⌘Q, ⌘H ; Jasette se
/// ferme avec sa dernière fenêtre, comme sous Linux.
pub fn installer_menus(cx: &mut gpui::App) {
    use gpui::{KeyBinding, Menu, MenuItem};
    cx.on_action(|_: &Quitter, cx| cx.quit());
    cx.on_action(|_: &Masquer, cx| cx.hide());
    cx.on_action(|_: &MasquerAutres, cx| cx.hide_other_apps());
    cx.bind_keys([
        KeyBinding::new("cmd-q", Quitter, None),
        KeyBinding::new("cmd-h", Masquer, None),
        KeyBinding::new("cmd-alt-h", MasquerAutres, None),
    ]);
    cx.set_menus(vec![Menu::new("Jasette").items([
        MenuItem::action("Masquer Jasette", Masquer),
        MenuItem::action("Masquer les autres", MasquerAutres),
        MenuItem::separator(),
        MenuItem::action("Quitter Jasette", Quitter),
    ])]);
    cx.on_window_closed(|cx, _| {
        if cx.windows().is_empty() {
            cx.quit();
        }
    })
    .detach();
}
