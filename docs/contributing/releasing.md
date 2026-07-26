# Releasing

Releases are driven by **pull request labels**. There is no manual tagging step.

## Cutting a release

Add one of these labels to a pull request before merging it to `main`:

| Label | Effect |
|---|---|
| `bump:major` | `v0.12.0` → `v1.0.0` |
| `bump:minor` | `v0.12.0` → `v0.13.0` |
| `bump:patch` | `v0.12.0` → `v0.12.1` |

On merge, the release workflow creates the new tag and updates the corresponding major and
minor tags — releasing `v1.2.3` also moves `v1` and `v1.2`.

A pull request merged **without** one of these labels does not produce a release. Labelling
a pull request also posts a status comment showing which version it would produce, so the
effect is visible before merge.

## What a tag produces

Pushing a `v*` tag builds and publishes the Docker images — one per supported PHP version,
for `linux/amd64` and `linux/arm64`:

```text
ghcr.io/drupdater/drupdater-php8.2
ghcr.io/drupdater/drupdater-php8.3
ghcr.io/drupdater/drupdater-php8.4
ghcr.io/drupdater/drupdater-php8.5
```

Each gets the full version, the major and minor tags, and `latest`. Build provenance
attestation is pushed alongside. See [Docker images](../reference/docker-images.md).

## Choosing a bump

Drupdater is pre-1.0, so `v0.x` does not carry the usual semver guarantees — but the
distinction still matters to anyone pinning a minor tag.

Use **minor** for anything user-visible:

- A new or removed CLI flag, or a change to what one does
- A new `.drupdater.yaml` key, or a change to a default
- A new addon, or a change to which addons run by default
- Any change to the [run report](../reference/run-report.md)

Use **patch** for fixes and internal work that leaves all of the above unchanged.

### If the report schema changes

`schema_version` is a separate contract from the release version. Adding a field does
**not** bump it — consumers must ignore unknown fields. Removing or renaming one does, and
that is a minor bump at minimum.

## Adding a PHP version

When a new PHP release arrives, or an old one reaches end of life:

1. Update the matrix in `.github/workflows/docker-image.yml`.
2. Update the published list in
   [`docs/reference/docker-images.md`](../reference/docker-images.md).

The `Dockerfile` takes `PHP_VERSION` as a build argument, so no change is needed there.

## Known gap: images report `dev`

The `Makefile` injects the version via `-ldflags`, but the `Dockerfile`'s build stage does
not. Binaries inside published images therefore report `drupdater_version: "dev"` in the
[run report](../reference/run-report.md).

This is a real bug rather than a design decision. Until it is fixed, the image tag is the
only reliable way to know which version is running.
