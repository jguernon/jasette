#!/bin/bash
# Jasette pour macOS — compile et empaquette, SUR LE MAC (lancé par
# paquets/empaqueter-macos.sh depuis le PC Linux, ou à la main) :
#
#   macos/construire.sh               Apple Silicon (arm64) seulement
#   macos/construire.sh --universel   arm64 + x86_64 (binaires « universels », lipo)
#   macos/construire.sh --sans-signal sans le pont Signal (libsignal est long à compiler)
#
# Le dossier de travail est le parent de la copie des sources :
#   ~/Developer/jasette-macos/
#     src/       les sources (rsync depuis le PC)
#     outils/    rustup + cargo (RUSTUP_HOME, CARGO_HOME), go, protoc, cmake
#     cache/     modules et caches de Go, libsignal et sa compilation
#     target/    compilation Rust (CARGO_TARGET_DIR)
#     sortie/    Jasette.app, le .dmg, version-macos.json
#
# Variables :
#   JASETTE_BUILD_MACOS   le nombre de commits (build.rs ; donné par le PC)
#   JASETTE_IDENTITE      une identité « Developer ID Application: … (ÉQUIPE) » du
#                         trousseau : signature avec runtime renforcé + horodatage.
#                         Sans elle : signature ad hoc (codesign -s -), pour essais.
#   JASETTE_NOTARY        le nom d'un profil notarytool du trousseau (p. ex.
#                         jasette-notary) : le .dmg est alors notarisé et agrafé.
#
# Préparer un Mac (une fois, sans sudo, sans Homebrew ; Xcode installé) :
#   D=~/Developer/jasette-macos; mkdir -p $D/outils && cd $D/outils
#   curl -sSf https://sh.rustup.rs -o rustup-init.sh
#   RUSTUP_HOME=$D/outils/rustup CARGO_HOME=$D/outils/cargo sh rustup-init.sh -y \
#       --no-modify-path --profile minimal -t x86_64-apple-darwin
#   curl -sSL https://go.dev/dl/go1.26.8.darwin-arm64.tar.gz | tar xz
# protoc et cmake (pour libsignal) sont téléchargés par macos/libsignal.sh.
set -euo pipefail
umask 022

ICI="$(cd "$(dirname "$0")" && pwd)"
SRC="$(dirname "$ICI")"
BASE="$(dirname "$SRC")"
OUTILS="$BASE/outils"
C="$BASE/cache"
SORTIE="$BASE/sortie"
TRAVAIL="$SORTIE/travail"

UNIVERSEL=0
SIGNAL=1
for a in "$@"; do
	case "$a" in
		--universel) UNIVERSEL=1 ;;
		--sans-signal) SIGNAL=0 ;;
		*) echo "option inconnue : $a" >&2; exit 2 ;;
	esac
done

export RUSTUP_HOME="$OUTILS/rustup" CARGO_HOME="$OUTILS/cargo" CARGO_TARGET_DIR="$BASE/target"
export PATH="$OUTILS/cargo/bin:$OUTILS/go/bin:$OUTILS/cmake/CMake.app/Contents/bin:$OUTILS/protoc/bin:/usr/bin:/bin:/usr/sbin:/sbin"
export GOMODCACHE="$C/gomod" GOCACHE="$C/gocache" GOPATH="$C/gopath"
export GOTMPDIR="$C/gotmp" TMPDIR="$C/gotmp" GOFLAGS=-modcacherw GOTOOLCHAIN=local
export MACOSX_DEPLOYMENT_TARGET=12.0
# Ménager le Mac (24 Go, 8 cœurs ; il est déjà tombé sous deux compilations
# à 8 tâches) : 4 tâches au plus, une compilation à la fois. JASETTE_JOBS pour changer.
JOBS="${JASETTE_JOBS:-4}"
export CARGO_BUILD_JOBS="$JOBS" GOMAXPROCS="$JOBS" CMAKE_BUILD_PARALLEL_LEVEL="$JOBS" NUM_JOBS="$JOBS"
mkdir -p "$C/gotmp" "$SORTIE" "$CARGO_TARGET_DIR"
# Spotlight n'indexe pas les dossiers de compilation.
touch "$C/.metadata_never_index" "$CARGO_TARGET_DIR/.metadata_never_index" "$OUTILS/.metadata_never_index"

VERSION="$(sed -n 's/^version = "\(.*\)"/\1/p' "$SRC/Cargo.toml" | head -1)"
[[ -n "$VERSION" ]] || { echo "version introuvable dans Cargo.toml" >&2; exit 1; }
BUILD="${JASETTE_BUILD_MACOS:-0}"
echo "== Jasette $VERSION ($BUILD) pour macOS"

# Les architectures : Rust (cible), Go (GOARCH), clang (-arch).
RUST_CIBLES=(aarch64-apple-darwin)
GO_ARCHS=(arm64)
[[ $UNIVERSEL == 1 ]] && RUST_CIBLES+=(x86_64-apple-darwin) && GO_ARCHS+=(amd64)
rustup target add "${RUST_CIBLES[@]}" >/dev/null

rm -rf "$TRAVAIL"
mkdir -p "$TRAVAIL/bin"
BIN="$TRAVAIL/bin"

# Un binaire par architecture → un seul (lipo s'il y en a plusieurs).
fusionner() {
	local sortie="$1"; shift
	if [[ $# == 1 ]]; then cp "$1" "$sortie"; else lipo -create "$@" -output "$sortie"; fi
	chmod 755 "$sortie"
}

# ---- 1. Le bureau (Rust + GPUI) ----
morceaux=()
for t in "${RUST_CIBLES[@]}"; do
	echo "== jasette ($t)"
	(cd "$SRC" && cargo build --release --target "$t")
	morceaux+=("$CARGO_TARGET_DIR/$t/release/jasette")
done
fusionner "$BIN/jasette" "${morceaux[@]}"
strip -x "$BIN/jasette" 2>/dev/null || true

# ---- 2. Les ponts (Go pur) ----
for paire in pont:jasette-pont pont-meta:jasette-meta pont-ig:jasette-ig pont-wa:jasette-wa \
	pont-x:jasette-x pont-telegram:jasette-telegram pont-bluesky:jasette-bluesky pont-discord:jasette-discord \
	pont-reddit:jasette-reddit pont-linkedin:jasette-linkedin; do
	dossier="${paire%%:*}"; nom="${paire##*:}"
	morceaux=()
	for g in "${GO_ARCHS[@]}"; do
		echo "== $nom ($g)"
		(cd "$SRC/$dossier" && CGO_ENABLED=0 GOOS=darwin GOARCH="$g" go build -p "$JOBS" -trimpath -ldflags "-s -w" -o "$BIN/$nom-$g" .)
		morceaux+=("$BIN/$nom-$g")
	done
	fusionner "$BIN/$nom" "${morceaux[@]}"
	rm -f "${morceaux[@]}"
done

# ---- 3. Signal : libsignal-ffi (Rust, statique) puis cgo ----
if [[ $SIGNAL == 1 ]]; then
	if bash "$ICI/libsignal.sh" "${RUST_CIBLES[@]}"; then
		morceaux=()
		for t in "${RUST_CIBLES[@]}"; do
			case "$t" in aarch64-*) g=arm64; a=arm64 ;; *) g=amd64; a=x86_64 ;; esac
			echo "== jasette-signal ($g)"
			(cd "$SRC/pont-signal" && CGO_ENABLED=1 GOOS=darwin GOARCH="$g" CC="clang -arch $a" CXX="clang++ -arch $a" \
				CGO_LDFLAGS="-L$C/libsignal-target/$t/release" \
				go build -p "$JOBS" -trimpath -ldflags "-s -w" -o "$BIN/jasette-signal-$g" .)
			morceaux+=("$BIN/jasette-signal-$g")
		done
		fusionner "$BIN/jasette-signal" "${morceaux[@]}"
		rm -f "${morceaux[@]}"
	else
		echo "!! libsignal n'a pas compilé : Jasette.app sans Signal" >&2
	fi
fi

# ---- 4. Les fenêtres de connexion (Swift, WKWebView) ----
morceaux=()
for t in "${RUST_CIBLES[@]}"; do
	case "$t" in aarch64-*) a=arm64 ;; *) a=x86_64 ;; esac
	echo "== jasette-connexion ($a)"
	xcrun swiftc -O -target "$a-apple-macos12.0" "$ICI/connexion/main.swift" -o "$BIN/jasette-connexion-$a" \
		-module-cache-path "$C/swift-modules"
	morceaux+=("$BIN/jasette-connexion-$a")
done
fusionner "$BIN/jasette-connexion" "${morceaux[@]}"
rm -f "${morceaux[@]}"

# ---- 5. Jasette.app ----
echo "== Jasette.app"
APP="$TRAVAIL/Jasette.app"
mkdir -p "$APP/Contents/MacOS" "$APP/Contents/Resources"
for f in "$BIN"/*; do install -m755 "$f" "$APP/Contents/MacOS/"; done
sed -e "s/@VERSION@/$VERSION/g" -e "s/@BUILD@/$BUILD/g" "$ICI/Info.plist" > "$APP/Contents/Info.plist"
plutil -lint "$APP/Contents/Info.plist" >/dev/null
printf 'APPL????' > "$APP/Contents/PkgInfo"
cp "$SRC/LICENSE" "$APP/Contents/Resources/LICENSE.txt"

# L'icône : un .iconset depuis les PNG de assets/png, puis iconutil.
ICONES="$TRAVAIL/jasette.iconset"
mkdir -p "$ICONES"
P="$SRC/assets/png"
cp "$P/jasette-16.png" "$ICONES/icon_16x16.png"
cp "$P/jasette-32.png" "$ICONES/icon_16x16@2x.png"
cp "$P/jasette-32.png" "$ICONES/icon_32x32.png"
cp "$P/jasette-64.png" "$ICONES/icon_32x32@2x.png"
cp "$P/jasette-128.png" "$ICONES/icon_128x128.png"
cp "$P/jasette-256.png" "$ICONES/icon_128x128@2x.png"
cp "$P/jasette-256.png" "$ICONES/icon_256x256.png"
cp "$P/jasette-512.png" "$ICONES/icon_256x256@2x.png"
cp "$P/jasette-512.png" "$ICONES/icon_512x512.png"
sips -z 1024 1024 "$P/jasette-512.png" --out "$ICONES/icon_512x512@2x.png" >/dev/null
iconutil -c icns "$ICONES" -o "$APP/Contents/Resources/jasette.icns"

# ---- 6. La signature ----
# Chaque exécutable d'abord, puis le paquet (pas de --deep pour la vraie signature).
if [[ -n "${JASETTE_IDENTITE:-}" ]]; then
	echo "== signature : $JASETTE_IDENTITE"
	SIGNE=(codesign --force --timestamp --options runtime --entitlements "$ICI/jasette.entitlements" -s "$JASETTE_IDENTITE")
else
	echo "== signature ad hoc (essais seulement : Gatekeeper refusera l'app téléchargée)"
	SIGNE=(codesign --force --options runtime --entitlements "$ICI/jasette.entitlements" -s -)
fi
for f in "$APP/Contents/MacOS"/*; do
	[[ "$(basename "$f")" == jasette ]] && continue
	"${SIGNE[@]}" "$f"
done
"${SIGNE[@]}" "$APP/Contents/MacOS/jasette"
"${SIGNE[@]}" "$APP"
codesign --verify --strict --verbose=1 "$APP"

# ---- 7. Le .dmg (glisser Jasette dans Applications) ----
ARCH_NOM="arm64"
[[ $UNIVERSEL == 1 ]] && ARCH_NOM="universel"
DMG="$SORTIE/jasette-$VERSION-macos.dmg"
echo "== $(basename "$DMG") ($ARCH_NOM)"
SCENE="$TRAVAIL/dmg"
mkdir -p "$SCENE"
cp -R "$APP" "$SCENE/"
ln -s /Applications "$SCENE/Applications"
rm -f "$DMG"
hdiutil create -quiet -volname "Jasette" -srcfolder "$SCENE" -fs HFS+ -format UDZO -imagekey zlib-level=9 -ov "$DMG"
if [[ -n "${JASETTE_IDENTITE:-}" ]]; then
	codesign --force --timestamp -s "$JASETTE_IDENTITE" "$DMG"
fi
if [[ -n "${JASETTE_NOTARY:-}" ]]; then
	[[ -n "${JASETTE_IDENTITE:-}" ]] || { echo "JASETTE_NOTARY demande JASETTE_IDENTITE (Developer ID)" >&2; exit 1; }
	echo "== notarisation (profil $JASETTE_NOTARY)"
	xcrun notarytool submit "$DMG" --keychain-profile "$JASETTE_NOTARY" --wait
	xcrun stapler staple "$DMG"
	xcrun stapler validate "$DMG"
fi
# Garder aussi l'app hors du .dmg (essais sur ce Mac).
rm -rf "$SORTIE/Jasette.app"
cp -R "$APP" "$SORTIE/"

# ---- 8. version-macos.json (la mise à jour dans l'app, src/maj.rs) ----
# Comme version-linux.json (paquets/empaqueter.sh) : le numéro vient du binaire
# (« jasette 1.0.1 (123) 10123 »), le paquet est le .dmg (clé « dmg »).
LIGNE="$("$APP/Contents/MacOS/jasette" --version)"
CODE="${LIGNE##* }"
NOM="$(sed 's/^jasette //; s/ [0-9]*$//' <<<"$LIGNE")"
[[ "$CODE" =~ ^[0-9]+$ && -n "$NOM" ]] || { echo "jasette --version illisible : $LIGNE" >&2; exit 1; }
/usr/bin/python3 -I - "$SORTIE/version-macos.json" "$CODE" "$NOM" "$SRC/paquets/notes-maj.json" \
	"https://jonathanguernon.net/jasette/telecharger/$(basename "$DMG")?v=$CODE" "$DMG" "$ARCH_NOM" <<'PY'
import hashlib, json, os, sys
sortie, code, nom, notes, url, dmg, arch = sys.argv[1:]
n = {"fr": "", "en": ""}
if os.path.isfile(notes):
    with open(notes, encoding="utf-8") as f:
        lu = json.load(f)
    n = {"fr": str(lu.get("fr", "")).strip(), "en": str(lu.get("en", "")).strip()}
h = hashlib.sha256()
with open(dmg, "rb") as f:
    for bloc in iter(lambda: f.read(1 << 20), b""):
        h.update(bloc)
v = {"versionCode": int(code), "versionName": nom, "notes": n,
     "dmg": {"url": url, "sha256": h.hexdigest(), "taille": os.path.getsize(dmg), "architecture": arch}}
with open(sortie + ".tmp", "w", encoding="utf-8") as f:
    json.dump(v, f, ensure_ascii=False, indent=2)
    f.write("\n")
os.replace(sortie + ".tmp", sortie)
PY
chmod 644 "$SORTIE/version-macos.json"
rm -rf "$TRAVAIL"
echo "== fini"
ls -la "$DMG" "$SORTIE/version-macos.json"
