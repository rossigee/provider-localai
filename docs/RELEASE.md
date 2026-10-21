# Release Process

## Current Release

`v0.21.0`

## Preparation

1. Create `release/v0.21.0` from the latest `origin/master`.
2. Update `VERSION`, `internal/version/version.go`, `package/crossplane.yaml`, and current installation references.
3. Add the release entry to `CHANGELOG.md`.
4. Run:

   ```bash
   make reviewable
   make build
   make build.all build.artifacts VERSION=v0.21.0 PLATFORMS="linux_amd64 linux_arm64"
   for platform in linux_amd64 linux_arm64; do
     make xpkg.build VERSION=v0.21.0 PLATFORMS="linux_amd64 linux_arm64" PLATFORM="$platform"
   done
   ```

5. Open a pull request targeting `master` and wait for review, CI, and security checks.

## Tagging

After the pull request is merged and `master` is green:

```bash
set -euo pipefail
VERSION=v0.21.0
git fetch origin master
test -z "$(git status --porcelain)"
test "$(git rev-parse HEAD)" = "$(git rev-parse origin/master)"
test "$(<VERSION)" = "$VERSION"
test -z "$(git show-ref --tags "$VERSION")"
test -z "$(git ls-remote --tags origin "refs/tags/$VERSION")"
git tag -a "$VERSION" -m "Release $VERSION" HEAD
git push origin "refs/tags/$VERSION"
```

Published tags are immutable. Never force-move or delete a release tag.

## Workflow

The tag-only workflow builds `linux_amd64` and `linux_arm64` xpkg files, publishes the version and `latest` references to GHCR, verifies equal digests and both platforms, and creates the GitHub Release.

## Verification

```bash
gh run list --workflow Release --limit 1
gh release view v0.21.0
docker buildx imagetools inspect \
  ghcr.io/rossigee/provider-keycloak:v0.21.0 \
  --format '{{.Manifest.Digest}}'
docker buildx imagetools inspect \
  ghcr.io/rossigee/provider-keycloak:latest \
  --format '{{.Manifest.Digest}}'
```

The two digests must match, and both OCI indexes must contain `linux/amd64` and `linux/arm64`.
