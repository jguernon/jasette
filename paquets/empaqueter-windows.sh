#!/usr/bin/env bash
# Jasette — les paquets Windows (x86_64), compilés depuis Linux, dans .cache/paquets/ :
#   jasette-<version>-windows-x86_64.zip         portable : tout dans un dossier, à décompresser
#   jasette-<version>-windows-x86_64-setup.exe   installateur (Inno Setup), dans le compte de
#                                                l'utilisateur, sans droits d'administrateur
#   version-windows.json                         la mise à jour dans l'app (src/maj_windows.rs) :
#                                                versionCode, versionName, notes, et le zip :
#                                                adresse, SHA-256, taille
#   SHA256SUMS-windows                           les sommes de ces deux paquets (SHA256SUMS,
#                                                celui de empaqueter.sh, n'est pas touché)
#
#   paquets/empaqueter-windows.sh               tout
#   paquets/empaqueter-windows.sh --sans-setup  sans l'installateur
#
# Les outils (sans sudo) :
#   - cargo-xwin (cargo install cargo-xwin) : la cible x86_64-pc-windows-msvc avec le CRT et
#     le SDK de Windows, téléchargés une fois dans .cache/xwin (licence Microsoft acceptée
#     par XWIN_ACCEPT_LICENSE=1) ; clang-cl, llvm-lib et llvm-rc de LLVM (/usr/lib/llvm-*) ;
#     lld-link : rust-lld de rustup (un lien dans .cache/xwin-bin) ;
#   - Go (GOOS=windows, CGO_ENABLED=0) pour les ponts ;
#   - wine : fxc.exe (les shaders de GPUI, voir paquets/windows/shaders-gpui.sh) et
#     ISCC.exe d'Inno Setup (téléchargé une fois, installé dans le préfixe .cache/wine).
#
# Pas de Signal sous Windows (libsignal en compilation croisée : pas encore), ni de
# serveur Jasette (jasette-serveur et systemd restent sous Linux ; le mode client marche).
#
# Tout ce qui est compilé ou téléchargé reste dans .cache/ (jamais ~/.cache ni /tmp) :
# .cache/target-windows (Rust), .cache/paquets-build/windows (binaires, arborescence),
# .cache/paquets (le résultat). Depuis un worktree git rangé dans .cache, c'est le
# .cache du dépôt principal.
set -euo pipefail
umask 022

ICI="$(cd "$(dirname "$0")" && pwd)"
PROJET="$(dirname "$ICI")"
C="$(dirname "$(git -C "$PROJET" rev-parse --path-format=absolute --git-common-dir)")/.cache"
export JASETTE_CACHE="$C"
TRAVAIL="$C/paquets-build/windows"
BIN="$TRAVAIL/bin"
SORTIE="${JASETTE_PAQUETS:-$C/paquets}"
SETUP=1
[[ "${1:-}" == "--sans-setup" ]] && SETUP=0
CIBLE_RUST=x86_64-pc-windows-msvc

VERSION="$(sed -n 's/^version = "\(.*\)"/\1/p' "$PROJET/Cargo.toml" | head -1)"
[[ -n "$VERSION" ]] || { echo "version introuvable dans Cargo.toml" >&2; exit 1; }
# Le même numéro que build.rs (10 000 + le nombre de commits).
NUMERO="$(git -C "$PROJET" rev-list --count HEAD)"
CODE=$((10000 + NUMERO))
NOM_VERSION="$VERSION alpha ($NUMERO)"
echo "== Jasette $NOM_VERSION — Windows x86_64"

mkdir -p "$BIN" "$SORTIE" "$C/gotmp" "$C/xwin-bin"
cd "$PROJET"

# ---- 0. L'outillage ----
LLVM="$(ls -d /usr/lib/llvm-*/bin 2>/dev/null | sort -V | tail -1)"
RUST_LLD="$(ls "$HOME"/.rustup/toolchains/stable-*/lib/rustlib/*/bin/rust-lld 2>/dev/null | head -1)"
[[ -x "$C/xwin-bin/lld-link" || -z "$RUST_LLD" ]] || ln -sf "$RUST_LLD" "$C/xwin-bin/lld-link"
export PATH="$C/xwin-bin:$HOME/.cargo/bin:${LLVM:+$LLVM:}$PATH"
export XWIN_CACHE_DIR="$C/xwin" XWIN_ACCEPT_LICENSE=1
export CARGO_TARGET_DIR="$C/target-windows"
# Le CRT de Visual C++ lié dans l'exécutable : rien à installer à côté (VCRUNTIME140.dll).
export RUSTFLAGS="-C target-feature=+crt-static"
export WINEPREFIX="$C/wine" WINEDEBUG="${WINEDEBUG:-fixme-all}"
for outil in cargo-xwin clang-cl llvm-lib llvm-rc lld-link go wine zip; do
	command -v "$outil" >/dev/null || { echo "outil manquant : $outil" >&2; exit 1; }
done

# ---- 1. Le bureau (Rust) ----
# gpui_windows ne précompile ses shaders que sur une machine Windows : on les
# fabrique (fxc.exe sous wine), puis on les pose dans son OUT_DIR et on relance.
GPUI_V="$(awk '/^name = "gpui-pre-windows"$/{getline; gsub(/version = |"/,""); print; exit}' Cargo.lock)"
GPUI_SRC="$(ls -d "${CARGO_HOME:-$HOME/.cargo}"/registry/src/*/gpui-pre-windows-"$GPUI_V"/src 2>/dev/null | head -1 || true)"
if [[ -z "$GPUI_SRC" ]]; then
	cargo fetch --target "$CIBLE_RUST" >/dev/null
	GPUI_SRC="$(ls -d "${CARGO_HOME:-$HOME/.cargo}"/registry/src/*/gpui-pre-windows-"$GPUI_V"/src | head -1)"
fi
SHADERS="$C/gpui-shaders/$GPUI_V/shaders_bytes.rs"
[[ -s "$SHADERS" ]] || "$ICI/windows/shaders-gpui.sh" "$GPUI_SRC" "$SHADERS"

echo "== jasette.exe (cargo xwin build --release)"
compiler_bureau() {
	local r=0
	cargo xwin build --release --target "$CIBLE_RUST" > "$TRAVAIL/cargo.log" 2>&1 || r=$?
	grep -E '^error' -A6 "$TRAVAIL/cargo.log" | grep -v shaders_bytes || true
	return $r
}
if ! compiler_bureau; then
	if grep -q 'shaders_bytes.rs' "$TRAVAIL/cargo.log"; then
		for d in "$CARGO_TARGET_DIR/$CIBLE_RUST"/release/build/gpui-pre-windows-*/out; do cp "$SHADERS" "$d/"; done
		compiler_bureau || { tail -30 "$TRAVAIL/cargo.log" >&2; exit 1; }
	else
		tail -30 "$TRAVAIL/cargo.log" >&2; exit 1
	fi
fi
cp "$CARGO_TARGET_DIR/$CIBLE_RUST/release/jasette.exe" "$BIN/jasette.exe"

# ---- 2. La fenêtre de connexion (WebView2), programme à part ----
echo "== jasette-connexion.exe"
(cd connexion/windows && CARGO_TARGET_DIR="$C/target-connexion-windows" \
	cargo xwin build --release --target "$CIBLE_RUST" 2>&1 | grep -E '^error' -A5 || true)
cp "$C/target-connexion-windows/$CIBLE_RUST/release/jasette-connexion.exe" "$BIN/"

# ---- 3. Les ponts (Go) ----
export GOMODCACHE="$C/gomod" GOCACHE="$C/gocache" GOPATH="$C/gopath"
export GOTMPDIR="$C/gotmp" TMPDIR="$C/gotmp" GOFLAGS=-modcacherw GOTOOLCHAIN=local
for paire in pont:jasette-pont pont-meta:jasette-meta pont-ig:jasette-ig pont-wa:jasette-wa \
	pont-x:jasette-x pont-telegram:jasette-telegram pont-bluesky:jasette-bluesky pont-discord:jasette-discord \
	pont-reddit:jasette-reddit pont-linkedin:jasette-linkedin; do
	dossier="${paire%%:*}"; nom="${paire##*:}"
	echo "== $nom.exe"
	(cd "$dossier" && GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o "$BIN/$nom.exe" .)
done
EXECUTABLES=(jasette jasette-connexion jasette-pont jasette-meta jasette-ig jasette-wa jasette-x jasette-telegram \
	jasette-bluesky jasette-discord jasette-reddit jasette-linkedin)

# ---- 4. Le zip portable ----
echo "== .zip"
NOM="jasette-$VERSION-windows-x86_64"
ARBRE="$TRAVAIL/arbre/$NOM"
rm -rf "$TRAVAIL/arbre"
mkdir -p "$ARBRE"
for f in "${EXECUTABLES[@]}"; do install -m644 "$BIN/$f.exe" "$ARBRE/$f.exe"; done
install -m644 LICENSE "$ARBRE/LICENSE.txt"
sed 's/$/\r/' > "$ARBRE/LISEZMOI.txt" <<EOF
Jasette $NOM_VERSION — Windows 10 et 11 (x86_64)
https://jonathanguernon.net/jasette/

Lancer : jasette.exe (rien à installer). Garde tous les fichiers ensemble :
Jasette cherche ses ponts (jasette-pont.exe, jasette-meta.exe…) et sa fenêtre de
connexion (jasette-connexion.exe) à côté d'elle.

Tes données : %LOCALAPPDATA%\\jasette (journal : jasette.log).

Il faut : Windows 10 (1809) ou 11, 64 bits, DirectX 11. Pour Messenger,
Instagram, X, Reddit, LinkedIn et « Se connecter avec Google » : le runtime
Microsoft Edge WebView2 (déjà dans Windows 11 et les Windows 10 à jour).

Pas encore sous Windows : Signal, le mode « Je suis le serveur Jasette »
(installe le serveur sur une machine Linux et branche-toi comme client),
les messages vocaux (enregistrement) et « Ce que j'écoute ».

Code source (AGPL-3.0) : https://github.com/jguernon/jasette
EOF
ZIP="$SORTIE/$NOM.zip"
rm -f "$ZIP"
# Sans attributs Unix (-X), dossiers inclus : maj_windows.rs lit exactement ce format.
(cd "$TRAVAIL/arbre" && TZ=UTC zip -X -9 -q -r "$ZIP" "$NOM")

# ---- 5. L'installateur (Inno Setup sous wine) ----
SETUP_EXE=""
if [[ $SETUP == 1 ]]; then
	echo "== installateur (Inno Setup)"
	ISCC="$WINEPREFIX/drive_c/InnoSetup/ISCC.exe"
	if [[ ! -f "$ISCC" ]]; then
		IS_V=6.7.4
		mkdir -p "$C/telechargements/inno"
		INNO="$C/telechargements/inno/innosetup-$IS_V.exe"
		[[ -f "$INNO" ]] || curl -fsSL -o "$INNO" "https://github.com/jrsoftware/issrc/releases/download/is-${IS_V//./_}/innosetup-$IS_V.exe"
		wine "$INNO" /VERYSILENT /SUPPRESSMSGBOXES /CURRENTUSER /NOICONS '/DIR=C:\InnoSetup' >/dev/null 2>&1
	fi
	(cd "$ICI/windows" && wine "$ISCC" /Q "/DVersion=$VERSION" "/DNumero=$NUMERO" \
		"/DArbre=$(winepath -w "$ARBRE")" "/DSortie=$(winepath -w "$SORTIE")" jasette.iss 2>&1 \
		| grep -v -i 'fixme\|radv' || true)
	SETUP_EXE="$SORTIE/$NOM-setup.exe"
	[[ -f "$SETUP_EXE" ]] || { echo "l'installateur n'a pas été produit" >&2; exit 1; }
fi

# ---- 6. version-windows.json : la mise à jour dans l'app ----
BASE_URL="https://jonathanguernon.net/jasette/telecharger"
python3 -I - "$SORTIE/version-windows.json" "$CODE" "$NOM_VERSION" "$ICI/notes-maj.json" \
	"$BASE_URL/$(basename "$ZIP")?v=$CODE" "$ZIP" \
	"${SETUP_EXE:+$BASE_URL/$(basename "$SETUP_EXE")?v=$CODE}" "$SETUP_EXE" <<'PY'
import hashlib, json, os, sys
sortie, code, nom, notes, url_zip, zip_, url_setup, setup = sys.argv[1:]
n = {"fr": "", "en": ""}
if os.path.isfile(notes):
    with open(notes, encoding="utf-8") as f:
        lu = json.load(f)
    n = {"fr": str(lu.get("fr", "")).strip(), "en": str(lu.get("en", "")).strip()}
def paquet(url, chemin):
    h = hashlib.sha256()
    with open(chemin, "rb") as f:
        for bloc in iter(lambda: f.read(1 << 20), b""):
            h.update(bloc)
    return {"url": url, "sha256": h.hexdigest(), "taille": os.path.getsize(chemin)}
v = {"versionCode": int(code), "versionName": nom, "notes": n, "zip": paquet(url_zip, zip_)}
if setup:
    v["setup"] = paquet(url_setup, setup)
with open(sortie + ".tmp", "w", encoding="utf-8") as f:
    json.dump(v, f, ensure_ascii=False, indent=2)
    f.write("\n")
os.replace(sortie + ".tmp", sortie)
PY
chmod 644 "$SORTIE/version-windows.json" "$ZIP" ${SETUP_EXE:+"$SETUP_EXE"}
echo "== version-windows.json"
cat "$SORTIE/version-windows.json"

# ---- 7. Les sommes des paquets Windows ----
cd "$SORTIE"
sha256sum -- "$(basename "$ZIP")" ${SETUP_EXE:+"$(basename "$SETUP_EXE")"} > SHA256SUMS-windows
chmod 644 SHA256SUMS-windows
echo "== $SORTIE"
ls -la "$ZIP" ${SETUP_EXE:+"$SETUP_EXE"} version-windows.json SHA256SUMS-windows
cat SHA256SUMS-windows
