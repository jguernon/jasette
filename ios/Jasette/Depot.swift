// Jasette iOS — l'état de l'app : la connexion au serveur, les conversations,
// les messages et les marques (comme Depot.kt d'Android, en bien plus petit).

import Foundation
import Observation
import UIKit

enum EtatLien: Equatable {
    case aucun, connexion, reconnexion, branche
}

@MainActor
@Observable
final class Depot {
    // MARK: Le serveur

    private(set) var config: ConfigServeur?
    private(set) var etatLien: EtatLien = .aucun
    /// La dernière erreur de connexion (refus, certificat changé, injoignable).
    private(set) var erreurLien: String?
    /// Une erreur d'un service à montrer un moment (envoi refusé…).
    var erreur: String?
    /// Les services offerts par le serveur (`auth ok`).
    private(set) var services: [String] = []
    /// L'état de chaque service (`state` : ready, connecting, unpaired…).
    private(set) var etats: [String: String] = [:]
    /// Le compte de chaque service (`account`).
    private(set) var comptes: [String: String] = [:]
    @ObservationIgnored private var connexion: ConnexionServeur?

    // MARK: Les données

    private(set) var convs: [Service: [Conv]] = [:]
    private(set) var avatars: [String: String] = [:]
    private(set) var messages: [String: [Msg]] = [:]
    private(set) var plusAnciens: [String: Bool] = [:]
    /// La conversation dont on attend les messages (indicateur de chargement).
    private(set) var chargement: String?
    /// Les pièces reçues : media_id → chemin du serveur ; et celles en échec.
    private(set) var medias: [String: String] = [:]
    private(set) var echecsMedias: Set<String> = []
    @ObservationIgnored private var demandesMedias: Set<String> = []

    // MARK: Les marques (§5.5), en lecture seule

    private(set) var epingles: [String] = []
    private(set) var archivesJasette: Set<String> = []
    private(set) var privees: Set<String> = []
    private(set) var cacherApresJours = 0
    private(set) var ordreServices: [String] = []
    /// La sourdine (marque `sourdine`) : id → échéance (ms), 0 = sans fin.
    /// iOS n'a pas de notifications : seule la cloche barrée se montre.
    private(set) var sourdine: [String: Int64] = [:]
    /// Le spam (marque `spam`) : ces conversations n'apparaissent pas dans la
    /// liste (le dossier Spam est sur le bureau et Android).
    private(set) var spam: Set<String> = []
    private(set) var marquesRecues = false

    /// La conversation ouverte et l'app au premier plan.
    var ouverte: String?
    private(set) var premierPlan = false
    @ObservationIgnored private var envoiSeq = 0

    init() {
        config = Trousseau.lire()
    }

    // MARK: - Jumelage et connexion

    func jumele(_ c: ConfigServeur) {
        Trousseau.garder(c)
        config = c
        brancher()
    }

    /// « Ne plus utiliser ce serveur » : tout est oublié ici (le serveur garde ses services).
    func oublierServeur() {
        connexion?.arreter()
        connexion = nil
        if let c = config {
            try? FileManager.default.removeItem(at: ConnexionServeur(config: c, surNouvelle: { _ in }).cache)
        }
        Trousseau.garder(nil)
        config = nil
        etatLien = .aucun
        erreurLien = nil
        services = []
        etats = [:]
        comptes = [:]
        convs = [:]
        avatars = [:]
        messages = [:]
        medias = [:]
        echecsMedias = []
        demandesMedias = []
        epingles = []
        archivesJasette = []
        privees = []
        sourdine = [:]
        spam = []
        marquesRecues = false
        ouverte = nil
    }

    /// L'app passe au premier plan (branchement) ou le quitte (débranchement).
    func passerPremierPlan(_ oui: Bool) {
        premierPlan = oui
        if oui { brancher() } else { debrancher() }
    }

    private func brancher() {
        guard let c = config, premierPlan else { return }
        if connexion == nil {
            connexion = ConnexionServeur(config: c) { [weak self] n in self?.recevoir(n) }
        }
        connexion?.demarrer()
    }

    private func debrancher() {
        connexion?.arreter()
        connexion = nil
        if config != nil { etatLien = .aucun }
    }

    func fichierLocal(_ distant: String) async -> URL? {
        guard let cx = connexion else { return nil }
        return await cx.fichierLocal(distant)
    }

    // MARK: - Les nouvelles du serveur

    private func recevoir(_ n: ConnexionServeur.Nouvelle) {
        switch n {
        case .cherche(let premiere):
            etatLien = premiere ? .connexion : .reconnexion
        case .branche(let s):
            etatLien = .branche
            erreurLien = nil
            services = s
            // Les fils déjà chargés peuvent avoir manqué des messages : la conversation ouverte est redemandée.
            if let o = ouverte { demanderFil(o) }
        case .erreur(let texte):
            erreurLien = texte
        case .coupe:
            etatLien = .reconnexion
        case .marques(let m):
            recevoirMarques(m)
        case .evenement(let svc, let ev):
            guard let s = Service(rawValue: svc) else { return }
            recevoirEvenement(s.prefixer(ev), service: s)
        }
    }

    private func recevoirMarques(_ m: [String: Any]) {
        epingles = m.chaines("epingles")
        archivesJasette = Set(m.chaines("archives"))
        privees = Set(m.chaines("privees"))
        cacherApresJours = Int(m.entier("cacher_apres_jours"))
        ordreServices = m.chaines("ordre_services")
        var s: [String: Int64] = [:]
        for (id, v) in m.objet("sourdine") ?? [:] {
            if let n = v as? NSNumber { s[id] = n.int64Value }
        }
        sourdine = s
        spam = Set((m.objet("spam") ?? [:]).keys)
        marquesRecues = true
    }

    private func recevoirEvenement(_ v: [String: Any], service: Service) {
        let svc = service.cle
        switch v.chaine("ev") {
        case "state":
            let avant = etats[svc]
            let apres = v.chaine("state")
            etats[svc] = apres
            if apres == "ready" && avant != "ready" {
                // Le serveur rejoue sa liste gardée ; on la redemande quand même (lecture seule).
                commande(service, ["cmd": "conversations", "count": 50])
                if let o = ouverte, Service.de(o) == service { demanderFil(o) }
            }
            if apres == "unpaired" {
                convs[service] = nil
                comptes[svc] = nil
            }
        case "account":
            let nom = v.chaine("name").isEmpty ? v.chaine("email") : v.chaine("name")
            if !nom.isEmpty { comptes[svc] = nom }
        case "conversations":
            // Les archives des services (`folder: archive`) ne sont pas montrées dans cette version.
            if v.chaine("folder") == "archive" || v.booleen("archive") { return }
            var vus = Set<String>()
            convs[service] = v.objets("items").map(Conv.init).filter { !$0.id.isEmpty && vus.insert($0.id).inserted }
        case "conversation":
            guard let item = v.objet("item") else { return }
            let c = Conv(item)
            var l = convs[service] ?? []
            l.removeAll { $0.id == c.id }
            if !c.archived { l.append(c) }
            convs[service] = l
        case "avatar":
            let conv = v.chaine("conv")
            let path = v.chaine("path")
            if !conv.isEmpty && !path.isEmpty { avatars[conv] = path }
        case "media":
            let id = v.chaine("media_id")
            demandesMedias.remove(id)
            let path = v.chaine("path")
            if path.isEmpty {
                echecsMedias.insert(id)
            } else {
                medias[id] = path
                echecsMedias.remove(id)
            }
        case "messages":
            recevoirFil(v)
        case "message":
            guard let item = v.objet("item") else { return }
            recevoirMessage(Msg(item))
        case "deleted":
            let id = v.chaine("conv")
            convs[service]?.removeAll { $0.id == id }
            messages[id] = nil
        case "sent":
            if !v.booleen("ok") {
                let tmp = v.chaine("tmp_id")
                for (conv, liste) in messages where liste.contains(where: { $0.tmpId == tmp }) {
                    messages[conv] = liste.map { m in
                        var m = m
                        if m.tmpId == tmp && m.local { m.status = "ÉCHEC" }
                        return m
                    }
                }
            }
        case "error":
            let texte = v.chaine("text")
            if v.chaine("where") == "messages" { chargement = nil }
            if !texte.isEmpty && v.chaine("where") != "search" { erreur = "\(service.titre) : \(texte)" }
        default:
            break
        }
    }

    private func recevoirFil(_ v: [String: Any]) {
        let conv = v.chaine("conv")
        guard !conv.isEmpty else { return }
        let items = v.objets("items").map(Msg.init)
        let liste = messages[conv] ?? []
        if v.booleen("older") {
            let ids = Set(liste.map(\.id))
            messages[conv] = items.filter { !ids.contains($0.id) } + liste
            plusAnciens[conv] = v.booleen("more")
            if chargement == conv { chargement = nil }
            return
        }
        // Le serveur répond d'abord depuis son cache (`cache:true`), puis le pont fait foi (§5.8).
        let duCache = v.booleen("cache")
        let fusion = fusionner(liste, items, autorite: !duCache)
        messages[conv] = fusion
        let plusVieux = items.map(\.time).min().map { d in fusion.contains { !$0.local && $0.time < d } } ?? false
        plusAnciens[conv] = v.booleen("more") || plusVieux
        if chargement == conv && (!duCache || !fusion.isEmpty) { chargement = nil }
        if !duCache && premierPlan && ouverte == conv { marquerLu(conv) }
    }

    /// La fusion d'une page (comme `fusionner` d'Android et `fusionner_page` du bureau).
    private func fusionner(_ liste: [Msg], _ items: [Msg], autorite: Bool) -> [Msg] {
        var vus = Set<String>()
        let recus = items.filter { !$0.local && vus.insert($0.id).inserted }
        let ids = Set(recus.map(\.id))
        let tmps = Set(recus.compactMap { $0.tmpId.isEmpty ? nil : $0.tmpId })
        let debut = recus.map(\.time).min() ?? Int64.max
        let fin = recus.map(\.time).max() ?? Int64.min
        var locaux: [Msg] = []
        var autres: [Msg] = []
        for m in liste {
            if ids.contains(m.id) { continue }
            if !m.tmpId.isEmpty && tmps.contains(m.tmpId) { continue }
            if recus.contains(where: { m.localPour($0) }) { continue }
            if m.local {
                locaux.append(m)
            } else if autorite && m.time >= debut && m.time <= fin {
                continue
            } else {
                autres.append(m)
            }
        }
        return (recus + autres).sorted { $0.time < $1.time } + locaux
    }

    private func recevoirMessage(_ m: Msg) {
        if var liste = messages[m.conv] {
            if let i = liste.firstIndex(where: { $0.id == m.id || (!m.tmpId.isEmpty && $0.tmpId == m.tmpId) || $0.localPour(m) }) {
                liste[i] = m
            } else {
                liste.append(m)
            }
            let locaux = liste.filter(\.local)
            messages[m.conv] = liste.filter { !$0.local }.sorted { $0.time < $1.time } + locaux
        }
        // La liste suit : aperçu, heure, non-lu.
        let s = Service.de(m.conv)
        if var l = convs[s], let i = l.firstIndex(where: { $0.id == m.conv }), m.time >= l[i].time {
            if !m.text.isEmpty { l[i].preview = m.text } else if !m.attachments.isEmpty { l[i].preview = apercuPieces(m) }
            l[i].time = m.time
            l[i].fromMe = m.fromMe
            let ouverteIci = premierPlan && ouverte == m.conv
            if !m.fromMe && !ouverteIci { l[i].unread = true }
            convs[s] = l
            if !m.fromMe && ouverteIci { marquerLu(m.conv) }
        }
    }

    // MARK: - La liste

    /// Les services montrés : ceux du serveur, dans l'ordre choisi (`ordre_services`).
    var servicesMontres: [Service] {
        Service.ordonnes(ordreServices, parmi: Service.allCases.filter { services.contains($0.cle) })
    }

    enum Filtre: Hashable {
        case tout, groupes, service(Service)
    }

    /// Les conversations d'une vue (jamais les privées, §5.5 « Règle des privées »).
    func liste(_ f: Filtre) -> [Conv] {
        let offerts = Set(services)
        var l = convs.filter { offerts.contains($0.key.cle) }.values.flatMap { $0 }
            .filter { !privees.contains($0.id) && !spam.contains($0.id) && !$0.archived && !archivesJasette.contains($0.id) }
        switch f {
        case .tout:
            l = l.filter { !$0.large }
            if cacherApresJours > 0 {
                let limite = Int64(Date().timeIntervalSince1970 * 1000) - Int64(cacherApresJours) * 86_400_000
                let ep = Set(epingles)
                l = l.filter { $0.time >= limite || $0.unread || ep.contains($0.id) }
            }
        case .groupes:
            l = l.filter(\.large)
        case .service(let s):
            l = l.filter { $0.service == s && !$0.large }
        }
        let rang = Dictionary(epingles.enumerated().map { ($1, $0) }, uniquingKeysWith: { a, _ in a })
        return l.sorted { a, b in
            switch (rang[a.id], rang[b.id]) {
            case let (x?, y?): return x < y
            case (_?, nil): return true
            case (nil, _?): return false
            default: return a.time > b.time
            }
        }
    }

    func estEpinglee(_ id: String) -> Bool { epingles.contains(id) }

    /// En sourdine maintenant (une échéance passée ne compte plus).
    func estEnSourdine(_ id: String) -> Bool {
        guard let t = sourdine[id] else { return false }
        return t == 0 || t > Int64(Date().timeIntervalSince1970 * 1000)
    }

    func conv(_ id: String) -> Conv? { convs[Service.de(id)]?.first { $0.id == id } }

    var aDesGroupes: Bool { convs.values.contains { $0.contains { $0.large && !privees.contains($0.id) } } }

    // MARK: - Commandes

    private func commande(_ s: Service, _ cmd: [String: Any]) {
        connexion?.envoyer(svc: s.cle, cmd: s.deprefixer(cmd))
    }

    private func commande(_ cmd: [String: Any]) {
        guard let conv = (cmd["conv"] as? String) ?? (cmd["media_id"] as? String) else { return }
        commande(Service.de(conv), cmd)
    }

    /// Ouvre une conversation : ses messages sont demandés, elle est marquée lue.
    func ouvrir(_ id: String) {
        ouverte = id
        if messages[id] == nil { chargement = id }
        demanderFil(id)
        if messages[id] != nil { marquerLu(id) }
    }

    func fermer(_ id: String) {
        if ouverte == id { ouverte = nil }
        if chargement == id { chargement = nil }
    }

    private func demanderFil(_ id: String) {
        commande(["cmd": "messages", "conv": id, "count": 60])
    }

    func plusVieux(_ id: String) {
        guard chargement != id, let premier = messages[id]?.first(where: { !$0.local }) else { return }
        chargement = id
        commande(["cmd": "messages", "conv": id, "count": 60, "before": premier.id, "before_time": premier.time])
    }

    /// « Lu » : seulement pour la conversation ouverte et visible.
    func marquerLu(_ id: String) {
        guard let c = conv(id), c.unread || c.mentions > 0 else { return }
        var cmd: [String: Any] = ["cmd": "read", "conv": id]
        if let dernier = messages[id]?.last(where: { !$0.local }) { cmd["msg"] = dernier.id }
        commande(cmd)
        let s = Service.de(id)
        if var l = convs[s], let i = l.firstIndex(where: { $0.id == id }) {
            l[i].unread = false
            l[i].mentions = 0
            convs[s] = l
        }
    }

    /// Envoie un texte (seulement sur un geste de l'utilisateur) : une bulle locale le montre tout de suite.
    func envoyer(_ conv: String, _ texte: String) {
        let t = texte.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !t.isEmpty else { return }
        envoiSeq += 1
        let maintenant = Int64(Date().timeIntervalSince1970 * 1000)
        let tmp = "jasette_\(maintenant)_\(envoiSeq)"
        let local = Msg(id: tmp, conv: conv, text: t, fromMe: true, time: maintenant, status: "ENVOI", tmpId: tmp)
        messages[conv, default: []].append(local)
        commande(["cmd": "send", "conv": conv, "text": t, "tmp_id": tmp])
    }

    /// Demande une image au pont, quand elle paraît à l'écran (§6).
    func demanderMedia(_ p: Piece) {
        let id = p.mediaId
        guard !id.isEmpty, medias[id] == nil, !echecsMedias.contains(id), !demandesMedias.contains(id) else { return }
        demandesMedias.insert(id)
        commande(["cmd": "download", "media_id": id, "key": p.key, "mime": p.mime])
    }

    /// Un nouvel essai sur un geste de l'utilisateur.
    func reessayerMedia(_ p: Piece) {
        echecsMedias.remove(p.mediaId)
        demandesMedias.remove(p.mediaId)
        demanderMedia(p)
    }
}

func apercuPieces(_ m: Msg) -> String {
    m.attachments.contains(where: \.estImage) ? t("📷 Photo") : t("📎 Pièce jointe")
}
