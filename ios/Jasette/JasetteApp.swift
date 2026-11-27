// Jasette iOS — l'app : un client du Serveur Jasette (PROTOCOLE.md).
//
// iOS ne peut pas faire tourner les ponts (arrière-plan, sessions longues) :
// tout passe par le serveur de l'utilisateur, comme Android ou le bureau en
// mode client. L'app se branche au premier plan et se débranche en le quittant.

import SwiftUI

@main
struct JasetteApp: App {
    @State private var depot = Depot()
    @Environment(\.scenePhase) private var phase

    var body: some Scene {
        WindowGroup {
            Racine()
                .environment(depot)
                .preferredColorScheme(.dark)
                .tint(Theme.accent)
        }
        .onChange(of: phase, initial: true) { _, p in
            depot.passerPremierPlan(p == .active)
        }
    }
}

struct Racine: View {
    @Environment(Depot.self) private var depot

    var body: some View {
        // Lu ici : changer la langue redessine tout.
        let _ = Langue.partagee.choix
        Group {
            if depot.config == nil {
                JumelageView()
            } else {
                ListeView()
            }
        }
        .background(Theme.fond.ignoresSafeArea())
    }
}

/// Le numéro de version montré : « 1.0.1 alpha (306) ».
var versionAffichee: String {
    let i = Bundle.main.infoDictionary ?? [:]
    let v = i["CFBundleShortVersionString"] as? String ?? "?"
    let b = i["CFBundleVersion"] as? String ?? "?"
    return "\(v) alpha (\(b))"
}

#if DEBUG
/// Essais dans le simulateur seulement (jamais dans l'.ipa, construit en Release) :
/// arguments de lancement `-essaiAdresse 127.0.0.1:47900 -essaiCode 123456`
/// (jumelage sans geste), `-essaiArret verification` (s'arrêter à la
/// confirmation), `-essaiOuvrir <id>` (ouvrir une conversation),
/// `-essaiEcran reglages`, `-essaiTexte "…"` (envoyer ce texte dans la conversation ouverte).
enum Essai {
    static func valeur(_ cle: String) -> String? {
        let v = UserDefaults.standard.string(forKey: cle)
        return (v?.isEmpty ?? true) ? nil : v
    }
}
#endif
