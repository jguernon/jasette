package ca.jasette.android

import android.app.PendingIntent
import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
import android.content.pm.ApplicationInfo
import android.content.pm.PackageInfo
import android.content.pm.PackageInstaller
import android.content.pm.PackageManager
import android.net.Uri
import android.os.Build
import android.provider.Settings
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.core.content.IntentCompat
import androidx.core.content.pm.PackageInfoCompat
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.ensureActive
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import java.io.File
import java.net.HttpURLConnection
import java.net.URL
import java.security.MessageDigest

/**
 * La mise à jour dans l'app, pour l'APK publié sur jonathanguernon.net/jasette :
 *
 *  1. [verifier] lit `version.json` (HTTPS, délais courts ; hors ligne, rien ne
 *     se passe) au démarrage, au plus une fois toutes les 6 heures, ou par le
 *     bouton « Chercher une mise à jour » des réglages ;
 *  2. une version plus récente (versionCode plus grand) montre une carte en haut
 *     de la liste : Mettre à jour ou Plus tard (cachée jusqu'à la version suivante) ;
 *  3. Mettre à jour télécharge l'APK dans le cache de l'app (jamais tout seul :
 *     seulement quand on touche le bouton, réseau mobile ou non), vérifie son
 *     SHA-256, son nom de paquet et son certificat (le même que l'app installée),
 *     puis l'installe par une session [PackageInstaller]. Android demande la
 *     confirmation ; la permission « installer des apps inconnues » est demandée
 *     d'abord, par l'écran des réglages d'Android.
 *
 * Une version debug (installée par adb, signée par la clé de débogage) ne se
 * met pas à jour elle-même : le bouton dit seulement qu'une release existe.
 */
object MiseAJour {
    private const val PREFS = "mise_a_jour"
    private const val CLE_VERIF = "derniere_verif"
    private const val CLE_IGNOREE = "ignoree"
    private const val CLE_JSON = "version_json"
    private const val INTERVALLE = 6 * 60 * 60 * 1000L
    private const val TAILLE_MAX = 400L * 1024 * 1024
    private const val ACTION_STATUT = "ca.jasette.android.MISE_A_JOUR_STATUT"

    sealed interface Etat {
        data object Repos : Etat
        data object Recherche : Etat
        /** Réponse au bouton : rien de plus récent. */
        data object AJour : Etat
        data class Erreur(val message: String) : Etat
        /** [fraction] de 0 à 1, ou -1 si la taille est inconnue. */
        data class Telechargement(val fraction: Float) : Etat
        data object Verification : Etat
        /** L'APK est vérifié ; il manque « installer des apps inconnues ». */
        data object Permission : Etat
        /** La session est envoyée : Android demande la confirmation. */
        data object Installation : Etat
    }

    private val portee = CoroutineScope(SupervisorJob() + Dispatchers.IO)
    private var app: Context? = null
    private var tache: Job? = null

    /** La version publiée la plus récente connue (gardée entre deux lancements). */
    var disponible by mutableStateOf<VersionDispo?>(null)
        private set
    var etat by mutableStateOf<Etat>(Etat.Repos)
        private set
    /** Le versionCode remis à plus tard (« Plus tard ») : sa carte reste cachée. */
    var ignoree by mutableStateOf(0L)
        private set

    var codeInstalle = 0L
        private set
    var nomInstalle = ""
        private set
    /** Une version debug (adb) : pas de mise à jour par l'app. */
    var debug = false
        private set

    fun charger(ctx: Context) {
        val a = ctx.applicationContext
        app = a
        debug = (a.applicationInfo.flags and ApplicationInfo.FLAG_DEBUGGABLE) != 0
        try {
            val pi = a.packageManager.getPackageInfo(a.packageName, 0)
            codeInstalle = PackageInfoCompat.getLongVersionCode(pi)
            nomInstalle = pi.versionName.orEmpty()
        } catch (_: Exception) {}
        val p = prefs(a)
        ignoree = p.getLong(CLE_IGNOREE, 0L)
        disponible = p.getString(CLE_JSON, null)?.let { VersionDispo.lire(it) }
            ?.takeIf { VersionDispo.plusRecente(it, codeInstalle) }
        // Les APK déjà installés (ou abandonnés) ne restent pas dans le cache.
        if (disponible == null) dossier(a).deleteRecursively()
    }

    private fun prefs(ctx: Context) = ctx.getSharedPreferences(PREFS, Context.MODE_PRIVATE)
    private fun dossier(ctx: Context) = File(ctx.cacheDir, "mise-a-jour")
    private fun fichier(ctx: Context, v: VersionDispo) = File(dossier(ctx), "jasette-${v.versionCode}.apk")

    /** La carte en haut de la liste : une release plus récente, pas remise à plus tard. */
    val carteVisible: Boolean
        get() {
            val d = disponible ?: return false
            if (debug || !VersionDispo.plusRecente(d, codeInstalle)) return false
            return d.versionCode != ignoree || etat.enCours()
        }

    private fun Etat.enCours() =
        this is Etat.Telechargement || this == Etat.Verification || this == Etat.Permission || this == Etat.Installation

    /** Au démarrage : au plus une vérification toutes les 6 heures, jamais en debug. */
    fun auDemarrage(ctx: Context) {
        if (debug) return
        val derniere = prefs(ctx.applicationContext).getLong(CLE_VERIF, 0L)
        val maintenant = System.currentTimeMillis()
        if (maintenant - derniere in 0 until INTERVALLE) return
        verifier(ctx, manuel = false)
    }

    /** Lit version.json. [manuel] : le bouton des réglages (on dit le résultat, même « à jour »). */
    fun verifier(ctx: Context, manuel: Boolean) {
        val a = ctx.applicationContext
        if (tache?.isActive == true || etat.enCours()) return
        if (manuel) etat = Etat.Recherche
        tache = portee.launch {
            val v = try {
                lireVersion()
            } catch (_: Exception) {
                null
            }
            withContext(Dispatchers.Main) {
                if (v == null) {
                    // Hors ligne ou fichier illisible : silencieux au démarrage.
                    if (manuel) etat = Etat.Erreur(t("Impossible de joindre jonathanguernon.net. Réessaie plus tard."))
                    return@withContext
                }
                prefs(a).edit().putLong(CLE_VERIF, System.currentTimeMillis())
                    .putString(CLE_JSON, v.second).apply()
                val d = v.first
                disponible = d.takeIf { VersionDispo.plusRecente(it, codeInstalle) }
                if (manuel) {
                    etat = if (disponible == null) Etat.AJour else Etat.Repos
                    // Le bouton montre de nouveau une version remise à plus tard.
                    if (disponible != null) remettre(a, 0L)
                }
            }
        }
    }

    /** version.json et son texte ; null si la réponse n'est pas un 200 lisible. */
    private fun lireVersion(): Pair<VersionDispo, String>? {
        // Le paramètre contourne les caches entre l'app et le site.
        val c = ouvrir("${VersionDispo.ADRESSE_VERSION}?t=${System.currentTimeMillis() / 60_000}")
        try {
            if (c.responseCode != 200) return null
            val octets = c.inputStream.use { it.readNBytesCompat(64 * 1024) }
            val texte = String(octets, Charsets.UTF_8)
            return VersionDispo.lire(texte)?.let { it to texte }
        } finally {
            c.disconnect()
        }
    }

    private fun java.io.InputStream.readNBytesCompat(max: Int): ByteArray {
        val sortie = java.io.ByteArrayOutputStream()
        val tampon = ByteArray(8192)
        while (sortie.size() < max) {
            val n = read(tampon, 0, minOf(tampon.size, max - sortie.size()))
            if (n < 0) break
            sortie.write(tampon, 0, n)
        }
        return sortie.toByteArray()
    }

    /** Une connexion HTTPS vers le site, délais courts. Les redirections ne quittent pas HTTPS. */
    private fun ouvrir(adresse: String): HttpURLConnection {
        require(VersionDispo.adresseSure(adresse.substringBefore('?')))
        val c = URL(adresse).openConnection() as HttpURLConnection
        c.connectTimeout = 6_000
        c.readTimeout = 10_000
        c.instanceFollowRedirects = false
        c.setRequestProperty("User-Agent", "Jasette-Android/$nomInstalle")
        c.setRequestProperty("Cache-Control", "no-cache")
        return c
    }

    /** « Plus tard » : la carte se cache jusqu'à la version suivante. */
    fun plusTard() {
        val d = disponible ?: return
        app?.let { remettre(it, d.versionCode) }
    }

    private fun remettre(ctx: Context, code: Long) {
        ignoree = code
        prefs(ctx).edit().putLong(CLE_IGNOREE, code).apply()
    }

    /** Le réseau actif est-il facturé (mobile) ? Seulement pour le dire sur le bouton. */
    fun reseauMobile(ctx: Context): Boolean = try {
        val cm = ctx.getSystemService(android.net.ConnectivityManager::class.java)
        cm?.isActiveNetworkMetered == true
    } catch (_: Exception) {
        false
    }

    /** Mettre à jour (le bouton) : télécharger, vérifier, installer. */
    fun mettreAJour(ctx: Context) {
        val a = ctx.applicationContext
        val d = disponible ?: return
        if (debug || tache?.isActive == true) return
        etat = Etat.Telechargement(-1f)
        tache = portee.launch {
            val f = fichier(a, d)
            val erreur = try {
                if (!f.isFile || sha256(f) != d.sha256) telecharger(d, f)
                withContext(Dispatchers.Main) { etat = Etat.Verification }
                verifierApk(a, d, f)
            } catch (e: kotlinx.coroutines.CancellationException) {
                throw e
            } catch (e: Exception) {
                t("Le téléchargement a échoué. Vérifie ta connexion et réessaie.")
            }
            withContext(Dispatchers.Main) {
                if (erreur != null) {
                    f.delete()
                    etat = Etat.Erreur(erreur)
                } else {
                    installer(a)
                }
            }
        }
    }

    private suspend fun telecharger(d: VersionDispo, f: File) {
        f.parentFile?.let { it.deleteRecursively(); it.mkdirs() }
        val c = ouvrir(d.url)
        try {
            c.readTimeout = 30_000
            if (c.responseCode != 200) throw java.io.IOException("HTTP ${c.responseCode}")
            val total = c.contentLengthLong.takeIf { it > 0 } ?: d.taille
            if (total > TAILLE_MAX) throw java.io.IOException("trop gros")
            var lu = 0L
            var dernier = -1
            val partiel = File(f.path + ".partiel")
            c.inputStream.use { entree ->
                partiel.outputStream().use { sortie ->
                    val tampon = ByteArray(64 * 1024)
                    while (true) {
                        kotlin.coroutines.coroutineContext.ensureActive()
                        val n = entree.read(tampon)
                        if (n < 0) break
                        sortie.write(tampon, 0, n)
                        lu += n
                        if (lu > TAILLE_MAX) throw java.io.IOException("trop gros")
                        val pc = if (total > 0) (lu * 100 / total).toInt() else -1
                        if (pc != dernier) {
                            dernier = pc
                            val fr = if (total > 0) (lu.toFloat() / total).coerceIn(0f, 1f) else -1f
                            withContext(Dispatchers.Main) { etat = Etat.Telechargement(fr) }
                        }
                    }
                }
            }
            if (!partiel.renameTo(f)) throw java.io.IOException("renommage")
        } finally {
            c.disconnect()
        }
    }

    /** null si l'APK est bon ; sinon, le message pour l'utilisateur. */
    private fun verifierApk(ctx: Context, d: VersionDispo, f: File): String? {
        if (sha256(f) != d.sha256) {
            return t("Le fichier téléchargé ne correspond pas à son empreinte SHA-256. Rien n'a été installé.")
        }
        val pm = ctx.packageManager
        val flags = if (Build.VERSION.SDK_INT >= 28) PackageManager.GET_SIGNING_CERTIFICATES
        else @Suppress("DEPRECATION") PackageManager.GET_SIGNATURES
        @Suppress("DEPRECATION")
        val archive = pm.getPackageArchiveInfo(f.path, flags)
            ?: return t("Le fichier téléchargé n'est pas un APK valide. Rien n'a été installé.")
        if (archive.packageName != ctx.packageName) {
            return t("Le fichier téléchargé n'est pas Jasette. Rien n'a été installé.")
        }
        if (PackageInfoCompat.getLongVersionCode(archive) <= codeInstalle) {
            return t("Le fichier téléchargé n'est pas plus récent que la version installée.")
        }
        @Suppress("DEPRECATION")
        val installee = pm.getPackageInfo(ctx.packageName, flags)
        val a = certificats(archive)
        val i = certificats(installee)
        if (a.isEmpty() || i.isEmpty() || a.intersect(i).isEmpty()) {
            return t("Cette mise à jour n'est pas signée par le même certificat que la Jasette installée : Android la refuserait. Rien n'a été installé.")
        }
        return null
    }

    /** Les empreintes SHA-256 des certificats de signature (avec leur historique de rotation). */
    private fun certificats(pi: PackageInfo): Set<String> {
        val signatures = if (Build.VERSION.SDK_INT >= 28) {
            val si = pi.signingInfo ?: return emptySet()
            if (si.hasMultipleSigners()) si.apkContentsSigners else si.signingCertificateHistory
        } else {
            @Suppress("DEPRECATION") pi.signatures
        }
        return signatures.orEmpty().map { hex(MessageDigest.getInstance("SHA-256").digest(it.toByteArray())) }.toSet()
    }

    private fun sha256(f: File): String {
        val md = MessageDigest.getInstance("SHA-256")
        f.inputStream().use { e ->
            val tampon = ByteArray(64 * 1024)
            while (true) {
                val n = e.read(tampon)
                if (n < 0) break
                md.update(tampon, 0, n)
            }
        }
        return hex(md.digest())
    }

    private fun hex(b: ByteArray) = b.joinToString("") { "%02x".format(it) }

    /** Jasette peut-elle installer des apps ? (« installer des apps inconnues ») */
    fun installationPermise(ctx: Context): Boolean = ctx.packageManager.canRequestPackageInstalls()

    /** L'écran d'Android « Installer des apps inconnues », pour Jasette. */
    fun ouvrirAutorisation(ctx: Context) {
        val i = Intent(Settings.ACTION_MANAGE_UNKNOWN_APP_SOURCES, Uri.parse("package:${ctx.packageName}"))
            .addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
        try {
            ctx.startActivity(i)
        } catch (_: Exception) {
            ctx.startActivity(Intent(Settings.ACTION_SECURITY_SETTINGS).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK))
        }
    }

    /** Retour des réglages d'Android : la permission est donnée, on installe. */
    fun auRetour(ctx: Context) {
        if (etat == Etat.Permission && installationPermise(ctx)) installer(ctx.applicationContext)
    }

    /** Installe l'APK vérifié par une session PackageInstaller (sur le fil principal). */
    fun installer(ctx: Context) {
        val d = disponible ?: return
        val f = fichier(ctx, d)
        if (!f.isFile) {
            etat = Etat.Repos
            return
        }
        if (!installationPermise(ctx)) {
            etat = Etat.Permission
            return
        }
        etat = Etat.Installation
        portee.launch {
            val erreur = try {
                // L'APK est revérifié juste avant : le cache aurait pu changer.
                verifierApk(ctx, d, f) ?: run {
                    session(ctx, f)
                    null
                }
            } catch (_: Exception) {
                t("L'installation n'a pas pu commencer.")
            }
            if (erreur != null) withContext(Dispatchers.Main) { etat = Etat.Erreur(erreur) }
        }
    }

    private fun session(ctx: Context, f: File) {
        val installeur = ctx.packageManager.packageInstaller
        val params = PackageInstaller.SessionParams(PackageInstaller.SessionParams.MODE_FULL_INSTALL)
        params.setAppPackageName(ctx.packageName)
        params.setSize(f.length())
        val id = installeur.createSession(params)
        installeur.openSession(id).use { s ->
            s.openWrite("jasette.apk", 0, f.length()).use { sortie ->
                f.inputStream().use { it.copyTo(sortie, 64 * 1024) }
                s.fsync(sortie)
            }
            val retour = Intent(ctx, MiseAJourRecepteur::class.java).setAction(ACTION_STATUT)
            val drapeaux = PendingIntent.FLAG_UPDATE_CURRENT or
                (if (Build.VERSION.SDK_INT >= 31) PendingIntent.FLAG_MUTABLE else 0)
            s.commit(PendingIntent.getBroadcast(ctx, id, retour, drapeaux).intentSender)
        }
    }

    /** Le statut de la session (MiseAJourRecepteur). */
    internal fun statut(ctx: Context, intent: Intent) {
        when (intent.getIntExtra(PackageInstaller.EXTRA_STATUS, PackageInstaller.STATUS_FAILURE)) {
            PackageInstaller.STATUS_PENDING_USER_ACTION -> {
                val confirmer = IntentCompat.getParcelableExtra(intent, Intent.EXTRA_INTENT, Intent::class.java)
                if (confirmer != null) {
                    try {
                        ctx.startActivity(confirmer.addFlags(Intent.FLAG_ACTIVITY_NEW_TASK))
                    } catch (_: Exception) {
                        etat = Etat.Erreur(t("L'installation n'a pas pu commencer."))
                    }
                }
            }
            // En cas de succès, Android remplace l'app (et arrête ce processus).
            PackageInstaller.STATUS_SUCCESS -> etat = Etat.Repos
            // Annulée par l'utilisateur : la carte revient, l'APK vérifié reste prêt.
            PackageInstaller.STATUS_FAILURE_ABORTED -> etat = Etat.Repos
            PackageInstaller.STATUS_FAILURE_CONFLICT, PackageInstaller.STATUS_FAILURE_INCOMPATIBLE ->
                etat = Etat.Erreur(t("Android refuse cette mise à jour : elle n'est pas signée par le même certificat que la Jasette installée."))
            else -> {
                val m = intent.getStringExtra(PackageInstaller.EXTRA_STATUS_MESSAGE)
                etat = Etat.Erreur(t("L'installation a échoué.") + (m?.let { " ($it)" } ?: ""))
            }
        }
    }

    /** Efface le message d'erreur ou « à jour ». */
    fun effacerMessage() {
        if (etat is Etat.Erreur || etat == Etat.AJour) etat = Etat.Repos
    }
}

/** Reçoit le statut de la session d'installation ([MiseAJour.session]). */
class MiseAJourRecepteur : BroadcastReceiver() {
    override fun onReceive(context: Context, intent: Intent) {
        MiseAJour.statut(context, intent)
    }
}
