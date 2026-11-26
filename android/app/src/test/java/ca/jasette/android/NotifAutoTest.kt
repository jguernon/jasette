package ca.jasette.android

import android.app.Application
import android.content.Intent
import android.os.Bundle
import androidx.core.app.NotificationCompat
import androidx.core.app.NotificationManagerCompat
import androidx.core.app.Person
import androidx.core.app.RemoteInput
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.RuntimeEnvironment
import org.robolectric.Shadows.shadowOf
import org.robolectric.annotation.Config

/** Les notifications de message pour Android Auto (NotifAuto.kt) : actions sémantiques, RemoteInput, sans interface. */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [35])
class NotifAutoTest {
    private val ctx: Application get() = RuntimeEnvironment.getApplication()

    private fun conv(id: String, group: Boolean = false, large: Boolean = false, readOnly: Boolean = false, announce: Boolean = false) = Conv(
        id = id, name = "Alice", preview = "", fromMe = false, time = 0, unread = true, group = group, rcs = false,
        color = "", members = emptyList(), readOnly = readOnly, archived = false, e2ee = false, number = "",
        large = large, announce = announce,
    )

    /** Construit une notification comme PontService.notifier. */
    private fun notification(c: Conv?, id: String): android.app.Notification {
        PontService.canaux(ctx)
        val moi = Person.Builder().setName("Moi").build()
        val alice = Person.Builder().setName("Alice").build()
        val style = NotifAuto.style(ctx, id) { NotificationCompat.MessagingStyle(moi) }
            .addMessage("Salut", 1000, alice)
            .also { if (c?.group == true) { it.setConversationTitle("Groupe"); it.setGroupConversation(true) } }
        val b = NotificationCompat.Builder(ctx, PontService.CANAL_MESSAGES)
            .setSmallIcon(R.drawable.ic_notif).setStyle(style).setCategory(NotificationCompat.CATEGORY_MESSAGE)
        NotifAuto.equiper(ctx, b, c, id)
        NotifAuto.noter(id, b, style)
        return b.build()
    }

    private fun actions(n: android.app.Notification) =
        (0 until NotificationCompat.getActionCount(n)).map { NotificationCompat.getAction(n, it)!! }

    private fun invisibles(n: android.app.Notification) = NotificationCompat.getInvisibleActions(n)

    @Test
    fun repondreEtLu() {
        val n = notification(conv("m:1"), "m:1")
        val rep = actions(n).single { it.semanticAction == NotificationCompat.Action.SEMANTIC_ACTION_REPLY }
        assertFalse(rep.showsUserInterface)
        val saisies = rep.remoteInputs
        assertNotNull(saisies)
        assertEquals(NotifAuto.CLE_REPONSE, saisies!!.single().resultKey)
        // Marquer comme lu : invisible, sans interface.
        assertTrue(actions(n).none { it.semanticAction == NotificationCompat.Action.SEMANTIC_ACTION_MARK_AS_READ })
        val lu = invisibles(n).single { it.semanticAction == NotificationCompat.Action.SEMANTIC_ACTION_MARK_AS_READ }
        assertFalse(lu.showsUserInterface)
        // Jamais une activité : un broadcast vers AutoRecepteur.
        assertTrue(shadowOf(rep.actionIntent).isBroadcastIntent)
        assertTrue(shadowOf(lu.actionIntent).isBroadcastIntent)
        assertEquals(AutoRecepteur::class.java.name, shadowOf(rep.actionIntent).savedIntent.component?.className)
        // MessagingStyle avec la personne.
        val style = NotificationCompat.MessagingStyle.extractMessagingStyleFromNotification(n)!!
        assertEquals("Alice", style.messages.last().person?.name)
    }

    @Test
    fun groupe() {
        val n = notification(conv("w:g", group = true), "w:g")
        val style = NotificationCompat.MessagingStyle.extractMessagingStyleFromNotification(n)!!
        assertTrue(style.isGroupConversation)
        assertEquals("Groupe", style.conversationTitle)
        assertEquals(1, actions(n).count { it.semanticAction == NotificationCompat.Action.SEMANTIC_ACTION_REPLY })
    }

    @Test
    fun lectureSeuleEtGrandsEspaces() {
        // Lecture seule : pas de Répondre, mais Marquer comme lu.
        val ro = notification(conv("t:1", readOnly = true), "t:1")
        assertTrue(actions(ro).none { it.semanticAction == NotificationCompat.Action.SEMANTIC_ACTION_REPLY })
        assertEquals(1, invisibles(ro).size)
        // Un grand espace (Canaux, §5.13) : rien pour la voiture.
        val grand = notification(conv("d:1", large = true), "d:1")
        assertTrue(actions(grand).isEmpty())
        assertTrue(invisibles(grand).isEmpty())
    }

    @Test
    fun reponseAjouteMonMessage() {
        val id = "s:1"
        val n = notification(conv(id), id)
        NotificationManagerCompat.from(ctx).notify(PontService.idNotification(id), n)
        NotifAuto.ajouterReponse(ctx, id, "J'arrive")
        val nm = shadowOf(ctx.getSystemService(android.app.NotificationManager::class.java))
        val apres = nm.getNotification(PontService.idNotification(id))
        val style = NotificationCompat.MessagingStyle.extractMessagingStyleFromNotification(apres)!!
        val dernier = style.messages.last()
        assertEquals("J'arrive", dernier.text.toString())
        // Personne nulle : c'est moi.
        assertNull(dernier.person)
        // Un deuxième message reçu s'ajoute à la notification affichée.
        val n2 = notification(conv(id), id)
        assertEquals(3, NotificationCompat.MessagingStyle.extractMessagingStyleFromNotification(n2)!!.messages.size)
    }

    @Test
    fun reponseLueDeLIntention() {
        val intent = Intent()
        val r = Bundle().apply { putCharSequence(NotifAuto.CLE_REPONSE, "Oui") }
        RemoteInput.addResultsToIntent(arrayOf(RemoteInput.Builder(NotifAuto.CLE_REPONSE).build()), intent, r)
        assertEquals("Oui", RemoteInput.getResultsFromIntent(intent)?.getCharSequence(NotifAuto.CLE_REPONSE))
    }
}
