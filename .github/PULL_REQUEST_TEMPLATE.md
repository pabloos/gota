**What this changes and why**

<!-- The "why" matters more than the "what" here — the diff already shows what changed. -->

**Checklist**

- [ ] `gofmt -l .` is clean
- [ ] `go vet ./...` passes
- [ ] `go test ./...` passes
- [ ] New behavior has test coverage (fixtures under `testdata/`, not embedded strings — see [CONTRIBUTING.md](../CONTRIBUTING.md#code-conventions))
- [ ] README updated, if this changes documented behavior
