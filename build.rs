//! Le numéro de version du bureau, inscrit dans le binaire (src/maj.rs) :
//! le même schéma que l'Android (android/app/build.gradle.kts) :
//!   JASETTE_BUILD = le nombre de commits git (git rev-list --count HEAD), 0 sans git ;
//!   JASETTE_CODE  = 10 000 + JASETTE_BUILD (le « versionCode », qui augmente à chaque commit) ;
//!   JASETTE_NOM   = « 1.0.0 (N) », la version de Cargo.toml et le numéro de build.

use std::path::Path;
use std::process::Command;

fn main() {
    let racine = std::env::var("CARGO_MANIFEST_DIR").unwrap_or_else(|_| ".".into());
    let git = Path::new(&racine).join(".git");
    // Recompter quand HEAD avance (nouveau commit, autre branche).
    println!("cargo:rerun-if-changed=build.rs");
    if git.is_dir() {
        println!("cargo:rerun-if-changed={}", git.join("HEAD").display());
        println!("cargo:rerun-if-changed={}", git.join("packed-refs").display());
        if let Ok(head) = std::fs::read_to_string(git.join("HEAD"))
            && let Some(r) = head.trim().strip_prefix("ref: ")
        {
            println!("cargo:rerun-if-changed={}", git.join(r).display());
        }
    }
    let build: u64 = Command::new("git")
        .args(["-C", &racine, "rev-list", "--count", "HEAD"])
        .output()
        .ok()
        .filter(|o| o.status.success())
        .and_then(|o| String::from_utf8_lossy(&o.stdout).trim().parse().ok())
        .unwrap_or(0);
    // macOS seulement : compilé sur le Mac depuis une copie sans .git
    // (paquets/empaqueter-macos.sh) ; le nombre de commits arrive par
    // JASETTE_BUILD_MACOS. (CARGO_CFG_TARGET_OS : la cible, pas la machine.)
    let build = if std::env::var("CARGO_CFG_TARGET_OS").as_deref() == Ok("macos") {
        println!("cargo:rerun-if-env-changed=JASETTE_BUILD_MACOS");
        std::env::var("JASETTE_BUILD_MACOS").ok().and_then(|v| v.trim().parse().ok()).unwrap_or(build)
    } else {
        build
    };
    let version = std::env::var("CARGO_PKG_VERSION").unwrap_or_default();
    let nom = if build > 0 { format!("{version} ({build})") } else { version };
    println!("cargo:rustc-env=JASETTE_BUILD={build}");
    println!("cargo:rustc-env=JASETTE_CODE={}", 10_000 + build);
    println!("cargo:rustc-env=JASETTE_NOM={nom}");
}
