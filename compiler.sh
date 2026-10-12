#!/bin/sh
# Compile Greffier dans dist/.
# Usage : ./compiler.sh [version] [systèmes]     (Go ≥ 1.22 ; utilisé aussi par la GitHub Action)
#   systèmes : parmi « windows darwin linux » (tous par défaut), ex. ./compiler.sh v1.2.0 darwin
# L'icône de la barre des menus macOS demande cgo, donc une compilation sur un Mac :
# compilés ailleurs, les binaires macOS fonctionnent sans icône.
set -eu
cd "$(dirname "$0")"
VERSION="${1:-dev-$(date +%Y%m%d)}"
SYSTEMES="${2:-windows darwin linux}"
rm -rf dist && mkdir -p dist
for cible in windows/amd64/greffier-windows-x64.exe windows/arm64/greffier-windows-arm64.exe \
             darwin/arm64/greffier-macos-apple-silicon darwin/amd64/greffier-macos-intel \
             linux/amd64/greffier-linux-x64; do
  os=${cible%%/*}; reste=${cible#*/}; arch=${reste%%/*}; nom=${reste#*/}
  case " $SYSTEMES " in *" $os "*) ;; *) continue ;; esac
  cgo=0
  if [ "$os" = darwin ] && [ "$(uname)" = Darwin ]; then cgo=1; fi
  echo "→ $nom (cgo=$cgo)"
  CGO_ENABLED=$cgo GOOS=$os GOARCH=$arch go build -trimpath -ldflags "-s -w -X main.version=$VERSION" -o "dist/$nom" .
done
( cd dist && { sha256sum -- * 2>/dev/null || shasum -a 256 -- *; } > ../SHA256SUMS.tmp ) && mv SHA256SUMS.tmp dist/SHA256SUMS.txt
ls -l dist
