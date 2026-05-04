---
title: imgcli Docs
slug: /
description: Documentation for imgcli.
---

# imgcli Docs

`imgcli` is a prototype CLI for building disk image artifacts from Git-tracked
CUE configuration and, later, publishing those artifacts to `imgsrv`.

The project is intentionally early. Start with the
[initial design](./design.md), which captures the current product boundary,
prototype scope, configuration direction, and planned command set.

## Current Status

- No installable CLI binary has been published yet.
- The first prototype target is a local-only IncusOS artifact path.
- Publishing will target `imgsrv` once its API is stable enough to drive
  against.
