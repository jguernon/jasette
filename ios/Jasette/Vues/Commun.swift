// Jasette iOS — les petites pièces communes : avatar, image du serveur,
// bandeau de version limitée, heures.

import SwiftUI
import UIKit

/// Le bandeau « Version limitée (alpha) » (écran de jumelage, À propos).
struct BandeauLimite: View {
    var body: some View {
        HStack(alignment: .top, spacing: 8) {
            Image(systemName: "info.circle.fill").foregroundStyle(Theme.averti)
            Text(t("Version limitée (alpha) : client d'un Serveur Jasette, sans notifications push ni connexion des services."))
                .font(.footnote)
                .foregroundStyle(Theme.texte)
                .fixedSize(horizontal: false, vertical: true)
        }
        .padding(10)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(Theme.averti.opacity(0.14), in: RoundedRectangle(cornerRadius: 10))
        .overlay(RoundedRectangle(cornerRadius: 10).stroke(Theme.averti.opacity(0.45), lineWidth: 1))
    }
}

/// Les images déjà décodées (petites copies), en mémoire.
final class CacheImages: @unchecked Sendable {
    static let partage = CacheImages()
    private let c = NSCache<NSString, UIImage>()

    func image(_ url: URL, cote: CGFloat) -> UIImage? {
        let cle = "\(url.path)#\(Int(cote))" as NSString
        if let i = c.object(forKey: cle) { return i }
        guard let brute = UIImage(contentsOfFile: url.path) else { return nil }
        let echelle = UIScreen.main.scale
        let max = cote * echelle
        var i = brute
        if brute.size.width * brute.scale > max || brute.size.height * brute.scale > max {
            let r = min(max / (brute.size.width * brute.scale), max / (brute.size.height * brute.scale))
            let taille = CGSize(width: brute.size.width * brute.scale * r, height: brute.size.height * brute.scale * r)
            i = brute.preparingThumbnail(of: taille) ?? brute
        }
        c.setObject(i, forKey: cle)
        return i
    }
}

/// Une image du serveur (chemin absolu du serveur), copiée dans le cache local.
struct ImageDistante<Repli: View>: View {
    @Environment(Depot.self) private var depot
    let chemin: String?
    var cote: CGFloat = 96
    var remplir = true
    @ViewBuilder var repli: () -> Repli
    @State private var image: UIImage?

    var body: some View {
        Group {
            if let image {
                Image(uiImage: image).resizable().aspectRatio(contentMode: remplir ? .fill : .fit)
            } else {
                repli()
            }
        }
        .task(id: chemin) {
            image = nil
            guard let chemin, let url = await depot.fichierLocal(chemin) else { return }
            let c = cote
            image = await Task.detached { CacheImages.partage.image(url, cote: c) }.value
        }
    }
}

/// L'avatar d'une conversation : sa photo, sinon ses initiales ; le logo du service en coin.
struct AvatarConv: View {
    @Environment(Depot.self) private var depot
    let conv: Conv
    var taille: CGFloat = 52
    var logo = true

    var body: some View {
        ImageDistante(chemin: depot.avatars[conv.id], cote: taille) {
            ZStack {
                Circle().fill(Color(texte: conv.color) ?? Theme.avatarDefaut)
                if conv.group && initiales(conv.name).isEmpty {
                    Image(systemName: "person.2.fill").foregroundStyle(.white).font(.system(size: taille * 0.38))
                } else {
                    Text(initiales(conv.name))
                        .font(.system(size: taille * 0.38, weight: .semibold))
                        .foregroundStyle(.white)
                }
            }
        }
        .frame(width: taille, height: taille)
        .clipShape(Circle())
        .overlay(alignment: .bottomTrailing) {
            if logo {
                Image(conv.service.logo)
                    .resizable()
                    .frame(width: taille * 0.38, height: taille * 0.38)
                    .clipShape(RoundedRectangle(cornerRadius: taille * 0.11))
                    .overlay(RoundedRectangle(cornerRadius: taille * 0.11).stroke(Theme.liste, lineWidth: 2))
                    .offset(x: 3, y: 3)
            }
        }
    }
}

func initiales(_ nom: String) -> String {
    let mots = nom.split(whereSeparator: { $0.isWhitespace }).filter { $0.first?.isLetter == true }
    return mots.prefix(2).compactMap { $0.first.map { String($0).uppercased() } }.joined()
}

/// L'heure d'une conversation dans la liste : « 14:05 », « Hier », « lun. », « 12/03/25 ».
func heureListe(_ ms: Int64) -> String {
    guard ms > 0 else { return "" }
    let d = Date(timeIntervalSince1970: Double(ms) / 1000)
    let cal = Calendar.current
    let f = DateFormatter()
    f.locale = Locale(identifier: Langue.partagee.anglais ? "en_CA" : "fr_CA")
    if cal.isDateInToday(d) {
        f.dateStyle = .none
        f.timeStyle = .short
        return f.string(from: d)
    }
    if cal.isDateInYesterday(d) { return t("Hier") }
    if let jours = cal.dateComponents([.day], from: d, to: Date()).day, jours < 7 {
        f.setLocalizedDateFormatFromTemplate("EEE")
        return f.string(from: d)
    }
    f.dateStyle = .short
    f.timeStyle = .none
    return f.string(from: d)
}

func heureBulle(_ ms: Int64) -> String {
    let f = DateFormatter()
    f.locale = Locale(identifier: Langue.partagee.anglais ? "en_CA" : "fr_CA")
    f.dateStyle = .none
    f.timeStyle = .short
    return f.string(from: Date(timeIntervalSince1970: Double(ms) / 1000))
}

func jourFil(_ ms: Int64) -> String {
    let d = Date(timeIntervalSince1970: Double(ms) / 1000)
    if Calendar.current.isDateInToday(d) { return t("Aujourd'hui") }
    if Calendar.current.isDateInYesterday(d) { return t("Hier") }
    let f = DateFormatter()
    f.locale = Locale(identifier: Langue.partagee.anglais ? "en_CA" : "fr_CA")
    f.setLocalizedDateFormatFromTemplate("EEEEdMMMMy")
    return f.string(from: d)
}
