#!/bin/bash
# libsignal-ffi (Rust, bibliothèque statique) pour le pont Signal sur macOS,
# comme pont-signal/build.sh sous Linux. Lancé par macos/construire.sh avec
# les cibles Rust voulues (aarch64-apple-darwin, x86_64-apple-darwin).
# Tout reste dans ~/Developer/jasette-macos (outils/, cache/).
set -euo pipefail
ICI="$(cd "$(dirname "$0")" && pwd)"
SRC="$(dirname "$ICI")"
BASE="$(dirname "$SRC")"
OUTILS="$BASE/outils"
C="$BASE/cache"
mkdir -p "$OUTILS" "$C"

# La version de libsignal que demande signalmeow.
SIGNAL_MOD="$(cd "$SRC/pont-signal" && go list -m -f '{{.Dir}}' go.mau.fi/mautrix-signal)"
VERSION="$(sed -n 's/^const Version = "\(.*\)"/\1/p' "$SIGNAL_MOD/pkg/libsignalgo/signalversion/version.go")"
echo "== libsignal $VERSION"

# protoc et cmake : les archives officielles, dans outils/ (pas de Homebrew).
if [[ ! -x "$OUTILS/protoc/bin/protoc" ]]; then
	mkdir -p "$OUTILS/protoc"
	curl -sSL -o "$OUTILS/protoc/protoc.zip" https://github.com/protocolbuffers/protobuf/releases/download/v29.3/protoc-29.3-osx-universal_binary.zip
	(cd "$OUTILS/protoc" && unzip -qo protoc.zip && rm protoc.zip)
fi
if [[ ! -x "$OUTILS/cmake/CMake.app/Contents/bin/cmake" ]]; then
	mkdir -p "$OUTILS/cmake"
	curl -sSL https://github.com/Kitware/CMake/releases/download/v3.31.6/cmake-3.31.6-macos-universal.tar.gz \
		| tar xz -C "$OUTILS/cmake" --strip-components 1
fi

if [[ ! -d "$C/libsignal/.git" ]]; then
	git clone -q --depth 1 --branch "$VERSION" https://github.com/signalapp/libsignal.git "$C/libsignal"
elif [[ "$(git -C "$C/libsignal" describe --tags 2>/dev/null)" != "$VERSION" ]]; then
	git -C "$C/libsignal" fetch -q --depth 1 origin tag "$VERSION"
	git -C "$C/libsignal" checkout -q "$VERSION"
fi

# protobuf-parse cherche protoc dans le PATH (PROTOC ne suffit pas).
export PATH="$OUTILS/protoc/bin:$OUTILS/cmake/CMake.app/Contents/bin:$PATH"
for t in "$@"; do
	echo "== libsignal-ffi ($t)"
	(
		cd "$C/libsignal"
		PROTOC="$OUTILS/protoc/bin/protoc" \
		CARGO_TARGET_DIR="$C/libsignal-target" \
		RUSTUP_TOOLCHAIN=stable \
		RUSTC_WRAPPER="" \
		cargo build -p libsignal-ffi --profile=release --target "$t"
	)
	ls -la "$C/libsignal-target/$t/release/libsignal_ffi.a"
done
