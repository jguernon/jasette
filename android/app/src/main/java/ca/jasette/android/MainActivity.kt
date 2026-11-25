package ca.jasette.android

import android.Manifest
import android.content.Intent
import android.os.Build
import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.SystemBarStyle
import androidx.activity.compose.setContent
import androidx.activity.enableEdgeToEdge
import androidx.activity.result.contract.ActivityResultContracts
import ca.jasette.android.ui.App
import ca.jasette.android.ui.JasetteTheme

class MainActivity : ComponentActivity() {

    private val demandeNotifs = registerForActivityResult(ActivityResultContracts.RequestPermission()) {}

    override fun onCreate(savedInstanceState: Bundle?) {
        enableEdgeToEdge(
            statusBarStyle = SystemBarStyle.dark(0),
            navigationBarStyle = SystemBarStyle.dark(0),
        )
        super.onCreate(savedInstanceState)
        PontService.canaux(this)
        PontService.lancer(this)
        Depot.demarrer(this)
        if (Build.VERSION.SDK_INT >= 33) demandeNotifs.launch(Manifest.permission.POST_NOTIFICATIONS)
        lireIntent(intent)
        setContent { JasetteTheme { App(this) } }
    }

    override fun onNewIntent(intent: Intent) {
        super.onNewIntent(intent)
        lireIntent(intent)
    }

    private fun lireIntent(intent: Intent?) {
        intent?.getStringExtra(EXTRA_CONV)?.let { Depot.aOuvrir = it }
    }

    override fun onStart() {
        super.onStart()
        Depot.visible = true
    }

    override fun onStop() {
        Depot.visible = false
        // L'app passe en arrière-plan : le cache des conversations est écrit sans attendre.
        CacheConvs.ecrireMaintenant()
        super.onStop()
    }

    companion object {
        const val EXTRA_CONV = "conv"
    }
}
