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
    let version = std::env::var("CARGO_PKG_VERSION").unwrap_or_default();
    // Windows (la cible) : « alpha » dans le nom tant que la version n'est pas éprouvée.
    let alpha = if std::env::var("CARGO_CFG_TARGET_OS").as_deref() == Ok("windows") { " alpha" } else { "" };
    let nom = if build > 0 { format!("{version}{alpha} ({build})") } else { format!("{version}{alpha}") };
    println!("cargo:rustc-env=JASETTE_BUILD={build}");
    println!("cargo:rustc-env=JASETTE_CODE={}", 10_000 + build);
    println!("cargo:rustc-env=JASETTE_NOM={nom}");
    // Windows seulement (la cible, pas la machine qui compile) : l'icône, le
    // manifeste et la version dans l'exécutable. Sans effet pour Linux.
    if std::env::var("CARGO_CFG_TARGET_OS").as_deref() == Ok("windows") {
        ressources_windows(&racine, &std::env::var("CARGO_PKG_VERSION").unwrap_or_default(), build);
    }
}

/// Compile assets/windows/jasette.rc (llvm-rc, ou $JASETTE_RC) en un .res
/// donné à l'éditeur de liens (lld-link accepte les .res).
fn ressources_windows(racine: &str, version: &str, build: u64) {
    let dossier = Path::new(racine).join("assets/windows");
    for f in ["jasette.rc", "jasette.ico"] {
        println!("cargo:rerun-if-changed={}", dossier.join(f).display());
    }
    println!("cargo:rerun-if-env-changed=JASETTE_RC");
    let sortie = Path::new(&std::env::var("OUT_DIR").unwrap()).join("jasette.res");
    let mut v: Vec<u64> = version.split('.').map(|p| p.parse().unwrap_or(0)).collect();
    v.resize(3, 0);
    let rc = std::env::var("JASETTE_RC").unwrap_or_else(|_| "llvm-rc".into());
    let statut = Command::new(&rc)
        .arg(format!("/DJASETTE_V1={}", v[0]))
        .arg(format!("/DJASETTE_V2={}", v[1]))
        .arg(format!("/DJASETTE_V3={}", v[2]))
        .arg(format!("/DJASETTE_V4={}", build.min(65535)))
        .arg(format!("/DJASETTE_VERSION=\"{version}.{build}\""))
        .arg("/C")
        .arg("65001")
        .arg("/FO")
        .arg(&sortie)
        .arg(dossier.join("jasette.rc"))
        .status();
    match statut {
        Ok(s) if s.success() => println!("cargo:rustc-link-arg-bins={}", sortie.display()),
        r => println!("cargo:warning={rc} : ressources Windows non compilées ({r:?}) : pas d'icône"),
    }
}
