// Jasette iOS — la boîte de réception : toutes les conversations de tous les
// services du serveur (épinglées en tête), filtres par service, état de la
// connexion, accès aux réglages.

import SwiftUI

struct ListeView: View {
    @Environment(Depot.self) private var depot
    @State private var chemin: [String] = []
    @State private var filtre: Depot.Filtre = .tout
    @State private var reglages = false

    var body: some View {
        NavigationStack(path: $chemin) {
            VStack(spacing: 0) {
                bandeauEtat
                filtres
                contenu
            }
            .background(Theme.liste.ignoresSafeArea())
            .navigationTitle("Jasette")
            .navigationBarTitleDisplayMode(.inline)
            .toolbarBackground(Theme.fond, for: .navigationBar)
            .toolbarBackground(.visible, for: .navigationBar)
            .toolbar {
                ToolbarItem(placement: .topBarTrailing) {
                    Button { reglages = true } label: { Image(systemName: "gearshape") }
                        .accessibilityLabel(t("Réglages"))
                }
            }
            .navigationDestination(for: String.self) { id in
                FilView(convId: id)
            }
        }
        .sheet(isPresented: $reglages) {
            ReglagesView()
        }
        .alert(depot.erreur ?? "", isPresented: Binding(get: { depot.erreur != nil }, set: { if !$0 { depot.erreur = nil } })) {
            Button(t("OK"), role: .cancel) {}
        }
        #if DEBUG
        .task { await essai() }
        #endif
    }

    @ViewBuilder private var bandeauEtat: some View {
        let texte: String? = switch depot.etatLien {
        case .branche: nil
        case .connexion, .aucun: t("Connexion…")
        case .reconnexion: t("Reconnexion…")
        }
        if let texte {
            VStack(spacing: 2) {
                HStack(spacing: 8) {
                    ProgressView().controlSize(.small)
                    Text(texte).font(.footnote).foregroundStyle(Theme.texte)
                }
                if let e = depot.erreurLien {
                    Text(e).font(.caption).foregroundStyle(Theme.danger).multilineTextAlignment(.center)
                }
            }
            .frame(maxWidth: .infinity)
            .padding(.vertical, 6)
            .background(Theme.surface)
        }
    }

    private var filtres: some View {
        let services = depot.servicesMontres.filter { s in depot.convs[s]?.isEmpty == false }
        return ScrollView(.horizontal, showsIndicators: false) {
            HStack(spacing: 8) {
                puce(t("Tout"), image: nil, .tout)
                ForEach(services) { s in puce(s.titre, image: s.logo, .service(s)) }
                if depot.aDesGroupes { puce(t("Groupes"), image: nil, .groupes) }
            }
            .padding(.horizontal, 12)
            .padding(.vertical, 8)
        }
        .background(Theme.fond)
    }

    private func puce(_ titre: String, image: String?, _ f: Depot.Filtre) -> some View {
        let choisi = filtre == f
        return Button {
            filtre = f
        } label: {
            HStack(spacing: 6) {
                if let image {
                    Image(image).resizable().frame(width: 18, height: 18).clipShape(RoundedRectangle(cornerRadius: 5))
                }
                Text(titre).font(.subheadline.weight(choisi ? .semibold : .regular))
            }
            .padding(.horizontal, 12)
            .padding(.vertical, 7)
            .foregroundStyle(choisi ? Color.white : Theme.texte)
            .background(choisi ? Theme.accent : Theme.surface, in: Capsule())
        }
        .buttonStyle(.plain)
    }

    @ViewBuilder private var contenu: some View {
        let l = depot.liste(filtre)
        if l.isEmpty {
            VStack(spacing: 10) {
                Spacer()
                if depot.etatLien == .branche && depot.services.isEmpty {
                    Text(t("Aucun service n'est connecté sur ton serveur Jasette. Connecte-les sur le serveur."))
                } else if depot.etatLien != .branche || !depot.marquesRecues {
                    ProgressView()
                    Text(t("Chargement des conversations…"))
                } else {
                    Text(t("Aucune conversation"))
                }
                Spacer()
            }
            .font(.callout)
            .foregroundStyle(Theme.textePale)
            .multilineTextAlignment(.center)
            .padding(30)
            .frame(maxWidth: .infinity)
        } else {
            List(l) { c in
                NavigationLink(value: c.id) {
                    LigneConv(conv: c, epinglee: depot.estEpinglee(c.id))
                }
                .listRowBackground(Theme.liste)
                .listRowInsets(EdgeInsets(top: 8, leading: 14, bottom: 8, trailing: 14))
                .listRowSeparatorTint(Theme.traitFin)
            }
            .listStyle(.plain)
            .scrollContentBackground(.hidden)
        }
    }

    #if DEBUG
    private func essai() async {
        if Essai.valeur("essaiEcran") == "reglages" {
            try? await Task.sleep(nanoseconds: 2_500_000_000)
            reglages = true
        }
        guard let id = Essai.valeur("essaiOuvrir") else { return }
        for _ in 0..<40 where depot.conv(id) == nil {
            try? await Task.sleep(nanoseconds: 250_000_000)
        }
        try? await Task.sleep(nanoseconds: 1_000_000_000)
        chemin = [id]
    }
    #endif
}

struct LigneConv: View {
    let conv: Conv
    let epinglee: Bool

    var body: some View {
        HStack(spacing: 12) {
            AvatarConv(conv: conv)
            VStack(alignment: .leading, spacing: 3) {
                HStack(alignment: .firstTextBaseline) {
                    Text(conv.name.isEmpty ? "…" : conv.name)
                        .font(.body.weight(conv.unread ? .bold : .semibold))
                        .foregroundStyle(Theme.texteFort)
                        .lineLimit(1)
                    Spacer(minLength: 6)
                    if epinglee {
                        Image(systemName: "pin.fill").font(.caption2).foregroundStyle(Theme.textePale)
                    }
                    Text(heureListe(conv.time))
                        .font(.caption)
                        .foregroundStyle(conv.unread ? Theme.accent : Theme.textePale)
                }
                HStack(alignment: .center) {
                    Text((conv.fromMe ? "\(t("Moi")) : " : "") + conv.preview)
                        .font(.subheadline.weight(conv.unread ? .semibold : .regular))
                        .foregroundStyle(conv.unread ? Theme.texte : Theme.apercu)
                        .lineLimit(2)
                    Spacer(minLength: 6)
                    if conv.unread || conv.mentions > 0 {
                        Circle().fill(Theme.accent).frame(width: 10, height: 10)
                    }
                }
            }
        }
        .padding(.vertical, 2)
    }
}
