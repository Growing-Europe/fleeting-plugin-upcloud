# Contributing

Thanks for your interest in `fleeting-plugin-upcloud`!

## Ground rule: provider-generic, no downstream coupling

This plugin is a **generic** GitLab Runner fleeting provider for UpCloud. It must be usable by anyone
running GitLab Runner on UpCloud, out of the box. Please keep it that way:

- **No organization-, environment-, or deployment-specific assumptions** in the code. Everything
  site-specific — zones, plans, images, networks, caps, labels, credentials — is **configuration**,
  passed via the plugin config or credentials, never hardcoded.
- **UpCloud specifics only.** No coupling to any particular downstream user, internal tooling, or
  private infrastructure.
- Sensible defaults; fail-closed on missing credentials; never log secrets.

PRs that introduce vendor/deployment coupling will be asked to move it behind configuration.

## Development

```sh
go build ./...
go vet ./...
go test ./...
```

- Unit tests must not require network access or credentials.
- The optional integration test runs against a real UpCloud account only when credentials are present
  in the environment; it is skipped otherwise (and in public CI).

## Releasing

Tagged releases (`vX.Y.Z`) publish built binaries. Follow [semver](https://semver.org); keep a CHANGELOG.

## License

By contributing, you agree your contributions are licensed under the [MIT License](LICENSE).
