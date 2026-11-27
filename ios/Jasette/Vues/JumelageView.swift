// Jasette iOS — le jumelage avec un serveur (PROTOCOLE.md §3.3) : adresse et
// code, puis le code de vérification à comparer avant d'envoyer quoi que ce
// soit qui dépende du code.

import SwiftUI
import UIKit

struct JumelageView: View {
    @Environment(Depot.self) private var depot
    @State private var adresse = ""
    @State private var code = ""
    @State private var nom = UIDevice.current.name
    @State private var verification: Jumelage.Verification?
    @State private var occupe = false
    @State private var erreur: String?

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 18) {
                HStack(spacing: 14) {
                    Image("Logo").resizable().frame(width: 64, height: 64)
                        .clipShape(RoundedRectangle(cornerRadius: 15))
                    VStack(alignment: .leading, spacing: 2) {
                        Text("Jasette").font(.largeTitle.bold()).foregroundStyle(Theme.texteFort)
                        Text(t("Utiliser mon serveur Jasette")).font(.subheadline).foregroundStyle(Theme.textePale)
                    }
                }
                .padding(.top, 24)

                BandeauLimite()

                if let v = verification {
                    confirmation(v)
                } else {
                    formulaire
                }

                if let erreur {
                    Text(erreur).font(.callout).foregroundStyle(Theme.danger)
                        .fixedSize(horizontal: false, vertical: true)
                }
                Text(versionAffichee).font(.caption2).foregroundStyle(Theme.textePale)
            }
            .padding(.horizontal, 20)
            .padding(.bottom, 30)
        }
        .scrollDismissesKeyboard(.interactively)
        .background(Theme.fond.ignoresSafeArea())
        #if DEBUG
        .task { await essai() }
        #endif
    }

    private var formulaire: some View {
        VStack(alignment: .leading, spacing: 14) {
            Text(t("Sur un appareil déjà jumelé : Réglages > Serveur Jasette > Jumeler un appareil (ou, sur le serveur : jasette-serveur --jumeler). Entre ici son adresse et le code affiché."))
                .font(.callout).foregroundStyle(Theme.texte)
                .fixedSize(horizontal: false, vertical: true)
            champ(t("Adresse du serveur"), texte: $adresse, exemple: "100.64.0.1 · serveur.tail1234.ts.net:47800")
                .keyboardType(.URL)
            champ(t("Code de jumelage"), texte: $code, exemple: "123 456")
                .keyboardType(.numberPad)
            champ(t("Nom de cet appareil"), texte: $nom, exemple: "iPhone")
            Button {
                Task { await verifier() }
            } label: {
                HStack {
                    if occupe { ProgressView().tint(.white) }
                    Text(t("Continuer")).bold()
                }
                .frame(maxWidth: .infinity, minHeight: 48)
            }
            .buttonStyle(.borderedProminent)
            .disabled(occupe || adresse.trimmingCharacters(in: .whitespaces).isEmpty || code.filter(\.isNumber).count != 6)
        }
    }

    private func champ(_ titre: String, texte: Binding<String>, exemple: String) -> some View {
        VStack(alignment: .leading, spacing: 6) {
            Text(titre).font(.footnote.weight(.semibold)).foregroundStyle(Theme.textePale)
            TextField(exemple, text: texte)
                .textInputAutocapitalization(.never)
                .autocorrectionDisabled()
                .padding(12)
                .background(Theme.champ, in: RoundedRectangle(cornerRadius: 10))
                .foregroundStyle(Theme.texteFort)
        }
    }

    private func confirmation(_ v: Jumelage.Verification) -> some View {
        VStack(alignment: .leading, spacing: 14) {
            Text("\(t("Le serveur")) \(v.adresse) \(t("affiche-t-il ce code de vérification ?"))")
                .font(.body).foregroundStyle(Theme.texte)
                .fixedSize(horizontal: false, vertical: true)
            Text(groupe(v.codeVerification))
                .font(.system(size: 44, weight: .bold, design: .monospaced))
                .foregroundStyle(Theme.texteFort)
                .frame(maxWidth: .infinity)
                .padding(.vertical, 14)
                .background(Theme.surface, in: RoundedRectangle(cornerRadius: 12))
            Button {
                Task { await jumeler(v) }
            } label: {
                HStack {
                    if occupe { ProgressView().tint(.white) }
                    Text(t("Oui, jumeler")).bold()
                }
                .frame(maxWidth: .infinity, minHeight: 48)
            }
            .buttonStyle(.borderedProminent)
            .disabled(occupe)
            Button(role: .cancel) {
                verification = nil
                erreur = t("Jumelage annulé : les codes ne correspondaient pas.")
            } label: {
                Text(t("Non, annuler")).frame(maxWidth: .infinity, minHeight: 44)
            }
            .buttonStyle(.bordered)
            .disabled(occupe)
        }
    }

    private func groupe(_ c: String) -> String {
        c.count == 6 ? "\(c.prefix(3)) \(c.suffix(3))" : c
    }

    private func verifier() async {
        occupe = true
        erreur = nil
        defer { occupe = false }
        do {
            verification = try await Jumelage.verifier(adresse: adresse, code: code)
        } catch {
            erreur = error.localizedDescription
        }
    }

    private func jumeler(_ v: Jumelage.Verification) async {
        occupe = true
        erreur = nil
        defer { occupe = false }
        do {
            let c = try await Jumelage.jumeler(v, nom: nom.isEmpty ? "iPhone" : nom)
            verification = nil
            depot.jumele(c)
        } catch {
            verification = nil
            erreur = error.localizedDescription
        }
    }

    #if DEBUG
    private func essai() async {
        guard let a = Essai.valeur("essaiAdresse"), let c = Essai.valeur("essaiCode") else { return }
        adresse = a
        code = c
        if let n = Essai.valeur("essaiNom") { nom = n }
        try? await Task.sleep(nanoseconds: 800_000_000)
        await verifier()
        guard let v = verification, Essai.valeur("essaiArret") != "verification" else { return }
        try? await Task.sleep(nanoseconds: 800_000_000)
        await jumeler(v)
    }
    #endif
}
