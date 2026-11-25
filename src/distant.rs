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

use std::collections::HashMap;
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
        } else if let Some(r) = ["fichier", "depose"]
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
}

impl Drop for PontDistant {
    fn drop(&mut self) {
        self.cx.desabonner(&self.svc);
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
}
