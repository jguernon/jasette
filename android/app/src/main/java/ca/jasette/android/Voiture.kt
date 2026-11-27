package ca.jasette.android

import android.content.Context
import android.content.Intent
import android.content.pm.ApplicationInfo
import android.graphics.Bitmap
import android.os.Handler
import android.os.Looper
import androidx.car.app.CarAppService
import androidx.car.app.CarContext
import androidx.car.app.Screen
import androidx.car.app.Session
import androidx.car.app.constraints.ConstraintManager
import androidx.car.app.messaging.model.CarMessage
import androidx.car.app.messaging.model.ConversationCallback
import androidx.car.app.messaging.model.ConversationItem
import androidx.car.app.model.Action
import androidx.car.app.model.CarIcon
import androidx.car.app.model.CarText
import androidx.car.app.model.GridItem
import androidx.car.app.model.GridTemplate
import androidx.car.app.model.Header
import androidx.car.app.model.ItemList
import androidx.car.app.model.ListTemplate
import androidx.car.app.model.SectionedItemList
import androidx.car.app.model.Template
import androidx.car.app.validation.HostValidator
import androidx.compose.runtime.snapshots.Snapshot
import androidx.core.app.Person
import androidx.core.graphics.drawable.IconCompat
import androidx.lifecycle.DefaultLifecycleObserver
import androidx.lifecycle.LifecycleOwner

/*
 * L'écran Jasette dans Android Auto (Car App Library, catégorie MESSAGING,
 * Car API 7 pour ConversationItem). La voiture fournit elle-même, pour chaque
 * ConversationItem, la lecture à voix haute, « Répondre » par la voix et le
 * marquage comme lu ; aucune saisie au clavier. Les envois prennent le chemin
 * normal (Depot.envoyer), comme les réponses des notifications (NotifAuto.kt).
 *
 * Un écran = un modèle : ConversationItem ne va que dans une liste
 * (ListTemplate), et une grille (GridTemplate) ne contient que des GridItem.
 * L'accueil est donc une liste (le dernier message reçu, puis les récentes),
 * et la grille des visages du Head est un écran à part, à un toucher
 * (bouton « Head » en haut).
 */

/** Le service que lance Android Auto. */
class JasetteVoitureService : CarAppService() {
    override fun createHostValidator(): HostValidator =
        // Version de développement : tout hôte (le Desktop Head Unit) ; sinon les hôtes de Google.
        if (applicationInfo.flags and ApplicationInfo.FLAG_DEBUGGABLE != 0) HostValidator.ALLOW_ALL_HOSTS_VALIDATOR
        else HostValidator.Builder(applicationContext)
            .addAllowedHosts(androidx.car.app.R.array.hosts_allowlist_sample)
            .build()

    override fun onCreateSession(): Session = SessionVoiture()
}

class SessionVoiture : Session() {
    override fun onCreateScreen(intent: Intent): Screen {
        // Les ponts doivent tourner : le service au premier plan démarre le dépôt.
        if (!Depot.demarre) try {
            PontService.lancer(carContext)
        } catch (_: Exception) {
        }
        return EcranAccueil(carContext)
    }
}

/** Ce que les écrans de la voiture montrent d'une conversation. */
object ConvVoiture {
    /** Au plus 5 messages par conversation (recommandation de Google). */
    const val MAX_MESSAGES = 5

    val moi: Person get() = Person.Builder().setName(t("Moi")).setKey("moi").build()

    private val images = object : LinkedHashMap<String, Bitmap?>(32, 0.75f, true) {
        override fun removeEldestEntry(eldest: MutableMap.MutableEntry<String, Bitmap?>?) = size > 60
    }

    /**
     * L'avatar rond, comme dans les notifications : l'icône du groupe d'un
     * expéditeur automatique, sinon la photo du contact, du service, ou la mosaïque du groupe.
     */
    fun image(ctx: Context, c: Conv): Bitmap? {
        RegroupementAuto.groupe(c)?.let { return imageGroupe(it, ca.jasette.android.ui.P.ega) }
        val photo = photoPerso(c) ?: Contacts.photoPour(c, grande = true) ?: Depot.avatarDe(c)
        val cle = "${c.id}|$photo"
        synchronized(images) { if (images.containsKey(cle)) return images[cle] }
        val b = avatarRond(ctx, photo) ?: if (c.group) mosaiqueRonde(ctx, Depot.mosaiques[c.id]) else null
        synchronized(images) { images[cle] = b }
        return b
    }

    fun icone(ctx: Context, c: Conv): CarIcon? = image(ctx, c)?.let { CarIcon.Builder(IconCompat.createWithBitmap(it)).build() }

    /** Les conversations qu'on montre dans la voiture : la boîte de réception, sans grands espaces, privées ni promotions regroupées. */
    fun recentes(): List<Conv> =
        Depot.boite.filter { dansBoiteDeReception(it) && !RegroupementAuto.promo(it) }.sortedByDescending { it.time }

    /** La conversation du dernier message reçu (non lue de préférence), mise en avant ; jamais une conversation en sourdine (Sourdine.kt). */
    fun dernierRecu(): Conv? = recentes().filter { notificationPermise(it.id) }
        .let { l -> l.firstOrNull { it.unread && !it.fromMe } ?: l.firstOrNull { !it.fromMe } }

    /** Le Head (marque `head`), ou les favoris si le Head est vide. */
    fun visages(): List<Conv> {
        val ids = Marques.head.toList().ifEmpty { Marques.favoris.toList() }
        return ids.filter { !Marques.estPrivee(it) }.mapNotNull { Depot.conv(it) }.filter { !it.large }
    }

    /** Les messages pour la voiture, du plus ancien au plus récent ; l'aperçu de la liste s'ils ne sont pas chargés. */
    fun messages(c: Conv, moi: Person): List<CarMessage> {
        val nom = nomAffiche(c).ifEmpty { Service.de(c.id).court }
        val personne = { m: Msg ->
            if (m.fromMe) moi
            else {
                val n = if (c.group && m.sender.isNotEmpty()) m.sender else nom
                Person.Builder().setName(n).setKey(m.senderId.ifEmpty { n }).build()
            }
        }
        val liste = Depot.messages[c.id].orEmpty().filter { !it.local && it.apercu().isNotEmpty() }.takeLast(MAX_MESSAGES)
        if (liste.isEmpty()) {
            val texte = traduirePont(c.preview).ifEmpty { "…" }
            return listOf(
                CarMessage.Builder()
                    .setSender(if (c.fromMe) moi else Person.Builder().setName(nom).setKey(nom).build())
                    .setBody(CarText.create(texte))
                    .setReceivedTimeEpochMillis(c.time)
                    .setRead(!c.unread || c.fromMe)
                    .build(),
            )
        }
        // Non lus : les messages reçus après mon dernier, si la conversation est non lue.
        val dernierAMoi = liste.indexOfLast { it.fromMe }
        return liste.mapIndexed { i, m ->
            CarMessage.Builder()
                .setSender(personne(m))
                .setBody(CarText.create(traduirePont(m.apercu(), m.kind == "admin")))
                .setReceivedTimeEpochMillis(m.time)
                .setRead(m.fromMe || !c.unread || i < dernierAMoi)
                .build()
        }
    }

    /** Un ConversationItem : la voiture y met Lire, Répondre (voix) et Marquer comme lu. */
    fun item(ctx: Context, c: Conv): ConversationItem {
        val moi = moi
        val b = ConversationItem.Builder(
            c.id, CarText.create(nomAffiche(c).ifEmpty { Service.de(c.id).court }), moi,
            messages(c, moi), rappels(ctx, c.id),
        ).setGroupConversation(c.group)
        icone(ctx, c)?.let { b.setIcon(it) }
        return b.build()
    }

    fun rappels(ctx: Context, id: String) = object : ConversationCallback {
        override fun onMarkAsRead() {
            // Comme ouvrir la conversation dans l'app : le « lu » part au service.
            AutoRecepteur.quandPret(ctx, id) { Depot.marquerLu(id) }
            NotifAuto.retirer(ctx, id)
        }

        override fun onTextReply(replyText: String) {
            val texte = replyText.trim()
            if (texte.isEmpty()) return
            AutoRecepteur.quandPret(ctx, id) {
                Depot.envoyer(id, texte)
                Depot.marquerLu(id)
            }
            NotifAuto.retirer(ctx, id)
        }
    }

    /** Un visage de la grille : l'avatar rond (ou l'icône des visages), le nom, « Nouveau » si non lue. */
    fun visage(ctx: Context, c: Conv, ouvrir: () -> Unit): GridItem {
        val icone = icone(ctx, c) ?: CarIcon.Builder(IconCompat.createWithResource(ctx, R.drawable.ic_visages)).build()
        val g = GridItem.Builder().setTitle(nomAffiche(c).ifEmpty { Service.de(c.id).court })
            .setImage(icone, GridItem.IMAGE_TYPE_LARGE)
            .setOnClickListener { ouvrir() }
        if (c.unread) g.setText(t("Nouveau"))
        return g.build()
    }

    /** Le nombre d'éléments permis par l'hôte (ConstraintManager), [defaut] si inconnu. */
    fun limite(ctx: CarContext, type: Int, defaut: Int): Int = try {
        ctx.getCarService(ConstraintManager::class.java).getContentLimit(type)
    } catch (_: Exception) {
        defaut
    }
}

/**
 * Un écran qui se redessine quand l'état de l'app change (nouveaux messages,
 * envois), pendant qu'il est à l'écran, au plus une fois par seconde.
 */
abstract class EcranVivant(ctx: CarContext) : Screen(ctx) {
    private val principal = Handler(Looper.getMainLooper())
    private var prevu = false
    private var observateur: androidx.compose.runtime.snapshots.ObserverHandle? = null

    init {
        lifecycle.addObserver(object : DefaultLifecycleObserver {
            override fun onStart(owner: LifecycleOwner) {
                observateur = Snapshot.registerApplyObserver { _, _ ->
                    if (!prevu) {
                        prevu = true
                        principal.postDelayed({ prevu = false; invalidate() }, 1000)
                    }
                }
            }

            override fun onStop(owner: LifecycleOwner) {
                observateur?.dispose()
                observateur = null
            }
        })
    }
}

/** L'accueil : le dernier message reçu, puis les conversations récentes. */
class EcranAccueil(ctx: CarContext) : EcranVivant(ctx) {
    override fun onGetTemplate(): Template {
        val limite = ConvVoiture.limite(carContext, ConstraintManager.CONTENT_LIMIT_TYPE_LIST, 6)
        val dernier = ConvVoiture.dernierRecu()
        val recentes = ConvVoiture.recentes().filter { it.id != dernier?.id }
            .take((limite - (if (dernier != null) 1 else 0)).coerceAtLeast(0))
        val header = Header.Builder()
            .setStartHeaderAction(Action.APP_ICON)
            .setTitle("Jasette")
            .apply {
                if (ConvVoiture.visages().isNotEmpty()) {
                    addEndHeaderAction(
                        Action.Builder()
                            .setTitle(if (Marques.head.isNotEmpty()) "Head" else t("Favoris"))
                            .setIcon(CarIcon.Builder(IconCompat.createWithResource(carContext, R.drawable.ic_visages)).build())
                            .setOnClickListener { screenManager.push(EcranVisages(carContext)) }
                            .build(),
                    )
                }
            }
            .build()
        val b = ListTemplate.Builder().setHeader(header)
        if (!Depot.demarre || (dernier == null && recentes.isEmpty() && Depot.convs.isEmpty())) {
            return b.setLoading(true).build()
        }
        if (dernier != null) {
            b.addSectionedList(
                SectionedItemList.create(ItemList.Builder().addItem(ConvVoiture.item(carContext, dernier)).build(), t("Dernier message")),
            )
        }
        if (recentes.isNotEmpty()) {
            val l = ItemList.Builder()
            recentes.forEach { l.addItem(ConvVoiture.item(carContext, it)) }
            b.addSectionedList(SectionedItemList.create(l.build(), t("Récentes")))
        }
        if (dernier == null && recentes.isEmpty()) {
            b.setSingleList(ItemList.Builder().setNoItemsMessage(t("Aucune conversation")).build())
        }
        return b.build()
    }
}

/** La grille des visages du Head (ou des favoris), avatars ronds. */
class EcranVisages(ctx: CarContext) : EcranVivant(ctx) {
    override fun onGetTemplate(): Template {
        val limite = ConvVoiture.limite(carContext, ConstraintManager.CONTENT_LIMIT_TYPE_GRID, 6)
        val l = ItemList.Builder().setNoItemsMessage(t("Aucune conversation"))
        for (c in ConvVoiture.visages().take(limite)) {
            l.addItem(ConvVoiture.visage(carContext, c) { screenManager.push(EcranConversation(carContext, c.id)) })
        }
        return GridTemplate.Builder()
            .setHeader(
                Header.Builder().setStartHeaderAction(Action.BACK)
                    .setTitle(if (Marques.head.isNotEmpty()) "Head" else t("Favoris")).build(),
            )
            .setItemImageShape(GridTemplate.ITEM_IMAGE_SHAPE_CIRCLE)
            .setSingleList(l.build())
            .build()
    }
}

/** Une conversation : ses derniers messages ; la voiture offre Lire et Répondre par la voix. */
class EcranConversation(ctx: CarContext, private val id: String) : EcranVivant(ctx) {
    init {
        // Les derniers messages, s'ils ne sont pas encore là (ne marque rien comme lu).
        Depot.prechargerFil(id)
    }

    override fun onGetTemplate(): Template {
        val c = Depot.conv(id)
        val header = Header.Builder().setStartHeaderAction(Action.BACK)
            .setTitle(c?.let { nomAffiche(it) }?.ifEmpty { null } ?: "Jasette").build()
        val b = ListTemplate.Builder().setHeader(header)
        if (c == null) return b.setLoading(true).build()
        return b.setSingleList(ItemList.Builder().addItem(ConvVoiture.item(carContext, c)).build()).build()
    }
}
