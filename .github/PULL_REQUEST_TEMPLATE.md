<!-- Thanks for contributing! Keep PRs small and focused. -->

## What and why

<!-- What does this change do, and why is it needed? Link any issue. -->

Closes #

## Checklist

- [ ] `go build ./...`, `go vet ./...`, `go test -race ./...` pass locally
- [ ] `gofmt -l .` prints nothing
- [ ] `golangci-lint run` is clean
- [ ] No hardcoded site-specific value (zones, plans, images, hosts, caps — all
      configuration); no GE/organization-specific references (PRIME INVARIANT in `AGENTS.md`)
- [ ] No secret in code, tests, config, fixtures, or git history; `config.toml` stays git-ignored
- [ ] Tests ship with the code; new code paths are covered
- [ ] Commits are conventional and signed

## Notes for reviewers

<!-- Anything reviewers should pay special attention to. -->
