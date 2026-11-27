#!/usr/bin/env bash
# Jasette (Windows) — les shaders HLSL de GPUI, précompilés par fxc.exe sous wine.
#
# gpui_windows les compile dans son build.rs seulement quand la machine qui
# compile est Windows (#[cfg(target_os = "windows")] d'un script de build =
# l'hôte). En compilation croisée depuis Linux, la version release cherche
# alors $OUT_DIR/shaders_bytes.rs, absent. Ce script le fabrique, de la même
# façon que ce build.rs (fxc /T vs_4_1|ps_4_1 /O3, puis un tableau Rust par
# shader), avec fxc.exe et d3dcompiler_47.dll du SDK Windows (paquet NuGet
# Microsoft.Windows.SDK.CPP, téléchargé une fois dans .cache/telechargements).
#
#   shaders-gpui.sh <dossier des sources de gpui-pre-windows> <fichier de sortie>
set -euo pipefail
SRC="$1"; SORTIE="$2"
ICI="$(cd "$(dirname "$0")" && pwd)"
PROJET="$(dirname "$(dirname "$ICI")")"
# Le .cache du dépôt principal (aussi depuis un worktree git rangé dans .cache).
C="${JASETTE_CACHE:-$(dirname "$(git -C "$PROJET" rev-parse --path-format=absolute --git-common-dir)")/.cache}"
FXC="$C/telechargements/fxc"
export WINEPREFIX="${WINEPREFIX:-$C/wine}"
export WINEDLLOVERRIDES="d3dcompiler_47=n"

if [[ ! -f "$FXC/fxc.exe" || ! -f "$FXC/d3dcompiler_47.dll" ]]; then
	V=10.0.28000.2957
	mkdir -p "$FXC"
	echo "== fxc.exe : Microsoft.Windows.SDK.CPP $V (NuGet)"
	curl -fsSL -o "$FXC/sdk.nupkg" "https://api.nuget.org/v3-flatcontainer/microsoft.windows.sdk.cpp/$V/microsoft.windows.sdk.cpp.$V.nupkg"
	unzip -j -o -q "$FXC/sdk.nupkg" "c/bin/10.0.28000.0/x64/fxc.exe" "c/bin/10.0.28000.0/x64/d3dcompiler_47.dll" -d "$FXC"
	rm -f "$FXC/sdk.nupkg"
fi

TRAVAIL="$C/gpui-shaders/travail"
rm -rf "$TRAVAIL"; mkdir -p "$TRAVAIL"
cp "$SRC"/*.hlsl "$TRAVAIL/"
cp "$FXC/fxc.exe" "$FXC/d3dcompiler_47.dll" "$TRAVAIL/"
: > "$TRAVAIL/shaders_bytes.rs"
for module in quad opaque_quad shadow path_tile underline monochrome_sprite subpixel_sprite polychrome_sprite; do
	for t in vertex:vs:VERTEX fragment:ps:FRAGMENT; do
		IFS=: read -r entree prefixe nom <<<"$t"
		const="${module^^}_${nom}_BYTES"
		(cd "$TRAVAIL" && wine fxc.exe /nologo /T "${prefixe}_4_1" /E "${module}_${entree}" /Fh "${module}_${prefixe}.h" \
			/Vn "$const" /O3 shaders.hlsl >> "$TRAVAIL/fxc.log" 2>&1) \
			|| { echo "fxc : échec pour ${module}_${entree} (voir $TRAVAIL/fxc.log)" >&2; exit 1; }
		# « const BYTE NOM[] = { … }; » devient « const NOM: &[u8] = &[ … ]; »
		python3 -I - "$TRAVAIL/${module}_${prefixe}.h" "$const" >> "$TRAVAIL/shaders_bytes.rs" <<'PY'
import sys
texte = open(sys.argv[1], encoding="utf-8", errors="replace").read()
g = texte[texte.index("const BYTE"):]
corps = g[g.index("=") + 1:].strip()
print(f"const {sys.argv[2]}: &[u8] = &{corps.replace('{', '[').replace('}', ']')}")
PY
	done
done
mkdir -p "$(dirname "$SORTIE")"
mv "$TRAVAIL/shaders_bytes.rs" "$SORTIE"
rm -rf "$TRAVAIL"
echo "== shaders GPUI : $SORTIE"
