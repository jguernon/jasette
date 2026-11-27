// Jasette iOS — les services, les conversations et les messages (mêmes champs
// que les ponts, comme Modeles.kt d'Android).

import Foundation

/**
 * Les services. Les ids des services autres que Google portent un préfixe dans
 * l'interface, comme sur le bureau et Android : « m: » Messenger, « i: »
 * Instagram, « w: » WhatsApp… Le préfixe dit aussi à quel pont envoyer une commande.
 */
enum Service: String, CaseIterable, Identifiable {
    case google, messenger, instagram, whatsapp, signal, x, telegram, bluesky, discord, reddit, linkedin

    var id: String { rawValue }
    var cle: String { rawValue }

    var titre: String {
        switch self {
        case .google: return "Google Messages"
        case .messenger: return "Messenger"
        case .instagram: return "Instagram"
        case .whatsapp: return "WhatsApp"
        case .signal: return "Signal"
        case .x: return "X"
        case .telegram: return "Telegram"
        case .bluesky: return "Bluesky"
        case .discord: return "Discord"
        case .reddit: return "Reddit"
        case .linkedin: return "LinkedIn"
        }
    }

    var prefixe: String {
        switch self {
        case .google: return ""
        case .messenger: return "m:"
        case .instagram: return "i:"
        case .whatsapp: return "w:"
        case .signal: return "s:"
        case .x: return "x:"
        case .telegram: return "t:"
        case .bluesky: return "b:"
        case .discord: return "d:"
        case .reddit: return "r:"
        case .linkedin: return "l:"
        }
    }

    /// Le logo (catalogue d'images).
    var logo: String { "service-\(rawValue)" }

    private static let prefixes = allCases.filter { !$0.prefixe.isEmpty }

    /// Le service d'un id d'interface (Google s'il n'a pas de préfixe connu).
    static func de(_ id: String) -> Service { prefixes.first { id.hasPrefix($0.prefixe) } ?? .google }

    /// Les services de [parmi], dans l'ordre choisi (marque `ordre_services`, §5.5).
    static func ordonnes(_ ordre: [String], parmi: [Service]) -> [Service] {
        var l: [Service] = []
        for c in ordre { if let s = Service(rawValue: c), parmi.contains(s), !l.contains(s) { l.append(s) } }
        for s in parmi where !l.contains(s) { l.append(s) }
        return l
    }

    /**
     * Ajoute le préfixe du service aux ids d'une nouvelle de son pont (comme
     * `prefixerMeta` d'Android) : `conv`, `media_id`, et `id` dans `item(s)`.
     */
    func prefixer(_ ev: [String: Any]) -> [String: Any] {
        if prefixe.isEmpty { return ev }
        func ids(_ x: Any, dansItem: Bool) -> Any {
            if let o = x as? [String: Any] {
                var r = o
                for (k, v) in o {
                    let cle = k == "conv" || k == "media_id" || (dansItem && k == "id")
                    if cle, let s = v as? String, !s.isEmpty, !s.hasPrefix(prefixe) {
                        r[k] = prefixe + s
                    } else if v is [String: Any] || v is [Any] {
                        r[k] = ids(v, dansItem: k == "item" || k == "items")
                    }
                }
                return r
            }
            if let a = x as? [Any] { return a.map { ids($0, dansItem: dansItem) } }
            return x
        }
        return ids(ev, dansItem: false) as? [String: Any] ?? ev
    }

    /// Retire le préfixe des ids d'une commande (`conv`, `media_id`, `msg`, `id`).
    func deprefixer(_ cmd: [String: Any]) -> [String: Any] {
        if prefixe.isEmpty { return cmd }
        var r = cmd
        for k in ["conv", "media_id", "msg", "id"] {
            if let s = r[k] as? String, s.hasPrefix(prefixe) { r[k] = String(s.dropFirst(prefixe.count)) }
        }
        return r
    }
}

/// Une conversation (mêmes champs que `Conversation` des ponts).
struct Conv: Identifiable, Equatable {
    let id: String
    var name: String
    var preview: String
    var fromMe: Bool
    var time: Int64
    var unread: Bool
    var group: Bool
    var color: String
    var readOnly: Bool
    var archived: Bool
    /// Un grand espace (§5.13) : salon, canal, communauté… (pas dans la boîte de réception).
    var large: Bool
    var mentions: Int

    var service: Service { Service.de(id) }

    init(_ o: [String: Any]) {
        id = o.chaine("id")
        name = o.chaine("name")
        preview = o.chaine("preview")
        fromMe = o.booleen("from_me")
        time = o.entier("time")
        unread = o.booleen("unread")
        group = o.booleen("group")
        color = o.chaine("color")
        readOnly = o.booleen("read_only")
        archived = o.booleen("archived")
        large = o.booleen("large")
        mentions = max(0, Int(o.entier("mentions")))
    }
}

/// Une pièce jointe. La clé (base64) est renvoyée telle quelle au pont.
struct Piece: Equatable {
    let mediaId: String
    let key: String
    let mime: String
    let name: String
    let w: Int64
    let h: Int64

    var estImage: Bool { mime.hasPrefix("image/") }

    init(_ o: [String: Any]) {
        mediaId = o.chaine("media_id")
        key = o.chaine("key")
        mime = o.chaine("mime")
        name = o.chaine("name")
        w = o.entier("w")
        h = o.entier("h")
    }
}

struct Reaction: Equatable {
    let emoji: String
    let fromMe: Bool
}

struct Msg: Identifiable, Equatable {
    let id: String
    let conv: String
    var text: String
    var fromMe: Bool
    var sender: String
    var time: Int64
    var status: String
    var tmpId: String
    var attachments: [Piece]
    var reactions: [Reaction]

    /// Un envoi local pas encore confirmé par le pont.
    var local: Bool { id == tmpId }

    init(id: String, conv: String, text: String, fromMe: Bool, time: Int64, status: String, tmpId: String) {
        self.id = id
        self.conv = conv
        self.text = text
        self.fromMe = fromMe
        sender = ""
        self.time = time
        self.status = status
        self.tmpId = tmpId
        attachments = []
        reactions = []
    }

    init(_ o: [String: Any]) {
        id = o.chaine("id")
        conv = o.chaine("conv")
        // Un message de service connu (§5.14) : son texte dans la langue de l'interface.
        text = texteSysteme(o.chaine("system")) ?? o.chaine("text")
        fromMe = o.booleen("from_me")
        sender = o.chaine("sender")
        time = o.entier("time")
        status = o.chaine("status")
        tmpId = o.chaine("tmp_id")
        attachments = o.objets("attachments").map(Piece.init)
        reactions = o.objets("reactions").map { Reaction(emoji: $0.chaine("emoji"), fromMe: $0.booleen("from_me")) }
            .filter { !$0.emoji.isEmpty }
    }

    /**
     * Cet envoi local est-il le même que [m], le message confirmé ? Par tmp_id,
     * sinon : de moi, même texte, à moins de 10 minutes d'écart (comme Android).
     */
    func localPour(_ m: Msg) -> Bool {
        if !local || m.local { return false }
        if !m.tmpId.isEmpty && tmpId == m.tmpId { return true }
        return m.fromMe && text == m.text && attachments.count == m.attachments.count && abs(m.time - time) < 600_000
    }
}

/// Le texte d'un code `system` (§5.14), ou nil.
func texteSysteme(_ code: String) -> String? {
    switch code {
    case "deleted": return t("🚫 Message retiré")
    case "call": return t("📞 Appel")
    case "history_cleared": return t("Historique effacé")
    case "safety_number_changed": return t("🔐 Le numéro de sécurité de cette personne a changé")
    case "decrypt_failed": return t("⚠ Un message n'a pas pu être déchiffré : vérifie dans Signal sur le téléphone")
    default: return nil
    }
}
