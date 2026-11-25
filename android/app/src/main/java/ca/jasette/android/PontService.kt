package ca.jasette.android

import android.Manifest
import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.app.Service as ServiceAndroid
import android.content.Context
import android.content.Intent
import android.content.pm.PackageManager
import android.content.pm.ServiceInfo
import android.os.Build
import android.os.IBinder
import androidx.core.app.NotificationCompat
import androidx.core.app.NotificationManagerCompat
import androidx.core.app.ServiceCompat
import androidx.core.content.ContextCompat

/**
 * Le service au premier plan : il garde les ponts vivants quand l'interface
 * est fermée, et affiche une notification pour chaque nouveau message.
 */
class PontService : ServiceAndroid() {

    override fun onBind(intent: Intent?): IBinder? = null

    override fun onCreate() {
        super.onCreate()
        canaux(this)
        val type = if (Build.VERSION.SDK_INT >= 34) ServiceInfo.FOREGROUND_SERVICE_TYPE_REMOTE_MESSAGING else 0
        ServiceCompat.startForeground(this, ID_SERVICE, notificationService(), type)
        Depot.notifier = { m, conv -> notifier(m, conv) }
        Depot.surChangementMode = { rafraichir() }
        Depot.demarrer(this)
        // Le mode (local ou serveur) n'est connu qu'une fois le dépôt démarré.
        rafraichir()
    }

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        if (intent?.action == ACTION_ARRETER) {
            Depot.arreter()
            ServiceCompat.stopForeground(this, ServiceCompat.STOP_FOREGROUND_REMOVE)
            stopSelf()
            return START_NOT_STICKY
        }
        return START_STICKY
    }

    override fun onDestroy() {
        Depot.notifier = null
        Depot.surChangementMode = null
        super.onDestroy()
    }

    /** La notification du service, selon le mode et la langue. */
    private fun rafraichir() {
        try {
            NotificationManagerCompat.from(this).notify(ID_SERVICE, notificationService())
        } catch (_: SecurityException) {
        }
    }

    private fun ouvrir(conv: String?): PendingIntent {
        val intent = Intent(this, MainActivity::class.java)
            .addFlags(Intent.FLAG_ACTIVITY_SINGLE_TOP or Intent.FLAG_ACTIVITY_CLEAR_TOP)
        if (conv != null) intent.putExtra(MainActivity.EXTRA_CONV, conv)
        return PendingIntent.getActivity(
            this, conv?.hashCode() ?: 0, intent,
            PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE,
        )
    }

    private fun notificationService(): Notification {
        val arreter = PendingIntent.getService(
            this, 1, Intent(this, PontService::class.java).setAction(ACTION_ARRETER),
            PendingIntent.FLAG_IMMUTABLE,
        )
        return NotificationCompat.Builder(this, CANAL_SERVICE)
            .setSmallIcon(R.drawable.ic_notif)
            .setContentTitle("Jasette")
            .setContentText(
                Depot.serveur?.let { "${t("Connecté à ton serveur Jasette")} (${it.adresse})" }
                    ?: t("Connecté à tes messageries"),
            )
            .setOngoing(true)
            .setSilent(true)
            .setContentIntent(ouvrir(null))
            .addAction(0, t("Quitter"), arreter)
            .build()
    }

    private fun notifier(m: Msg, conv: Conv?) {
        if (Build.VERSION.SDK_INT >= 33 &&
            ContextCompat.checkSelfPermission(this, Manifest.permission.POST_NOTIFICATIONS) != PackageManager.PERMISSION_GRANTED
        ) return
        val titre = conv?.name?.ifEmpty { null } ?: m.sender.ifEmpty { Service.de(m.conv).court }
        val texte = if (conv?.group == true && m.sender.isNotEmpty()) "${m.sender}${t(" : ")}${m.apercu()}" else m.apercu()
        val n = NotificationCompat.Builder(this, CANAL_MESSAGES)
            .setSmallIcon(R.drawable.ic_notif)
            .setColor(0xFF2F6BFF.toInt())
            .setContentTitle(titre)
            .setContentText(texte)
            .setStyle(NotificationCompat.BigTextStyle().bigText(texte))
            .setSubText(Service.de(m.conv).court)
            .setWhen(m.time)
            .setShowWhen(true)
            .setAutoCancel(true)
            .setCategory(NotificationCompat.CATEGORY_MESSAGE)
            .setPriority(NotificationCompat.PRIORITY_HIGH)
            .setContentIntent(ouvrir(m.conv))
            .build()
        try {
            NotificationManagerCompat.from(this).notify(idNotification(m.conv), n)
        } catch (_: SecurityException) {
        }
    }

    companion object {
        const val CANAL_SERVICE = "service"
        const val CANAL_MESSAGES = "messages"
        const val ID_SERVICE = 1
        const val ACTION_ARRETER = "ca.jasette.ARRETER"

        /** Une notification par conversation. */
        fun idNotification(conv: String) = 1000 + (conv.hashCode() and 0x7fffffff) % 1_000_000

        fun canaux(ctx: Context) {
            val nm = ctx.getSystemService(NotificationManager::class.java)
            nm.createNotificationChannel(
                NotificationChannel(CANAL_SERVICE, t("Jasette en marche"), NotificationManager.IMPORTANCE_MIN).apply {
                    description = t("Garde la connexion à Google Messages, Messenger et Instagram (ou à ton serveur Jasette).")
                    setShowBadge(false)
                },
            )
            nm.createNotificationChannel(
                NotificationChannel(CANAL_MESSAGES, t("Nouveaux messages"), NotificationManager.IMPORTANCE_HIGH),
            )
        }

        fun lancer(ctx: Context) {
            ContextCompat.startForegroundService(ctx, Intent(ctx, PontService::class.java))
        }
    }
}
