//! Le pont Go (`jasette-pont`), lancé comme processus enfant.
//!
//! Commandes : une ligne JSON sur son entrée standard. Nouvelles : une ligne
//! JSON sur sa sortie standard, lue par un fil à part et gardée jusqu'à ce que
//! l'interface la prenne ([`Pont::recevoir`]). Ses journaux passent tels quels
//! dans la sortie d'erreur de Jasette.

use std::io::{BufRead, BufReader, Write};
use std::path::PathBuf;
use std::process::{Child, ChildStdin, Command, Stdio};
use std::sync::Mutex;
use std::sync::mpsc::{Receiver, channel};

use serde_json::{Value, json};

pub struct Pont {
    enfant: Mutex<Child>,
    entree: Mutex<ChildStdin>,
    nouvelles: Mutex<Receiver<Value>>,
}

/// La variable d'environnement `var`, sinon `nom` à côté de l'exécutable,
/// sinon dans `.cache/bin` du projet.
fn chemin(nom: &str, var: &str) -> Option<PathBuf> {
    if let Some(p) = std::env::var_os(var) {
        return Some(PathBuf::from(p));
    }
    let mut essais = Vec::new();
    if let Ok(exe) = std::env::current_exe()
        && let Some(dir) = exe.parent()
    {
        essais.push(dir.join(nom));
    }
    essais.push(PathBuf::from(env!("CARGO_MANIFEST_DIR")).join(".cache/bin").join(nom));
    essais.into_iter().find(|p| p.is_file())
}

impl Pont {
    /// Le pont Google Messages.
    pub fn lancer() -> Result<Self, String> {
        Self::lancer_nomme("jasette-pont", "JASETTE_PONT")
    }

    /// Le pont Messenger.
    pub fn lancer_meta() -> Result<Self, String> {
        Self::lancer_nomme("jasette-meta", "JASETTE_META")
    }

    fn lancer_nomme(nom: &str, var: &str) -> Result<Self, String> {
        let chemin = chemin(nom, var).ok_or_else(|| format!("{nom} introuvable"))?;
        let mut cmd = Command::new(&chemin);
        // En développement, les photos téléchargées restent dans .cache du projet.
        if std::env::var_os("JASETTE_MEDIA").is_none() {
            let projet = PathBuf::from(env!("CARGO_MANIFEST_DIR")).join(".cache");
            if projet.is_dir() {
                cmd.env("JASETTE_MEDIA", projet.join("media"));
            }
        }
        let mut enfant = cmd
            .stdin(Stdio::piped())
            .stdout(Stdio::piped())
            .stderr(Stdio::inherit())
            .spawn()
            .map_err(|e| format!("{}: {e}", chemin.display()))?;
        let entree = enfant.stdin.take().ok_or("pas d'entrée")?;
        let sortie = enfant.stdout.take().ok_or("pas de sortie")?;
        let (tx, rx) = channel();
        std::thread::Builder::new()
            .name("pont".into())
            .spawn(move || {
                for ligne in BufReader::new(sortie).lines() {
                    let Ok(ligne) = ligne else { break };
                    match serde_json::from_str::<Value>(&ligne) {
                        Ok(v) => {
                            if tx.send(v).is_err() {
                                return;
                            }
                        }
                        Err(e) => eprintln!("jasette : ligne illisible du pont : {e}"),
                    }
                }
                let _ = tx.send(json!({"ev": "pont_ferme"}));
            })
            .map_err(|e| e.to_string())?;
        Ok(Self { enfant: Mutex::new(enfant), entree: Mutex::new(entree), nouvelles: Mutex::new(rx) })
    }

    pub fn envoyer(&self, commande: &Value) {
        if let Ok(mut e) = self.entree.lock() {
            let _ = writeln!(e, "{commande}");
            let _ = e.flush();
        }
    }

    /// Tout ce qui est arrivé depuis la dernière fois.
    pub fn recevoir(&self) -> Vec<Value> {
        self.nouvelles.lock().map(|rx| rx.try_iter().collect()).unwrap_or_default()
    }
}

impl Drop for Pont {
    fn drop(&mut self) {
        self.envoyer(&json!({"cmd": "quit"}));
        if let Ok(mut e) = self.enfant.lock() {
            let _ = e.wait();
        }
    }
}
