package ca.jasette.android

import org.json.JSONObject

/**
 * La version publiée sur le site : `version.json`, à côté de l'APK
 * (https://jonathanguernon.net/jasette/telecharger/version.json), écrit par
 * paquets/empaqueter.sh à partir de l'APK release signé :
 *
 *     {"versionCode": 10112, "versionName": "1.0.0 (112)",
 *      "url": "https://jonathanguernon.net/jasette/telecharger/jasette-1.0.0.apk?v=10112",
 *      "sha256": "…64 chiffres hexadécimaux…", "taille": 56623104,
 *      "notes": {"fr": "…", "en": "…"}}
 *
 * Rien d'Android ici (seulement org.json) : c'est ce que testent les tests unitaires.
 */
data class VersionDispo(
    val versionCode: Long,
    val versionName: String,
    val url: String,
    val sha256: String,
    /** En octets ; 0 si le fichier ne le dit pas. */
    val taille: Long,
    val notesFr: String,
    val notesEn: String,
) {
    /** Les notes dans la langue voulue (l'autre si celle-là manque). */
    fun notes(anglais: Boolean): String =
        if (anglais) notesEn.ifBlank { notesFr } else notesFr.ifBlank { notesEn }

    companion object {
        /** Le seul hôte d'où Jasette accepte un APK (le site de Jonathan, en HTTPS). */
        const val HOTE = "jonathanguernon.net"
        const val ADRESSE_VERSION = "https://$HOTE/jasette/telecharger/version.json"

        private val HEX64 = Regex("^[0-9a-f]{64}$")

        /**
         * Lit `version.json` ; null s'il est illisible ou douteux : pas de
         * versionCode positif, adresse autre que https://jonathanguernon.net/…,
         * ou SHA-256 qui n'a pas 64 chiffres hexadécimaux.
         */
        fun lire(json: String): VersionDispo? {
            val o = try {
                JSONObject(json)
            } catch (_: Exception) {
                return null
            }
            val code = o.optLong("versionCode", 0L)
            if (code <= 0L) return null
            val url = o.optString("url", "").trim()
            if (!adresseSure(url)) return null
            val sha = o.optString("sha256", "").trim().lowercase()
            if (!HEX64.matches(sha)) return null
            val notes = o.optJSONObject("notes")
            return VersionDispo(
                versionCode = code,
                versionName = o.optString("versionName", "").trim().ifEmpty { code.toString() },
                url = url,
                sha256 = sha,
                taille = o.optLong("taille", 0L).coerceAtLeast(0L),
                notesFr = notes?.optString("fr", "")?.trim().orEmpty(),
                notesEn = notes?.optString("en", "")?.trim().orEmpty(),
            )
        }

        /** HTTPS seulement, et seulement sur [HOTE] (pas de sous-domaine ni d'autre port). */
        fun adresseSure(url: String): Boolean {
            val u = try {
                java.net.URI(url)
            } catch (_: Exception) {
                return false
            }
            return u.scheme.equals("https", ignoreCase = true) &&
                u.host.equals(HOTE, ignoreCase = true) &&
                u.port == -1 && u.userInfo == null && !u.path.isNullOrEmpty()
        }

        /** La version publiée est-elle plus récente que celle installée ? (le versionCode seul compte) */
        fun plusRecente(publiee: VersionDispo?, installee: Long): Boolean =
            publiee != null && publiee.versionCode > installee
    }
}
