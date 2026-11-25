#!/usr/bin/env bash
# Jasette Android — compile libsignal-ffi (Rust) pour aarch64-linux-android.
#
# Le pont Signal (../pont-signal) a besoin de libsignal_ffi.a pour se lier.
# Ce script la compile avec le NDK, une fois : la compilation est longue, et
# build-ponts.sh (lancé par Gradle) ne fait que vérifier si elle existe. Sans
# elle, le pont Signal n'est pas embarqué et Signal reste absent de l'app.
#
# Les sources de libsignal (.cache/libsignal, la version demandée par
# signalmeow) et protoc (.cache/protoc) viennent de ../pont-signal/build.sh.
# Tout reste dans ../.cache :
#   .cache/libsignal-target/aarch64-linux-android/release/libsignal_ffi.a
#   .cache/cargo-home (registre de cargo)
set -euo pipefail

ICI="$(cd "$(dirname "$0")" && pwd)"
PROJET="$(dirname "$ICI")"
C="$PROJET/.cache"

SDK="${ANDROID_HOME:-${ANDROID_SDK_ROOT:-}}"
if [[ -z "$SDK" && -f "$ICI/local.properties" ]]; then
    SDK="$(sed -n 's/^sdk\.dir=//p' "$ICI/local.properties")"
fi
[[ -n "$SDK" ]] || SDK="$HOME/Android/Sdk"
NDK="${ANDROID_NDK_HOME:-$(ls -d "$SDK"/ndk/* 2>/dev/null | sort -V | tail -1)}"
API=26
BIN="$NDK/toolchains/llvm/prebuilt/linux-x86_64/bin"
[[ -x "$BIN/aarch64-linux-android${API}-clang" ]] || { echo "clang du NDK introuvable dans $BIN" >&2; exit 1; }
[[ -d "$C/libsignal" ]] || { echo "Sources de libsignal absentes : lance d'abord ../pont-signal/build.sh" >&2; exit 1; }
[[ -x "$C/protoc/bin/protoc" ]] || { echo "protoc absent : lance d'abord ../pont-signal/build.sh" >&2; exit 1; }

mkdir -p "$C/gotmp"
# bindgen (boring-sys) refuse la cible Android sans numéro d'API : on la lui
# donne avec BINDGEN_EXTRA_CLANG_ARGS.
cd "$C/libsignal"
# RUSTUP_TOOLCHAIN=stable : la chaîne déjà installée (avec la cible
# aarch64-linux-android), pas celle épinglée par libsignal.
env \
    PATH="$C/protoc/bin:$HOME/.cargo/bin:$PATH" \
    PROTOC="$C/protoc/bin/protoc" \
    CARGO_HOME="$C/cargo-home" \
    CARGO_TARGET_DIR="$C/libsignal-target" \
    TMPDIR="$C/gotmp" \
    RUSTUP_TOOLCHAIN=stable \
    RUSTC_WRAPPER="" \
    ANDROID_NDK_HOME="$NDK" ANDROID_NDK_ROOT="$NDK" ANDROID_NDK="$NDK" \
    CC_aarch64_linux_android="$BIN/aarch64-linux-android${API}-clang" \
    CXX_aarch64_linux_android="$BIN/aarch64-linux-android${API}-clang++" \
    AR_aarch64_linux_android="$BIN/llvm-ar" \
    RANLIB_aarch64_linux_android="$BIN/llvm-ranlib" \
    BINDGEN_EXTRA_CLANG_ARGS_aarch64_linux_android="--target=aarch64-linux-android${API} --sysroot=$BIN/../sysroot" \
    CARGO_TARGET_AARCH64_LINUX_ANDROID_LINKER="$BIN/aarch64-linux-android${API}-clang" \
    cargo build -p libsignal-ffi --profile=release --target aarch64-linux-android

ls -lh "$C/libsignal-target/aarch64-linux-android/release/libsignal_ffi.a"
