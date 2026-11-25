#!/bin/sh
# Jasette — installation dans ton compte, sans sudo (archive .tar.gz).
#   ./installer.sh              installe dans ~/.local (lib/jasette, bin, applications, icônes)
#   ./installer.sh --retirer    désinstalle (tes données dans ~/.local/share/jasette restent)
# Pour tout le système, préfère le paquet .deb.
set -eu
ICI=$(cd "$(dirname "$0")" && pwd)
PREFIXE="${PREFIXE:-$HOME/.local}"
LIB="$PREFIXE/lib/jasette"
UNITE="${XDG_CONFIG_HOME:-$HOME/.config}/systemd/user/jasette-serveur.service"

if [ "${1:-}" = "--retirer" ]; then
	systemctl --user disable --now jasette-serveur 2>/dev/null || true
	rm -rf "$LIB"
	rm -f "$PREFIXE/bin/jasette" "$PREFIXE/bin/jasette-serveur" \
		"$PREFIXE/share/applications/jasette.desktop" "$UNITE"
	find "$PREFIXE/share/icons/hicolor" -name 'jasette.*' -delete 2>/dev/null || true
	echo "Jasette est retiré. Tes données (~/.local/share/jasette) sont gardées."
	exit 0
fi

mkdir -p "$LIB" "$PREFIXE/bin" "$PREFIXE/share/applications"
for f in jasette jasette-serveur jasette-pont jasette-meta jasette-ig jasette-wa jasette-signal jasette-x \
	jasette-connexion.py jasette-connexion-meta.py jasette-connexion-x.py; do
	install -m 755 "$ICI/$f" "$LIB/$f"
done
ln -sf "$LIB/jasette" "$PREFIXE/bin/jasette"
ln -sf "$LIB/jasette-serveur" "$PREFIXE/bin/jasette-serveur"
sed "s|^Exec=.*|Exec=$LIB/jasette|" "$ICI/jasette.desktop" > "$PREFIXE/share/applications/jasette.desktop"
(cd "$ICI/icons" && find . -type f) | while read -r i; do
	install -Dm644 "$ICI/icons/$i" "$PREFIXE/share/icons/hicolor/$i"
done
mkdir -p "$(dirname "$UNITE")"
sed "s|@LIB@|$LIB|g" "$ICI/jasette-serveur.service" > "$UNITE"
systemctl --user daemon-reload 2>/dev/null || true
echo "Jasette est installé dans $LIB."
echo "Lance-le depuis le menu, ou : $PREFIXE/bin/jasette"
echo "Serveur (facultatif) : systemctl --user enable --now jasette-serveur"
