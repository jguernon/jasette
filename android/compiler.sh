#!/usr/bin/env bash
# Jasette Android — compile l'APK et le copie dans ../.cache/.
#   ./compiler.sh             APK debug    → ../.cache/jasette-debug.apk
#   ./compiler.sh --release   APK release signé avec ../signing/jasette-release.properties
#                             → ../.cache/jasette-<version>-release.apk
#   ./compiler.sh --aab       App Bundle release signé (clé d'importation de Google Play)
#                             → ../.cache/paquets/jasette-<version>.aab
#   --installer               l'installe aussi sur l'appareil branché (adb ; APK seulement).
# Gradle et ses caches restent dans ../.cache/gradle (GRADLE_USER_HOME).
set -euo pipefail
ICI="$(cd "$(dirname "$0")" && pwd)"
PROJET="$(dirname "$ICI")"
CACHE="$PROJET/.cache"
export GRADLE_USER_HOME="$CACHE/gradle"

RELEASE=0
AAB=0
INSTALLER=0
for a in "$@"; do
    case "$a" in
        --release) RELEASE=1 ;;
        --aab) RELEASE=1; AAB=1 ;;
        --installer) INSTALLER=1 ;;
        *) echo "option inconnue : $a (--release, --aab, --installer)" >&2; exit 2 ;;
    esac
done

if [[ ! -f "$ICI/local.properties" ]]; then
    echo "sdk.dir=${ANDROID_HOME:-$HOME/Android/Sdk}" > "$ICI/local.properties"
fi
cd "$ICI"

if [[ $RELEASE == 1 ]]; then
    if [[ ! -f "$PROJET/signing/jasette-release.properties" ]]; then
        echo "Clé de signature absente : $PROJET/signing/jasette-release.properties" >&2
        exit 1
    fi
    VERSION="$(sed -n 's/^version = "\(.*\)"/\1/p' "$PROJET/Cargo.toml" | head -1)"
    if [[ $AAB == 1 ]]; then
        ./gradlew --console=plain bundleRelease
        mkdir -p "$CACHE/paquets"
        AAB_SORTIE="$CACHE/paquets/jasette-$VERSION.aab"
        cp app/build/outputs/bundle/release/app-release.aab "$AAB_SORTIE"
        echo "AAB : $AAB_SORTIE"
        exit 0
    fi
    ./gradlew --console=plain assembleRelease
    APK="$CACHE/jasette-$VERSION-release.apk"
    cp app/build/outputs/apk/release/app-release.apk "$APK"
else
    ./gradlew --console=plain assembleDebug
    APK="$CACHE/jasette-debug.apk"
    cp app/build/outputs/apk/debug/app-debug.apk "$APK"
fi
echo "APK : $APK"
if [[ $INSTALLER == 1 ]]; then
    adb install -r "$APK"
fi
