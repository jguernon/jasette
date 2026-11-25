package ca.jasette.android

import android.content.ContentResolver
import android.content.Intent
import android.net.Uri
import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.SystemBarStyle
import androidx.activity.compose.setContent
import androidx.activity.enableEdgeToEdge
import androidx.core.content.IntentCompat
import androidx.core.content.pm.ShortcutManagerCompat
import ca.jasette.android.ui.EcranPartage
import ca.jasette.android.ui.JasetteTheme

/**
 * La cible du menu de partage d'Android (ACTION_SEND et ACTION_SEND_MULTIPLE) :
 * une fenêtre légère, translucide, qui monte par-dessus l'app d'origine. On y
 * choisit une ou plusieurs conversations récentes, on ajoute un message si on
 * veut, et on envoie ; elle se ferme ensuite et l'app d'origine revient.
 *
 * Ouverte par un raccourci de partage direct ([Raccourcis]), la conversation
 * est déjà choisie (EXTRA_SHORTCUT_ID). Les conversations privées n'y
 * apparaissent jamais.
 */
class PartageActivity : ComponentActivity() {

    override fun onCreate(savedInstanceState: Bundle?) {
        enableEdgeToEdge(
            statusBarStyle = SystemBarStyle.dark(0),
            navigationBarStyle = SystemBarStyle.dark(0),
        )
        super.onCreate(savedInstanceState)
        // Les ponts (ou la connexion au serveur) : déjà en marche, ou lancés ici.
        // Depot.demarrer montre tout de suite la liste gardée sur disque (CacheConvs).
        PontService.canaux(this)
        PontService.lancer(this)
        Depot.demarrer(this)
        Contacts.demarrer(this)
        val contenu = Partage.lire(intent, packageName)
        val choisie = intent.getStringExtra(ShortcutManagerCompat.EXTRA_SHORTCUT_ID)
            ?.takeIf { it.isNotEmpty() && !Marques.estPrivee(it) }
        setContent { JasetteTheme { EcranPartage(contenu, choisie) { finish() } } }
    }

    override fun onStop() {
        CacheConvs.ecrireMaintenant()
        super.onStop()
    }
}

/** Ce qu'une autre app partage : un texte (ou un lien), et des fichiers. */
class Partage(val texte: String, val sujet: String, val uris: List<Uri>) {
    val vide get() = texte.isBlank() && uris.isEmpty()

    companion object {
        fun lire(intent: Intent?, paquet: String): Partage {
            if (intent == null) return Partage("", "", emptyList())
            val textes = buildList {
                intent.getCharSequenceExtra(Intent.EXTRA_TEXT)?.let { add(it.toString()) }
                if (isEmpty()) intent.getCharSequenceArrayListExtra(Intent.EXTRA_TEXT)?.forEach { add(it.toString()) }
            }
            val sujet = intent.getCharSequenceExtra(Intent.EXTRA_SUBJECT)?.toString().orEmpty()
            val uris = LinkedHashSet<Uri>()
            when (intent.action) {
                Intent.ACTION_SEND ->
                    IntentCompat.getParcelableExtra(intent, Intent.EXTRA_STREAM, Uri::class.java)?.let { uris.add(it) }
                Intent.ACTION_SEND_MULTIPLE ->
                    IntentCompat.getParcelableArrayListExtra(intent, Intent.EXTRA_STREAM, Uri::class.java)?.let { uris.addAll(it) }
            }
            if (uris.isEmpty()) {
                intent.clipData?.let { cd -> for (i in 0 until cd.itemCount) cd.getItemAt(i).uri?.let { uris.add(it) } }
            }
            return Partage(
                texte = textes.joinToString("\n").trim(),
                sujet = sujet.trim(),
                uris = uris.filter { sure(it, paquet) },
            )
        }

        /**
         * Seulement les content:// des autres apps : jamais un file:// ni un
         * fichier de Jasette elle-même (une app malveillante pourrait sinon
         * faire envoyer nos fichiers privés à un contact).
         */
        private fun sure(u: Uri, paquet: String): Boolean =
            u.scheme == ContentResolver.SCHEME_CONTENT &&
                u.authority?.let { it != "$paquet.fichiers" && !it.startsWith("$paquet.") } == true
    }
}
