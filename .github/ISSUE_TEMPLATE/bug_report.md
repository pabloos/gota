---
name: Bug report
about: Something gota generates is wrong, or it fails on valid input
title: ""
labels: bug
---

**What happened**

A clear description of the incorrect behavior — what gota produced vs.
what you expected.

**Minimal reproduction**

The smallest Go source snippet (route + handler, and any `gota:` comment
involved) that reproduces it. If it depends on being part of a larger
package/module, a minimal `go.mod` + file layout helps.

**Command run**

```sh
go run ./cmd/gota --dir ... --out ...
```

**gota version / commit**

The commit hash (`git rev-parse HEAD`) or module version
(`go list -m github.com/pabloos/gota`) you're using. There's no
`--version` flag yet.

**Generated output vs. expected output**

Paste the relevant part of the generated spec, and what you expected
instead.
