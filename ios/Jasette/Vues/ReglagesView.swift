// Jasette iOS — les réglages minimaux : le serveur, ses services, la langue,
// « À propos » et « Déconnecter ».

import SwiftUI

struct ReglagesView: View {
    @Environment(Depot.self) private var depot
    @Environment(\.dismiss) private var fermer
    @State private var confirmer = false

    var body: some View {
        @Bindable var langue = Langue.partagee
        NavigationStack {
            Form {
                Section(t("Votre serveur")) {
                    ligne(t("Adresse"), depot.config?.adresse ?? "")
                    ligne(t("Cet appareil"), depot.config?.nom ?? "")
                    HStack {
                        Text(t("État"))
                        Spacer()
                        Circle().fill(depot.etatLien == .branche ? Theme.succes : Theme.averti).frame(width: 8, height: 8)
                        Text(etatLien).foregroundStyle(Theme.textePale)
                    }
                    if let e = depot.erreurLien, depot.etatLien != .branche {
                        Text(e).font(.footnote).foregroundStyle(Theme.danger)
                    }
                }

                if !depot.services.isEmpty {
                    Section {
                        ForEach(depot.servicesMontres) { s in
                            HStack(spacing: 10) {
                                Image(s.logo).resizable().frame(width: 26, height: 26)
                                    .clipShape(RoundedRectangle(cornerRadius: 7))
                                VStack(alignment: .leading, spacing: 1) {
                                    Text(s.titre)
                                    if let c = depot.comptes[s.cle] {
                                        Text(c).font(.caption).foregroundStyle(Theme.textePale)
                                    }
                                }
                                Spacer()
                                Text(etatService(depot.etats[s.cle])).font(.footnote).foregroundStyle(Theme.textePale)
                            }
                        }
                    } header: {
                        Text(t("Services"))
                    } footer: {
                        Text(t("Les services se connectent et se déconnectent sur le serveur."))
                    }
                }

                Section(t("Langue")) {
                    Picker(t("Langue"), selection: $langue.choix) {
                        Text(t("Automatique")).tag("auto")
                        Text("Français").tag("fr")
                        Text("English").tag("en")
                    }
                    .pickerStyle(.segmented)
                }

                Section(t("À propos")) {
                    BandeauLimite()
                        .listRowInsets(EdgeInsets(top: 8, leading: 12, bottom: 8, trailing: 12))
                    ligne(t("Version"), versionAffichee)
                    Text(t("Les notifications push et la connexion des services viendront dans une prochaine version. Jasette se met à jour quand l'app est ouverte."))
                        .font(.footnote).foregroundStyle(Theme.textePale)
                }

                Section {
                    Button(t("Déconnecter"), role: .destructive) { confirmer = true }
                }
            }
            .scrollContentBackground(.hidden)
            .background(Theme.fond.ignoresSafeArea())
            .navigationTitle(t("Réglages"))
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .confirmationAction) {
                    Button(t("Fermer")) { fermer() }
                }
            }
            .confirmationDialog(t("Ne plus utiliser ce serveur ?"), isPresented: $confirmer, titleVisibility: .visible) {
                Button(t("Déconnecter"), role: .destructive) {
                    fermer()
                    depot.oublierServeur()
                }
                Button(t("Annuler"), role: .cancel) {}
            } message: {
                Text(t("Cet appareil se débranchera du serveur Jasette. Tes services restent connectés sur le serveur ; pour revenir, il faudra jumeler de nouveau avec un nouveau code."))
            }
        }
        .preferredColorScheme(.dark)
    }

    private func ligne(_ titre: String, _ valeur: String) -> some View {
        HStack {
            Text(titre)
            Spacer()
            Text(valeur).foregroundStyle(Theme.textePale).lineLimit(1).truncationMode(.middle)
        }
    }

    private var etatLien: String {
        switch depot.etatLien {
        case .branche: return t("Connecté à ton serveur Jasette")
        case .connexion: return t("Connexion…")
        case .reconnexion: return t("Reconnexion…")
        case .aucun: return t("Hors ligne")
        }
    }

    private func etatService(_ e: String?) -> String {
        switch e {
        case "ready": return t("prêt")
        case "unpaired": return t("non connecté")
        case "pairing": return t("association")
        case "reconnecting": return t("reconnexion")
        case "phone_offline": return t("téléphone hors ligne")
        case nil: return "…"
        default: return t("connexion")
        }
    }
}
