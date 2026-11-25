#!/usr/bin/env bash
# Jasette — un serveur Jasette rempli de conversations INVENTÉES, pour prendre
# les captures du Play Store sur un téléphone ou une tablette sans montrer de
# vraies données.
#
#   android/play/serveur-fictif.sh [fr|en]       lance le serveur (Ctrl+C pour l'arrêter)
#   android/play/serveur-fictif.sh --jumeler     dans un 2e terminal : le code de jumelage
#
# Il réutilise les faux ponts des captures du bureau (.cache/captures/faux-*,
# faux-pont.py, avatars/) : aucun réseau, aucun compte. Ses données restent
# dans .cache/serveur-fictif/ (effaçable). Port 47801, pour ne pas gêner un
# vrai serveur Jasette sur 47800.
#
# Sur l'appareil : Jasette en mode « Client », adresse <IP de ce PC>:47801, le
# code de --jumeler. Fais-le dans un profil invité ou un 2e utilisateur
# Android (ou sur une tablette) pour ne pas toucher à tes vrais comptes.
set -euo pipefail
ICI="$(cd "$(dirname "$0")" && pwd)"
PROJET="$(cd "$ICI/../.." && pwd)"
CAP="$PROJET/.cache/captures"
D="$PROJET/.cache/serveur-fictif"
SRV="$D/jasette-serveur"
[[ -x "$CAP/faux-google" ]] || { echo "Faux ponts absents : $CAP" >&2; exit 1; }
mkdir -p "$D/data" "$D/media"
# Toujours recompilé depuis serveur/ (le cache de Go rend ça instantané).
(cd "$PROJET/serveur" && GOCACHE="$PROJET/.cache/gocache" GOMODCACHE="$PROJET/.cache/gomod" \
    GOPATH="$PROJET/.cache/gopath" go build -o "$SRV" .)
export JASETTE_DATA="$D/data" JASETTE_MEDIA="$D/media" JASETTE_SERVEUR_PORT=47801
if [[ "${1:-}" == "--jumeler" ]]; then
    exec "$SRV" --jumeler
fi
export FAUX_AVATARS="$CAP/avatars" FAUX_LANGUE="${1:-fr}"
export JASETTE_PONT="$CAP/faux-google" JASETTE_META="$CAP/faux-messenger" JASETTE_IG="$CAP/faux-instagram" \
       JASETTE_WA="$CAP/faux-whatsapp" JASETTE_SIGNAL="$CAP/faux-signal" JASETTE_X="$CAP/faux-x"
echo "Serveur fictif sur le port 47801 ($(hostname -I | awk '{print $1}')), langue ${FAUX_LANGUE}."
exec "$SRV"
