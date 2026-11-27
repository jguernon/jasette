// Jasette iOS — jumelage avec un serveur Jasette (PROTOCOLE.md §3), et les
// identifiants gardés dans le Trousseau iOS.

import CryptoKit
import Foundation
import Security

/// Ce qu'on garde d'un serveur jumelé (le jeton comme un mot de passe : Trousseau).
struct ConfigServeur: Codable, Equatable {
    var adresse: String
    var empreinte: String
    var jeton: String
    var nom: String
}

enum Jumelage {
    /// Étape 1 faite : le code de vérification à faire confirmer.
    struct Verification: Equatable {
        let adresse: String
        let code: String
        let empreinte: String
        let codeVerification: String
    }

    /**
     * Étape 1 : se brancher sans épinglage, retenir l'empreinte, lire l'accueil
     * et fermer. Rien de secret n'est envoyé.
     */
    static func verifier(adresse: String, code: String) async throws -> Verification {
        let c = try normaliserCode(code)
        let l = try await Lien.ouvrir(adresse: adresse, attendue: nil)
        l.fermer()
        return Verification(
            adresse: adresse.trimmingCharacters(in: .whitespacesAndNewlines), code: c, empreinte: l.empreinte,
            codeVerification: codeVerification(code: c, empreinte: l.empreinte))
    }

    /// Étape 2 : jumeler en exigeant l'empreinte vue à l'étape 1.
    static func jumeler(_ v: Verification, nom: String) async throws -> ConfigServeur {
        let l = try await Lien.ouvrir(adresse: v.adresse, attendue: v.empreinte)
        defer { l.fermer() }
        var octets = [UInt8](repeating: 0, count: 32)
        guard SecRandomCopyBytes(kSecRandomDefault, 32, &octets) == errSecSuccess else {
            throw ErreurLien(texte: "aléa indisponible")
        }
        let nc = Data(octets).base64EncodedString()
        let nomPris = String(nom.trimmingCharacters(in: .whitespacesAndNewlines).prefix(60))
        let preuve = hmac(code: v.code, message: "\(VERSION_PROTOCOLE)/client|\(l.empreinte)|\(l.nonce)|\(nc)")
        l.ecrire(["jumeler": ["nom": nomPris, "nonce": nc, "preuve": hex(preuve)]])
        let annuler = l.minuterie(20)
        defer { annuler() }
        let r = try await l.lire()
        if let e = erreurDe(r) { throw ErreurLien(texte: t(e), refus: true) }
        guard let j = r.objet("jumele"), !j.chaine("jeton").isEmpty else {
            throw ErreurLien(texte: t("réponse inattendue du serveur"))
        }
        // Le serveur prouve à son tour qu'il connaissait le code.
        let attendue = hmac(code: v.code, message: "\(VERSION_PROTOCOLE)/serveur|\(l.empreinte)|\(l.nonce)|\(nc)")
        guard hex(attendue) == j.chaine("preuve").lowercased() else {
            throw ErreurLien(texte: t("le serveur n'a pas prouvé qu'il connaissait le code"))
        }
        let retenu = j.chaine("nom")
        return ConfigServeur(adresse: v.adresse, empreinte: l.empreinte, jeton: j.chaine("jeton"),
                             nom: retenu.isEmpty ? nomPris : retenu)
    }

    static func normaliserCode(_ code: String) throws -> String {
        let c = code.filter { $0.isASCII && $0.isNumber }
        if c.count != 6 { throw ErreurLien(texte: t("le code de jumelage a 6 chiffres")) }
        return c
    }

    /// SHA-256("jasette/1/verification|code|empreinte") → 4 octets grand-boutistes mod 1 000 000.
    static func codeVerification(code: String, empreinte: String) -> String {
        let s = Array(SHA256.hash(data: Data("\(VERSION_PROTOCOLE)/verification|\(code)|\(empreinte)".utf8)))
        let n = (UInt32(s[0]) << 24) | (UInt32(s[1]) << 16) | (UInt32(s[2]) << 8) | UInt32(s[3])
        return String(format: "%06u", n % 1_000_000)
    }

    static func hmac(code: String, message: String) -> [UInt8] {
        let cle = SymmetricKey(data: Data(code.utf8))
        return Array(HMAC<SHA256>.authenticationCode(for: Data(message.utf8), using: cle))
    }
}

/// Le texte d'une trame d'erreur du serveur (§2), ou nil.
func erreurDe(_ v: [String: Any]) -> String? {
    guard v["erreur"] != nil else { return nil }
    let texte = v.chaine("texte")
    return texte.isEmpty ? v.chaine("erreur") : texte
}

// MARK: - Le Trousseau

/// La configuration du serveur, dans le Trousseau (kSecAttrAccessibleAfterFirstUnlock).
enum Trousseau {
    private static let service = "ca.jasette.ios.serveur"
    private static let compte = "serveur"

    private static var requete: [String: Any] {
        [kSecClass as String: kSecClassGenericPassword,
         kSecAttrService as String: service,
         kSecAttrAccount as String: compte]
    }

    static func lire() -> ConfigServeur? {
        var q = requete
        q[kSecReturnData as String] = true
        q[kSecMatchLimit as String] = kSecMatchLimitOne
        var res: CFTypeRef?
        guard SecItemCopyMatching(q as CFDictionary, &res) == errSecSuccess, let d = res as? Data else { return nil }
        return try? JSONDecoder().decode(ConfigServeur.self, from: d)
    }

    @discardableResult
    static func garder(_ c: ConfigServeur?) -> Bool {
        SecItemDelete(requete as CFDictionary)
        guard let c, let d = try? JSONEncoder().encode(c) else { return true }
        var q = requete
        q[kSecValueData as String] = d
        q[kSecAttrAccessible as String] = kSecAttrAccessibleAfterFirstUnlock
        return SecItemAdd(q as CFDictionary, nil) == errSecSuccess
    }
}
