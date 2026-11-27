//! Jasette (Windows) — la fenêtre de connexion par page web (WebView2).
//!
//! La même chose que les scripts Python de `connexion/` sous Linux, en un
//! seul programme : Jasette le lance avec les mêmes arguments,
//!
//!   jasette-connexion.exe jasette-connexion.py                       Google
//!   jasette-connexion.exe jasette-connexion-meta.py --messenger      (ou --facebook, --instagram)
//!   jasette-connexion.exe jasette-connexion-x.py --sans-code
//!   jasette-connexion.exe jasette-connexion-reddit.py --reddit
//!   jasette-connexion.exe jasette-connexion-linkedin.py --linkedin
//!
//! et lit sa réponse de la même façon : une fois connecté, une seule ligne
//! JSON sur la sortie standard, {"cookies": {…}, "platform": "…"}, puis la
//! fenêtre se ferme (code 0). Fermée avant : rien d'écrit, code 1.
//!
//! Les données de navigation restent dans
//! %LOCALAPPDATA%\jasette\<connexion…>\webview2 (le service se souvient de toi
//! la fois suivante). Rien d'autre n'est lu. Il faut le runtime WebView2
//! (inclus dans Windows 11 et dans les Windows 10 à jour).

#![cfg_attr(not(debug_assertions), windows_subsystem = "windows")]

use std::cell::{Cell, RefCell};
use std::collections::{BTreeMap, BTreeSet};
use std::io::Write;
use std::path::PathBuf;
use std::time::Duration;

use serde_json::{Value, json};
use tao::dpi::LogicalSize;
use tao::event::{Event, WindowEvent};
use tao::event_loop::{ControlFlow, EventLoopBuilder, EventLoopProxy};
use tao::window::WindowBuilder;
use wry::{PageLoadEvent, WebContext, WebView, WebViewBuilder};

/// Google refuse les navigateurs « intégrés » : on se présente comme Firefox
/// (comme jasette-connexion.py).
const AGENT_FIREFOX: &str = "Mozilla/5.0 (X11; Linux x86_64; rv:140.0) Gecko/20100101 Firefox/140.0";
/// Les ponts Meta et X parlent comme Chrome : la fenêtre se présente de la même façon.
const AGENT_CHROME: &str =
    "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/141.0.0.0 Safari/537.36";

#[derive(Clone, Copy, PartialEq)]
enum Genre {
    Google,
    Meta,
    X,
    Reddit,
    LinkedIn,
}

struct Service {
    genre: Genre,
    titre: &'static str,
    dossier: &'static str,
    debut: &'static str,
    agent: Option<&'static str>,
}

/// Le service d'après les arguments des scripts Python (nom du script, option).
fn service(args: &[String]) -> Option<Service> {
    let script = args.first()?.rsplit(['\\', '/']).next()?.to_string();
    let option = |o: &str| args.iter().skip(1).any(|a| a == o);
    let s = |genre, titre, dossier, debut, agent| Some(Service { genre, titre, dossier, debut, agent });
    match script.as_str() {
        "jasette-connexion.py" => s(
            Genre::Google,
            "Google",
            "connexion",
            "https://accounts.google.com/AccountChooser?continue=https://messages.google.com/web/config",
            Some(AGENT_FIREFOX),
        ),
        "jasette-connexion-meta.py" if option("--instagram") => {
            s(Genre::Meta, "Instagram", "connexion-meta", "https://www.instagram.com/direct/inbox/", Some(AGENT_CHROME))
        }
        "jasette-connexion-meta.py" if option("--facebook") => {
            s(Genre::Meta, "Messenger", "connexion-meta", "https://www.facebook.com/messages/", Some(AGENT_CHROME))
        }
        "jasette-connexion-meta.py" => {
            s(Genre::Meta, "Messenger", "connexion-meta", "https://www.messenger.com/?no_redirect=true", Some(AGENT_CHROME))
        }
        "jasette-connexion-x.py" => s(Genre::X, "X", "connexion-x", "https://x.com/i/flow/login", Some(AGENT_CHROME)),
        // Reddit et LinkedIn : l'agent propre du navigateur (un moteur qui se
        // dit autre chose se fait repérer), comme sous Linux.
        "jasette-connexion-reddit.py" => s(Genre::Reddit, "Reddit", "connexion-reddit", "https://www.reddit.com/login/", None),
        "jasette-connexion-linkedin.py" => {
            s(Genre::LinkedIn, "LinkedIn", "connexion-linkedin", "https://www.linkedin.com/login", None)
        }
        _ => None,
    }
}

fn commence(url: &str, prefixes: &[&str]) -> bool {
    prefixes.iter().any(|p| url.starts_with(p))
}

/// La page affichée est-elle celle où vérifier la session ? Rend la plateforme
/// (Meta : celle de la page, Messenger pouvant renvoyer vers Facebook).
fn page_a_verifier(genre: Genre, url: &str) -> Option<&'static str> {
    match genre {
        Genre::Google => url.starts_with("https://messages.google.com/").then_some(""),
        Genre::Meta => {
            if commence(url, &["https://www.instagram.com/", "https://instagram.com/"]) {
                Some("instagram")
            } else if commence(url, &["https://www.facebook.com/", "https://facebook.com/"]) {
                Some("facebook")
            } else if commence(url, &["https://www.messenger.com/", "https://messenger.com/"]) {
                Some("messenger")
            } else {
                None
            }
        }
        Genre::X => commence(
            url,
            &[
                "https://x.com/",
                "https://www.x.com/",
                "https://twitter.com/",
                "https://www.twitter.com/",
                "https://mobile.x.com/",
                "https://mobile.twitter.com/",
            ],
        )
        .then_some("x"),
        Genre::Reddit => commence(
            url,
            &[
                "https://www.reddit.com/",
                "https://reddit.com/",
                "https://old.reddit.com/",
                "https://new.reddit.com/",
                "https://chat.reddit.com/",
                "https://sh.reddit.com/",
            ],
        )
        .then_some("reddit"),
        Genre::LinkedIn => {
            let chemin = url.strip_prefix("https://www.linkedin.com").or_else(|| url.strip_prefix("https://linkedin.com"))?;
            let en_cours = ["/login", "/checkpoint", "/uas/", "/authwall", "/signup"];
            (!en_cours.iter().any(|p| chemin.starts_with(p))).then_some("linkedin")
        }
    }
}

type Voulus = (Vec<&'static str>, Option<Vec<&'static str>>, Vec<&'static str>);

/// Les adresses dont on lit les cookies, ceux qu'on garde (None : tous ceux
/// au nom raisonnable) et ceux sans lesquels on n'est pas connecté
/// (les mêmes listes que les scripts Python).
fn cookies_voulus(genre: Genre, plateforme: &str) -> Voulus {
    let meta = vec![
        "c_user", "xs", "datr", "fr", "sb", "wd", "dpr", "presence", "sessionid", "csrftoken", "ds_user_id", "mid", "ig_did",
        "rur", "shbid", "shbts",
    ];
    match (genre, plateforme) {
        (Genre::Google, _) => (
            vec!["https://messages.google.com/", "https://www.google.com/", "https://accounts.google.com/"],
            Some(vec!["SID", "HSID", "SSID", "APISID", "SAPISID", "OSID", "__Secure-1PSIDTS"]),
            vec!["SID", "HSID", "SSID", "APISID", "SAPISID", "OSID"],
        ),
        (Genre::Meta, "instagram") => (
            vec!["https://www.instagram.com/", "https://instagram.com/"],
            Some(meta),
            vec!["sessionid", "csrftoken", "ds_user_id"],
        ),
        (Genre::Meta, "facebook") => (
            vec!["https://www.facebook.com/", "https://facebook.com/", "https://www.messenger.com/"],
            Some(meta),
            vec!["c_user", "xs", "datr"],
        ),
        (Genre::Meta, _) => (
            vec!["https://www.messenger.com/", "https://messenger.com/", "https://www.facebook.com/"],
            Some(meta),
            vec!["c_user", "xs", "datr"],
        ),
        (Genre::X, _) => (
            vec!["https://x.com/", "https://twitter.com/", "https://api.x.com/"],
            Some(vec![
                "auth_token", "ct0", "twid", "kdt", "att", "guest_id", "guest_id_ads", "guest_id_marketing",
                "personalization_id", "lang", "dnt", "d_prefs", "__cf_bm", "gt",
            ]),
            vec!["auth_token", "ct0"],
        ),
        (Genre::Reddit, _) => (
            vec!["https://www.reddit.com/", "https://reddit.com/", "https://chat.reddit.com/"],
            Some(vec!["reddit_session", "csrf_token", "token_v2", "loid", "session_tracker", "edgebucket", "rdt", "csv"]),
            vec!["reddit_session"],
        ),
        (Genre::LinkedIn, _) => (vec!["https://www.linkedin.com/", "https://linkedin.com/"], None, vec!["li_at", "JSESSIONID"]),
    }
}

fn nom_valide(n: &str) -> bool {
    !n.is_empty() && n.len() <= 64 && n.bytes().all(|b| b.is_ascii_alphanumeric() || b"_.-".contains(&b))
}

/// %LOCALAPPDATA%\jasette\<dossier>\webview2 (XDG_DATA_HOME, posé par Jasette, d'abord).
fn dossier_donnees(dossier: &str) -> PathBuf {
    std::env::var_os("XDG_DATA_HOME")
        .filter(|v| !v.is_empty())
        .or_else(|| std::env::var_os("LOCALAPPDATA"))
        .map(PathBuf::from)
        .unwrap_or_else(|| PathBuf::from("."))
        .join("jasette")
        .join(dossier)
        .join("webview2")
}

fn message(texte: &str) {
    use windows_sys::Win32::UI::WindowsAndMessaging::{MB_ICONERROR, MB_OK, MessageBoxW};
    let t: Vec<u16> = texte.encode_utf16().chain([0]).collect();
    let titre: Vec<u16> = "Jasette".encode_utf16().chain([0]).collect();
    // SAFETY : chaînes terminées par un zéro, vivantes pendant l'appel.
    unsafe {
        MessageBoxW(std::ptr::null_mut(), t.as_ptr(), titre.as_ptr(), MB_OK | MB_ICONERROR);
    }
}

#[derive(Debug)]
enum Signal {
    Minuterie,
    Chargee,
    /// Reddit : le HTML de la page du chat (pour la version du client web).
    HtmlReddit(String),
    /// LinkedIn : l'agent du navigateur (le pont le reprend).
    Agent(String),
}

/// Reddit : le HTML de la page affichée (tronqué).
fn lire_html(vue: &WebView, proxy: &EventLoopProxy<Signal>) {
    let p = proxy.clone();
    let r = vue.evaluate_script_with_callback("document.documentElement.outerHTML.slice(0, 400000)", move |r| {
        let _ = p.send_event(Signal::HtmlReddit(serde_json::from_str::<String>(&r).unwrap_or_default()));
    });
    if r.is_err() {
        let _ = proxy.send_event(Signal::HtmlReddit(String::new()));
    }
}

/// L'état d'une connexion, entre deux vérifications.
struct Etat {
    genre: Genre,
    /// Reddit : la page du chat (qui pose csrf_token et token_v2 à jour), ouverte puis lue.
    chat_ouvert: Cell<bool>,
    chat_pret: Cell<bool>,
    version_reddit: RefCell<Option<String>>,
    agent: RefCell<Option<String>>,
}

impl Etat {
    /// Vérifie la session ; Some(ligne JSON) une fois connecté.
    fn verifier(&self, vue: &WebView, proxy: &EventLoopProxy<Signal>) -> Option<String> {
        let url = vue.url().ok()?;
        let plateforme = page_a_verifier(self.genre, &url)?;
        let (domaines, voulus, requis) = cookies_voulus(self.genre, plateforme);
        let mut trouves: BTreeMap<String, String> = BTreeMap::new();
        for d in domaines {
            match vue.cookies_for_url(d) {
                Ok(liste) => {
                    for c in liste {
                        let (n, v) = (c.name().to_string(), c.value().to_string());
                        let garde = match &voulus {
                            Some(l) => l.contains(&n.as_str()),
                            None => nom_valide(&n),
                        };
                        if garde && !v.is_empty() {
                            trouves.entry(n).or_insert(v);
                        }
                    }
                }
                Err(e) => eprintln!("jasette-connexion : cookies de {d} : {e}"),
            }
        }
        let presents: BTreeSet<&str> = trouves.keys().map(String::as_str).collect();
        if !requis.iter().all(|r| presents.contains(r)) {
            // Pas encore connecté : la minuterie revérifie.
            return None;
        }
        match self.genre {
            Genre::Reddit => {
                if !self.chat_ouvert.get() {
                    self.chat_ouvert.set(true);
                    if url.starts_with("https://www.reddit.com/chat/") {
                        lire_html(vue, proxy);
                    } else {
                        let _ = vue.load_url("https://www.reddit.com/chat/");
                    }
                    return None;
                }
                if !self.chat_pret.get() || (!presents.contains("csrf_token") && !presents.contains("token_v2")) {
                    return None;
                }
                if let Some(v) = self.version_reddit.borrow().clone() {
                    trouves.insert("jasette_client_version".into(), v);
                }
            }
            Genre::LinkedIn => {
                let Some(a) = self.agent.borrow().clone() else {
                    let p = proxy.clone();
                    let _ = vue.evaluate_script_with_callback("navigator.userAgent", move |r| {
                        let _ = p.send_event(Signal::Agent(serde_json::from_str::<String>(&r).unwrap_or_default()));
                    });
                    return None;
                };
                trouves.insert("jasette_user_agent".into(), a);
            }
            _ => {}
        }
        let mut sortie = json!({ "cookies": trouves });
        if self.genre != Genre::Google {
            sortie["platform"] = Value::String(plateforme.to_string());
        }
        Some(sortie.to_string())
    }
}

fn main() {
    let args: Vec<String> = std::env::args().skip(1).collect();
    let Some(svc) = service(&args) else {
        eprintln!("jasette-connexion : service inconnu ({args:?})");
        std::process::exit(2);
    };
    let donnees = dossier_donnees(svc.dossier);
    let _ = std::fs::create_dir_all(&donnees);

    let boucle = EventLoopBuilder::<Signal>::with_user_event().build();
    let fenetre = match WindowBuilder::new()
        .with_title(format!("Connexion à {} — Jasette", svc.titre))
        .with_inner_size(LogicalSize::new(560.0, 760.0))
        .build(&boucle)
    {
        Ok(f) => f,
        Err(e) => {
            eprintln!("jasette-connexion : fenêtre : {e}");
            std::process::exit(1);
        }
    };

    let proxy = boucle.create_proxy();
    let p_charge = proxy.clone();
    let mut contexte = WebContext::new(Some(donnees));
    let mut constructeur = WebViewBuilder::new_with_web_context(&mut contexte)
        .with_url(svc.debut)
        .with_devtools(false)
        .with_on_page_load_handler(move |e, _url| {
            if matches!(e, PageLoadEvent::Finished) {
                let _ = p_charge.send_event(Signal::Chargee);
            }
        });
    if let Some(a) = svc.agent {
        constructeur = constructeur.with_user_agent(a);
    }
    let vue = match constructeur.build(&fenetre) {
        Ok(v) => v,
        Err(e) => {
            eprintln!("jasette-connexion : WebView2 : {e}");
            message(
                "La fenêtre de connexion a besoin du runtime Microsoft Edge WebView2.\n\n\
                 Installez-le depuis https://go.microsoft.com/fwlink/p/?LinkId=2124703 puis réessayez.",
            );
            std::process::exit(1);
        }
    };

    // Les cookies arrivent parfois après le dernier changement d'adresse : on
    // revérifie toutes les 2 secondes (comme les scripts Python).
    let p_minuterie = proxy.clone();
    std::thread::spawn(move || {
        loop {
            std::thread::sleep(Duration::from_secs(2));
            if p_minuterie.send_event(Signal::Minuterie).is_err() {
                return;
            }
        }
    });

    let etat = Etat {
        genre: svc.genre,
        chat_ouvert: Cell::new(false),
        chat_pret: Cell::new(false),
        version_reddit: RefCell::new(None),
        agent: RefCell::new(None),
    };
    let motif_version =
        regex_lite::Regex::new(r#""?(?:client[-_]?version|clientVersion)"?\s*[:=]\s*"(\d{4}-\d{2}-\d{2}T[^"]{1,40})""#)
            .expect("motif");

    boucle.run(move |evenement, _, controle| {
        *controle = ControlFlow::Wait;
        let verifier = || {
            if let Some(ligne) = etat.verifier(&vue, &proxy) {
                let mut out = std::io::stdout().lock();
                let ok = writeln!(out, "{ligne}").and_then(|_| out.flush()).is_ok();
                std::process::exit(if ok { 0 } else { 1 });
            }
        };
        match evenement {
            Event::UserEvent(Signal::Minuterie) => verifier(),
            Event::UserEvent(Signal::Chargee) => {
                if etat.genre == Genre::Reddit && etat.chat_ouvert.get() && !etat.chat_pret.get() {
                    lire_html(&vue, &proxy);
                }
                verifier();
            }
            Event::UserEvent(Signal::HtmlReddit(html)) => {
                etat.chat_pret.set(true);
                if let Some(m) = motif_version.captures(&html) {
                    *etat.version_reddit.borrow_mut() = Some(m[1].to_string());
                }
                verifier();
            }
            Event::UserEvent(Signal::Agent(a)) => {
                if a.starts_with("Mozilla/") {
                    *etat.agent.borrow_mut() = Some(a.chars().take(400).collect());
                    verifier();
                }
            }
            Event::WindowEvent { event: WindowEvent::CloseRequested, .. } => {
                eprintln!("jasette-connexion : fenêtre fermée sans connexion");
                std::process::exit(1);
            }
            _ => {}
        }
    });
}
