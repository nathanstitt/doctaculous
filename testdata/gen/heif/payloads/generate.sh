#!/bin/sh
# Regenerates the committed .hevc payloads and .yuv.gz reference dumps from
# the deterministic source PNGs (gen_sources.go). Run manually on a machine
# with ffmpeg+libx265; CI never runs this — outputs are committed.
#
#   go run gen_sources.go && sh generate.sh
#
# Reference dumps are ffmpeg's own decode of each payload (HEVC decoding is
# normative, so any conformant decoder yields identical YUV).
set -e

enc() { # enc <src.png> <out base> <pix_fmt> <x265 params>
  ffmpeg -v error -y -i "$1" -frames:v 1 -c:v libx265 -pix_fmt "$3" \
    -x265-params "log-level=none:$4" -f hevc "$2.hevc"
  ffmpeg -v error -y -i "$2.hevc" -f rawvideo "$2.yuv"
  gzip -9 -n -f "$2.yuv"
}

for qp in 12 27 40; do
  for sz in 16x16 64x64 96x80; do
    enc "src-$sz.png" "x265-$sz-qp$qp"        yuv420p    "qp=$qp:keyint=1"
    enc "src-$sz.png" "x265-$sz-qp$qp-nofilt" yuv420p    "qp=$qp:keyint=1:no-sao=1:no-deblock=1"
  done
done
for sz in 16x16 64x64 96x80; do
  enc "src-$sz.png" "x265-$sz-qp27-10bit"        yuv420p10le "qp=27:keyint=1"
  enc "src-$sz.png" "x265-$sz-qp27-10bit-nofilt" yuv420p10le "qp=27:keyint=1:no-sao=1:no-deblock=1"
done

# 30x22: not multiples of the 8-px min CU, so the SPS carries a conformance
# window (coded 32x24, cropped on output).
enc src-30x22.png x265-30x22-qp27        yuv420p "qp=27:keyint=1"
enc src-30x22.png x265-30x22-qp27-nofilt yuv420p "qp=27:keyint=1:no-sao=1:no-deblock=1"

# 512x512: one default and one pre-filter payload at qp27 (kept single-QP for size).
enc src-512x512.png x265-512x512-qp27        yuv420p "qp=27:keyint=1"
enc src-512x512.png x265-512x512-qp27-nofilt yuv420p "qp=27:keyint=1:no-sao=1:no-deblock=1"

# Toolset variants at 64x64 qp27 (each isolates one decoder feature):
enc src-64x64.png x265-64x64-qp27-nowpp    yuv420p "qp=27:keyint=1:no-wpp=1"
enc src-64x64.png x265-64x64-qp27-tskip    yuv420p "qp=27:keyint=1:tskip=1"
enc src-64x64.png x265-64x64-qp27-lossless yuv420p "lossless=1:keyint=1"
enc src-64x64.png x265-64x64-qp27-scaling  yuv420p "qp=27:keyint=1:scaling-list=default"
enc src-64x64.png x265-64x64-qp27-nosdh    yuv420p "qp=27:keyint=1:no-signhide=1"
# ctu16: 4 CTU rows in 64 px, so WPP substreams/entry points are exercised.
enc src-64x64.png x265-64x64-qp27-ctu16    yuv420p "qp=27:keyint=1:ctu=16"

# Pre-filter (no SAO/deblock) variants of each coding tool, so the
# reconstruction milestone can verify them bit-exact before loop filters land:
enc src-64x64.png x265-64x64-qp27-tskip-nofilt    yuv420p "qp=27:keyint=1:tskip=1:no-sao=1:no-deblock=1"
enc src-64x64.png x265-64x64-qp27-lossless-nofilt yuv420p "lossless=1:keyint=1:no-sao=1:no-deblock=1"
enc src-64x64.png x265-64x64-qp27-scaling-nofilt  yuv420p "qp=27:keyint=1:scaling-list=default:no-sao=1:no-deblock=1"
enc src-64x64.png x265-64x64-qp27-nosdh-nofilt    yuv420p "qp=27:keyint=1:no-signhide=1:no-sao=1:no-deblock=1"
enc src-64x64.png x265-64x64-qp27-ctu16-nofilt    yuv420p "qp=27:keyint=1:ctu=16:no-sao=1:no-deblock=1"
enc src-96x80.png x265-96x80-qp27-ctu16-nofilt    yuv420p "qp=27:keyint=1:ctu=16:no-sao=1:no-deblock=1"

# kvazaar tile fixtures (x265 cannot emit tiles): all-intra, SAO+deblock on.
kvz() { # kvz <src.png> <WxH> <out base> <extra kvazaar args...>
  src=$1; res=$2; base=$3; shift 3
  ffmpeg -v error -y -i "$src" -f rawvideo -pix_fmt yuv420p "$base-in.yuv"
  kvazaar -i "$base-in.yuv" --input-res "$res" -q 27 -p 1 --frames 1 "$@" -o "$base.hevc" >/dev/null 2>&1
  ffmpeg -v error -y -i "$base.hevc" -f rawvideo "$base.yuv"
  gzip -9 -n -f "$base.yuv"; rm -f "$base-in.yuv"
}
kvz src-96x80.png   96x80   kvazaar-96x80-qp27-tiles2x2   --tiles 2x2 --no-wpp
kvz src-512x512.png 512x512 kvazaar-512x512-qp27-tiles2x2 --tiles 2x2 --no-wpp

rm -f src-*.png
