# Releasing

Push a version tag. The Release workflow runs GoReleaser, which builds the
archives and Linux packages, signs `checksums.txt`, and publishes the
GitHub release. It takes a few minutes.

```console
git tag -a v0.3.0 -m "codexctl v0.3.0"
git push origin v0.3.0
```

Wait for CI to pass on the commit before tagging it.

## Signing key

`codexctl update` only installs releases signed with the Ed25519 key built
into the binary.

| Where | What |
| --- | --- |
| `CODEXCTL_SIGNING_KEY` repository secret | Private key |
| `internal/update/sign.go` | Public key |

To create a key:

```console
go run ./tools/sign keygen -out signing.key
gh secret set CODEXCTL_SIGNING_KEY < signing.key
```

Then paste the printed public key into `internal/update/sign.go`.

To rotate the key, publish one release signed with the old key that embeds
the new one. Binaries trust only the key they were built with.

## Testing a release locally

```console
goreleaser check
CODEXCTL_SIGNING_KEY=$(cat signing.key) goreleaser release --snapshot --clean
go run ./tools/sign verify dist/checksums.txt
```

Snapshot builds in CI skip signing.
