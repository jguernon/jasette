package ca.jasette.android

import android.util.Log
import org.json.JSONObject
import java.io.BufferedWriter
import java.io.File
import java.io.OutputStreamWriter
import java.util.concurrent.Executors
import java.util.concurrent.TimeUnit

/**
 * Un service vu par l'interface : un pont local ([PontLocal]) ou le même
 * service servi par un serveur Jasette ([PontDistant], voir Serveur.kt).
 * Comme `Pont` du bureau (src/pont.rs) : le reste de l'app ne voit pas la différence.
 */
interface Pont {
    val nom: String
    fun envoyer(commande: JSONObject)
    fun vivant(): Boolean
    fun arreter()
}

/**
 * Un pont Go (`libjasette-pont.so` ou `libjasette-meta.so`), lancé comme processus.
 *
 * Même protocole que sur le bureau (src/pont.rs) : une commande JSON par ligne
 * sur son entrée standard, une nouvelle JSON par ligne sur sa sortie standard,
 * ses journaux sur sa sortie d'erreur (renvoyés dans logcat, étiquette « jasette-<nom> »).
 *
 * [surNouvelle] est appelée depuis le fil de lecture ; à la fin du processus,
 * elle reçoit `{"ev":"pont_ferme"}`, comme sur le bureau.
 */
class PontLocal(
    override val nom: String,
    executable: File,
    env: Map<String, String>,
    private val surNouvelle: (JSONObject) -> Unit,
) : Pont {
    private val processus: Process
    private val entree: BufferedWriter
    // Les écritures passent par un seul fil : jamais d'attente sur le fil de l'interface.
    private val ecrivain = Executors.newSingleThreadExecutor { r -> Thread(r, "pont-$nom-entree") }
    @Volatile private var arrete = false

    init {
        val pb = ProcessBuilder(executable.path)
        pb.environment().putAll(env)
        pb.redirectErrorStream(false)
        processus = pb.start()
        entree = BufferedWriter(OutputStreamWriter(processus.outputStream, Charsets.UTF_8))

        Thread({
            try {
                processus.inputStream.bufferedReader(Charsets.UTF_8).useLines { lignes ->
                    for (ligne in lignes) {
                        if (ligne.isBlank()) continue
                        try {
                            surNouvelle(JSONObject(ligne))
                        } catch (e: Exception) {
                            Log.w("jasette-$nom", "ligne illisible du pont : ${e.message}")
                        }
                    }
                }
            } catch (e: Exception) {
                Log.w("jasette-$nom", "lecture : ${e.message}")
            }
            surNouvelle(JSONObject().put("ev", "pont_ferme"))
        }, "pont-$nom-sortie").start()

        Thread({
            try {
                processus.errorStream.bufferedReader(Charsets.UTF_8).useLines { lignes ->
                    lignes.forEach { Log.i("jasette-$nom", it) }
                }
            } catch (_: Exception) {
            }
        }, "pont-$nom-journal").start()
    }

    override fun envoyer(commande: JSONObject) {
        if (arrete) return
        val ligne = commande.toString()
        ecrivain.execute {
            try {
                entree.write(ligne)
                entree.newLine()
                entree.flush()
            } catch (e: Exception) {
                Log.w("jasette-$nom", "écriture : ${e.message}")
            }
        }
    }

    override fun vivant(): Boolean = processus.isAlive

    /** « quit », puis on attend un peu avant de forcer. */
    override fun arreter() {
        if (arrete) return
        envoyer(JSONObject().put("cmd", "quit"))
        arrete = true
        ecrivain.shutdown()
        Thread {
            try {
                ecrivain.awaitTermination(1, TimeUnit.SECONDS)
                entree.close()
                if (!processus.waitFor(3, TimeUnit.SECONDS)) processus.destroy()
            } catch (_: Exception) {
                processus.destroy()
            }
        }.start()
    }
}
