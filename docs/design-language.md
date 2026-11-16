# Aetheris Design Language — *Quiet Sculpture*

Anchored directly in the **Aetheris** and **nest** brand artworks: dimensional objects of brushed silver, frosted glass, and warm coral-to-apricot facets resting in quiet porcelain space — with minimal text and physical 1:1 spring motion.

---

## 1. Visual Principles

1. **Form Before Labels** — Folders, media, documents, and sync states read through silhouette, material, and light rather than text badges (`PDF`, `LAN`, `PC+NAS`) or table headers.
2. **Framed Porcelain Stage** — A warm `#F7EBE8` seashell surround frames a luminous `#FCF9F7` porcelain sheet (`#141418` → `#1E1E24` in dark mode), echoing the vertical framing of both brand renders.
3. **One Quiet Voice per Screen** — One sculptural title (`Canela Deck`), icon-only chrome controls, clean object names (`Lexend`), and a closed-by-default Inspector (`i`).
4. **Physical Continuity** — Every gesture responds on `pointerdown`, tracks 1:1 with grab offset, stretches via `rubberband`, and settles on analytical springs (`dampingRatio` + `response`).

---

## 2. Color & Material System

### Brand Core & Surface Ramp

| Token | Light (*Porcelain*) | Dark (*Obsidian*) | Role |
| :--- | :--- | :--- | :--- |
| `--coral` | `#E54B4B` | `#E54B4B` | Primary warmth, offline pin bead, active accent |
| `--apricot` | `#FFA987` | `#FFA987` | Facet highlight, gradient pair with `--coral` |
| `--seashell` | `#F7EBE8` | `#131317` | Outer ambient frame |
| `--stage` | `#FFFFFF → #FCF9F7` | `#222229 → #1E1E24` | Main workspace sheet (`26px` radius) |
| `--ink` | `#1E1E24` (`16.0:1`) | `#F7EBE8` (`14.2:1`) | Primary titles & filenames (WCAG AAA) |
| `--ink-2` | `#444140` (`9.7:1`) | `#C8BFBC` (`8.3:1`) | Interactive icons & active controls |
| `--ink-3` | `#7A7270` (`4.7:1`) | `#8E8684` (`4.8:1`) | Secondary facts & muted sizes (WCAG AA) |
| `--glass-fill` | `rgba(255,253,252,0.74)` | `rgba(34,34,42,0.76)` | Floating capsules, popovers (`blur(22px)`) |

> [!IMPORTANT]
> **Color Discipline:** Zero rainbow SaaS file-type colors. Every surface, icon, and state stays strictly inside Porcelain, Obsidian, Brushed Silver, Coral (`#E54B4B`), and Apricot (`#FFA987`).

---

## 3. Typography

Zero `Segoe UI` and zero monospace UI chrome.

| Role | Typeface | Weight | Size / Tracking | Notes |
| :--- | :--- | :--- | :--- | :--- |
| **Display Title** | `Canela Deck` | `300 Light` | `2.35rem` / `-0.015em` | Section & folder titles (`Files`, `Recent`, `Synced`). Paired with `unicode-range` fallback to `Lexend` for extended glyphs. |
| **Modal / Sheet Title** | `Canela Deck` | `300 Light` | `1.55–1.75rem` / `-0.015em` | Quick Look & Inspector object title. |
| **Object Name & Nav** | `Lexend` | `300–400` | `0.84–0.90rem` / `-0.011em` | Open circular apertures matching the `Aetheris` / `nest` wordmarks. |
| **Quiet Numerals** | `Lexend` | `300 Light` | `0.75–0.78rem` / `tabular-nums` | Storage caption (`5.5 TB`) and list file sizes. |

*Web deployment note:* Self-host subsetted `.woff2` files in `web/public/fonts/` so production builds render identically across Windows, macOS, iOS, and Linux without external font CDNs.

---

## 4. Silence Rules (Text Reduction)

- **Left Rail:** Sculpted Aetheris mark + icon-only `+` button + 4 single-word destinations (`Files`, `Recent`, `Pinned`, `Trash`) + hairline storage bar (`5.5 TB`). Zero server URLs, zero item-count pills.
- **Header:** Current title (with SVG chevron `›` for sub-folders) + 3 icon-only circular buttons (`Search`, `Grid/List`, `Details`). Zero sub-headers, zero filter strips, zero column header bars.
- **Gallery Tiles (Default):** Dimensional **Nest Folder** or matted visual preview + clean name + `6px` coral bead *only* when pinned offline. Extensions and timestamps stay tucked inside the optional Inspector.
- **List Rows:** Miniature visual + name + muted size + interactive `7px` pin ring.
- **Upload / Hydration Feedback:** Floating corner pill with a circular stroke ring and count (`1`) — auto-dismissing without toast prose.
- **Windows Companion (`Aetheris.exe`):** Standalone `318px` window with sculpted emblem, single `Canela Deck` state title (`Synced` / `Syncing` / `Offline`), hairline progress bar, `Open Aetheris` button, and two switches.

---

## 5. Fluid Motion Specification

| Interaction | Physics | Parameters |
| :--- | :--- | :--- |
| **Nav & Mode Pills** | Critically damped `Spring` | `dampingRatio: 1.0`, `response: 0.26s` |
| **Quick Look Sheet** | Origin-anchored symmetric `Spring` | `dampingRatio: 0.94`, `response: 0.32s` from/to clicked tile rect |
| **Drag & Flick** | 1:1 pointer capture + momentum | Grab-offset preserved; `rubberband(offset, dim, 0.55)`; `project(v, 0.996)` |
| **Accessibility** | `@media (prefers-reduced-motion)` & `(prefers-reduced-transparency)` | Instant `.snap()` transitions and opaque `--bg-card` surfaces |

---

## 6. Live App Rollout Plan (Pending Sign-Off)

1. **`web/src/tokens.css` & `web/public/fonts/`** — Introduce Porcelain/Obsidian tokens, `@font-face` declarations, and shared analytical [`spring.ts`](../web/src/lib/spring.ts) helpers.
2. **`web/src/lib/Logo.svelte` & Object Visuals** — Replace flat icons with the sculpted Aetheris emblem, Nest Folder SVG, and wordless file miniatures.
3. **[`Shell.svelte`](../web/src/Shell.svelte), [`Files.svelte`](../web/src/Files.svelte), [`Preview.svelte`](../web/src/Preview.svelte)** — Apply the framed porcelain stage, icon-only header, sculptural Gallery Grid + Whisper List, and origin-anchored Quick Look.
4. **[`web/src/desktop/Home.svelte`](../web/src/desktop/Home.svelte) & [`Pair.svelte`](../web/src/desktop/Pair.svelte)** — Simplify the Windows companion to the quiet standalone plinth layout.
