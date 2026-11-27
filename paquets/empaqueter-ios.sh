#!/usr/bin/env bash
# Jasette — l'app iOS (version limitée, alpha), construite sur le Mac depuis ce PC :
#   .cache/paquets/jasette-<version>-ios.ipa   .ipa NON signé, pour AltStore / SideStore
#                                               (qui le re-signent avec l'Apple ID de l'utilisateur)
#
#   paquets/empaqueter-ios.sh               l'.ipa
#   paquets/empaqueter-ios.sh simulateur    seulement Jasette.app pour le simulateur (essais)
#
# 1. rsync de ios/ vers le Mac (~/Developer/jasette-ios/src) ;
# 2. attente : pas de compilation pendant qu'une autre tourne (cargo, rustc,
#    construire.sh du paquet macOS) — le Mac a déjà planté sous la charge ;
# 3. ssh : ios/construire-ios.sh sous caffeinate -i (xcodebuild -jobs 4) ;
# 4. l'.ipa revient dans .cache/paquets/.
# Variable : JASETTE_MAC, l'hôte ssh (défaut : macminitc). XcodeGen doit être dans
# ~/Developer/jasette-ios/outils/xcodegen (voir ios/README.md).
set -euo pipefail
umask 022

ICI="$(cd "$(dirname "$0")" && pwd)"
PROJET="$(dirname "$ICI")"
DEPOT="$(dirname "$(git -C "$PROJET" rev-parse --path-format=absolute --git-common-dir)")"
SORTIE="${JASETTE_PAQUETS:-$DEPOT/.cache/paquets}"
MAC="${JASETTE_MAC:-macminitc}"
DISTANT="Developer/jasette-ios"
MODE="${1:-ipa}"
mkdir -p "$SORTIE"
cd "$PROJET"

VERSION="$(sed -n 's/^version = "\(.*\)"/\1/p' Cargo.toml | head -1)"
[[ -n "$VERSION" ]] || { echo "version introuvable dans Cargo.toml" >&2; exit 1; }
BUILD="$(git rev-list --count HEAD)"
IPA="jasette-$VERSION-ios.ipa"
echo "== Jasette iOS $VERSION alpha ($BUILD), sur $MAC"

echo "== rsync → $MAC:~/$DISTANT/src"
ssh "$MAC" "mkdir -p ~/$DISTANT/src"
rsync -a --delete --exclude Jasette.xcodeproj --exclude .DS_Store ios/ "$MAC:$DISTANT/src/"

echo "== attente d'une autre compilation sur le Mac, s'il y en a une"
ssh "$MAC" 'n=0; while pgrep -x cargo >/dev/null || pgrep -x rustc >/dev/null || pgrep -f "construire[.]sh" >/dev/null || pgrep -x xcodebuild >/dev/null; do
	[ $n -eq 0 ] && echo "   une compilation tourne : on attend…"; n=$((n+1)); sleep 20; done'

q() { printf '%q' "$1"; }
ENV="JASETTE_VERSION=$(q "$VERSION") JASETTE_BUILD=$(q "$BUILD") JASETTE_IPA=$(q "$IPA")"
echo "== construction ($MODE)"
ssh "$MAC" "cd ~/$DISTANT/src && env $ENV caffeinate -i bash construire-ios.sh $(q "$MODE")"

if [[ "$MODE" == ipa ]]; then
	echo "== rapatriement"
	rsync -a "$MAC:$DISTANT/sortie/$IPA" "$SORTIE/"
	chmod 644 "$SORTIE/$IPA"
	(cd "$SORTIE" && sha256sum "$IPA")
	ls -la "$SORTIE/$IPA"
fi
