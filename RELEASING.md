# Releasing

## Goal

Publish new versions to Go consumers and `pkg.go.dev` without changing behavior for users pinned to older versions.

## Rules

1. Never move or recreate an existing Git tag.
2. Use semantic version tags:
   - `v0.0.2` or `v0.1.0` while the API is still stabilizing
   - `v1.2.3` after the package is declared stable
   - `v1.2.3-rc.1` for pre-releases
3. The current public release is `v0.0.1`, which means the package is still pre-stable.
4. Do not claim `v1` compatibility guarantees until the exported API and behavior are intentionally stable.
5. Do not ship breaking API changes on the `v1` module path.
6. If a breaking change is required after `v1.0.0`, create a new major module path such as:

```go
module github.com/kzzan/s3kit/v2
```

7. Maintain the previous stable major line on a separate branch if you still need patches for it.

## Stable Release Checklist

1. Run:

```bash
go test ./...
go vet ./...
go mod tidy
```

2. Review public API changes.
3. Update release notes or changelog if needed.
4. Create and push a tag:

```bash
git tag v0.0.2
git push origin v0.0.2
```

5. Confirm the GitHub Actions `Release` workflow passed.
6. Confirm the version appears on `pkg.go.dev`.

## When to Cut v1.0.0

Move from `v0` to `v1` only when all of the following are true:

1. The exported API surface is intentional rather than exploratory.
2. Core request and error behavior is covered by tests.
3. README examples match the supported usage model.
4. You are prepared to preserve backward compatibility on the `v1` path.

## Major Version Checklist

1. Create a new major version branch or development line.
2. If you are moving from `v0` to `v1`, keep the module path as `github.com/kzzan/s3kit` and tag `v1.0.0`.
3. If you are moving from `v1` to `v2+`, change `go.mod` to the new major module path.
4. Update imports in examples and tests to the new path when releasing `v2+`.
5. Tag the first breaking release, for example `v2.0.0`.
6. Keep the older stable line on a maintenance branch if it is still supported.
