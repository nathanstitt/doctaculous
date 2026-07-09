# Vendored: xiaoqidun/jbig2

A pure-Go, Apache-2.0 JBIG2 (ITU T.88 / ISO-IEC 14492) decoder, vendored into this
repository so the build never depends on the upstream remaining available.

- **Source:** https://github.com/xiaoqidun/jbig2
- **Version:** v0.0.0-20260709020415-1bb076bd002c (commit 1bb076bd002c)
- **License:** Apache-2.0 (see LICENSE and NOTICE in this directory)

We vendor rather than take a `go get` dependency because the upstream is a new,
solo-authored project; vendoring the Apache-2.0 source (which the license permits)
insulates our build and lets us pin/fix it in-tree. Its only external dependency is
`golang.org/x/image`, which the toolkit already uses, so no new module enters `go.mod`.

To update: re-copy the upstream `.go` + `LICENSE` + `NOTICE`, update the version above,
and run the package smoke test (`go test ./pkg/pdf/filter/jbig2/`).
