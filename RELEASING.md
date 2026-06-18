# Releasing

This project ships **signed** releases: each tag triggers a pipeline that builds the
binaries, signs the checksums with keyless [cosign](https://docs.sigstore.dev/),
generates [SLSA](https://slsa.dev) build provenance, and attaches per-archive SBOMs.
See [Verifying releases](README.md#verifying-releases) for how consumers verify them.

## Versioning

The project follows [Semantic Versioning](https://semver.org/spec/v2.0.0.html), and
every user-visible change is recorded in [CHANGELOG.md](CHANGELOG.md).

## Go module immutability — never re-tag a published version

Go's module ecosystem treats a published version as **immutable**. Once a tag has been
fetched, `proxy.golang.org` and the checksum database (`sum.golang.org`) record its
content hash permanently, and the publisher cannot revoke or change it.

Therefore:

- **Never move or re-tag a version that has been published** (i.e. pushed and observed
  by the module proxy). Re-pointing an existing tag to different content poisons the
  coordinate: consumers fetching with `GOPROXY=direct` get a checksum-mismatch security
  error, and proxy users get stale content. This is **not recoverable**.
- **To fix a released version, cut a new one.** Land the fix on `main` and tag the next
  version. If the goal is to steer consumers away from a bad release, add a
  [`retract`](https://go.dev/ref/mod#go-mod-file-retract) directive in `go.mod` — note a
  retraction only takes effect once it ships in a **higher** version than the one being
  retracted, so it too requires a new tag.
- **A tag is immutable even if its release failed.** If a tag was pushed but produced no
  usable artifacts, do not delete and re-push it — the proxy may already have recorded
  it. Cut the next patch version instead.

## Cutting a release

1. Ensure `main` is green and `CHANGELOG.md` has an entry for the new version.
2. Create an annotated, **signed** tag on the release commit: `git tag -s vX.Y.Z -m '…'`.
3. Verify the tag locally before pushing: `git tag -v vX.Y.Z` (good signature) and confirm
   it points at the intended commit.
4. Push the tag. The release workflow runs automatically and publishes the signed
   artifacts, provenance, and SBOMs.
5. After publishing, verify the released artifacts end-to-end (checksums, cosign
   signature with the identity flags, SLSA provenance) before announcing the release.

If a release pipeline fails, **fix forward to the next version** — do not re-push the
same tag to changed content (see *Go module immutability* above).
