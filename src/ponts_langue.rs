//! Les textes que les ponts (Go) écrivent eux-mêmes, en français : message
//! retiré, pièce jointe, message de service (« Alice a ajouté Bob »)…
//!
//! Les ponts ne connaissent pas la langue de l'interface, et le serveur garde
//! leurs messages en cache tels quels : c'est donc le client qui les traduit,
//! au moment de les montrer (bulle, aperçu de la liste, notification,
//! recherche), en reconnaissant la chaîne exacte, son début (« ↗️ Message
//! transféré … ») ou sa forme (« {a} a ajouté {b} »). Les vieux ponts et les
//! vieux caches se traduisent donc aussi.
//!
//! Les ponts récents ajoutent aux messages de service fixes un champ neutre,
//! `system` (PROTOCOLE.md, §5.14) : il passe avant le texte.
//!
//! Les mêmes tables, mot pour mot, sont dans Android (Langue.kt, `traduirePont`) :
//! le test `memes_tables_qu_android` le vérifie.

use std::borrow::Cow;

/// Le champ `system` d'un message : son texte (français, comme celui des ponts).
pub fn texte_systeme(code: &str) -> Option<&'static str> {
    SYSTEME.iter().find(|(c, _)| *c == code).map(|(_, fr)| *fr)
}

/// Le texte d'un message, en français : celui du champ `system` s'il est
/// connu, sinon `text`.
pub fn francais<'a>(text: &'a str, system: &str) -> &'a str {
    texte_systeme(system).unwrap_or(text)
}

/// Les codes de `system` (PROTOCOLE.md, §5.14) et leur texte.
pub const SYSTEME: &[(&str, &str)] = &[
    ("deleted", "🚫 Message retiré"),
    ("call", "📞 Appel"),
    ("history_cleared", "Historique effacé"),
    ("safety_number_changed", "🔐 Le numéro de sécurité de cette personne a changé"),
    ("decrypt_failed", "⚠ Un message n'a pas pu être déchiffré : vérifie dans Signal sur le téléphone"),
];

/// Une ligne entière.
pub const EXACTS: &[(&str, &str)] = &[
    ("🚫 Message retiré", "🚫 Message removed"),
    ("Message retiré", "Message removed"),
    ("🚫 Message supprimé", "🚫 Message deleted"),
    ("Message supprimé", "Message deleted"),
    ("📷 Photo", "📷 Photo"),
    ("🎬 Vidéo", "🎬 Video"),
    ("🎤 Message vocal", "🎤 Voice message"),
    ("📎 Pièce jointe", "📎 Attachment"),
    ("📎 Fichier", "📎 File"),
    ("📷 Photo (expirée)", "📷 Photo (expired)"),
    ("📎 Fichier (expiré)", "📎 File (expired)"),
    ("Autocollant", "Sticker"),
    ("(autocollant)", "(sticker)"),
    ("pièce jointe", "attachment"),
    ("message vocal", "voice message"),
    ("autocollant", "sticker"),
    ("📍 Position", "📍 Location"),
    ("📍 Position en direct", "📍 Live location"),
    ("📞 Appel", "📞 Call"),
    ("Historique effacé", "History cleared"),
    ("Le groupe est devenu un supergroupe", "The group became a supergroup"),
    ("🔒 Conversation chiffrée", "🔒 Encrypted conversation"),
    ("🔒 Message chiffré pas encore lisible (il arrivera peut-être plus tard)", "🔒 Encrypted message not readable yet (it may arrive later)"),
    ("🔒 Message chiffré dans un format que Jasette ne sait pas encore lire : ouvre X pour le voir.", "🔒 Encrypted message in a format Jasette can't read yet: open X to see it."),
    ("🔒 Message chiffré illisible (clé du fil manquante).", "🔒 Unreadable encrypted message (missing thread key)."),
    ("📍 Partage de position en direct (visible dans Messenger)", "📍 Live location sharing (visible in Messenger)"),
    ("👤 Fiche de contact (visible dans Messenger)", "👤 Contact card (visible in Messenger)"),
    ("(contenu partagé : ouvre Messenger)", "(shared content: open Messenger)"),
    ("⤴ a remonté un message", "⤴ bumped a message"),
    ("(photo éphémère : ouvre Messenger)", "(disappearing photo: open Messenger)"),
    ("📷 Photo éphémère : ouvre Instagram pour la voir", "📷 Disappearing photo: open Instagram to see it"),
    ("🎬 Vidéo éphémère : ouvre Instagram pour la voir", "🎬 Disappearing video: open Instagram to see it"),
    ("(pièce jointe non prise en charge)", "(unsupported attachment)"),
    ("📞 Appel Signal : réponds sur le téléphone", "📞 Signal call: answer on the phone"),
    ("📞 Appel de groupe Signal", "📞 Signal group call"),
    ("🔐 Le numéro de sécurité de cette personne a changé", "🔐 This person's safety number changed"),
    ("👁 Média à vue unique : ouvre Signal sur le téléphone pour le voir", "👁 View-once media: open Signal on the phone to see it"),
    ("💸 Paiement Signal : ouvre Signal sur le téléphone", "💸 Signal payment: open Signal on the phone"),
    ("🎁 Badge cadeau : ouvre Signal sur le téléphone", "🎁 Gift badge: open Signal on the phone"),
    ("⏱ Messages éphémères désactivés", "⏱ Disappearing messages off"),
    ("⚠ Un message n'a pas pu être déchiffré : vérifie dans Signal sur le téléphone", "⚠ A message couldn't be decrypted: check Signal on the phone"),
    ("⏳ Message en attente (ouvre WhatsApp sur le téléphone s'il n'arrive pas)", "⏳ Waiting for this message (open WhatsApp on the phone if it doesn't arrive)"),
    ("👁 Message à vue unique : ouvre WhatsApp sur le téléphone", "👁 View-once message: open WhatsApp on the phone"),
    ("(vote dans Signal)", "(vote in Signal)"),
    ("(vote sur le téléphone)", "(vote on the phone)"),
    ("Demande de message", "Message request"),
    // Noms donnés par les ponts (conversation sans nom, compte disparu…).
    ("Messages enregistrés", "Saved Messages"),
    ("Note à moi-même", "Note to Self"),
    ("Compte supprimé", "Deleted Account"),
    ("Groupe", "Group"),
    ("Conversation Messenger", "Messenger conversation"),
    ("Conversation Instagram", "Instagram conversation"),
    ("Conversation WhatsApp", "WhatsApp conversation"),
    ("Conversation Signal", "Signal conversation"),
    ("Conversation Telegram", "Telegram conversation"),
    ("Conversation X", "X conversation"),
    ("Conversation LinkedIn", "LinkedIn conversation"),
];

/// Le début d'une ligne (la suite est gardée, traduite si elle est connue).
pub const DEBUTS: &[(&str, &str)] = &[
    ("↗️ Message transféré", "↗️ Forwarded message"),
    ("↗ Transféré de ", "↗ Forwarded from "),
    ("📊 Sondage : ", "📊 Poll: "),
    ("⏱ Messages éphémères : ", "⏱ Disappearing messages: "),
    ("👤 Contact : ", "👤 Contact: "),
    ("👥 Contacts : ", "👥 Contacts: "),
    ("Demande de message · ", "Message request · "),
    ("↪ Vous : ", "↪ You: "),
];

/// La fin d'une ligne (un aperçu met tout le message sur une ligne).
pub const FINS: &[(&str, &str)] = &[
    ("(vote dans Signal)", "(vote in Signal)"),
    ("(vote sur le téléphone)", "(vote on the phone)"),
];

/// Des formes avec des noms : {a} et {b} sont gardés (« Quelqu'un » traduit).
/// Celles-ci ont une fin fixe : reconnues partout (bulle, aperçu).
pub const FORMES: &[(&str, &str)] = &[
    ("📞 {a} a lancé un appel", "📞 {a} started a call"),
    ("{a} a quitté le groupe", "{a} left the group"),
    ("{a} a rejoint le groupe", "{a} joined the group"),
    ("{a} a changé l'image du groupe", "{a} changed the group image"),
    ("{a} a changé la photo du groupe", "{a} changed the group photo"),
    ("{a} a retiré la photo du groupe", "{a} removed the group photo"),
    ("{a} a épinglé un message", "{a} pinned a message"),
    ("{a} est maintenant sur Telegram", "{a} is now on Telegram"),
    ("{a} a créé le groupe « {b} »", "{a} created the group “{b}”"),
    ("{a} a renommé le groupe « {b} »", "{a} renamed the group “{b}”"),
    ("Canal « {a} » créé", "Channel “{a}” created"),
    ("👥 Invitation au groupe « {a} »", "👥 Group invite “{a}”"),
    ("(message non pris en charge : ouvre {a})", "(unsupported message: open {a})"),
];

/// Des formes à fin libre : seulement pour un message de service (`kind`
/// « admin »), pour ne pas toucher un message ordinaire qui leur ressemble.
pub const FORMES_SERVICE: &[(&str, &str)] = &[
    ("{a} a renommé le groupe : {b}", "{a} renamed the group: {b}"),
    ("{a} a ajouté {b}", "{a} added {b}"),
    ("{a} a retiré {b}", "{a} removed {b}"),
];

/// Le texte dans la langue de l'interface (voir [traduire]).
pub fn affiche(texte: &str, service: bool) -> Cow<'_, str> {
    traduire(texte, service, crate::anglais())
}

/// Traduit en anglais (si `anglais`) les textes connus des ponts, ligne par
/// ligne ; le reste est gardé tel quel. `service` : un message de service
/// (`kind` « admin »), où les formes à fin libre sont aussi reconnues.
pub fn traduire(texte: &str, service: bool, anglais: bool) -> Cow<'_, str> {
    if !anglais || texte.is_empty() {
        return Cow::Borrowed(texte);
    }
    let seule = !texte.contains('\n');
    let mut change = false;
    let lignes: Vec<Cow<str>> = texte
        .split('\n')
        .map(|l| {
            let t = ligne(l, service && seule);
            change |= matches!(t, Cow::Owned(_));
            t
        })
        .collect();
    if !change {
        return Cow::Borrowed(texte);
    }
    Cow::Owned(lignes.join("\n"))
}

fn ligne(l: &str, service: bool) -> Cow<'_, str> {
    if let Some((_, en)) = EXACTS.iter().find(|(fr, _)| *fr == l) {
        return Cow::Owned(en.to_string());
    }
    for (fr, en) in DEBUTS {
        if let Some(reste) = l.strip_prefix(fr) {
            // « ↗️ Message transféré » doit finir là, ou avant une espace.
            let limite = fr.ends_with(' ') || reste.is_empty() || reste.starts_with(char::is_whitespace);
            if limite {
                return Cow::Owned(format!("{en}{}", ligne(reste, false)));
            }
        }
    }
    // La citation d'une réponse : « ↪ Alice : 📷 Photo ».
    if let Some(reste) = l.strip_prefix("↪ ") {
        if let Some((qui, cite)) = reste.split_once(" : ")
            && let Cow::Owned(c) = ligne(cite, false)
        {
            return Cow::Owned(format!("↪ {qui}: {c}"));
        }
        if let Cow::Owned(c) = ligne(reste, false) {
            return Cow::Owned(format!("↪ {c}"));
        }
        return Cow::Borrowed(l);
    }
    let formes = FORMES.iter().chain(if service { FORMES_SERVICE } else { &[] });
    for (fr, en) in formes {
        if let Some(t) = forme(l, fr, en) {
            return Cow::Owned(t);
        }
    }
    for (fr, en) in FINS {
        if let Some(debut) = l.strip_suffix(fr) {
            return Cow::Owned(format!("{debut}{en}"));
        }
    }
    Cow::Borrowed(l)
}

/// `l` a-t-elle la forme `fr` (« {a} a ajouté {b} ») ? Sa traduction selon `en`.
fn forme(l: &str, fr: &str, en: &str) -> Option<String> {
    // Les morceaux fixes, et le nom de chaque trou, dans l'ordre.
    let mut fixes = Vec::new();
    let mut trous = Vec::new();
    let mut reste = fr;
    while let Some(i) = reste.find('{') {
        fixes.push(&reste[..i]);
        trous.push(&reste[i..i + 3]);
        reste = &reste[i + 3..];
    }
    fixes.push(reste);
    let mut s = l.strip_prefix(fixes[0])?;
    let mut valeurs = Vec::new();
    for (k, fixe) in fixes[1..].iter().enumerate() {
        let v = if k + 2 == fixes.len() {
            s.strip_suffix(fixe)?
        } else {
            let i = s.find(fixe)?;
            let v = &s[..i];
            s = &s[i + fixe.len()..];
            v
        };
        if v.trim().is_empty() {
            return None;
        }
        valeurs.push(v);
    }
    let mut t = en.to_string();
    for (trou, v) in trous.iter().zip(valeurs) {
        t = t.replace(trou, &noms(v));
    }
    Some(t)
}

/// « Quelqu'un », dans une liste de noms (« Alice, quelqu'un »).
fn noms(v: &str) -> String {
    v.split(", ")
        .map(|n| match n {
            "Quelqu'un" => "Someone",
            "quelqu'un" => "someone",
            n => n,
        })
        .collect::<Vec<_>>()
        .join(", ")
}

#[cfg(test)]
mod tests {
    use super::*;

    fn en(s: &str) -> String {
        traduire(s, false, true).into_owned()
    }

    #[test]
    fn francais_inchange() {
        assert!(matches!(traduire("🚫 Message retiré", false, false), Cow::Borrowed("🚫 Message retiré")));
    }

    #[test]
    fn exacts() {
        assert_eq!(en("🚫 Message retiré"), "🚫 Message removed");
        assert_eq!(en("Message retiré"), "Message removed");
        assert_eq!(en("🚫 Message supprimé"), "🚫 Message deleted");
        assert_eq!(en("🔒 Conversation chiffrée"), "🔒 Encrypted conversation");
        assert_eq!(en("📎 Pièce jointe"), "📎 Attachment");
        assert_eq!(en("🎬 Vidéo"), "🎬 Video");
        // Un texte ordinaire ne bouge pas (et n'est pas copié).
        assert!(matches!(traduire("Salut !", false, true), Cow::Borrowed("Salut !")));
        assert_eq!(en("Message retiré hier"), "Message retiré hier");
    }

    #[test]
    fn debuts() {
        assert_eq!(en("↗️ Message transféré"), "↗️ Forwarded message");
        assert_eq!(en("↗️ Message transféré\nallo"), "↗️ Forwarded message\nallo");
        assert_eq!(en("↗️ Message transféré allo"), "↗️ Forwarded message allo");
        assert_eq!(en("↗️ Message transférés"), "↗️ Message transférés");
        assert_eq!(en("↗ Transféré de Bob\nsalut"), "↗ Forwarded from Bob\nsalut");
        assert_eq!(en("📊 Sondage : Pizza ?\n• Oui\n• Non\n(vote dans Signal)"), "📊 Poll: Pizza ?\n• Oui\n• Non\n(vote in Signal)");
        assert_eq!(en("⏱ Messages éphémères : 1h0m0s"), "⏱ Disappearing messages: 1h0m0s");
        assert_eq!(en("Demande de message · Message retiré"), "Message request · Message removed");
        assert_eq!(en("Demande de message"), "Message request");
    }

    #[test]
    fn aperçu_sur_une_ligne() {
        assert_eq!(en("📊 Pizza ? • Oui • Non (vote sur le téléphone)"), "📊 Pizza ? • Oui • Non (vote on the phone)");
    }

    #[test]
    fn citations() {
        assert_eq!(en("↪ Message supprimé\nOK"), "↪ Message deleted\nOK");
        assert_eq!(en("↪ Vous : 📷 Photo\nbelle"), "↪ You: 📷 Photo\nbelle");
        assert_eq!(en("↪ Vous : on se voit ?\noui"), "↪ You: on se voit ?\noui");
        assert_eq!(en("↪ Alice : 🎤 Message vocal\nreçu"), "↪ Alice: 🎤 Voice message\nreçu");
        assert_eq!(en("↪ Alice : salut\nyo"), "↪ Alice : salut\nyo");
    }

    #[test]
    fn formes() {
        assert_eq!(en("Alice a quitté le groupe"), "Alice left the group");
        assert_eq!(en("Quelqu'un a épinglé un message"), "Someone pinned a message");
        assert_eq!(en("📞 Bob a lancé un appel"), "📞 Bob started a call");
        assert_eq!(en("Alice a créé le groupe « Souper »"), "Alice created the group “Souper”");
        assert_eq!(en("Canal « Nouvelles » créé"), "Channel “Nouvelles” created");
        assert_eq!(en("(message non pris en charge : ouvre WhatsApp)"), "(unsupported message: open WhatsApp)");
        assert_eq!(en("Conversation Signal"), "Signal conversation");
        assert_eq!(en("Conversation intéressante"), "Conversation intéressante");
        assert_eq!(en("Alice a retiré la photo du groupe"), "Alice removed the group photo");
        // Un trou vide : pas la forme.
        assert_eq!(en(" a quitté le groupe"), " a quitté le groupe");
    }

    #[test]
    fn formes_de_service() {
        let s = |t: &str| traduire(t, true, true).into_owned();
        assert_eq!(s("Alice a ajouté Bob, quelqu'un"), "Alice added Bob, someone");
        assert_eq!(s("Quelqu'un a retiré Bob"), "Someone removed Bob");
        assert_eq!(s("Alice a renommé le groupe : Chalet"), "Alice renamed the group: Chalet");
        assert_eq!(s("Alice a retiré la photo du groupe"), "Alice removed the group photo");
        // Hors d'un message de service : un texte ordinaire qui leur ressemble.
        assert_eq!(en("Maman a ajouté du sel"), "Maman a ajouté du sel");
    }

    #[test]
    fn systeme() {
        assert_eq!(francais("🚫 Message supprimé", "deleted"), "🚫 Message retiré");
        assert_eq!(francais("texte", "inconnu"), "texte");
        assert_eq!(francais("texte", ""), "texte");
        for (_, fr) in SYSTEME {
            assert_ne!(en(fr), *fr, "« {fr} » sans traduction");
        }
    }

    /// Les tables d'Android (Langue.kt) : les mêmes paires, mot pour mot.
    #[test]
    fn memes_tables_qu_android() {
        let kt = std::fs::read_to_string(concat!(
            env!("CARGO_MANIFEST_DIR"),
            "/android/app/src/main/java/ca/jasette/android/Langue.kt"
        ))
        .unwrap();
        let tables: [&[(&str, &str)]; 6] = [SYSTEME, EXACTS, DEBUTS, FINS, FORMES, FORMES_SERVICE];
        for (fr, en) in tables.iter().flat_map(|t| t.iter()) {
            let paire = format!("\"{fr}\" to \"{en}\"");
            assert!(kt.contains(&paire), "Langue.kt n'a pas {paire}");
        }
    }
}
