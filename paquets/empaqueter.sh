#!/usr/bin/env bash
# Jasette — les paquets à télécharger, dans .cache/paquets/ :
#   jasette_<version>_amd64.deb              Linux (Debian, Ubuntu…), tout dans /usr/lib/jasette
#   jasette-<version>-linux-x86_64.tar.gz    Linux, sans installation (ou ./installer.sh dans ~/.local)
#   jasette-<version>.apk                    Android, signé release (si ../signing/ existe)
#   SHA256SUMS
#
#   paquets/empaqueter.sh            tout
#   paquets/empaqueter.sh --sans-apk Linux seulement
#
# Tout ce qui est compilé ou copié reste dans .cache/ (jamais ~/.cache ni /tmp) :
# .cache/paquets-build/ (binaires, arborescences), .cache/paquets/ (le résultat).
# Jasette trouve ses ponts et ses scripts de connexion à côté de son
# exécutable (pont.rs : chemin() ; main.rs : script_connexion_nomme()) : ils
# sont donc tous rangés ensemble, dans /usr/lib/jasette (lien /usr/bin/jasette).
set -euo pipefail
umask 022

ICI="$(cd "$(dirname "$0")" && pwd)"
PROJET="$(dirname "$ICI")"
C="$PROJET/.cache"
TRAVAIL="$C/paquets-build"
BIN="$TRAVAIL/bin"
SORTIE="$C/paquets"
APK=1
[[ "${1:-}" == "--sans-apk" ]] && APK=0

VERSION="$(sed -n 's/^version = "\(.*\)"/\1/p' "$PROJET/Cargo.toml" | head -1)"
[[ -n "$VERSION" ]] || { echo "version introuvable dans Cargo.toml" >&2; exit 1; }
echo "== Jasette $VERSION"

mkdir -p "$BIN" "$SORTIE" "$C/gotmp"
cd "$PROJET"

# ---- 1. Le bureau (Rust), en release, strippé ----
echo "== jasette (cargo build --release)"
PATH="$HOME/.cargo/bin:$PATH" cargo build --release
strip -o "$BIN/jasette" "$C/target/release/jasette"

# ---- 2. Les ponts et le serveur (Go) ----
export GOMODCACHE="$C/gomod" GOCACHE="$C/gocache" GOPATH="$C/gopath"
export GOTMPDIR="$C/gotmp" TMPDIR="$C/gotmp" GOFLAGS=-modcacherw GOTOOLCHAIN=local
for paire in pont:jasette-pont pont-meta:jasette-meta pont-ig:jasette-ig pont-wa:jasette-wa \
	pont-x:jasette-x pont-telegram:jasette-telegram serveur:jasette-serveur; do
	dossier="${paire%%:*}"; nom="${paire##*:}"
	echo "== $nom"
	(cd "$dossier" && go build -trimpath -ldflags "-s -w" -o "$BIN/$nom" .)
done

# Signal : libsignal_ffi.a (Rust) d'abord, par pont-signal/build.sh s'il le faut.
echo "== jasette-signal"
if [[ ! -f "$C/libsignal-target/release/libsignal_ffi.a" ]]; then
	sh pont-signal/build.sh
fi
mkdir -p "$C/staticlibs"
ln -sf "$(g++ -print-file-name=libstdc++.a)" "$C/staticlibs/libstdc++.a"
# Les guillemets simples dans CGO_LDFLAGS : le chemin du projet contient des espaces.
(cd pont-signal && CGO_ENABLED=1 LIBRARY_PATH="$C/libsignal-target/release" \
	CGO_LDFLAGS="'-L$C/staticlibs'" \
	go build -trimpath -ldflags '-s -w -extldflags=-static-libgcc' -o "$BIN/jasette-signal" .)

EXECUTABLES=(jasette jasette-serveur jasette-pont jasette-meta jasette-ig jasette-wa jasette-signal jasette-x jasette-telegram)
SCRIPTS=(jasette-connexion.py jasette-connexion-meta.py jasette-connexion-x.py)

# La plus récente version de la glibc demandée par l'un des binaires.
GLIBC="$(for f in "${EXECUTABLES[@]}"; do objdump -T "$BIN/$f"; done | grep -o 'GLIBC_[0-9.]*' | sed 's/GLIBC_//' | sort -Vu | tail -1)"
echo "== glibc demandée : $GLIBC"

# L'unité systemd du serveur, pour un dossier d'installation donné.
unite() {
	local lib="$1"
	printf '%s\n' \
		"# Jasette — le serveur, comme service systemd de l'utilisateur." \
		"#   systemctl --user enable --now jasette-serveur" \
		"#   loginctl enable-linger \"\$USER\"     # pour qu'il tourne sans session ouverte" \
		"# Jumeler un appareil : jasette-serveur --jumeler" \
		"# Journaux :            journalctl --user -u jasette-serveur -f" \
		""
	sed -n '/^\[Unit\]/,$p' serveur/jasette-serveur.service \
		| sed "s|^ExecStart=.*|ExecStart=$lib/jasette-serveur --port 47800 --bin $lib|" \
		| grep -v '^# Les ponts sont cherchés'
}

# Les icônes : PNG de 16 à 512, et le SVG.
icones() {
	local dest="$1"
	for png in assets/png/jasette-*.png; do
		local t="${png##*-}"; t="${t%.png}"
		install -Dm644 "$png" "$dest/${t}x${t}/apps/jasette.png"
	done
	install -Dm644 assets/jasette.svg "$dest/scalable/apps/jasette.svg"
}

# ---- 3. Le paquet .deb ----
echo "== .deb"
RACINE="$TRAVAIL/deb"
rm -rf "$RACINE"
mkdir -p "$RACINE/DEBIAN" "$RACINE/usr/lib/jasette" "$RACINE/usr/bin" \
	"$RACINE/usr/share/applications" "$RACINE/usr/lib/systemd/user" "$RACINE/usr/share/doc/jasette"
for f in "${EXECUTABLES[@]}"; do install -m755 "$BIN/$f" "$RACINE/usr/lib/jasette/$f"; done
for f in "${SCRIPTS[@]}"; do install -m755 "connexion/$f" "$RACINE/usr/lib/jasette/$f"; done
ln -s ../lib/jasette/jasette "$RACINE/usr/bin/jasette"
ln -s ../lib/jasette/jasette-serveur "$RACINE/usr/bin/jasette-serveur"
install -m644 "$ICI/jasette.desktop" "$RACINE/usr/share/applications/jasette.desktop"
icones "$RACINE/usr/share/icons/hicolor"
unite /usr/lib/jasette > "$RACINE/usr/lib/systemd/user/jasette-serveur.service"
chmod 644 "$RACINE/usr/lib/systemd/user/jasette-serveur.service"
cat > "$RACINE/usr/share/doc/jasette/copyright" <<EOF
Format: https://www.debian.org/doc/packaging-manuals/copyright-format/1.0/
Upstream-Name: Jasette
Upstream-Contact: https://jonathanguernon.net/jasette/
Source: https://github.com/jguernon/jasette

Files: *
Copyright: 2026 Jonathan Guernon
License: AGPL-3.0-or-later
 Le texte complet de la licence : https://www.gnu.org/licenses/agpl-3.0.txt
 (et le fichier LICENSE des sources).
EOF
printf 'jasette (%s) unstable; urgency=medium\n\n  * Version %s.\n\n -- Jonathan Guernon <https://jonathanguernon.net/jasette/>  %s\n' \
	"$VERSION" "$VERSION" "$(date -R)" | gzip -9n > "$RACINE/usr/share/doc/jasette/changelog.gz"
find "$RACINE" -type d -exec chmod 755 {} +
TAILLE="$(du -sk --exclude=DEBIAN "$RACINE" | cut -f1)"
cat > "$RACINE/DEBIAN/control" <<EOF
Package: jasette
Version: $VERSION
Architecture: amd64
Maintainer: Jonathan Guernon <https://jonathanguernon.net/jasette/>
Installed-Size: $TAILLE
Depends: libc6 (>= $GLIBC), libgcc-s1, libxcb1, libxkbcommon0, libxkbcommon-x11-0, libvulkan1, libwayland-client0, python3, python3-gi, gir1.2-webkit-6.0, gir1.2-gtk-4.0
Recommends: mesa-vulkan-drivers | vulkan-icd, libegl1, libwayland-egl1, ffmpeg, pipewire-bin, libnotify-bin, xdg-utils
Section: net
Priority: optional
Homepage: https://jonathanguernon.net/jasette/
Description: toutes tes messageries dans une seule app
 Jasette rassemble Google Messages (SMS/RCS), Messenger, Instagram,
 WhatsApp, Signal, X et Telegram dans une seule fenêtre. Il fonctionne seul, comme
 serveur toujours allumé (jasette-serveur, service systemd de
 l'utilisateur) ou comme client d'un serveur Jasette, par un lien TLS
 épinglé. Aucun service tiers : tout passe par tes appareils.
EOF
DEB="$SORTIE/jasette_${VERSION}_amd64.deb"
rm -f "$DEB"
dpkg-deb --root-owner-group -Zxz --build "$RACINE" "$DEB"

# ---- 4. L'archive .tar.gz ----
echo "== .tar.gz"
NOM="jasette-$VERSION-linux-x86_64"
ARBRE="$TRAVAIL/tar/$NOM"
rm -rf "$TRAVAIL/tar"
mkdir -p "$ARBRE"
for f in "${EXECUTABLES[@]}"; do install -m755 "$BIN/$f" "$ARBRE/$f"; done
for f in "${SCRIPTS[@]}"; do install -m755 "connexion/$f" "$ARBRE/$f"; done
install -m755 "$ICI/installer.sh" "$ARBRE/installer.sh"
install -m644 "$ICI/jasette.desktop" "$ARBRE/jasette.desktop"
icones "$ARBRE/icons"
unite @LIB@ > "$ARBRE/jasette-serveur.service"
install -m644 LICENSE "$ARBRE/LICENSE"
cat > "$ARBRE/LISEZMOI.txt" <<EOF
Jasette $VERSION — Linux x86_64
https://jonathanguernon.net/jasette/

Lancer sans installer :   ./jasette
Installer dans ton compte (~/.local, sans sudo) :   ./installer.sh
Désinstaller :   ./installer.sh --retirer

Garde tous les fichiers ensemble : Jasette cherche ses ponts (jasette-pont,
jasette-meta…) et ses fenêtres de connexion (*.py) à côté de lui.

Il faut : glibc $GLIBC ou plus récente (Ubuntu 26.04, Debian 14…), Vulkan,
et pour les fenêtres de connexion : python3-gi, gir1.2-webkit-6.0 et
gir1.2-gtk-4.0.

Serveur (facultatif), après ./installer.sh :
  systemctl --user enable --now jasette-serveur
  jasette-serveur --jumeler

Code source (AGPL-3.0) : https://github.com/jguernon/jasette
EOF
TGZ="$SORTIE/$NOM.tar.gz"
find "$TRAVAIL/tar" -type d -exec chmod 755 {} +
tar --owner=0 --group=0 --numeric-owner -C "$TRAVAIL/tar" -czf "$TGZ" "$NOM"

# ---- 5. L'APK Android, signé release ----
if [[ $APK == 1 ]]; then
	echo "== APK"
	"$PROJET/android/compiler.sh" --release
	cp "$C/jasette-$VERSION-release.apk" "$SORTIE/jasette-$VERSION.apk"
fi

# ---- 6. Les sommes ----
cd "$SORTIE"
rm -f SHA256SUMS
sha256sum -- *.deb *.tar.gz $(ls ./*.apk 2>/dev/null | sed 's|^\./||') > SHA256SUMS
echo "== $SORTIE"
ls -la "$SORTIE"
cat SHA256SUMS
