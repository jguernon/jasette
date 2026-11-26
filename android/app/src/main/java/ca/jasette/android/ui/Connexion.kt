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
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.imePadding
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material3.Button
import androidx.compose.material3.ButtonDefaults
import androidx.compose.material3.OutlinedTextField
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.input.PasswordVisualTransformation
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
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

    // X : la même page et les mêmes cookies que connexion/jasette-connexion-x.py.
    const val X_DEBUT = "https://x.com/i/flow/login"
    val X_DOMAINES = listOf("https://x.com/", "https://twitter.com/", "https://api.x.com/")
    val X_REQUIS = setOf("auth_token", "ct0")
    // twid donne l'identifiant du compte ; les autres font ressembler le pont au navigateur.
    val X_VOULUS = X_REQUIS + setOf(
        "twid", "kdt", "att", "guest_id", "guest_id_ads", "guest_id_marketing",
        "personalization_id", "lang", "dnt", "d_prefs", "__cf_bm", "gt",
    )

    fun estX(url: String) = listOf(
        "https://x.com/", "https://www.x.com/", "https://twitter.com/",
        "https://www.twitter.com/", "https://mobile.x.com/", "https://mobile.twitter.com/",
    ).any { url.startsWith(it) }

    // Reddit : la même page et les mêmes cookies que connexion/jasette-connexion-reddit.py.
    // Une fois connecté, la page du chat, pour que Reddit y pose ses cookies de chat.
    const val REDDIT_DEBUT = "https://www.reddit.com/login/"
    const val REDDIT_CHAT = "https://www.reddit.com/chat/"
    val REDDIT_DOMAINES = listOf("https://www.reddit.com/", "https://reddit.com/", "https://chat.reddit.com/")
    val REDDIT_REQUIS = setOf("reddit_session")
    val REDDIT_VOULUS = REDDIT_REQUIS + setOf("csrf_token", "token_v2", "loid", "session_tracker", "edgebucket", "rdt", "csv")

    // LinkedIn : la même page et les mêmes cookies que connexion/jasette-connexion-linkedin.py.
    const val LINKEDIN_DEBUT = "https://www.linkedin.com/login"
    val LINKEDIN_DOMAINES = listOf("https://www.linkedin.com/", "https://linkedin.com/")
    val LINKEDIN_REQUIS = setOf("li_at", "JSESSIONID")
    val LINKEDIN_VOULUS = LINKEDIN_REQUIS + setOf(
        "bcookie", "bscookie", "lidc", "li_mc", "liap", "li_rm", "lang", "li_theme", "li_theme_set",
        "timezone", "li_sugr", "UserMatchHistory", "AnalyticsSyncHistory", "lms_ads", "lms_analytics", "fptctx2", "__cf_bm",
    )
    // Les pages où la connexion n'est pas finie.
    private val LINKEDIN_EN_COURS = listOf("/login", "/checkpoint", "/uas/", "/authwall", "/signup")

    fun linkedinConnecte(url: String): Boolean {
        val chemin = when {
            url.startsWith("https://www.linkedin.com/") -> url.removePrefix("https://www.linkedin.com")
            url.startsWith("https://linkedin.com/") -> url.removePrefix("https://linkedin.com")
            else -> return false
        }
        return LINKEDIN_EN_COURS.none { chemin.startsWith(it) }
    }

    fun estReddit(url: String) = listOf(
        "https://www.reddit.com/", "https://reddit.com/", "https://old.reddit.com/",
        "https://new.reddit.com/", "https://chat.reddit.com/", "https://sh.reddit.com/",
    ).any { url.startsWith(it) }

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
    // X : une fois les cookies lus, la fenêtre web laisse la place au code X Chat.
    var cookiesX by remember { mutableStateOf<Map<String, String>?>(null) }
    val c = cookiesX
    if (c != null) {
        CodeXChat(c, fini)
        return
    }
    val principal = remember { Handler(Looper.getMainLooper()) }
    val etat = remember { object { var fini = false; var vue: WebView? = null; var chatOuvert = false; var chatCharge = false } }

    // Vérifie les cookies : sur Google, une fois arrivé sur messages.google.com ;
    // sur Meta, dès que les cookies requis existent (la page de connexion a la
    // même adresse que la boîte de réception).
    fun verifier() {
        if (etat.fini) return
        val url = etat.vue?.url ?: return
        if (service == Service.LINKEDIN) {
            // Connecté : li_at et JSESSIONID, hors des pages de connexion et de vérification.
            if (!Pages.linkedinConnecte(url)) return
            val c = lireCookies(Pages.LINKEDIN_DOMAINES, Pages.LINKEDIN_VOULUS)
            if (!c.keys.containsAll(Pages.LINKEDIN_REQUIS)) return
            etat.fini = true
            // L'agent de cette WebView : le pont le reprend (pas un cookie).
            Depot.connecterMeta(Service.LINKEDIN, c + ("jasette_user_agent" to Pages.AGENT_META), "linkedin")
            fini()
        } else if (service == Service.REDDIT) {
            if (!Pages.estReddit(url)) return
            val c = lireCookies(Pages.REDDIT_DOMAINES, Pages.REDDIT_VOULUS)
            if (!c.keys.containsAll(Pages.REDDIT_REQUIS)) return
            // Connecté : d'abord la page du chat (cookies csrf_token et token_v2 à jour).
            if (!etat.chatOuvert) {
                etat.chatOuvert = true
                if (!url.startsWith(Pages.REDDIT_CHAT)) {
                    principal.post { etat.vue?.loadUrl(Pages.REDDIT_CHAT) }
                    return
                }
                etat.chatCharge = true
            }
            if (!etat.chatCharge || (!c.containsKey("csrf_token") && !c.containsKey("token_v2"))) return
            etat.fini = true
            Depot.connecterMeta(Service.REDDIT, c, "reddit")
            fini()
        } else if (service == Service.X) {
            if (!Pages.estX(url)) return
            val c = lireCookies(Pages.X_DOMAINES, Pages.X_VOULUS)
            // Connecté (pas seulement visiteur) : auth_token et ct0, et plus sur la page de connexion.
            if (c.keys.containsAll(Pages.X_REQUIS) && !url.contains("/i/flow/login")) {
                etat.fini = true
                principal.post { cookiesX = c }
            }
        } else if (service == Service.GOOGLE) {
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
        Service.X -> t("Connexion à X")
        Service.REDDIT -> t("Connexion à Reddit")
        Service.LINKEDIN -> t("Connexion à LinkedIn")
        // WhatsApp et Signal s'associent par code (Association.kt) : jamais ici.
        else -> service.court
    }
    Scaffold(
        containerColor = P.fond,
        topBar = {
            BarreHaut(
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
                        override fun onPageFinished(view: WebView?, url: String?) {
                            if (etat.chatOuvert && url?.startsWith(Pages.REDDIT_CHAT) == true) etat.chatCharge = true
                            verifier()
                        }
                        override fun doUpdateVisitedHistory(view: WebView?, url: String?, reload: Boolean) = verifier()
                    }
                    loadUrl(
                        when (service) {
                            Service.GOOGLE -> Pages.GOOGLE
                            Service.X -> Pages.X_DEBUT
                            Service.REDDIT -> Pages.REDDIT_DEBUT
                            Service.LINKEDIN -> Pages.LINKEDIN_DEBUT
                            else -> Pages.META_DEBUT.getValue(plateforme)
                        },
                    )
                }
            },
        )
    }
}

/**
 * X, deuxième étape : le code d'accès X Chat (4 chiffres), envoyé avec les
 * cookies dans un seul `login`. Un seul essai : X verrouille X Chat après
 * quelques codes faux, donc rien n'est renvoyé tout seul.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
private fun CodeXChat(cookies: Map<String, String>, fini: () -> Unit) {
    var code by remember { mutableStateOf("") }
    Scaffold(
        containerColor = P.ecran,
        topBar = {
            BarreHaut(
                navigationIcon = { IconButton(onClick = fini) { Icon(Icons.Filled.Close, t("Fermer")) } },
                title = { Text(t("Code d'accès X Chat")) },
                colors = barreCouleurs(),
            )
        },
    ) { marges ->
        Column(
            Modifier.fillMaxSize().padding(marges).defilementAvecOmbre().imePadding().padding(16.dp),
            verticalArrangement = Arrangement.spacedBy(14.dp),
        ) {
            Column(
                Modifier.fillMaxWidth().carte(16.dp).padding(16.dp),
                verticalArrangement = Arrangement.spacedBy(12.dp),
            ) {
                Text(
                    t("X chiffre tes messages privés. Pour que Jasette puisse les lire, entre le code d'accès (4 chiffres) choisi dans X Chat. Jasette ne le garde pas."),
                    color = P.texte, fontSize = 14.sp,
                )
                Text(t("X verrouille X Chat après quelques codes faux."), color = P.danger, fontSize = 14.sp, fontWeight = FontWeight.SemiBold)
                OutlinedTextField(
                    code, { v -> code = v.filter { it.isDigit() }.take(4) },
                    label = { Text(t("Code d'accès")) },
                    singleLine = true,
                    visualTransformation = PasswordVisualTransformation(),
                    keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.NumberPassword),
                    shape = formeChamp,
                    colors = couleursChamp(),
                )
                Button(
                    onClick = {
                        Depot.connecterX(cookies, code)
                        fini()
                    },
                    enabled = code.length == 4,
                    shape = formeBouton,
                    colors = ButtonDefaults.buttonColors(containerColor = P.accentPlein, contentColor = P.surAccent),
                ) { Text(t("Continuer")) }
                Text(
                    t("Pas encore de code ? Active d'abord X Chat dans l'application X ou sur x.com/messages."),
                    color = P.textePale, fontSize = 13.sp,
                )
            }
        }
    }
}
