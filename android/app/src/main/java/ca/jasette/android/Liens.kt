package ca.jasette.android

import android.content.Context
import android.content.SharedPreferences
import android.graphics.Bitmap
import android.graphics.BitmapFactory
import android.util.Log
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateMapOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import org.json.JSONObject
import java.io.ByteArrayOutputStream
import java.io.File
import java.io.InputStream
import java.net.HttpURLConnection
import java.net.Inet4Address
import java.net.Inet6Address
import java.net.InetAddress
import java.net.URI
import java.net.URL
import java.security.MessageDigest
import java.util.concurrent.Executors

/**
 * L'aperçu d'un lien (PROTOCOLE.md, §5.9) : `link_preview` d'un message
 * (fourni par le service), ou fait par Jasette. La vignette : [image] (un
 * fichier local), [imageUrl] (à aller chercher) ou [imageMedia] (une pièce à
 * demander au pont).
 */
data class LienApercu(
    val url: String,
    val title: String,
    val description: String,
    val site: String,
    val image: String = "",
    val imageUrl: String = "",
    val imageMedia: Piece? = null,
) {
    fun json(): JSONObject {
        val o = JSONObject().put("url", url).put("title", title).put("description", description).put("site", site)
        if (image.isNotEmpty()) o.put("image", image)
        if (imageUrl.isNotEmpty()) o.put("image_url", imageUrl)
        imageMedia?.let { o.put("image_media", it.json()) }
        return o
    }

    /** Le domaine affiché sous la carte. */
    val domaine get() = Liens.hote(url)?.removePrefix("www.") ?: ""

    /**
     * Aucune source d'image (ni fichier, ni adresse, ni pièce) : Messenger en
     * envoie ainsi pour un reel Facebook partagé. Jasette cherche alors elle-même
     * l'aperçu de [url] pour en prendre l'image ([completePar]).
     */
    val sansImage get() = image.isEmpty() && imageUrl.isEmpty() && imageMedia == null

    /**
     * Cet aperçu (du service) complété par celui que Jasette a cherché : l'image
     * de [c], et ses titre, description et site là où le service n'en a pas mis.
     * Sans [c] (pas encore trouvé) : celui-ci tel quel.
     */
    fun completePar(c: LienApercu?): LienApercu {
        if (c == null) return this
        return LienApercu(
            url = url.ifBlank { c.url },
            title = title.ifBlank { c.title },
            description = description.ifBlank { c.description },
            site = site.ifBlank { c.site },
            image = c.image,
        )
    }

    companion object {
        fun de(o: JSONObject?): LienApercu? {
            if (o == null) return null
            val a = LienApercu(
                url = o.chaine("url"),
                title = o.chaine("title"),
                description = o.chaine("description"),
                site = o.chaine("site"),
                image = o.chaine("image"),
                imageUrl = o.chaine("image_url"),
                imageMedia = o.optJSONObject("image_media")?.let { Piece.de(it) }?.takeIf { it.mediaId.isNotEmpty() },
            )
            return a.takeIf { it.url.isNotEmpty() && (it.title.isNotEmpty() || it.description.isNotEmpty()) }
        }
    }
}

/** Les liens dans le texte, et les aperçus faits nous-mêmes (sans serveur Jasette). */
object Liens {
    private const val ETIQUETTE = "jasette-liens"
    private const val DELAI_MS = 5000
    private const val HTML_MAX = 2 shl 20
    private const val IMAGE_MAX = 8 shl 20
    private const val LARGEUR = 640
    /**
     * Facebook et Instagram refusent leurs pages à un navigateur non connecté,
     * mais donnent leurs balises au robot d'aperçus de Facebook : pour leurs
     * pages seulement, on se présente comme lui.
     */
    private const val UA_ROBOT_FB = "facebookexternalhit/1.1 (+http://www.facebook.com/externalhit_uatext.php)"

    private fun uaPour(hote: String): String {
        val h = hote.lowercase().trimEnd('.')
        val meta = listOf("facebook.com", "fb.com", "fb.watch", "instagram.com").any { h == it || h.endsWith(".$it") }
        return if (meta) UA_ROBOT_FB else UA
    }

    private const val UA = "Mozilla/5.0 (Linux; Android 14) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Mobile Safari/537.36"

    private fun horsLien(c: Char): Boolean =
        c.isWhitespace() || c.isISOControl() || c in "<>\"`«»“”‘’，。、「」" ||
            Character.isSurrogate(c) || c.code in 0x2600..0x27BF || c.code in 0x2B00..0x2BFF ||
            c.code == 0xFE0F || c.code == 0x200D || c.code == 0x200B || c.code == 0x2060

    /**
     * Les adresses du texte (http(s) et www.) : leur plage et l'adresse à ouvrir
     * (`www.…` devient `https://www.…`), sans la ponctuation finale.
     */
    /**
     * [texte] sans le lien qui mène à [url] (sinon sans le premier lien), et
     * sans les espaces laissés autour.
     */
    fun sansLien(texte: String, url: String): String {
        val liens = trouver(texte)
        val (plage, _) = liens.firstOrNull { it.second == url || it.second.trimEnd('/') == url.trimEnd('/') }
            ?: liens.firstOrNull() ?: return texte
        // La ponctuation qui suivait l'adresse seule (« Lis ça : https://… . »)
        // ne veut plus rien dire ; un « : » qui l'annonçait non plus.
        var avant = texte.substring(0, plage.first).trimEnd()
        var apres = texte.substring(plage.last + 1).trimStart()
        if (apres.all { it in ".,;:!?…" }) apres = ""
        if (apres.isEmpty()) avant = avant.trimEnd(':', ' ')
        return "$avant $apres".trim()
    }

    fun trouver(texte: String): List<Pair<IntRange, String>> {
        val r = ArrayList<Pair<IntRange, String>>()
        val bas = texte.lowercase(java.util.Locale.ROOT)
        if (bas.length != texte.length) return r
        var i = 0
        while (i < texte.length) {
            val www = when {
                bas.startsWith("https://", i) || bas.startsWith("http://", i) -> false
                bas.startsWith("www.", i) -> true
                else -> null
            }
            val p = if (i > 0) texte[i - 1] else ' '
            val frontiere = !(p.isLetterOrDigit() || p in ".@/-_")
            if (www == null || !frontiere) {
                i++
                continue
            }
            var fin = i
            while (fin < texte.length && !horsLien(texte[fin])) fin++
            while (fin > i) {
                val c = texte[fin - 1]
                val morceau = texte.substring(i, fin)
                val enlever = when (c) {
                    '.', ',', ';', ':', '!', '?', '\'', '…', '*', '_' -> true
                    ')' -> morceau.count { it == '(' } < morceau.count { it == ')' }
                    ']' -> morceau.count { it == '[' } < morceau.count { it == ']' }
                    '}' -> morceau.count { it == '{' } < morceau.count { it == '}' }
                    else -> false
                }
                if (!enlever) break
                fin--
            }
            val brut = texte.substring(i, fin)
            val sansSchema = if (www) brut else brut.substringAfter("://", "")
            val hote = sansSchema.split('/', '?', '#').first()
            val valide = if (www) {
                hote.length > 4 && hote.substring(4).contains('.') && !hote.endsWith('.')
            } else {
                hote.any { it.isLetterOrDigit() }
            }
            if (valide) {
                r.add(i until fin to (if (www) "https://$brut" else brut))
                i = maxOf(fin, i + 1)
            } else {
                i += 4
            }
        }
        return r
    }

    fun premier(texte: String): String? {
        if (!texte.contains("://") && !texte.contains("www.", ignoreCase = true)) return null
        return trouver(texte).firstOrNull()?.second
    }

    fun hote(url: String): String? = try {
        URI(url).host?.lowercase()?.takeIf { it.isNotEmpty() }
    } catch (_: Exception) {
        url.substringAfter("://").split('/', '?', '#', ':').firstOrNull()?.lowercase()?.takeIf { it.isNotEmpty() }
    }

    // ---- Les garde-fous du réseau ----

    /** Les adresses qu'un aperçu ne joint jamais (réseau local, boucle, plages réservées). */
    fun ipInterdite(a: InetAddress): Boolean {
        if (a.isLoopbackAddress || a.isSiteLocalAddress || a.isLinkLocalAddress || a.isAnyLocalAddress || a.isMulticastAddress) return true
        val o = a.address.map { it.toInt() and 0xff }
        return when (a) {
            is Inet4Address -> o[0] == 0 || (o[0] == 100 && o[1] in 64..127) || (o[0] == 192 && o[1] == 0 && o[2] == 0) ||
                (o[0] == 198 && o[1] in 18..19) || o[0] >= 240 || (o[0] == 169 && o[1] == 254)
            is Inet6Address -> (o[0] and 0xfe) == 0xfc || (o[0] == 0xfe && (o[1] and 0xc0) == 0x80) ||
                (o[0] == 0xfe && (o[1] and 0xc0) == 0xc0) || (o[0] == 0 && o[1] == 0x64 && o[2] == 0xff && o[3] == 0x9b) ||
                (o.subList(0, 10).all { it == 0 } && o[10] == 0xff && o[11] == 0xff &&
                    ipInterdite(InetAddress.getByAddress(a.address.copyOfRange(12, 16))))
            else -> true
        }
    }

    /** http(s), un vrai nom d'hôte, pas d'identifiants, et un nom qui ne mène à aucune adresse interdite. */
    private fun acceptable(u: URL): Boolean {
        if (u.protocol != "http" && u.protocol != "https") return false
        if (u.userInfo != null) return false
        val h = (u.host ?: return false).lowercase().trimEnd('.').removePrefix("[").removeSuffix("]")
        if (h.isEmpty() || h == "localhost" || h.endsWith(".localhost") || h.endsWith(".local") || h.endsWith(".internal")) return false
        if (!h.contains('.') && !h.contains(':')) return false
        return try {
            val adresses = InetAddress.getAllByName(h)
            adresses.isNotEmpty() && adresses.none { ipInterdite(it) }
        } catch (_: Exception) {
            false
        }
    }

    /** GET avec les garde-fous : 5 s, 5 redirections au plus, chaque adresse vérifiée. */
    private fun ouvrir(url: String, accepte: String): HttpURLConnection? {
        var u = URL(url)
        repeat(6) {
            if (!acceptable(u)) return null
            val c = (u.openConnection(java.net.Proxy.NO_PROXY) as HttpURLConnection).apply {
                instanceFollowRedirects = false
                connectTimeout = DELAI_MS
                readTimeout = DELAI_MS
                setRequestProperty("User-Agent", uaPour(u.host ?: ""))
                setRequestProperty("Accept", accepte)
                setRequestProperty("Accept-Language", "fr-CA,fr;q=0.9,en;q=0.8")
            }
            val code = c.responseCode
            if (code in 300..399) {
                val loc = c.getHeaderField("Location")
                c.disconnect()
                if (loc.isNullOrEmpty()) return null
                u = URL(u, loc)
                return@repeat
            }
            if (code !in 200..299) {
                c.disconnect()
                return null
            }
            return c
        }
        return null
    }

    private fun lire(e: InputStream, max: Int): ByteArray? {
        val sortie = ByteArrayOutputStream()
        val buf = ByteArray(16 shl 10)
        val debut = System.currentTimeMillis()
        while (sortie.size() <= max) {
            if (System.currentTimeMillis() - debut > DELAI_MS * 2) break
            val n = e.read(buf)
            if (n < 0) break
            sortie.write(buf, 0, n)
        }
        return if (sortie.size() > max && max == IMAGE_MAX) null else sortie.toByteArray().let { if (it.size > max) it.copyOf(max) else it }
    }

    fun cle(url: String): String =
        MessageDigest.getInstance("SHA-256").digest(url.toByteArray()).take(16).joinToString("") { "%02x".format(it.toInt() and 0xff) }

    /**
     * L'aperçu d'une page, du cache (7 jours ; 6 h pour un échec) ou du réseau.
     * null : pas d'aperçu. Bloquant : hors du fil principal.
     */
    fun chercher(url: String, dossier: File): LienApercu? {
        val k = cle(url)
        val fichier = File(dossier, "$k.json")
        try {
            if (fichier.isFile) {
                val o = JSONObject(fichier.readText())
                val a = LienApercu.de(o.optJSONObject("apercu"))
                val duree = if (a != null) 7 * 24 * 3600_000L else 6 * 3600_000L
                if (System.currentTimeMillis() - o.optLong("quand") < duree && (a == null || a.image.isEmpty() || File(a.image).isFile)) return a
            }
        } catch (_: Exception) {
        }
        val a = try {
            lirePage(url, dossier, k)
        } catch (e: Exception) {
            Log.i(ETIQUETTE, "pas d'aperçu : ${e.javaClass.simpleName}")
            null
        }
        try {
            dossier.mkdirs()
            val o = JSONObject().put("quand", System.currentTimeMillis())
            if (a != null) o.put("apercu", a.json())
            val tmp = File(dossier, "$k.tmp")
            tmp.writeText(o.toString())
            tmp.renameTo(fichier)
        } catch (_: Exception) {
        }
        return a
    }

    private fun lirePage(url: String, dossier: File, k: String): LienApercu? {
        val c = ouvrir(url, "text/html,application/xhtml+xml;q=0.9,*/*;q=0.5") ?: return null
        try {
            val ct = (c.contentType ?: "").substringBefore(';').trim().lowercase()
            if (ct.isNotEmpty() && ct != "text/html" && ct != "application/xhtml+xml") return null
            val html = String(c.inputStream.use { lire(it, HTML_MAX) } ?: return null, Charsets.UTF_8)
            val a = balises(html, c.url.toString())
            if (a.title.isEmpty() && a.description.isEmpty()) return null
            val img = if (a.imageUrl.isNotEmpty()) image(a.imageUrl, File(dossier, "$k.jpg")) else null
            return a.copy(url = url, image = img ?: "", imageUrl = "")
        } finally {
            c.disconnect()
        }
    }

    /** Une image (8 Mio au plus), réduite à 640 px de large, en JPEG dans [sortie]. */
    fun image(url: String, sortie: File): String? {
        if (sortie.isFile) return sortie.path
        val c = try {
            ouvrir(url, "image/webp,image/png,image/jpeg,image/*;q=0.8")
        } catch (_: Exception) {
            null
        } ?: return null
        try {
            val brut = c.inputStream.use { lire(it, IMAGE_MAX) } ?: return null
            val o = BitmapFactory.Options().apply { inJustDecodeBounds = true }
            BitmapFactory.decodeByteArray(brut, 0, brut.size, o)
            if (o.outWidth <= 0 || o.outHeight <= 0) return null
            var echantillon = 1
            while (o.outWidth / (echantillon * 2) >= LARGEUR) echantillon *= 2
            val bmp = BitmapFactory.decodeByteArray(brut, 0, brut.size, BitmapFactory.Options().apply { inSampleSize = echantillon })
                ?: return null
            val reduite = if (bmp.width > LARGEUR) {
                Bitmap.createScaledBitmap(bmp, LARGEUR, maxOf(1, bmp.height * LARGEUR / bmp.width), true)
            } else {
                bmp
            }
            sortie.parentFile?.mkdirs()
            val tmp = File(sortie.path + ".tmp")
            tmp.outputStream().use { reduite.compress(Bitmap.CompressFormat.JPEG, 82, it) }
            if (!tmp.renameTo(sortie)) return null
            return sortie.path
        } catch (_: Exception) {
            return null
        } finally {
            c.disconnect()
        }
    }

    // ---- Les balises OpenGraph et Twitter card ----

    private val reMeta = Regex("<meta\\b([^>]*)>", RegexOption.IGNORE_CASE)
    private val reAttr = Regex("([a-zA-Z:_-]+)\\s*=\\s*(\"([^\"]*)\"|'([^']*)'|([^\\s\"'>]+))")
    private val reTitre = Regex("<title[^>]*>(.*?)</title", setOf(RegexOption.IGNORE_CASE, RegexOption.DOT_MATCHES_ALL))

    private fun entites(s: String): String =
        if (!s.contains('&')) s else android.text.Html.fromHtml(s, android.text.Html.FROM_HTML_MODE_LEGACY).toString()

    private fun couper(s: String, n: Int): String {
        val t = s.trim().split(Regex("\\s+")).joinToString(" ")
        return if (t.length <= n) t else t.take(n - 1).trimEnd() + "…"
    }

    fun balises(html: String, base: String): LienApercu {
        val metas = LinkedHashMap<String, String>()
        for (m in reMeta.findAll(html)) {
            val attrs = HashMap<String, String>()
            for (a in reAttr.findAll(m.groupValues[1])) {
                attrs[a.groupValues[1].lowercase()] = a.groupValues[3].ifEmpty { a.groupValues[4].ifEmpty { a.groupValues[5] } }
            }
            val prop = (attrs["property"] ?: attrs["name"] ?: attrs["itemprop"])?.trim()?.lowercase() ?: continue
            val contenu = attrs["content"]?.trim()?.takeIf { it.isNotEmpty() } ?: continue
            metas.putIfAbsent(prop, entites(contenu))
        }
        fun premier(vararg cles: String) = cles.firstNotNullOfOrNull { metas[it] } ?: ""
        var titre = premier("og:title", "twitter:title")
        if (titre.isEmpty()) titre = reTitre.find(html)?.groupValues?.get(1)?.let { entites(it) } ?: ""
        var image = premier("og:image:secure_url", "og:image", "og:image:url", "twitter:image", "twitter:image:src", "image")
        if (image.isNotEmpty()) {
            image = try {
                val u = URL(URL(base), image)
                if (u.protocol == "http" || u.protocol == "https") u.toString() else ""
            } catch (_: Exception) {
                ""
            }
        }
        val site = premier("og:site_name", "application-name").ifEmpty { hote(base)?.removePrefix("www.") ?: "" }
        return LienApercu(
            url = base, title = couper(titre, 300), description = couper(premier("og:description", "twitter:description", "description"), 500),
            site = couper(site, 100), imageUrl = image,
        )
    }
}

/**
 * Les aperçus de liens de l'interface : ceux que Jasette a faits (adresse →
 * aperçu ; null : en cours ou aucun), les images `image_url` des services, et
 * le réglage « Aperçus des liens ».
 */
object Apercus {
    val pages = mutableStateMapOf<String, LienApercu?>()
    val images = mutableStateMapOf<String, String?>()
    private val fils = Executors.newFixedThreadPool(3) { Thread(it, "apercus").apply { isDaemon = true } }
    private val principal = android.os.Handler(android.os.Looper.getMainLooper())
    private var prefs: SharedPreferences? = null
    private var dossier: File? = null

    /** Réglages > Apparence > « Aperçus des liens » (activé par défaut). */
    var actifs by mutableStateOf(true)
        private set

    fun charger(ctx: Context) {
        prefs = ctx.applicationContext.getSharedPreferences("jasette_apercus", Context.MODE_PRIVATE)
        actifs = prefs?.getBoolean("actifs", true) ?: true
        dossier = File(ctx.applicationContext.cacheDir, "apercus")
    }

    fun choisir(oui: Boolean) {
        actifs = oui
        prefs?.edit()?.putBoolean("actifs", oui)?.apply()
    }

    /**
     * Ce qu'il faut chercher pour ces messages (les plus récents d'abord) : la
     * vignette des aperçus du service, et l'aperçu de la première adresse des
     * autres (si le réglage le permet). À appeler sur le fil principal.
     */
    fun demander(msgs: List<Msg>, serveur: ConnexionServeur?) {
        val d = dossier ?: return
        for (m in msgs.asReversed().take(80)) {
            val lp = m.apercu
            if (lp != null) {
                if (lp.image.isNotEmpty()) continue
                // Un aperçu du service sans aucune image (un reel Facebook partagé dans
                // Messenger) : on cherche celui de son adresse, pour l'image.
                if (lp.sansImage) {
                    if (!actifs) continue
                    val u = adresse(lp, m.text) ?: continue
                    if (pages.containsKey(u)) continue
                    pages[u] = null
                    fils.execute {
                        val a = page(u, serveur, d)
                        principal.post { pages[u] = a }
                    }
                    continue
                }
                val piece = lp.imageMedia
                if (piece != null) {
                    Depot.telecharger(piece)
                } else if (lp.imageUrl.isNotEmpty() && !images.containsKey(lp.imageUrl)) {
                    val u = lp.imageUrl
                    images[u] = null
                    fils.execute {
                        val p = Liens.image(u, File(d, "i-${Liens.cle(u)}.jpg"))
                        principal.post { images[u] = p }
                    }
                }
                continue
            }
            if (!actifs) continue
            val u = Liens.premier(m.text) ?: continue
            if (pages.containsKey(u)) continue
            pages[u] = null
            fils.execute {
                val a = page(u, serveur, d)
                principal.post { pages[u] = a }
            }
        }
    }

    /** Par le serveur Jasette s'il répond, sinon nous-mêmes. */
    private fun page(u: String, serveur: ConnexionServeur?, d: File): LienApercu? {
        if (serveur != null && !serveur.ferme) {
            try {
                val r = serveur.apercu(u)
                return if (r == null) null else LienApercu.de(r.put("url", u))
            } catch (_: Exception) {
                // Pas de réponse (serveur d'avant les aperçus, coupure) : on le fait ici.
            }
        }
        return Liens.chercher(u, d)
    }

    /** L'adresse dont chercher l'aperçu quand celui du service n'a pas d'image : la sienne, sinon la première du texte. */
    private fun adresse(lp: LienApercu, texte: String): String? = lp.url.trim().ifEmpty { null } ?: Liens.premier(texte)

    /** L'aperçu à montrer sous ce message, et le fichier de sa vignette. */
    fun de(m: Msg): Pair<LienApercu, String?>? {
        val lp = m.apercu
        if (lp != null && lp.sansImage && actifs) {
            // Sans image du service : celle de l'aperçu cherché par Jasette, s'il y en a un.
            val a = lp.completePar(adresse(lp, m.text)?.let { pages[it] })
            return a to a.image.ifEmpty { null }
        }
        if (lp != null) {
            val img = when {
                lp.image.isNotEmpty() -> lp.image
                lp.imageMedia != null -> Depot.medias[lp.imageMedia.mediaId]
                lp.imageUrl.isNotEmpty() -> images[lp.imageUrl]
                else -> null
            }
            return lp to img
        }
        if (!actifs) return null
        val a = pages[Liens.premier(m.text) ?: return null] ?: return null
        return a to a.image.ifEmpty { null }
    }
}
