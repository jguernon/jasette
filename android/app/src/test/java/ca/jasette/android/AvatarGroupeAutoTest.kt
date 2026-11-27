package ca.jasette.android

import android.app.Application
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertSame
import org.junit.Assert.assertTrue
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.RuntimeEnvironment
import org.robolectric.annotation.Config

/**
 * L'avatar d'un expéditeur automatique (RegroupementAuto.groupe) : la
 * détection tourne même quand les trois cases sont décochées, avec les mêmes
 * exclusions, et la correction à la main gagne ; l'image des notifications
 * (Raccourcis.kt, `imageGroupe`).
 */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [35])
class AvatarGroupeAutoTest {
    private val ctx: Application get() = RuntimeEnvironment.getApplication()

    // Un numéro court de Google Messages (SMS) : un seul code suffit.
    private fun conv(id: String, preview: String, fromMe: Boolean = false) = Conv(
        id = id, name = id, preview = preview, fromMe = fromMe, time = 1000, unread = true, group = false, rcs = false,
        color = "", members = emptyList(), readOnly = false, archived = false, e2ee = false, number = id,
    )

    @After
    fun menage() {
        for (id in listOf("72345", "72346")) Marques.oublier(id)
        Depot.messages.clear()
    }

    @Test
    fun detecteeCasesDecochees() {
        Marques.charger(ctx)
        RegroupementAuto.charger(ctx)
        for (g in GroupeAuto.entries) RegroupementAuto.choisir(ctx, g, false)
        val c = conv("72345", "Votre code est 482913")
        assertFalse(RegroupementAuto.unActif)
        // Rien n'est regroupé, mais l'avatar connaît son groupe.
        assertNull(RegroupementAuto.groupeActif(c))
        assertEquals(GroupeAuto.CODES, RegroupementAuto.groupe(c))
        // Une conversation où j'ai écrit, ou un message ordinaire : rien.
        assertNull(RegroupementAuto.groupe(conv("72346", "Votre code est 482913", fromMe = true)))
        assertNull(RegroupementAuto.groupe(conv("72346", "Salut, on se voit demain?")))
        // La correction à la main gagne, case cochée ou non.
        Marques.corrigerGroupe(c.id, "aucun")
        assertNull(RegroupementAuto.groupe(c))
        Marques.corrigerGroupe(c.id, "avis")
        assertEquals(GroupeAuto.AVIS, RegroupementAuto.groupe(c))
        // Un favori n'est jamais classé, même corrigé.
        Marques.basculerFavori(c.id)
        assertTrue(RegroupementAuto.horsClassement(c))
        assertNull(RegroupementAuto.groupe(c))
        Marques.basculerFavori(c.id)
        assertEquals(GroupeAuto.AVIS, RegroupementAuto.groupe(c))
        // Case cochée : regroupée.
        RegroupementAuto.choisir(ctx, GroupeAuto.AVIS, true)
        assertEquals(GroupeAuto.AVIS, RegroupementAuto.groupeActif(c))
        RegroupementAuto.choisir(ctx, GroupeAuto.AVIS, false)
    }

    @Test
    fun imageDesNotifications() {
        for (g in GroupeAuto.entries) for (ega in listOf(false, true)) {
            val b = imageGroupe(g, ega)
            assertEquals(256, b.width)
            assertEquals(256, b.height)
            // Gardée : la même image la fois suivante.
            assertSame(b, imageGroupe(g, ega))
            // Les pixels ne se vérifient pas ici : le rendu natif de Robolectric ne se
            // charge pas depuis ce disque (l'espace du chemin) ; seulement que le
            // dessin passe et que l'image est gardée.
        }
    }
}
