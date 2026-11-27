// Jasette — les fenêtres de connexion sur macOS (WKWebView).
//
// L'équivalent des scripts Python GTK/WebKit de connexion/ (Linux), en un seul
// petit programme : jasette-connexion, rangé à côté de jasette dans
// Jasette.app/Contents/MacOS. Jasette (src/macos.rs) le lance avec, en premier
// argument, le nom du script Linux qu'il remplace, puis l'option de ce script :
//
//   jasette-connexion jasette-connexion.py                       Google Messages
//   jasette-connexion jasette-connexion-meta.py --messenger      (--facebook, --instagram)
//   jasette-connexion jasette-connexion-x.py --sans-code         X
//   jasette-connexion jasette-connexion-reddit.py --reddit       Reddit
//   jasette-connexion jasette-connexion-linkedin.py --linkedin   LinkedIn
//
// Le contrat est le même que celui des scripts : une fois connecté, une seule
// ligne JSON sur la sortie standard,
//   {"cookies": {...}, "platform": "..."}       (sans « platform » pour Google)
// puis code de sortie 0 ; fenêtre fermée avant : rien sur la sortie standard,
// code 1. Seuls les témoins (cookies) nommés pour le service sont lus.
//
// Chaque service garde sa propre session de navigation (WKWebsiteDataStore
// à identifiant fixe, macOS 14 et plus ; le magasin par défaut de l'app
// avant), pour que le site se souvienne de toi la fois suivante.
//
// Compilation (macos/construire.sh) :
//   swiftc -O -target arm64-apple-macos12 main.swift -o jasette-connexion

import Cocoa
import WebKit

// MARK: - Les services

enum Genre {
    case google, meta, x, reddit, linkedin
}

struct Profil {
    let genre: Genre
    let titre: String
    let debut: URL
    /// Les hôtes dont on lit les témoins, dans l'ordre (le premier trouvé gagne).
    let hotes: [String]
    let requis: Set<String>
    /// nil : tous les noms valides (LinkedIn).
    let voulus: Set<String>?
    /// « platform » dans le JSON (nil pour Google).
    let plateforme: String?
    /// L'agent du navigateur (nil : celui de WebKit).
    let agent: String?
    /// L'identifiant fixe du magasin de navigation de ce service.
    let magasin: String
}

let AGENT_CHROME = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/141.0.0.0 Safari/537.36"
let AGENT_FIREFOX = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10.15; rv:140.0) Gecko/20100101 Firefox/140.0"

func profil(arguments: [String]) -> Profil? {
    let script = arguments.first.map { ($0 as NSString).lastPathComponent } ?? ""
    let options = Set(arguments.dropFirst())
    let env = ProcessInfo.processInfo.environment
    switch script {
    case "jasette-connexion.py", "--google":
        return Profil(
            genre: .google, titre: "Google",
            debut: URL(string: "https://accounts.google.com/AccountChooser?continue=https://messages.google.com/web/config")!,
            hotes: ["messages.google.com", "www.google.com", "accounts.google.com"],
            requis: ["SID", "HSID", "SSID", "APISID", "SAPISID", "OSID"],
            voulus: ["SID", "HSID", "SSID", "APISID", "SAPISID", "OSID", "__Secure-1PSIDTS"],
            plateforme: nil, agent: AGENT_FIREFOX,
            magasin: "4A415345-5454-4500-0000-000000000001")
    case "jasette-connexion-meta.py":
        let voulus: Set<String> = ["c_user", "xs", "datr", "fr", "sb", "wd", "dpr", "presence",
                                   "sessionid", "csrftoken", "ds_user_id", "mid", "ig_did", "rur", "shbid", "shbts"]
        if options.contains("--instagram") {
            return Profil(
                genre: .meta, titre: "Instagram",
                debut: URL(string: "https://www.instagram.com/direct/inbox/")!,
                hotes: ["www.instagram.com", "instagram.com"],
                requis: ["sessionid", "csrftoken", "ds_user_id"], voulus: voulus,
                plateforme: "instagram", agent: AGENT_CHROME,
                magasin: "4A415345-5454-4500-0000-000000000002")
        }
        let facebook = options.contains("--facebook")
        return Profil(
            genre: .meta, titre: "Messenger",
            debut: URL(string: facebook ? "https://www.facebook.com/messages/" : "https://www.messenger.com/?no_redirect=true")!,
            hotes: facebook ? ["www.facebook.com", "facebook.com", "www.messenger.com"]
                            : ["www.messenger.com", "messenger.com", "www.facebook.com"],
            requis: ["c_user", "xs", "datr"], voulus: voulus,
            plateforme: facebook ? "facebook" : "messenger", agent: AGENT_CHROME,
            magasin: "4A415345-5454-4500-0000-000000000003")
    case "jasette-connexion-x.py":
        let requis: Set<String> = ["auth_token", "ct0"]
        return Profil(
            genre: .x, titre: "X",
            debut: URL(string: "https://x.com/i/flow/login")!,
            hotes: ["x.com", "twitter.com", "api.x.com"],
            requis: requis,
            voulus: requis.union(["twid", "kdt", "att", "guest_id", "guest_id_ads", "guest_id_marketing",
                                  "personalization_id", "lang", "dnt", "d_prefs", "__cf_bm", "gt"]),
            plateforme: "x", agent: env["JASETTE_X_AGENT"],
            magasin: "4A415345-5454-4500-0000-000000000004")
    case "jasette-connexion-reddit.py":
        let requis: Set<String> = ["reddit_session"]
        return Profil(
            genre: .reddit, titre: "Reddit",
            debut: URL(string: "https://www.reddit.com/login/")!,
            hotes: ["www.reddit.com", "reddit.com", "chat.reddit.com"],
            requis: requis,
            voulus: requis.union(["csrf_token", "token_v2", "loid", "session_tracker", "edgebucket", "rdt", "csv"]),
            plateforme: "reddit", agent: env["JASETTE_REDDIT_AGENT"],
            magasin: "4A415345-5454-4500-0000-000000000005")
    case "jasette-connexion-linkedin.py":
        return Profil(
            genre: .linkedin, titre: "LinkedIn",
            debut: URL(string: "https://www.linkedin.com/login")!,
            hotes: ["www.linkedin.com", "linkedin.com"],
            requis: ["li_at", "JSESSIONID"], voulus: nil,
            plateforme: "linkedin", agent: env["JASETTE_LINKEDIN_AGENT"],
            magasin: "4A415345-5454-4500-0000-000000000006")
    default:
        return nil
    }
}

func journal(_ texte: String) {
    FileHandle.standardError.write(("jasette-connexion : " + texte + "\n").data(using: .utf8)!)
}

/// Le témoin s'applique-t-il à cet hôte ? (domaine exact, ou parent avec un point)
func applique(_ c: HTTPCookie, a hote: String) -> Bool {
    var d = c.domain.lowercased()
    if d.hasPrefix(".") { d.removeFirst() }
    return hote == d || hote.hasSuffix("." + d)
}

func nomValide(_ n: String) -> Bool {
    !n.isEmpty && n.count <= 64 && n.allSatisfy { $0.isASCII && ($0.isLetter || $0.isNumber || "_.-".contains($0)) }
}

func commencePar(_ uri: String, _ prefixes: [String]) -> Bool {
    prefixes.contains { uri.hasPrefix($0) }
}

// MARK: - La fenêtre

final class Connexion: NSObject, NSWindowDelegate, WKNavigationDelegate, WKUIDelegate {
    let p: Profil
    let fenetre: NSWindow
    let vue: WKWebView
    var fini = false
    var lecture = false
    var minuterie: Timer?
    var observation: NSKeyValueObservation?
    var surgissantes: [NSWindow] = []
    // Reddit
    var chatOuvert = false
    var chatPret = false
    var versionReddit: String?
    // LinkedIn
    var agentLu: String?

    init(_ p: Profil) {
        self.p = p
        let config = WKWebViewConfiguration()
        if #available(macOS 14.0, *), let id = UUID(uuidString: p.magasin) {
            config.websiteDataStore = WKWebsiteDataStore(forIdentifier: id)
        } else {
            config.websiteDataStore = .default()
        }
        config.preferences.javaScriptCanOpenWindowsAutomatically = true
        vue = WKWebView(frame: NSRect(x: 0, y: 0, width: 560, height: 760), configuration: config)
        if let a = p.agent { vue.customUserAgent = a }
        fenetre = NSWindow(contentRect: NSRect(x: 0, y: 0, width: 560, height: 760),
                           styleMask: [.titled, .closable, .miniaturizable, .resizable],
                           backing: .buffered, defer: false)
        super.init()
        fenetre.title = "Connexion à \(p.titre) — Jasette"
        fenetre.contentView = vue
        fenetre.delegate = self
        fenetre.isReleasedWhenClosed = false
        fenetre.center()
        vue.navigationDelegate = self
        vue.uiDelegate = self
        // Les sites d'une seule page changent d'adresse sans nouveau chargement.
        observation = vue.observe(\.url, options: [.new]) { [weak self] _, _ in
            DispatchQueue.main.async { self?.verifier() }
        }
        // Les témoins arrivent parfois après le dernier changement d'adresse.
        minuterie = Timer.scheduledTimer(withTimeInterval: 2, repeats: true) { [weak self] _ in self?.verifier() }
        vue.load(URLRequest(url: p.debut))
        fenetre.makeKeyAndOrderFront(nil)
        NSApp.activate(ignoringOtherApps: true)
    }

    // ---- Fermeture ----

    func windowWillClose(_ notification: Notification) {
        if !fini {
            journal("fenêtre fermée sans connexion")
            exit(1)
        }
    }

    // ---- Chargements ----

    func webView(_ webView: WKWebView, didFinish navigation: WKNavigation!) {
        if webView !== vue { return }
        switch p.genre {
        case .reddit where chatOuvert: lireVersionReddit()
        case .linkedin where agentLu == nil: lireAgent()
        default: break
        }
        verifier()
    }

    /// « Continuer avec Google / Apple » : une fenêtre surgissante, même session.
    func webView(_ webView: WKWebView, createWebViewWith configuration: WKWebViewConfiguration,
                 for navigationAction: WKNavigationAction, windowFeatures: WKWindowFeatures) -> WKWebView? {
        let nouvelle = WKWebView(frame: NSRect(x: 0, y: 0, width: 500, height: 680), configuration: configuration)
        // Google refuse les navigateurs « intégrés » qui se disent Chrome.
        nouvelle.customUserAgent = AGENT_FIREFOX
        nouvelle.uiDelegate = self
        let fen = NSWindow(contentRect: NSRect(x: 0, y: 0, width: 500, height: 680),
                           styleMask: [.titled, .closable, .resizable], backing: .buffered, defer: false)
        fen.title = "Connexion — Jasette"
        fen.isReleasedWhenClosed = false
        fen.contentView = nouvelle
        fen.center()
        fen.makeKeyAndOrderFront(nil)
        surgissantes.append(fen)
        return nouvelle
    }

    func webViewDidClose(_ webView: WKWebView) {
        if let fen = surgissantes.first(where: { $0.contentView === webView }) {
            fen.close()
            surgissantes.removeAll { $0 === fen }
        }
    }

    // ---- La connexion ----

    func adresse() -> String { vue.url?.absoluteString ?? "" }

    /// La page affichée est-elle celle d'un compte connecté (ou en voie de l'être) ?
    func surLeSite() -> Bool {
        let u = adresse()
        switch p.genre {
        case .google:
            return u.hasPrefix("https://messages.google.com/")
        case .meta:
            return commencePar(u, ["https://www.instagram.com/", "https://instagram.com/",
                                   "https://www.facebook.com/", "https://facebook.com/",
                                   "https://www.messenger.com/", "https://messenger.com/"])
        case .x:
            return commencePar(u, ["https://x.com/", "https://www.x.com/", "https://twitter.com/",
                                   "https://www.twitter.com/", "https://mobile.x.com/", "https://mobile.twitter.com/"])
        case .reddit:
            return commencePar(u, ["https://www.reddit.com/", "https://reddit.com/", "https://old.reddit.com/",
                                   "https://new.reddit.com/", "https://chat.reddit.com/", "https://sh.reddit.com/"])
        case .linkedin:
            return connexionLinkedinFinie(u)
        }
    }

    func connexionLinkedinFinie(_ u: String) -> Bool {
        guard commencePar(u, ["https://www.linkedin.com/", "https://linkedin.com/"]) else { return false }
        let chemin = u.replacingOccurrences(of: "^https://(www\\.)?linkedin\\.com", with: "", options: .regularExpression)
        return !["/login", "/checkpoint", "/uas/", "/authwall", "/signup"].contains { chemin.hasPrefix($0) }
    }

    func verifier() {
        if fini || lecture || !surLeSite() { return }
        lecture = true
        vue.configuration.websiteDataStore.httpCookieStore.getAllCookies { [weak self] tous in
            guard let self else { return }
            self.lecture = false
            var trouves: [String: String] = [:]
            for hote in self.p.hotes {
                for c in tous where applique(c, a: hote) && !c.value.isEmpty && trouves[c.name] == nil {
                    let voulu = self.p.voulus.map { $0.contains(c.name) } ?? nomValide(c.name)
                    if voulu { trouves[c.name] = c.value }
                }
            }
            self.connecte(trouves)
        }
    }

    func connecte(_ trouvesRecus: [String: String]) {
        var trouves = trouvesRecus
        if fini || !p.requis.isSubset(of: Set(trouves.keys)) { return }
        switch p.genre {
        case .reddit:
            // Connecté : la page du chat pose csrf_token et token_v2 à jour.
            if !chatOuvert {
                chatOuvert = true
                let chat = "https://www.reddit.com/chat/"
                if adresse().hasPrefix(chat) { lireVersionReddit() } else { vue.load(URLRequest(url: URL(string: chat)!)) }
                return
            }
            if !chatPret || (trouves["csrf_token"] == nil && trouves["token_v2"] == nil) { return }
            if let v = versionReddit { trouves["jasette_client_version"] = v }
        case .linkedin:
            guard let a = agentLu else { lireAgent(); return }
            trouves["jasette_user_agent"] = a
        default:
            break
        }
        terminer(trouves)
    }

    func terminer(_ trouves: [String: String]) {
        fini = true
        minuterie?.invalidate()
        var sortie: [String: Any] = ["cookies": trouves]
        if let pl = p.plateforme { sortie["platform"] = pl }
        if let data = try? JSONSerialization.data(withJSONObject: sortie, options: []),
           let ligne = String(data: data, encoding: .utf8) {
            print(ligne)
            fflush(stdout)
        }
        exit(0)
    }

    func lireVersionReddit() {
        vue.evaluateJavaScript("document.documentElement.outerHTML.slice(0, 400000)") { [weak self] res, _ in
            guard let self else { return }
            self.chatPret = true
            if let texte = res as? String,
               let re = try? NSRegularExpression(pattern: "\"?(?:client[-_]?version|clientVersion)\"?\\s*[:=]\\s*\"(\\d{4}-\\d{2}-\\d{2}T[^\"]{1,40})\""),
               let m = re.firstMatch(in: texte, range: NSRange(texte.startIndex..., in: texte)),
               let r = Range(m.range(at: 1), in: texte) {
                self.versionReddit = String(texte[r])
            }
            self.verifier()
        }
    }

    func lireAgent() {
        vue.evaluateJavaScript("navigator.userAgent") { [weak self] res, _ in
            guard let self else { return }
            if let a = res as? String, a.hasPrefix("Mozilla/") {
                self.agentLu = String(a.prefix(400))
                self.verifier()
            }
        }
    }
}

// MARK: - Le menu (copier, coller, quitter)

func menuPrincipal() -> NSMenu {
    let barre = NSMenu()
    let app = NSMenuItem()
    let menuApp = NSMenu()
    menuApp.addItem(withTitle: "Quitter", action: #selector(NSWindow.performClose(_:)), keyEquivalent: "q")
    app.submenu = menuApp
    barre.addItem(app)
    let edition = NSMenuItem()
    let menuEdition = NSMenu(title: "Édition")
    menuEdition.addItem(withTitle: "Annuler", action: Selector(("undo:")), keyEquivalent: "z")
    menuEdition.addItem(withTitle: "Rétablir", action: Selector(("redo:")), keyEquivalent: "Z")
    menuEdition.addItem(.separator())
    menuEdition.addItem(withTitle: "Couper", action: #selector(NSText.cut(_:)), keyEquivalent: "x")
    menuEdition.addItem(withTitle: "Copier", action: #selector(NSText.copy(_:)), keyEquivalent: "c")
    menuEdition.addItem(withTitle: "Coller", action: #selector(NSText.paste(_:)), keyEquivalent: "v")
    menuEdition.addItem(withTitle: "Tout sélectionner", action: #selector(NSText.selectAll(_:)), keyEquivalent: "a")
    edition.submenu = menuEdition
    barre.addItem(edition)
    return barre
}

// MARK: - Le départ

guard let p = profil(arguments: Array(CommandLine.arguments.dropFirst())) else {
    journal("service inconnu : \(CommandLine.arguments.dropFirst().joined(separator: " "))")
    exit(2)
}
let app = NSApplication.shared
app.setActivationPolicy(.regular)
app.mainMenu = menuPrincipal()
let connexion = Connexion(p)
app.run()
