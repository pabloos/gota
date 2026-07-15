---
name: Feature request
about: Propose new gota behavior — a router plugin, an inference case, a CLI flag...
title: ""
labels: enhancement
---

**What's missing**

What gota can't do today that you need. If it's a static-analysis case
it doesn't yet handle, a small Go snippet showing the pattern helps a
lot.

**Proposed behavior**

What you'd expect gota to infer, or what the new surface (flag, plugin,
comment field) should look like. If this touches `gota:` comment syntax,
remember the project's core rule: the comment *is* OpenAPI vocabulary,
not an intermediate DSL — new fields should map to something that
already exists in the OpenAPI spec (including Specification Extensions,
`x-*`) rather than inventing new syntax.

**Alternatives considered**

Anything you already tried working around, and why it wasn't enough.
