#!/bin/sh
# Compile Greffier pour toutes les plateformes dans dist/.
# Usage : ./compiler.sh [version]      (Go ≥ 1.22 ; utilisé aussi par la GitHub Action)
set -eu
cd "$(dirname "$0")"
VERSION="${1:-dev-$(date +%Y%m%d)}"
rm -rf dist && mkdir -p dist
for cible in windows/amd64/greffier-windows-x64.exe windows/arm64/greffier-windows-arm64.exe \
             darwin/arm64/greffier-macos-apple-silicon darwin/amd64/greffier-macos-intel \
             linux/amd64/greffier-linux-x64; do
  os=${cible%%/*}; reste=${cible#*/}; arch=${reste%%/*}; nom=${reste#*/}
  echo "→ $nom"
  CGO_ENABLED=0 GOOS=$os GOARCH=$arch go build -trimpath -ldflags "-s -w -X main.version=$VERSION" -o "dist/$nom" .
done
( cd dist && sha256sum * > SHA256SUMS.txt )
ls -l dist
