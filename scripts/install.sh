#!/usr/bin/env sh
# Nemalo installer for Linux and macOS.
#
#   curl -fsSL https://raw.githubusercontent.com/blisspixel/nemalo/main/scripts/install.sh | sh
#   curl -fsSL https://raw.githubusercontent.com/blisspixel/nemalo/main/scripts/install.sh | sh -s -- --version 0.1.0-alpha.3
#   curl -fsSL https://raw.githubusercontent.com/blisspixel/nemalo/main/scripts/install.sh | sh -s -- --uninstall
#
# Downloads the published release archive for this platform, verifies it against
# the release SHA256SUMS, and installs the single nemalo executable. Nothing else
# is installed, no shell profile is edited, and no data or library is touched.
# NEMALO_INSTALL_DIR overrides the default directory, ~/.local/bin.
set -eu

REPO="blisspixel/nemalo"
DIR="${NEMALO_INSTALL_DIR:-$HOME/.local/bin}"
VERSION=""
UNINSTALL=0

err() {
  echo "nemalo install: $1" >&2
  [ -n "${2:-}" ] && echo "  hint: $2" >&2
  exit 1
}

have() { command -v "$1" >/dev/null 2>&1; }

usage() {
  echo "usage: install.sh [--version VERSION] [--dir DIRECTORY] [--uninstall]"
}

while [ $# -gt 0 ]; do
  case "$1" in
    --version) [ $# -ge 2 ] || err "--version needs a value"; VERSION=$2; shift 2 ;;
    --version=*) VERSION=${1#*=}; shift ;;
    --dir) [ $# -ge 2 ] || err "--dir needs a value"; DIR=$2; shift 2 ;;
    --dir=*) DIR=${1#*=}; shift ;;
    --uninstall) UNINSTALL=1; shift ;;
    -h|--help) usage; exit 0 ;;
    *) usage >&2; err "unknown option $1" ;;
  esac
done

case "$DIR" in
  /*) ;;
  *) err "install directory must be an absolute path: $DIR" ;;
esac

if [ "$UNINSTALL" -eq 1 ]; then
  if [ -e "$DIR/nemalo" ] || [ -L "$DIR/nemalo" ]; then
    rm -f "$DIR/nemalo"
    echo "removed $DIR/nemalo"
  else
    echo "nothing to remove at $DIR/nemalo"
  fi
  echo "Libraries, intake folders, configuration, and state were not changed."
  exit 0
fi

os=$(uname -s)
case "$os" in
  Linux) os=linux ;;
  Darwin) os=darwin ;;
  MINGW*|MSYS*|CYGWIN*)
    err "this is the Linux and macOS installer" \
      "in PowerShell run: irm https://raw.githubusercontent.com/$REPO/main/scripts/install.ps1 | iex" ;;
  *) err "unsupported operating system $os" "build from source with Go: see the README" ;;
esac
arch=$(uname -m)
case "$arch" in
  x86_64|amd64) arch=amd64 ;;
  aarch64|arm64) arch=arm64 ;;
  *) err "unsupported architecture $arch" "releases provide amd64 and arm64" ;;
esac

# fetch downloads one URL to a new file. curl retries transient HTTP
# errors and timeouts itself; a dropped connection is tried again up to
# three times, each from an empty file, with a growing pause. A failure to
# write the file is reported as that, since a retry cannot help, and an
# empty response is a failure. It returns 22 for an HTTP error response.
fetch() {
  attempt=1
  while :; do
    rm -f "$2"
    status=0
    if have curl; then
      curl -fsSL --proto '=https' --tlsv1.2 --connect-timeout 20 --retry 3 --retry-delay 2 "$1" -o "$2" || status=$?
      written=23
      http=22
    elif have wget; then
      wget -q --https-only --timeout=20 --tries=3 -O "$2" "$1" || status=$?
      written=3
      http=8
    else
      err "curl or wget is required"
    fi
    if [ "$status" -eq 0 ] && [ -s "$2" ]; then
      return 0
    fi
    rm -f "$2"
    if [ "$status" -eq "$written" ]; then
      err "could not write $2" "the folder for temporary files may be full; set TMPDIR to a folder with free space"
    fi
    if [ "$status" -eq "$http" ]; then
      return 22
    fi
    if [ "$attempt" -ge 3 ]; then
      return 1
    fi
    sleep $((attempt * 2))
    attempt=$((attempt + 1))
  done
}

sha256() {
  if have sha256sum; then
    sha256sum "$1" | awk '{print $1}'
  elif have shasum; then
    shasum -a 256 "$1" | awk '{print $1}'
  else
    err "sha256sum or shasum is required to verify the download"
  fi
}

# A full or missing temporary folder falls back to ~/.cache.
work=$(mktemp -d "${TMPDIR:-/tmp}/nemalo-install.XXXXXX" 2>/dev/null) ||
  { mkdir -p "$HOME/.cache" && work=$(mktemp -d "$HOME/.cache/nemalo-install.XXXXXX"); } ||
  err "could not create a temporary folder" "set TMPDIR to a folder with free space"
trap 'rm -rf "$work"' EXIT INT TERM

if [ -z "$VERSION" ]; then
  # /releases/latest excludes prereleases. The list endpoint's order is not
  # semantic version order: on 2026-10-13 its first item was v0.1.0-alpha.9
  # after v0.1.0-alpha.10 was published. Choose the highest version.
  # A GITHUB_TOKEN, when set, is sent only to api.github.com, to avoid the
  # unauthenticated rate limit on shared networks such as CI runners. The
  # header goes through stdin so the token never appears in process arguments.
  # A list that names no release is asked for again, twice: the list API has
  # briefly answered with none while releases existed.
  for attempt in 1 2 3; do
  if [ -n "${GITHUB_TOKEN:-}" ] && have curl; then
    printf 'Authorization: Bearer %s\n' "$GITHUB_TOKEN" |
      curl -fsSL --proto '=https' --tlsv1.2 -H @- \
        "https://api.github.com/repos/$REPO/releases?per_page=100" -o "$work/releases.json"
  else
    fetch "https://api.github.com/repos/$REPO/releases?per_page=100" "$work/releases.json"
  fi || err "could not query the release list" "pass --version VERSION to skip the lookup"
  VERSION=$(awk '
    function numcmp(a, b, aa, bb) {
      aa = a
      bb = b
      sub(/^0+/, "", aa)
      sub(/^0+/, "", bb)
      if (aa == "") aa = "0"
      if (bb == "") bb = "0"
      if (length(aa) > length(bb)) return 1
      if (length(aa) < length(bb)) return -1
      if (aa > bb) return 1
      if (aa < bb) return -1
      return 0
    }
    function vercmp(a, b, ac, bc, ap, bp, i, c, an, bn, n, as, bs, left, right) {
      ac = a
      bc = b
      sub(/^v/, "", ac)
      sub(/^v/, "", bc)
      ap = ""
      bp = ""
      if (match(ac, /-/)) {
        ap = substr(ac, RSTART + 1)
        ac = substr(ac, 1, RSTART - 1)
      }
      if (match(bc, /-/)) {
        bp = substr(bc, RSTART + 1)
        bc = substr(bc, 1, RSTART - 1)
      }
      split(ac, as, ".")
      split(bc, bs, ".")
      for (i = 1; i <= 3; i++) {
        c = numcmp(as[i], bs[i])
        if (c != 0) return c
      }
      if (ap == "" && bp == "") return 0
      if (ap == "") return 1
      if (bp == "") return -1
      an = split(ap, left, ".")
      bn = split(bp, right, ".")
      n = an
      if (bn < n) n = bn
      for (i = 1; i <= n; i++) {
        if (left[i] ~ /^[0-9]+$/ && right[i] ~ /^[0-9]+$/) c = numcmp(left[i], right[i])
        else if (left[i] ~ /^[0-9]+$/) c = -1
        else if (right[i] ~ /^[0-9]+$/) c = 1
        else if (left[i] > right[i]) c = 1
        else if (left[i] < right[i]) c = -1
        else c = 0
        if (c != 0) return c
      }
      if (an > bn) return 1
      if (an < bn) return -1
      return 0
    }
    BEGIN { best = ""; count = 0; tag = ""; draft = 0; too = 0 }
    /^  \{$/ { tag = ""; draft = 0; next }
    /^    "tag_name": "/ {
      tag = $0
      sub(/^    "tag_name": "/, "", tag)
      sub(/".*$/, "", tag)
      next
    }
    /^    "draft": true[,]?$/ { draft = 1; next }
    /^  \},?$/ {
      count++
      if (count >= 100) { too = 1; exit }
      if (!draft && tag ~ /^v?[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.]+)?$/ && (best == "" || vercmp(tag, best) > 0)) best = tag
      tag = ""
      draft = 0
      next
    }
    END { if (too) print "TOO_MANY"; else if (best != "") print best }
  ' "$work/releases.json") || err "could not read the release list" "pass --version VERSION to skip the lookup"
  [ -z "$VERSION" ] || break
  [ "$attempt" = 3 ] || sleep "$((attempt * 2))"
  done
  [ "$VERSION" != "TOO_MANY" ] || err "the release list is too long to choose a version" "pass --version VERSION"
  [ -n "$VERSION" ] || err "no published release found"
fi
VERSION=${VERSION#v}
printf '%s\n' "$VERSION" | grep -Eq '^[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.]+)?$' ||
  err "invalid version $VERSION"

asset="nemalo_${VERSION}_${os}_${arch}.tar.gz"
base="https://github.com/$REPO/releases/download/v$VERSION"
echo "Installing Nemalo $VERSION ($os/$arch) into $DIR"
fetch "$base/SHA256SUMS" "$work/SHA256SUMS" || {
  [ $? -eq 22 ] && err "release $VERSION was not found at $base" "check the version, or omit --version for the newest"
  err "could not download $base/SHA256SUMS" "check the network and retry"
}
fetch "$base/$asset" "$work/$asset" || {
  [ $? -eq 22 ] && err "release $VERSION has no archive $asset" "releases provide linux and darwin, amd64 and arm64"
  err "could not download $base/$asset" "check the network and retry"
}

expected=$(awk -v name="$asset" '$2 == name || $2 == "*" name { n++; d = tolower($1) } END { if (n == 1) print d }' "$work/SHA256SUMS")
printf '%s\n' "$expected" | grep -Eq '^[0-9a-f]{64}$' ||
  err "SHA256SUMS does not list exactly one valid digest for $asset"
actual=$(sha256 "$work/$asset" | tr 'A-F' 'a-f')
[ "$actual" = "$expected" ] || err "checksum mismatch for $asset (expected $expected, got $actual)" "the download may be corrupt; retry"
echo "Verified SHA-256 $actual"

mkdir "$work/extract"
tar -xzf "$work/$asset" -C "$work/extract" nemalo || err "the archive does not contain the nemalo executable"
[ -f "$work/extract/nemalo" ] && [ ! -L "$work/extract/nemalo" ] || err "the archive member is not a regular file"
chmod 0755 "$work/extract/nemalo"
"$work/extract/nemalo" version >/dev/null || err "the downloaded executable does not run on this system"

mkdir -p "$DIR"
# Replace atomically within the destination directory.
cp "$work/extract/nemalo" "$DIR/.nemalo.install.$$"
mv -f "$DIR/.nemalo.install.$$" "$DIR/nemalo"
echo "Installed $DIR/nemalo ($("$DIR/nemalo" version))"

case ":$PATH:" in
  *":$DIR:"*) ;;
  *) echo "Note: $DIR is not on PATH. Add it in your shell profile, for example:"
     echo "  export PATH=\"$DIR:\$PATH\"" ;;
esac
echo "Next: nemalo doctor"
