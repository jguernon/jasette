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
    fun serviceDeTousLesIds() {
        assertEquals(Service.GOOGLE, Service.de("33612345678"))
        assertEquals(Service.DISCORD, Service.de("d:123"))
        assertEquals(Service.DISCORD, Service.de("d:g1:2"))
        assertEquals(Service.DISCORD, Service.de("d:g1"))
        assertEquals(Service.TELEGRAM, Service.de("t:c7003"))
        assertEquals(Service.WHATSAPP, Service.de("w:1@g.us"))
        assertEquals(Service.MESSENGER, Service.de("m:2101"))
    }

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
        assertFalse("un canal ne notifie jamais, même une mention", notifierMessage(canal, msg("t:1", mentionsMe = true)))
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
    fun parentsGeneriquesPourToutService() {
        val convs = listOf(
            conv("d:g2:1", "général", kind = "server", parent = "g2", parentName = "Zorg", unread = true, mentions = 1, time = 50),
            conv("d:g1:1", "général", kind = "server", parent = "g1", parentName = "Atelier", unread = true, time = 10),
            conv("d:g1:2", "annonces", kind = "server", parent = "g1", parentName = "Atelier", mentions = 2, time = 20),
            // Un salon sans parent : une ligne simple, rangée avec les parents par activité.
            conv("d:7", "seul", kind = "server", time = 30),
            // Un groupe privé Discord n'est pas `large` : jamais dans Groupes.
            conv("d:9", "Amis", large = false, time = 99),
        )
        val l = lignesService(Service.DISCORD, convs)
        assertEquals(3, l.size)
        val zorg = (l[0] as LigneGroupes.Parent).e
        assertEquals("Zorg", zorg.nom)
        assertEquals("d:g2", zorg.id)
        assertEquals(LigneGroupes.Espace(convs[3]), l[1])
        val atelier = (l[2] as LigneGroupes.Parent).e
        assertEquals("d:g1", atelier.id)
        assertEquals(2, atelier.salons.size)
        assertEquals(1, atelier.nonLus)
        assertEquals(2, atelier.mentions)
        assertEquals(2, espacesNonLus(Service.DISCORD, convs))
    }

    @Test
    fun communauteWhatsappEnTeteDeSesGroupes() {
        val convs = listOf(
            conv("w:c1@g.us", "Quartier", kind = "community", time = 1),
            conv("w:a@g.us", "Annonces", kind = "community_group", parent = "c1@g.us", announce = true, time = 2),
            conv("w:b@g.us", "Jardin", kind = "community_group", parent = "c1@g.us", time = 9),
            conv("w:d@g.us", "Orphelin", kind = "community_group", parent = "c2@g.us", time = 3),
            conv("w:n@g.us", "Seul", kind = "community_group", time = 5),
            conv("w:p@g.us", "Famille", large = false, time = 20),
        )
        val l = lignesService(Service.WHATSAPP, convs)
        assertEquals(3, l.size)
        val quartier = (l[0] as LigneGroupes.Parent).e
        // Sans parent_name : le nom de la conversation parente ; elle est en tête, sans ligne à part.
        assertEquals("Quartier", quartier.nom)
        assertEquals("w:c1@g.us", quartier.id)
        assertEquals(convs[0], quartier.tete)
        assertEquals(listOf(convs[1], convs[2]), quartier.salons)
        assertEquals(LigneGroupes.Espace(convs[4]), l[1])
        // Parent absent de la liste et sans nom : « Communauté » (traduit à l'affichage).
        val orphelin = (l[2] as LigneGroupes.Parent).e
        assertEquals("", orphelin.nom)
        assertEquals("Communauté", orphelin.generique)
        assertEquals("Serveur", EspaceParent(Service.DISCORD, "g1", "", listOf(conv("d:g1:1", kind = "server", parent = "g1"))).generique)
        // Ouvrir depuis une notification : l'espace du parent, préfixé et unique par service.
        assertEquals("w:c1@g.us", espaceDe(convs[1], convs))
        assertEquals("w:c1@g.us", espaceDe(convs[0], convs))
        assertEquals(null, espaceDe(convs[4], convs))
        assertEquals("w:c1@g.us", parentDe("w:c1@g.us", convs)?.id)
        assertEquals(null, parentDe("d:c1@g.us", convs))
    }

    @Test
    fun telegramSansParentParActivite() {
        val convs = listOf(
            conv("t:2", "Super", kind = "supergroup", time = 1),
            conv("t:1", "Canal", kind = "channel", time = 3),
            conv("t:3", "Inconnu", kind = "autre_chose", time = 2),
        )
        val l = lignesService(Service.TELEGRAM, convs)
        assertEquals(listOf(LigneGroupes.Espace(convs[1]), LigneGroupes.Espace(convs[2]), LigneGroupes.Espace(convs[0])), l)
        assertTrue(lignesService(Service.SIGNAL, convs).isEmpty())
    }

    @Test
    fun toutMarquerCommeLu() {
        val convs = listOf(
            conv("d:g1:1", kind = "server", parent = "g1", unread = true),
            conv("d:g1:2", kind = "server", parent = "g1"),
            conv("d:g1:3", kind = "server", parent = "g1", mentions = 2),
            conv("d:g2:1", kind = "server", parent = "g2", unread = true),
            conv("d:5", kind = "server", unread = true),
            conv("d:6", large = false, unread = true),
            conv("d:g1:9", kind = "server", parent = "g1", unread = true),
            conv("t:1", kind = "channel", unread = true),
            conv("w:c@g.us", kind = "community", unread = true),
            conv("w:x@g.us", kind = "community_group", parent = "c@g.us", unread = true),
        )
        val privee = { id: String -> id == "d:g1:9" }
        // La section Discord : seulement ses espaces non lus (ou mentionnés), sans les privées.
        assertEquals(
            listOf("d:g1:1", "d:g1:3", "d:g2:1", "d:5"),
            aMarquerLus(Service.DISCORD, convs, privee = privee).map { it.id },
        )
        // Un parent : ses salons seulement, pas ceux d'un autre parent.
        assertEquals(listOf("d:g1:1", "d:g1:3"), aMarquerLus(Service.DISCORD, convs, "d:g1", privee).map { it.id })
        // Une communauté : elle-même et ses groupes.
        assertEquals(listOf("w:c@g.us", "w:x@g.us"), aMarquerLus(Service.WHATSAPP, convs, "w:c@g.us").map { it.id })
        assertTrue(aMarquerLus(Service.SIGNAL, convs).isEmpty())
    }

    @Test
    fun auteurGardeParLeCache() {
        val m = msg("d:g1:2").copy(senderId = "4242")
        assertEquals(m, Msg.de(JSONObject(m.json().toString())))
        assertEquals("4242", Msg.de(JSONObject("""{"id":"1","conv":"d:1","sender_id":"4242"}""")).senderId)
        assertEquals("", Msg.de(JSONObject("""{"id":"1","conv":"d:1"}""")).senderId)
    }
}
