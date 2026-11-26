package ca.jasette.android

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

/** La conversion des rangées SMS et MMS vers le protocole des ponts (SmsJson, pour PontSms). */
class SmsJsonTest {
    private fun sms(type: Int, statut: Int = -1, corps: String = "Salut", date: Long = 1_700_000_000_000) =
        LigneSms(id = 42, thread = 7, adresse = "+15145551234", corps = corps, date = date, type = type, statut = statut)

    @Test
    fun smsRecu() {
        val o = SmsJson.msgSms(sms(1), "Camille")!!
        assertEquals("sms.42", o.getString("id"))
        assertEquals("sms-7", o.getString("conv"))
        assertEquals("Salut", o.getString("text"))
        assertFalse(o.getBoolean("from_me"))
        assertEquals("Camille", o.getString("sender"))
        assertEquals(1_700_000_000_000, o.getLong("time"))
        assertEquals("INCOMING_COMPLETE", o.getString("status"))
        assertFalse(o.has("attachments"))
    }

    @Test
    fun smsRecuSansContact() {
        assertEquals("+15145551234", SmsJson.msgSms(sms(1), null)!!.getString("sender"))
    }

    @Test
    fun statutsEnvoi() {
        assertEquals("OUTGOING_COMPLETE", SmsJson.msgSms(sms(2), null)!!.getString("status"))
        assertEquals("OUTGOING_DELIVERED", SmsJson.msgSms(sms(2, 0), null)!!.getString("status"))
        assertEquals("OUTGOING_FAILED", SmsJson.msgSms(sms(2, 64), null)!!.getString("status"))
        assertEquals("OUTGOING_SENDING", SmsJson.msgSms(sms(4), null)!!.getString("status"))
        assertEquals("OUTGOING_SENDING", SmsJson.msgSms(sms(6), null)!!.getString("status"))
        assertEquals("OUTGOING_FAILED", SmsJson.msgSms(sms(5), null)!!.getString("status"))
        val moi = SmsJson.msgSms(sms(2), "Camille")!!
        assertTrue(moi.getBoolean("from_me"))
        assertEquals("", moi.getString("sender"))
        // Les statuts tombent sur les coches de l'interface.
        assertEquals(Coche.DISTRIBUE, coche(Msg.de(SmsJson.msgSms(sms(2, 0), null)!!)))
        assertEquals(Coche.ECHEC, coche(Msg.de(SmsJson.msgSms(sms(5), null)!!)))
    }

    @Test
    fun brouillonIgnore() {
        assertNull(SmsJson.msgSms(sms(3), null))
    }

    @Test
    fun mmsAvecImage() {
        val l = LigneMms(
            id = 9, thread = 7, dateSec = 1_700_000_000, boite = 1, de = "+15145551234", texte = "",
            pieces = listOf(PieceMms(55, "image/jpeg", "photo.jpg")),
        )
        val o = SmsJson.msgMms(l, "Camille")!!
        assertEquals("mms.9", o.getString("id"))
        assertEquals(1_700_000_000_000, o.getLong("time"))
        val m = Msg.de(o)
        assertEquals(1, m.attachments.size)
        assertEquals("mmspart-55", m.attachments[0].mediaId)
        assertTrue(m.attachments[0].estImage)
        assertEquals(55L, SmsJson.pieceDe("mmspart-55"))
        assertEquals("📷 Photo", SmsJson.apercuMms(l))
    }

    @Test
    fun conversationADeux() {
        val o = SmsJson.conv(7, listOf("+15145551234"), listOf("Camille"), "Salut", false, 123, true)
        val c = Conv.de(o)
        assertEquals("sms-7", c.id)
        assertEquals("Camille", c.name)
        assertEquals("+15145551234", c.number)
        assertTrue(c.unread)
        assertFalse(c.group)
        assertFalse(c.readOnly)
        assertEquals(7L, SmsJson.threadDe(c.id))
        // Pas de préfixe de service : rangée avec Google Messages.
        assertNull(Service.prefixeDe(c.id))
    }

    @Test
    fun groupeEnLectureSeule() {
        val c = Conv.de(SmsJson.conv(8, listOf("+1514", "+1438"), listOf("Camille", "Léo"), "", true, 1, false))
        assertTrue(c.group)
        assertTrue(c.readOnly)
        assertEquals("Camille, Léo", c.name)
        assertEquals("", c.number)
    }

    @Test
    fun envoiGardeParJasette() {
        val local = SmsJson.msgLocal("jasette_1_1", 7, "Allo", 1_700_000_000_000)
        val m = Msg.de(local)
        assertEquals("jasette_1_1", m.tmpId)
        assertFalse(m.local) // id ≠ tmp_id : ce n'est plus une bulle « Envoi… »
        assertEquals(Coche.ENVOYE, coche(m))
        assertTrue(SmsJson.memeEnvoi(local, sms(2, corps = "Allo", date = 1_700_000_030_000)))
        assertFalse(SmsJson.memeEnvoi(local, sms(1, corps = "Allo", date = 1_700_000_030_000)))
        assertFalse(SmsJson.memeEnvoi(local, sms(2, corps = "Allo", date = 1_700_000_300_000)))
    }
}
