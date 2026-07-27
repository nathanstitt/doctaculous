# pkg/heif test fixtures

`sips-quad-64x48.heic` — a real HEVC-coded HEIC produced by Apple's `sips`
(VideoToolbox encoder) on macOS from a self-authored 64×48 four-quadrant PNG
(generated in-repo style, no third-party content):

```
sips -s format heic quad64.png --out sips-quad-64x48.heic
```

Committed because Go has no HEIF encoder; the container structure exercised
by tests (heic brand, meta/iinf/iloc/iprp, hvcC) is real-world Apple output.
License: self-authored, same license as the repository (MIT).

All other containers used by tests are built deterministically in memory by
`testdata/gen/heif`.

`heifenc-noise-96x80.heic` — the same kind of self-authored source (the
96x80 gradient+noise pattern from `testdata/gen/heif/payloads/gen_sources.go`)
encoded with libheif's `heif-enc -q 60`, exercising libheif's container
boxing on top of an x265 bitstream:

```
heif-enc -q 60 -o heifenc-noise-96x80.heic src-96x80.png
```

License: self-authored, same license as the repository (MIT).
