package ca.jasette.android

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

/** Les mêmes essais que src/codes.rs (bureau) : les deux détections doivent s'accorder. */
class CodesTest {
    @Test
    fun trouveLesCodes() {
        val cas = listOf(
            "Votre code est 482913" to "482913",
            "G-123456 is your Google verification code." to "123456",
            "G-123456 est votre code de validation Google." to "123456",
            "Code : 12-34-56" to "123456",
            "Your Uber code: 4821. Never share this code." to "4821",
            "Votre code de vérification Desjardins : 123 456" to "123456",
            "Use 123 456 to verify your Instagram account." to "123456",
            "Your verification code is K7Q2M9" to "K7Q2M9",
            "Use 739201 to verify your account ending in 4321" to "739201",
            "Votre NIP temporaire est 8841" to "8841",
            "Votre mot de passe à usage unique : 55667788" to "55667788",
            "[Steam] Code 2FA : F4K9X" to "F4K9X",
            "Your OTP is 0042. It expires in 10 minutes." to "0042",
            "PIN: 9137" to "9137",
            "Confirmation code 1234-5678" to "12345678",
            "Votre code d'accès est 650194. Ne le partagez avec personne." to "650194",
            "WhatsApp code 123-456. Don't share this code with others" to "123456",
            "Telegram code: 74219\n\nDo not give this code to anyone" to "74219",
            "Ton code de connexion : 384 921 (valide 10 min)" to "384921",
        )
        for ((texte, attendu) in cas) assertEquals(texte, attendu, codeVerification(texte))
    }

    @Test
    fun ecarteLesFaux() {
        val cas = listOf(
            "Appelle-moi au 514-555-1234",
            "Code de confirmation envoyé au 514-555-1234",
            "Rappel : appelez le +1 514 555 1234 pour confirmer",
            "Ton code postal c'est H2X 1Y4",
            "Le code promo SAVE20 donne 20 % de rabais",
            "Confirmez votre rendez-vous du 12/05/2025 à 10:30",
            "Confirmation : votre paiement de 1500 \$ a été reçu",
            "Confirmation : votre paiement de \$1500 a été reçu",
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
        )
        for (texte in cas) assertNull(texte, codeVerification(texte))
    }

    @Test
    fun unLongMessageNeComptePas() {
        assertNull(codeVerification("Votre code est 482913. " + "bla ".repeat(120)))
    }
}
