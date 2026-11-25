// Jasette Android : l'interface en Compose, les ponts Go du bureau comme processus.
plugins {
    id("com.android.application")
    id("org.jetbrains.kotlin.plugin.compose")
}

// La version vient de Cargo.toml, comme celle du bureau.
val versionJasette: String = rootProject.file("../Cargo.toml").readLines()
    .first { it.startsWith("version = ") }
    .substringAfter('"').substringBefore('"')

android {
    namespace = "ca.jasette.android"
    compileSdk = 37

    defaultConfig {
        applicationId = "ca.jasette"
        minSdk = 26
        targetSdk = 36
        versionCode = 1
        versionName = versionJasette
        // Les ponts ne sont compilés que pour arm64 (les téléphones).
        ndk { abiFilters += "arm64-v8a" }
    }

    buildTypes {
        release {
            isMinifyEnabled = false
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
}
