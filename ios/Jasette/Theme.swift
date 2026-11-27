// Jasette iOS — les couleurs du thème sombre (palette `sombre()` de ui/Theme.kt
// d'Android, elle-même reprise du bureau).

import SwiftUI

extension Color {
    init(hex: UInt32, alpha: Double = 1) {
        self.init(.sRGB,
                  red: Double((hex >> 16) & 0xff) / 255,
                  green: Double((hex >> 8) & 0xff) / 255,
                  blue: Double(hex & 0xff) / 255,
                  opacity: alpha)
    }

    /// « #rrggbb » ou « rrggbb » (couleur d'une conversation), sinon nil.
    init?(texte: String) {
        let s = texte.hasPrefix("#") ? String(texte.dropFirst()) : texte
        guard s.count == 6, let n = UInt32(s, radix: 16) else { return nil }
        self.init(hex: n)
    }
}

enum Theme {
    static let fond = Color(hex: 0x1c1c1e)
    static let liste = Color(hex: 0x252527)
    static let fil = Color(hex: 0x000000)
    static let surface = Color(hex: 0x2c2c2e)
    static let champ = Color(hex: 0x333336)
    static let choisi = Color(hex: 0x3a3a3c)
    static let traitFin = Color(hex: 0x333335)
    static let texteFort = Color.white
    static let texte = Color(hex: 0xc7c7cc)
    static let textePale = Color(hex: 0x8e8e93)
    static let apercu = Color(hex: 0xa0a0a5)
    static let accent = Color(hex: 0x2f6bff)
    static let expediteur = Color(hex: 0xf08a6c)
    static let heureEnvoyee = Color.white.opacity(0.7)
    static let echecSurBulle = Color(hex: 0xffb4ae)
    static let danger = Color(hex: 0xff453a)
    static let succes = Color(hex: 0x30d158)
    static let averti = Color(hex: 0xd97706)
    static let avatarDefaut = Color(hex: 0x3b82f6)
}
