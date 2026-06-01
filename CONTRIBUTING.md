# Contributing

## Development Flow

1. Create a focused branch from `main`.
2. Keep API changes backward compatible unless the change is explicitly planned for a major release.
3. Run the local quality gates before opening a pull request:

```bash
go test ./...
go vet ./...
go mod tidy
golangci-lint run
```

## Pull Request Standards

- Add or update tests for every behavior change.
- Keep public APIs documented so `pkg.go.dev` stays complete.
- Prefer standard library features before adding new dependencies.
- Upgrade Go modules conservatively:
  - patch updates are the default
  - minor or major upgrades need changelog review
  - pre-v1 modules need manual compatibility review even for minor updates

## Commit and Release Notes

- Use clear commit messages that explain the user-visible change.
- Call out breaking changes, dependency upgrades, and security fixes in the pull request description.
- Never retag an existing release. Published tags must be immutable so older `go` consumers keep reproducible builds.

## Versioning and Releases

- The currently published line is `v0.x`, starting at `v0.0.1`.
- On `v0`:
  - use `v0.0.z` for fixes and narrowly scoped improvements
  - use `v0.y.0` for broader API iteration before stability is declared
  - do not break users casually just because `v0` allows it
- Declare `v1.0.0` only when the public API and behavior are ready for compatibility guarantees.
- After `v1.0.0`, bug fixes go out as patch tags such as `v1.0.1`, and backward-compatible feature additions go out as minor tags such as `v1.1.0`.
- If a post-`v1` change would break imports or public behavior, publish it as a new major module path, for example `github.com/kzzan/s3kit/v2`.
- Keep maintenance for older stable lines on dedicated branches such as `release/v1` if a future `v2` line is opened.

## Reporting Issues

- Use GitHub issues for bugs and feature requests.
- Use GitHub Security Advisories for vulnerabilities. See [SECURITY.md](SECURITY.md).
