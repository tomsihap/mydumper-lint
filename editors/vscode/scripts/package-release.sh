#!/usr/bin/env bash
# Packages the extension for every platform from goreleaser's archives:
#
#   scripts/package-release.sh DIST OUT
#
# DIST is goreleaser's dist/ (metadata.json and the mydumper-lint_<version>_<os>_<arch>
# archives). OUT receives one mydumper-lint-<target>-<version>.vsix per VS Code target,
# each with the executable of that platform in bin/, and mydumper-lint-<version>.vsix, the
# universal package without executable (other platforms: the extension then runs
# mydumper-lint from PATH). The Visual Studio Marketplace and Open VSX serve each
# platform its own package.
#
# The archives' version must be the extension's (release-please bumps both), unless
# SNAPSHOT=1: CI packages a goreleaser snapshot to test this script.
set -euo pipefail

if [[ $# -ne 2 ]]; then
  echo "usage: $0 DIST OUT" >&2
  exit 2
fi
dist=$(cd "$1" && pwd)
mkdir -p "$2"
out=$(cd "$2" && pwd)
cd "$(dirname "$0")/.."

version=$(node -p 'require("./package.json").version')
archives=$(node -p 'require(process.argv[1]).version' "$dist/metadata.json")
if [[ $archives != "$version" && ${SNAPSHOT:-} != 1 ]]; then
  echo "the archives are mydumper-lint $archives, the extension is $version" >&2
  exit 1
fi

# VS Code target, then goreleaser's os_arch. Alpine runs the Linux executables:
# they are static (CGO_ENABLED=0 in .goreleaser.yaml).
targets=(
  "linux-x64 linux_amd64"
  "linux-arm64 linux_arm64"
  "alpine-x64 linux_amd64"
  "alpine-arm64 linux_arm64"
  "darwin-x64 darwin_amd64"
  "darwin-arm64 darwin_arm64"
  "win32-x64 windows_amd64"
)

# The Marketplace shows the extension's CHANGELOG.md: the project's.
if [[ -f ../../CHANGELOG.md ]]; then
  cp ../../CHANGELOG.md CHANGELOG.md
fi
trap 'rm -rf bin CHANGELOG.md' EXIT

vsce=node_modules/.bin/vsce
for t in "${targets[@]}"; do
  read -r target osarch <<<"$t"
  rm -rf bin
  mkdir bin
  if [[ $osarch == windows_* ]]; then
    exe=mydumper-lint.exe
    unzip -q -j "$dist/mydumper-lint_${archives}_${osarch}.zip" "$exe" -d bin
  else
    exe=mydumper-lint
    tar -xzf "$dist/mydumper-lint_${archives}_${osarch}.tar.gz" -C bin "$exe"
    chmod 755 "bin/$exe"
  fi
  vsix="$out/mydumper-lint-$target-$version.vsix"
  "$vsce" package --no-dependencies --target "$target" --out "$vsix"

  # The package must carry its platform and its executable, executable.
  if ! unzip -p "$vsix" extension.vsixmanifest | grep -q "TargetPlatform=\"$target\""; then
    echo "$vsix: no TargetPlatform=\"$target\" in extension.vsixmanifest" >&2
    exit 1
  fi
  entry=$(unzip -Z "$vsix" "extension/bin/$exe")
  if [[ $exe != *.exe && $entry != -rwxr-xr-x* ]]; then
    echo "$vsix: extension/bin/$exe is not executable: $entry" >&2
    exit 1
  fi
done

rm -rf bin
vsix="$out/mydumper-lint-$version.vsix"
"$vsce" package --no-dependencies --out "$vsix"
if unzip -Z1 "$vsix" | grep -q '^extension/bin/'; then
  echo "$vsix: the universal package must not carry an executable" >&2
  exit 1
fi

ls -l "$out"/*.vsix
