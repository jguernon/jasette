// Jasette iOS — la connexion TLS épinglée au serveur Jasette (PROTOCOLE.md §1).
//
// TCP + TLS 1.3 (Network.framework). Aucune autorité de certification : seule
// compte l'empreinte SHA-256 du certificat feuille (DER), en hexadécimal
// minuscule. Les trames sont des lignes JSON terminées par « \n ».

import CryptoKit
import Foundation
import Network

let VERSION_PROTOCOLE = "jasette/1"
let PORT_PAR_DEFAUT: UInt16 = 47800

/// Une connexion ratée. [empreinte] ou [refus] : on attend 60 s avant de réessayer.
struct ErreurLien: LocalizedError {
    let texte: String
    var empreinte = false
    var refus = false
    var errorDescription: String? { texte }
}

func hex<S: Sequence>(_ d: S) -> String where S.Element == UInt8 {
    d.map { String(format: "%02x", $0) }.joined()
}

/// Sépare « hôte », « hôte:port », « [ipv6]:port » ou une IPv6 nue.
func hotePort(_ adresse: String) throws -> (String, UInt16) {
    let a = adresse.trimmingCharacters(in: .whitespacesAndNewlines)
    if a.isEmpty { throw ErreurLien(texte: t("adresse vide")) }
    func port(_ p: Substring) throws -> UInt16 {
        guard let n = UInt16(p), n > 0 else { throw ErreurLien(texte: "\(t("port invalide")) : \(p)") }
        return n
    }
    if a.hasPrefix("[") {
        guard let f = a.firstIndex(of: "]") else { throw ErreurLien(texte: "\(t("adresse invalide")) : \(a)") }
        let hote = String(a[a.index(after: a.startIndex)..<f])
        let reste = a[a.index(after: f)...]
        return (hote, reste.hasPrefix(":") ? try port(reste.dropFirst()) : PORT_PAR_DEFAUT)
    }
    if a.filter({ $0 == ":" }).count == 1, let i = a.firstIndex(of: ":") {
        return (String(a[..<i]), try port(a[a.index(after: i)...]))
    }
    return (a, PORT_PAR_DEFAUT)
}

// MARK: - JSON

func lireJSON(_ ligne: String) -> [String: Any]? {
    guard let d = ligne.data(using: .utf8) else { return nil }
    return (try? JSONSerialization.jsonObject(with: d)) as? [String: Any]
}

func ecrireJSON(_ o: [String: Any]) -> String {
    guard let d = try? JSONSerialization.data(withJSONObject: o, options: [.withoutEscapingSlashes]) else { return "{}" }
    return String(decoding: d, as: UTF8.self)
}

extension Dictionary where Key == String, Value == Any {
    func chaine(_ k: String) -> String {
        switch self[k] {
        case let s as String: return s
        case let n as NSNumber: return n.stringValue
        default: return ""
        }
    }
    func entier(_ k: String) -> Int64 { (self[k] as? NSNumber)?.int64Value ?? Int64(chaine(k)) ?? 0 }
    func booleen(_ k: String) -> Bool { (self[k] as? NSNumber)?.boolValue ?? false }
    func objet(_ k: String) -> [String: Any]? { self[k] as? [String: Any] }
    func objets(_ k: String) -> [[String: Any]] { self[k] as? [[String: Any]] ?? [] }
    func chaines(_ k: String) -> [String] { (self[k] as? [Any])?.compactMap { $0 as? String } ?? [] }
}

// MARK: - Le lien

/// Une boîte partagée avec le bloc de vérification TLS (il tourne sur une autre file).
private final class Vue: @unchecked Sendable {
    private let verrou = NSLock()
    private var _empreinte: String?
    var empreinte: String? {
        get { verrou.lock(); defer { verrou.unlock() }; return _empreinte }
        set { verrou.lock(); _empreinte = newValue; verrou.unlock() }
    }
}

/// Une connexion TLS : lignes JSON dans les deux sens. Un seul lecteur à la fois.
final class Lien: @unchecked Sendable {
    private let cx: NWConnection
    private var tampon = Data()
    private var cherche = 0
    let empreinte: String
    /// Le nonce de l'accueil du serveur (base64, tel quel).
    private(set) var nonce = ""

    private init(cx: NWConnection, empreinte: String) {
        self.cx = cx
        self.empreinte = empreinte
    }

    /// Écrit une ligne (l'ordre des envois est garanti par NWConnection).
    func ecrire(_ ligne: String) {
        cx.send(content: Data((ligne + "\n").utf8), completion: .contentProcessed { [weak self] err in
            if err != nil { self?.fermer() }
        })
    }

    func ecrire(_ o: [String: Any]) { ecrire(ecrireJSON(o)) }

    func fermer() { cx.cancel() }

    /// La ligne suivante, ou nil si la connexion est fermée.
    func ligne() async throws -> String? {
        while true {
            if let i = tampon[cherche...].firstIndex(of: 0x0A) {
                let l = String(decoding: tampon[tampon.startIndex..<i], as: UTF8.self)
                tampon = Data(tampon[tampon.index(after: i)...])
                cherche = tampon.startIndex
                return l
            }
            cherche = tampon.endIndex
            if tampon.count > 32 << 20 { throw ErreurLien(texte: "trame trop longue") }
            guard let d = try await recevoir() else { return nil }
            tampon.append(d)
        }
    }

    /// Une trame JSON (erreur si la connexion est fermée ou la ligne illisible).
    func lire() async throws -> [String: Any] {
        guard let l = try await ligne() else { throw ErreurLien(texte: t("connexion fermée")) }
        guard let o = lireJSON(l) else { throw ErreurLien(texte: t("réponse inattendue du serveur")) }
        return o
    }

    private func recevoir() async throws -> Data? {
        try await withCheckedThrowingContinuation { (c: CheckedContinuation<Data?, Error>) in
            cx.receive(minimumIncompleteLength: 1, maximumLength: 1 << 18) { data, _, complet, err in
                if let data, !data.isEmpty { c.resume(returning: data); return }
                if let err { c.resume(throwing: err); return }
                if complet { c.resume(returning: nil); return }
                c.resume(returning: Data())
            }
        }
    }

    /// Coupe la connexion si [secondes] passent avant l'appel du retour.
    func minuterie(_ secondes: Double) -> () -> Void {
        let w = DispatchWorkItem { [weak self] in self?.fermer() }
        DispatchQueue.global().asyncAfter(deadline: .now() + secondes, execute: w)
        return { w.cancel() }
    }

    /**
     * TCP + TLS 1.3, avec l'empreinte [attendue] exigée (ou seulement retenue
     * si nil), puis lecture de l'accueil du serveur (§2).
     */
    static func ouvrir(adresse: String, attendue: String?) async throws -> Lien {
        let (hote, port) = try hotePort(adresse)
        let vue = Vue()
        let file = DispatchQueue(label: "ca.jasette.lien")
        let tls = NWProtocolTLS.Options()
        let sp = tls.securityProtocolOptions
        sec_protocol_options_set_min_tls_protocol_version(sp, .TLSv13)
        // « jasette-serveur » : le nom SNI (ignoré par le serveur).
        sec_protocol_options_set_tls_server_name(sp, "jasette-serveur")
        // Pas d'autorité de certification : seule l'empreinte compte.
        sec_protocol_options_set_verify_block(sp, { _, confiance, fini in
            let t = sec_trust_copy_ref(confiance).takeRetainedValue()
            guard let chaine = SecTrustCopyCertificateChain(t) as? [SecCertificate], let feuille = chaine.first else {
                fini(false)
                return
            }
            let e = hex(SHA256.hash(data: SecCertificateCopyData(feuille) as Data))
            vue.empreinte = e
            fini(attendue == nil || e == attendue)
        }, file)
        let tcp = NWProtocolTCP.Options()
        tcp.noDelay = true
        tcp.connectionTimeout = 10
        let params = NWParameters(tls: tls, tcp: tcp)
        guard let p = NWEndpoint.Port(rawValue: port) else { throw ErreurLien(texte: t("port invalide")) }
        let cx = NWConnection(host: NWEndpoint.Host(hote), port: p, using: params)

        do {
            try await attendrePret(cx, file: file)
        } catch {
            cx.cancel()
            if let a = attendue, let v = vue.empreinte, v != a {
                throw ErreurLien(
                    texte: t("le certificat du serveur a changé : connexion refusée. S'il a été réinstallé, jumelle de nouveau."),
                    empreinte: true)
            }
            throw ErreurLien(texte: "\(t("serveur injoignable")) : \(error.localizedDescription)")
        }
        guard let e = vue.empreinte else {
            cx.cancel()
            throw ErreurLien(texte: t("pas de certificat"))
        }
        let lien = Lien(cx: cx, empreinte: e)
        // Le serveur parle le premier ; 20 s au plus.
        let annuler = lien.minuterie(20)
        defer { annuler() }
        let bonjour: [String: Any]
        do {
            bonjour = try await lien.lire()
        } catch {
            lien.fermer()
            throw ErreurLien(texte: "\(t("serveur injoignable")) : \(error.localizedDescription)")
        }
        if bonjour.chaine("jasette") != "serveur" {
            lien.fermer()
            throw ErreurLien(texte: t("ce n'est pas un serveur Jasette"))
        }
        lien.nonce = bonjour.chaine("nonce")
        return lien
    }

    private static func attendrePret(_ cx: NWConnection, file: DispatchQueue) async throws {
        final class Une: @unchecked Sendable { var faite = false }
        let une = Une()
        try await withCheckedThrowingContinuation { (c: CheckedContinuation<Void, Error>) in
            @Sendable func finir(_ r: Result<Void, Error>) {
                if une.faite { return }
                une.faite = true
                cx.stateUpdateHandler = nil
                c.resume(with: r)
            }
            cx.stateUpdateHandler = { etat in
                switch etat {
                case .ready: finir(.success(()))
                case .failed(let e), .waiting(let e): finir(.failure(e))
                case .cancelled: finir(.failure(ErreurLien(texte: t("connexion fermée"))))
                default: break
                }
            }
            cx.start(queue: file)
            file.asyncAfter(deadline: .now() + 12) {
                finir(.failure(ErreurLien(texte: t("délai dépassé"))))
            }
        }
    }
}
