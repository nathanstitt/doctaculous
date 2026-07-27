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
