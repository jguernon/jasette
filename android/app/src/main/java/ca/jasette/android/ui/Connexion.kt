package ca.jasette.android.ui

import ca.jasette.android.t
import android.annotation.SuppressLint
import android.os.Handler
import android.os.Looper
import android.webkit.CookieManager
import android.webkit.WebView
import android.webkit.WebViewClient
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.padding
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Close
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.material3.TopAppBar
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.remember
import androidx.compose.ui.Modifier
import androidx.compose.ui.viewinterop.AndroidView
import androidx.webkit.WebSettingsCompat
import androidx.webkit.WebViewFeature
import ca.jasette.android.Depot
import ca.jasette.android.Service

/**
 * La fenêtre de connexion : la même page que les scripts Python du bureau
 * (connexion/jasette-connexion.py et jasette-connexion-meta.py), les mêmes
 * cookies requis, lus par CookieManager, puis envoyés au pont
 * (« google » pour Google, « login » pour Meta).
 *
 * Les cookies de la WebView restent dans le stockage privé de l'app : le
 * service se souvient du compte la fois suivante.
 */
private object Pages {
    // Google : le même chemin que messages.google.com.
    const val GOOGLE = "https://accounts.google.com/AccountChooser?continue=https://messages.google.com/web/config"
    val GOOGLE_VOULUS = setOf("SID", "HSID", "SSID", "APISID", "SAPISID", "OSID", "__Secure-1PSIDTS")
    val GOOGLE_REQUIS = setOf("SID", "HSID", "SSID", "APISID", "SAPISID", "OSID")
    val GOOGLE_DOMAINES = listOf("https://messages.google.com/", "https://www.google.com/", "https://accounts.google.com/")
    // Google refuse parfois les navigateurs « intégrés » : on se présente comme Firefox.
    const val AGENT_GOOGLE = "Mozilla/5.0 (X11; Linux x86_64; rv:140.0) Gecko/20100101 Firefox/140.0"

    val META_DEBUT = mapOf(
        "messenger" to "https://www.messenger.com/?no_redirect=true",
        "facebook" to "https://www.facebook.com/messages/",
        "instagram" to "https://www.instagram.com/direct/inbox/",
    )
    val META_DOMAINES = mapOf(
        "messenger" to listOf("https://www.messenger.com/", "https://messenger.com/", "https://www.facebook.com/"),
        "facebook" to listOf("https://www.facebook.com/", "https://facebook.com/", "https://www.messenger.com/"),
        "instagram" to listOf("https://www.instagram.com/", "https://instagram.com/"),
    )
    val META_VOULUS = setOf(
        "c_user", "xs", "datr", "fr", "sb", "wd", "dpr", "presence",
        "sessionid", "csrftoken", "ds_user_id", "mid", "ig_did", "rur", "shbid", "shbts",
    )
    val META_REQUIS = mapOf(
        "messenger" to setOf("c_user", "xs", "datr"),
        "facebook" to setOf("c_user", "xs", "datr"),
        "instagram" to setOf("sessionid", "csrftoken", "ds_user_id"),
    )
    // Le pont parle à Meta comme Chrome (de bureau : messenger.com refuse les mobiles).
    const val AGENT_META = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) " +
        "Chrome/141.0.0.0 Safari/537.36"

    fun plateformeDe(url: String): String? = when {
        url.startsWith("https://www.instagram.com/") || url.startsWith("https://instagram.com/") -> "instagram"
        url.startsWith("https://www.facebook.com/") || url.startsWith("https://facebook.com/") -> "facebook"
        url.startsWith("https://www.messenger.com/") || url.startsWith("https://messenger.com/") -> "messenger"
        else -> null
    }
}

/** Les cookies voulus pour ces adresses (le premier trouvé l'emporte, comme sur le bureau). */
private fun lireCookies(domaines: List<String>, voulus: Set<String>): Map<String, String> {
    val cm = CookieManager.getInstance()
    val trouves = LinkedHashMap<String, String>()
    for (url in domaines) {
        val brut = cm.getCookie(url) ?: continue
        for (morceau in brut.split(";")) {
            val i = morceau.indexOf('=')
            if (i <= 0) continue
            val nom = morceau.substring(0, i).trim()
            if (nom in voulus && nom !in trouves) trouves[nom] = morceau.substring(i + 1).trim()
        }
    }
    return trouves
}

@SuppressLint("SetJavaScriptEnabled")
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun Connexion(service: Service, plateforme: String, fini: () -> Unit) {
    val principal = remember { Handler(Looper.getMainLooper()) }
    val etat = remember { object { var fini = false; var vue: WebView? = null } }

    // Vérifie les cookies : sur Google, une fois arrivé sur messages.google.com ;
    // sur Meta, dès que les cookies requis existent (la page de connexion a la
    // même adresse que la boîte de réception).
    fun verifier() {
        if (etat.fini) return
        val url = etat.vue?.url ?: return
        if (service == Service.GOOGLE) {
            if (!url.startsWith("https://messages.google.com/")) return
            val c = lireCookies(Pages.GOOGLE_DOMAINES, Pages.GOOGLE_VOULUS)
            if (c.keys.containsAll(Pages.GOOGLE_REQUIS)) {
                etat.fini = true
                Depot.connecterGoogle(c)
                fini()
            }
        } else {
            val plat = Pages.plateformeDe(url) ?: return
            if ((service == Service.INSTAGRAM) != (plat == "instagram")) return
            val c = lireCookies(Pages.META_DOMAINES.getValue(plat), Pages.META_VOULUS)
            if (c.keys.containsAll(Pages.META_REQUIS.getValue(plat))) {
                etat.fini = true
                Depot.connecterMeta(service, c, plat)
                fini()
            }
        }
    }

    // Minuterie : on revérifie toutes les 2 secondes.
    DisposableEffect(Unit) {
        val tic = object : Runnable {
            override fun run() {
                verifier()
                if (!etat.fini) principal.postDelayed(this, 2000)
            }
        }
        principal.postDelayed(tic, 2000)
        onDispose {
            principal.removeCallbacks(tic)
            CookieManager.getInstance().flush()
            etat.vue?.destroy()
        }
    }

    val titre = when (service) {
        Service.GOOGLE -> t("Connexion à Google")
        Service.MESSENGER -> if (plateforme == "facebook") t("Connexion à Facebook") else t("Connexion à Messenger")
        Service.INSTAGRAM -> t("Connexion à Instagram")
        // WhatsApp, Signal et X se connectent sur le serveur : jamais ici.
        else -> service.court
    }
    Scaffold(
        containerColor = P.fond,
        topBar = {
            TopAppBar(
                navigationIcon = { IconButton(onClick = fini) { Icon(Icons.Filled.Close, t("Fermer")) } },
                title = { Text(titre) },
                colors = barreCouleurs(),
            )
        },
    ) { marges ->
        AndroidView(
            modifier = Modifier.fillMaxSize().padding(marges),
            factory = { ctx ->
                WebView(ctx).apply {
                    etat.vue = this
                    settings.javaScriptEnabled = true
                    settings.domStorageEnabled = true
                    settings.useWideViewPort = true
                    settings.loadWithOverviewMode = true
                    settings.userAgentString = if (service == Service.GOOGLE) Pages.AGENT_GOOGLE else Pages.AGENT_META
                    // Sans l'en-tête X-Requested-With, la page ne sait pas qu'elle est dans une WebView.
                    if (WebViewFeature.isFeatureSupported(WebViewFeature.REQUESTED_WITH_HEADER_ALLOW_LIST)) {
                        WebSettingsCompat.setRequestedWithHeaderOriginAllowList(settings, emptySet())
                    }
                    val cm = CookieManager.getInstance()
                    cm.setAcceptCookie(true)
                    cm.setAcceptThirdPartyCookies(this, true)
                    webViewClient = object : WebViewClient() {
                        override fun onPageFinished(view: WebView?, url: String?) = verifier()
                        override fun doUpdateVisitedHistory(view: WebView?, url: String?, reload: Boolean) = verifier()
                    }
                    loadUrl(
                        if (service == Service.GOOGLE) Pages.GOOGLE else Pages.META_DEBUT.getValue(plateforme),
                    )
                }
            },
        )
    }
}
