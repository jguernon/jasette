#!/usr/bin/env bash
# Jasette Android — compile l'APK debug et le copie dans ../.cache/jasette-debug.apk.
# Gradle et ses caches restent dans ../.cache/gradle (GRADLE_USER_HOME).
# Avec « --installer », l'installe aussi sur l'appareil branché (adb).
set -euo pipefail
ICI="$(cd "$(dirname "$0")" && pwd)"
CACHE="$(dirname "$ICI")/.cache"
export GRADLE_USER_HOME="$CACHE/gradle"
if [[ ! -f "$ICI/local.properties" ]]; then
    echo "sdk.dir=${ANDROID_HOME:-$HOME/Android/Sdk}" > "$ICI/local.properties"
fi
cd "$ICI"
./gradlew --console=plain assembleDebug
cp app/build/outputs/apk/debug/app-debug.apk "$CACHE/jasette-debug.apk"
echo "APK : $CACHE/jasette-debug.apk"
if [[ "${1:-}" == "--installer" ]]; then
    adb install -r "$CACHE/jasette-debug.apk"
fi
