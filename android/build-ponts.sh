#!/usr/bin/env bash
# Jasette Android — compile les deux ponts Go pour android/arm64.
#
# Les ponts (../pont et ../pont-meta) ne sont pas modifiés : on les compile
# tels quels en exécutables, nommés lib*.so et rangés dans
# ../.cache/android-jni/arm64-v8a/ (le dossier jniLibs de l'app, voir
# app/build.gradle.kts) pour qu'Android les installe dans nativeLibraryDir
# (le seul endroit d'où une app a le droit de lancer un exécutable).
# Gradle lance ce script avant chaque compilation (tâche « ponts »).
#
# CGO est activé avec le clang du NDK : sans cgo, le résolveur DNS de Go
# cherche /etc/resolv.conf, qui n'existe pas sur Android (golang/go#10714).
# Avec cgo, Go passe par getaddrinfo de bionic. SQLite reste en pur Go.
#
# Tous les caches Go restent dans ../.cache (gomod, gocache, gopath).
set -euo pipefail

ICI="$(cd "$(dirname "$0")" && pwd)"
PROJET="$(dirname "$ICI")"
CACHE="$PROJET/.cache"

SDK="${ANDROID_HOME:-${ANDROID_SDK_ROOT:-}}"
if [[ -z "$SDK" ]]; then
    if [[ -f "$ICI/local.properties" ]]; then
        SDK="$(sed -n 's/^sdk\.dir=//p' "$ICI/local.properties")"
    fi
fi
[[ -n "$SDK" ]] || SDK="$HOME/Android/Sdk"
NDK="${ANDROID_NDK_HOME:-$(ls -d "$SDK"/ndk/* 2>/dev/null | sort -V | tail -1)}"
API=26
CC="$NDK/toolchains/llvm/prebuilt/linux-x86_64/bin/aarch64-linux-android${API}-clang"
[[ -x "$CC" ]] || { echo "clang du NDK introuvable : $CC" >&2; exit 1; }

export GOMODCACHE="$CACHE/gomod" GOCACHE="$CACHE/gocache" GOPATH="$CACHE/gopath"
export GOOS=android GOARCH=arm64 CGO_ENABLED=1 CC
SORTIE="$CACHE/android-jni/arm64-v8a"
mkdir -p "$SORTIE"

construire() { # <dossier du pont> <nom de la .so>
    echo "→ $1 → $2"
    (cd "$PROJET/$1" && go build -trimpath -ldflags="-s -w" -o "$SORTIE/$2" .)
}
construire pont libjasette-pont.so
construire pont-meta libjasette-meta.so
ls -lh "$SORTIE"
