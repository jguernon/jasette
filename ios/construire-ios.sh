#!/usr/bin/env bash
# Jasette iOS — construit sur le Mac (lancé par paquets/empaqueter-ios.sh) :
#   construire-ios.sh ipa          .ipa non signé → ~/Developer/jasette-ios/sortie/
#   construire-ios.sh simulateur   Jasette.app (Debug) pour le simulateur iOS
# Variables : JASETTE_VERSION (1.0.1), JASETTE_BUILD (nombre de commits),
#             JASETTE_IPA (nom du fichier .ipa).
# Tout reste dans ~/Developer/jasette-ios (XcodeGen dans outils/, DerivedData
# dans derive/). Compilation ménagée : -jobs 4.
set -euo pipefail
ICI="$(cd "$(dirname "$0")" && pwd)"
RACINE="$HOME/Developer/jasette-ios"
XG="$RACINE/outils/xcodegen/bin/xcodegen"
VERSION="${JASETTE_VERSION:?version}"
BUILD="${JASETTE_BUILD:?build}"
MODE="${1:-ipa}"
DERIVE="$RACINE/derive"
SORTIE="$RACINE/sortie"
mkdir -p "$SORTIE"

[[ -x "$XG" ]] || { echo "XcodeGen absent : $XG (voir ios/README.md)" >&2; exit 1; }
cd "$ICI"
"$XG" generate --spec project.yml --quiet

COMMUN=(-project Jasette.xcodeproj -scheme Jasette -derivedDataPath "$DERIVE" -jobs 4
	MARKETING_VERSION="$VERSION" CURRENT_PROJECT_VERSION="$BUILD")

case "$MODE" in
ipa)
	ARCHIVE="$SORTIE/Jasette.xcarchive"
	rm -rf "$ARCHIVE"
	xcodebuild "${COMMUN[@]}" -configuration Release -destination 'generic/platform=iOS' \
		-archivePath "$ARCHIVE" archive \
		CODE_SIGNING_ALLOWED=NO CODE_SIGNING_REQUIRED=NO CODE_SIGN_IDENTITY="" -quiet
	APP="$ARCHIVE/Products/Applications/Jasette.app"
	[[ -d "$APP" ]] || { echo "Jasette.app absent de l'archive" >&2; exit 1; }
	T="$SORTIE/ipa"
	rm -rf "$T"
	mkdir -p "$T/Payload"
	cp -R "$APP" "$T/Payload/"
	IPA="${JASETTE_IPA:-Jasette.ipa}"
	rm -f "$SORTIE/$IPA"
	(cd "$T" && zip -qry "$SORTIE/$IPA" Payload)
	/usr/libexec/PlistBuddy -c 'Print :CFBundleShortVersionString' -c 'Print :CFBundleVersion' "$APP/Info.plist" 2>/dev/null || true
	rm -rf "$T" "$ARCHIVE"
	ls -la "$SORTIE/$IPA"
	;;
simulateur)
	xcodebuild "${COMMUN[@]}" -configuration Debug -destination 'generic/platform=iOS Simulator' build -quiet
	ls -d "$DERIVE/Build/Products/Debug-iphonesimulator/Jasette.app"
	;;
*)
	echo "usage : construire-ios.sh ipa|simulateur" >&2
	exit 2
	;;
esac
