# pkg/internal/webp test fixtures

The three still images are copied verbatim from the `golang.org/x/image`
module's own `testdata/` (BSD-3-Clause, the same license as the decoder this
package wraps — see that module's LICENSE). They are committed because Go has
no WebP encoder in the standard library, so a real-world file cannot be
generated in memory the way the PNG/JPEG fixtures elsewhere in the repo are.

| File | Source name in x/image@v0.43.0 | What it exercises |
| --- | --- | --- |
| `still-lossy.webp` | `video-001.lossy.webp` | plain lossy VP8, no VP8X chunk |
| `still-lossless.webp` | `tux.lossless.webp` | plain lossless VP8L, no VP8X chunk |
| `still-lossy-alpha.webp` | `yellow_rose.lossy-with-alpha.webp` | extended VP8X container with an alpha plane |

The alpha fixture matters beyond alpha itself: it is a genuine VP8X file whose
flags byte is `0x10`, one bit away from the `0x02` animation bit, so it guards
`IsAnimated` against a sloppy mask that would refuse a perfectly good still.

`animated.webp` is synthesized, because upstream ships no animated fixture and
none of the still sources can be made into one without an encoder. It is a
structurally valid animated file — a VP8X chunk with the animation bit set, an
`ANIM` chunk, and two `ANMF` frames each wrapping the real VP8L bitstream
lifted out of `still-lossless.webp`, so the frames are genuinely decodable
rather than a header stub. Built by `testdata/gen/webp/gen_animated.go`;
license: self-authored (MIT), over BSD-3-Clause VP8L payload bytes as above.
