package ca.jasette.android

import android.app.PendingIntent
import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
import android.net.Uri
import android.os.Build
import android.os.Handler
import android.os.Looper
import androidx.core.app.NotificationCompat
import androidx.core.app.NotificationManagerCompat
import androidx.core.app.RemoteInput

/**
 * Les notifications de message comme les veut Android Auto (« Build messaging
 * apps for Android Auto ») : une action Répondre (RemoteInput, sémantique
 * REPLY, sans interface) et une action invisible Marquer comme lu (sémantique
 * MARK_AS_READ, sans interface), toutes deux reçues par [AutoRecepteur],
 * jamais par une activité. Android Auto lit alors le message à voix haute et
 * fait dicter la réponse ; le téléphone montre aussi Répondre dans la
 * notification.
 */
object NotifAuto {
    const val ACTION_REPONDRE = "ca.jasette.REPONDRE"
    const val ACTION_LU = "ca.jasette.LU"
    const val EXTRA_CONV = "conv"
    const val CLE_REPONSE = "reponse"

    /** Peut-on répondre depuis la notification ? Pas en lecture seule, ni dans un grand espace (§5.13). */
    fun peutRepondre(c: Conv?): Boolean = c == null || (!c.readOnly && !c.announce && !c.large)

    /** Les actions de la voiture ont-elles leur place ? Jamais pour un grand espace (Canaux, §5.13) ni une promotion regroupée. */
    fun pourVoiture(c: Conv?): Boolean = c?.large != true && !RegroupementAuto.promo(c)

    private fun intention(ctx: Context, action: String, conv: String): Intent =
        Intent(ctx, AutoRecepteur::class.java)
            .setAction(action)
            // Une donnée par conversation : un PendingIntent distinct pour chacune.
            .setData(Uri.fromParts("jasette", conv, action))
            .putExtra(EXTRA_CONV, conv)

    /** « Répondre » : RemoteInput, SEMANTIC_ACTION_REPLY, sans interface (la voiture dicte). */
    fun actionRepondre(ctx: Context, conv: String): NotificationCompat.Action {
        // RemoteInput remplit l'intention : elle doit rester modifiable (Android 12+).
        val drapeaux = PendingIntent.FLAG_UPDATE_CURRENT or
            (if (Build.VERSION.SDK_INT >= 31) PendingIntent.FLAG_MUTABLE else 0)
        val pi = PendingIntent.getBroadcast(ctx, 0, intention(ctx, ACTION_REPONDRE, conv), drapeaux)
        val saisie = RemoteInput.Builder(CLE_REPONSE).setLabel(t("Répondre")).build()
        return NotificationCompat.Action.Builder(R.drawable.ic_notif, t("Répondre"), pi)
            .addRemoteInput(saisie)
            .setAllowGeneratedReplies(true)
            .setSemanticAction(NotificationCompat.Action.SEMANTIC_ACTION_REPLY)
            .setShowsUserInterface(false)
            .build()
    }

    /** « Marquer comme lu » : SEMANTIC_ACTION_MARK_AS_READ, sans interface, ajoutée en action invisible. */
    fun actionLu(ctx: Context, conv: String): NotificationCompat.Action {
        val pi = PendingIntent.getBroadcast(
            ctx, 0, intention(ctx, ACTION_LU, conv),
            PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE,
        )
        return NotificationCompat.Action.Builder(R.drawable.ic_notif, t("Marquer comme lu"), pi)
            .setSemanticAction(NotificationCompat.Action.SEMANTIC_ACTION_MARK_AS_READ)
            .setShowsUserInterface(false)
            .build()
    }

    /** Ajoute Répondre (visible) et Marquer comme lu (invisible) à une notification de message. */
    fun equiper(ctx: Context, b: NotificationCompat.Builder, conv: Conv?, id: String): NotificationCompat.Builder {
        if (!pourVoiture(conv)) return b
        if (peutRepondre(conv)) b.addAction(actionRepondre(ctx, id))
        return b.addInvisibleAction(actionLu(ctx, id))
    }

    // ---- La notification de chaque conversation, gardée pour la compléter ----

    /** La dernière notification construite par conversation : son Builder et son MessagingStyle. */
    private class Courante(val builder: NotificationCompat.Builder, val style: NotificationCompat.MessagingStyle)

    private val courantes = object : LinkedHashMap<String, Courante>(16, 0.75f, true) {
        override fun removeEldestEntry(eldest: MutableMap.MutableEntry<String, Courante>?) = size > 40
    }

    /** Messages gardés dans une notification (Android Auto lit les non-lus, du plus ancien au plus récent). */
    private const val MAX_MESSAGES = 10

    /**
     * Le style d'une nouvelle notification de [conv] : celui de la notification
     * encore affichée, complété (les messages non lus s'accumulent), ou [neuf].
     */
    fun style(ctx: Context, conv: String, neuf: () -> NotificationCompat.MessagingStyle): NotificationCompat.MessagingStyle {
        val c = synchronized(courantes) { courantes[conv] }
        if (c != null && affichee(ctx, conv)) {
            val m = c.style.messages
            while (m.size >= MAX_MESSAGES) m.removeAt(0)
            return c.style
        }
        return neuf()
    }

    fun noter(conv: String, builder: NotificationCompat.Builder, style: NotificationCompat.MessagingStyle) {
        synchronized(courantes) { courantes[conv] = Courante(builder, style) }
    }

    fun oublier(conv: String) {
        synchronized(courantes) { courantes.remove(conv) }
    }

    private fun affichee(ctx: Context, conv: String): Boolean = try {
        val id = PontService.idNotification(conv)
        ctx.getSystemService(android.app.NotificationManager::class.java).activeNotifications.any { it.id == id }
    } catch (_: Exception) {
        false
    }

    /**
     * Après une réponse : la notification montre mon message (MessagingStyle,
     * personne = moi), sans son ni vibration. Le renvoi arrête aussi
     * l'indicateur « réponse en cours » d'Android. Sans notification gardée
     * (processus relancé), elle est simplement retirée.
     */
    fun ajouterReponse(ctx: Context, conv: String, texte: String?) {
        val nm = NotificationManagerCompat.from(ctx)
        val id = PontService.idNotification(conv)
        val c = synchronized(courantes) { courantes[conv] }
        if (c == null) {
            nm.cancel(id)
            return
        }
        if (!texte.isNullOrBlank()) {
            val m = c.style.messages
            while (m.size >= MAX_MESSAGES) m.removeAt(0)
            // Personne nulle : le message est de l'utilisateur (la Person « moi » du style).
            c.style.addMessage(texte, System.currentTimeMillis(), null as androidx.core.app.Person?)
        }
        c.builder.setStyle(c.style).setOnlyAlertOnce(true).setSilent(true).setWhen(System.currentTimeMillis())
        try {
            nm.notify(id, c.builder.build())
        } catch (_: SecurityException) {
        }
    }

    /** Marquée comme lue (voiture ou téléphone) : la notification disparaît. */
    fun retirer(ctx: Context, conv: String) {
        oublier(conv)
        NotificationManagerCompat.from(ctx).cancel(PontService.idNotification(conv))
    }
}

/**
 * Reçoit Répondre et Marquer comme lu des notifications (Android Auto ou le
 * téléphone). La réponse prend le chemin d'envoi normal ([Depot.envoyer] :
 * la bulle « envoi… », le mode serveur ou local, les SMS du téléphone).
 */
class AutoRecepteur : BroadcastReceiver() {
    override fun onReceive(ctx: Context, intent: Intent) {
        val conv = intent.getStringExtra(NotifAuto.EXTRA_CONV) ?: return
        when (intent.action) {
            NotifAuto.ACTION_REPONDRE -> {
                val texte = RemoteInput.getResultsFromIntent(intent)
                    ?.getCharSequence(NotifAuto.CLE_REPONSE)?.toString()?.trim()
                if (texte.isNullOrEmpty()) {
                    NotifAuto.ajouterReponse(ctx, conv, null)
                    return
                }
                quandPret(ctx, conv) {
                    Depot.envoyer(conv, texte)
                    // Répondre, c'est avoir lu (comme dans l'app, où l'on répond conversation ouverte).
                    Depot.marquerLu(conv)
                }
                NotifAuto.ajouterReponse(ctx, conv, texte)
            }
            NotifAuto.ACTION_LU -> {
                // Comme ouvrir la conversation dans l'app : le « lu » part au service.
                quandPret(ctx, conv) { Depot.marquerLu(conv) }
                NotifAuto.retirer(ctx, conv)
            }
        }
    }

    companion object {
        /**
         * Lance [action] sur le fil principal dès que le service de [conv] est
         * prêt. Processus relancé : le service au premier plan (et le dépôt)
         * redémarre d'abord ; on attend 30 s au plus.
         */
        fun quandPret(ctx: Context, conv: String, action: () -> Unit) {
            val principal = Handler(Looper.getMainLooper())
            if (!Depot.demarre) PontService.lancer(ctx)
            val service = Service.de(conv)
            val debut = System.currentTimeMillis()
            fun essayer() {
                if (Depot.demarre && Depot.pret(service)) action()
                else if (System.currentTimeMillis() - debut < 30_000) principal.postDelayed({ essayer() }, 500)
            }
            principal.post { essayer() }
        }
    }
}
