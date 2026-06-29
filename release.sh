#!/usr/bin/env sh
# release.sh — cut a release in one command.
#
#   ./release.sh v0.1.0
#
# Ties the whole publish together: cross-compiles the binaries (build-release.sh),
# tags the commit, pushes the tag, and creates the GitHub release with the
# assets attached. end-user install.sh then downloads the matching binary from
# releases/latest/download — no Go toolchain on their side.
#
# Idempotent: re-running for the same version re-uses the existing tag/release
# and clobbers the uploaded assets, so a half-finished release is safe to retry.
#
# Requires: git, go, and an authenticated `gh` (run `gh auth login` once).
#
# Env overrides:
#   HELPME_ALLOW_DIRTY=1     skip the clean-working-tree check
#   HELPME_RELEASE_BRANCH    branch a release must be cut from (default: master)
set -e

VERSION="${1:-}"
RELEASE_BRANCH="${HELPME_RELEASE_BRANCH:-master}"

die() { echo "release: $*" >&2; exit 1; }

# --- args -------------------------------------------------------------------
[ -n "$VERSION" ] || die "usage: ./release.sh vX.Y.Z   (e.g. ./release.sh v0.1.0)"
# Semver-ish: v MAJOR.MINOR.PATCH, optional -prerelease / +build.
echo "$VERSION" | grep -Eq '^v[0-9]+\.[0-9]+\.[0-9]+([-+].+)?$' \
  || die "version must look like v1.2.3 (optionally v1.2.3-rc1); got '$VERSION'"

# --- preflight --------------------------------------------------------------
command -v git >/dev/null 2>&1 || die "git not found on PATH"
command -v go  >/dev/null 2>&1 || die "go not found on PATH (needed to cross-compile)"
command -v gh  >/dev/null 2>&1 || die "gh not found — install GitHub CLI: https://cli.github.com"
gh auth status >/dev/null 2>&1 || die "gh is not authenticated — run: gh auth login"

REPO_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
cd "$REPO_DIR"

branch=$(git rev-parse --abbrev-ref HEAD)
[ "$branch" = "$RELEASE_BRANCH" ] \
  || die "on branch '$branch', expected '$RELEASE_BRANCH' (override: HELPME_RELEASE_BRANCH=$branch)"

if [ -z "$HELPME_ALLOW_DIRTY" ] && [ -n "$(git status --porcelain)" ]; then
  die "working tree is dirty — commit/stash first, or set HELPME_ALLOW_DIRTY=1"
fi

# Tag must not already point at a *different* commit, else the release would
# describe code that isn't what we're building.
head=$(git rev-parse HEAD)
if git rev-parse -q --verify "refs/tags/$VERSION" >/dev/null 2>&1; then
  tagged=$(git rev-parse "refs/tags/$VERSION^{commit}")
  [ "$tagged" = "$head" ] \
    || die "tag $VERSION already exists at $tagged but HEAD is $head — delete the tag or pick a new version"
  echo "tag $VERSION already exists at HEAD — reusing"
fi

# --- build ------------------------------------------------------------------
echo "==> Building $VERSION"
./build-release.sh "$VERSION"
# Confirm the expected assets landed.
[ -f dist/SHA256SUMS ] || die "build produced no dist/SHA256SUMS"
ls dist/helpme-bin-* >/dev/null 2>&1 || die "build produced no dist/helpme-bin-* assets"

# --- tag + push -------------------------------------------------------------
if ! git rev-parse -q --verify "refs/tags/$VERSION" >/dev/null 2>&1; then
  echo "==> Tagging $VERSION"
  git tag -a "$VERSION" -m "helpme $VERSION"
fi
echo "==> Pushing tag $VERSION"
git push origin "refs/tags/$VERSION"

# --- publish ----------------------------------------------------------------
# A pre-release (v1.2.3-rc1) must not become "latest", which install.sh pulls.
prerelease=
case "$VERSION" in *-*) prerelease="--prerelease" ;; esac

if gh release view "$VERSION" >/dev/null 2>&1; then
  echo "==> Release $VERSION exists — uploading assets (clobber)"
  gh release upload "$VERSION" dist/helpme-bin-* dist/SHA256SUMS --clobber
else
  echo "==> Creating release $VERSION"
  # shellcheck disable=SC2086
  gh release create "$VERSION" dist/helpme-bin-* dist/SHA256SUMS \
    --title "helpme $VERSION" --generate-notes $prerelease
fi

echo
echo "Released $VERSION."
gh release view "$VERSION" --json url --jq .url 2>/dev/null || true
echo "Users can now:  curl -fsSL https://raw.githubusercontent.com/alanmun/helpme/$RELEASE_BRANCH/install.sh | sh"
