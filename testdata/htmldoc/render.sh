#!/usr/bin/env bash
#
# render.sh — render the HTML showcase in this directory to one PNG per page,
# exactly the way a user would: build the doctaculous CLI, then run
# `doctaculous rasterize` against index.html. Prints the directory the images
# were written to.
#
# Usage:
#   ./render.sh [flags]
#
# Flags:
#   --out DIR         write PNGs to DIR (created if needed; default: a temp dir)
#   --dpi N           output resolution in DPI (default: 150)
#   --page-size SIZE  "letter" to paginate (default) or "tall" for one tall page
#   -h, --help        show this help
#
# Examples:
#   ./render.sh                          # temp dir, Letter pages at 150 DPI
#   ./render.sh --out ./out              # ./out
#   ./render.sh --out ./out --dpi 300    # ./out at 300 DPI
#   ./render.sh --page-size tall         # one tall page

set -euo pipefail

usage() {
  sed -n '2,21p' "${BASH_SOURCE[0]}" | sed 's/^# \{0,1\}//'
}

# Defaults.
out_dir=""
dpi=150
page_size=letter

# Parse flags, supporting both "--flag value" and "--flag=value".
while [[ $# -gt 0 ]]; do
  case "$1" in
    --out) out_dir="$2"; shift 2 ;;
    --out=*) out_dir="${1#*=}"; shift ;;
    --dpi) dpi="$2"; shift 2 ;;
    --dpi=*) dpi="${1#*=}"; shift ;;
    --page-size) page_size="$2"; shift 2 ;;
    --page-size=*) page_size="${1#*=}"; shift ;;
    -h|--help) usage; exit 0 ;;
    *) echo "render.sh: unknown flag '$1' (try --help)" >&2; exit 2 ;;
  esac
done

if [[ "$page_size" != "letter" && "$page_size" != "tall" ]]; then
  echo "render.sh: --page-size must be 'letter' or 'tall', got '$page_size'" >&2
  exit 2
fi

# Resolve this script's directory (the doc to render) and the repo root, so the
# script works regardless of the current working directory.
script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd "$script_dir" && git rev-parse --show-toplevel)"
input="$script_dir/index.html"

# Output directory: --out, or a fresh temp dir. Resolve to an absolute path so the
# printed location is unambiguous.
if [[ -n "$out_dir" ]]; then
  mkdir -p "$out_dir"
else
  out_dir="$(mktemp -d "${TMPDIR:-/tmp}/htmldoc-render.XXXXXX")"
fi
out_dir="$(cd "$out_dir" && pwd)"

# The CLI --page-size flag is omitted (one tall page) unless rendering Letter pages.
size_flag=()
if [[ "$page_size" == "letter" ]]; then
  size_flag=(--page-size letter)
fi

echo "building doctaculous CLI..." >&2
cli="$(mktemp "${TMPDIR:-/tmp}/doctaculous.XXXXXX")"
trap 'rm -f "$cli"' EXIT
( cd "$repo_root" && go build -o "$cli" ./cmd/doctaculous )

echo "rendering $input ..." >&2
"$cli" rasterize "$input" \
  "${size_flag[@]}" \
  --pages all \
  --dpi "$dpi" \
  --out "$out_dir/page-%d.png"

# The directory path is the ONLY thing on stdout, so it is cleanly captured by
# `dir=$(./render.sh ...)`; the human-readable label goes to stderr.
echo "images written to:" >&2
echo "$out_dir"
