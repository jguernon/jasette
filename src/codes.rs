// Les codes de vérification (2FA, connexion, confirmation) des messages
// reçus : « Votre code est 482913 », « G-123456 is your Google verification
// code », « Code : 12-34-56 », « Your code is K7Q2M9 ». Le fil montre à côté de
// la bulle un petit bouton qui copie le code seul (sans tirets).
//
// La même logique, pas à pas, est dans android/…/Codes.kt : toute
// modification se fait des deux côtés, avec les mêmes essais.
//
// Il faut un mot-clé (code, vérification, OTP, PIN, NIP, 2FA, confirm…) et un
// seul code plausible : 4 à 8 chiffres (seuls, en groupes égaux « 12-34-56 »,
// « 123 456 », ou derrière un préfixe « G- »), sinon un code de 4 à 8 lettres
// majuscules et chiffres mêlés. Les numéros de téléphone, montants, dates,
// heures, années et adresses sont écartés ; dans le doute, rien.

/// Au-delà, ce n'est plus un texto de code (une infolettre qui parle de « code »).
const LONGUEUR_MAX: usize = 400;

/// Les expressions où « code » ne veut pas dire un code à usage unique.
const FAUX_CODES: [&str; 9] = [
    "code postal", "postal code", "zip code", "code promo", "promo code", "code de réduction",
    "discount code", "coupon code", "code coupon",
];

/// Les mots-clés : (racine, mot entier ?). Une racine commence un mot ; un
/// mot entier finit aussi là.
const MOTS_CLES: [(&str, bool); 15] = [
    ("code", false),
    ("passcode", false),
    ("verif", false),
    ("vérif", false),
    ("otp", true),
    ("pin", true),
    ("nip", true),
    ("2fa", true),
    ("confirm", false),
    ("authenti", false),
    ("one-time", false),
    ("one time", false),
    ("usage unique", false),
    ("mot de passe temporaire", false),
    ("validation", false),
];

/// Les mots qui, juste après un nombre, en font un montant, une durée, une
/// distance ou une adresse.
const MOTS_APRES: [&str; 37] = [
    "$", "€", "£", "%", "dollar", "dollars", "usd", "cad", "eur", "euros", "km", "kg", "lb", "lbs", "points", "pts",
    "min", "minutes", "sec", "secondes", "seconds", "heures", "hours", "jours", "days", "rue", "avenue", "av", "ave",
    "boul", "boulevard", "chemin", "ch", "st", "street", "road", "blvd",
];

/// Les mots qui, juste avant un nombre (un ou deux mots plus tôt), en font
/// un numéro de compte, de carte, de commande, de suivi.
const MOTS_AVANT: [&str; 18] = [
    "ending", "ends", "terminant", "finissant", "commande", "order", "compte", "account", "carte", "card", "facture",
    "invoice", "suivi", "tracking", "colis", "vol", "flight", "chambre",
];

/// Un code trouvé : sa valeur à copier, et où il est (en caractères).
#[derive(Debug, Clone, PartialEq)]
struct Candidat {
    valeur: String,
    debut: usize,
    fin: usize,
}

fn lettre_ou_chiffre(c: char) -> bool {
    c.is_alphanumeric()
}

/// Les positions (début, fin) des mots-clés dans `t` (déjà en minuscules).
fn mots_cles(t: &[char]) -> Vec<(usize, usize)> {
    let mut v = Vec::new();
    for (racine, entier) in MOTS_CLES {
        let r: Vec<char> = racine.chars().collect();
        if r.len() > t.len() {
            continue;
        }
        for i in 0..=t.len() - r.len() {
            if t[i..i + r.len()] != r[..] {
                continue;
            }
            if i > 0 && lettre_ou_chiffre(t[i - 1]) {
                continue;
            }
            let fin = i + r.len();
            if entier && fin < t.len() && lettre_ou_chiffre(t[fin]) {
                continue;
            }
            v.push((i, fin));
        }
    }
    v
}

/// Le mot (lettres, ou un symbole seul) qui suit la position `i`, après les espaces.
fn mot_apres(t: &[char], mut i: usize) -> String {
    while i < t.len() && t[i] == ' ' {
        i += 1;
    }
    if i < t.len() && !lettre_ou_chiffre(t[i]) {
        return t[i].to_string();
    }
    let mut s = String::new();
    while i < t.len() && t[i].is_alphabetic() {
        s.push(t[i]);
        i += 1;
    }
    s
}

/// Les deux mots qui précèdent la position `i` (le plus proche d'abord).
fn mots_avant(t: &[char], i: usize) -> Vec<String> {
    let mut mots = Vec::new();
    let mut j = i;
    for _ in 0..2 {
        while j > 0 && !t[j - 1].is_alphabetic() {
            j -= 1;
        }
        let fin = j;
        while j > 0 && t[j - 1].is_alphabetic() {
            j -= 1;
        }
        if j == fin {
            break;
        }
        mots.push(t[j..fin].iter().collect());
    }
    mots
}

/// Le nombre `[debut, fin)` est-il un vrai nombre seul (pas un montant, une
/// date, une heure, un téléphone, une adresse) ?
fn nombre_isole(t: &[char], debut: usize, fin: usize) -> bool {
    let chiffre = |i: usize| i < t.len() && t[i].is_ascii_digit();
    if debut > 0 {
        let a = t[debut - 1];
        if lettre_ou_chiffre(a) || "+($€£¥".contains(a) {
            return false;
        }
        // 1234,56 · 12/05 · 14:30 · 3.14 : le morceau d'un nombre plus grand.
        if ".,:/".contains(a) && debut >= 2 && chiffre(debut - 2) {
            return false;
        }
    }
    if fin < t.len() {
        let p = t[fin];
        if lettre_ou_chiffre(p) || "$€£¥%)".contains(p) {
            return false;
        }
        if ".,:/".contains(p) && chiffre(fin + 1) {
            return false;
        }
    }
    let apres = mot_apres(t, fin);
    if MOTS_APRES.contains(&apres.as_str()) {
        return false;
    }
    let avant = mots_avant(t, debut);
    if avant.iter().any(|m| MOTS_AVANT.contains(&m.as_str())) {
        return false;
    }
    // Avant le nombre, un « $ » détaché (« $ 1500 »).
    let mut j = debut;
    while j > 0 && t[j - 1] == ' ' {
        j -= 1;
    }
    if j > 0 && "$€£".contains(t[j - 1]) {
        return false;
    }
    true
}

/// Les codes faits de chiffres : seuls, en groupes égaux, ou après « G- ».
fn candidats_chiffres(t: &[char], original: &[char]) -> Vec<Candidat> {
    let mut v = Vec::new();
    let mut i = 0;
    while i < t.len() {
        if !t[i].is_ascii_digit() || (i > 0 && t[i - 1].is_ascii_digit()) {
            i += 1;
            continue;
        }
        // Une suite de groupes de chiffres séparés par un seul « - » ou « ».
        let debut = i;
        let mut groupes: Vec<(usize, usize)> = Vec::new();
        let mut j = i;
        loop {
            let g = j;
            while j < t.len() && t[j].is_ascii_digit() {
                j += 1;
            }
            groupes.push((g, j));
            if j + 1 < t.len() && (t[j] == '-' || t[j] == ' ') && t[j + 1].is_ascii_digit() {
                j += 1;
            } else {
                break;
            }
        }
        let fin = j;
        i = fin;
        // « G-123456 », « FB-12345 » : une ou deux majuscules, un tiret, les chiffres.
        if groupes.len() == 1 && debut >= 2 && t[debut - 1] == '-' {
            let mut k = debut - 1;
            while k > 0 && original[k - 1].is_ascii_uppercase() {
                k -= 1;
            }
            let n = debut - 1 - k;
            if (1..=2).contains(&n) && (k == 0 || !lettre_ou_chiffre(t[k - 1])) {
                let l = fin - debut;
                if (4..=8).contains(&l) && (fin >= t.len() || !lettre_ou_chiffre(t[fin])) {
                    v.push(Candidat { valeur: t[debut..fin].iter().collect(), debut: k, fin });
                }
                continue;
            }
        }
        let chiffres: String = groupes.iter().flat_map(|&(a, b)| t[a..b].iter()).collect();
        let n = chiffres.chars().count();
        if groupes.len() > 1 {
            let l0 = groupes[0].1 - groupes[0].0;
            let egaux = groupes.iter().all(|&(a, b)| b - a == l0);
            if egaux && (2..=4).contains(&l0) && groupes.len() <= 4 && (4..=8).contains(&n) && nombre_isole(t, debut, fin) {
                v.push(Candidat { valeur: chiffres, debut, fin });
            }
            // Sinon : un téléphone, une date (2025-03-14) ; dans le doute, rien.
            continue;
        }
        if !(4..=8).contains(&n) || !nombre_isole(t, debut, fin) {
            continue;
        }
        // Une année.
        if n == 4 && let Ok(a) = chiffres.parse::<u32>() && (1900..=2099).contains(&a) {
            continue;
        }
        v.push(Candidat { valeur: chiffres, debut, fin });
    }
    v
}

/// Les codes de lettres majuscules et de chiffres mêlés (« K7Q2M9 »).
fn candidats_alphanumeriques(original: &[char]) -> Vec<Candidat> {
    let mut v = Vec::new();
    let mut i = 0;
    while i < original.len() {
        if !original[i].is_ascii_alphanumeric() || (i > 0 && lettre_ou_chiffre(original[i - 1])) {
            i += 1;
            continue;
        }
        let debut = i;
        while i < original.len() && original[i].is_ascii_alphanumeric() {
            i += 1;
        }
        // Collé à une autre lettre (accentuée) : un mot, pas un code.
        if i < original.len() && lettre_ou_chiffre(original[i]) {
            continue;
        }
        let mot = &original[debut..i];
        let chiffres = mot.iter().filter(|c| c.is_ascii_digit()).count();
        let lettres = mot.len() - chiffres;
        if (4..=8).contains(&mot.len()) && chiffres > 0 && lettres > 0 && mot.iter().all(|c| !c.is_ascii_lowercase()) {
            v.push(Candidat { valeur: mot.iter().collect(), debut, fin: i });
        }
    }
    v
}

/// Le code de vérification d'un message reçu, à copier (sans tirets) ;
/// None s'il n'y en a pas, ou dans le doute.
pub(crate) fn code_verification(texte: &str) -> Option<String> {
    let original: Vec<char> = texte.chars().collect();
    if original.is_empty() || original.len() > LONGUEUR_MAX {
        return None;
    }
    // En minuscules, caractère pour caractère (les positions restent les mêmes).
    let mut t: Vec<char> = original.iter().map(|c| c.to_lowercase().next().unwrap_or(*c)).collect();
    // Les « code postal », « code promo »… ne comptent pas.
    for f in FAUX_CODES {
        let f: Vec<char> = f.chars().collect();
        let mut i = 0;
        while i + f.len() <= t.len() {
            if t[i..i + f.len()] == f[..] {
                for c in &mut t[i..i + f.len()] {
                    *c = '_';
                }
                i += f.len();
            } else {
                i += 1;
            }
        }
    }
    let cles = mots_cles(&t);
    if cles.is_empty() {
        return None;
    }
    let mut candidats = candidats_chiffres(&t, &original);
    if candidats.is_empty() {
        candidats = candidats_alphanumeriques(&original);
        // Le mot-clé lui-même (« 2FA ») n'est pas un code.
        candidats.retain(|c| !cles.iter().any(|&(a, b)| a < c.fin && c.debut < b));
    }
    choisir(&candidats, &cles)
}

/// Un seul code : lui. Plusieurs : le plus proche d'un mot-clé, s'il n'y en a
/// qu'un à cette distance.
fn choisir(candidats: &[Candidat], cles: &[(usize, usize)]) -> Option<String> {
    let premier = candidats.first()?;
    if candidats.iter().all(|c| c.valeur == premier.valeur) {
        return Some(premier.valeur.clone());
    }
    let distance = |c: &Candidat| {
        cles.iter()
            .map(|&(a, b)| if c.debut >= b { c.debut - b } else if a >= c.fin { a - c.fin } else { 0 })
            .min()
            .unwrap_or(usize::MAX)
    };
    let min = candidats.iter().map(distance).min()?;
    let proches: Vec<&Candidat> = candidats.iter().filter(|c| distance(c) == min).collect();
    if proches.iter().all(|c| c.valeur == proches[0].valeur) { Some(proches[0].valeur.clone()) } else { None }
}

#[cfg(test)]
mod essais {
    use super::code_verification as code;

    #[test]
    fn trouve_les_codes() {
        let cas = [
            ("Votre code est 482913", "482913"),
            ("G-123456 is your Google verification code.", "123456"),
            ("G-123456 est votre code de validation Google.", "123456"),
            ("Code : 12-34-56", "123456"),
            ("Your Uber code: 4821. Never share this code.", "4821"),
            ("Votre code de vérification Desjardins : 123 456", "123456"),
            ("Use 123 456 to verify your Instagram account.", "123456"),
            ("Your verification code is K7Q2M9", "K7Q2M9"),
            ("Use 739201 to verify your account ending in 4321", "739201"),
            ("Votre NIP temporaire est 8841", "8841"),
            ("Votre mot de passe à usage unique : 55667788", "55667788"),
            ("[Steam] Code 2FA : F4K9X", "F4K9X"),
            ("Your OTP is 0042. It expires in 10 minutes.", "0042"),
            ("PIN: 9137", "9137"),
            ("Confirmation code 1234-5678", "12345678"),
            ("Votre code d'accès est 650194. Ne le partagez avec personne.", "650194"),
            ("WhatsApp code 123-456. Don't share this code with others", "123456"),
            ("Telegram code: 74219\n\nDo not give this code to anyone", "74219"),
            ("Ton code de connexion : 384 921 (valide 10 min)", "384921"),
        ];
        for (texte, attendu) in cas {
            assert_eq!(code(texte).as_deref(), Some(attendu), "{texte}");
        }
    }

    #[test]
    fn ecarte_les_faux() {
        let cas = [
            "Appelle-moi au 514-555-1234",
            "Code de confirmation envoyé au 514-555-1234",
            "Rappel : appelez le +1 514 555 1234 pour confirmer",
            "Ton code postal c'est H2X 1Y4",
            "Le code promo SAVE20 donne 20 % de rabais",
            "Confirmez votre rendez-vous du 12/05/2025 à 10:30",
            "Confirmation : votre paiement de 1500 $ a été reçu",
            "Confirmation : votre paiement de $1500 a été reçu",
            "Confirmez votre réservation au 1234 rue Sainte-Catherine",
            "Votre commande 2024 est confirmée",
            "Your PIN code will expire in 2025",
            "Salut! Ton code est bon, on se voit à 1800h?",
            "On se voit à 482913 heures",
            "Combien ça coûte? 1500",
            "Votre facture de 1234,56 est confirmée",
            "Confirmé pour le 2025-03-14 à 14:30",
            "J'ai codé 4 heures hier",
            "Pinned 123456",
            "Code : 12-345-6789",
            "",
            "Votre compte 123456 a été confirmé",
        ];
        for texte in cas {
            assert_eq!(code(texte), None, "{texte}");
        }
    }

    #[test]
    fn un_long_message_ne_compte_pas() {
        let long = format!("Votre code est 482913. {}", "bla ".repeat(120));
        assert_eq!(code(&long), None);
    }
}
