package ca.jasette.android

import androidx.car.app.model.GridTemplate
import androidx.car.app.model.ItemList
import androidx.car.app.model.ListTemplate
import androidx.car.app.model.SectionedItemList
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.RuntimeEnvironment
import org.robolectric.annotation.Config

/** L'écran Android Auto (Voiture.kt) : messages de la voiture, et modèles acceptés par la Car App Library. */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [35])
class VoitureTest {
    private val ctx get() = RuntimeEnvironment.getApplication()

    private fun conv(id: String, unread: Boolean = true, group: Boolean = false, preview: String = "Salut") = Conv(
        id = id, name = "Alice", preview = preview, fromMe = false, time = 1000, unread = unread, group = group, rcs = false,
        color = "", members = emptyList(), readOnly = false, archived = false, e2ee = false, number = "",
    )

    private fun msg(id: String, conv: String, texte: String, moi: Boolean, temps: Long, auteur: String = "") = Msg(
        id = id, conv = conv, text = texte, media = "", fromMe = moi, sender = auteur, time = temps, status = "",
        kind = "", tmpId = "", attachments = emptyList(), reactions = emptyList(),
    )

    @After
    fun menage() {
        Depot.messages.clear()
    }

    @Test
    fun apercuSansMessages() {
        val l = ConvVoiture.messages(conv("m:1"), ConvVoiture.moi)
        assertEquals(1, l.size)
        assertEquals("Salut", l[0].body.toString())
        assertFalse(l[0].isRead)
    }

    @Test
    fun cinqDerniersEtNonLus() {
        val id = "w:1"
        Depot.messages[id] = (1..8).map { msg("$it", id, "m$it", moi = it == 6, temps = it * 10L) }
        val l = ConvVoiture.messages(conv(id), ConvVoiture.moi)
        assertEquals(ConvVoiture.MAX_MESSAGES, l.size)
        assertEquals(listOf("m4", "m5", "m6", "m7", "m8"), l.map { it.body.toString() })
        // Avant et y compris mon message : lus ; après : non lus.
        assertEquals(listOf(true, true, true, false, false), l.map { it.isRead })
        assertEquals("moi", l[2].sender?.key)
    }

    @Test
    fun groupeNommeLAuteur() {
        val id = "s:g"
        Depot.messages[id] = listOf(msg("1", id, "yo", moi = false, temps = 1, auteur = "Bob"))
        val l = ConvVoiture.messages(conv(id, group = true), ConvVoiture.moi)
        assertEquals("Bob", l[0].sender?.name)
    }

    @Test
    fun modelesValides() {
        // ConversationItem dans un ListTemplate sectionné (accueil) et seul (conversation) : la bibliothèque valide à build().
        val a = ConvVoiture.item(ctx, conv("m:1"))
        val b = ConvVoiture.item(ctx, conv("i:2", unread = false))
        val accueil = ListTemplate.Builder().setHeader(androidx.car.app.model.Header.Builder().setTitle("Jasette").build())
            .addSectionedList(SectionedItemList.create(ItemList.Builder().addItem(a).build(), "Dernier message"))
            .addSectionedList(SectionedItemList.create(ItemList.Builder().addItem(b).build(), "Récentes"))
            .build()
        assertEquals(2, accueil.sectionedLists.size)
        assertEquals("m:1", a.id)
        // La grille des visages : des GridItem ronds.
        val g = GridTemplate.Builder().setHeader(androidx.car.app.model.Header.Builder().setTitle("Head").build()).setItemImageShape(GridTemplate.ITEM_IMAGE_SHAPE_CIRCLE)
            .setSingleList(ItemList.Builder().addItem(ConvVoiture.visage(ctx, conv("m:1")) {}).build())
            .build()
        assertTrue(g.singleList!!.items.isNotEmpty())
    }
}
