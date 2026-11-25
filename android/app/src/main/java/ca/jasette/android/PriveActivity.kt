package ca.jasette.android

import android.os.Bundle
import android.view.WindowManager
import androidx.activity.ComponentActivity
import androidx.activity.SystemBarStyle
import androidx.activity.compose.setContent
import androidx.activity.enableEdgeToEdge
import ca.jasette.android.ui.EcranPrive
import ca.jasette.android.ui.JasetteTheme

/**
 * La vue Privées, ouverte par le lanceur au cadenas (`.LanceurPrive`, activé
 * seulement quand la Liste privée l'est). Une tâche à part (taskAffinity) :
 * l'icône normale ne la ramène jamais. FLAG_SECURE : ni capture d'écran, ni
 * aperçu dans les apps récentes. Elle se reverrouille dès qu'elle passe en
 * arrière-plan, et après 1 minute sans toucher l'écran ([Prive.expiree]).
 */
class PriveActivity : ComponentActivity() {

    override fun onCreate(savedInstanceState: Bundle?) {
        enableEdgeToEdge(
            statusBarStyle = SystemBarStyle.dark(0),
            navigationBarStyle = SystemBarStyle.dark(0),
        )
        super.onCreate(savedInstanceState)
        window.setFlags(WindowManager.LayoutParams.FLAG_SECURE, WindowManager.LayoutParams.FLAG_SECURE)
        PontService.canaux(this)
        PontService.lancer(this)
        Depot.demarrer(this)
        setContent { JasetteTheme { EcranPrive(this) } }
    }

    override fun onStart() {
        super.onStart()
        Depot.visible = true
    }

    override fun onUserInteraction() {
        super.onUserInteraction()
        Prive.toucher()
    }

    override fun onStop() {
        Depot.visible = false
        Prive.verrouiller()
        CacheConvs.ecrireMaintenant()
        super.onStop()
    }
}
