#!/usr/bin/env sh
# build-release.sh — maintainer cross-compile.
#
# Produces static, dependency-free binaries for the common targets plus a
# SHA256SUMS file, all under dist/. Attach these to a GitHub release; the
# end-user install.sh downloads the matching one (no Go toolchain required).
#
#   ./build-release.sh v0.1.0
#
# Hooks are embedded in the binary (see hooks.go), so only the binaries ship.
set -e

VERSION="${1:-dev}"
OUT="${OUT:-dist}"

rm -rf "$OUT"
mkdir -p "$OUT"

# os/arch targets. Windows ships too: the shell hook is bash, and MSYS2 / Git
# Bash / Cygwin are POSIX bash on top of native Windows that run native .exe
# binaries (so does WSL, but WSL is just linux/amd64|arm64).
targets="linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64 windows/arm64"

for t in $targets; do
  os=${t%/*}
  arch=${t#*/}
  ext=""
  [ "$os" = windows ] && ext=".exe"   # native Windows executables need the suffix
  name="helpme-bin-${os}-${arch}${ext}"
  echo "building $name"
  CGO_ENABLED=0 GOOS="$os" GOARCH="$arch" \
    go build -trimpath -ldflags "-s -w -X main.version=$VERSION" -o "$OUT/$name" .
done

# Checksums (use sha256sum, or shasum -a 256 on macOS).
if command -v sha256sum >/dev/null 2>&1; then
  ( cd "$OUT" && sha256sum helpme-bin-* > SHA256SUMS )
else
  ( cd "$OUT" && shasum -a 256 helpme-bin-* > SHA256SUMS )
fi

echo
echo "Built version $VERSION -> $OUT/"
ls -la "$OUT"
