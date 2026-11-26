package ca.jasette.android

import org.json.JSONObject
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

/** L'onglet Groupes (PROTOCOLE.md §5.13) : règle de la boîte de réception, notifications, rangement. */
class GroupesTest {
    private fun conv(
        id: String,
        name: String = id,
        large: Boolean = true,
        kind: String = "",
        parent: String = "",
        parentName: String = "",
        section: String = "",
        sectionOrder: Int = 0,
        order: Int = 0,
        mentions: Int = 0,
        time: Long = 0,
        unread: Boolean = false,
        announce: Boolean = false,
    ) = Conv(
        id = id, name = name, preview = "", fromMe = false, time = time, unread = unread, group = true, rcs = false,
        color = "", members = emptyList(), readOnly = false, archived = false, e2ee = false, number = "",
        large = large, kind = kind, announce = announce, parent = parent, parentName = parentName, section = section,
        sectionOrder = sectionOrder, order = order, mentions = mentions,
    )

    private fun msg(conv: String, fromMe: Boolean = false, mentionsMe: Boolean = false, replyToMe: Boolean = false) = Msg(
        id = "1", conv = conv, text = "salut", media = "", fromMe = fromMe, sender = "", time = 1, status = "", kind = "",
        tmpId = "", attachments = emptyList(), reactions = emptyList(), mentionsMe = mentionsMe, replyToMe = replyToMe,
    )

    @Test
    fun regleDeLaBoite() {
        // Sans `large` : toujours dans la boîte de réception.
        assertTrue(dansBoiteDeReception(conv("w:1", large = false)))
        // `large` : jamais, même avec des mentions (« inbox = chat »).
        val g = conv("d:g1:2", kind = "server")
        assertFalse(dansBoiteDeReception(g))
        assertFalse(dansBoiteDeReception(g.copy(mentions = 2, unread = true)))
    }

    @Test
    fun attentionEtNotifications() {
        assertTrue(demandeAttention(msg("t:1", mentionsMe = true)))
        assertTrue(demandeAttention(msg("t:1", replyToMe = true)))
        assertFalse("pas mes propres messages", demandeAttention(msg("t:1", fromMe = true, mentionsMe = true)))
        assertFalse(demandeAttention(msg("t:1")))
        val canal = conv("t:1", kind = "channel")
        // Un grand espace : seulement sur mention ou réponse.
        assertFalse(notifierMessage(canal, msg("t:1")))
        assertTrue(notifierMessage(canal, msg("t:1", mentionsMe = true)))
        // Une conversation ordinaire, ou inconnue : comme avant.
        assertTrue(notifierMessage(conv("w:2", large = false), msg("w:2")))
        assertTrue(notifierMessage(null, msg("w:3")))
    }

    @Test
    fun champsGardesParLeCache() {
        val c = conv(
            "d:g1:2", kind = "server", parent = "g1", parentName = "Mon serveur", section = "Texte", sectionOrder = 3,
            order = 7, mentions = 4, announce = true,
        )
        assertEquals(c, Conv.de(JSONObject(c.json().toString())))
        // Un pont d'avant : rien de neuf, tout reste comme avant.
        val vieux = Conv.de(JSONObject("""{"id":"w:1","name":"A","time":5}"""))
        assertFalse(vieux.large)
        assertEquals("", vieux.kind)
        assertEquals(0, vieux.mentions)
        assertFalse(vieux.json().has("large"))
        val m = msg("t:1", mentionsMe = true, replyToMe = true)
        assertEquals(m, Msg.de(JSONObject(m.json().toString())))
    }

    @Test
    fun salonsRangesParCategorie() {
        val salons = listOf(
            conv("d:g1:5", "zeta", section = "Voix", sectionOrder = 2, order = 0),
            conv("d:g1:4", "beta", section = "Texte", sectionOrder = 1, order = 2),
            conv("d:g1:3", "alpha", section = "Texte", sectionOrder = 1, order = 2),
            conv("d:g1:2", "gamma", section = "Texte", sectionOrder = 1, order = 1),
            conv("d:g1:1", "règles", section = "", order = 5),
        )
        val r = rangerSalons(salons)
        assertEquals(listOf("", "Texte", "Voix"), r.map { it.first })
        assertEquals(listOf("gamma", "alpha", "beta"), r[1].second.map { it.name })
    }

    @Test
    fun serveursDiscord() {
        val convs = listOf(
            conv("d:g2:1", "général", kind = "server", parent = "g2", parentName = "Zorg", unread = true, mentions = 1),
            conv("d:g1:1", "général", kind = "server", parent = "g1", parentName = "Atelier", unread = true),
            conv("d:g1:2", "annonces", kind = "server", parent = "g1", parentName = "Atelier"),
            // Un groupe privé Discord n'est pas `large` : jamais dans Groupes.
            conv("d:9", "Amis", large = false),
        )
        val l = lignesService(Service.DISCORD, convs)
        assertEquals(2, l.size)
        val a = (l[0] as LigneGroupes.Serveur).e
        assertEquals("Atelier", a.nom)
        assertEquals("d:g1", a.id)
        assertEquals(2, a.salons.size)
        assertEquals(1, a.nonLus)
        assertEquals(1, (l[1] as LigneGroupes.Serveur).e.mentions)
        assertEquals(2, espacesNonLus(Service.DISCORD, convs))
    }

    @Test
    fun whatsappCommunautesPuisChaines() {
        val convs = listOf(
            conv("w:c1@g.us", "Quartier", kind = "community"),
            conv("w:a@g.us", "Annonces", kind = "community_group", parent = "c1@g.us", announce = true, time = 1),
            conv("w:b@g.us", "Jardin", kind = "community_group", parent = "c1@g.us", time = 9),
            conv("w:d@g.us", "Orphelin", kind = "community_group", parent = "c2@g.us", parentName = "École", time = 3),
            conv("w:n@newsletter", "Nouvelles", kind = "newsletter"),
            conv("w:p@g.us", "Famille", large = false),
        )
        val l = lignesService(Service.WHATSAPP, convs)
        assertEquals(
            listOf(
                LigneGroupes.Entete("École"),
                LigneGroupes.Espace(convs[3], retrait = true),
                LigneGroupes.Espace(convs[0]),
                LigneGroupes.Espace(convs[1], retrait = true),
                LigneGroupes.Espace(convs[2], retrait = true),
                LigneGroupes.Titre("Chaînes"),
                LigneGroupes.Espace(convs[4]),
            ),
            l,
        )
    }

    @Test
    fun telegramCanauxPuisSupergroupes() {
        val convs = listOf(
            conv("t:2", "Super", kind = "supergroup"),
            conv("t:1", "Canal", kind = "channel"),
            conv("t:3", "Inconnu", kind = "autre_chose"),
        )
        val l = lignesService(Service.TELEGRAM, convs)
        assertEquals(
            listOf(
                LigneGroupes.Titre("Canaux"), LigneGroupes.Espace(convs[1]),
                LigneGroupes.Titre("Supergroupes"), LigneGroupes.Espace(convs[0]),
                LigneGroupes.Titre("Autres"), LigneGroupes.Espace(convs[2]),
            ),
            l,
        )
        assertTrue(lignesService(Service.SIGNAL, convs).isEmpty())
    }
}
