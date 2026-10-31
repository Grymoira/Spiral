# Licensing

The original Spiral source code and documentation in this repository are distributed under the **MIT License**. See [`../LICENSE`](../LICENSE) for the complete license, including the required copyright and permission notice.

This does **not** relicense third-party software. The optional desktop application uses Qt under its own licensing terms (depending on modules and distribution choice, e.g. LGPL/GPL or commercial licensing). Before shipping Qt runtime libraries, verify every shipped module, license notices, dynamic-linking and relinking requirements, and deployment obligations. System-provided audio codecs and other libraries may have additional terms. Neither music catalog contents nor third-party audio recordings are included in Spiral's MIT grant.

The server implementation uses only Go's standard library. No third-party project source or media assets were copied as part of this milestone.

## Adobe Source Code Pro desktop UI font (Issue #74)

The Spiral desktop UI embeds **unmodified** Adobe **Source Code Pro** TTF
Regular, Medium, Semibold and Bold from the official project:
https://github.com/adobe-fonts/source-code-pro

- Family: **Source Code Pro** (distinct from Source Sans Pro).
- Upstream version: **2.42.0**, source revision
  `803b7e23ec97ae58b6232ea76519a76d428ba268` (official `release` branch at integration).
- License: **SIL Open Font License 1.1 (OFL-1.1)**; © 2023 Adobe,
  Reserved Font Name **Source**. This font license is *not* MIT.
- Full, unmodified original license: `client/assets/fonts/LICENSE.md`.
  The Windows portable package includes
  `LICENSES/Source-Code-Pro-OFL-1.1.txt`.
- Upstream file provenance (original Git blob SHA-1):
  - `SourceCodePro-Regular.ttf`: `10c73d7d901cf0dfa8ba03e45e3271d466291520`
  - `SourceCodePro-Medium.ttf`: `492841d1b0227ecbc727f77bf48d1b69a729b5ac`
  - `SourceCodePro-Semibold.ttf`: `6109ddd4af9f738e50939b2a502d76a967615b2c`
  - `SourceCodePro-Bold.ttf`: `a032f5928a96b420267c95916da0a5958960915a`

Qt loads these embedded resources through `QFontDatabase`, then applies a
shared QFont to the root ApplicationWindow and controls. The font does not
supply complete CJK or emoji coverage; the UI declares Windows Microsoft
YaHei/JhengHei/Yu Gothic and cross-platform Noto CJK fallbacks, then relies
on Qt/OS font substitution for remaining Unicode characters. Correct glyph
rendering, 100%/150% DPI and high-contrast appearance require native GUI
acceptance; CI source/Qt startup checks are not visual evidence.
