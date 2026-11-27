package ca.jasette.android

import android.content.Context
import android.content.SharedPreferences
import android.util.Log
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateListOf
import androidx.compose.runtime.mutableStateMapOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.compose.runtime.snapshots.SnapshotStateList
import androidx.compose.runtime.snapshots.SnapshotStateMap
import org.json.JSONArray
import org.json.JSONObject

/** Un emoji rapide fait au plus 32 octets (un graphème, PROTOCOLE.md §5.5). */
const val EMOJI_RAPIDE_MAX = 32

/** Une valeur de `groupes_auto` ("codes", "promos", "avis", "aucun") : 16 octets au plus. */
const val GROUPE_AUTO_MAX = 16

/**
 * Les crans du curseur « Cacher les conversations plus anciennes de la boîte de
 * réception », en jours : tout afficher, 7, 15, 30, 60, 90 jours, 6 mois, 1 an
 * (comme `CACHER_APRES_JOURS` du bureau).
 */
val CACHER_APRES_JOURS = intArrayOf(0, 7, 15, 30, 60, 90, 182, 365)

/** Au-delà de dix ans, autant tout afficher (comme le serveur) ; négatif : 0. */
fun bornerJours(j: Int): Int = if (j < 0 || j > 3650) 0 else j

/**
 * Vrai si une conversation est trop vieille pour la boîte de réception : son
 * dernier message ([tempsMs]) date de plus de [jours] (0 : tout afficher). Une
 * épinglée ou une non lue reste toujours visible ; une heure inconnue (0) aussi.
 */
fun tropAncienne(tempsMs: Long, nonLue: Boolean, epinglee: Boolean, jours: Int, maintenantMs: Long): Boolean =
    jours > 0 && !epinglee && !nonLue && tempsMs > 0 && maintenantMs - tempsMs > jours * 86_400_000L

/**
 * Les favoris et les épingles (`etoiles`/`head` et `epingles` des réglages du
 * bureau) : des listes d'ids de conversations, dans leur ordre, gardées dans
 * les SharedPreferences privées de l'app.
 *
 * Comme au bureau, deux listes distinctes : les favoris (la vue de l'étoile
 * du rail) et le « Head » (la grille d'avatars en haut de la boîte de réception).
 *
 * On y garde aussi deux choix d'apparence : deux volets sur grand écran, et
 * l'heure de chaque message.
 *
 * Et les archives de Jasette : les conversations archivées par Jasette elle-même,
 * pour les services qui ne savent pas archiver (Messenger, Instagram, Signal,
 * X ; Google et WhatsApp archivent pour de vrai). Un nouveau message reçu les
 * ramène dans la boîte de réception ([desarchiverSiRecu]), sauf avec
 * [garderArchives].
 *
 * En mode client du serveur Jasette, le serveur est la source : il garde les
 * quatre listes et les partage entre les appareils (serveur/PROTOCOLE.md,
 * §5.5, et `SuiviMarques` dans src/distant.rs, dont ceci est la traduction).
 * La copie locale garde aussi la dernière version reçue (0 : jamais
 * synchronisé avec ce serveur) et « changées ici, pas encore confirmées ».
 * Tout se passe sur le fil principal.
 */
object Marques {
    private const val PREFS = "marques"
    private var prefs: SharedPreferences? = null

    /** Les favoris, dans l'ordre choisi (réorganisable). */
    val favoris: SnapshotStateList<String> = mutableStateListOf()
    /** Les épinglées en haut de la liste, dans l'ordre d'épinglage. */
    val epingles: SnapshotStateList<String> = mutableStateListOf()
    /** Le Head : la grille en haut de la boîte de réception, dans l'ordre d'ajout. */
    val head: SnapshotStateList<String> = mutableStateListOf()
    /** Les archives de Jasette (ids préfixés), dans l'ordre d'archivage. */
    val archives: SnapshotStateList<String> = mutableStateListOf()
    /**
     * La Liste privée (ids préfixés) : cachées partout dans l'app normale, montrées
     * seulement dans la vue Privées ([Prive]). Partagée par le serveur comme les
     * autres listes (`privees`) ; le code, lui, reste sur l'appareil.
     */
    val privees: SnapshotStateList<String> = mutableStateListOf()
    /**
     * Le « Mood » de chaque conversation (id préfixé → id du mood, voir
     * [ca.jasette.android.ui.MOODS]) : couleurs des bulles et fond du fil.
     * Gardé ici d'abord ; partagé par le serveur Jasette s'il y en a un
     * (champ `moods` des marques, §5.5). Ignoré en EGA.
     */
    val moods: SnapshotStateMap<String, String> = mutableStateMapOf()
    /**
     * L'« emoji rapide » de chaque conversation (id préfixé → emoji) : un bouton
     * rond à droite de la saisie l'envoie d'un appui. Aucun par défaut. Partagé
     * comme les moods (champ `emojis` des marques, §5.5), en EGA aussi.
     */
    val emojis: SnapshotStateMap<String, String> = mutableStateMapOf()
    /**
     * Le regroupement automatique corrigé à la main (GroupesAuto.kt) : id
     * préfixé → "codes", "promos", "avis" ou "aucun". Gagne toujours sur la
     * détection ; partagé comme les emojis rapides (champ `groupes_auto`, §5.5).
     */
    val groupesAuto: SnapshotStateMap<String, String> = mutableStateMapOf()
    /**
     * Depuis quand (ms, horloge du téléphone) chaque id est dans [archives], ici :
     * seul un message reçu après cette heure la désarchive (pas une réaction
     * ni la mise à jour d'un vieux message). Propre à l'appareil, jamais envoyé.
     */
    private val archivesDepuis = HashMap<String, Long>()

    /**
     * « Garder les conversations archivées hors de la boîte de réception » :
     * vrai, un message reçu ne désarchive plus ([desarchiverSiRecu]). Faux par
     * défaut. Une marque comme les listes (`garder_archives`, §5.5) : partagée
     * par le serveur Jasette s'il y en a un, gardée ici sinon.
     */
    var garderArchives by mutableStateOf(false)
        private set

    fun choisirGarderArchives(oui: Boolean) {
        if (oui == garderArchives) return
        garderArchives = oui
        changement()
    }

    /**
     * « Cacher les conversations plus anciennes de la boîte de réception », en
     * jours (0 : tout afficher ; crans dans [CACHER_APRES_JOURS]). Seul
     * l'affichage filtre ([tropAncienne]). Une marque (`cacher_apres_jours`,
     * §5.5) : partagée par le serveur Jasette s'il y en a un, gardée ici sinon.
     */
    var cacherApresJours by mutableStateOf(0)
        private set

    fun choisirCacherApresJours(jours: Int) {
        val j = bornerJours(jours)
        if (j == cacherApresJours) return
        cacherApresJours = j
        changement()
    }

    /**
     * « Afficher qui est en ligne » : point vert sur les contacts actifs et
     * « Actif il y a 12 min » (serveur/PROTOCOLE.md §5.10). Vrai par défaut.
     * Une marque (`presence_en_ligne`, §5.5) : partagée par le serveur Jasette
     * s'il y en a un, gardée ici sinon. [surPresence] prévient les ponts locaux.
     */
    var presenceEnLigne by mutableStateOf(true)
        private set

    /** Appelée à chaque changement de [presenceEnLigne] (ici ou par le serveur). Posée par [Depot]. */
    var surPresence: ((Boolean) -> Unit)? = null

    fun choisirPresenceEnLigne(oui: Boolean) {
        if (oui == presenceEnLigne) return
        presenceEnLigne = oui
        changement()
        surPresence?.invoke(oui)
    }

    /**
     * Le jeu de sons des conversations (Sons.kt) : "bulle" (défaut), "bois",
     * "verre" ou "aucun". Une marque (`sons_conversation`, §5.5) : partagée
     * par le serveur Jasette s'il y en a un, gardée ici sinon.
     */
    var sonsConversation by mutableStateOf("bulle")
        private set

    fun choisirSons(jeu: String) {
        val j = jeuSons(jeu)
        if (j == sonsConversation) return
        sonsConversation = j
        changement()
    }

    /**
     * « Afficher les notes de mes contacts » (bulles du Head, PROTOCOLE.md §5.12).
     * Vrai par défaut. Une marque (`notes_contacts`) ; [surNotesContacts]
     * prévient le pont Instagram local.
     */
    var notesContacts by mutableStateOf(true)
        private set

    /** Appelée à chaque changement de [notesContacts] (ici ou par le serveur). Posée par [Depot]. */
    var surNotesContacts: ((Boolean) -> Unit)? = null

    fun choisirNotesContacts(oui: Boolean) {
        if (oui == notesContacts) return
        notesContacts = oui
        changement()
        surNotesContacts?.invoke(oui)
    }

    /**
     * « Partager ce que j'écoute sur Spotify » : le nom de l'appareil qui publie
     * la musique (`notes_musique`, §5.5), "" = désactivé. Un seul appareil à la fois.
     */
    var notesMusique by mutableStateOf("")
        private set

    fun choisirNotesMusique(appareil: String) {
        val a = bornerOctets(appareil, 128)
        if (a == notesMusique) return
        notesMusique = a
        changement()
    }

    /** Ce que dit la note musicale (`notes_musique_format`) : "artiste" (défaut), "genre" ou "titre". */
    var notesMusiqueFormat by mutableStateOf("artiste")
        private set

    fun choisirNotesMusiqueFormat(f: String) {
        val n = formatMusique(f)
        if (n == notesMusiqueFormat) return
        notesMusiqueFormat = n
        changement()
    }

    /** Les services où « Quoi de neuf ? » publie (`notes_services`) : ["tout"] ou une liste précise. */
    var notesServices by mutableStateOf(listOf("tout"))
        private set

    fun choisirNotesServices(l: List<String>) {
        val n = servicesNotes(l)
        if (n == notesServices) return
        notesServices = n
        changement()
    }

    /**
     * L'ordre des services choisi dans Réglages → Comptes → « Dans la boîte de
     * réception » (leurs clés ; vide : l'ordre par défaut), appliqué partout par
     * `Depot.services` (voir [Service.ordonnes]). Une marque (`ordre_services`,
     * §5.5) partagée par le serveur Jasette comme l'ordre des favoris, gardée
     * ici sinon (mode autonome, serveur d'avant la marque). L'ordre choisi avant
     * le partage (préférences « affichage ») est repris par [charger] et envoyé
     * une fois au serveur qui n'en a pas ([recu]).
     */
    var ordreServices by mutableStateOf<List<String>>(emptyList())
        private set

    fun choisirOrdreServices(l: List<String>) {
        val n = nettoyerOrdreServices(l)
        if (n == ordreServices) return
        ordreServices = n
        changement()
    }

    /**
     * La fiche de la personne (Personnes.kt) : les conversations reliées à la
     * main (marque `personnes`) et les refus « Pas la même personne » (marque
     * `personnes_refus`), partagés par le serveur Jasette.
     */
    var personnes by mutableStateOf<List<Personne>>(emptyList())
        private set
    var personnesRefus by mutableStateOf<List<Refus>>(emptyList())
        private set

    /**
     * Le nom et la photo choisis à la main (crayon de la fiche, marque
     * `contacts_perso`), {id: ContactPerso} ; voir ContactsPerso.kt.
     */
    var contactsPerso by mutableStateOf<Map<String, ContactPerso>>(emptyMap())
        private set

    /** Appelé après chaque réception de marques (Contacts Google d'un autre appareil). */
    var surContactsPerso: ((Map<String, ContactPerso>) -> Unit)? = null

    /** Le nom et la photo d'une personne viennent de changer ici (et ses liens, au besoin). */
    fun changerContactsPerso(c: Map<String, ContactPerso>, p: List<Personne> = personnes) {
        val nc = ContactsPerso.nettoyer(c)
        val np = Personnes.nettoyer(p)
        if (nc == contactsPerso && np == personnes) return
        contactsPerso = nc
        personnes = np
        changement()
    }

    /** Les liens ou refus viennent de changer ici (fiche de la personne). */
    fun changerPersonnes(p: List<Personne>, r: List<Refus>) {
        val np = Personnes.nettoyer(p)
        val nr = Personnes.nettoyerRefus(r)
        if (np == personnes && nr == personnesRefus) return
        personnes = np
        personnesRefus = nr
        changement()
    }

    /** Chaque bulle montre son heure (et sa date si ce n'est pas aujourd'hui), comme `details_heures`. */
    var heureChaqueMessage by mutableStateOf(true)
        private set

    fun choisirHeureChaqueMessage(oui: Boolean) {
        heureChaqueMessage = oui
        prefs?.edit()?.putBoolean("heure_chaque_message", oui)?.apply()
    }

    /** Deux volets (liste + fil) sur grand écran : « Automatique » (vrai, le défaut) ou « Désactivé ». */
    var deuxVolets by mutableStateOf(true)
        private set

    fun choisirDeuxVolets(oui: Boolean) {
        deuxVolets = oui
        prefs?.edit()?.putBoolean("deux_volets", oui)?.apply()
    }

    /**
     * La grille du Head : le nombre de colonnes (3 à 6, 4 par défaut) ; autant
     * de rangées qu'il faut pour tous les Head. Propre à l'appareil : jamais
     * envoyé au serveur (pas dans [listes]).
     */
    var headGrille by mutableStateOf("4")
        private set

    /** Les choix offerts dans Réglages → Apparence. */
    val GRILLES = listOf("3", "4", "5", "6")

    /** Le nombre de colonnes d'un choix ; une ancienne valeur (« auto », « 6x2 »…) devient 3 à 6. */
    fun colonnes(grille: String): Int =
        grille.substringBefore('x').toIntOrNull()?.coerceIn(3, 6) ?: 4

    /** commit() : écrit tout de suite, comme les listes (voir [garder]). */
    fun choisirHeadGrille(grille: String) {
        headGrille = colonnes(grille).toString()
        prefs?.edit()?.putString("head_grille", headGrille)?.commit()
    }

    /** La grosseur du texte des messages (1 = normal, 0,85 à 1,6). Propre à l'appareil. */
    var tailleMessages by mutableStateOf(1f)
        private set

    fun choisirTailleMessages(e: Float) {
        tailleMessages = e.coerceIn(0.85f, 1.6f)
        prefs?.edit()?.putFloat("taille_messages", tailleMessages)?.apply()
    }

    /** Mon avatar : « google », « messenger », « instagram », « fichier », « initiale » ou vide (pas encore choisi). */
    var avatarSource by mutableStateOf("")
        private set
    /** L'image choisie sur le téléphone (copiée dans les fichiers privés de l'app). */
    var avatarFichier by mutableStateOf<String?>(null)
        private set

    fun choisirAvatar(source: String, fichier: String? = null) {
        avatarSource = source
        if (fichier != null) avatarFichier = fichier
        prefs?.edit()?.putString("avatar_source", source)?.putString("avatar_fichier", avatarFichier)?.apply()
    }

    /** La dernière version des marques reçue du serveur (0 : jamais synchronisé avec lui). */
    private var version = 0L
    /** Changées ici, pas encore confirmées par le serveur. */
    private var aEnvoyer = false
    /** L'empreinte du serveur dont vient [version]. */
    private var serveurMarques = ""

    fun charger(ctx: Context) {
        val p = ctx.applicationContext.getSharedPreferences(PREFS, Context.MODE_PRIVATE)
        prefs = p
        version = p.getLong("version", 0L)
        aEnvoyer = p.getBoolean("a_envoyer", false)
        serveurMarques = p.getString("serveur", "") ?: ""
        avatarSource = p.getString("avatar_source", "") ?: ""
        avatarFichier = p.getString("avatar_fichier", null)
        deuxVolets = p.getBoolean("deux_volets", true)
        heureChaqueMessage = p.getBoolean("heure_chaque_message", true)
        headGrille = colonnes(p.getString("head_grille", "4") ?: "4").toString()
        tailleMessages = p.getFloat("taille_messages", 1f).coerceIn(0.85f, 1.6f)
        head.clear()
        head.addAll(lire(p, "head"))
        favoris.clear()
        favoris.addAll(lire(p, "favoris"))
        epingles.clear()
        epingles.addAll(lire(p, "epingles"))
        archives.clear()
        archives.addAll(lire(p, "archives"))
        privees.clear()
        privees.addAll(lire(p, "privees"))
        moods.clear()
        moods.putAll(lireMoods(p.getString("moods", "{}") ?: "{}"))
        emojis.clear()
        emojis.putAll(lireObjet("emojis", p.getString("emojis", "{}") ?: "{}", EMOJI_RAPIDE_MAX))
        groupesAuto.clear()
        groupesAuto.putAll(lireObjet("groupes_auto", p.getString("groupes_auto", "{}") ?: "{}", GROUPE_AUTO_MAX))
        garderArchives = p.getBoolean("garder_archives", false)
        cacherApresJours = bornerJours(p.getInt("cacher_apres_jours", 0))
        presenceEnLigne = p.getBoolean("presence_en_ligne", true)
        sonsConversation = jeuSons(p.getString("sons_conversation", "bulle"))
        notesContacts = p.getBoolean("notes_contacts", true)
        notesMusique = p.getString("notes_musique", "") ?: ""
        notesMusiqueFormat = formatMusique(p.getString("notes_musique_format", "artiste"))
        notesServices = try {
            servicesNotes(JSONArray(p.getString("notes_services", "[\"tout\"]") ?: "[]").chaines())
        } catch (_: Exception) { listOf("tout") }
        ordreServices = if (p.contains("ordre_services")) {
            try {
                nettoyerOrdreServices(JSONArray(p.getString("ordre_services", "[]") ?: "[]").chaines())
            } catch (_: Exception) { emptyList() }
        } else {
            // L'ordre d'avant le partage (ServicesCaches, préférences « affichage »,
            // clés séparées par des virgules) : repris tel quel, envoyé au serveur
            // s'il n'en a pas (voir [recu]).
            nettoyerOrdreServices(
                ctx.applicationContext.getSharedPreferences("affichage", Context.MODE_PRIVATE)
                    .getString("ordre_services", "").orEmpty().split(','),
            )
        }
        personnes = try {
            Personnes.lire(JSONArray(p.getString("personnes", "[]") ?: "[]"))
        } catch (_: Exception) { emptyList() }
        personnesRefus = try {
            Personnes.lireRefus(JSONArray(p.getString("personnes_refus", "[]") ?: "[]"))
        } catch (_: Exception) { emptyList() }
        contactsPerso = try {
            ContactsPerso.lire(JSONObject(p.getString("contacts_perso", "{}") ?: "{}"))
        } catch (_: Exception) { emptyMap() }
        archivesDepuis.clear()
        try {
            val o = JSONObject(p.getString("archives_depuis", "{}") ?: "{}")
            for (k in o.keys()) archivesDepuis[k] = o.optLong(k)
        } catch (_: Exception) {}
        majDepuis()
    }

    /** Une heure d'archivage pour chaque archive (maintenant, si elle manque), aucune pour les autres. */
    private fun majDepuis() {
        val maintenant = System.currentTimeMillis()
        archivesDepuis.keys.retainAll(archives.toSet())
        for (id in archives) if ((archivesDepuis[id] ?: 0L) <= 0L) archivesDepuis[id] = maintenant
    }

    /** Un objet {id: mood} ; les valeurs vides ou non textuelles sont ignorées. */
    private fun lireMoods(brut: String): Map<String, String> = lireObjet("moods", brut, 64)

    private fun lireObjet(cle: String, brut: String, max: Int): Map<String, String> = try {
        objetMoods(JSONObject(brut), max)
    } catch (_: Exception) {
        Log.w("jasette", "marques « $cle » illisibles : mises de côté")
        prefs?.edit()?.putString("${cle}_illisible", brut)?.commit()
        emptyMap()
    }

    /** {id: texte} ; vides, non textuelles ou de plus de [max] octets : ignorées. */
    private fun objetMoods(o: JSONObject?, max: Int = 64): Map<String, String> {
        val r = LinkedHashMap<String, String>()
        if (o == null) return r
        for (k in o.keys()) {
            val v = o.opt(k) as? String ?: continue
            if (k.isNotEmpty() && v.isNotEmpty() && v.toByteArray().size <= max) r[k] = v
        }
        return r
    }

    private fun lire(p: SharedPreferences, cle: String): List<String> {
        val brut = p.getString(cle, "[]") ?: "[]"
        return try {
            JSONArray(brut).chaines().distinct()
        } catch (_: Exception) {
            // Jamais perdu : le texte abîmé est mis de côté avant toute réécriture.
            Log.w("jasette", "marques « $cle » illisibles : mises de côté")
            p.edit().putString("${cle}_illisible", brut).commit()
            emptyList()
        }
    }

    /**
     * commit(), pas apply() : écrit tout de suite sur le disque, pour qu'une
     * mise à jour de l'app juste après un changement ne le perde pas.
     */
    private fun garder() {
        prefs?.edit()
            ?.putString("favoris", JSONArray(favoris.toList()).toString())
            ?.putString("epingles", JSONArray(epingles.toList()).toString())
            ?.putString("head", JSONArray(head.toList()).toString())
            ?.putString("archives", JSONArray(archives.toList()).toString())
            ?.putString("privees", JSONArray(privees.toList()).toString())
            ?.putString("moods", JSONObject(moods.toMap()).toString())
            ?.putString("emojis", JSONObject(emojis.toMap()).toString())
            ?.putString("groupes_auto", JSONObject(groupesAuto.toMap()).toString())
            ?.putBoolean("garder_archives", garderArchives)
            ?.putInt("cacher_apres_jours", cacherApresJours)
            ?.putBoolean("presence_en_ligne", presenceEnLigne)
            ?.putString("sons_conversation", sonsConversation)
            ?.putBoolean("notes_contacts", notesContacts)
            ?.putString("notes_musique", notesMusique)
            ?.putString("notes_musique_format", notesMusiqueFormat)
            ?.putString("notes_services", JSONArray(notesServices).toString())
            ?.putString("ordre_services", JSONArray(ordreServices).toString())
            ?.putString("personnes", Personnes.json(personnes).toString())
            ?.putString("personnes_refus", Personnes.jsonRefus(personnesRefus).toString())
            ?.putString("contacts_perso", ContactsPerso.json(contactsPerso).toString())
            ?.putString("archives_depuis", JSONObject(archivesDepuis.toMap()).toString())
            ?.putLong("version", version)
            ?.putBoolean("a_envoyer", aEnvoyer)
            ?.putString("serveur", serveurMarques)
            ?.commit()
    }

    fun estFavori(id: String) = id in favoris
    fun estEpinglee(id: String) = id in epingles
    fun estDansHead(id: String) = id in head
    fun estArchivee(id: String) = id in archives
    fun estPrivee(id: String) = id in privees

    /** Le mood choisi pour [id] (id du mood), ou null : « Par défaut ». */
    fun mood(id: String): String? = moods[id]

    /** Choisit le mood de [id] ; null ou vide le retire (« Par défaut »). */
    fun choisirMood(id: String, mood: String?) {
        if ((moods[id] ?: "") == (mood ?: "")) return
        if (mood.isNullOrEmpty()) moods.remove(id) else moods[id] = mood
        changement()
    }

    /** L'emoji rapide de [id], ou null : aucun (par défaut). */
    fun emojiRapide(id: String): String? = emojis[id]?.takeIf { it.isNotEmpty() }

    /** Choisit l'emoji rapide de [id] ; null ou vide le retire (« Aucun »). */
    fun choisirEmojiRapide(id: String, emoji: String?) {
        val e = emoji?.takeIf { it.isNotEmpty() && it.toByteArray().size <= EMOJI_RAPIDE_MAX }
        if (emojis[id] == e) return
        if (e == null) emojis.remove(id) else emojis[id] = e
        changement()
    }

    /** La correction à la main du regroupement automatique de [id] ("codes", "promos", "avis", "aucun"), ou null. */
    fun groupeAuto(id: String): String? = groupesAuto[id]

    /** Corrige à la main le groupe de [id] : « Mettre dans … » ou "aucun" (« Ce n'est pas … »). */
    fun corrigerGroupe(id: String, valeur: String) {
        if (lireCorrection(valeur) == null || groupesAuto[id] == valeur) return
        groupesAuto[id] = valeur
        changement()
    }

    /** Rend privée (à la fin de la liste) ou ne l'est plus ; rien si c'est déjà fait. */
    fun rendrePrivee(id: String, oui: Boolean) {
        if (oui == (id in privees)) return
        if (oui) privees.add(id) else privees.remove(id)
        changement()
        // Le partage direct : une privée n'y apparaît jamais.
        Raccourcis.privees()
    }

    /** Toutes les privées redeviennent normales (désactivation de la Liste privée). */
    fun remettrePrivees() {
        if (privees.isEmpty()) return
        privees.clear()
        changement()
        Raccourcis.privees()
    }

    /** Archive par Jasette (à la fin de la liste) ou désarchive ; rien si c'est déjà fait. */
    fun archiver(id: String, oui: Boolean) {
        if (oui == (id in archives)) return
        if (oui) archives.add(id) else archives.remove(id)
        majDepuis()
        changement()
    }

    /**
     * Un message reçu (pas envoyé par moi) dans [id], daté de [temps] (ms) :
     * s'il est plus récent que l'archivage, la conversation revient dans la
     * boîte de réception. Idempotent. En mode client, le serveur fait de même
     * et sa diffusion arrive d'habitude avant le message : rien à faire alors.
     */
    fun desarchiverSiRecu(id: String, temps: Long) {
        // Gardées hors de la boîte de réception : elle reste dans Archives
        // (non lue et notifiée comme d'habitude, voir Depot.recevoirMessage).
        if (garderArchives) return
        val depuis = archivesDepuis[id] ?: return
        if (id !in archives || temps <= depuis) return
        archives.remove(id)
        majDepuis()
        changement()
    }

    /** Ajoute au Head (à la fin) ou l'en retire. */
    fun basculerHead(id: String) {
        if (!head.remove(id)) head.add(id)
        changement()
    }

    /** Ajoute aux favoris (à la fin) ou les en retire. */
    fun basculerFavori(id: String) {
        if (!favoris.remove(id)) favoris.add(id)
        changement()
    }

    /** Épingle (à la fin des épinglées) ou désépingle. */
    fun basculerEpingle(id: String) {
        if (!epingles.remove(id)) epingles.add(id)
        changement()
    }

    /** Monte (-1) ou descend (+1) un favori d'un cran (comme `deplacer_favori`). */
    fun deplacerFavori(id: String, sens: Int) {
        val i = favoris.indexOf(id)
        val j = i + sens
        if (i < 0 || j < 0 || j >= favoris.size) return
        favoris[i] = favoris[j]
        favoris[j] = id
        changement()
    }

    /** Une conversation supprimée n'est plus ni favorite, ni épinglée, ni archivée. */
    fun oublier(id: String) {
        val a = favoris.remove(id)
        val b = epingles.remove(id)
        val c = head.remove(id)
        val d = archives.remove(id)
        val e = privees.remove(id)
        val f = moods.remove(id) != null
        val g = emojis.remove(id) != null
        val h = groupesAuto.remove(id) != null
        if (d) majDepuis()
        if (a || b || c || d || e || f || g || h) changement()
    }

    // ---- La synchronisation avec le serveur Jasette (§5.5) ----

    /** Les quatre listes, figées (head, favoris, épinglées, archives). */
    private data class Listes(
        val head: List<String>,
        val favoris: List<String>,
        val epingles: List<String>,
        val archives: List<String>,
        val privees: List<String>,
        val moods: Map<String, String>,
        val emojis: Map<String, String>,
        val garderArchives: Boolean,
        val cacherApresJours: Int,
        val presenceEnLigne: Boolean,
        val sons: String,
        val notesContacts: Boolean = true,
        val notesMusique: String = "",
        val notesMusiqueFormat: String = "artiste",
        val notesServices: List<String> = listOf("tout"),
        val personnes: List<Personne> = emptyList(),
        val personnesRefus: List<Refus> = emptyList(),
        val contactsPerso: Map<String, ContactPerso> = emptyMap(),
        val ordreServices: List<String> = emptyList(),
        val groupesAuto: Map<String, String> = emptyMap(),
    ) {
        fun vide() = head.isEmpty() && favoris.isEmpty() && epingles.isEmpty() && archives.isEmpty() && privees.isEmpty() &&
            moods.isEmpty() && emojis.isEmpty() && !garderArchives && cacherApresJours == 0 && presenceEnLigne &&
            sons == "bulle" && notesContacts && notesMusique.isEmpty() && notesMusiqueFormat == "artiste" && notesServices == listOf("tout") &&
            personnes.isEmpty() && personnesRefus.isEmpty() && contactsPerso.isEmpty() && ordreServices.isEmpty() && groupesAuto.isEmpty()

        /** Chaque liste de this, dans son ordre, puis les ids de [b] qui n'y sont pas. */
        fun union(b: Listes) = Listes(
            (head + b.head).filter { it.isNotEmpty() }.distinct(),
            (favoris + b.favoris).filter { it.isNotEmpty() }.distinct(),
            (epingles + b.epingles).filter { it.isNotEmpty() }.distinct(),
            (archives + b.archives).filter { it.isNotEmpty() }.distinct(),
            (privees + b.privees).filter { it.isNotEmpty() }.distinct(),
            // Les moods : ceux de this gagnent, puis les conversations que seul [b] a.
            b.moods + moods,
            // De même pour les emojis rapides.
            b.emojis + emojis,
            // Un oui/non : celui de this.
            garderArchives,
            // De même pour la durée.
            cacherApresJours,
            presenceEnLigne,
            sons,
            notesContacts,
            notesMusique,
            notesMusiqueFormat,
            notesServices,
            // Les personnes par id (les nôtres gagnent), une conversation à une seule ; l'union des refus.
            Personnes.unir(personnes, b.personnes),
            Personnes.nettoyerRefus(personnesRefus + b.personnesRefus),
            // Comme les moods : les entrées de this gagnent.
            ContactsPerso.nettoyer(b.contactsPerso + contactsPerso),
            // L'ordre des services : celui de this ; vide (jamais choisi), celui de [b].
            ordreServices.ifEmpty { b.ordreServices },
            // Les corrections du regroupement : comme les emojis, les nôtres gagnent.
            b.groupesAuto + groupesAuto,
        )

        /** Les ids de this absents de [b], liste par liste. */
        fun moins(b: Listes) = Listes(
            head - b.head.toSet(), favoris - b.favoris.toSet(), epingles - b.epingles.toSet(),
            archives - b.archives.toSet(), privees - b.privees.toSet(),
            moods.filter { (k, v) -> b.moods[k] != v },
            emojis.filter { (k, v) -> b.emojis[k] != v },
            garderArchives,
            cacherApresJours,
            presenceEnLigne,
            sons,
            notesContacts,
            notesMusique,
            notesMusiqueFormat,
            notesServices,
            personnes.filter { it !in b.personnes },
            personnesRefus.filter { it !in b.personnesRefus },
            contactsPerso.filter { (k, v) -> b.contactsPerso[k] != v },
            ordreServices,
            groupesAuto.filter { (k, v) -> b.groupesAuto[k] != v },
        )
    }

    private fun listes() =
        Listes(
            head.toList(), favoris.toList(), epingles.toList(), archives.toList(), privees.toList(), moods.toMap(), emojis.toMap(),
            garderArchives, cacherApresJours, presenceEnLigne, sonsConversation,
            notesContacts, notesMusique, notesMusiqueFormat, notesServices,
            personnes, personnesRefus, contactsPerso, ordreServices, groupesAuto.toMap(),
        )

    private fun poser(l: Listes) {
        if (head.toList() != l.head) { head.clear(); head.addAll(l.head) }
        if (favoris.toList() != l.favoris) { favoris.clear(); favoris.addAll(l.favoris) }
        if (epingles.toList() != l.epingles) { epingles.clear(); epingles.addAll(l.epingles) }
        if (archives.toList() != l.archives) { archives.clear(); archives.addAll(l.archives); majDepuis() }
        if (privees.toList() != l.privees) {
            privees.clear()
            privees.addAll(l.privees)
            Raccourcis.privees()
        }
        if (moods.toMap() != l.moods) { moods.clear(); moods.putAll(l.moods) }
        if (emojis.toMap() != l.emojis) { emojis.clear(); emojis.putAll(l.emojis) }
        if (groupesAuto.toMap() != l.groupesAuto) { groupesAuto.clear(); groupesAuto.putAll(l.groupesAuto) }
        garderArchives = l.garderArchives
        cacherApresJours = l.cacherApresJours
        if (presenceEnLigne != l.presenceEnLigne) {
            presenceEnLigne = l.presenceEnLigne
            surPresence?.invoke(l.presenceEnLigne)
        }
        sonsConversation = l.sons
        if (notesContacts != l.notesContacts) {
            notesContacts = l.notesContacts
            surNotesContacts?.invoke(l.notesContacts)
        }
        notesMusique = l.notesMusique
        notesMusiqueFormat = l.notesMusiqueFormat
        notesServices = l.notesServices
        personnes = l.personnes
        personnesRefus = l.personnesRefus
        contactsPerso = l.contactsPerso
        ordreServices = l.ordreServices
    }

    /** Envoie une trame au serveur ; faux si la connexion est coupée. Posé par [Depot] en mode client. */
    private var envoyeur: ((JSONObject) -> Boolean)? = null
    /** L'empreinte du serveur branché (mode client), ou vide. */
    private var serveurBranche = ""
    /** Le serveur a donné son état sur cette connexion (rejeu reçu). */
    private var synchro = false
    /** Notre trame en attente de réponse : son id et ce qu'elle portait. */
    private var enVol: Pair<Long, Listes>? = null
    private var prochain = 0L

    /** Mode client (ou plus de serveur si [envoi] est null) : tout recommence, on attend le rejeu. */
    fun brancher(empreinte: String?, envoi: ((JSONObject) -> Boolean)?) {
        serveurBranche = empreinte ?: ""
        envoyeur = envoi
        synchro = false
        enVol = null
    }

    private fun changement() {
        aEnvoyer = true
        garder()
        CacheMarques.synchroniser()
        pousser()
    }

    private fun pousser() {
        val envoi = envoyeur ?: return
        if (!synchro || enVol != null || !aEnvoyer) return
        val l = listes()
        prochain++
        enVol = prochain to l
        val corps = JSONObject().put("head", JSONArray(l.head)).put("favoris", JSONArray(l.favoris))
            .put("epingles", JSONArray(l.epingles)).put("archives", JSONArray(l.archives))
            .put("privees", JSONArray(l.privees))
            .put("moods", JSONObject(l.moods))
            .put("emojis", JSONObject(l.emojis))
            .put("garder_archives", l.garderArchives)
            .put("cacher_apres_jours", l.cacherApresJours)
            .put("presence_en_ligne", l.presenceEnLigne)
            .put("sons_conversation", l.sons)
            .put("notes_contacts", l.notesContacts)
            .put("notes_musique", l.notesMusique)
            .put("notes_musique_format", l.notesMusiqueFormat)
            .put("notes_services", JSONArray(l.notesServices))
            .put("personnes", Personnes.json(l.personnes))
            .put("personnes_refus", Personnes.jsonRefus(l.personnesRefus))
            .put("contacts_perso", ContactsPerso.json(l.contactsPerso))
            .put("ordre_services", JSONArray(l.ordreServices))
            .put("groupes_auto", JSONObject(l.groupesAuto))
            .put("version", version).put("id", prochain)
        if (!envoi(JSONObject().put("marques", corps))) {
            // La connexion tombe : le rejeu suivant tranchera.
            synchro = false
            enVol = null
        }
    }

    /** Une trame `marques` du serveur, ou null : la connexion est tombée. Sur le fil principal. */
    fun recu(m: JSONObject?) {
        if (m == null) {
            synchro = false
            enVol = null
            return
        }
        val s = Listes(
            m.optJSONArray("head").chaines().distinct(),
            m.optJSONArray("favoris").chaines().distinct(),
            m.optJSONArray("epingles").chaines().distinct(),
            // Un serveur d'avant les archives ne les envoie pas : elles restent
            // alors propres à cet appareil (le serveur est censé avoir les nôtres).
            if (m.has("archives")) m.optJSONArray("archives").chaines().distinct() else archives.toList(),
            // De même pour les privées (serveur d'avant `privees`).
            if (m.has("privees")) m.optJSONArray("privees").chaines().distinct() else privees.toList(),
            // Et pour les moods (serveur d'avant `moods`) : les nôtres restent, jamais vidés.
            if (m.optJSONObject("moods") != null) objetMoods(m.optJSONObject("moods")) else moods.toMap(),
            // Et pour les emojis rapides (serveur d'avant `emojis`).
            if (m.optJSONObject("emojis") != null) objetMoods(m.optJSONObject("emojis"), EMOJI_RAPIDE_MAX) else emojis.toMap(),
            // Et pour `garder_archives` (serveur d'avant ce réglage) : le nôtre reste.
            if (m.opt("garder_archives") is Boolean) m.optBoolean("garder_archives") else garderArchives,
            // Et pour `cacher_apres_jours` (serveur d'avant ce réglage) : le nôtre reste.
            if (m.opt("cacher_apres_jours") is Number) bornerJours(m.optInt("cacher_apres_jours")) else cacherApresJours,
            // Et pour `presence_en_ligne` (absent : le nôtre reste ; vrai par défaut).
            if (m.opt("presence_en_ligne") is Boolean) m.optBoolean("presence_en_ligne") else presenceEnLigne,
            // Et pour `sons_conversation` (absent : le nôtre reste) ; un booléen : vrai "bulle", faux "aucun".
            when (val j = m.opt("sons_conversation")) {
                is Boolean -> if (j) "bulle" else "aucun"
                is String -> jeuSons(j)
                else -> sonsConversation
            },
            // Les réglages de « Quoi de neuf ? » (absents : les nôtres restent).
            if (m.opt("notes_contacts") is Boolean) m.optBoolean("notes_contacts") else notesContacts,
            (m.opt("notes_musique") as? String)?.let { bornerOctets(it, 128) } ?: notesMusique,
            (m.opt("notes_musique_format") as? String)?.let { formatMusique(it) } ?: notesMusiqueFormat,
            m.optJSONArray("notes_services")?.let { servicesNotes(it.chaines()) } ?: notesServices,
            // Et pour les personnes (serveur d'avant la fiche de la personne) : les nôtres restent.
            m.optJSONArray("personnes")?.let { Personnes.lire(it) } ?: personnes,
            m.optJSONArray("personnes_refus")?.let { Personnes.lireRefus(it) } ?: personnesRefus,
            // Et pour les contacts perso (serveur d'avant le crayon de la fiche) : les nôtres restent.
            m.optJSONObject("contacts_perso")?.let { ContactsPerso.lire(it) } ?: contactsPerso,
            // Et pour l'ordre des services (serveur d'avant `ordre_services`) : le nôtre reste.
            m.optJSONArray("ordre_services")?.let { nettoyerOrdreServices(it.chaines()) } ?: ordreServices,
            // Et pour le regroupement automatique (serveur d'avant `groupes_auto`) : les nôtres restent.
            m.optJSONObject("groupes_auto")?.let { objetMoods(it, GROUPE_AUTO_MAX) } ?: groupesAuto.toMap(),
        )
        val v = m.optLong("version", -1)
        if (v < 0) return
        val id = if (m.has("id")) m.optLong("id", -1) else -1
        val refus = m.optString("refus", "")
        val avant = Triple(listes(), version, aEnvoyer)
        val serveurAvant = serveurMarques
        // Un autre serveur que celui des marques locales : jamais synchronisé avec lui.
        if (serveurBranche.isNotEmpty() && serveurMarques != serveurBranche) {
            serveurMarques = serveurBranche
            version = 0
        }
        synchro = true
        val vol = enVol
        when {
            vol != null && id == vol.first -> {
                enVol = null
                version = v
                val l = if (refus.isNotEmpty()) {
                    // Refusée (version périmée qui aurait vidé une liste) : l'ordre du serveur, puis le nôtre.
                    Log.w("jasette", "marques refusées par le serveur : union avec les siennes")
                    s.union(listes())
                } else {
                    // Acceptée : nos changements faits depuis, plus ce que le serveur a ajouté.
                    listes().union(s.moins(vol.second))
                }
                poser(l)
                aEnvoyer = l != s
            }
            // La diffusion d'un autre appareil a croisé notre trame : sa réponse tranchera.
            vol != null -> return
            // Le serveur n'a encore rien : il prend nos marques.
            v == 0L -> {
                version = 0
                aEnvoyer = !listes().vide()
            }
            // Rien de changé ici : le serveur est la source. Sauf l'ordre des
            // services choisi ici avant qu'il soit partagé, quand le serveur n'en
            // a pas encore : l'union (plus bas) le garde et l'envoie, une fois.
            !aEnvoyer && version in 1L..v && !(s.ordreServices.isEmpty() && ordreServices.isNotEmpty()) -> {
                poser(s)
                version = v
            }
            // Changé ici pendant une coupure, et personne d'autre : la copie locale part.
            aEnvoyer && version == v -> {}
            // Première synchronisation avec ce serveur, ou changé des deux côtés : union.
            else -> {
                val l = s.union(listes())
                poser(l)
                version = v
                aEnvoyer = l != s
            }
        }
        if (Triple(listes(), version, aEnvoyer) != avant || serveurMarques != serveurAvant) garder()
        if (listes() != avant.first) CacheMarques.synchroniser()
        surContactsPerso?.invoke(contactsPerso)
        pousser()
    }
}

/** Un jeu de sons connu, sinon "bulle" (le défaut). */
fun jeuSons(j: String?): String = when (j) {
    "bulle", "bois", "verre", "aucun" -> j
    else -> "bulle"
}

/**
 * `notes_services` normalisé (PROTOCOLE.md §5.5) : sans doublon ni nom vide,
 * 32 noms de 32 octets au plus ; vide ou contenant "tout" : ["tout"].
 */
fun servicesNotes(l: List<String>): List<String> {
    val n = l.filter { it.isNotEmpty() && it.toByteArray().size <= 32 }.distinct().take(32)
    return if (n.isEmpty() || "tout" in n) listOf("tout") else n
}

/**
 * `ordre_services` normalisé comme par le serveur (PROTOCOLE.md §5.5) : les
 * clés, sans doublon ni clé vide, 32 de 32 octets au plus ; vide : l'ordre par défaut.
 */
fun nettoyerOrdreServices(l: List<String>): List<String> =
    l.map { it.trim() }.filter { it.isNotEmpty() && it.toByteArray().size <= 32 }.distinct().take(32)

/** [s] coupé à [max] octets UTF-8, sans couper un caractère. */
fun bornerOctets(s: String, max: Int): String {
    if (s.toByteArray().size <= max) return s
    var i = 0
    var octets = 0
    while (i < s.length) {
        val cp = s.codePointAt(i)
        val n = String(Character.toChars(cp)).toByteArray().size
        if (octets + n > max) break
        octets += n
        i += Character.charCount(cp)
    }
    return s.substring(0, i)
}

/** Un format de note musicale connu, sinon "artiste" (le défaut). */
fun formatMusique(f: String?): String = when (f) {
    "artiste", "genre", "titre" -> f
    else -> "artiste"
}
