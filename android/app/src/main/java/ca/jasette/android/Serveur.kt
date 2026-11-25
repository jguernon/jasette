package ca.jasette.android

import android.content.Context
import android.os.Build
import android.provider.Settings
import android.util.Base64
import android.util.Log
import org.json.JSONArray
import org.json.JSONObject
import java.io.BufferedReader
import java.io.BufferedWriter
import java.io.File
import java.io.FileInputStream
import java.io.FileOutputStream
import java.io.IOException
import java.io.InputStreamReader
import java.io.OutputStreamWriter
import java.net.InetAddress
import java.net.InetSocketAddress
import java.net.Socket
import java.security.MessageDigest
import java.security.SecureRandom
import java.security.cert.CertificateException
import java.security.cert.X509Certificate
import java.util.concurrent.ConcurrentHashMap
import java.util.concurrent.ExecutorService
import java.util.concurrent.Executors
import java.util.concurrent.LinkedBlockingQueue
import java.util.concurrent.RejectedExecutionException
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicLong
import javax.crypto.Mac
import javax.crypto.spec.SecretKeySpec
import javax.net.ssl.SSLContext
import javax.net.ssl.SSLSocket
import javax.net.ssl.X509TrustManager

/*
 * Le client du serveur Jasette (`jasette-serveur`, voir serveur/PROTOCOLE.md).
 * Traduction de src/distant.rs (bureau) :
 *
 * - une seule connexion TLS 1.3 par serveur, partagée par les services ;
 * - certificat auto-signé épinglé par son empreinte (SHA-256 du DER), aucune AC ;
 * - reconnexion automatique (1, 2, 4… 30 s ; 60 s après un refus) ;
 * - les chemins de fichiers du serveur sont téléchargés (`get`) dans un cache
 *   local et réécrits ; `send_media` téléverse d'abord le fichier (`put`).
 *
 * Le jeton est un secret : il ne passe jamais dans les journaux.
 */

private const val ETIQUETTE = "jasette-serveur"
private const val VERSION_PROTOCOLE = "jasette/1"
const val PORT_PAR_DEFAUT = 47800
private const val SILENCE_MAX_MS = 90_000
private const val ACCUEIL_MS = 20_000
private const val FRAICHEUR_MS = 600_000L
private const val MORCEAU_DEPOT = 256 shl 10
private val CLES_CHEMINS = setOf("path", "avatar", "photo", "image", "thumb", "fichier")

/** Ce qu'on garde d'un serveur jumelé (stockage privé de l'app, MODE_PRIVATE). */
data class ConfigServeur(
    /** « hôte:port » (ou « hôte », port 47800). */
    val adresse: String,
    /** SHA-256 du certificat du serveur, hexadécimal minuscule (épinglé). */
    val empreinte: String,
    /** Le jeton de cet appareil : secret, comme un mot de passe. */
    val jeton: String,
    /** Le nom sous lequel le serveur connaît cet appareil. */
    val nomAppareil: String,
) {
    // Jamais le jeton dans un journal.
    override fun toString() = "ConfigServeur(adresse=$adresse, empreinte=$empreinte, jeton=…, nomAppareil=$nomAppareil)"

    companion object {
        private const val PREFS = "serveur"

        fun lire(ctx: Context): ConfigServeur? {
            val p = ctx.getSharedPreferences(PREFS, Context.MODE_PRIVATE)
            if (p.getString("mode", "") != "client") return null
            val adresse = p.getString("adresse", "") ?: ""
            val empreinte = p.getString("empreinte", "") ?: ""
            val jeton = p.getString("jeton", "") ?: ""
            if (adresse.isEmpty() || empreinte.length != 64 || jeton.isEmpty()) return null
            return ConfigServeur(adresse, empreinte, jeton, p.getString("nom", "") ?: "")
        }

        /** null : « Sur cet appareil » (et le jeton est effacé). */
        fun garder(ctx: Context, c: ConfigServeur?) {
            val e = ctx.getSharedPreferences(PREFS, Context.MODE_PRIVATE).edit().clear()
            if (c != null) {
                e.putString("mode", "client").putString("adresse", c.adresse).putString("empreinte", c.empreinte)
                    .putString("jeton", c.jeton).putString("nom", c.nomAppareil)
            }
            e.commit()
        }
    }
}

/** Le résultat de [Jumelage.verifier], à faire confirmer avant de jumeler. */
data class Verification(val adresse: String, val code: String, val empreinte: String, val codeVerification: String) {
    override fun toString() = "Verification(adresse=$adresse, empreinte=$empreinte)"
}

/** Une connexion ratée : [empreinte] ou [refus] → on attend 60 s avant de réessayer. */
class ErreurLien(message: String, val empreinte: Boolean = false, val refus: Boolean = false) : IOException(message)

// ---------------------------------------------------------------------------
// Jumelage (§3 du protocole)
// ---------------------------------------------------------------------------

object Jumelage {
    /**
     * Étape 1 : se brancher sans épinglage, retenir l'empreinte, lire l'accueil
     * et fermer. Rien de secret n'est envoyé. Bloquant : hors du fil principal.
     */
    fun verifier(adresse: String, code: String): Verification {
        val c = normaliserCode(code)
        val a = ouvrir(adresse, null)
        a.lien.fermer()
        return Verification(adresse.trim(), c, a.empreinte, codeVerification(c, a.empreinte))
    }

    /** Étape 2 : jumeler en exigeant l'empreinte vue à l'étape 1. Bloquant. */
    fun jumeler(v: Verification, nom: String): ConfigServeur {
        val a = ouvrir(v.adresse, v.empreinte)
        try {
            val nc = Base64.encodeToString(ByteArray(32).also { SecureRandom().nextBytes(it) }, Base64.NO_WRAP)
            val preuve = hmac(v.code, "$VERSION_PROTOCOLE/client|${a.empreinte}|${a.nonce}|$nc")
            a.lien.ecrire(
                JSONObject().put("jumeler", JSONObject().put("nom", nom.take(60)).put("nonce", nc).put("preuve", hex(preuve)))
                    .toString(),
            )
            val r = a.lien.lire()
            erreurDe(r)?.let { throw ErreurLien(t(it), refus = true) }
            val j = r.optJSONObject("jumele") ?: throw IOException(t("réponse inattendue du serveur"))
            val jeton = j.chaine("jeton")
            if (jeton.isEmpty()) throw IOException(t("réponse inattendue du serveur"))
            // Le serveur prouve à son tour qu'il connaissait le code.
            val attendue = hmac(v.code, "$VERSION_PROTOCOLE/serveur|${a.empreinte}|${a.nonce}|$nc")
            if (!MessageDigest.isEqual(attendue, deHex(j.chaine("preuve")))) {
                throw IOException(t("le serveur n'a pas prouvé qu'il connaissait le code"))
            }
            return ConfigServeur(v.adresse, a.empreinte, jeton, j.chaine("nom").ifEmpty { nom })
        } finally {
            a.lien.fermer()
        }
    }

    fun normaliserCode(code: String): String {
        val c = code.filter { it in '0'..'9' }
        if (c.length != 6) throw IOException(t("le code de jumelage a 6 chiffres"))
        return c
    }

    /** SHA-256("jasette/1/verification|code|empreinte") → 4 octets grand-boutistes mod 1 000 000. */
    fun codeVerification(code: String, empreinte: String): String {
        val s = MessageDigest.getInstance("SHA-256").digest("$VERSION_PROTOCOLE/verification|$code|$empreinte".toByteArray())
        val n = ((s[0].toLong() and 0xff) shl 24) or ((s[1].toLong() and 0xff) shl 16) or
            ((s[2].toLong() and 0xff) shl 8) or (s[3].toLong() and 0xff)
        return "%06d".format(n % 1_000_000)
    }

    /** Le nom de ce téléphone (celui des réglages Android, sinon le modèle). */
    fun nomAppareil(ctx: Context): String {
        val n = try {
            Settings.Global.getString(ctx.contentResolver, Settings.Global.DEVICE_NAME)
        } catch (_: Exception) {
            null
        }
        return (n?.takeIf { it.isNotBlank() } ?: "${Build.MANUFACTURER} ${Build.MODEL}").trim().take(60)
    }
}

private fun hmac(code: String, message: String): ByteArray {
    val m = Mac.getInstance("HmacSHA256")
    m.init(SecretKeySpec(code.toByteArray(Charsets.US_ASCII), "HmacSHA256"))
    return m.doFinal(message.toByteArray(Charsets.UTF_8))
}

private fun hex(b: ByteArray) = b.joinToString("") { "%02x".format(it.toInt() and 0xff) }

private fun deHex(s: String): ByteArray =
    ByteArray(s.length / 2) { i -> (s.substring(2 * i, 2 * i + 2).toIntOrNull(16) ?: 0).toByte() }

private fun erreurDe(v: JSONObject): String? {
    if (!v.has("erreur")) return null
    return v.chaine("texte").ifEmpty { v.chaine("erreur") }
}

// ---------------------------------------------------------------------------
// TLS épinglé
// ---------------------------------------------------------------------------

/** Sépare « hôte », « hôte:port », « [ipv6]:port » ou une IPv6 nue. */
fun hotePort(adresse: String): Pair<String, Int> {
    val a = adresse.trim()
    if (a.isEmpty()) throw IOException(t("adresse vide"))
    fun port(p: String) = p.toIntOrNull()?.takeIf { it in 1..65535 } ?: throw IOException("${t("port invalide")} : $p")
    if (a.startsWith("[")) {
        val f = a.indexOf(']')
        if (f < 0) throw IOException("${t("adresse invalide")} : $a")
        val reste = a.substring(f + 1)
        return a.substring(1, f) to if (reste.startsWith(":")) port(reste.drop(1)) else PORT_PAR_DEFAUT
    }
    return when (a.count { it == ':' }) {
        1 -> a.substringBefore(':') to port(a.substringAfter(':'))
        else -> a to PORT_PAR_DEFAUT
    }
}

/** Une connexion TLS : lignes JSON dans les deux sens. */
private class Lien(private val sock: SSLSocket) {
    private val entree = BufferedReader(InputStreamReader(sock.inputStream, Charsets.UTF_8), 64 shl 10)
    private val sortie = BufferedWriter(OutputStreamWriter(sock.outputStream, Charsets.UTF_8), 64 shl 10)

    @Synchronized
    fun ecrire(ligne: String) {
        sortie.write(ligne)
        sortie.write("\n")
        sortie.flush()
    }

    /** Une ligne, ou null si la connexion est fermée. */
    fun ligne(): String? = entree.readLine()

    fun lire(): JSONObject = JSONObject(ligne() ?: throw IOException(t("connexion fermée")))

    fun delai(ms: Int) {
        sock.soTimeout = ms
    }

    fun fermer() {
        try {
            sock.close()
        } catch (_: Exception) {
        }
    }
}

private class Accueil(val lien: Lien, val empreinte: String, val nonce: String)

/**
 * TCP + TLS 1.3, avec l'empreinte [attendue] exigée (ou seulement retenue si
 * null), puis lecture de l'accueil du serveur. Bloquant.
 */
private fun ouvrir(adresse: String, attendue: String?): Accueil {
    val (hote, port) = hotePort(adresse)
    val ips = try {
        InetAddress.getAllByName(hote).toList()
    } catch (e: Exception) {
        throw ErreurLien("${t("serveur injoignable")} : $hote ${t("introuvable")}")
    }
    var brut: Socket? = null
    var derniere = ""
    for (ip in ips) {
        val s = Socket()
        try {
            s.connect(InetSocketAddress(ip, port), 10_000)
            brut = s
            break
        } catch (e: Exception) {
            derniere = e.message ?: e.toString()
            try {
                s.close()
            } catch (_: Exception) {
            }
        }
    }
    if (brut == null) throw ErreurLien("${t("serveur injoignable")} : $derniere")
    brut.tcpNoDelay = true
    brut.soTimeout = ACCUEIL_MS

    var vue: String? = null
    // Pas d'autorité de certification : seule l'empreinte compte.
    val epinglage = object : X509TrustManager {
        override fun checkClientTrusted(chain: Array<out X509Certificate>?, authType: String?) =
            throw CertificateException("client")

        override fun checkServerTrusted(chain: Array<out X509Certificate>?, authType: String?) {
            val feuille = chain?.firstOrNull() ?: throw CertificateException("pas de certificat")
            val e = hex(MessageDigest.getInstance("SHA-256").digest(feuille.encoded))
            vue = e
            if (attendue != null && e != attendue) throw CertificateException("empreinte du serveur inattendue")
        }

        override fun getAcceptedIssuers(): Array<X509Certificate> = arrayOf()
    }
    val sock: SSLSocket
    try {
        val ctx = SSLContext.getInstance("TLS")
        ctx.init(null, arrayOf(epinglage), SecureRandom())
        // « jasette-serveur » : le nom SNI (ignoré par le serveur).
        sock = ctx.socketFactory.createSocket(brut, "jasette-serveur", port, true) as SSLSocket
        if ("TLSv1.3" in sock.supportedProtocols) sock.enabledProtocols = arrayOf("TLSv1.3")
        sock.startHandshake()
    } catch (e: Exception) {
        try {
            brut.close()
        } catch (_: Exception) {
        }
        val v = vue
        if (attendue != null && v != null && v != attendue) {
            throw ErreurLien(
                t("le certificat du serveur a changé : connexion refusée. S'il a été réinstallé, jumelle de nouveau."),
                empreinte = true,
            )
        }
        throw ErreurLien("${t("serveur injoignable")} : ${e.message ?: e}")
    }
    val empreinte = vue ?: run {
        sock.close()
        throw ErreurLien(t("pas de certificat"))
    }
    val lien = Lien(sock)
    val bonjour = try {
        lien.lire()
    } catch (e: Exception) {
        lien.fermer()
        throw ErreurLien("${t("serveur injoignable")} : ${e.message ?: e}")
    }
    if (bonjour.chaine("jasette") != "serveur") {
        lien.fermer()
        throw ErreurLien(t("ce n'est pas un serveur Jasette"))
    }
    return Accueil(lien, empreinte, bonjour.chaine("nonce"))
}

// ---------------------------------------------------------------------------
// La connexion partagée (§4 à §6)
// ---------------------------------------------------------------------------

/**
 * La connexion à un serveur jumelé, partagée par les [PontDistant]. Elle se
 * branche et se rebranche toute seule dans son propre fil.
 *
 * [surMarques] reçoit, dans l'ordre et sur le fil des événements, chaque
 * trame `marques` (§5.5 : le contenu de {"marques":{…}}), ou null quand la
 * connexion tombe.
 */
class ConnexionServeur(
    private val config: ConfigServeur,
    private val cache: File,
    /** Reçoit la liste `services` de chaque `auth ok` (sur le fil de connexion). */
    private val surServices: (List<String>) -> Unit = {},
    private val surMarques: (JSONObject?) -> Unit = {},
) {
    private val abonnes = ConcurrentHashMap<String, (JSONObject) -> Unit>()
    private val verrou = Any()
    /** La connexion en cours (null pendant une coupure) et les commandes gardées en attendant. */
    private var lien: Lien? = null
    private val enAttente = ArrayList<String>()
    /** Réponses attendues (fichiers, dépôts), par identifiant de requête. */
    private val attentes = ConcurrentHashMap<Long, LinkedBlockingQueue<JSONObject>>()
    private val prochain = AtomicLong(1)
    @Volatile var ferme = false
        private set
    @Volatile private var dejaConnecte = false
    /** Les services offerts par le serveur (après l'auth). */
    @Volatile var services: List<String> = emptyList()
        private set
    private val vus = ConcurrentHashMap<String, Pair<File, Long>>()
    // Les écritures (commandes venues du fil principal) passent par un seul fil.
    private val ecrivain: ExecutorService = Executors.newSingleThreadExecutor { Thread(it, "serveur-ecriture") }
    // Les événements : réécriture des chemins (téléchargements) puis remise, dans l'ordre.
    private val traitement: ExecutorService = Executors.newSingleThreadExecutor { Thread(it, "serveur-evenements") }

    init {
        Thread(::boucle, "serveur-connexion").start()
    }

    private fun executer(ex: ExecutorService, r: () -> Unit) {
        try {
            ex.execute(r)
        } catch (_: RejectedExecutionException) {
        }
    }

    private fun lienCourant(): Lien? = synchronized(verrou) { lien }

    /** [surNouvelle] reçoit les événements de [svc] (sur un fil à part). */
    fun abonner(svc: String, surNouvelle: (JSONObject) -> Unit) {
        val l = lienCourant()
        abonnes[svc] = surNouvelle
        if (l == null) {
            val etat = if (dejaConnecte) "reconnecting" else "connecting"
            executer(traitement) { surNouvelle(JSONObject().put("ev", "state").put("state", etat)) }
        } else {
            // Déjà branché : le serveur a rejoué l'état avant notre arrivée.
            envoyerLigne(JSONObject().put("rejouer", svc).toString())
        }
    }

    fun desabonner(svc: String) {
        abonnes.remove(svc)
        if (abonnes.isEmpty()) fermer()
    }

    fun fermer() {
        ferme = true
        lienCourant()?.fermer()
        ecrivain.shutdown()
        traitement.shutdown()
    }

    private fun vers(svc: String, ev: JSONObject) {
        abonnes[svc]?.invoke(ev)
    }

    private fun versTous(ev: JSONObject) {
        executer(traitement) { for (cb in abonnes.values) cb(JSONObject(ev.toString())) }
    }

    /** Envoie une ligne (depuis n'importe quel fil), ou la garde pour la prochaine connexion. */
    private fun envoyerLigne(ligne: String) {
        executer(ecrivain) {
            val l = synchronized(verrou) {
                lien ?: run {
                    if (enAttente.size >= 500) enAttente.removeAt(0)
                    enAttente.add(ligne)
                    null
                }
            } ?: return@executer
            try {
                l.ecrire(ligne)
            } catch (_: Exception) {
                l.fermer()
                synchronized(verrou) { enAttente.add(ligne) }
            }
        }
    }

    /**
     * Envoie une trame `marques` tout de suite, sans la garder pendant une
     * coupure (le rejeu suivant dira où en est le serveur). Faux si coupée.
     */
    fun envoyerMarques(trame: JSONObject): Boolean {
        val l = lienCourant() ?: return false
        val ligne = trame.toString()
        executer(ecrivain) {
            try {
                l.ecrire(ligne)
            } catch (_: Exception) {
                l.fermer()
            }
        }
        return true
    }

    /** Une commande pour le pont [svc] du serveur. */
    fun envoyer(svc: String, commande: JSONObject) {
        when (commande.chaine("cmd")) {
            // Le pont appartient au serveur.
            "quit" -> {}
            "send_media" -> {
                if (commande.chaine("path").isEmpty()) {
                    envoyerLigne(JSONObject().put("svc", svc).put("cmd", commande).toString())
                    return
                }
                val cmd = JSONObject(commande.toString())
                Thread({
                    try {
                        cmd.put("path", televerser(File(cmd.chaine("path"))))
                        envoyerLigne(JSONObject().put("svc", svc).put("cmd", cmd).toString())
                    } catch (e: Exception) {
                        Log.w(ETIQUETTE, "téléversement : ${e.message}")
                        executer(traitement) {
                            vers(svc, JSONObject().put("ev", "error").put("where", "send").put("text", e.message ?: "?"))
                            vers(
                                svc,
                                JSONObject().put("ev", "sent").put("conv", cmd.opt("conv")).put("tmp_id", cmd.opt("tmp_id"))
                                    .put("ok", false),
                            )
                        }
                    }
                }, "serveur-televersement").start()
            }
            else -> envoyerLigne(JSONObject().put("svc", svc).put("cmd", commande).toString())
        }
    }

    private fun connecter(): Lien {
        val a = ouvrir(config.adresse, config.empreinte)
        try {
            a.lien.ecrire(JSONObject().put("auth", JSONObject().put("jeton", config.jeton)).toString())
            val r = a.lien.lire()
            erreurDe(r)?.let { throw ErreurLien(t(it), refus = true) }
            if (r.chaine("auth") != "ok") throw ErreurLien(t("réponse inattendue du serveur"))
            services = r.optJSONArray("services").chaines()
            a.lien.delai(SILENCE_MAX_MS)
            return a.lien
        } catch (e: ErreurLien) {
            a.lien.fermer()
            throw e
        } catch (e: Exception) {
            a.lien.fermer()
            throw ErreurLien("${t("serveur injoignable")} : ${e.message ?: e}")
        }
    }

    /** Le fil de la connexion : se brancher, lire, se rebrancher. */
    private fun boucle() {
        var attente = 1000L
        var erreurDite = ""
        var etatDit = false
        while (!ferme) {
            val l = try {
                connecter()
            } catch (e: ErreurLien) {
                val texte = e.message ?: ""
                if (texte != erreurDite) {
                    Log.w(ETIQUETTE, "serveur ${config.adresse} : $texte")
                    if (e.empreinte || e.refus) {
                        versTous(JSONObject().put("ev", "error").put("where", "serveur").put("text", texte))
                    }
                    erreurDite = texte
                }
                if (!etatDit) {
                    versTous(JSONObject().put("ev", "state").put("state", if (dejaConnecte) "reconnecting" else "connecting"))
                    etatDit = true
                }
                if (e.empreinte || e.refus) attente = 60_000
                dormir(attente)
                if (attente < 30_000) attente = minOf(attente * 2, 30_000)
                continue
            }
            if (ferme) {
                l.fermer()
                break
            }
            Log.i(ETIQUETTE, "branché au serveur ${config.adresse}")
            attente = 1000
            erreurDite = ""
            dejaConnecte = true
            val gardees = synchronized(verrou) {
                lien = l
                ArrayList(enAttente).also { enAttente.clear() }
            }
            // Après `lien` : un service ajouté maintenant redemande son état (« rejouer »).
            surServices(services)
            for (svc in abonnes.keys) {
                if (svc !in services) {
                    executer(traitement) {
                        vers(svc, JSONObject().put("ev", "error").put("where", "serveur").put("text", "${t("le serveur n'offre pas")} $svc"))
                    }
                }
            }
            gardees.forEach { envoyerLigne(it) }
            try {
                while (!ferme) {
                    val ligne = l.ligne() ?: break
                    if (ligne.isNotBlank()) trame(ligne, l)
                }
            } catch (_: Exception) {
            }
            synchronized(verrou) { lien = null }
            l.fermer()
            // Les requêtes de fichiers en cours échouent tout de suite.
            val perdue = JSONObject().put("erreur", t("connexion perdue")).put("fin", true)
            attentes.values.forEach { it.offer(perdue) }
            attentes.clear()
            executer(traitement) { surMarques(null) }
            if (ferme) break
            Log.w(ETIQUETTE, "serveur ${config.adresse} : connexion perdue")
            versTous(JSONObject().put("ev", "state").put("state", "reconnecting"))
            etatDit = true
            dormir(attente)
            attente = minOf(attente * 2, 30_000)
        }
    }

    private fun dormir(ms: Long) {
        val fin = System.currentTimeMillis() + ms
        while (!ferme && System.currentTimeMillis() < fin) Thread.sleep(200)
    }

    private fun trame(ligne: String, l: Lien) {
        val v = try {
            JSONObject(ligne)
        } catch (_: Exception) {
            Log.w(ETIQUETTE, "trame illisible du serveur")
            return
        }
        when {
            v.has("svc") -> {
                val svc = v.chaine("svc")
                val ev = v.optJSONObject("ev") ?: return
                executer(traitement) {
                    if (!abonnes.containsKey(svc)) return@executer
                    reecrire(ev)
                    vers(svc, ev)
                }
            }
            v.has("marques") -> {
                val m = v.optJSONObject("marques") ?: return
                executer(traitement) { surMarques(m) }
            }
            v.has("ping") -> {
                val p = v.get("ping")
                executer(ecrivain) {
                    try {
                        l.ecrire(JSONObject().put("pong", p).toString())
                    } catch (_: Exception) {
                        l.fermer()
                    }
                }
            }
            v.has("fichier") || v.has("depose") -> {
                val r = v.optJSONObject("fichier") ?: v.optJSONObject("depose") ?: return
                attentes[r.optLong("id", -1)]?.offer(r)
            }
        }
    }

    /** Remplace, à toute profondeur, les chemins du serveur par des copies locales. */
    private fun reecrire(x: Any?) {
        when (x) {
            is JSONObject -> for (k in x.keys().asSequence().toList()) {
                val valeur = x.opt(k)
                if (valeur is String && valeur.startsWith("/") && k in CLES_CHEMINS) {
                    telecharger(valeur)?.let { x.put(k, it.path) }
                } else {
                    reecrire(valeur)
                }
            }
            is JSONArray -> for (i in 0 until x.length()) reecrire(x.opt(i))
        }
    }

    private fun attendre(): Pair<Long, LinkedBlockingQueue<JSONObject>> {
        val id = prochain.getAndIncrement()
        val q = LinkedBlockingQueue<JSONObject>()
        attentes[id] = q
        return id to q
    }

    /** Copie locale d'un fichier du serveur (rien n'est renvoyé s'il n'a pas changé). */
    private fun telecharger(distant: String): File? {
        vus[distant]?.let { (f, quand) ->
            if (System.currentTimeMillis() - quand < FRAICHEUR_MS && f.isFile) return f
        }
        val nom = nomLocal(distant)
        val local = File(cache, nom)
        val l = lienCourant() ?: return null
        val (id, q) = attendre()
        var sortie: FileOutputStream? = null
        val partiel = File(cache, ".$nom.part")
        try {
            val connu = if (local.isFile) sha256Fichier(local) else ""
            l.ecrire(
                JSONObject().put("get", JSONObject().put("id", id).put("chemin", distant).put("sha256", connu)).toString(),
            )
            cache.mkdirs()
            while (true) {
                val m = q.poll(120, TimeUnit.SECONDS) ?: throw IOException("délai dépassé")
                m.chaine("erreur").let { if (it.isNotEmpty()) throw IOException(it) }
                if (m.optBoolean("inchange")) break
                if (sortie == null) sortie = FileOutputStream(partiel)
                val d = m.chaine("donnees")
                if (d.isNotEmpty()) sortie.write(Base64.decode(d, Base64.DEFAULT))
                if (m.optBoolean("fin")) {
                    sortie.close()
                    sortie = null
                    if (!partiel.renameTo(local)) throw IOException("renommage")
                    break
                }
            }
            vus[distant] = local to System.currentTimeMillis()
            return local
        } catch (e: Exception) {
            Log.w(ETIQUETTE, "fichier du serveur non reçu : ${e.message}")
            partiel.delete()
            return null
        } finally {
            try {
                sortie?.close()
            } catch (_: Exception) {
            }
            attentes.remove(id)
        }
    }

    /** Téléverse un fichier local ; rend son chemin sur le serveur. Bloquant. */
    private fun televerser(local: File): String {
        val taille = local.length()
        val l = lienCourant() ?: throw IOException(t("serveur injoignable"))
        val (id, q) = attendre()
        try {
            FileInputStream(local).use { f ->
                val morceau = ByteArray(MORCEAU_DEPOT)
                var envoye = 0L
                var premier = true
                while (true) {
                    var n = 0
                    while (n < morceau.size) {
                        val k = f.read(morceau, n, morceau.size - n)
                        if (k < 0) break
                        n += k
                    }
                    envoye += n
                    val fin = n < morceau.size || envoye >= taille
                    val put = JSONObject().put("id", id)
                        .put("donnees", Base64.encodeToString(morceau, 0, n, Base64.NO_WRAP)).put("fin", fin)
                    if (premier) put.put("nom", local.name)
                    premier = false
                    l.ecrire(JSONObject().put("put", put).toString())
                    // Le serveur ne répond qu'à la fin, ou en cas d'erreur.
                    q.poll()?.chaine("erreur")?.let { if (it.isNotEmpty()) throw IOException(it) }
                    if (fin) break
                }
            }
            val m = q.poll(120, TimeUnit.SECONDS) ?: throw IOException(t("le serveur n'a pas confirmé le dépôt"))
            m.chaine("erreur").let { if (it.isNotEmpty()) throw IOException(it) }
            return m.chaine("chemin").ifEmpty { throw IOException("dépôt sans chemin") }
        } finally {
            attentes.remove(id)
        }
    }

    companion object {
        /** Le cache local des fichiers d'un serveur : <files>/distant/<hôte>. */
        fun dossierCache(ctx: Context, adresse: String): File {
            val hote = adresse.trim().map { if (it.isLetterOrDigit() || it == '.' || it == '-') it else '_' }.joinToString("")
            return File(ctx.filesDir, "distant/$hote").apply { mkdirs() }
        }
    }
}

/** Nom local d'un fichier du serveur : SHA-256 du chemin (16 octets) et son extension. */
private fun nomLocal(distant: String): String {
    val h = hex(MessageDigest.getInstance("SHA-256").digest(distant.toByteArray()).copyOf(16))
    val ext = distant.substringAfterLast('/').let { if ('.' in it) it.substringAfterLast('.') else "" }
        .filter { it.isLetterOrDigit() && it.code < 128 }.take(8)
    return if (ext.isEmpty()) h else "$h.$ext"
}

private fun sha256Fichier(f: File): String = try {
    val md = MessageDigest.getInstance("SHA-256")
    FileInputStream(f).use { e ->
        val buf = ByteArray(64 shl 10)
        while (true) {
            val n = e.read(buf)
            if (n < 0) break
            md.update(buf, 0, n)
        }
    }
    hex(md.digest())
} catch (_: Exception) {
    ""
}

// ---------------------------------------------------------------------------
// Le pont distant d'un service
// ---------------------------------------------------------------------------

/** Un service servi par le serveur Jasette : même usage qu'un [PontLocal]. */
class PontDistant(
    override val nom: String,
    private val cx: ConnexionServeur,
    surNouvelle: (JSONObject) -> Unit,
) : Pont {
    @Volatile private var arrete = false

    init {
        cx.abonner(nom, surNouvelle)
    }

    override fun envoyer(commande: JSONObject) {
        if (!arrete) cx.envoyer(nom, commande)
    }

    override fun vivant() = !arrete && !cx.ferme

    /** Le pont appartient au serveur : on se débranche seulement. */
    override fun arreter() {
        if (arrete) return
        arrete = true
        cx.desabonner(nom)
    }
}
