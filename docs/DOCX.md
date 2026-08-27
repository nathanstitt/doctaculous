# DOCX — status and open work

Shipped features are inventoried in [../FEATURES.md](../FEATURES.md); this file holds what is
NOT done.

- **DOCX fonts** — de-obfuscate embedded `word/fonts/*` (improves bold/italic fidelity), and give
  DOCX the system-font default (it currently resolves bundled-only; the `OSFontProvider` seam exists,
  it is just not installed in `docxDocument`).
