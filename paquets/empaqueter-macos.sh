#!/usr/bin/env bash
# Jasette — le paquet macOS, construit sur le Mac depuis ce PC Linux :
#   .cache/paquets/jasette-<version>-macos.dmg   Jasette.app à glisser dans Applications
#   .cache/paquets/version-macos.json            la mise à jour dans l'app (src/maj.rs) :
#                                                versionCode, versionName, notes, et le
#                                                .dmg (adresse, SHA-256, taille)
#
#   paquets/empaqueter-macos.sh                 Apple Silicon (arm64)
#   paquets/empaqueter-macos.sh --universel     arm64 + x86_64 (Mac Intel aussi)
#   paquets/empaqueter-macos.sh --sans-signal   sans le pont Signal
#
# 1. rsync des sources vers le Mac (~/Developer/jasette-macos/src, sans .git,
#    .cache, android ni signing) ;
# 2. ssh : macos/construire.sh (cargo, go, swiftc, iconutil, codesign, hdiutil) ;
# 3. le .dmg et version-macos.json reviennent dans .cache/paquets/.
#
# Variables (transmises au Mac) :
#   JASETTE_MAC        l'hôte ssh (défaut : macminitc)
#   JASETTE_IDENTITE   « Developer ID Application: Nom (ÉQUIPE) » : vraie signature
#                      (sinon ad hoc, pour essais seulement)
#   JASETTE_NOTARY     profil notarytool du trousseau du Mac (p. ex. jasette-notary) :
#                      notarisation et agrafage du .dmg
# Le Mac doit avoir Xcode ; Rust (rustup) et Go sont dans ~/Developer/jasette-macos/outils
# (installation sans sudo : voir l’en-tête de macos/construire.sh).
set -euo pipefail
umask 022

ICI="$(cd "$(dirname "$0")" && pwd)"
PROJET="$(dirname "$ICI")"
C="$PROJET/.cache"
# Le .cache du dépôt principal, même lancé depuis un worktree (.cache/wt-macos).
DEPOT="$(dirname "$(git -C "$PROJET" rev-parse --path-format=absolute --git-common-dir)")"
SORTIE="${JASETTE_PAQUETS:-$DEPOT/.cache/paquets}"
MAC="${JASETTE_MAC:-macminitc}"
DISTANT="Developer/jasette-macos"
mkdir -p "$SORTIE"
cd "$PROJET"

VERSION="$(sed -n 's/^version = "\(.*\)"/\1/p' Cargo.toml | head -1)"
[[ -n "$VERSION" ]] || { echo "version introuvable dans Cargo.toml" >&2; exit 1; }
# Le numéro de build (build.rs) : le nombre de commits, compté ici (pas de .git sur le Mac).
BUILD="$(git rev-list --count HEAD)"
echo "== Jasette $VERSION ($BUILD) pour macOS, sur $MAC"

echo "== rsync → $MAC:~/$DISTANT/src"
ssh "$MAC" "mkdir -p ~/$DISTANT/src"
rsync -a --delete \
	--exclude .git --exclude .cache --exclude target --exclude android --exclude signing \
	--exclude __pycache__ --exclude '*.tmp' --exclude session.json \
	./ "$MAC:$DISTANT/src/"

q() { printf '%q' "$1"; }
ENV="JASETTE_BUILD_MACOS=$(q "$BUILD")"
[[ -n "${JASETTE_IDENTITE:-}" ]] && ENV+=" JASETTE_IDENTITE=$(q "$JASETTE_IDENTITE")"
[[ -n "${JASETTE_NOTARY:-}" ]] && ENV+=" JASETTE_NOTARY=$(q "$JASETTE_NOTARY")"
ARGS=""
for a in "$@"; do ARGS+=" $(q "$a")"; done

echo "== construction sur le Mac"
# caffeinate -i : le Mac ne s'endort pas pendant la compilation.
ssh "$MAC" "cd ~/$DISTANT/src && env $ENV caffeinate -i bash macos/construire.sh$ARGS"

echo "== rapatriement"
DMG="jasette-$VERSION-macos.dmg"
rsync -a "$MAC:$DISTANT/sortie/$DMG" "$MAC:$DISTANT/sortie/version-macos.json" "$SORTIE/"
chmod 644 "$SORTIE/$DMG" "$SORTIE/version-macos.json"
(cd "$SORTIE" && sha256sum "$DMG")
ls -la "$SORTIE/$DMG" "$SORTIE/version-macos.json"
