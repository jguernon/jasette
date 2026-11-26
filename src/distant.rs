//! Le client du serveur Jasette (`jasette-serveur`, voir `serveur/PROTOCOLE.md`).
//!
//! Une seule connexion TLS par serveur, partagée entre les services : chaque
//! [`PontDistant`] (un par service) lit sa part du flux. L'empreinte du
//! certificat est épinglée au jumelage ; la connexion se refait toute seule.
//! Pendant une coupure, l'interface reçoit `{"ev":"state","state":"reconnecting"}`
//! (ou `connecting` si le serveur n'a jamais répondu).
//!
//! Les chemins de fichiers des événements (avatars, photos téléchargées)
//! sont ceux du serveur : on télécharge ces fichiers dans un cache local et
//! on réécrit les chemins avant de passer l'événement à l'interface. Pour
//! `send_media`, le fichier local est d'abord téléversé sur le serveur.

use std::collections::{BTreeMap, HashMap};
use std::fmt;
use std::fs;
use std::io::{self, Read, Write};
use std::net::{Shutdown, SocketAddr, TcpStream, ToSocketAddrs};
use std::path::{Path, PathBuf};
use std::sync::atomic::{AtomicBool, AtomicU64, Ordering};
use std::sync::mpsc::{Receiver, RecvTimeoutError, Sender, channel};
use std::sync::{Arc, Mutex, MutexGuard, Weak};
use std::time::{Duration, Instant};

use base64::Engine;
use base64::engine::general_purpose::STANDARD as B64;
use ring::rand::{SecureRandom, SystemRandom};
use ring::{digest, hmac};
use rustls::client::danger::{HandshakeSignatureValid, ServerCertVerified, ServerCertVerifier};
use rustls::crypto::{CryptoProvider, verify_tls12_signature, verify_tls13_signature};
use rustls::pki_types::{CertificateDer, ServerName, UnixTime};
use rustls::{ClientConfig, ClientConnection, DigitallySignedStruct, SignatureScheme};
use serde::{Deserialize, Serialize};
use serde_json::{Value, json};

/// Le port du serveur quand l'adresse n'en donne pas.
#[allow(dead_code)] // API pour l'interface (réglages « serveur Jasette »)
pub const PORT_PAR_DEFAUT: u16 = 47800;

const VERSION_PROTOCOLE: &str = "jasette/1";
const LIGNE_MAX: usize = 8 << 20;
const MORCEAU_DEPOT: usize = 256 << 10;
/// Le serveur envoie un ping toutes les 25 s : sans rien pendant 90 s, il est perdu.
const SILENCE_MAX: Duration = Duration::from_secs(90);
/// Un fichier déjà vérifié n'est pas redemandé avant ce délai.
const FRAICHEUR: Duration = Duration::from_secs(600);
/// Les clés d'événements dont la valeur peut être un chemin de fichier du serveur.
const CLES_CHEMINS: &[&str] = &["path", "avatar", "photo", "image", "thumb", "fichier"];

/// Ce que l'interface garde dans ses réglages pour un serveur jumelé.
#[derive(Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct ConfigServeur {
    /// « hôte:port » (ou « hôte », port 47800).
    pub adresse: String,
    /// SHA-256 du certificat du serveur, hexadécimal minuscule (épinglé).
    pub empreinte: String,
    /// Le jeton de cet appareil : secret, à garder comme un mot de passe.
    pub jeton: String,
    /// Le nom sous lequel le serveur connaît cet appareil.
    pub nom_appareil: String,
}

impl fmt::Debug for ConfigServeur {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.debug_struct("ConfigServeur")
            .field("adresse", &self.adresse)
            .field("empreinte", &self.empreinte)
            .field("jeton", &"…")
            .field("nom_appareil", &self.nom_appareil)
            .finish()
    }
}

/// Le résultat de [`verifier_serveur`] : à montrer avant de jumeler.
#[allow(dead_code)] // API pour l'interface (réglages « serveur Jasette »)
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct Verification {
    /// L'empreinte du certificat vu, à passer telle quelle à [`jumeler`].
    pub empreinte: String,
    /// 6 chiffres : doivent être ceux qu'affiche `jasette-serveur --jumeler`.
    pub code_verification: String,
}

// ---------------------------------------------------------------------------
// Jumelage
// ---------------------------------------------------------------------------

/// Étape 1 du jumelage : se brancher au serveur sans rien lui envoyer de
/// secret, et calculer le code de vérification à partir du code saisi et de
/// l'empreinte vue. L'interface l'affiche ; l'utilisateur confirme qu'il est
/// identique à celui du serveur (sinon : quelqu'un est au milieu).
#[allow(dead_code)] // API pour l'interface (réglages « serveur Jasette »)
pub fn verifier_serveur(adresse: &str, code: &str) -> Result<Verification, String> {
    let code = normaliser_code(code)?;
    let accueil = accueillir(adresse, None).map_err(|e| e.to_string())?;
    accueil.canal.fermer();
    Ok(Verification { code_verification: code_verification(&code, &accueil.empreinte), empreinte: accueil.empreinte })
}

/// Étape 2 : jumeler cet appareil, en exigeant le certificat d'empreinte
/// `empreinte` (celle de [`verifier_serveur`], confirmée par l'utilisateur).
/// Rend de quoi se brancher ensuite ([`super::Pont::distant`]).
#[allow(dead_code)] // API pour l'interface (réglages « serveur Jasette »)
pub fn jumeler(adresse: &str, code: &str, nom_appareil: &str, empreinte: &str) -> Result<ConfigServeur, String> {
    let code = normaliser_code(code)?;
    let empreinte = empreinte.trim().to_ascii_lowercase();
    let mut a = accueillir(adresse, Some(&empreinte)).map_err(|e| e.to_string())?;
    let nonce_client = B64.encode(aleatoire::<32>());
    let preuve = preuve(&code, "client", &a.empreinte, &a.nonce, &nonce_client);
    a.canal
        .ecrire_ligne(&json!({"jumeler": {"nom": nom_appareil, "nonce": nonce_client, "preuve": hex(&preuve)}}).to_string())
        .map_err(|e| format!("serveur : {e}"))?;
    let reponse = a.lecteur.ligne_json().map_err(|e| format!("serveur : {e}"));
    a.canal.fermer();
    let reponse = reponse?;
    if let Some(texte) = erreur_de(&reponse) {
        return Err(texte);
    }
    let j = reponse.get("jumele").ok_or("réponse inattendue du serveur")?;
    let jeton = j.get("jeton").and_then(Value::as_str).ok_or("pas de jeton")?;
    let nom = j.get("nom").and_then(Value::as_str).unwrap_or(nom_appareil);
    // Le serveur prouve à son tour qu'il connaissait le code.
    let attendue = preuve_hmac(&code, "serveur", &a.empreinte, &a.nonce, &nonce_client);
    let recue = j.get("preuve").and_then(Value::as_str).map(de_hex).unwrap_or_default();
    if hmac::verify(&attendue.0, &attendue.1, &recue).is_err() {
        return Err("le serveur n'a pas prouvé qu'il connaissait le code".into());
    }
    Ok(ConfigServeur {
        adresse: adresse.trim().to_string(),
        empreinte: a.empreinte,
        jeton: jeton.to_string(),
        nom_appareil: nom.to_string(),
    })
}

fn normaliser_code(code: &str) -> Result<String, String> {
    let chiffres: String = code.chars().filter(char::is_ascii_digit).collect();
    if chiffres.len() != 6 {
        return Err("le code de jumelage a 6 chiffres".into());
    }
    Ok(chiffres)
}

/// 6 chiffres : SHA-256("jasette/1/verification|<code>|<empreinte>"), 4
/// premiers octets en grand-boutiste, modulo 1 000 000.
#[allow(dead_code)] // API pour l'interface (réglages « serveur Jasette »)
pub fn code_verification(code: &str, empreinte: &str) -> String {
    let s = digest::digest(&digest::SHA256, format!("{VERSION_PROTOCOLE}/verification|{code}|{empreinte}").as_bytes());
    let n = u32::from_be_bytes([s.as_ref()[0], s.as_ref()[1], s.as_ref()[2], s.as_ref()[3]]);
    format!("{:06}", n % 1_000_000)
}

fn preuve_hmac(code: &str, role: &str, empreinte: &str, ns: &str, nc: &str) -> (hmac::Key, Vec<u8>) {
    let cle = hmac::Key::new(hmac::HMAC_SHA256, code.as_bytes());
    (cle, format!("{VERSION_PROTOCOLE}/{role}|{empreinte}|{ns}|{nc}").into_bytes())
}

fn preuve(code: &str, role: &str, empreinte: &str, ns: &str, nc: &str) -> Vec<u8> {
    let (cle, msg) = preuve_hmac(code, role, empreinte, ns, nc);
    hmac::sign(&cle, &msg).as_ref().to_vec()
}

// ---------------------------------------------------------------------------
// TLS : épinglage de l'empreinte
// ---------------------------------------------------------------------------

#[derive(Debug)]
struct Epinglage {
    attendue: Option<String>,
    vue: Mutex<Option<String>>,
    fournisseur: Arc<CryptoProvider>,
}

impl ServerCertVerifier for Epinglage {
    fn verify_server_cert(
        &self,
        certificat: &CertificateDer<'_>,
        _intermediaires: &[CertificateDer<'_>],
        _nom: &ServerName<'_>,
        _ocsp: &[u8],
        _maintenant: UnixTime,
    ) -> Result<ServerCertVerified, rustls::Error> {
        let vue = hex(digest::digest(&digest::SHA256, certificat.as_ref()).as_ref());
        if let Ok(mut v) = self.vue.lock() {
            *v = Some(vue.clone());
        }
        match &self.attendue {
            Some(a) if *a != vue => Err(rustls::Error::General("empreinte du serveur inattendue".into())),
            _ => Ok(ServerCertVerified::assertion()),
        }
    }

    fn verify_tls12_signature(
        &self,
        message: &[u8],
        cert: &CertificateDer<'_>,
        dss: &DigitallySignedStruct,
    ) -> Result<HandshakeSignatureValid, rustls::Error> {
        verify_tls12_signature(message, cert, dss, &self.fournisseur.signature_verification_algorithms)
    }

    fn verify_tls13_signature(
        &self,
        message: &[u8],
        cert: &CertificateDer<'_>,
        dss: &DigitallySignedStruct,
    ) -> Result<HandshakeSignatureValid, rustls::Error> {
        verify_tls13_signature(message, cert, dss, &self.fournisseur.signature_verification_algorithms)
    }

    fn supported_verify_schemes(&self) -> Vec<SignatureScheme> {
        self.fournisseur.signature_verification_algorithms.supported_schemes()
    }
}

#[derive(Debug)]
enum ErreurLien {
    /// Le serveur présente un autre certificat que celui épinglé.
    Empreinte(String),
    /// Le serveur refuse (jeton révoqué, code faux…).
    Refus(String),
    Reseau(String),
}

impl fmt::Display for ErreurLien {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            Self::Empreinte(vue) => write!(
                f,
                "le certificat du serveur a changé (empreinte {vue}) : connexion refusée. \
                 Si le serveur a été réinstallé, jumelez de nouveau."
            ),
            Self::Refus(texte) => write!(f, "{texte}"),
            Self::Reseau(texte) => write!(f, "serveur injoignable : {texte}"),
        }
    }
}

/// Sépare « hôte:port » ; sans port, 47800.
fn adresses(adresse: &str) -> Result<Vec<SocketAddr>, String> {
    let a = adresse.trim();
    if let Ok(s) = a.parse::<SocketAddr>() {
        return Ok(vec![s]);
    }
    let avec_port = match a.rsplit_once(':') {
        Some((h, p)) if !h.contains(':') || h.ends_with(']') => {
            if p.parse::<u16>().is_ok() {
                a.to_string()
            } else {
                return Err(format!("port invalide : {p}"));
            }
        }
        Some(_) => format!("[{a}]:{PORT_PAR_DEFAUT}"), // IPv6 sans crochets
        None => format!("{a}:{PORT_PAR_DEFAUT}"),
    };
    avec_port.to_socket_addrs().map(Iterator::collect).map_err(|e| format!("{a} : {e}"))
}

/// La moitié écriture d'une connexion TLS (partagée), plus de quoi la couper.
struct Canal {
    conn: Mutex<ClientConnection>,
    sock: Mutex<TcpStream>,
    coupe: TcpStream,
}

fn verrou<T>(m: &Mutex<T>) -> MutexGuard<'_, T> {
    m.lock().unwrap_or_else(|e| e.into_inner())
}

impl Canal {
    fn ecrire_ligne(&self, ligne: &str) -> io::Result<()> {
        let mut c = verrou(&self.conn);
        let mut s = verrou(&self.sock);
        let mut donnees = Vec::with_capacity(ligne.len() + 1);
        donnees.extend_from_slice(ligne.as_bytes());
        donnees.push(b'\n');
        let mut reste = &donnees[..];
        while !reste.is_empty() {
            let n = c.writer().write(reste)?;
            reste = &reste[n..];
            let mut vide = true;
            while c.wants_write() {
                vide = false;
                c.write_tls(&mut *s)?;
            }
            if n == 0 && vide {
                return Err(io::Error::new(io::ErrorKind::WriteZero, "tampon TLS plein"));
            }
        }
        s.flush()
    }

    fn fermer(&self) {
        let _ = self.coupe.shutdown(Shutdown::Both);
    }
}

/// La moitié lecture : un seul fil s'en sert.
struct Lecteur {
    canal: Arc<Canal>,
    sock: TcpStream,
    tampon: Vec<u8>,
    ferme: bool,
}

impl Lecteur {
    /// Fait passer le texte déjà déchiffré dans le tampon.
    fn vider(&mut self, c: &mut ClientConnection) -> io::Result<()> {
        let mut morceau = [0u8; 16 << 10];
        loop {
            match c.reader().read(&mut morceau) {
                Ok(0) => {
                    self.ferme = true;
                    return Ok(());
                }
                Ok(n) => self.tampon.extend_from_slice(&morceau[..n]),
                Err(e) if e.kind() == io::ErrorKind::WouldBlock => return Ok(()),
                Err(e) => return Err(e),
            }
        }
    }

    fn ligne(&mut self) -> io::Result<Vec<u8>> {
        loop {
            if let Some(pos) = self.tampon.iter().position(|&b| b == b'\n') {
                let mut l: Vec<u8> = self.tampon.drain(..=pos).collect();
                l.pop();
                return Ok(l);
            }
            if self.ferme {
                return Err(io::Error::new(io::ErrorKind::UnexpectedEof, "connexion fermée"));
            }
            if self.tampon.len() > LIGNE_MAX {
                return Err(io::Error::new(io::ErrorKind::InvalidData, "trame trop longue"));
            }
            let mut brut = [0u8; 64 << 10];
            let n = self.sock.read(&mut brut)?;
            if n == 0 {
                return Err(io::Error::new(io::ErrorKind::UnexpectedEof, "connexion fermée"));
            }
            let canal = self.canal.clone();
            let mut c = verrou(&canal.conn);
            let mut tranche = &brut[..n];
            while !tranche.is_empty() {
                c.read_tls(&mut tranche)?;
                c.process_new_packets().map_err(io::Error::other)?;
                self.vider(&mut c)?;
            }
            if c.wants_write() {
                let mut s = verrou(&canal.sock);
                while c.wants_write() {
                    c.write_tls(&mut *s)?;
                }
            }
        }
    }

    fn ligne_json(&mut self) -> io::Result<Value> {
        let l = self.ligne()?;
        serde_json::from_slice(&l).map_err(|e| io::Error::new(io::ErrorKind::InvalidData, e))
    }
}

struct Accueil {
    canal: Arc<Canal>,
    lecteur: Lecteur,
    empreinte: String,
    /// Le nonce du serveur (base64), pour les preuves du jumelage.
    nonce: String,
}

/// Connexion TCP + TLS (épinglée si `attendue`), puis lecture de l'accueil
/// du serveur.
fn accueillir(adresse: &str, attendue: Option<&str>) -> Result<Accueil, ErreurLien> {
    let mut sock = None;
    let mut derniere = String::from("adresse vide");
    for a in adresses(adresse).map_err(ErreurLien::Reseau)? {
        match TcpStream::connect_timeout(&a, Duration::from_secs(10)) {
            Ok(s) => {
                sock = Some(s);
                break;
            }
            Err(e) => derniere = e.to_string(),
        }
    }
    let mut sock = sock.ok_or(ErreurLien::Reseau(derniere))?;
    let reseau = |e: io::Error| ErreurLien::Reseau(e.to_string());
    let _ = sock.set_nodelay(true);
    sock.set_read_timeout(Some(Duration::from_secs(20))).map_err(reseau)?;
    sock.set_write_timeout(Some(Duration::from_secs(30))).map_err(reseau)?;

    let fournisseur = Arc::new(rustls::crypto::ring::default_provider());
    let verif = Arc::new(Epinglage {
        attendue: attendue.map(str::to_string),
        vue: Mutex::new(None),
        fournisseur: fournisseur.clone(),
    });
    let config = ClientConfig::builder_with_provider(fournisseur)
        .with_protocol_versions(&[&rustls::version::TLS13])
        .map_err(|e| ErreurLien::Reseau(e.to_string()))?
        .dangerous()
        .with_custom_certificate_verifier(verif.clone())
        .with_no_client_auth();
    let nom = ServerName::try_from("jasette-serveur").map_err(|e| ErreurLien::Reseau(e.to_string()))?;
    let mut conn = ClientConnection::new(Arc::new(config), nom).map_err(|e| ErreurLien::Reseau(e.to_string()))?;
    while conn.is_handshaking() {
        if let Err(e) = conn.complete_io(&mut sock) {
            let vue = verrou(&verif.vue).clone();
            return Err(match (vue, attendue) {
                (Some(v), Some(a)) if v != a => ErreurLien::Empreinte(v),
                _ => ErreurLien::Reseau(e.to_string()),
            });
        }
    }
    let empreinte = verrou(&verif.vue).clone().ok_or(ErreurLien::Reseau("pas de certificat".into()))?;
    let lecture = sock.try_clone().map_err(reseau)?;
    let coupe = sock.try_clone().map_err(reseau)?;
    let canal = Arc::new(Canal { conn: Mutex::new(conn), sock: Mutex::new(sock), coupe });
    let mut lecteur = Lecteur { canal: canal.clone(), sock: lecture, tampon: Vec::new(), ferme: false };
    {
        let mut c = verrou(&canal.conn);
        lecteur.vider(&mut c).map_err(reseau)?;
    }
    let bonjour = lecteur.ligne_json().map_err(reseau)?;
    if bonjour.get("jasette").and_then(Value::as_str) != Some("serveur") {
        canal.fermer();
        return Err(ErreurLien::Reseau("ce n'est pas un serveur Jasette".into()));
    }
    let nonce = bonjour.get("nonce").and_then(Value::as_str).unwrap_or_default().to_string();
    Ok(Accueil { canal, lecteur, empreinte, nonce })
}

fn erreur_de(v: &Value) -> Option<String> {
    let code = v.get("erreur")?.as_str().unwrap_or("erreur");
    Some(v.get("texte").and_then(Value::as_str).unwrap_or(code).to_string())
}

// ---------------------------------------------------------------------------
// La connexion partagée
// ---------------------------------------------------------------------------

struct Connexion {
    config: ConfigServeur,
    cache: PathBuf,
    abonnes: Mutex<HashMap<String, Sender<Value>>>,
    /// La connexion en cours (None pendant une coupure), et les commandes
    /// mises de côté en attendant : sous le même verrou.
    lien: Mutex<(Option<Arc<Canal>>, Vec<String>)>,
    /// Réponses attendues (fichiers, dépôts), par identifiant de requête.
    attentes: Mutex<HashMap<u64, Sender<Value>>>,
    prochain: AtomicU64,
    arret: AtomicBool,
    deja_connecte: AtomicBool,
    vus: Mutex<HashMap<String, (PathBuf, Instant)>>,
    /// Les trames `marques` reçues (et les coupures), dans l'ordre, en
    /// attendant que l'interface les prenne.
    marques: Mutex<Vec<NouvelleMarques>>,
}

static CONNEXIONS: Mutex<Vec<Weak<Connexion>>> = Mutex::new(Vec::new());

impl Connexion {
    /// La connexion à ce serveur, créée (et lancée) au besoin.
    fn obtenir(config: &ConfigServeur) -> Arc<Connexion> {
        let mut liste = verrou(&CONNEXIONS);
        liste.retain(|w| w.strong_count() > 0);
        for w in liste.iter() {
            if let Some(cx) = w.upgrade()
                && cx.config.adresse == config.adresse
                && cx.config.jeton == config.jeton
                && !cx.arret.load(Ordering::SeqCst)
            {
                return cx;
            }
        }
        let cx = Arc::new(Connexion {
            cache: dossier_cache(&config.adresse),
            config: config.clone(),
            abonnes: Mutex::new(HashMap::new()),
            lien: Mutex::new((None, Vec::new())),
            attentes: Mutex::new(HashMap::new()),
            prochain: AtomicU64::new(1),
            arret: AtomicBool::new(false),
            deja_connecte: AtomicBool::new(false),
            vus: Mutex::new(HashMap::new()),
            marques: Mutex::new(Vec::new()),
        });
        liste.push(Arc::downgrade(&cx));
        let (tx, rx) = channel();
        let a = cx.clone();
        let _ = std::thread::Builder::new().name("serveur".into()).spawn(move || a.boucle(tx));
        let b = cx.clone();
        let _ = std::thread::Builder::new().name("serveur-evs".into()).spawn(move || b.traitement(rx));
        cx
    }

    fn canal(&self) -> Option<Arc<Canal>> {
        verrou(&self.lien).0.clone()
    }

    fn abonner(&self, svc: &str) -> Receiver<Value> {
        let (tx, rx) = channel();
        let canal = self.canal();
        if canal.is_none() {
            let etat = if self.deja_connecte.load(Ordering::SeqCst) { "reconnecting" } else { "connecting" };
            let _ = tx.send(json!({"ev": "state", "state": etat}));
        }
        verrou(&self.abonnes).insert(svc.to_string(), tx);
        // Déjà branché : le serveur a rejoué l'état avant notre arrivée.
        if let Some(c) = canal {
            let _ = c.ecrire_ligne(&json!({"rejouer": svc}).to_string());
        }
        rx
    }

    fn desabonner(&self, svc: &str) {
        let vide = {
            let mut a = verrou(&self.abonnes);
            a.remove(svc);
            a.is_empty()
        };
        if vide {
            self.arret.store(true, Ordering::SeqCst);
            if let Some(c) = self.canal() {
                c.fermer();
            }
        }
    }

    fn vers(&self, svc: &str, ev: Value) {
        if let Some(tx) = verrou(&self.abonnes).get(svc) {
            let _ = tx.send(ev);
        }
    }

    fn vers_tous(&self, ev: &Value) {
        for tx in verrou(&self.abonnes).values() {
            let _ = tx.send(ev.clone());
        }
    }

    /// Envoie une ligne, ou la garde pour la prochaine connexion.
    fn envoyer_ligne(&self, ligne: String) {
        let canal = {
            let mut l = verrou(&self.lien);
            match l.0.clone() {
                Some(c) => c,
                None => {
                    if l.1.len() >= 500 {
                        l.1.remove(0);
                    }
                    l.1.push(ligne);
                    return;
                }
            }
        };
        if canal.ecrire_ligne(&ligne).is_err() {
            canal.fermer();
            verrou(&self.lien).1.push(ligne);
        }
    }

    /// Envoie une ligne tout de suite, sans la garder pendant une coupure
    /// (les marques : le rejeu suivant dira où en est le serveur).
    fn envoyer_direct(&self, ligne: &str) -> bool {
        let Some(canal) = self.canal() else { return false };
        if canal.ecrire_ligne(ligne).is_err() {
            canal.fermer();
            return false;
        }
        true
    }

    fn envoyer(self: &Arc<Self>, svc: &str, commande: &Value) {
        match commande.get("cmd").and_then(Value::as_str) {
            // Le pont appartient au serveur.
            Some("quit") => {}
            Some("send_media") if commande.get("path").and_then(Value::as_str).is_some() => {
                let cx = self.clone();
                let svc = svc.to_string();
                let mut cmd = commande.clone();
                let _ = std::thread::Builder::new().name("televersement".into()).spawn(move || {
                    let local = cmd.get("path").and_then(Value::as_str).unwrap_or_default().to_string();
                    match cx.televerser(Path::new(&local)) {
                        Ok(chemin) => {
                            cmd["path"] = Value::String(chemin);
                            cx.envoyer_ligne(json!({"svc": svc, "cmd": cmd}).to_string());
                        }
                        Err(e) => {
                            eprintln!("jasette : téléversement : {e}");
                            cx.vers(&svc, json!({"ev": "error", "where": "send", "text": e}));
                            cx.vers(
                                &svc,
                                json!({"ev": "sent", "conv": cmd.get("conv"), "tmp_id": cmd.get("tmp_id"), "ok": false}),
                            );
                        }
                    }
                });
            }
            _ => self.envoyer_ligne(json!({"svc": svc, "cmd": commande}).to_string()),
        }
    }

    fn connecter(&self) -> Result<(Arc<Canal>, Lecteur, Vec<String>), ErreurLien> {
        let mut a = accueillir(&self.config.adresse, Some(&self.config.empreinte))?;
        let reseau = |e: io::Error| ErreurLien::Reseau(e.to_string());
        a.canal.ecrire_ligne(&json!({"auth": {"jeton": self.config.jeton}}).to_string()).map_err(reseau)?;
        let r = a.lecteur.ligne_json().map_err(reseau)?;
        if let Some(texte) = erreur_de(&r) {
            a.canal.fermer();
            return Err(ErreurLien::Refus(texte));
        }
        if r.get("auth").and_then(Value::as_str) != Some("ok") {
            a.canal.fermer();
            return Err(ErreurLien::Reseau("réponse inattendue du serveur".into()));
        }
        let services = r
            .get("services")
            .and_then(Value::as_array)
            .map(|l| l.iter().filter_map(Value::as_str).map(str::to_string).collect())
            .unwrap_or_default();
        a.lecteur.sock.set_read_timeout(Some(SILENCE_MAX)).map_err(reseau)?;
        Ok((a.canal, a.lecteur, services))
    }

    /// Le fil de la connexion : se brancher, lire, se rebrancher.
    fn boucle(self: Arc<Self>, evs: Sender<(String, Value)>) {
        let mut attente = Duration::from_secs(1);
        let mut erreur_dite = String::new();
        let mut etat_dit = false;
        while !self.arret.load(Ordering::SeqCst) {
            match self.connecter() {
                Ok((canal, mut lecteur, services)) => {
                    eprintln!("jasette : branché au serveur {}", self.config.adresse);
                    attente = Duration::from_secs(1);
                    erreur_dite.clear();
                    self.deja_connecte.store(true, Ordering::SeqCst);
                    let en_attente = {
                        let mut l = verrou(&self.lien);
                        l.0 = Some(canal.clone());
                        std::mem::take(&mut l.1)
                    };
                    for svc in verrou(&self.abonnes).keys() {
                        if !services.iter().any(|s| s == svc) {
                            let _ = evs.send((
                                svc.clone(),
                                json!({"ev": "error", "where": "serveur", "text": format!("le serveur n'offre pas {svc}")}),
                            ));
                        }
                    }
                    for l in en_attente {
                        self.envoyer_ligne(l);
                    }
                    while let Ok(l) = lecteur.ligne() {
                        self.trame(&l, &evs, &canal);
                    }
                    verrou(&self.lien).0 = None;
                    canal.fermer();
                    // Les requêtes de fichiers en cours échouent tout de suite.
                    verrou(&self.attentes).clear();
                    verrou(&self.marques).push(NouvelleMarques::Coupure);
                    if self.arret.load(Ordering::SeqCst) {
                        break;
                    }
                    eprintln!("jasette : serveur {} : connexion perdue", self.config.adresse);
                    self.vers_tous(&json!({"ev": "state", "state": "reconnecting"}));
                    etat_dit = true;
                }
                Err(e) => {
                    let texte = e.to_string();
                    if texte != erreur_dite {
                        eprintln!("jasette : serveur {} : {texte}", self.config.adresse);
                        if matches!(e, ErreurLien::Empreinte(_) | ErreurLien::Refus(_)) {
                            self.vers_tous(&json!({"ev": "error", "where": "serveur", "text": texte}));
                        }
                        erreur_dite = texte;
                    }
                    if !etat_dit {
                        let etat = if self.deja_connecte.load(Ordering::SeqCst) { "reconnecting" } else { "connecting" };
                        self.vers_tous(&json!({"ev": "state", "state": etat}));
                        etat_dit = true;
                    }
                    if matches!(e, ErreurLien::Empreinte(_) | ErreurLien::Refus(_)) {
                        attente = Duration::from_secs(60);
                    }
                }
            }
            let fin = Instant::now() + attente;
            while Instant::now() < fin && !self.arret.load(Ordering::SeqCst) {
                std::thread::sleep(Duration::from_millis(200));
            }
            // Attente progressive : 1, 2, 4… 30 s (60 s après un refus).
            if attente < Duration::from_secs(30) {
                attente = (attente * 2).min(Duration::from_secs(30));
            }
        }
    }

    fn trame(&self, ligne: &[u8], evs: &Sender<(String, Value)>, canal: &Canal) {
        let Ok(mut v) = serde_json::from_slice::<Value>(ligne) else {
            eprintln!("jasette : trame illisible du serveur");
            return;
        };
        if let Some(svc) = v.get("svc").and_then(Value::as_str).map(str::to_string) {
            if let Some(ev) = v.get_mut("ev").map(Value::take) {
                let _ = evs.send((svc, ev));
            }
        } else if let Some(p) = v.get("ping") {
            let _ = canal.ecrire_ligne(&json!({"pong": p}).to_string());
        } else if let Some(m) = v.get("marques") {
            match NouvelleMarques::lire(m) {
                Some(n) => verrou(&self.marques).push(n),
                None => eprintln!("jasette : marques illisibles du serveur"),
            }
        } else if let Some(r) = ["fichier", "depose", "mcp", "apercu"]
            .into_iter()
            .find(|k| v.get(*k).is_some())
            .and_then(|k| v.get_mut(k).map(Value::take))
            && let Some(id) = r.get("id").and_then(Value::as_u64)
            && let Some(tx) = verrou(&self.attentes).get(&id)
        {
            let _ = tx.send(r);
        }
    }

    /// Le fil des événements : réécrire les chemins (en téléchargeant), puis
    /// remettre au bon service, dans l'ordre d'arrivée.
    fn traitement(self: Arc<Self>, rx: Receiver<(String, Value)>) {
        for (svc, mut ev) in rx {
            if !verrou(&self.abonnes).contains_key(&svc) {
                continue;
            }
            self.reecrire(&mut ev);
            self.vers(&svc, ev);
        }
    }

    fn reecrire(&self, v: &mut Value) {
        match v {
            Value::Object(m) => {
                for (k, x) in m.iter_mut() {
                    if let Value::String(s) = x
                        && s.starts_with('/')
                        && CLES_CHEMINS.contains(&k.as_str())
                    {
                        if let Some(local) = self.telecharger(s) {
                            *s = local.to_string_lossy().into_owned();
                        }
                    } else {
                        self.reecrire(x);
                    }
                }
            }
            Value::Array(a) => a.iter_mut().for_each(|x| self.reecrire(x)),
            _ => {}
        }
    }

    fn attendre(&self) -> (u64, Receiver<Value>) {
        let id = self.prochain.fetch_add(1, Ordering::SeqCst);
        let (tx, rx) = channel();
        verrou(&self.attentes).insert(id, tx);
        (id, rx)
    }

    /// Copie locale d'un fichier du serveur (rien n'est redemandé s'il n'a
    /// pas changé : le serveur compare les SHA-256).
    fn telecharger(&self, distant: &str) -> Option<PathBuf> {
        if let Some((p, quand)) = verrou(&self.vus).get(distant)
            && quand.elapsed() < FRAICHEUR
            && p.is_file()
        {
            return Some(p.clone());
        }
        let local = self.cache.join(nom_local(distant));
        let canal = self.canal()?;
        let connu = sha256_fichier(&local).unwrap_or_default();
        let (id, rx) = self.attendre();
        let r = (|| {
            canal
                .ecrire_ligne(&json!({"get": {"id": id, "chemin": distant, "sha256": connu}}).to_string())
                .map_err(|e| e.to_string())?;
            let _ = fs::create_dir_all(&self.cache);
            let partiel = self.cache.join(format!(".{}.part", nom_local(distant)));
            let mut f: Option<fs::File> = None;
            loop {
                let m = match rx.recv_timeout(Duration::from_secs(120)) {
                    Ok(m) => m,
                    Err(RecvTimeoutError::Timeout) => return Err("délai dépassé".to_string()),
                    Err(RecvTimeoutError::Disconnected) => return Err("connexion perdue".to_string()),
                };
                if let Some(e) = m.get("erreur").and_then(Value::as_str) {
                    return Err(e.to_string());
                }
                if m.get("inchange").and_then(Value::as_bool) == Some(true) {
                    return Ok(());
                }
                if f.is_none() {
                    f = Some(fs::File::create(&partiel).map_err(|e| e.to_string())?);
                }
                if let (Some(d), Some(f)) = (m.get("donnees").and_then(Value::as_str), f.as_mut()) {
                    let brut = B64.decode(d).map_err(|e| e.to_string())?;
                    f.write_all(&brut).map_err(|e| e.to_string())?;
                }
                if m.get("fin").and_then(Value::as_bool) == Some(true) {
                    drop(f);
                    return fs::rename(&partiel, &local).map_err(|e| e.to_string());
                }
            }
        })();
        verrou(&self.attentes).remove(&id);
        match r {
            Ok(()) => {
                verrou(&self.vus).insert(distant.to_string(), (local.clone(), Instant::now()));
                Some(local)
            }
            Err(e) => {
                eprintln!("jasette : fichier du serveur non reçu : {e}");
                None
            }
        }
    }

    /// Téléverse un fichier local ; rend son chemin sur le serveur.
    fn televerser(&self, local: &Path) -> Result<String, String> {
        let mut f = fs::File::open(local).map_err(|e| format!("{} : {e}", local.display()))?;
        let taille = f.metadata().map_err(|e| e.to_string())?.len();
        let nom = local.file_name().map(|n| n.to_string_lossy().into_owned()).unwrap_or_else(|| "fichier".into());
        let canal = self.canal().ok_or("serveur injoignable")?;
        let (id, rx) = self.attendre();
        let r = (|| {
            let mut envoye = 0u64;
            let mut morceau = vec![0u8; MORCEAU_DEPOT];
            loop {
                let n = lire_plein(&mut f, &mut morceau).map_err(|e| e.to_string())?;
                envoye += n as u64;
                let fin = n < morceau.len() || envoye >= taille;
                let mut put = json!({"id": id, "donnees": B64.encode(&morceau[..n]), "fin": fin});
                if envoye == n as u64 {
                    put["nom"] = Value::String(nom.clone());
                }
                canal.ecrire_ligne(&json!({ "put": put }).to_string()).map_err(|e| e.to_string())?;
                // Le serveur ne répond qu'à la fin, ou en cas d'erreur.
                if let Ok(m) = rx.try_recv()
                    && let Some(e) = m.get("erreur").and_then(Value::as_str)
                {
                    return Err(e.to_string());
                }
                if fin {
                    break;
                }
            }
            let m = rx.recv_timeout(Duration::from_secs(120)).map_err(|_| "le serveur n'a pas confirmé le dépôt")?;
            if let Some(e) = m.get("erreur").and_then(Value::as_str) {
                return Err(e.to_string());
            }
            m.get("chemin").and_then(Value::as_str).map(str::to_string).ok_or_else(|| "dépôt sans chemin".to_string())
        })();
        verrou(&self.attentes).remove(&id);
        r
    }
}

fn lire_plein(f: &mut fs::File, buf: &mut [u8]) -> io::Result<usize> {
    let mut n = 0;
    while n < buf.len() {
        match f.read(&mut buf[n..])? {
            0 => break,
            k => n += k,
        }
    }
    Ok(n)
}

// ---------------------------------------------------------------------------
// La connexion MCP du serveur (PROTOCOLE.md, §9)
// ---------------------------------------------------------------------------

/// L'état de la connexion MCP du serveur, tel qu'il le répond à la trame `mcp`.
#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct EtatMcp {
    pub ouvert: bool,
    /// Vide si fermé.
    pub jeton: String,
    pub envoi: bool,
    pub port: u16,
    pub chemin: String,
    /// Les adresses IPv4 du serveur, puis 127.0.0.1.
    pub adresses: Vec<String>,
    /// Faux si le port n'a pas pu être ouvert (voir `erreur`).
    pub ecoute: bool,
    pub erreur: Option<String>,
}

impl EtatMcp {
    fn lire(v: &Value) -> Self {
        let ch = |k: &str| v.get(k).and_then(Value::as_str).unwrap_or_default().to_string();
        Self {
            ouvert: v.get("ouvert").and_then(Value::as_bool).unwrap_or(false),
            jeton: ch("jeton"),
            envoi: v.get("envoi").and_then(Value::as_bool).unwrap_or(false),
            port: v.get("port").and_then(Value::as_u64).unwrap_or(47810) as u16,
            chemin: v.get("chemin").and_then(Value::as_str).unwrap_or("/mcp").to_string(),
            adresses: v
                .get("adresses")
                .and_then(Value::as_array)
                .map(|a| a.iter().filter_map(|x| x.as_str().map(str::to_string)).collect())
                .unwrap_or_default(),
            ecoute: v.get("ecoute").and_then(Value::as_bool).unwrap_or(false),
            erreur: v.get("erreur").and_then(Value::as_str).filter(|e| !e.is_empty()).map(str::to_string),
        }
    }

    /// Les URL du point MCP : d'abord par `hote` (celui par lequel cet
    /// appareil joint le serveur), puis par les adresses du serveur.
    pub fn urls(&self, hote: &str) -> Vec<String> {
        let mut v: Vec<String> = Vec::new();
        let hotes = std::iter::once(hote.to_string()).chain(self.adresses.iter().cloned());
        for h in hotes.filter(|h| !h.is_empty()) {
            let h = if h.contains(':') && !h.starts_with('[') { format!("[{h}]") } else { h };
            let u = format!("http://{h}:{}{}", self.port, self.chemin);
            if !v.contains(&u) {
                v.push(u);
            }
        }
        v
    }
}

/// L'hôte d'une adresse « hôte », « hôte:port » ou « [ipv6]:port ».
pub fn hote_de(adresse: &str) -> String {
    let a = adresse.trim();
    if let Some(reste) = a.strip_prefix('[') {
        return reste.split(']').next().unwrap_or_default().to_string();
    }
    match a.rsplit_once(':') {
        Some((h, p)) if !h.contains(':') && p.parse::<u16>().is_ok() => h.to_string(),
        _ => a.to_string(),
    }
}

/// Envoie une trame `mcp` (`etat`, `ouvrir`, `fermer`, `regenerer`, `envoi`)
/// sur la connexion déjà ouverte avec ce serveur, et attend la réponse.
/// Bloquant (10 s au plus) : à appeler hors du fil de l'interface.
pub fn commande_mcp(config: &ConfigServeur, action: &str, envoi: Option<bool>) -> Result<EtatMcp, &'static str> {
    let cx = {
        let liste = verrou(&CONNEXIONS);
        liste
            .iter()
            .filter_map(Weak::upgrade)
            .find(|c| c.config.adresse == config.adresse && c.config.jeton == config.jeton && !c.arret.load(Ordering::SeqCst))
    };
    let Some(cx) = cx else { return Err("Pas de connexion au serveur Jasette.") };
    let (id, rx) = cx.attendre();
    let mut m = json!({"id": id, "action": action});
    if let Some(e) = envoi {
        m["envoi"] = json!(e);
    }
    let r = if cx.envoyer_direct(&json!({ "mcp": m }).to_string()) {
        match rx.recv_timeout(Duration::from_secs(10)) {
            Ok(v) => Ok(v),
            Err(_) => Err("Le serveur Jasette ne répond pas. S'il est d'une version sans connexion MCP, mettez-le à jour."),
        }
    } else {
        Err("Pas de connexion au serveur Jasette.")
    };
    verrou(&cx.attentes).remove(&id);
    let v = r?;
    let e = EtatMcp::lire(&v);
    // Un serveur d'avant la connexion MCP ne répond jamais ; une réponse sans
    // `port` non plus n'en vient pas.
    if v.get("port").is_none() {
        return Err("Le serveur Jasette ne répond pas. S'il est d'une version sans connexion MCP, mettez-le à jour.");
    }
    Ok(e)
}

/// L'aperçu d'un lien, demandé au serveur Jasette (PROTOCOLE.md, §5.9) sur
/// la connexion déjà ouverte : `Ok(Some(réponse))` (son `image` déjà
/// téléchargée ici), `Ok(None)` si le serveur n'en a pas fait, `Err` s'il
/// ne répond pas (pas de connexion, ou serveur d'avant les aperçus).
/// Bloquant (12 s au plus) : hors du fil de l'interface.
pub fn apercu_lien(config: &ConfigServeur, url: &str) -> Result<Option<Value>, ()> {
    let cx = {
        let liste = verrou(&CONNEXIONS);
        liste
            .iter()
            .filter_map(Weak::upgrade)
            .find(|c| c.config.adresse == config.adresse && c.config.jeton == config.jeton && !c.arret.load(Ordering::SeqCst))
    };
    let cx = cx.ok_or(())?;
    let (id, rx) = cx.attendre();
    let r = if cx.envoyer_direct(&json!({"apercu": {"id": id, "url": url}}).to_string()) {
        rx.recv_timeout(Duration::from_secs(12)).map_err(|_| ())
    } else {
        Err(())
    };
    verrou(&cx.attentes).remove(&id);
    let mut v = r?;
    if v.get("erreur").is_some() {
        return Ok(None);
    }
    if let Some(Value::String(image)) = v.get_mut("image") {
        match image.starts_with('/').then(|| cx.telecharger(image)).flatten() {
            Some(local) => *image = local.to_string_lossy().into_owned(),
            None => image.clear(),
        }
    }
    Ok(Some(v))
}

// ---------------------------------------------------------------------------
// Le pont distant d'un service
// ---------------------------------------------------------------------------

/// Un service servi par un serveur Jasette : même usage qu'un pont local.
pub struct PontDistant {
    cx: Arc<Connexion>,
    svc: String,
    nouvelles: Mutex<Receiver<Value>>,
}

impl PontDistant {
    /// `service` : « google », « messenger » ou « instagram ».
    pub fn brancher(config: &ConfigServeur, service: &str) -> Self {
        let cx = Connexion::obtenir(config);
        let rx = cx.abonner(service);
        Self { cx, svc: service.to_string(), nouvelles: Mutex::new(rx) }
    }

    pub fn envoyer(&self, commande: &Value) {
        self.cx.envoyer(&self.svc, commande);
    }

    pub fn recevoir(&self) -> Vec<Value> {
        verrou(&self.nouvelles).try_iter().collect()
    }

    /// Les marques reçues du serveur (et les coupures) depuis la dernière
    /// fois. La file est celle de la connexion, partagée par les services :
    /// le premier qui la lit prend tout.
    pub fn recevoir_marques(&self) -> Vec<NouvelleMarques> {
        std::mem::take(&mut *verrou(&self.cx.marques))
    }

    /// Envoie une trame `marques` (faite par [`SuiviMarques`]) ; faux si la
    /// connexion est coupée.
    pub fn envoyer_marques(&self, trame: &Value) -> bool {
        self.cx.envoyer_direct(&trame.to_string())
    }
}

impl Drop for PontDistant {
    fn drop(&mut self) {
        self.cx.desabonner(&self.svc);
    }
}

// ---------------------------------------------------------------------------
// Les marques (Head, favoris, épinglées, archives), gardées par le serveur
// ---------------------------------------------------------------------------

/// Au-delà de dix ans, autant tout afficher (`cacher_apres_jours`, comme le serveur).
pub const CACHER_APRES_JOURS_MAX: u32 = 3650;

/// Les quatre listes ordonnées d'ids de conversations (PROTOCOLE.md, §5.5).
#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct Marques {
    pub head: Vec<String>,
    /// La vue de l'étoile (`etoiles` dans les réglages du bureau).
    pub favoris: Vec<String>,
    pub epingles: Vec<String>,
    /// L'archivage propre à Jasette (Messenger, Instagram, Signal, X).
    pub archives: Vec<String>,
    /// La Liste privée : cachées partout sauf dans la vue Privées. Le code
    /// qui ouvre cette vue n'est jamais synchronisé.
    pub privees: Vec<String>,
    /// Le Mood de chaque conversation (id → nom), PROTOCOLE.md §5.5.
    pub moods: BTreeMap<String, String>,
    /// L'emoji rapide de chaque conversation (id → emoji), PROTOCOLE.md §5.5.
    pub emojis: BTreeMap<String, String>,
    /// « Garder les conversations archivées hors de la boîte de réception » :
    /// un message reçu ne désarchive plus (PROTOCOLE.md §5.5). Faux par défaut.
    pub garder_archives: bool,
    /// « Cacher les conversations plus anciennes de la boîte de réception »,
    /// en jours (0 : tout afficher), PROTOCOLE.md §5.5. Filtré à l'affichage.
    pub cacher_apres_jours: u32,
    /// « Afficher qui est en ligne » désactivé (marque `presence_en_ligne`
    /// à faux, PROTOCOLE.md §5.10). Gardé à l'envers : faux, le réglage par
    /// défaut, veut dire « afficher ».
    pub presence_cachee: bool,
}

impl Marques {
    pub fn vide(&self) -> bool {
        self.head.is_empty()
            && self.favoris.is_empty()
            && self.epingles.is_empty()
            && self.archives.is_empty()
            && self.privees.is_empty()
            && self.moods.is_empty()
            && self.emojis.is_empty()
            && !self.garder_archives
            && self.cacher_apres_jours == 0
            && !self.presence_cachee
    }

    /// Chaque liste de `dabord`, dans son ordre, puis les ids de `ensuite` qui n'y sont pas.
    pub fn union(dabord: &Marques, ensuite: &Marques) -> Marques {
        let unir = |a: &Vec<String>, b: &Vec<String>| {
            let mut r: Vec<String> = Vec::with_capacity(a.len() + b.len());
            for id in a.iter().chain(b.iter()) {
                if !id.is_empty() && !r.contains(id) {
                    r.push(id.clone());
                }
            }
            r
        };
        Marques {
            head: unir(&dabord.head, &ensuite.head),
            favoris: unir(&dabord.favoris, &ensuite.favoris),
            epingles: unir(&dabord.epingles, &ensuite.epingles),
            archives: unir(&dabord.archives, &ensuite.archives),
            privees: unir(&dabord.privees, &ensuite.privees),
            // Les moods de `dabord` gagnent, `ensuite` ajoute les ids qui manquent.
            moods: ensuite.moods.iter().chain(dabord.moods.iter()).map(|(k, v)| (k.clone(), v.clone())).collect(),
            // De même pour les emojis rapides.
            emojis: ensuite.emojis.iter().chain(dabord.emojis.iter()).map(|(k, v)| (k.clone(), v.clone())).collect(),
            // Un oui/non : celui de `dabord`.
            garder_archives: dabord.garder_archives,
            cacher_apres_jours: dabord.cacher_apres_jours,
            presence_cachee: dabord.presence_cachee,
        }
    }

    /// Les ids de `self` absents de `autre`, liste par liste.
    fn moins(&self, autre: &Marques) -> Marques {
        let garder = |a: &Vec<String>, b: &Vec<String>| a.iter().filter(|id| !b.contains(id)).cloned().collect();
        Marques {
            head: garder(&self.head, &autre.head),
            favoris: garder(&self.favoris, &autre.favoris),
            epingles: garder(&self.epingles, &autre.epingles),
            archives: garder(&self.archives, &autre.archives),
            privees: garder(&self.privees, &autre.privees),
            moods: self.moods.iter().filter(|(k, v)| autre.moods.get(*k) != Some(*v)).map(|(k, v)| (k.clone(), v.clone())).collect(),
            emojis: self.emojis.iter().filter(|(k, v)| autre.emojis.get(*k) != Some(*v)).map(|(k, v)| (k.clone(), v.clone())).collect(),
            garder_archives: self.garder_archives,
            cacher_apres_jours: self.cacher_apres_jours,
            presence_cachee: self.presence_cachee,
        }
    }
}

/// Ce que l'interface a reçu du serveur à propos des marques.
#[derive(Clone, Debug, PartialEq, Eq)]
pub enum NouvelleMarques {
    /// Une trame `{"marques":{…}}` : rejeu, diffusion ou réponse à notre envoi.
    /// `avec_archives` : faux pour un serveur d'avant `archives` (champ absent) ;
    /// le client garde alors ses archives pour lui seul.
    /// `avec_privees` : de même pour un serveur d'avant `privees`.
    /// `avec_moods` : de même pour un serveur d'avant `moods`.
    /// `avec_emojis` : de même pour un serveur d'avant `emojis`.
    /// `avec_garder` : de même pour un serveur d'avant `garder_archives`.
    /// `avec_cacher` : de même pour un serveur d'avant `cacher_apres_jours`.
    /// `avec_presence` : de même pour un serveur d'avant `presence_en_ligne`.
    Recues {
        marques: Marques,
        version: u64,
        id: Option<u64>,
        refus: Option<String>,
        avec_archives: bool,
        avec_privees: bool,
        avec_moods: bool,
        avec_emojis: bool,
        avec_garder: bool,
        avec_cacher: bool,
        avec_presence: bool,
    },
    /// La connexion est tombée : rien n'est plus en vol.
    Coupure,
}

impl NouvelleMarques {
    fn lire(v: &Value) -> Option<Self> {
        let liste = |k: &str| -> Option<Vec<String>> {
            match v.get(k) {
                None | Some(Value::Null) => Some(Vec::new()),
                Some(Value::Array(a)) => Some(a.iter().filter_map(Value::as_str).map(str::to_string).collect()),
                Some(_) => None,
            }
        };
        // Un objet {id: texte} : entrées vides ou démesurées ignorées.
        let objet = |nom: &str, max: usize| -> BTreeMap<String, String> {
            match v.get(nom) {
                Some(Value::Object(o)) => o
                    .iter()
                    .filter_map(|(k, n)| Some((k.clone(), n.as_str()?.to_string())))
                    .filter(|(k, n)| !k.is_empty() && !n.is_empty() && n.len() <= max)
                    .collect(),
                _ => BTreeMap::new(),
            }
        };
        Some(Self::Recues {
            marques: Marques {
                head: liste("head")?,
                favoris: liste("favoris")?,
                epingles: liste("epingles")?,
                archives: liste("archives")?,
                privees: liste("privees")?,
                moods: objet("moods", 64),
                emojis: objet("emojis", EMOJI_RAPIDE_MAX),
                garder_archives: v.get("garder_archives").and_then(Value::as_bool).unwrap_or(false),
                // Négatif ou démesuré : 0 (tout afficher), comme le serveur.
                cacher_apres_jours: v
                    .get("cacher_apres_jours")
                    .and_then(Value::as_u64)
                    .filter(|j| *j <= CACHER_APRES_JOURS_MAX as u64)
                    .unwrap_or(0) as u32,
                // Absent : vrai (afficher).
                presence_cachee: v.get("presence_en_ligne").and_then(Value::as_bool) == Some(false),
            },
            version: v.get("version")?.as_u64()?,
            id: v.get("id").and_then(Value::as_u64),
            refus: v.get("refus").and_then(Value::as_str).map(str::to_string),
            avec_archives: v.get("archives").is_some_and(Value::is_array),
            avec_privees: v.get("privees").is_some_and(Value::is_array),
            avec_moods: v.get("moods").is_some_and(Value::is_object),
            avec_emojis: v.get("emojis").is_some_and(Value::is_object),
            avec_garder: v.get("garder_archives").is_some_and(Value::is_boolean),
            avec_cacher: v.get("cacher_apres_jours").is_some_and(Value::is_number),
            avec_presence: v.get("presence_en_ligne").is_some_and(Value::is_boolean),
        })
    }
}

/// Un emoji rapide fait au plus 32 octets (un graphème, PROTOCOLE.md §5.5).
pub const EMOJI_RAPIDE_MAX: usize = 32;

/// La copie locale des marques, gardée dans les réglages : les listes, la
/// dernière version reçue du serveur (0 : jamais synchronisé avec lui) et
/// « changées ici, pas encore confirmées par le serveur ».
#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct CopieMarques {
    pub listes: Marques,
    pub version: u64,
    pub a_envoyer: bool,
}

/// La synchronisation des marques avec le serveur (PROTOCOLE.md, §5.5) :
/// une seule trame en vol à la fois ; les changements faits entre-temps
/// partent à la réponse. Ne garde rien sur disque : la [`CopieMarques`] le fait.
#[derive(Debug, Default)]
pub struct SuiviMarques {
    /// Le serveur a donné son état sur cette connexion (rejeu reçu).
    synchro: bool,
    /// Notre trame en attente de réponse : son id et ce qu'elle portait.
    en_vol: Option<(u64, Marques)>,
    prochain: u64,
}

impl SuiviMarques {
    /// Tout recommence (changement de serveur, de mode) : on attend le rejeu.
    pub fn reinitialiser(&mut self) {
        self.synchro = false;
        self.en_vol = None;
    }

    /// L'envoi d'une trame a échoué : la connexion tombe, le rejeu suivant tranchera.
    pub fn envoi_rate(&mut self) {
        self.reinitialiser();
    }

    /// Les marques viennent de changer ici. Rend la trame à envoyer, s'il le faut maintenant.
    pub fn changement(&mut self, c: &mut CopieMarques) -> Option<Value> {
        c.a_envoyer = true;
        self.pousser(c)
    }

    fn pousser(&mut self, c: &CopieMarques) -> Option<Value> {
        if !self.synchro || self.en_vol.is_some() || !c.a_envoyer {
            return None;
        }
        self.prochain += 1;
        let id = self.prochain;
        self.en_vol = Some((id, c.listes.clone()));
        Some(json!({"marques": {
            "head": c.listes.head,
            "favoris": c.listes.favoris,
            "epingles": c.listes.epingles,
            "archives": c.listes.archives,
            "privees": c.listes.privees,
            "moods": c.listes.moods,
            "emojis": c.listes.emojis,
            "garder_archives": c.listes.garder_archives,
            "cacher_apres_jours": c.listes.cacher_apres_jours,
            "presence_en_ligne": !c.listes.presence_cachee,
            "version": c.version,
            "id": id,
        }}))
    }

    /// Une nouvelle du serveur : met la copie locale à jour, et rend la trame
    /// à renvoyer s'il le faut (union, changements faits pendant l'envoi…).
    pub fn recu(&mut self, n: NouvelleMarques, c: &mut CopieMarques) -> Option<Value> {
        let (s, v, id, refus) = match n {
            NouvelleMarques::Coupure => {
                self.reinitialiser();
                return None;
            }
            NouvelleMarques::Recues {
                marques,
                version,
                id,
                refus,
                avec_archives,
                avec_privees,
                avec_moods,
                avec_emojis,
                avec_garder,
                avec_cacher,
                avec_presence,
            } => {
                // Un serveur d'avant `archives` : nos archives restent les nôtres, jamais vidées.
                let mut marques = marques;
                if !avec_archives {
                    marques.archives = c.listes.archives.clone();
                }
                // De même pour les privées (serveur d'avant `privees`).
                if !avec_privees {
                    marques.privees = c.listes.privees.clone();
                }
                // Et pour les moods (serveur d'avant `moods`).
                if !avec_moods {
                    marques.moods = c.listes.moods.clone();
                }
                // Et pour les emojis rapides (serveur d'avant `emojis`).
                if !avec_emojis {
                    marques.emojis = c.listes.emojis.clone();
                }
                // Et pour `garder_archives` (serveur d'avant ce réglage).
                if !avec_garder {
                    marques.garder_archives = c.listes.garder_archives;
                }
                // Et pour `cacher_apres_jours`.
                if !avec_cacher {
                    marques.cacher_apres_jours = c.listes.cacher_apres_jours;
                }
                // Et pour `presence_en_ligne`.
                if !avec_presence {
                    marques.presence_cachee = c.listes.presence_cachee;
                }
                (marques, version, id, refus)
            }
        };
        self.synchro = true;
        let notre = matches!((&self.en_vol, id), (Some((e, _)), Some(i)) if *e == i);
        if notre {
            let (_, envoyees) = self.en_vol.take().unwrap_or_default();
            c.version = v;
            c.listes = if refus.is_some() {
                // Trame refusée (version périmée qui aurait vidé une liste) :
                // l'ordre du serveur, puis ce qu'on a de plus.
                eprintln!("jasette : marques refusées par le serveur ; union avec les siennes");
                Marques::union(&s, &c.listes)
            } else {
                // Acceptée : ce qu'on a encore changé depuis, plus ce que le
                // serveur a ajouté de son côté (union faite par lui).
                Marques::union(&c.listes, &s.moins(&envoyees))
            };
            c.a_envoyer = c.listes != s;
            return self.pousser(c);
        }
        if self.en_vol.is_some() {
            // La diffusion d'un autre appareil a croisé notre trame : la
            // réponse à celle-ci donnera l'état final.
            return None;
        }
        if v == 0 {
            // Le serveur n'a encore rien : il prend nos marques.
            c.version = 0;
            c.a_envoyer = !c.listes.vide();
            return self.pousser(c);
        }
        if !c.a_envoyer && c.version > 0 && c.version <= v {
            // Rien de changé ici : le serveur est la source.
            c.listes = s;
            c.version = v;
            return None;
        }
        if c.a_envoyer && c.version == v {
            // Changé ici pendant une coupure, et personne d'autre n'a rien changé.
            return self.pousser(c);
        }
        // Première synchronisation avec ce serveur, ou changé des deux côtés :
        // union, l'ordre du serveur d'abord. Personne ne perd rien.
        c.listes = Marques::union(&s, &c.listes);
        c.version = v;
        c.a_envoyer = c.listes != s;
        self.pousser(c)
    }
}

// ---------------------------------------------------------------------------
// Petits outils
// ---------------------------------------------------------------------------

/// Le cache local des fichiers d'un serveur : sous JASETTE_MEDIA, sinon
/// JASETTE_DATA, sinon (en développement) .cache du projet, sinon
/// ~/.local/share/jasette/distant/<hôte>.
fn dossier_cache(adresse: &str) -> PathBuf {
    let hote: String = adresse
        .trim()
        .chars()
        .map(|c| if c.is_ascii_alphanumeric() || c == '.' || c == '-' { c } else { '_' })
        .collect();
    let base = if let Some(m) = std::env::var_os("JASETTE_MEDIA") {
        PathBuf::from(m)
    } else if let Some(d) = std::env::var_os("JASETTE_DATA") {
        PathBuf::from(d)
    } else if PathBuf::from(env!("CARGO_MANIFEST_DIR")).join(".cache").is_dir() {
        PathBuf::from(env!("CARGO_MANIFEST_DIR")).join(".cache/media")
    } else {
        let donnees = std::env::var_os("XDG_DATA_HOME")
            .map(PathBuf::from)
            .or_else(|| std::env::var_os("HOME").map(|h| PathBuf::from(h).join(".local/share")))
            .unwrap_or_else(|| PathBuf::from("."));
        donnees.join("jasette")
    };
    let d = base.join("distant").join(hote);
    let _ = fs::create_dir_all(&d);
    #[cfg(unix)]
    {
        use std::os::unix::fs::PermissionsExt;
        let _ = fs::set_permissions(&d, fs::Permissions::from_mode(0o700));
    }
    d
}

/// Nom local d'un fichier du serveur : SHA-256 du chemin, et son extension.
fn nom_local(distant: &str) -> String {
    let h = hex(&digest::digest(&digest::SHA256, distant.as_bytes()).as_ref()[..16]);
    let ext: String = Path::new(distant)
        .extension()
        .map(|e| e.to_string_lossy().chars().filter(char::is_ascii_alphanumeric).take(8).collect())
        .unwrap_or_default();
    if ext.is_empty() { h } else { format!("{h}.{ext}") }
}

fn sha256_fichier(p: &Path) -> Option<String> {
    let mut f = fs::File::open(p).ok()?;
    let mut ctx = digest::Context::new(&digest::SHA256);
    let mut buf = vec![0u8; 64 << 10];
    loop {
        match f.read(&mut buf) {
            Ok(0) => break,
            Ok(n) => ctx.update(&buf[..n]),
            Err(_) => return None,
        }
    }
    Some(hex(ctx.finish().as_ref()))
}

fn aleatoire<const N: usize>() -> [u8; N] {
    let mut b = [0u8; N];
    let _ = SystemRandom::new().fill(&mut b);
    b
}

fn hex(b: &[u8]) -> String {
    b.iter().map(|o| format!("{o:02x}")).collect()
}

fn de_hex(s: &str) -> Vec<u8> {
    (0..s.len() / 2).filter_map(|i| u8::from_str_radix(s.get(2 * i..2 * i + 2)?, 16).ok()).collect()
}

// ---------------------------------------------------------------------------
// Essai de bout en bout (contre un serveur de test, voir serveur/PROTOCOLE.md)
// ---------------------------------------------------------------------------

#[cfg(test)]
mod essais {
    use super::*;

    /// JASETTE_ESSAI_ADRESSE=127.0.0.1:47899 JASETTE_ESSAI_CODE=123456 \
    /// cargo test essai_serveur -- --ignored --nocapture
    #[test]
    #[ignore]
    fn essai_serveur() {
        let adresse = std::env::var("JASETTE_ESSAI_ADRESSE").expect("JASETTE_ESSAI_ADRESSE");
        let code = std::env::var("JASETTE_ESSAI_CODE").expect("JASETTE_ESSAI_CODE");
        let v = verifier_serveur(&adresse, &code).expect("vérification");
        println!("empreinte {} vérification {}", v.empreinte, v.code_verification);
        if let Ok(attendu) = std::env::var("JASETTE_ESSAI_VERIF") {
            assert_eq!(v.code_verification, attendu, "code de vérification");
        }
        // Une mauvaise empreinte doit être refusée avant tout envoi.
        let faux = "00".repeat(32);
        assert!(jumeler(&adresse, &code, "essai", &faux).unwrap_err().contains("certificat"));
        let config = jumeler(&adresse, &code, "essai", &v.empreinte).expect("jumelage");
        println!("jumelé : {config:?}");
        // Le code ne sert qu'une fois.
        assert!(jumeler(&adresse, &code, "essai2", &v.empreinte).is_err());

        let google = PontDistant::brancher(&config, "google");
        let messenger = PontDistant::brancher(&config, "messenger");
        let mut vus_g = Vec::new();
        let mut vus_m = Vec::new();
        let fin = Instant::now() + Duration::from_secs(20);
        while Instant::now() < fin {
            vus_g.extend(google.recevoir());
            vus_m.extend(messenger.recevoir());
            let a = |l: &Vec<Value>, e: &str| l.iter().any(|v| v["ev"] == e);
            if a(&vus_g, "hello") && a(&vus_g, "state") && a(&vus_m, "hello") && a(&vus_m, "state") {
                break;
            }
            std::thread::sleep(Duration::from_millis(100));
        }
        println!("google : {vus_g:?}");
        println!("messenger : {vus_m:?}");
        for (nom, l) in [("google", &vus_g), ("messenger", &vus_m)] {
            assert!(l.iter().any(|v| v["ev"] == "hello"), "{nom} : pas de hello");
            assert!(l.iter().any(|v| v["ev"] == "state" && v["state"] != "connecting"), "{nom} : pas de state");
        }
        // « quit » est ignoré : le pont du serveur reste en vie.
        google.envoyer(&json!({"cmd": "quit"}));

        // Fichiers : un média du serveur, téléchargé et réécrit.
        if let Ok(fichier) = std::env::var("JASETTE_ESSAI_FICHIER") {
            let local = google.cx.telecharger(&fichier).expect("téléchargement");
            assert_eq!(fs::read(&local).unwrap(), fs::read(&fichier).unwrap());
            let mut ev = json!({"ev": "avatar", "conv": "x", "path": fichier});
            google.cx.reecrire(&mut ev);
            assert_eq!(ev["path"].as_str().unwrap(), local.to_str().unwrap());
            // Aussi dans une liste (les contacts : `items[].path`).
            let mut ev = json!({"ev": "contacts", "items": [{"id": "5145550000", "name": "x", "path": fichier}]});
            google.cx.reecrire(&mut ev);
            assert_eq!(ev["items"][0]["path"].as_str().unwrap(), local.to_str().unwrap());
            // Hors des racines : refusé.
            assert!(google.cx.telecharger("/etc/passwd").is_none());
            assert!(google.cx.telecharger(&format!("{fichier}/../../serveur/cle.pem")).is_none());
            // Téléversement.
            let chemin = google.cx.televerser(Path::new(&fichier)).expect("téléversement");
            println!("déposé : {chemin}");
            assert_eq!(fs::read(&chemin).unwrap(), fs::read(&fichier).unwrap());
        }

        // Rejeu pour un service branché en retard.
        drop(messenger);
        let messenger = PontDistant::brancher(&config, "messenger");
        let fin = Instant::now() + Duration::from_secs(5);
        let mut l = Vec::new();
        while Instant::now() < fin && !l.iter().any(|v: &Value| v["ev"] == "hello") {
            l.extend(messenger.recevoir());
            std::thread::sleep(Duration::from_millis(50));
        }
        assert!(l.iter().any(|v| v["ev"] == "hello"), "rejeu : {l:?}");
        println!("rejeu : {l:?}");
    }

    /// Comme essai_serveur ; pendant les 40 s qui suivent, couper puis
    /// relancer le serveur : on doit voir « reconnecting » puis l'état réel.
    #[test]
    #[ignore]
    fn essai_reconnexion() {
        let adresse = std::env::var("JASETTE_ESSAI_ADRESSE").expect("JASETTE_ESSAI_ADRESSE");
        let code = std::env::var("JASETTE_ESSAI_CODE").expect("JASETTE_ESSAI_CODE");
        let v = verifier_serveur(&adresse, &code).expect("vérification");
        let config = jumeler(&adresse, &code, "reconnexion", &v.empreinte).expect("jumelage");
        let google = PontDistant::brancher(&config, "google");
        let mut etats = Vec::new();
        let fin = Instant::now() + Duration::from_secs(40);
        while Instant::now() < fin {
            for ev in google.recevoir() {
                if ev["ev"] == "state" {
                    println!("{:>5.1} s : {}", 40.0 - (fin - Instant::now()).as_secs_f32(), ev["state"]);
                    etats.push(ev["state"].as_str().unwrap_or_default().to_string());
                }
            }
            std::thread::sleep(Duration::from_millis(100));
        }
        let i = etats.iter().position(|e| e == "reconnecting").expect("pas de reconnecting");
        assert!(etats[i..].iter().any(|e| e == "unpaired"), "pas d'état après la reconnexion : {etats:?}");
    }

    // -----------------------------------------------------------------------
    // Les marques
    // -----------------------------------------------------------------------

    fn m(head: &[&str], favoris: &[&str], epingles: &[&str]) -> Marques {
        let v = |l: &[&str]| l.iter().map(|x| x.to_string()).collect();
        Marques { head: v(head), favoris: v(favoris), epingles: v(epingles), ..Default::default() }
    }

    fn recues(marques: Marques, version: u64, id: Option<u64>) -> NouvelleMarques {
        NouvelleMarques::Recues { marques, version, id, refus: None,
            avec_archives: true,
            avec_privees: true,
            avec_moods: true,
            avec_emojis: true,
            avec_garder: true,
            avec_cacher: true,
            avec_presence: true,
        }
    }

    fn id_de(t: &Value) -> u64 {
        t["marques"]["id"].as_u64().unwrap()
    }

    #[test]
    fn marques_serveur_vide_recoit_les_locales() {
        let mut s = SuiviMarques::default();
        let mut c = CopieMarques { listes: m(&[], &["a", "b"], &[]), version: 0, a_envoyer: false };
        let t = s.recu(recues(Marques::default(), 0, None), &mut c).expect("envoi");
        assert_eq!(t["marques"]["version"], 0);
        assert_eq!(t["marques"]["favoris"], json!(["a", "b"]));
        // Réponse : acceptée, version 1.
        assert!(s.recu(recues(m(&[], &["a", "b"], &[]), 1, Some(id_de(&t))), &mut c).is_none());
        assert_eq!(c, CopieMarques { listes: m(&[], &["a", "b"], &[]), version: 1, a_envoyer: false });
    }

    #[test]
    fn marques_premiere_connexion_union_ordre_du_serveur() {
        let mut s = SuiviMarques::default();
        let mut c = CopieMarques { listes: m(&["h"], &["b", "x"], &[]), version: 0, a_envoyer: false };
        let t = s.recu(recues(m(&[], &["a", "b"], &["p"]), 4, None), &mut c).expect("union envoyée");
        assert_eq!(c.listes, m(&["h"], &["a", "b", "x"], &["p"]));
        assert_eq!(t["marques"]["version"], 4);
        // Rien de local en plus : on adopte sans rien envoyer.
        let mut s2 = SuiviMarques::default();
        let mut c2 = CopieMarques::default();
        assert!(s2.recu(recues(m(&[], &["a"], &[]), 4, None), &mut c2).is_none());
        assert_eq!(c2, CopieMarques { listes: m(&[], &["a"], &[]), version: 4, a_envoyer: false });
    }

    #[test]
    fn marques_le_serveur_est_la_source() {
        let mut s = SuiviMarques::default();
        let mut c = CopieMarques { listes: m(&[], &["a", "b"], &[]), version: 3, a_envoyer: false };
        // Un autre appareil a retiré « b » : on suit, même vers moins.
        assert!(s.recu(recues(m(&[], &["a"], &[]), 4, None), &mut c).is_none());
        assert_eq!(c.listes, m(&[], &["a"], &[]));
        assert_eq!(c.version, 4);
    }

    #[test]
    fn marques_une_trame_en_vol_a_la_fois() {
        let mut s = SuiviMarques::default();
        let mut c = CopieMarques { listes: m(&[], &["a", "b", "c"], &[]), version: 5, a_envoyer: false };
        assert!(s.recu(recues(c.listes.clone(), 5, None), &mut c).is_none());
        c.listes.favoris.retain(|x| x != "b");
        let t1 = s.changement(&mut c).expect("premier envoi");
        c.listes.favoris.retain(|x| x != "c");
        assert!(s.changement(&mut c).is_none(), "attend la réponse");
        // Réponse au premier : le second part, avec la nouvelle version.
        let t2 = s.recu(recues(m(&[], &["a", "c"], &[]), 6, Some(id_de(&t1))), &mut c).expect("second envoi");
        assert_eq!(t2["marques"]["version"], 6);
        assert_eq!(t2["marques"]["favoris"], json!(["a"]));
        assert!(s.recu(recues(m(&[], &["a"], &[]), 7, Some(id_de(&t2))), &mut c).is_none());
        assert_eq!(c, CopieMarques { listes: m(&[], &["a"], &[]), version: 7, a_envoyer: false });
    }

    #[test]
    fn marques_archives() {
        // Lues et envoyées comme les autres listes.
        let n = NouvelleMarques::lire(&json!({"head": [], "favoris": [], "epingles": [], "archives": ["m:1"], "version": 3}));
        assert!(matches!(&n, Some(NouvelleMarques::Recues { marques, avec_archives: true, .. }) if marques.archives == ["m:1"]));
        let mut s = SuiviMarques::default();
        let mut c = CopieMarques { listes: m(&[], &[], &[]), version: 3, a_envoyer: false };
        assert!(s.recu(n.unwrap(), &mut c).is_none());
        assert_eq!(c.listes.archives, ["m:1"]);
        c.listes.archives.push("s:2".into());
        let t = s.changement(&mut c).expect("envoi");
        assert_eq!(t["marques"]["archives"], json!(["m:1", "s:2"]));
        // Un serveur d'avant `archives` (champ absent) : les nôtres restent, sans rien renvoyer.
        let mut s = SuiviMarques::default();
        let mut c = CopieMarques { listes: m(&[], &["a"], &[]), version: 3, a_envoyer: false };
        c.listes.archives = vec!["m:1".into()];
        let vieux = NouvelleMarques::lire(&json!({"head": [], "favoris": ["b"], "epingles": [], "version": 4})).unwrap();
        assert!(s.recu(vieux, &mut c).is_none());
        assert_eq!(c.listes.favoris, ["b"]);
        assert_eq!(c.listes.archives, ["m:1"]);
    }

    #[test]
    fn marques_privees() {
        // La liste `privees` va et vient comme les autres.
        let n = NouvelleMarques::lire(
            &json!({"head": [], "favoris": [], "epingles": [], "archives": [], "privees": ["s:1"], "version": 3}),
        );
        let mut s = SuiviMarques::default();
        let mut c = CopieMarques::default();
        s.recu(n.unwrap(), &mut c);
        assert_eq!(c.listes.privees, ["s:1"]);
        c.listes.privees.push("m:2".into());
        let t = s.changement(&mut c).expect("envoi");
        assert_eq!(t["marques"]["privees"], json!(["s:1", "m:2"]));
        // Un serveur d'avant `privees` : les nôtres restent, jamais vidées.
        let mut s = SuiviMarques::default();
        let mut c = CopieMarques { version: 4, ..Default::default() };
        c.listes.privees = vec!["s:1".into()];
        let vieux = NouvelleMarques::lire(
            &json!({"head": [], "favoris": ["b"], "epingles": [], "archives": [], "version": 4}),
        )
        .unwrap();
        assert!(s.recu(vieux, &mut c).is_none());
        assert_eq!(c.listes.privees, ["s:1"]);
    }

    #[test]
    fn marques_emojis() {
        // L'emoji rapide va et vient ; trop long (plus de 32 octets) : ignoré.
        let long = "👍".repeat(9);
        let n = NouvelleMarques::lire(&json!({"head": [], "favoris": [], "epingles": [], "archives": [], "privees": [],
            "moods": {}, "emojis": {"m:1": "👍", "x:2": "", "w:3": long}, "version": 3}));
        let mut s = SuiviMarques::default();
        let mut c = CopieMarques::default();
        s.recu(n.unwrap(), &mut c);
        assert_eq!(c.listes.emojis.len(), 1);
        c.listes.emojis.insert("s:1".into(), "❤️".into());
        c.listes.emojis.remove("m:1");
        let t = s.changement(&mut c).expect("envoi");
        assert_eq!(t["marques"]["emojis"], json!({"s:1": "❤️"}));
        // Un serveur d'avant `emojis` : les nôtres restent.
        let mut s = SuiviMarques::default();
        let mut c = CopieMarques { version: 4, ..Default::default() };
        c.listes.emojis.insert("s:1".into(), "🔥".into());
        let vieux = NouvelleMarques::lire(&json!({"head": [], "favoris": ["b"], "epingles": [], "archives": [], "privees": [], "moods": {}, "version": 4})).unwrap();
        assert!(s.recu(vieux, &mut c).is_none());
        assert_eq!(c.listes.emojis.get("s:1").map(String::as_str), Some("🔥"));
        // Union : le premier gagne pour un même id.
        let mut a = Marques::default();
        a.emojis.insert("k".into(), "👍".into());
        let mut b = Marques::default();
        b.emojis.insert("k".into(), "🎉".into());
        b.emojis.insert("j".into(), "🌹".into());
        let u = Marques::union(&a, &b);
        assert_eq!(u.emojis.get("k").map(String::as_str), Some("👍"));
        assert_eq!(u.emojis.len(), 2);
    }

    #[test]
    fn marques_garder_archives() {
        // Absent du rejeu d'un serveur à jour ? Il l'envoie toujours ; faux par défaut.
        let n = NouvelleMarques::lire(&json!({"head": [], "favoris": [], "epingles": [], "archives": ["m:1"], "privees": [],
            "moods": {}, "emojis": {}, "garder_archives": false, "version": 3}));
        let mut s = SuiviMarques::default();
        let mut c = CopieMarques::default();
        s.recu(n.unwrap(), &mut c);
        assert!(!c.listes.garder_archives);
        // Activé ici : envoyé.
        c.listes.garder_archives = true;
        let t = s.changement(&mut c).expect("envoi");
        assert_eq!(t["marques"]["garder_archives"], json!(true));
        // Activé ailleurs : reçu.
        let mut s = SuiviMarques::default();
        let mut c = CopieMarques { version: 4, ..Default::default() };
        let n = NouvelleMarques::lire(&json!({"head": [], "favoris": ["b"], "epingles": [], "archives": [], "privees": [],
            "moods": {}, "emojis": {}, "garder_archives": true, "version": 5}));
        assert!(s.recu(n.unwrap(), &mut c).is_none());
        assert!(c.listes.garder_archives);
        // Un serveur d'avant `garder_archives` : le nôtre reste.
        let mut s = SuiviMarques::default();
        let mut c = CopieMarques { version: 4, ..Default::default() };
        c.listes.garder_archives = true;
        let vieux = NouvelleMarques::lire(&json!({"head": [], "favoris": ["b"], "epingles": [], "archives": [], "privees": [],
            "moods": {}, "emojis": {}, "version": 5})).unwrap();
        assert!(s.recu(vieux, &mut c).is_none());
        assert!(c.listes.garder_archives);
        // Seul réglage posé : ce n'est pas « vide », le serveur neuf le reçoit.
        assert!(!c.listes.vide());
    }

    #[test]
    fn marques_presence_en_ligne() {
        // Absent : vrai (afficher) ; envoyé tel quel.
        let n = NouvelleMarques::lire(&json!({"head": [], "favoris": [], "epingles": [], "archives": [], "privees": [],
            "moods": {}, "emojis": {}, "presence_en_ligne": false, "version": 5}));
        let mut s = SuiviMarques::default();
        let mut c = CopieMarques { version: 4, ..Default::default() };
        assert!(s.recu(n.unwrap(), &mut c).is_none());
        assert!(c.listes.presence_cachee);
        c.listes.presence_cachee = false;
        let t = s.changement(&mut c).expect("envoi");
        assert_eq!(t["marques"]["presence_en_ligne"], json!(true));
        // Un serveur d'avant `presence_en_ligne` : le nôtre reste.
        let mut s = SuiviMarques::default();
        let mut c = CopieMarques { version: 4, ..Default::default() };
        c.listes.presence_cachee = true;
        let vieux = NouvelleMarques::lire(&json!({"head": [], "favoris": ["b"], "epingles": [], "archives": [], "privees": [],
            "moods": {}, "emojis": {}, "version": 5})).unwrap();
        assert!(s.recu(vieux, &mut c).is_none());
        assert!(c.listes.presence_cachee);
        assert!(!c.listes.vide());
        // Le défaut (afficher) : vide.
        assert!(Marques::default().vide());
    }

    #[test]
    fn marques_cacher_apres_jours() {
        // Reçu du serveur.
        let mut s = SuiviMarques::default();
        let mut c = CopieMarques { version: 4, ..Default::default() };
        let n = NouvelleMarques::lire(&json!({"head": [], "favoris": ["b"], "epingles": [], "archives": [], "privees": [],
            "moods": {}, "emojis": {}, "garder_archives": false, "cacher_apres_jours": 30, "version": 5}));
        assert!(s.recu(n.unwrap(), &mut c).is_none());
        assert_eq!(c.listes.cacher_apres_jours, 30);
        // Changé ici : envoyé.
        c.listes.cacher_apres_jours = 365;
        let t = s.changement(&mut c).expect("envoi");
        assert_eq!(t["marques"]["cacher_apres_jours"], json!(365));
        // Un serveur d'avant ce réglage : le nôtre reste.
        let mut s = SuiviMarques::default();
        let mut c = CopieMarques { version: 4, ..Default::default() };
        c.listes.cacher_apres_jours = 7;
        let vieux = NouvelleMarques::lire(&json!({"head": [], "favoris": ["b"], "epingles": [], "archives": [], "privees": [],
            "moods": {}, "emojis": {}, "garder_archives": false, "version": 5})).unwrap();
        assert!(s.recu(vieux, &mut c).is_none());
        assert_eq!(c.listes.cacher_apres_jours, 7);
        assert!(!c.listes.vide());
        // Une valeur absurde : 0.
        let n = NouvelleMarques::lire(&json!({"head": [], "favoris": [], "epingles": [], "archives": [], "privees": [],
            "cacher_apres_jours": -3, "version": 5})).unwrap();
        let NouvelleMarques::Recues { marques, .. } = n else { panic!() };
        assert_eq!(marques.cacher_apres_jours, 0);
    }

    #[test]
    fn marques_moods() {
        // Les moods vont et viennent ; un objet, jamais null.
        let n = NouvelleMarques::lire(&json!({"head": [], "favoris": [], "epingles": [], "archives": [], "privees": [],
            "moods": {"m:1": "amour", "x:2": ""}, "version": 3}));
        let mut s = SuiviMarques::default();
        let mut c = CopieMarques::default();
        s.recu(n.unwrap(), &mut c);
        assert_eq!(c.listes.moods.len(), 1);
        c.listes.moods.insert("s:1".into(), "ocean".into());
        c.listes.moods.remove("m:1");
        let t = s.changement(&mut c).expect("envoi");
        assert_eq!(t["marques"]["moods"], json!({"s:1": "ocean"}));
        // Un serveur d'avant `moods` : les nôtres restent.
        let mut s = SuiviMarques::default();
        let mut c = CopieMarques { version: 4, ..Default::default() };
        c.listes.moods.insert("s:1".into(), "menthe".into());
        let vieux = NouvelleMarques::lire(&json!({"head": [], "favoris": ["b"], "epingles": [], "archives": [], "privees": [], "version": 4})).unwrap();
        assert!(s.recu(vieux, &mut c).is_none());
        assert_eq!(c.listes.moods.get("s:1").map(String::as_str), Some("menthe"));
        // Union : le premier gagne pour un même id.
        let mut a = Marques::default();
        a.moods.insert("k".into(), "amour".into());
        let mut b = Marques::default();
        b.moods.insert("k".into(), "soleil".into());
        b.moods.insert("j".into(), "nuit".into());
        let u = Marques::union(&a, &b);
        assert_eq!(u.moods.get("k").map(String::as_str), Some("amour"));
        assert_eq!(u.moods.len(), 2);
    }

    #[test]
    fn marques_hors_ligne() {
        // Changé pendant une coupure, personne d'autre : la copie locale part telle quelle.
        let mut s = SuiviMarques::default();
        let mut c = CopieMarques { listes: m(&[], &["a", "b"], &[]), version: 5, a_envoyer: false };
        c.listes.favoris.clear();
        assert!(s.changement(&mut c).is_none(), "pas branché");
        let t = s.recu(recues(m(&[], &["a", "b"], &[]), 5, None), &mut c).expect("envoi");
        assert_eq!(t["marques"]["favoris"], json!([]));
        assert_eq!(t["marques"]["version"], 5);
        // Changé ici et ailleurs : union.
        let mut s = SuiviMarques::default();
        let mut c = CopieMarques { listes: m(&[], &["a", "x"], &[]), version: 5, a_envoyer: true };
        let t = s.recu(recues(m(&[], &["a", "y"], &[]), 6, None), &mut c).expect("union");
        assert_eq!(t["marques"]["favoris"], json!(["a", "y", "x"]));
        // Refus du serveur : union avec les siennes.
        let id = id_de(&t);
        let r = NouvelleMarques::Recues {
            marques: m(&[], &["z"], &[]),
            version: 9,
            id: Some(id),
            refus: Some("non".into()),
            avec_archives: true,
            avec_privees: true,
            avec_moods: true,
            avec_emojis: true,
            avec_garder: true,
            avec_cacher: true,
            avec_presence: true,
        };
        let t = s.recu(r, &mut c).expect("renvoi");
        assert_eq!(t["marques"]["favoris"], json!(["z", "a", "y", "x"]));
        assert_eq!(t["marques"]["version"], 9);
        // Une coupure : plus rien en vol, on attend le rejeu.
        s.recu(NouvelleMarques::Coupure, &mut c);
        assert!(s.changement(&mut c).is_none());
    }

    /// Un appareil branché au serveur de test, avec sa copie des marques.
    struct Appareil {
        nom: &'static str,
        pont: PontDistant,
        suivi: SuiviMarques,
        copie: CopieMarques,
    }

    impl Appareil {
        fn brancher(nom: &'static str, config: &ConfigServeur, copie: CopieMarques) -> Self {
            Self { nom, pont: PontDistant::brancher(config, "google"), suivi: SuiviMarques::default(), copie }
        }

        /// Traite ce qui est arrivé, pendant `ms` millisecondes.
        fn pomper(&mut self, ms: u64) {
            let fin = Instant::now() + Duration::from_millis(ms);
            while Instant::now() < fin {
                let _ = self.pont.recevoir();
                for n in self.pont.recevoir_marques() {
                    println!("{} reçoit {n:?}", self.nom);
                    if let Some(t) = self.suivi.recu(n, &mut self.copie) {
                        println!("{} envoie {t}", self.nom);
                        assert!(self.pont.envoyer_marques(&t));
                    }
                }
                std::thread::sleep(Duration::from_millis(20));
            }
        }

        fn changer(&mut self, f: impl FnOnce(&mut Marques)) {
            f(&mut self.copie.listes);
            if let Some(t) = self.suivi.changement(&mut self.copie) {
                println!("{} envoie {t}", self.nom);
                assert!(self.pont.envoyer_marques(&t));
            }
        }
    }

    /// Jumelle un appareil au serveur de test : `--jumeler` lancé ici, avec
    /// le même JASETTE_DATA que le serveur.
    fn jumeler_essai(adresse: &str, nom: &str) -> ConfigServeur {
        let bin = std::env::var("JASETTE_ESSAI_SERVEUR").expect("JASETTE_ESSAI_SERVEUR");
        let sortie = std::process::Command::new(bin).arg("--jumeler").output().expect("--jumeler");
        let texte = String::from_utf8_lossy(&sortie.stdout);
        let code: String = texte
            .lines()
            .find(|l| l.starts_with("Code de jumelage"))
            .map(|l| l.chars().filter(char::is_ascii_digit).collect())
            .expect("code");
        let v = verifier_serveur(adresse, &code).expect("vérification");
        jumeler(adresse, &code, nom, &v.empreinte).expect("jumelage")
    }

    fn marques_du_serveur() -> Value {
        let d = std::env::var("JASETTE_DATA").expect("JASETTE_DATA");
        serde_json::from_str(&fs::read_to_string(Path::new(&d).join("serveur/marques.json")).unwrap()).unwrap()
    }

    /// Jumelle un « bureau » au serveur de test et écrit son reglages.json
    /// (mode client, avec des favoris) dans JASETTE_ESSAI_BUREAU, pour lancer
    /// la vraie interface dessus.
    #[test]
    #[ignore]
    fn essai_config_bureau() {
        let adresse = std::env::var("JASETTE_ESSAI_ADRESSE").expect("JASETTE_ESSAI_ADRESSE");
        let dossier = PathBuf::from(std::env::var("JASETTE_ESSAI_BUREAU").expect("JASETTE_ESSAI_BUREAU"));
        let cfg = jumeler_essai(&adresse, "bureau-ui");
        fs::create_dir_all(&dossier).unwrap();
        let r = json!({
            "mode_serveur": "client", "serveur": cfg, "notifications": false,
            "etoiles": ["f1", "m:f2", "i:f3"], "favoris": ["h1"], "epingles": [],
        });
        fs::write(dossier.join("reglages.json"), serde_json::to_string_pretty(&r).unwrap()).unwrap();
    }

    /// Contre un serveur de test (voir l'essai dans .cache/essai-marques) :
    /// JASETTE_DATA=… JASETTE_ESSAI_ADRESSE=127.0.0.1:PORT JASETTE_ESSAI_SERVEUR=…/jasette-serveur \
    /// cargo test essai_marques -- --ignored --nocapture
    #[test]
    #[ignore]
    fn essai_marques() {
        let adresse = std::env::var("JASETTE_ESSAI_ADRESSE").expect("JASETTE_ESSAI_ADRESSE");
        let cfg_a = jumeler_essai(&adresse, "bureau");
        let cfg_b = jumeler_essai(&adresse, "fold");
        let cfg_c = jumeler_essai(&adresse, "neuf");

        // 1. Le bureau a déjà des favoris ; le serveur n'a rien : ils montent.
        let favoris_bureau = ["a1", "a2", "a3", "m:a4", "i:a5"];
        let mut a = Appareil::brancher(
            "bureau",
            &cfg_a,
            CopieMarques { listes: m(&[], &favoris_bureau, &[]), version: 0, a_envoyer: false },
        );
        a.pomper(1500);
        assert_eq!(a.copie.version, 1, "{:?}", a.copie);
        assert_eq!(marques_du_serveur()["favoris"], json!(favoris_bureau));
        assert_eq!(marques_du_serveur()["version"], 1);

        // 2. Le Fold a les siens : union, l'ordre du serveur d'abord ; diffusée au bureau.
        let mut b = Appareil::brancher(
            "fold",
            &cfg_b,
            CopieMarques { listes: m(&["b1"], &["a2", "b2"], &["e1"]), version: 0, a_envoyer: false },
        );
        b.pomper(1500);
        a.pomper(500);
        let attendu = m(&["b1"], &["a1", "a2", "a3", "m:a4", "i:a5", "b2"], &["e1"]);
        assert_eq!(b.copie.listes, attendu);
        assert_eq!(a.copie.listes, attendu, "diffusion");
        assert_eq!(a.copie.version, 2);
        assert_eq!(b.copie.version, 2);

        // 3. Un appareil neuf (rien) : il prend tout, sans rien envoyer.
        let mut c = Appareil::brancher("neuf", &cfg_c, CopieMarques::default());
        c.pomper(1500);
        assert_eq!(c.copie.listes, attendu);
        assert_eq!(marques_du_serveur()["version"], 2);

        // 4. Un retrait et un réordonnancement, partout.
        a.changer(|l| l.favoris.retain(|x| x != "a3"));
        a.pomper(500);
        a.changer(|l| l.favoris.swap(0, 1));
        a.pomper(500);
        b.pomper(500);
        c.pomper(500);
        let attendu = m(&["b1"], &["a2", "a1", "m:a4", "i:a5", "b2"], &["e1"]);
        for x in [&a, &b, &c] {
            assert_eq!(x.copie.listes, attendu, "{}", x.nom);
            assert_eq!(x.copie.version, 4, "{}", x.nom);
        }

        // 5. Garde-fou : des listes vides d'une version périmée sont refusées.
        let brute = json!({"marques": {"head": [], "favoris": [], "epingles": [], "version": 1, "id": 999}});
        assert!(c.pont.envoyer_marques(&brute));
        std::thread::sleep(Duration::from_millis(500));
        let reponses = c.pont.recevoir_marques();
        println!("refus : {reponses:?}");
        assert!(reponses.iter().any(|n| matches!(n,
            NouvelleMarques::Recues { id: Some(999), refus: Some(_), version: 4, marques, .. } if *marques == attendu)));
        assert_eq!(marques_du_serveur()["version"], 4);
        assert_eq!(marques_du_serveur()["favoris"], json!(["a2", "a1", "m:a4", "i:a5", "b2"]));
        // Une version périmée non vide : union, rien ne se perd.
        let brute = json!({"marques": {"head": ["b1"], "favoris": ["z"], "epingles": ["e1"], "version": 1, "id": 1000}});
        assert!(c.pont.envoyer_marques(&brute));
        std::thread::sleep(Duration::from_millis(500));
        assert_eq!(marques_du_serveur()["favoris"], json!(["z", "a2", "a1", "m:a4", "i:a5", "b2"]));
        assert_eq!(marques_du_serveur()["version"], 5);
        a.pomper(300);
        b.pomper(300);
        c.pomper(300);
        a.changer(|l| l.favoris.retain(|x| x != "z"));
        a.pomper(500);
        b.pomper(300);
        c.pomper(300);

        // 6. Vider une liste exprès, à jour : accepté et diffusé.
        a.changer(|l| l.head.clear());
        a.pomper(500);
        b.pomper(500);
        c.pomper(500);
        assert_eq!(marques_du_serveur()["head"], json!([]));
        assert!(b.copie.listes.head.is_empty() && c.copie.listes.head.is_empty());
        let v = a.copie.version;
        assert_eq!(b.copie.version, v);

        // 7. Rejeu : le Fold se débranche, change hors ligne ; le bureau aussi.
        let copie_b = b.copie.clone();
        drop(b);
        std::thread::sleep(Duration::from_millis(300));
        a.changer(|l| l.favoris.push("y".into()));
        a.pomper(500);
        let mut copie_b = copie_b;
        copie_b.listes.favoris.push("x".into());
        copie_b.a_envoyer = true;
        let mut b = Appareil::brancher("fold", &cfg_b, copie_b);
        b.pomper(1500);
        a.pomper(500);
        c.pomper(500);
        for x in [&a, &b, &c] {
            assert!(x.copie.listes.favoris.contains(&"x".to_string()), "{} : x", x.nom);
            assert!(x.copie.listes.favoris.contains(&"y".to_string()), "{} : y", x.nom);
            assert_eq!(x.copie.listes, a.copie.listes, "{}", x.nom);
        }
        // Un appareil à jour qui se rebranche reçoit le rejeu et ne change rien.
        let copie_c = c.copie.clone();
        drop(c);
        std::thread::sleep(Duration::from_millis(300));
        let mut c = Appareil::brancher("neuf", &cfg_c, copie_c.clone());
        c.pomper(1500);
        assert_eq!(c.copie, copie_c, "rejeu");
        println!("final : {}", marques_du_serveur());

        #[cfg(unix)]
        {
            use std::os::unix::fs::PermissionsExt;
            let d = std::env::var("JASETTE_DATA").unwrap();
            let mode = fs::metadata(Path::new(&d).join("serveur/marques.json")).unwrap().permissions().mode();
            assert_eq!(mode & 0o777, 0o600);
        }
    }

    /// Le cache du serveur (PROTOCOLE.md, §5.8), contre un serveur de test dont
    /// le pont Google est un faux pont qui compte ses commandes dans
    /// FAUX_COMPTES (voir .cache/essai-cache) :
    /// JASETTE_DATA=… FAUX_COMPTES=… JASETTE_ESSAI_ADRESSE=127.0.0.1:PORT \
    /// JASETTE_ESSAI_SERVEUR=…/jasette-serveur cargo test essai_cache -- --ignored --nocapture
    #[test]
    #[ignore]
    fn essai_cache() {
        let adresse = std::env::var("JASETTE_ESSAI_ADRESSE").expect("JASETTE_ESSAI_ADRESSE");
        let comptes_f = std::env::var("FAUX_COMPTES").expect("FAUX_COMPTES");
        let comptes = || -> Value {
            fs::read_to_string(&comptes_f).ok().and_then(|t| serde_json::from_str(&t).ok()).unwrap_or(json!({}))
        };
        let n = |c: &Value, k: &str| c.get(k).and_then(Value::as_u64).unwrap_or(0);
        let cfg = jumeler_essai(&adresse, "cache");
        let google = PontDistant::brancher(&cfg, "google");

        // 1. Le préchargement : 30 récentes + les marquées c35 (Head) et c38
        //    (épinglée), une à la fois ; rien n'arrive aux clients, aucun `read`.
        let mut vus = Vec::new();
        let fin = Instant::now() + Duration::from_secs(120);
        while Instant::now() < fin && n(&comptes(), "messages") < 32 {
            vus.extend(google.recevoir());
            std::thread::sleep(Duration::from_millis(200));
        }
        std::thread::sleep(Duration::from_secs(3));
        vus.extend(google.recevoir());
        let c = comptes();
        println!("commandes reçues par le faux pont : {c}");
        assert_eq!(n(&c, "messages"), 32, "préchargement");
        for k in ["messages:c01", "messages:c30", "messages:c35", "messages:c38"] {
            assert_eq!(n(&c, k), 1, "{k}");
        }
        assert_eq!(n(&c, "messages:c31"), 0);
        assert_eq!(n(&c, "read"), 0, "aucun read");
        assert!(!vus.iter().any(|v| v["ev"] == "messages" || v["ev"] == "error"), "préchargement diffusé : {vus:?}");
        let listes: Vec<&Value> = vus.iter().filter(|v| v["ev"] == "conversations").collect();
        assert!(!listes.is_empty() && listes[0]["items"].as_array().unwrap().len() == 40, "liste");

        // 2. Réponse immédiate : la copie (cache:true, 40 messages), puis le pont (50).
        let attendre = |p: &PontDistant, conv: &str, nb: usize| -> Vec<Value> {
            let mut l = Vec::new();
            let fin = Instant::now() + Duration::from_secs(10);
            while Instant::now() < fin && l.len() < nb {
                l.extend(p.recevoir().into_iter().filter(|v| v["ev"] == "messages" && v["conv"] == conv));
                std::thread::sleep(Duration::from_millis(20));
            }
            l
        };
        let t0 = Instant::now();
        google.envoyer(&json!({"cmd": "messages", "conv": "c05", "count": 60}));
        let mut premiere = None;
        let mut l = Vec::new();
        let fin = Instant::now() + Duration::from_secs(10);
        while Instant::now() < fin && l.len() < 2 {
            for v in google.recevoir().into_iter().filter(|v| v["ev"] == "messages" && v["conv"] == "c05") {
                premiere.get_or_insert(t0.elapsed());
                l.push(v);
            }
            std::thread::sleep(Duration::from_millis(5));
        }
        println!("copie en {:?} ; réponse du pont en {:?}", premiere.unwrap(), t0.elapsed());
        assert_eq!(l.len(), 2);
        assert_eq!(l[0]["cache"], true);
        assert_eq!(l[0]["items"].as_array().unwrap().len(), 40);
        assert!(l[1].get("cache").is_none());
        assert_eq!(l[1]["items"].as_array().unwrap().len(), 50);
        // Fusion comme le bureau : sans doublon, par id.
        let msgs = |v: &Value| serde_json::from_value::<Vec<crate::Msg>>(v["items"].clone()).unwrap();
        let f = crate::cache::fusionner_copie(&[], msgs(&l[0]));
        let f = crate::cache::fusionner_page(&f, msgs(&l[1]));
        let ids: std::collections::HashSet<_> = f.iter().map(|m| m.id.clone()).collect();
        assert_eq!(f.len(), 50);
        assert_eq!(ids.len(), 50);

        // 3. Les événements tiennent le cache à jour : un message, une réaction.
        google.envoyer(&json!({"cmd": "faux_message", "conv": "c05", "text": "tout neuf"}));
        google.envoyer(&json!({"cmd": "faux_reaction", "conv": "c05"}));
        std::thread::sleep(Duration::from_millis(500));
        let _ = google.recevoir();
        google.envoyer(&json!({"cmd": "messages", "conv": "c05", "count": 60}));
        let l = attendre(&google, "c05", 2);
        assert_eq!(l[0]["cache"], true);
        let items = l[0]["items"].as_array().unwrap();
        assert_eq!(items.len(), 51, "nouveau message dans la copie");
        assert_eq!(items.last().unwrap()["text"], "tout neuf");
        assert!(items.iter().any(|m| m["id"] == "c05-m50" && m["reactions"][0]["emoji"] == "👍"), "réaction");

        // 4. Une conversation pas en cache : seulement la réponse du pont.
        google.envoyer(&json!({"cmd": "messages", "conv": "c39", "count": 60}));
        let l = attendre(&google, "c39", 1);
        assert_eq!(l.len(), 1);
        assert!(l[0].get("cache").is_none());

        // 5. Rejeu des listes (boîte, puis archives) à un client qui arrive.
        google.envoyer(&json!({"cmd": "conversations", "folder": "archive"}));
        std::thread::sleep(Duration::from_millis(500));
        let cfg2 = jumeler_essai(&adresse, "cache2");
        let autre = PontDistant::brancher(&cfg2, "google");
        let mut r = Vec::new();
        let fin = Instant::now() + Duration::from_secs(5);
        while Instant::now() < fin && r.iter().filter(|v: &&Value| v["ev"] == "conversations").count() < 2 {
            r.extend(autre.recevoir());
            std::thread::sleep(Duration::from_millis(50));
        }
        let evs: Vec<&str> = r.iter().filter_map(|v| v["ev"].as_str()).collect();
        println!("rejeu : {evs:?}");
        let i = evs.iter().position(|e| *e == "conversations").expect("liste rejouée");
        assert!(evs[..i].contains(&"state"), "l'état avant les listes");
        assert!(r[i].get("folder").is_none());
        assert_eq!(r[i + 1]["folder"], "archive");

        // 6. Toujours aucun read ; le fichier du cache est en 0600.
        std::thread::sleep(Duration::from_secs(6));
        assert_eq!(n(&comptes(), "read"), 0);
        #[cfg(unix)]
        {
            use std::os::unix::fs::PermissionsExt;
            let d = std::env::var("JASETTE_DATA").unwrap();
            let f = Path::new(&d).join("serveur/cache-google.json");
            assert_eq!(fs::metadata(&f).unwrap().permissions().mode() & 0o777, 0o600);
            let v: Value = serde_json::from_str(&fs::read_to_string(&f).unwrap()).unwrap();
            assert_eq!(v["convs"].as_object().unwrap().len(), 33);
            assert_eq!(v["convs"]["c05"]["items"].as_array().unwrap().len(), 51);
        }
    }
}
