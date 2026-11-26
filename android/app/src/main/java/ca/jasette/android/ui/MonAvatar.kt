package ca.jasette.android.ui

import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.PickVisualMediaRequest
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Check
import androidx.compose.material.icons.filled.AccountCircle
import androidx.compose.material.icons.filled.Person
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import ca.jasette.android.Depot
import ca.jasette.android.Marques
import ca.jasette.android.Service
import ca.jasette.android.t
import java.io.File

/** La photo de mon avatar selon le choix (comme `menu_avatar_vue` du bureau), ou null. */
private fun photoMoi(): String? = when (Marques.avatarSource) {
    "google" -> Depot.avatarsMoi[Service.GOOGLE]
    "messenger" -> Depot.avatarsMoi[Service.MESSENGER]
    "instagram" -> Depot.avatarsMoi[Service.INSTAGRAM]
    "fichier" -> Marques.avatarFichier?.takeIf { File(it).isFile }
    // WhatsApp, Signal, X (choisis par défaut quand leur photo arrive la première).
    else -> Service.deCle(Marques.avatarSource)?.let { Depot.avatarsMoi[it] }
}

/** Mon initiale : d'après le nom d'un compte connecté. */
private fun initialeMoi(): String? =
    (listOf(Service.MESSENGER, Service.INSTAGRAM, Service.GOOGLE) + Service.entries.filter { it.serveurSeulement })
        .firstNotNullOfOrNull { Depot.noms[it] }
        ?.firstOrNull { it.isLetter() }?.uppercase()

@Composable
fun AvatarMoi(taille: Dp, source: String = Marques.avatarSource) {
    val photo = when (source) {
        Marques.avatarSource -> photoMoi()
        "google" -> Depot.avatarsMoi[Service.GOOGLE]
        "messenger" -> Depot.avatarsMoi[Service.MESSENGER]
        "instagram" -> Depot.avatarsMoi[Service.INSTAGRAM]
        "fichier" -> Marques.avatarFichier?.takeIf { File(it).isFile }
        else -> Service.deCle(source)?.let { Depot.avatarsMoi[it] }
    }
    val rond = Modifier.cadreAvatar(taille)
    if (photo != null) {
        ImageFichier(photo, rond.background(P.surface), cote = 256, pixels = if (P.retro) PIXELS_RETRO else 0)
    } else {
        Box(rond.background(fondAvatar(P.avatarDefaut)), contentAlignment = Alignment.Center) {
            val i = initialeMoi()
            if (i != null) {
                Text(i, color = Color.White, fontSize = (taille.value * 0.42f).sp, fontWeight = FontWeight.SemiBold)
            } else {
                Icon(Icons.Filled.Person, null, tint = Color.White, modifier = Modifier.size(taille * 0.55f))
            }
        }
    }
}

/** Mon avatar, qui ouvre le menu des choix d'avatar. */
@Composable
fun BoutonAvatarMoi(taille: Dp) {
    var menu by remember { mutableStateOf(false) }
    Box(Modifier.clip(formeRonde).clickable { menu = true }.padding(4.dp)) { AvatarMoi(taille) }
    if (menu) MenuAvatar { menu = false }
}

/** Le menu de l'avatar : Google, Facebook, Instagram, une image du téléphone, ou l'initiale. */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun MenuAvatar(fermer: () -> Unit) {
    val ctx = LocalContext.current
    val actuel = Marques.avatarSource.ifEmpty { "initiale" }
    val choisirImage = rememberLauncherForActivityResult(ActivityResultContracts.PickVisualMedia()) { uri ->
        if (uri != null) {
            // Copiée dans les fichiers privés : l'image reste même si l'originale disparaît.
            val dest = File(ctx.filesDir, "avatar-${System.currentTimeMillis()}.img")
            runCatching {
                ctx.contentResolver.openInputStream(uri)?.use { e -> dest.outputStream().use { e.copyTo(it) } }
            }.onSuccess {
                Marques.avatarFichier?.let { ancien -> if (ancien != dest.path) File(ancien).delete() }
                Marques.choisirAvatar("fichier", dest.path)
                fermer()
            }
        }
    }
    ModalBottomSheet(onDismissRequest = fermer, containerColor = P.menu) {
        Column(Modifier.fillMaxWidth().navigationBarsPadding().padding(bottom = 12.dp)) {
            Text(
                t("Votre avatar"), color = P.texteFort, fontWeight = FontWeight.Bold, fontSize = 18.sp,
                modifier = Modifier.padding(horizontal = 20.dp, vertical = 8.dp),
            )
            @Composable
            fun Ligne(cle: String, titre: String, detail: String, dispo: Boolean, image: @Composable () -> Unit, action: () -> Unit) {
                Row(
                    Modifier.fillMaxWidth().let { if (dispo) it.clickable(onClick = action) else it }
                        .padding(horizontal = 20.dp, vertical = 10.dp),
                    verticalAlignment = Alignment.CenterVertically,
                    horizontalArrangement = Arrangement.spacedBy(14.dp),
                ) {
                    Box(Modifier.size(40.dp), contentAlignment = Alignment.Center) { image() }
                    Column(Modifier.weight(1f)) {
                        Text(titre, color = if (dispo) P.texteFort else P.textePale, fontWeight = FontWeight.SemiBold, fontSize = 15.sp)
                        Text(detail, color = P.textePale, fontSize = 13.sp)
                    }
                    if (cle == actuel) Icon(Icons.Filled.Check, null, tint = P.accent, modifier = Modifier.size(20.dp))
                }
            }
            fun choisir(cle: String) {
                Marques.choisirAvatar(cle)
                fermer()
            }
            val google = Depot.avatarsMoi[Service.GOOGLE] != null
            Ligne("google", t("Avatar de Google Messages"),
                if (google) t("Votre fiche sur le téléphone") else t("Aucune photo trouvée"), google,
                { AvatarAvecLogo("google", Service.GOOGLE) }) { choisir("google") }
            val fb = Depot.avatarsMoi[Service.MESSENGER] != null
            Ligne("messenger", t("Avatar de Facebook"),
                if (fb) (Depot.noms[Service.MESSENGER] ?: "Messenger") else t("Quand Messenger sera connecté"), fb,
                { AvatarAvecLogo("messenger", Service.MESSENGER) }) { choisir("messenger") }
            val ig = Depot.avatarsMoi[Service.INSTAGRAM] != null
            Ligne("instagram", t("Avatar d'Instagram"),
                if (ig) (Depot.noms[Service.INSTAGRAM] ?: "Instagram") else t("Quand Instagram sera connecté"), ig,
                { AvatarAvecLogo("instagram", Service.INSTAGRAM) }) { choisir("instagram") }
            Ligne("fichier", t("Choisir une image…"),
                Marques.avatarFichier?.let { t("Une image de votre téléphone") } ?: t("Une image de votre téléphone"), true,
                {
                    if (Marques.avatarFichier != null) AvatarMoi(36.dp, "fichier")
                    else Icon(Icons.Filled.AccountCircle, null, tint = P.icone, modifier = Modifier.size(26.dp))
                }) {
                choisirImage.launch(PickVisualMediaRequest(ActivityResultContracts.PickVisualMedia.ImageOnly))
            }
            Ligne("initiale", t("Initiale"), t("Sans photo"), true, { AvatarMoi(36.dp, "initiale") }) { choisir("initiale") }
            Spacer(Modifier.width(1.dp))
        }
    }
}

@Composable
private fun AvatarAvecLogo(source: String, s: Service) {
    Box(Modifier.size(40.dp)) {
        AvatarMoi(36.dp, source)
        LogoService(s, 16.dp, Modifier.align(Alignment.BottomEnd))
    }
}
