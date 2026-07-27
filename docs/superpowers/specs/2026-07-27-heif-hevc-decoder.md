# HEIF/HEIC decoding — pure-Go HEVC intra decoder (2026-07-27)

## What shipped

`pkg/heif` (container) + `pkg/heif/hevc` (codec): a from-scratch, MIT, pure-Go
decoder for HEVC-coded still HEIF images, integrated across the toolkit
(PR stack #79–#87). No viable pure-Go decoder existed to import; the only
candidate (rainliu/GoHM, 2013) has no license.

## Scope

Intra-only (no inter prediction/MC — stills never need it), 4:2:0, 8/10-bit,
Main/Main 10/Main Still Picture toolsets. Support gates on the actual SPS/PPS
feature switches, NOT the profile label: x265 labels all-intra streams RExt.
Implemented: CABAC (spec-exact engine + I-slice context tables), CTU quadtree,
NxN/PCM/lossless CUs, MPM, all intra modes with reference filtering/strong
smoothing, residual coding (sign hiding, mode-dependent scans, Rice levels),
scaling lists, per-QG QP, WPP, tiles, deblocking, SAO, conformance cropping.
Container: grids (iPhone tiling), auxiliary alpha, irot/imir/clap, nclx colour
(601/709/2020, limited/full), idat/multi-extent iloc, essential-property rule.
Rejected with typed errors: P/B slices, range extensions, non-4:2:0, >10-bit,
msf1 sequences, AVIF, protected items, external data references.

## Correctness method

HEVC decoding is normative, so the bar is bit-exactness: 42 committed payloads
(x265 + kvazaar, all QP/size/depth/tool variants, `testdata/gen/heif/payloads`
with recorded command lines) must reproduce ffmpeg's reference YUV exactly.
The divergence-hunting harness (`pkg/heif/hevc/debug_test.go`) diffs per-bin
CABAC engine state against a trace-instrumented reference decoder; it found
two single-entry table typos and the MPM z-scan availability rule. Real files
from three producers are committed: Apple sips, libheif heif-enc (both under
`pkg/heif/testdata/` with provenance) and kvazaar tile streams.

## Concurrency

One worker option threads through every level: WPP rows decode on a wavefront
(2-CTB lag; the scheduler mutex orders cross-row reads), tiles decode on
independent goroutines, loop filters/SAO split into disjoint bands, and grid
tiles fan out across a bounded pool. Parallel output is byte-identical to
`Workers=1` (determinism tests, race detector in CI). Measured: 512×512 WPP
2.5×, 2×2 tiles 2.6×. Follow-up (only if benchmarks ever demand): parse-ahead
+ CTB-diagonal reconstruction wavefront for no-WPP single-substream streams.

## Integration

`decodeImageBytes` case + registration lights up HTML/EPUB/PPTX `<img>`;
`FormatHEIC` (input-only) across the format tables; ISOBMFF ftyp-brand
detection with a tight allowlist (mp4/mov/avif stay unknown); OpenImageBytes;
DOCX/PPTX/RTF/EPUB writers transcode HEIC to PNG via `pkg/render/imageconv`
instead of degrading to alt text; htmldoc showcase §08 renders a HEIC beside
its PNG twin.

## Patents

HEVC is patent-encumbered (Access Advance/MPEG LA pools). Shipping MIT decoder
*code* is normal open-source practice (libde265, ffmpeg); deployers are
responsible for patent licensing in commercial products.
