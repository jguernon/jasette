// Jasette iOS — la connexion partagée à un serveur jumelé (PROTOCOLE.md §4 à §6).
//
// Une seule connexion par serveur, pour tous les services. Elle se branche et
// se rebranche toute seule (1, 2, 4… 30 s ; 60 s après un refus de jeton ou
// d'empreinte), répond aux signes de vie, coupe après 90 s de silence, et
// télécharge les fichiers du serveur (`get`) dans un cache local.
//
// L'app ne se branche qu'au premier plan : iOS ne la laisse pas vivre en
// arrière-plan (pas de notifications push dans cette version).

import CryptoKit
import Foundation

final class ConnexionServeur: @unchecked Sendable {
    enum Nouvelle {
        /// En cours de connexion (la première fois) ou de reconnexion.
        case cherche(premiere: Bool)
        /// Branché : les services offerts par le serveur.
        case branche([String])
        /// Une erreur à montrer (refus de jeton, certificat changé, serveur injoignable).
        case erreur(String)
        case evenement(svc: String, ev: [String: Any])
        case marques([String: Any])
        case coupe
    }

    let config: ConfigServeur
    /// Le cache local des fichiers de ce serveur.
    let cache: URL
    private let surNouvelle: @MainActor (Nouvelle) -> Void

    private let verrou = NSLock()
    private var lien: Lien?
    private var enAttente: [String] = []
    private var attentes: [Int: AsyncStream<[String: Any]>.Continuation] = [:]
    private var prochain = 1
    private var tache: Task<Void, Never>?
    private var dernierRecu = Date()
    private var vus: [String: Date] = [:]
    private var enCours: [String: Task<URL?, Never>] = [:]
    private let limiteur = Limiteur(3)

    init(config: ConfigServeur, surNouvelle: @escaping @MainActor (Nouvelle) -> Void) {
        self.config = config
        self.surNouvelle = surNouvelle
        let hote = config.adresse.map { $0.isLetter || $0.isNumber || $0 == "." || $0 == "-" ? $0 : "_" }
        let base = FileManager.default.urls(for: .cachesDirectory, in: .userDomainMask)[0]
        cache = base.appendingPathComponent("distant/\(String(hote))", isDirectory: true)
        try? FileManager.default.createDirectory(at: cache, withIntermediateDirectories: true)
    }

    private func avecVerrou<T>(_ f: () -> T) -> T {
        verrou.lock()
        defer { verrou.unlock() }
        return f()
    }

    var branche: Bool { avecVerrou { lien != nil } }

    private func remettre(_ n: Nouvelle) {
        DispatchQueue.main.async { MainActor.assumeIsolated { self.surNouvelle(n) } }
    }

    // MARK: - Marche et arrêt

    func demarrer() {
        avecVerrou {
            guard tache == nil else { return }
            tache = Task.detached { [weak self] in await self?.boucle() }
        }
    }

    func arreter() {
        let (t, l) = avecVerrou { () -> (Task<Void, Never>?, Lien?) in
            let r = (tache, lien)
            tache = nil
            return r
        }
        t?.cancel()
        l?.fermer()
    }

    // MARK: - Envoi

    /// Une commande pour le pont [svc] du serveur (gardée pendant une coupure).
    func envoyer(svc: String, cmd: [String: Any]) {
        if (cmd["cmd"] as? String) == "quit" { return }
        envoyerLigne(ecrireJSON(["svc": svc, "cmd": cmd]))
    }

    private func envoyerLigne(_ ligne: String) {
        let l: Lien? = avecVerrou {
            if let l = lien { return l }
            if enAttente.count >= 500 { enAttente.removeFirst() }
            enAttente.append(ligne)
            return nil
        }
        l?.ecrire(ligne)
    }

    // MARK: - La boucle

    private func connecter() async throws -> (Lien, [String]) {
        let l = try await Lien.ouvrir(adresse: config.adresse, attendue: config.empreinte)
        do {
            l.ecrire(["auth": ["jeton": config.jeton]])
            let annuler = l.minuterie(20)
            defer { annuler() }
            let r = try await l.lire()
            if let e = erreurDe(r) { throw ErreurLien(texte: t(e), refus: true) }
            guard r.chaine("auth") == "ok" else { throw ErreurLien(texte: t("réponse inattendue du serveur")) }
            return (l, r.chaines("services"))
        } catch let e as ErreurLien {
            l.fermer()
            throw e
        } catch {
            l.fermer()
            throw ErreurLien(texte: "\(t("serveur injoignable")) : \(error.localizedDescription)")
        }
    }

    private func boucle() async {
        var attente: Double = 1
        var dejaConnecte = false
        var erreurDite = ""
        while !Task.isCancelled {
            remettre(.cherche(premiere: !dejaConnecte))
            let l: Lien
            let services: [String]
            do {
                (l, services) = try await connecter()
            } catch {
                let e = error as? ErreurLien ?? ErreurLien(texte: error.localizedDescription)
                if e.texte != erreurDite {
                    remettre(.erreur(e.texte))
                    erreurDite = e.texte
                }
                if e.empreinte || e.refus { attente = 60 }
                if !(await dormir(attente)) { break }
                if attente < 30 { attente = min(attente * 2, 30) }
                continue
            }
            if Task.isCancelled {
                l.fermer()
                break
            }
            attente = 1
            erreurDite = ""
            dejaConnecte = true
            let gardees = avecVerrou { () -> [String] in
                lien = l
                dernierRecu = Date()
                let g = enAttente
                enAttente.removeAll()
                return g
            }
            remettre(.branche(services))
            gardees.forEach { l.ecrire($0) }
            // Aucune ligne depuis 90 s : connexion perdue (§5.3).
            let chien = Task.detached { [weak self] in
                while !Task.isCancelled {
                    try? await Task.sleep(nanoseconds: 5_000_000_000)
                    guard let self else { return }
                    if Date().timeIntervalSince(self.avecVerrou({ self.dernierRecu })) > 90 {
                        l.fermer()
                        return
                    }
                }
            }
            do {
                while let ligne = try await l.ligne() {
                    avecVerrou { dernierRecu = Date() }
                    if !ligne.isEmpty { trame(ligne, l) }
                }
            } catch {}
            chien.cancel()
            l.fermer()
            let perdues = avecVerrou { () -> [AsyncStream<[String: Any]>.Continuation] in
                lien = nil
                let a = Array(attentes.values)
                attentes.removeAll()
                return a
            }
            // Les requêtes de fichiers en cours échouent tout de suite.
            for c in perdues {
                c.yield(["erreur": t("connexion perdue"), "fin": true])
                c.finish()
            }
            remettre(.coupe)
            if Task.isCancelled { break }
            if !(await dormir(attente)) { break }
            attente = min(attente * 2, 30)
        }
    }

    /// Faux si la tâche a été annulée pendant l'attente.
    private func dormir(_ s: Double) async -> Bool {
        do {
            try await Task.sleep(nanoseconds: UInt64(s * 1_000_000_000))
            return true
        } catch {
            return false
        }
    }

    private func trame(_ ligne: String, _ l: Lien) {
        guard let v = lireJSON(ligne) else { return }
        if let svc = v["svc"] as? String {
            if let ev = v.objet("ev") { remettre(.evenement(svc: svc, ev: ev)) }
        } else if let m = v.objet("marques") {
            remettre(.marques(m))
        } else if let p = v["ping"] {
            l.ecrire(["pong": p])
        } else if let r = v.objet("fichier") ?? v.objet("depose") {
            let id = Int(r.entier("id"))
            let c = avecVerrou { attentes[id] }
            c?.yield(r)
        }
    }

    // MARK: - Fichiers (§6.1)

    /// Le nom local d'un fichier du serveur : SHA-256 du chemin (16 octets) et son extension.
    func urlLocale(_ distant: String) -> URL {
        let h = hex(SHA256.hash(data: Data(distant.utf8)).prefix(16))
        let dernier = distant.split(separator: "/").last.map(String.init) ?? ""
        var ext = ""
        if let i = dernier.lastIndex(of: ".") {
            ext = String(dernier[dernier.index(after: i)...].filter { $0.isASCII && ($0.isLetter || $0.isNumber) }.prefix(8))
        }
        return cache.appendingPathComponent(ext.isEmpty ? h : "\(h).\(ext)")
    }

    /**
     * La copie locale d'un fichier du serveur ([distant] commence par « / »).
     * Une copie déjà là sert tout de suite ; elle est revérifiée (sans renvoi
     * si elle n'a pas changé) au plus toutes les 10 minutes.
     */
    func fichierLocal(_ distant: String) async -> URL? {
        guard distant.hasPrefix("/") else { return nil }
        let local = urlLocale(distant)
        let existe = FileManager.default.fileExists(atPath: local.path)
        let t: Task<URL?, Never> = avecVerrou {
            if let vu = vus[distant], Date().timeIntervalSince(vu) < 600, existe {
                return Task { local }
            }
            if let t = enCours[distant] { return t }
            let t = Task { [weak self] () -> URL? in
                guard let self else { return nil }
                let r = await self.telecharger(distant, vers: local)
                _ = self.avecVerrou { self.enCours.removeValue(forKey: distant) }
                return r
            }
            enCours[distant] = t
            return t
        }
        if existe && !branche {
            return local
        }
        return await t.value
    }

    private func telecharger(_ distant: String, vers local: URL) async -> URL? {
        let fm = FileManager.default
        // Pas encore branché : on attend un peu (30 s au plus).
        var essais = 0
        while !branche && essais < 60 {
            try? await Task.sleep(nanoseconds: 500_000_000)
            essais += 1
        }
        guard branche else { return fm.fileExists(atPath: local.path) ? local : nil }
        await limiteur.prendre()
        defer { Task { await limiteur.rendre() } }

        let (flux, c) = AsyncStream<[String: Any]>.makeStream()
        let (id, l) = avecVerrou { () -> (Int, Lien?) in
            let id = prochain
            prochain += 1
            attentes[id] = c
            return (id, lien)
        }
        defer { _ = avecVerrou { attentes.removeValue(forKey: id) } }
        guard let l else { return fm.fileExists(atPath: local.path) ? local : nil }

        var connu = ""
        if let d = try? Data(contentsOf: local) { connu = hex(SHA256.hash(data: d)) }
        l.ecrire(["get": ["id": id, "chemin": distant, "sha256": connu]])

        let partiel = cache.appendingPathComponent(".\(local.lastPathComponent).part")
        var sortie: FileHandle?
        defer { try? sortie?.close() }
        // Délai : 120 s sans morceau → abandon.
        let chien = Task {
            try? await Task.sleep(nanoseconds: 120_000_000_000)
            if !Task.isCancelled { c.finish() }
        }
        defer { chien.cancel() }
        var ok = false
        for await m in flux {
            if !m.chaine("erreur").isEmpty { break }
            if m.booleen("inchange") {
                ok = true
                break
            }
            if sortie == nil {
                fm.createFile(atPath: partiel.path, contents: nil)
                sortie = try? FileHandle(forWritingTo: partiel)
                if sortie == nil { break }
            }
            let b64 = m.chaine("donnees")
            if !b64.isEmpty, let d = Data(base64Encoded: b64) { sortie?.write(d) }
            if m.booleen("fin") {
                try? sortie?.close()
                sortie = nil
                try? fm.removeItem(at: local)
                ok = (try? fm.moveItem(at: partiel, to: local)) != nil
                break
            }
        }
        if !ok {
            try? fm.removeItem(at: partiel)
            return nil
        }
        avecVerrou { vus[distant] = Date() }
        return local
    }
}

/// Au plus n téléchargements à la fois (le serveur en sert 2 par client, §6.1).
actor Limiteur {
    private var libres: Int
    private var file: [CheckedContinuation<Void, Never>] = []

    init(_ n: Int) { libres = n }

    func prendre() async {
        if libres > 0 {
            libres -= 1
            return
        }
        await withCheckedContinuation { file.append($0) }
    }

    func rendre() {
        if file.isEmpty { libres += 1 } else { file.removeFirst().resume() }
    }
}
