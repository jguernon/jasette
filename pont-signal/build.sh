#!/bin/sh
# Compile le pont Signal : libsignal-ffi (Rust), puis jasette-signal (Go + cgo).
#
# Tout reste dans <projet>/.cache/ (jamais ~/.cache ni /tmp) :
#   .cache/libsignal/          sources de libsignal (la version que demande signalmeow)
#   .cache/libsignal-target/   compilation Rust (CARGO_TARGET_DIR)
#   .cache/cargo-home/         registre de cargo (CARGO_HOME)
#   .cache/protoc/             protoc (libsignal en a besoin pour compiler)
#   .cache/staticlibs/         libstdc++.a, pour un binaire sans libstdc++.so
#   .cache/gomod, gocache, gopath, gotmp : Go
#   .cache/bin/jasette-signal  le binaire
#
# Le binaire ne dépend que de la glibc (libc, libm) : il se copie seul sur une
# autre machine Linux x86_64 (glibc au moins aussi récente).
set -eu

ICI=$(cd "$(dirname "$0")" && pwd)
PROJET=$(dirname "$ICI")
C="$PROJET/.cache"
mkdir -p "$C/bin" "$C/gotmp" "$C/staticlibs"

export GOMODCACHE="$C/gomod" GOCACHE="$C/gocache" GOPATH="$C/gopath"
export GOTMPDIR="$C/gotmp" TMPDIR="$C/gotmp" GOFLAGS=-modcacherw GOTOOLCHAIN=local

# La version de libsignal que demande signalmeow (pkg/libsignalgo/signalversion).
SIGNAL_MOD=$(cd "$ICI" && go list -m -f '{{.Dir}}' go.mau.fi/mautrix-signal)
VERSION=$(sed -n 's/^const Version = "\(.*\)"/\1/p' "$SIGNAL_MOD/pkg/libsignalgo/signalversion/version.go")
echo "libsignal $VERSION"

# protoc : binaire officiel, gardé dans le projet.
if [ ! -x "$C/protoc/bin/protoc" ]; then
	mkdir -p "$C/protoc"
	curl -sSL -o "$C/protoc/protoc.zip" https://github.com/protocolbuffers/protobuf/releases/download/v29.3/protoc-29.3-linux-x86_64.zip
	(cd "$C/protoc" && unzip -qo protoc.zip && rm protoc.zip)
fi

# Les sources de libsignal, à la bonne étiquette.
if [ ! -d "$C/libsignal/.git" ]; then
	git clone --depth 1 --branch "$VERSION" https://github.com/signalapp/libsignal.git "$C/libsignal"
elif [ "$(git -C "$C/libsignal" describe --tags 2>/dev/null)" != "$VERSION" ]; then
	git -C "$C/libsignal" fetch --depth 1 origin tag "$VERSION"
	git -C "$C/libsignal" checkout -q "$VERSION"
fi

# libsignal-ffi, comme build-rust.sh de mautrix-signal (bibliothèque statique).
# RUSTUP_TOOLCHAIN=stable : la chaîne stable déjà installée, au lieu de celle
# épinglée par libsignal (rust-toolchain), pour ne rien télécharger de plus.
(
	cd "$C/libsignal"
	PATH="$C/protoc/bin:$HOME/.cargo/bin:$PATH" \
	PROTOC="$C/protoc/bin/protoc" \
	CARGO_HOME="$C/cargo-home" \
	CARGO_TARGET_DIR="$C/libsignal-target" \
	RUSTUP_TOOLCHAIN=stable \
	RUSTFLAGS="-Ctarget-feature=-crt-static" \
	RUSTC_WRAPPER="" \
	cargo build -p libsignal-ffi --profile=release
)

# libstdc++ en statique : un dossier où l'éditeur de liens trouve le .a avant le .so.
ln -sf "$(g++ -print-file-name=libstdc++.a)" "$C/staticlibs/libstdc++.a"

cd "$ICI"
CGO_ENABLED=1 \
LIBRARY_PATH="$C/libsignal-target/release" \
CGO_LDFLAGS="'-L$C/staticlibs'" \
go build -trimpath -ldflags '-s -w -extldflags=-static-libgcc' -o "$C/bin/jasette-signal" .

ls -la "$C/bin/jasette-signal"
