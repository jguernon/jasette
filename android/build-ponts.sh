#!/usr/bin/env bash
# Jasette Android — compile les ponts Go pour android/arm64.
#
# Les ponts (../pont, ../pont-meta, ../pont-wa, ../pont-x, ../pont-telegram, ../pont-bluesky,
# ../pont-discord, ../pont-reddit
# et ../pont-signal)
# ne sont pas modifiés : on les compile
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
#
# Le pont Signal se lie à libsignal_ffi.a (Rust), compilée à part et une fois
# par build-libsignal.sh (c'est long). Si elle manque, le pont Signal n'est
# pas embarqué (on retire une vieille copie) : Signal reste absent de l'app,
# sans erreur.
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

export GOMODCACHE="$CACHE/gomod" GOCACHE="$CACHE/gocache" GOPATH="$CACHE/gopath" GOTMPDIR="$CACHE/gotmp" TMPDIR="$CACHE/gotmp"
mkdir -p "$CACHE/gotmp"
export GOOS=android GOARCH=arm64 CGO_ENABLED=1 CC
SORTIE="$CACHE/android-jni/arm64-v8a"
mkdir -p "$SORTIE"

construire() { # <dossier du pont> <nom de la .so>
    echo "→ $1 → $2"
    (cd "$PROJET/$1" && go build -trimpath -ldflags="-s -w" -o "$SORTIE/$2" .)
}
construire pont libjasette-pont.so
construire pont-meta libjasette-meta.so
construire pont-ig libjasette-ig.so  # Instagram : son nouveau protocole (instameow), comme au bureau
construire pont-wa libjasette-wa.so
construire pont-x libjasette-x.so
construire pont-telegram libjasette-telegram.so
construire pont-bluesky libjasette-bluesky.so
construire pont-discord libjasette-discord.so
construire pont-reddit libjasette-reddit.so

LIBSIGNAL="$CACHE/libsignal-target/aarch64-linux-android/release"
if [[ -f "$LIBSIGNAL/libsignal_ffi.a" ]]; then
    # libsignalgo demande -lstdc++, que le clang du NDK change en -lc++, donc
    # libc++_shared.so : absente du téléphone. Un libc++.so à nous (un script
    # d'éditeur de liens), cherché en premier, la remplace par libc++ statique.
    STL="$CACHE/android-stl"
    mkdir -p "$STL"
    echo 'INPUT(-lc++_static -lc++abi)' > "$STL/libc++.so"
    # Entre apostrophes : le chemin du projet contient des espaces.
    CGO_LDFLAGS="'-L$STL' '-L$LIBSIGNAL'" \
        construire pont-signal libjasette-signal.so
else
    echo "→ pont-signal : libsignal_ffi.a absente (lance build-libsignal.sh), Signal non embarqué"
    rm -f "$SORTIE/libjasette-signal.so"
fi
ls -lh "$SORTIE"
