#!/bin/sh
# Sign a macOS build so the keychain keeps trusting it across rebuilds.
#
# The keychain binds each stored secret to the code identity of the binary that
# stored it. Go ad-hoc signs by default, and an ad-hoc signature has no
# certificate to anchor to, so the identity falls back to the binary's cdhash -
# which is different for every build, even of identical source. The result is
# that every `go build` invalidates every stored token.
#
# A certificate fixes it. Signed, the designated requirement becomes the
# identifier plus the signing authority, with no cdhash in it, so any later build
# signed by the same certificate satisfies the same requirement.
#
#   REPOMAP_SIGN_IDENTITY  the identity to sign with, as `security find-identity
#                          -v -p codesigning` prints it. Required.
#
# Usage: scripts/sign.sh <binary>
set -eu

BINARY="${1:-}"
[ -n "$BINARY" ] || { echo "usage: $0 <binary>" >&2; exit 2; }
[ -f "$BINARY" ] || { echo "error: no such file: $BINARY" >&2; exit 2; }

# The identifier is pinned here rather than taken from the file name, because it
# is half of the designated requirement: a binary installed as `repomap` and one
# built as `./repomap-dev` must present the same identity or the keychain treats
# them as different programs.
IDENTIFIER="com.branow.repomap"

if [ "$(uname -s)" != "Darwin" ]; then
  echo "not macOS; nothing to sign" >&2
  exit 0
fi

if [ -z "${REPOMAP_SIGN_IDENTITY:-}" ]; then
  cat >&2 <<'EOF'
error: REPOMAP_SIGN_IDENTITY is not set, so this build would be ad-hoc signed and
       the keychain would stop trusting it the moment it is rebuilt.

  Pick an identity:

    security find-identity -v -p codesigning

  For local development, create one once - Keychain Access > Certificate
  Assistant > Create a Certificate, name it `repomap-dev`, identity type
  `Self Signed Root`, certificate type `Code Signing` - then:

    export REPOMAP_SIGN_IDENTITY=repomap-dev

  For a release, use the Developer ID Application certificate, which also gets
  the binaries past Gatekeeper without stripping quarantine by hand.
EOF
  exit 1
fi

codesign --force --options runtime \
  --identifier "$IDENTIFIER" \
  --sign "$REPOMAP_SIGN_IDENTITY" \
  "$BINARY"

# The requirement is what the keychain actually stores, so print it: a cdhash in
# here means the signature did not take and the next rebuild will lock you out.
# Some codesign versions prefix the line with "# " and some do not, and a pattern
# that misses it reads an empty requirement and passes everything.
requirement=$(codesign -d -r- "$BINARY" 2>/dev/null | sed -n 's/^\(# \)\{0,1\}designated => //p')
echo "signed $BINARY"
echo "  requirement: $requirement"

case "$requirement" in
  "")
    echo "error: could not read the designated requirement back from $BINARY" >&2
    exit 1
    ;;
  *cdhash*)
    echo "warning: the requirement is still cdhash-based, so a rebuild will lose" >&2
    echo "         keychain access. Check that the identity is a real certificate." >&2
    exit 1
    ;;
esac
