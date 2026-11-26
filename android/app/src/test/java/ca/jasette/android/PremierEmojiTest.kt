package ca.jasette.android

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

class PremierEmojiTest {
    @Test
    fun sequences() {
        assertEquals("🌴", premierEmoji("🌴 En vacances"))
        assertEquals("🎵", premierEmoji("🎵 Écoute Daft Punk"))
        assertNull(premierEmoji("En vacances !"))
        assertEquals("🇨🇦", premierEmoji("Go 🇨🇦 !"))
        assertEquals("👩‍💻", premierEmoji("👩‍💻 au boulot"))
        assertEquals("👨‍👩‍👧‍👦", premierEmoji("Famille 👨‍👩‍👧‍👦"))
        assertEquals("❤️", premierEmoji("❤️ toi"))
        assertEquals("👍🏽", premierEmoji("👍🏽 ok"))
        assertEquals("1️⃣", premierEmoji("Top 1️⃣"))
        assertEquals("🏴󠁧󠁢󠁳󠁣󠁴󠁿", premierEmoji("🏴󠁧󠁢󠁳󠁣󠁴󠁿 Écosse"))
        assertNull(premierEmoji("Prix 12 $"))
    }
}
