// Jasette iOS — une conversation : le fil des messages (images comprises) et
// l'envoi de texte. Rien ne part sans un geste de l'utilisateur ; ouvrir la
// conversation la marque lue (`read`), comme les autres clients.

import SwiftUI

struct FilView: View {
    @Environment(Depot.self) private var depot
    let convId: String
    @State private var texte = ""
    @FocusState private var focus: Bool

    private var conv: Conv? { depot.conv(convId) }
    private var messages: [Msg] { depot.messages[convId] ?? [] }

    var body: some View {
        VStack(spacing: 0) {
            ScrollViewReader { defile in
                ScrollView {
                    LazyVStack(spacing: 4) {
                        if depot.plusAnciens[convId] == true {
                            Button(t("Messages plus anciens")) { depot.plusVieux(convId) }
                                .font(.footnote)
                                .padding(.vertical, 8)
                        }
                        if depot.chargement == convId {
                            ProgressView().padding()
                        }
                        ForEach(Array(messages.enumerated()), id: \.element.id) { i, m in
                            if i == 0 || !memeJour(messages[i - 1].time, m.time) {
                                Text(jourFil(m.time))
                                    .font(.caption.weight(.semibold))
                                    .foregroundStyle(Theme.texteFort)
                                    .padding(.horizontal, 10).padding(.vertical, 4)
                                    .background(Theme.surface, in: Capsule())
                                    .padding(.vertical, 8)
                            }
                            Bulle(m: m,
                                  nom: (conv?.group ?? false) && !m.fromMe && (i == 0 || messages[i - 1].sender != m.sender)
                                      ? m.sender : nil)
                                .id(m.id)
                        }
                        Color.clear.frame(height: 4).id("fin")
                    }
                    .padding(.horizontal, 10)
                    .padding(.top, 6)
                }
                .defaultScrollAnchor(.bottom)
                .scrollDismissesKeyboard(.interactively)
                .onChange(of: messages.last?.id) { _, _ in
                    withAnimation { defile.scrollTo("fin", anchor: .bottom) }
                }
            }
            if conv?.readOnly == true {
                Text(t("Lecture seule")).font(.footnote).foregroundStyle(Theme.textePale).padding(10)
            } else {
                saisie
            }
        }
        .background(Theme.fil.ignoresSafeArea())
        .toolbarBackground(Theme.fond, for: .navigationBar)
        .toolbarBackground(.visible, for: .navigationBar)
        .navigationBarTitleDisplayMode(.inline)
        .toolbar {
            ToolbarItem(placement: .principal) {
                HStack(spacing: 8) {
                    if let c = conv { AvatarConv(conv: c, taille: 30, logo: false) }
                    VStack(alignment: .leading, spacing: 0) {
                        Text(conv?.name ?? "").font(.headline).foregroundStyle(Theme.texteFort).lineLimit(1)
                        if let c = conv {
                            Text(c.service.titre).font(.caption2).foregroundStyle(Theme.textePale)
                        }
                    }
                }
            }
        }
        .onAppear { depot.ouvrir(convId) }
        .onDisappear { depot.fermer(convId) }
        #if DEBUG
        .task {
            guard let t = Essai.valeur("essaiTexte") else { return }
            try? await Task.sleep(nanoseconds: 2_000_000_000)
            texte = t
            try? await Task.sleep(nanoseconds: 700_000_000)
            envoyer()
        }
        #endif
    }

    private var saisie: some View {
        HStack(alignment: .bottom, spacing: 8) {
            TextField(t("Message"), text: $texte, axis: .vertical)
                .lineLimit(1...6)
                .focused($focus)
                .padding(.horizontal, 14)
                .padding(.vertical, 9)
                .background(Theme.champ, in: RoundedRectangle(cornerRadius: 20))
                .foregroundStyle(Theme.texteFort)
            Button(action: envoyer) {
                Image(systemName: "arrow.up.circle.fill")
                    .font(.system(size: 34))
                    .foregroundStyle(vide ? Theme.textePale : Theme.accent)
            }
            .disabled(vide)
            .accessibilityLabel(t("Envoyer"))
        }
        .padding(.horizontal, 10)
        .padding(.vertical, 8)
        .background(Theme.fond)
    }

    private var vide: Bool { texte.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty }

    private func envoyer() {
        guard !vide else { return }
        depot.envoyer(convId, texte)
        texte = ""
    }

    private func memeJour(_ a: Int64, _ b: Int64) -> Bool {
        Calendar.current.isDate(Date(timeIntervalSince1970: Double(a) / 1000),
                                inSameDayAs: Date(timeIntervalSince1970: Double(b) / 1000))
    }
}

struct Bulle: View {
    @Environment(Depot.self) private var depot
    let m: Msg
    let nom: String?

    var body: some View {
        HStack {
            if m.fromMe { Spacer(minLength: 50) }
            VStack(alignment: m.fromMe ? .trailing : .leading, spacing: 3) {
                if let nom, !nom.isEmpty {
                    Text(nom).font(.caption.weight(.semibold)).foregroundStyle(Theme.expediteur).padding(.leading, 12)
                }
                ForEach(Array(m.attachments.enumerated()), id: \.offset) { _, p in
                    if p.estImage {
                        ImagePiece(p: p)
                    } else {
                        Text("📎 " + (p.name.isEmpty ? t("📎 Pièce jointe") : p.name))
                            .font(.subheadline)
                            .padding(10)
                            .foregroundStyle(m.fromMe ? .white : Theme.texteFort)
                            .background(m.fromMe ? Theme.accent : Theme.surface, in: RoundedRectangle(cornerRadius: 16))
                    }
                }
                if !m.text.isEmpty || m.attachments.isEmpty {
                    VStack(alignment: .trailing, spacing: 2) {
                        Text(m.text.isEmpty ? "…" : m.text)
                            .font(.body)
                            .foregroundStyle(m.fromMe ? Color.white : Theme.texteFort)
                            .textSelection(.enabled)
                        pied
                    }
                    .padding(.horizontal, 12)
                    .padding(.vertical, 7)
                    .background(m.fromMe ? Theme.accent : Theme.surface, in: RoundedRectangle(cornerRadius: 18))
                } else {
                    pied.padding(.horizontal, 6)
                }
                if !m.reactions.isEmpty {
                    Text(m.reactions.map(\.emoji).joined())
                        .font(.caption)
                        .padding(.horizontal, 6).padding(.vertical, 2)
                        .background(Theme.choisi, in: Capsule())
                }
            }
            if !m.fromMe { Spacer(minLength: 50) }
        }
    }

    private var pied: some View {
        HStack(spacing: 4) {
            if m.status == "ÉCHEC" {
                Text(t("Échec")).foregroundStyle(Theme.echecSurBulle)
            } else if m.local {
                Text(t("Envoi…"))
            }
            Text(heureBulle(m.time))
        }
        .font(.caption2)
        .foregroundStyle(m.fromMe ? Theme.heureEnvoyee : Theme.textePale)
    }
}

/// Une image d'un message : demandée au pont quand elle paraît (`download`), puis copiée du serveur.
struct ImagePiece: View {
    @Environment(Depot.self) private var depot
    let p: Piece

    private var ratio: CGFloat {
        p.w > 0 && p.h > 0 ? max(0.4, min(2.5, CGFloat(p.w) / CGFloat(p.h))) : 4.0 / 3.0
    }

    var body: some View {
        let chemin = depot.medias[p.mediaId]
        let echec = depot.echecsMedias.contains(p.mediaId)
        ImageDistante(chemin: chemin, cote: 600, remplir: true) {
            ZStack {
                Theme.surface
                if echec {
                    Button(t("Image non reçue · Réessayer")) { depot.reessayerMedia(p) }
                        .font(.caption)
                } else {
                    ProgressView()
                }
            }
        }
        .frame(width: 240, height: 240 / ratio)
        .clipShape(RoundedRectangle(cornerRadius: 16))
        .onAppear { depot.demanderMedia(p) }
    }
}
