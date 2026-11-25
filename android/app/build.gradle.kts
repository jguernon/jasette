// Jasette Android : l'interface en Compose, les ponts Go du bureau comme processus.
import java.util.Properties

plugins {
    id("com.android.application")
    id("org.jetbrains.kotlin.plugin.compose")
}

// La version vient de Cargo.toml, comme celle du bureau.
val versionJasette: String = rootProject.file("../Cargo.toml").readLines()
    .first { it.startsWith("version = ") }
    .substringAfter('"').substringBefore('"')

// Le versionCode (Google Play exige qu'il augmente à chaque envoi) se déduit de
// la version : X.Y.Z → X·1 000 000 + Y·1 000 + Z (0.1.0 → 1000). Il dépasse
// l'ancien versionCode 1 des APK déjà distribués, qui se mettent donc à jour.
val codeJasette: Int = versionJasette.split('.', '-').take(3)
    .map { it.toIntOrNull() ?: 0 }
    .plus(listOf(0, 0, 0))
    .let { (x, y, z) -> x * 1_000_000 + y * 1_000 + z }

// La clé de signature release (aussi la clé d'importation de Google Play) :
// ../signing/jasette-release.properties (storeFile, storePassword, keyAlias,
// keyPassword), jamais dans git. Sans ce fichier, tout se compile comme avant
// (debug), et un APK release sort non signé.
val fichierSignature = rootProject.file("../signing/jasette-release.properties")
val signature: Properties? = if (fichierSignature.isFile) {
    Properties().apply { fichierSignature.inputStream().use { load(it) } }
} else null

android {
    namespace = "ca.jasette.android"
    compileSdk = 37

    defaultConfig {
        applicationId = "ca.jasette"
        minSdk = 26
        targetSdk = 36
        versionCode = codeJasette
        versionName = versionJasette
        // Les ponts ne sont compilés que pour arm64 (les téléphones).
        ndk { abiFilters += "arm64-v8a" }
    }

    signingConfigs {
        if (signature != null) {
            create("release") {
                storeFile = fichierSignature.parentFile.resolve(signature.getProperty("storeFile"))
                storePassword = signature.getProperty("storePassword")
                keyAlias = signature.getProperty("keyAlias")
                keyPassword = signature.getProperty("keyPassword")
            }
        }
    }

    buildTypes {
        release {
            // R8 (minify) reste éteint, comme pour le debug.
            isMinifyEnabled = false
            signingConfigs.findByName("release")?.let { signingConfig = it }
        }
    }

    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }

    buildFeatures {
        compose = true
    }

    // Les ponts Go compilés (build-ponts.sh) : ../.cache/android-jni/arm64-v8a.
    sourceSets["main"].jniLibs.directories.add(rootProject.file("../.cache/android-jni").path)

    // Les ponts sont des exécutables : ils doivent être extraits sur le disque
    // (nativeLibraryDir) pour qu'on puisse les lancer.
    // Dans un App Bundle (Google Play), le même réglage met
    // android:extractNativeLibs="true" dans le manifeste et
    // uncompressNativeLibraries=false dans BundleConfig.pb : les APK que Play
    // génère gardent les .so compressés, et l'installeur les extrait.
    packaging {
        jniLibs.useLegacyPackaging = true
    }
}

// Compile les ponts Go avant l'APK (le cache de Go rend ça rapide quand rien n'a changé).
val ponts = tasks.register<Exec>("ponts") {
    description = "Compile les ponts Go pour android/arm64."
    workingDir = rootProject.projectDir
    commandLine("bash", rootProject.file("build-ponts.sh").path)
}
tasks.named("preBuild") { dependsOn(ponts) }

dependencies {
    implementation(platform("androidx.compose:compose-bom:2026.09.00"))
    implementation("androidx.compose.ui:ui")
    implementation("androidx.compose.material3:material3")
    implementation("androidx.compose.material:material-icons-core:1.7.8")
    implementation("androidx.activity:activity-compose:1.13.0")
    implementation("androidx.core:core-ktx:1.19.1")
    implementation("androidx.lifecycle:lifecycle-runtime-compose:2.11.0")
    implementation("androidx.webkit:webkit:1.14.0")
    implementation("org.jetbrains.kotlinx:kotlinx-coroutines-android:1.11.0")
    // Les codes QR de l'association WhatsApp et Signal (zxing core : l'encodeur seul, sans caméra).
    implementation("com.google.zxing:core:3.5.3")
    // Le flou derrière les barres (Haze) et le bouton + en verre liquide (haze-glass), Compose 1.12.
    implementation("dev.chrisbanes.haze:haze:2.1.0")
    implementation("dev.chrisbanes.haze:haze-blur:2.1.0")
    implementation("dev.chrisbanes.haze:haze-glass:2.1.0")
}
