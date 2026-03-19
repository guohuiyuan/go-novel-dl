# Tests

All Go tests are organized conceptually under `tests/`.

The project still keeps package-specific `_test.go` files next to the code they verify because Go's testing toolchain requires tests to live in the package directory when they need package-private access.

Use these commands:

```bash
go test ./...
go test ./internal/site
go test ./internal/exporter
```
