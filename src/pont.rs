//! Le pont Go (`jasette-pont`), lancé comme processus enfant.
//!
//! Commandes : une ligne JSON sur son entrée standard. Nouvelles : une ligne
//! JSON sur sa sortie standard, lue par un fil à part et gardée jusqu'à ce que
//! l'interface la prenne ([`Pont::recevoir`]). Ses journaux passent tels quels
//! dans la sortie d'erreur de Jasette.
//!
//! Ou bien le même service servi par un serveur Jasette ([`Pont::distant`],
//! voir `distant.rs`) : même interface, l'interface n'a qu'à choisir lequel
//! créer.

use std::io::{BufRead, BufReader, Write};
use std::path::PathBuf;
use std::process::{Child, ChildStdin, Command, Stdio};
use std::sync::Mutex;
use std::sync::mpsc::{Receiver, channel};

use serde_json::{Value, json};

#[path = "distant.rs"]
pub mod distant;

#[allow(unused_imports)] // utilisés par l'interface (réglages « serveur Jasette »)
pub use distant::{ConfigServeur, PORT_PAR_DEFAUT, Verification, code_verification, jumeler, verifier_serveur};
pub use distant::{CopieMarques, Marques, NouvelleMarques, SuiviMarques};

pub struct Pont {
    genre: Genre,
}

enum Genre {
    /// Un processus enfant sur cette machine.
    Local { enfant: Mutex<Child>, entree: Mutex<ChildStdin>, nouvelles: Mutex<Receiver<Value>> },
    /// Un service d'un serveur Jasette.
    Distant(distant::PontDistant),
}

/// La variable d'environnement `var`, sinon `nom` à côté de l'exécutable,
/// sinon dans `.cache/bin` du projet.
pub(crate) fn chemin(nom: &str, var: &str) -> Option<PathBuf> {
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
        Self::lancer_nomme("jasette-pont", "JASETTE_PONT", None)
    }

    /// Le pont d'un service autre que Google : « messenger » ou « instagram »
    /// (le pont Meta, une copie par service, chacune avec son dossier de
    /// données), ou « whatsapp », « signal », « x » (un pont chacun, qui parle
    /// le même protocole ; erreur si son binaire est absent).
    pub fn lancer_meta(service: &str) -> Result<Self, String> {
        match service {
            "whatsapp" => return Self::lancer_nomme("jasette-wa", "JASETTE_WA", None),
            "signal" => return Self::lancer_nomme("jasette-signal", "JASETTE_SIGNAL", None),
            "x" => return Self::lancer_nomme("jasette-x", "JASETTE_X", None),
            _ => {}
        }
        // Instagram a changé de protocole : son propre pont (instameow), s'il est là.
        if service == "instagram" && chemin("jasette-ig", "JASETTE_IG").is_some() {
            return Self::lancer_nomme("jasette-ig", "JASETTE_IG", Some(service));
        }
        Self::lancer_nomme("jasette-meta", "JASETTE_META", Some(service))
    }

    fn lancer_nomme(nom: &str, var: &str, service: Option<&str>) -> Result<Self, String> {
        let chemin = chemin(nom, var).ok_or_else(|| format!("{nom} introuvable"))?;
        let mut cmd = Command::new(&chemin);
        if let Some(s) = service {
            cmd.env("JASETTE_META_SERVICE", s);
        }
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
        Ok(Self {
            genre: Genre::Local { enfant: Mutex::new(enfant), entree: Mutex::new(entree), nouvelles: Mutex::new(rx) },
        })
    }

    /// Le service `service` (« google », « messenger », « instagram »,
    /// « whatsapp », « signal » ou « x ») d'un
    /// serveur Jasette jumelé. Ne bloque pas : la connexion (une seule par
    /// serveur, partagée entre les services) se fait en arrière-plan, et
    /// l'interface reçoit `state: connecting` puis l'état réel du pont du
    /// serveur.
    #[allow(dead_code)] // appelé par l'interface en mode « Utiliser un serveur Jasette »
    pub fn distant(config: &ConfigServeur, service: &str) -> Self {
        Self { genre: Genre::Distant(distant::PontDistant::brancher(config, service)) }
    }

    pub fn envoyer(&self, commande: &Value) {
        match &self.genre {
            Genre::Local { entree, .. } => {
                if let Ok(mut e) = entree.lock() {
                    let _ = writeln!(e, "{commande}");
                    let _ = e.flush();
                }
            }
            Genre::Distant(d) => d.envoyer(commande),
        }
    }

    /// Tout ce qui est arrivé depuis la dernière fois.
    pub fn recevoir(&self) -> Vec<Value> {
        match &self.genre {
            Genre::Local { nouvelles, .. } => nouvelles.lock().map(|rx| rx.try_iter().collect()).unwrap_or_default(),
            Genre::Distant(d) => d.recevoir(),
        }
    }
}

impl Pont {
    /// Les marques reçues du serveur Jasette (rien pour un pont local).
    pub fn recevoir_marques(&self) -> Vec<NouvelleMarques> {
        match &self.genre {
            Genre::Local { .. } => Vec::new(),
            Genre::Distant(d) => d.recevoir_marques(),
        }
    }

    /// Envoie une trame `marques` au serveur ; faux pour un pont local ou
    /// une connexion coupée.
    pub fn envoyer_marques(&self, trame: &Value) -> bool {
        match &self.genre {
            Genre::Local { .. } => false,
            Genre::Distant(d) => d.envoyer_marques(trame),
        }
    }
}

impl Drop for Pont {
    fn drop(&mut self) {
        // Un pont distant appartient au serveur : on se débranche seulement.
        if let Genre::Local { enfant, .. } = &self.genre {
            self.envoyer(&json!({"cmd": "quit"}));
            if let Ok(mut e) = enfant.lock() {
                let _ = e.wait();
            }
        }
    }
}
