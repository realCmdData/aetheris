# Aetheris Design Language — *Machined Hearth*

**Machined Hearth** is an industrial-archival design language authored specifically for **Aetheris** (`drive.example.com` and the Windows Files On-Demand client). It fuses the effortless cloud-drive utility of **Proton Drive**, **Google Drive**, and **macOS Finder** with the tactile physical authority of **Dieter Rams / Braun**, **Leica**, and **Teenage Engineering**—built strictly from your five brand swatches and the anodized aluminum geometry of the Aetheris / *nest* marks.

> Open `assets/brand/showcase.html` in any browser to test the live workspace, Windows hardware bridge, media totems, and Apple spring physics.

---

## 1. The Thesis: Refusing Generic SaaS Slop

Standard web drives—and AI-generated UI templates—collapse into the same visual clichés: puffy `18px` rounded cards on blurry drop shadows, glowing green status pills (`• Online`), 3-up "Suggested" promo cards, and a rainbow of unrelated blue/purple/green file badges.

**Aetheris is not a multi-tenant SaaS dashboard.** It is a private, cryptographic wire to physical spinning iron in your own home (`NAS-01`, plain ext4 files, BLAKE3 digests, 3-stream ranged TLS/WireGuard). Its interface should feel like a **precision archival instrument**:

1. **Monolith Palette Purity:** Zero borrowed rainbow hues. Every pixel on screen comes strictly from `#F7EBE8` (Seashell), `#FFA987` (Tangerine), `#E54B4B` (Strawberry), `#444140` (Graphite), `#1E1E24` (Shadow Grey), archival rag white (`#FCF9F7`), and the brushed silver (`#CFD2DA` / `#8B8F99`) of the logo's Möbius loop.
2. **Bead-Blasted Chassis & Double-Bevel Joinery:** The outer frame wears a subtle `2.6%` anodized micro-grain (`Seashell` in light, `Matte Carbon` in dark) surrounding a razor-smooth **Archival Sheet** (`12px` outer chamfer) bounded by a `1px` dark seam and a `1px` inner white specular crest—translating the metallic cloud rim directly into window architecture.
3. **Hardware-Honest Telemetry:** Instead of generic rounded gradient tubes, storage (`5.48 / 21.6 TB`) renders on a **24-tick graduated capacity ruler**, and active transfers unfurl a **3-lane parallel chunk tape** reflecting Aetheris's actual 3-worker transfer engine.

---

## 2. Color & Surface System (WCAG AA / AAA Verified)

| Token | Light Mode (*Seashell Paper*) | Dark Mode (*Carbon Darkroom*) | Role & Verified Contrast |
| :--- | :--- | :--- | :--- |
| `--chassis` | `#EFE3DE` → `#F7EBE8` | `#131317` → `#18181D` | Bead-blasted outer spine & Windows DWM caption frame |
| `--sheet` | `#FCF9F7` (Archival Rag) | `#1E1E24` (Shadow Grey) | Inset double-bevel workspace ledger & contact matrix |
| `--plate` | `#FFFFFF` | `#26252C` (Graphite Plate) | Dossier rail, segmented active sled, instrument cards |
| `--well` | `#F3E8E4` | `#18181D` | Recessed command omnibox, ruler track, telemetry tape |
| `--ink-1` | `#1E1E24` (`15.8:1` AAA) | `#F7EBE8` (`14.2:1` AAA) | Primary titles, filenames, primary ledger copy |
| `--ink-2` | `#444140` (`9.6:1` AAA) | `#BFB6B3` (`8.3:1` AAA) | Navigation labels, byte counts, dossier values |
| `--ink-3` | `#6A6362` (`5.4:1` AA) | `#8F8786` (`4.8:1` AA) | Tabular indices (`01`, `02`), timestamps, BLAKE3 hashes |
| `--cta-bg` | `#1E1E24` + `#E54B4B` jewel | `#FFA987` (`8.9:1` vs `#1E1E24`) | Leica-style split shutter primary Upload button |
| `--signal` | `#E54B4B` (Strawberry) | `#FFA987` (Tangerine) | Focus rings, selected-row spine rule (`3px`), Sync Locus core |
| `--row-select` | `rgba(255, 169, 135, 0.26)` | `rgba(255, 169, 135, 0.14)` | Selected ledger row & contact-sheet cell wash |

---

## 3. Physical Media Totems & Sync Locus Vocabulary

Rather than generic outline icons or rainbow SaaS chips, files are depicted as **palette-pure architectural miniatures** paired with a stamped monospace extension tag (`3MF`, `RAW`, `PDF`, `FLAC`):

- **Nested Hearth Folder (`folder`):** Brushed silver top index clip (`#B8BBC2` + specular highlight) above overlapping coral (`#E54B4B`) and translucent apricot (`#FFA987`) diagonal facets derived directly from the *nest* house geometry. Anchored folders embed a crisp white recessed house silhouette.
- **35mm Contact Frame (`raw` / images):** Matte `#1E1E24` film border with sprocket perforations framing a warm duotone horizon thumbnail (or the live `/api/v1/thumb` JPEG once decoded).
- **Isometric Wireframe Plate (`cad` / `.3mf` / archives):** Recessed plate carrying a precision `#1E1E24` + `#E54B4B` isometric projection.
- **Vermilion-Spined Sheet (`pdf` / docs):** Crisp `#FCF9F7` folded page with a `3.6px` `#E54B4B` left binding spine (PDF) or silver corner fold (Markdown/text).
- **Grooved Acoustic Plate (`audio` / video):** Matte `#1E1E24` disc housing concentric silver groove rings and a `#E54B4B` spindle core.

### The Sync Locus Jewel (`○` vs `⦿`)
Every file across Web and Windows shares one unmistakable physical state glyph:
- **`○ NAS` (Hollow Silver Ring):** Online-only on `NAS-01`; occupies `0 B` on your PC.
- **`⦿ PC+NAS` (Vermilion / Tangerine Core Ring):** Anchored locally (`Always keep on this device`). Clicking the jewel toggles pin state inline with immediate tactile response.

---

## 4. Typography & Swiss Baseline Grid

- **Display & UI Sans (`--sans`):** `"Avenir Next", "SF Pro Display", "Segoe UI Variable Display", system-ui, sans-serif`
  - **Folder Masthead (`1.75rem / 1.08`, weight `660`, tracking `-0.035em`):** Tight, authoritative geometric presence.
  - **Object Names (`0.9375rem / 1.45`, weight `540` files / `620` folders, tracking `-0.008em`):** Effortless scanability at `45px` row pitch.
- **Technical Index Mono (`--mono`):** `"JetBrains Mono", "SF Mono", "Cascadia Mono", Consolas, monospace` with `tabular-nums` (`12px` minimum floor):
  - Used exclusively for **row indices (`01`–`99`)**, **file extension stamps**, **byte sizes**, **ISO/short timestamps**, **BLAKE3 digests**, and **network latency (`9ms WG`)**—never as a decorative gimmick on prose.

---

## 5. Workspace & Windows Bridge Anatomy

1. **Left Architectural Spine (`228px`):**
   - Brand lockup + port index (`:8443`), **Split Shutter Upload CTA** (matte black trigger with recessed vermilion jewel + chevron menu for folder upload / `mkdir`), **Spring-tracked vertical spine bar** (`All Files`, `Recent Changes`, `Anchored on PC`, `30-Day Trash`), and the **24-Tick `NAS-01` Capacity Instrument** pinned at the foot.
2. **Double-Bevel Archival Sheet:**
   - **Command & Path Strip:** Monospace path breadcrumbs (`NAS-01 / INDUSTRIAL_DESIGN`), `/` instant filter omnibox, **Ledger ↔ Contact Sheet** mode switch, and **Dossier** rail toggle.
   - **Ledger View:** Indexed Swiss rows (`#`, `Object + Stamp`, `Sync Locus`, `Modified`, `Bytes`) with a `3px` vermilion left spine rule on selection.
   - **Contact Sheet View:** `1px` hairline proof matrix (`4:3` stage, index header `04 // 3MF`, corner crop marks in Dossier).
   - **3-Lane Telemetry Tape:** Unfurls along the bottom seam only during transfers, plotting parallel chunk streams (`STREAM 01–03`) and live `BLAKE3` commit status.
3. **Windows Sync Instrument (`client-win`):**
   - Replaces generic settings cards with a live **`DESKTOP-01 ⇄ NAS-01` Topology Bridge** (animated packet pulse scaled to idle vs burst throughput), 24-tick local NVMe hydration ruler, `Dehydrate Cache` control, and **rectangular stretching hardware rockers**.

---

## 6. Apple Fluid Motion Contract (`web/src/lib/spring.ts`)

- **Pointer-down Tactility:** `scale(0.972)` in `85ms ease-out` on `pointerdown`; rectangular rockers widen their actuator thumb while held before snapping across on release.
- **Critically Damped Structural Springs (`ζ = 1.0, response = 0.28s–0.36s`):** Drives the vertical spine bar, bench tab sled, and tap-triggered Loupe sheet—always interruptible mid-flight from live presentation coordinates.
- **Direct-Manipulation Optical Loupe (`ζ = 0.8, response = 0.30s` on flick):** `setPointerCapture` tracks 1:1 from grab offset, resists upward pulls via `rubberband(overshoot, height, 0.55)`, and projects release velocity via `x + (v/1000) * 0.998 / 0.002`.
- **Accessibility Overrides:** Full support for `prefers-reduced-motion` (instant/cross-fade), `prefers-reduced-transparency` (solid `--plate` surfaces), and `prefers-contrast: more`.
