package ca.jasette.android

import android.app.Application

/** L'application : rien d'autre que le point d'entrée (l'état vit dans [Depot]). */
class JasetteApp : Application() {
    override fun onCreate() {
        super.onCreate()
        Langue.charger(this)
        Marques.charger(this)
        CacheMarques.charger(this)
        ca.jasette.android.ui.Apparence.charger(this)
    }
}
