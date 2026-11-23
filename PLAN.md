# Aetheris: build plan

Personal Google Drive/OneDrive-style storage. Your Windows PC syncs with your QNAP NAS at home,
and a web UI at **drive.example.com** works from any browser, without opening your home network to the internet.

> **Living document.** Every session reads this first, works the first unchecked item of the
> current phase, then updates **Status** and **Session log** before ending.

---

## Status

| # | Phase | State |
|---|-------|-------|
| 0 | Groundwork (accounts, NAS prep, toolchains, brand assets) | **done** 2026-11-03 |
| 1 | Gateway spike: go/no-go on Vercel ⇄ Tailscale ⇄ NAS | **done, GO** 2026-11-03 |
| 2 | NAS server core (files, index, change feed, versions) | **done** 2026-11-03 |
| 3 | Auth & security (passkeys, devices, sessions) | **done** 2026-11-03 |
| 4 | Vercel gateway, production (drive.example.com) | **done** 2026-11-03 |
| 5 | Web UI | **live**: files, uploads incl. folders, previews, grid + thumbnails, versions, Recent, trash, settings, Quiet Sculpture redesign, file manager (multi-select, copy/cut/paste, drag, zip) and encrypted share links (**live 2026-11-07**, `1b95ce8`). Drive-style layout + Material Symbols (**live 2026-11-10**, `5a156a0`). **Computers** section (**live 2026-11-16**, `de8bc33`). Open: virtualization, screen-reader pass |
| 6 | NAS packaging & deploy (start on boot, updates) | **done** 2026-11-05 (real share, deploy + rollback, watchdog verified) |
| 7 | Windows client (Files On-Demand) | **v1.1.0 installed** (2026-11-16, self-hostable). In progress: two-way sync, tray + app window, own Explorer pane entry, uninstaller. 2026-11-16: **folders from this PC** (Computers), first-time setup, self-installing exe, versions + auto-update from GitHub releases (built and tested on the dev server; the first release waits for your token). Open: tailnet route test, deletes while offline, resumable uploads |
| 8 | Hardening, backup, polish | not started |

---

## Decisions (2026-11-03)

| Topic | Decision | Why |
|---|---|---|
| Remote access | **Hybrid**: any browser → Vercel gateway → WireGuard (Tailscale) → NAS. PC client goes direct over LAN or WireGuard, never through Vercel. | User wants Vercel and browser access from anywhere. Home router keeps zero open ports. |
| Why not "Vercel tunnel" | Vercel has no persistent tunnel (functions are request-scoped). The gateway *joins the tailnet itself* (Go + `tsnet`) and proxies. | It's the only way to put Vercel in front without exposing the NAS. |
| PC behaviour | **Files On-Demand** (Windows Cloud Files API), like OneDrive. Pin = "keep on this PC", unpin = NAS only. | User choice. Covers "store locally if I want to, or on the NAS". |
| Web UI sources | **NAS only.** PC-only files are not remotely reachable. | User choice. Smaller attack surface. |
| Users | Single user (you). The schema has a `users` table so more can be added later. | Personal files. |
| NAS software | **One static Go binary** (`aetheris`), with Tailscale embedded via `tsnet`. No Docker, no Container Station. | J1900 + 2 GB RAM. QTS 4.3.4 is too old to trust on any network path. Only this process joins the tailnet, never QTS. |
| Storage format | **Plain files** in a normal QNAP share, plus a SQLite index. No blob store, no proprietary format. | Your data stays readable over SMB/File Station even if Aetheris breaks. |
| Source of truth | The NAS. The PC holds placeholders plus pinned copies. | Simple, predictable conflict rules. |
| Server language | Go (cross-compiled `linux/amd64`, `CGO_ENABLED=0`). | Tiny RAM use, one file to deploy, `tsnet` is Go-native, and the gateway shares code. |
| Client language | C# / .NET 10 LTS, Native AOT. Win32/cfapi via CsWin32, sync-root registration via CsWinRT. | Cloud Files API bindings are generated from Windows metadata (correct structs). Small, fast exe. |
| Web UI stack | Svelte 5 + Vite, static SPA. Same build served by Vercel CDN and embedded in the NAS binary. | Small bundles, good motion primitives, no server rendering needed. |
| DNS | Keep example.com at **Namecheap**. Add one record `drive` → Vercel. Moving NS to Vercel DNS is optional and not needed. | Smallest change that doesn't put the domain's mail at risk. |
| NAS TLS cert | Tailscale's automatic cert for `aetheris.<tailnet>.ts.net` (tsnet `ListenTLS`). | No DNS API token on the NAS, renewal is automatic. Users only ever see drive.example.com. |
| Hash | **BLAKE3** (`zeebo/blake3`). Measured on the NAS: 244 MB/s vs SHA-256's 55 MB/s per core. | J1900 has no SHA-NI/AES-NI. SHA-256 would bottleneck LAN uploads. |
| Folders from the PC (2026-11-16) | Any folder (Desktop, Documents, ...) syncs **two-way** to `Computers/<PC name>/<folder>` on the NAS, every file kept on the PC. Each is a Cloud Files sync root of its own, so Explorer shows the status icons and the same engine handles deletes, renames, conflicts, Office saves and offline edits. The web shows `Computers` as a section; the Aetheris folder leaves it out. | Your request (OneDrive/Drive-style computer folders). Reusing the tested engine beat writing a second one. |
| Self-hostable (2026-11-16) | No installation is built into the code (your call: public-facing, your setup kept). The server's web address lives in its state folder (`aetheris setup -origins …` once, `origins.txt`); pairing answers say where the server is (tailnet name, LAN address, web address); the Windows app asks for the server with the code and keeps it in `server.txt`; the gateway finds `aetheris.<tailnet>` by itself or takes `AETHERIS_NAS`. | Anyone can run their own; your NAS, gateway and PC got their values written into those places instead. |
| Install & updates (2026-11-16) | **One self-contained exe** (`AetherisSetup.exe`, .NET inside) that installs itself **per Windows user, no admin** (your call), into `%LOCALAPPDATA%\Programs\Aetheris`. Versions come from git tags (`v1.2.3`). CI publishes to the releases of **`realCmdData/aetheris` itself** (your call, 2026-11-16, replacing a separate releases repo); installed copies update themselves from there, **which needs the repo public** (they read releases without a token). No MSI. | Works for every user without admin rights or Developer Mode, and updates silently like OneDrive. |
| User-facing URL | `https://drive.example.com` (apex `example.com` is possible with one different DNS record if preferred). | Keeps the apex free and the mail domain untouched. |

---

## Facts found (2026-11-03)

- **NAS**: QNAP reports as `Q805` / origin `TS-453-G Pro`, internal `TS-X53`, platform `X86_BAYTRAIL`, Celeron J1900, 2 GB RAM, hostname `NAS-01`, LAN `192.168.1.20`.
  - **Firmware QTS 4.3.4.0931 (build 2019-04-23).** Old, with many known critical CVEs. Never expose it, and update it if this unit allows.
  - Ports: QTS admin HTTP `8080`, HTTPS `443` (stunnel), Web Server `80`/`8081`. **443 on the LAN is taken by QTS**, so Aetheris's LAN listener uses `8443`.
  - **4.3.4 is the final firmware for this unit** (you checked), so QTS will never be patched again.
  - Checked over SSH (2026-11-03):
    - Kernel 4.2.8 x86_64, 4 cores. CPU flags `sse4_1 sse4_2` only: **no AES-NI, AVX or SHA-NI**.
    - RAM 1885 MB, about 1.1 GB available, 24 GB swap. Load average about 1.0 at idle (QTS services and other apps).
    - **No `/dev/net/tun`**, so tsnet runs in userspace mode (already planned). inotify: `max_user_watches` 131072, `max_user_instances` 128.
    - Share `/share/Aetheris` → `/share/CACHEDEV1_DATA/Aetheris`: ext4, 21.6 TB volume with 16.1 TB free, writable by `aetheris`.
    - **Outbound HTTPS works, but QTS's 2019 CA store can't verify today's certificate chains** (curl error 60).
      Every Go binary we ship embeds its own roots: import `golang.org/x/crypto/x509roots/fallback` and add `//go:debug x509usefallbackroots=1` in `main`.
    - **8443 is free** on the LAN (443 is QTS's).
    - sshd: P8 switches it to key-only (details in `CLAUDE.local.md`).
  - User `aetheris` (uid 1001) is in **administrators**, which QTS requires for SSH. It serves as the deploy account.
    Key auth uses `~/.ssh/aetheris_nas` on this PC, restricted to `from="192.168.1.*"` and installed by `scripts/install-nas-key.ps1`.
    Whether the server runs as a separate non-admin user is decided in P6.
- **PC**: Windows 11 Pro 26300, Ryzen 9 9900X3D, 31 GB RAM, LAN `192.168.1.30`.
  Installed: Node 24.19, pnpm, git, gh, OpenSSH, Python 3.13, Go 1.27.3, .NET SDK 10.0.403. **Missing: Tailscale.**
- **DNS**: the domain is at Namecheap (BasicDNS). One `CNAME` points `drive` at Vercel; nothing else changes.
- **Tailscale**: tailnet `tail1234.ts.net`, MagicDNS on, HTTPS certificates on, policy from this plan saved.
  PC `desktop-01` = `100.64.0.2` (client 1.104.5). The NAS node will be **`aetheris.tail1234.ts.net`**.
- **Vercel**: team "realcmddata's projects", Pro. The domain is not on Vercel DNS.
- **Brand**: logo at `assets/brand/aetheris-logo.jpg` (raster: glass infinity ring over a red→tangerine house inside a cloud outline, wordmark "Aetheris").

---

## Architecture

```
 Any browser ──HTTPS──▶ drive.example.com  (Vercel, region fra1)
                          ├─ static web UI from CDN
                          └─ /api/*  → Go gateway (Fluid compute)
                                        └─ tsnet node tag:gateway ──WireGuard──┐
                                                                               ▼
 Windows PC ─ Aetheris client ─┬─ LAN  https://192.168.1.20:8443 ───▶  NAS: `aetheris` (one Go process)
   Explorer: Files On-Demand   └─ WireGuard (Tailscale) ─────────────▶   ├─ own tailnet node tag:aetheris (tsnet)
                                                                         ├─ plain files in share /Aetheris
 QTS admin UI: LAN only. Never on the tailnet, never public.            └─ SQLite index + change journal
```

Properties:
- **Zero inbound ports** at home and no UPnP. The NAS only makes outbound connections.
- **Only the `aetheris` process is reachable** from the tailnet. QTS and its services are invisible to everything outside the LAN.
- The **PC client never touches Vercel**: LAN when at home (full gigabit), WireGuard when away.
- **Data stays plain files.** Aetheris adds an index and versions next to them.

### The honest trade-offs of the Vercel path
- Vercel terminates TLS for drive.example.com, so **file bytes are plaintext inside Vercel while proxied** (the same trade Cloudflare would have).
  Vercel also serves the UI's JavaScript, so you trust Vercel for browser sessions either way. Sync traffic never takes this path.
- Function limits: **4.5 MB request body**, **max 800 s duration** (Pro + Fluid).
  Uploads go in ≤ 4 MB chunks. Streamed **responses are not size-capped** (verified in P5 with 5.4 MB), so downloads are plain.
  Only multi-GB remote downloads could meet the 800 s ceiling; a ranged Service Worker is the fallback if it ever matters.
- **Cold start**: a fresh function instance must join the tailnet (estimate 1–3 s). Fluid compute reuses warm instances. Measure in P1.
- **Vercel freezes an instance whenever no request is open** (found in P1). Background goroutines stall, including tsnet's login, keepalives and handshakes.
  The gateway must join the tailnet *inside* a request, and must tolerate a stale WireGuard session after a thaw. Never assume work happens between requests.
- If Vercel can't get a direct WireGuard path, traffic falls back to Tailscale DERP relays (slower). Measure in P1.
- **Cost**: proxied bytes count as Vercel data transfer and function time. Set a **Spend Management cap**.
- **Fallback** if P1 fails: private-only (Tailscale app on each device, web UI served by the NAS). No code is wasted, because the web UI is already embedded in the server.
- Tailscale's coordination server sees metadata (which devices exist), not traffic. Optional: enable **Tailnet Lock**.

### Components & repo layout

```
Aetheris/
  CLAUDE.md            session rules (read first)
  PLAN.md              this file
  assets/brand/        logo source, SVG mark, .ico, tray icons
  server/              Go: NAS server  (cmd/aetheris)
  gateway/             Go: Vercel gateway (own go.mod; Vercel root dir = gateway/)
  web/                 Svelte 5 + Vite SPA (built into server/ embed and gateway static)
  client-win/          C# .NET 10: Windows client
  packaging/qpkg/      QTS package / autorun scripts
  docs/protocol.md     API contract shared by server, gateway, web, client
```

### NAS server (`server/`)
- Static binary, target **RSS < 100 MB** including tsnet, `GOMEMLIMIT=96MiB`.
- Listeners: tsnet `:443` (TLS, automatic ts.net cert) and LAN `192.168.1.20:8443` (same cert; the client sets SNI to the ts.net name).
- All file I/O goes through **`os.OpenRoot`** (Go ≥ 1.24) on the share root, which blocks `..` and symlink escapes by construction.
- Index: SQLite via `modernc.org/sqlite` (pure Go, WAL). Tables: `files` (path, parent, size, mtime, hash, rev, deleted), `journal` (seq, path, op, rev), `users`, `passkeys`, `devices`, `audit`.
- Change feed: monotonically increasing `seq`. Clients call `GET /changes?cursor=N&wait=30` (long-poll).
- Edits made outside Aetheris (SMB, File Station): inotify plus a periodic rescan (mtime+size first, hash only on change). Check `fs.inotify.max_user_watches` in P0; the rescan is the safety net.
- Writes: stream to `.aetheris/tmp/` → fsync → hash → atomic rename. The previous version moves to `.aetheris/versions/`.
- Concurrency: `If-Match: <rev>`. A mismatch returns `409`, and the client keeps both copies (`name (conflict PCNAME 2026-11-03).ext`).
- Trash: 30 days. Versions: last 10 per file, or 30 days, whichever keeps more.
- Reject names Windows can't store (`CON`, `NUL`…, trailing dot/space, `<>:"|?*`) at upload time.
- TLS: Go selects ChaCha20-Poly1305 automatically on CPUs without AES-NI, so no tuning is needed.
- Runs as the dedicated QTS user `aetheris` (not admin), with read/write on the `Aetheris` share only.

API sketch (`/api/v1`, JSON over HTTPS; the full contract goes in `docs/protocol.md` during P2):
```
GET  /tree?path=/x                 list children
GET  /changes?cursor=N&wait=30     change feed (long-poll)
GET  /file?path=..                 download (Range, ETag = rev)
POST /upload                       start → {id, chunkSize}
PUT  /upload/{id}?offset=N         chunk (≤ 4 MB via gateway, 16 MB direct)
POST /upload/{id}/commit           {path, baseRev, hash} → 200 | 409
POST /move  /delete  /mkdir  /restore
GET  /versions?path=   /search?q=   /thumb?path=&size=
POST /auth/passkey/{begin,finish}   GET/POST/DELETE /devices
```

### Vercel gateway (`gateway/`)
- Vercel project `aetheris` in the existing team. Go framework preset (`main.go` listens on `$PORT`), Fluid compute, region `fra1`.
- Joins the tailnet with `tsnet`: ephemeral, `tag:gateway`, reusable pre-authorized auth key stored as a **Sensitive** env var, state in `/tmp`.
- Reverse-proxies `/api/*` to `https://aetheris.<tailnet>.ts.net`.
- **Pre-auth filter** at the edge forwards only:
  1. auth endpoints, rate-limited with Vercel Firewall, and
  2. requests whose session token verifies against the NAS's **Ed25519 public key** (env var; the private key never leaves the NAS).

  Unauthenticated junk never crosses into the home network.
- Static UI from `web/` build: hashed immutable assets, `index.html` no-cache.

### Windows client (`client-win/`)
- Sync root `C:\Users\you\Aetheris`, registered with `StorageProviderSyncRootManager`. That gives an Explorer nav-pane entry with the Aetheris icon, native cloud/check status icons, and "Always keep on this device" / "Free up space" menu items for free.
- Placeholders for the whole tree come from `/tree` and stay current via `/changes`. Every start reconciles the folder against the tree.
- State lives on the placeholders, not in a database: FileIdentity is `rev\nhash`, and the NAS path is wherever the placeholder sits (a folder move never rewrites its contents).
  Changed content → dehydrate (exclusive handle, `VERIFY_IN_SYNC`); same hash → identity update only. Only in-sync placeholders are ever replaced or deleted.
- Open file → `FETCH_DATA` callback → ranged GET → `CfExecute(TRANSFER_DATA)` in 1 MB chunks. The response ETag must equal the placeholder's rev, otherwise the open fails (`NOT_IN_SYNC`) instead of splicing two versions.
- Local edits: `FileSystemWatcher` (2 s quiet) plus a full check at every start → chunked upload (16 MB) → commit with `baseRev`. What needs sending is read off each item: anything that is not an in-sync placeholder.
  New files and folders are converted to placeholders once the NAS has them. An edit made during the upload leaves the file unsynced, so it goes up again.
  `409`: same hash on the NAS (only moved/touched) → retry with its rev; gone on the NAS → the edit brings it back; otherwise keep both: `name (conflict PC yyyy-MM-dd HH.mm).ext` gets your version on both sides, the original name the NAS version, plus a notification.
  Deletes and renames: cfapi `NOTIFY_DELETE` / `NOTIFY_RENAME` go to the NAS before Windows completes them (folders whole, to the NAS trash; out of the folder, e.g. the Recycle Bin, = delete). Our own deletes and renames (applying NAS changes) are recognised by process id and pass.
  One gate is held while an upload commits and while NAS changes apply, so our own upload echoing back is never taken for an edit elsewhere. Windows clears in-sync on rename; the client restores it if size and write time are unchanged.
- Endpoint choice: one base URL `https://aetheris.tail1234.ts.net`; `SocketsHttpHandler.ConnectCallback` dials LAN `192.168.1.20:8443` first (1.5 s; `AETHERIS_LAN` overrides the address), else the tailnet name, and skips the LAN for a minute after a miss. TLS verifies the ts.net name either way. Never Vercel.
- Device token from pairing (code shown in the web UI), stored with DPAPI, revocable in the web UI.
- Tray (Shell_NotifyIcon via CsWin32, plain Win32): status line, Open Aetheris, open folder, open drive.example.com, pause/resume, start with Windows, quit. Left-click opens the app window.
- **App window** (2026-11-06): WebView2 (the Edge engine Windows ships; Core API in a plain Win32 window, no WPF/WinForms), interface in `web/src/desktop` (Svelte, same tokens, springs, type), solid page background with the title bar painted the same colour (`DWMWA_CAPTION_COLOR`; Mica dropped: with accent-coloured title bars it left a white page under dark-mode text),
  built by `pnpm build:desktop` into `client-win/ui` (next to the exe, served as `https://app.aetheris/`, CSP'd, never navigates away). JSON messages both ways (`AppWindow.cs` ⇄ `bridge.ts`, with a mock for designing in a browser: `pnpm dev:desktop`).
  Shows: status first (big), live transfers with spring-smoothed progress, open folder / drive / web, storage on this PC + "Free up space", drive letter, start with Windows, pause, activity (last 100), account + sign out (confirmed inline). Pairing happens here too (the Win32 pairing window is gone).
  The tray thread is STA with a SynchronizationContext pumped by its message loop; the engine always runs on the thread pool.
- **Explorer, as OneDrive**: inside the folder Explorer shows the cloud / check status column (verified). The folder itself wears the Aetheris logo (`desktop.ini` + read-only bit, written at every start; never uploaded).
  The registration also writes a top-level navigation-pane entry (`HKCU\Software\Classes\CLSID\{NamespaceCLSID}` + `Desktop\NameSpace`, pinned, logo icon), but **this Windows build (10.0.26300) doesn't show it**, nor a hand-made entry written exactly per Microsoft's guide, even after Explorer restarts and with `FolderValueFlags` 0x28. OneDrive (special-cased) and Linux (HKLM) do show.
  - [x] Real pane entry via package identity (2026-11-06, **works**: a top-level Aetheris node with the logo above This PC): `client-win/package/AppxManifest.xml` is a sparse package (`uap10:AllowExternalContent`, exe outside it, full trust, write virtualization off, `desktop3:Extension windows.cloudFiles`),
    `app.manifest` names it (`<msix packageName=Aetheris.Drive publisher=CN=Aetheris>`), the installer registers it with `Add-AppxPackage -Register … -ExternalLocation` (only in Developer Mode; otherwise it says so and the app runs unpackaged as before).
    With the identity the app registers the folder again (no unregister); if Windows refuses, it keeps the old registration and logs it. With Developer Mode on, Windows accepted it over the unpackaged registration
    (all 55 items intact, files open), gave it a new NamespaceCLSID, and the entry showed after an Explorer restart. The orphaned old namespace keys were removed by hand (a fresh install never has them).
  - **Never unregister a sync root that's in use**: Windows deletes every online-only placeholder and turns downloaded files into plain files (found on the dev profile: the next start then took the missing files as offline deletes). Never remove the package during updates either.
  - Offline-delete guard also restores when 5+ items and more than half of the tree are missing (small folders).
- **Drive letter** (2026-11-06, optional, **off by default**): the folder can also be a drive letter, like Google Drive's G:. Explorer treats it as a plain disk (no status icons, no "Always keep"), which is why it is only a shortcut. Per-user `DefineDosDevice` mapping (what `subst` does) plus per-user label/icon under `HKCU\Software\Classes\Applications\Explorer.exe\Drives\X`; no admin.
  Mapped while the app runs (files only open then anyway), removed on quit; a taken letter is never covered. Opening files through `A:\` hydrates as usual.
  Notifications (tray balloons, shown as Windows notifications) only for sign-outs and conflicts. One small native pairing window (themed v6 controls, per-monitor DPI manifest).
  One instance per profile (named mutex). Log: `%LOCALAPPDATA%\Aetheris\<profile>\aetheris.log`, rotated at 1 MB.
- Autostart via `HKCU\...\Run`. Unsigned at first (SmartScreen warning); code signing comes in P8.
- **Folders from this PC** (2026-11-16, `Engine.AddFolder/RemoveFolder`, `folders.txt` in the profile dir: local path + NAS path, fixed when added). NAS path `Computers/<Dns.GetHostName()>/<folder name>` (`name (2)` on a clash).
  - `SyncRoot` keeps a table of folders (the Aetheris folder = NAS `""`, everything outside `Computers`); `Rel`/`LocalPath`/`Owner` map paths both ways. One tree per pass, reconciled per folder, each with its own `synced-<id>.txt`.
  - Registered with `AllowPinning = false` (no "Free up space"), and every file downloads at once (`Local.Pin`). No `desktop.ini`/logo written into them.
  - Refused: whole drives, system and app folders, network and non-NTFS drives, anything inside or around another synced folder. Windows itself refuses folders another sync app (OneDrive) owns.
  - Added again: items already here with the NAS's size and mtime (ms) become its placeholders, no conflict copies (this also helps the Aetheris folder after a reinstall). NAS lacks the folder: what is here goes up again; nothing here is ever deleted for being missing there.
  - Deleted or moved on the NAS (the web's Computers, or `Computers/<PC>` above it), live or while the app was off: this PC **stops syncing it** and its files stay. Stop syncing in the app first downloads anything online-only (Windows deletes those on unregister), then unregisters; the NAS copy stays.
  - Junctions and symlinks (Documents' old "My Music") are skipped, never followed (`Local.Foreign`/`Walk`: placeholders are reparse points too, so it checks the link target).
- **First-time setup** (app window): connect (pairing code), then choose folders (Desktop, Documents, Pictures as switches; "Choose another folder..." opens Windows' folder picker), then Home. Home lists the folders with Stop syncing (asked inline) and "See them on the web". Pairing no longer opens Explorer over the setup.
- **Install, versions, updates** (`Setup.cs`, `Update.cs`, `.github/workflows/release.yml`): the release exe started from anywhere else quits the installed copy, copies itself in, unpacks `ui\`, `package\`, `uninstall.ps1` (once per build), registers the package in Developer Mode, and starts with its window open.
  The updater looks a minute after start and every 6 h at `releases/latest`, downloads `AetherisSetup.exe`, checks GitHub's SHA-256 `digest`, waits until nothing transfers, renames itself to `Aetheris.old.exe`, starts the new exe with `--after <pid>` and quits. Local builds (0.0.0), the dev profile and copies outside the install folder never update.
  Trust: whoever can publish releases in `realCmdData/aetheris` can update the app; code signing (P8) would add a check of its own. The workflow uses GitHub's own per-run token (`contents: write`), so there is no secret to keep.
  **The repo is private (checked 2026-11-16): `releases/latest` answers 404 without a token, so updates and the web's "Get Aetheris for Windows" link only work once it is public.** A token inside the app is no way out: anyone with the exe could read the code with it.

### Web UI (`web/`)
- Budget: **≤ 60 KB gz initial JS**, first paint < 1 s from CDN, 60 fps scrolling for 10k-item folders (virtualized list).
- Views: Files (list/grid, breadcrumbs, multi-select, drag-drop upload, full keyboard nav), Preview (image, video, audio, PDF, text), Search, Recent, Trash, Versions panel, Devices (pair/revoke), Settings. PWA manifest for the phone home screen.
- Uploads: 4 MB chunks, 3 in parallel, resumable after reload (cursor in IndexedDB). Downloads: Service Worker stitches ranged chunks into one streamed download.

---

## Security model

- **Network**: no port forwards, no UPnP (check the router too). Tailscale policy (draft below) allows only `you → tag:aetheris:443` and `tag:gateway → tag:aetheris:443`.
- **Identity**: passkeys (WebAuthn, RP ID `drive.example.com`). No passwords. The Windows client uses a paired device token.
  Direct connections are also checked with Tailscale `WhoIs` (must be one of your devices).
- **Sessions**: Ed25519-signed tokens (`{sid,exp}`) in cookie `__Host-aetheris` (`Secure`, `HttpOnly`, `SameSite=Strict`) for browsers, or a bearer token for devices.
  - Browser sessions last 30 days and devices a year, sliding.
  - The gateway verifies signatures with the public key. The NAS also checks revocation on every request.
  - Cookie writes need a matching `Origin` (CSRF second lock).
- **Recovery** (changed in P3): **no recovery codes**, because they'd be a phishable second way in. Register two or more passkeys (phone + PC).
  Lost them all? `aetheris reset` over SSH from the LAN prints a fresh 15-minute setup link.
- **Serving user files**: `Content-Disposition: attachment` unless the type is an allow-listed preview type. Previews carry `CSP: sandbox` and `nosniff`.
  User HTML/SVG is never rendered on the app origin (prevents stored XSS).
- **App headers**: strict CSP (no inline script, no third-party origins), HSTS, `frame-ancestors 'none'`, COOP `same-origin`.
- **Audit log**: sign-ins, pairings, revocations, deletes, restores. Visible in Settings.
- **Not a backup.** Versions and trash protect against mistakes, not against disk or NAS loss or ransomware on the NAS. P8 sets up QNAP HBS 3 to a USB disk or offsite target.
- **QTS 4.3.4**: update the firmware if possible. Even if it can't be updated, nothing in this design reaches QTS from outside the LAN.

Tailscale policy draft (validate in the admin console in P0):
```json
{
  "tagOwners": { "tag:aetheris": ["autogroup:admin"], "tag:gateway": ["autogroup:admin"] },
  "grants": [
    { "src": ["autogroup:member"], "dst": ["tag:aetheris"], "ip": ["tcp:443"] },
    { "src": ["tag:gateway"],      "dst": ["tag:aetheris"], "ip": ["tcp:443"] }
  ]
}
```

---

## Design system

Name **Aetheris**, domain **drive.example.com**. Follow the `apple-design` skill for every UI change.

**Language: "Quiet Sculpture"** (2026-11-07, your spec `docs/design-language.md`, live specimen `assets/brand/showcase.html`).
Objects read by silhouette and material, not by labels; one sculptural title per screen; a seashell surround frames one porcelain sheet.
Tokens live in `web/src/tokens.css`; the older "Machined Hearth" draft is kept as `docs/design-language-machined-hearth.md`.

### Palette & surfaces

| Token | Light (Porcelain) | Dark (Obsidian) | Role |
|---|---|---|---|
| `--coral` / `--apricot` | `#E54B4B` / `#FFA987` | same | Brand warmth: active icon, "+" facet, progress, `--brand-gradient`. Coral is 3.7:1, so **icons and large text only**. |
| `--ambient` | `#FAF1EE → #F5E6E1` | `#131317 → #17161B` | Outer seashell surround (web shell, sign-in) |
| `--stage` | `#FFF → #FCF9F7` | `#222229 → #1E1E24` | The porcelain sheet, `26px` radius; Quick Look and dialogs |
| `--card` | `#FFF` | `#272730` | Object mats, paper sheets, grouped rows |
| `--text` | `#1E1E24` (16:1) | `#F7EBE8` (14.2:1) | Titles, names |
| `--text-2` | `#444140` (9.7:1) | `#C8BFBC` (8.2:1) | Interactive icons |
| `--text-3` | `#6E6664` | `#9A9290` | Secondary facts. **Darker than the spec's `#7A7270`/`#8E8684`**, which measure 4.48:1 on porcelain and 4.15:1 on dark cards; these pass AA (≥ 4.7) on stage, card and seashell. |
| `--accent-fill` / `--on-accent` | `#C93A3A` / `#fff` (5.1:1) | `#FFA987` / `#1E1E24` (8.9:1) | Primary text buttons |
| `--error` | `#B42318` | `#F97066` | Always with icon and text, because the brand is red too |

- File types (2026-11-10, Drive-style): filled Material Symbols in a few tones (`--tone-*`): media and PDF coral, documents and code blue, sheets green, slides amber, folders and the rest neutral (`web/src/lib/Visual.svelte`). This replaced the wordless sculpted visuals, which you found hard to read.
- **Web app layer** (`web/src/drive.css`, web only; the Windows app window keeps the plinth): Drive's frame (warm `--frame` top bar + sidebar around one white `--surface`), tonal `--container` for the active nav item and selected objects, Material 3 shapes (pill buttons, outlined secondary buttons, solid menus, segmented layout toggle), titles in Lexend 400 instead of Canela.
- Light/dark follow `prefers-color-scheme`; `data-theme` overrides.

### Type
- **Display: Canela Deck Light** (`--font-display`, 300, tracking −0.015em) for titles only: 2.35rem page titles, 1.6rem sheet/section titles.
  Canela is commercial and only installed here as a trial, so it is **never shipped**: `local()` finds it on this PC, other devices fall back to Sitka Display / New York (`ui-serif`) / Georgia.
  The trial has basic Latin only, so ü, é, ß, – fall back glyph by glyph even here. A licensed woff2 next to Lexend fixes both.
- **Everything else: Lexend** (OFL, self-hosted latin + latin-ext woff2 in `web/src/fonts/`, 300/400/500, ~85 KB total). Body 300, names 400, captions 0.78rem 300. `tabular-nums` for sizes and dates.
- No Segoe UI, no monospace chrome (file *content* in Quick Look stays monospace).

### Materials
- Glass (`--glass`, `blur(22px) saturate(160%)`, white edge) only for things that float: menus, the Inspector, the upload pill, the phone tab bar.
- Never glass on glass. Quick Look and dialogs are the solid porcelain `.card` over a blurred, dimmed scrim.
- Scroll edges fade (mask) instead of 1 px dividers. `prefers-reduced-transparency` and `prefers-contrast: more` → solid cards and defined borders.

### Motion
- Springs everywhere a user touches (`web/src/lib/spring.ts`). Default **damping 1.0**; rail indicator **response 0.26 s**; Quick Look **0.94 / 0.32 s** from and back to the opened tile.
  Only momentum gestures (drag-release of Quick Look) get **damping 0.8, response 0.3 s**, with rubber-band and projection.
- Feedback on pointer-down (`scale(.97)`, 100 ms); the rail indicator moves on press, not on release.
- Panels leave along the edge they came from (Inspector slides right); menus bloom from their trigger.
- Progress moves continuously (upload ring, desktop hairline glides on a spring).
- `transform`/`opacity` only (exception: the search pill's width and the Inspector's content padding, once per toggle). `prefers-reduced-motion` → instant or short cross-fades, no travelling glint.

### Logo & icons
- **Source: your vector logo `assets/brand/logo.svg`** (2026-11-05; `logo-stacked.svg` is your variant with lighter ribbons).
  `aetheris-logo.jpg` stays only as the original reference.
- **In the UIs (2026-11-06, your call): the artwork itself.** `web/src/lib/Logo.svelte` inlines `assets/brand/logo-stacked.svg` (not an `<img>`), so CSS gives the
  `brand-wordmark` group `var(--text)` and the wordmark stays legible on dark. `variant="mark"` is the same file cropped to `155 57 490 490` (the drawing spans x 180–618, y 139–466 plus strokes) without the wordmark.
  Used for: web sign-in (full), rail / empty folder / offline (mark), app window plinth (mark) and pairing screen (full). No derived image is left in either UI.
  The showcase's redrawn emblem is **not** used: your 2026-11-06 call (the artwork itself) stands.
- **UI icons (2026-11-10): Material Symbols Rounded, weight 400** (`@material-symbols/svg-400`, Apache-2.0, dev dependency). `lib/Icon.svelte` globs only the listed files and inlines their path data (~9 KB gz); filled variants for active nav items and file types. Old icon names map to symbols, so both UIs use it. Add a symbol to the glob before using it.
- `pnpm icons` still derives `mark.svg` / `mark-small.svg` for what must be raster or tiny: favicon, PWA tiles, `aetheris.ico` (16–256).
- `.ico` sizes 16/20/24/32/48/256. Tray variants: idle, syncing, paused, error.
- Labels are plain and specific: "Keep on this PC", "Free up space", "Restore", "Remove device".

---

## Phases

Each item should end in something verifiable. **(you)** marks a step only you can do (logins, hardware, accounts). Everything else is the session's job.

### P0 Groundwork
- [x] (you) Tailscale account, MagicDNS and HTTPS certificates on, policy saved.
- [x] (you) Tailscale installed on this PC.
- [x] (you) NAS: SSH on, share `Aetheris` created, user `aetheris` created (ended up in administrators; see Facts).
- [x] (you) SSH key installed for `aetheris`. Claude logs in with the key and never with the password.
- [x] (you) Firmware: 4.3.4 is already the newest for this unit.
- [x] (you) Namecheap: stale apex A record deleted.
- [x] (you) Router: UPnP off.
- [x] Install Go (`winget install GoLang.Go`) and the .NET 10 SDK (`winget install Microsoft.DotNet.SDK.10`). Installed: go1.27.3, .NET SDK 10.0.403.
- [x] Repo linked to `github.com/realCmdData/aetheris` (private, `main`). `.gitignore` and README added. Folders for each component get created by the phase that fills them; the `docs/protocol.md` stub moved to P2.
- [x] SVG marks drawn: `assets/brand/mark.svg` (≥ 32 px, two-tone ring with over/under crossing) and `mark-small.svg` (≤ 24 px, infinity inside the house).
  `pnpm icons` builds `icons/aetheris.ico` (16–256) plus PNGs 16/32/48 and opaque tiles 180/192/512.
  Tokens are in `web/src/tokens.css` (`light-dark()` roles, materials, type, reduced-transparency and contrast overrides).
  The brand sheet `assets/brand/preview.html` is served by the `brand-sheet` entry in `.claude/launch.json` and was checked in light and dark.
- [x] NAS facts recorded in **Facts** over SSH. QPKG vs autorun is still open and moves to P6 (it needs admin, and the account has it).

### P1 Gateway spike (go/no-go)
- [x] NAS: `spike/nas` runs as `aetheris` in `~/spike` and was authorized via its login URL. Node `aetheris` = `100.64.0.1`, tagged.
  QTS BusyBox has **no `nohup`**. Start detached with `(cmd </dev/null >log 2>&1 &)`; with no tty there is no SIGHUP.
  The ts.net cert works on both listeners: tailnet `:443`, and LAN `:8443` via SNI. RSS 29 MB idle, 59 MB after transfers.
- [ ] Vercel: throwaway Go gateway with tsnet (`tag:gateway`) proxying `/api/ping` and ranged reads. Deploy to a preview URL.
  This needs one auth key, which you create: admin console → Settings → Keys → **Generate auth key**, set to **reusable + ephemeral + pre-approved**, tag `tag:gateway`.
  It goes into a Sensitive Vercel env var, never into git.
  **Status:** you imported the repo as Vercel project **`aetheris`** (root `spike/gateway`, `TS_AUTHKEY` set).
  It deployed to `aetheris-realcmddatas-projects.vercel.app`, behind Vercel Authentication.
  The connector was widened to `aetheris`. Creating an automation-bypass secret was **blocked by Claude's safety check** (it weakens protection), so measurements go through the connector's `web_fetch_vercel_url` (temporary share link, 23 h).
  The gateway measures the NAS leg itself (`/health`, `/bench?mb=N`).
  The project **auto-deploys every push to `main`** from `spike/gateway`. P4 re-points its root directory.
- [x] Measured on 2026-11-03 (fra1 ↔ home, Telekom):
  - **Two blockers found and fixed in `spike/gateway`:**
    1. Vercel freezes idle instances, so the join must run inside a request (see Trade-offs).
    2. Vercel's runtime hides the `AWS_LAMBDA_*` variables. Its only interface is `vint_runtime 169.254.100.6/30` (link-local), so Tailscale decided "network down", paused its control client and never logged in.
       **Fix: set the four `AWS_LAMBDA_*` vars if missing before tsnet starts**, which turns on Tailscale's own Lambda handling. The production gateway must keep this. Re-check after every tailscale.com upgrade.
  - **Cold join 158 ms**, first NAS round trip 117 ms (incl. WireGuard + TLS handshakes), **warm round trip 9 ms**.
  - Path is **direct** WireGuard to `84.133.122.86` (no DERP). **NAS → gateway 24.1 MB/s (193 Mbit/s)** sustained over 50 MB.
  - The NAS's `WhoIs` sees `aetheris-gateway tag:gateway tagged-devices`, so the server can tell gateway traffic from your devices.
  - The 4.5 MB / 800 s limits weren't tested on purpose: the design already moves ≤ 4 MB ranged chunks on this path, so they can't bite.
  - **After 4 min idle** the same instance thawed and answered in **55 ms** (a WireGuard re-handshake), so frozen-but-warm instances are cheap.
- [x] Measured PC → NAS with a 200 MB file, 2026-11-03:
  - **LAN `:8443` (TLS only): 60.5 MB/s, NAS ~27 % CPU** (one core; ChaCha20 is single-stream CPU-bound).
  - **Tailnet (userspace WireGuard + TLS, direct over LAN): 42 MB/s, NAS ~90 % CPU** (all cores).
  - Range `206` OK. Tailnet `WhoIs` returns `desktop-01` / `you@example.com`, so the direct-connection identity check works.
  - Consequences: the client must prefer LAN `:8443` at home (confirmed). P7 hydration of big files should use 2–3 parallel ranged requests to get past the single-stream cap. (Measured in P7: 60 MB/s was OpenSSL with ChaCha20. The Windows client gets AES-128-GCM, 17 MB/s per stream.) Remotely, home upload bandwidth caps long before the NAS CPU.
- [x] **Decision (2026-11-03): GO** with the Vercel gateway. Cold start is sub-second, the path is direct, ~190 Mbit/s NAS → Vercel, 9–55 ms round trips. No fallback needed.
  Carry into P4:
  - in-request join
  - the `AWS_LAMBDA_*` workaround
  - a long-lived credential instead of the 7-day spike key: a Tailscale **OAuth client** secret (scope `auth_keys`, tag `tag:gateway`) works as tsnet `AuthKey` and never expires
- [x] Cleanup:
  - NAS spike stopped; test blob and binary deleted.
  - **`~/spike/tsnet` is kept**: it holds the `aetheris` node identity. P2 moves it to the server's state dir, so the name stays `aetheris` and doesn't become `aetheris-1`.
  - `spike/gateway` stays in the repo until P4 replaces it (the Vercel project builds from it). `spike/nas` is deleted once `server/` exists (P2).

### P2 NAS server core
- [x] `server/` (module `aetheris/server`): `cmd/aetheris`, `internal/store`, `internal/api`.
  - Flags instead of a config file (`-data`, `-listen`, `-rescan`), JSON slog, graceful shutdown, `GOMEMLIMIT` 96 MiB default.
  - **Loopback-only until P3** adds auth; main refuses anything else.
- [x] SQLite (`modernc.org/sqlite`, WAL) with `PRAGMA user_version` migrations.
  - Tables: `files`, `journal`, `versions`, `trash`, `meta`.
  - An **epoch** id per index; a client cursor from another epoch gets `410` and resyncs.
- [x] API per `docs/protocol.md`: tree (NDJSON full snapshot with cursor), long-poll changes, ranged download with ETag = rev, resumable chunked upload, commit with `baseRev` (in the body, not `If-Match`), mkdir, move, delete, search.
  - Downloads re-check bytes against the index, so an ETag never labels different content.
- [x] Versions (hard link of the replaced file, no copy) and trash (one rename), both in `.aetheris/` as plain files with DB rows.
  - Retention: 30 days, newest 10 versions kept. Stale uploads go after 7 days.
- [x] Windows-name rules and case-collision checks, on input and when scanning.
- [x] Scanner (hashes outside the write lock, re-checks before recording) + fsnotify watcher (2 s debounce) + hourly full rescan.
  **Verified live on the NAS:** outside edits reach a waiting long-poll in ~2 s. Server RSS 11 MB.
- [x] Hash benchmark on the NAS: **BLAKE3 244 MB/s vs SHA-256 55 MB/s** per core. SHA-256 would be slower than the 60 MB/s LAN link. **BLAKE3 kept** (`zeebo/blake3`).
- [x] Tests: 9 store tests + 1 HTTP flow test. They **pass on Windows and on the NAS itself** (cross-compiled `go test -c`, run with `TMPDIR` on the data volume).
  The symlink-escape test only runs on the NAS (Windows can't create symlinks without privileges). `docs/protocol.md` written. `spike/nas` deleted.
- Carry-overs:
  - **P3**: add the tsnet + LAN listeners to main. Move `~/spike/tsnet` to the server's state dir (outside the share, e.g. `~/.aetheris/tsnet`) so the node stays `aetheris`.
  - **P6**: start script sets `umask 002`, so SMB users can edit files Aetheris creates (code uses 0664/0775).
  - Known gap: Unicode NFC/NFD names aren't normalized. Fine for Windows-only clients; revisit if a Mac client ever appears.

### P3 Auth & security
- [x] `internal/auth`: state in `-state` (default `~/.aetheris`, outside the share): `session.key` (Ed25519 seed, 0600) and `auth.db`.
  - CLI: `aetheris setup` prints `https://drive.example.com/setup#<token>` (15 min, one use). `aetheris reset` wipes passkeys and sessions. `aetheris pubkey` prints the key for the gateway.
- [x] Passkeys via `go-webauthn` v0.18.2: discoverable credentials, **user verification required**, clone warning rejected, existing passkeys excluded on re-register.
- [x] Sessions as above. Device pairing: 8-character code (no 0/O/1/I), 10 minutes, one use. Claims capped at 10/min, and 5 wrong guesses void all open codes.
  Sessions and passkeys can be listed and revoked; the last passkey can't be removed.
- [x] `tailnetGate`: tsnet `WhoIs` admits untagged devices and `tag:gateway` only. **Verified live:** the Vercel gateway passes.
  Tailnet `:443` and LAN `:8443` listeners in `main`. `-dev` (plain HTTP) is loopback-only.
- [x] `api.Secure` sets nosniff, no-referrer, no-store, CSP `default-src 'none'; frame-ancestors 'none'` and HSTS on everything.
  Files download unless `inline=1` **and** the type is a safe preview (fixed type, CSP sandbox, PDF without the sandbox). HTML and SVG are never inline.
- [x] Audit log covers setup, passkey add/remove/reject, login/fail, logout, pairing, revocations, deletes and restores. Source is the tailnet device name, or "via gateway" + client IP.
- [x] Tests (all also pass **on the NAS**):
  - forged, tampered, expired, wrong-key and revoked tokens are refused
  - the passkey flow uses a **software authenticator** (real CBOR/COSE/ECDSA), covering: no setup link, made-up link, link reuse, CSRF `403`, logout, unknown passkey, and an **impostor with the right credential ID but the wrong key**
  - pairing brute-force lockout and device revocation
  - HTML/SVG never inline
- [x] Live on the NAS:
  - with no passkey yet, tailnet and LAN answer `401`, and so do a forged bearer token and a guessed pairing code
  - all security headers are present
  - **Bug found and fixed:** subcommand parsing (Go evaluation order with `append` on `os.Args`)
  - `~/spike/tsnet` was moved to `~/.aetheris/tsnet` and the node is still `aetheris`. The server is stopped again until P6.
- Carry-overs:
  - **P4**: the gateway verifies tokens with `aetheris pubkey` run on the NAS, and Vercel Firewall rate-limits `/api/v1/auth/*` and `/api/v1/devices/claim`.
  - **P5**: setup page at `/setup#token`. Encourage a second passkey right after setup.

### P4 Vercel gateway (production)
- [x] `gateway/` (module `aetheris/gateway`) replaces the spike. The Vercel project `aetheris` now has root `gateway/`, the Go preset, `fra1`, and ignored build step `git diff --quiet HEAD^ HEAD -- .`.
  Env: `AETHERIS_PUBKEY` = `GAyS2r/wWI+1Arl8D033f8cN1K10zvnWpi1Llvmpul0=` (plain, public by design), `TS_AUTHKEY` (Sensitive).
- [x] Pre-filter (`filter` in `gateway/main.go`, 14 table tests):
  - only `/api/v1/`, no encoded or dot segments
  - bearer tokens refused (device tokens are LAN/tailnet only)
  - `devices/claim` unreachable
  - sign-in/setup endpoints open; everything else needs a validly signed, unexpired cookie
  - fails closed without a public key
  - `X-Forwarded-For` comes from Vercel's `X-Real-Ip`
  - a `tskey-client-…` secret gets `?preauthorized=true` (ephemeral by default)
- [x] Static web UI: Vercel serves `gateway/public/**` from its CDN. P5 builds the UI into it.
- [x] Domain `drive.example.com` added to the project, verified, and aliased to production.
- [x] **Verified end to end**: `GET /api/v1/auth/status` on the deployment reached the NAS through filter → WireGuard → `tailnetGate` → auth (`{"hasPasskeys":false}`).
  The Vercel connector **cannot edit firewall rules** (403), so those become dashboard steps below.
- [x] (you) Namecheap: `CNAME` host `drive` → `468900872aee068b.vercel-dns-016.com.` Resolves; Let's Encrypt cert issued by Vercel.
- [x] (you) Tailscale OAuth client secret in `TS_AUTHKEY` (updated 2026-11-03).
  Production redeployed (`dpl_8ntK8QUgz24F8hdvehqGP9EWCfvz`), and a fresh instance joined and reached the NAS.
  **Lesson:** the ignored-build-step command cancelled the redeploy after the env change (the newest commit didn't touch `gateway/`), so it was **removed**. Extra builds are cheap; silently stale env is not.
- [x] (you) Firewall rules active (verified via API): "Sign-in rate limit" (30/min/IP, deny 5 min) and "API flood limit" (1200/min/IP, deny 1 min).
  Vercel's managed CRS (gen, rce, sqli, xss) is on in log mode.
- [x] (you) Spend Management cap (reported done; the connector cannot read billing).
- [x] **Verified through `https://drive.example.com`** (2026-11-03):
  - `auth/status` reaches the NAS (0.56 s first, 0.27 s warm, both incl. a fresh TLS handshake)
  - no session `401`, forged cookie `401`, bearer `403`, pairing `404`, encoded slash `400`
  - headers appear once each
  - the audit source reads `web via gateway, client 84.133.122.86`
  - non-API paths get the placeholder text until P5 serves the app from `gateway/public/`
- Note: the NAS server is still running by hand (test data in `~/spike/data`) so the domain has something to reach. P6 replaces it with the service.

### P5 Web UI
Decisions made in P5:
- **Hash routing**: `/#/files/<path>`, `/#/trash`, `/#/settings`. The setup link is `/#setup=<token>`, and the token never reaches a server log.
  Every page is the static `index.html`, so no Vercel rewrites are needed.
- The build is **committed** into `gateway/public/` (`pnpm build` in `web/`). That's deterministic, with no multi-language build on Vercel. **Rebuild before pushing UI changes.**
- **No NAS-embedded copy of the UI**: passkeys are bound to `drive.example.com`, so another origin couldn't sign in. (Drops the "same build embedded in the NAS" idea.)
- App headers come from `gateway/vercel.json`: a strict CSP (`script-src 'self'`; `style-src` allows inline for Svelte's style bindings), nosniff, no-referrer, and a Permissions-Policy that only allows passkeys. Assets are `immutable`.
- Local dev:
  - `.dev/aetheris.exe serve -dev 127.0.0.1:8080 -rpid localhost -origins http://localhost:5173` against `.dev/data`, plus `pnpm.cmd --dir web dev`
  - `.dev/devsession.exe -state .dev/state` mints a cookie for the browser pane
  - `pnpm.ps1` is blocked by the execution policy, so use `pnpm.cmd`
  - The preview tool reads a stale launch config, so run the servers in terminal tabs

- [x] App shell: glass sidebar (bottom tab bar under 760 px), toolbar with scroll-edge mask, light/dark from tokens. Sign-in/setup screen with the full logo; "NAS unreachable" screen with retry.
- [x] Files view:
  - breadcrumbs (they wrap on phones), search (debounced), folders first
  - keyboard: ↑↓, Enter, F2, Delete, Backspace = up
  - context menu that grows from its trigger, inline rename (selects the name without the extension), new folder
  - drag-and-drop anywhere
  - **live refresh** via `GET /api/v1/changes/head` + long-poll (an outside file appears without a reload)
- [x] Uploads: 4 MB chunks (the gateway limit), 3 files in parallel, resume after drops, name clash → "a (1).txt".
  Verified: a 9 MB upload is byte-identical (SHA-256) with its mtime preserved.
- [x] Preview sheet:
  - spring with damping ratio/response (`web/src/lib/spring.ts`)
  - 1:1 drag with the grab offset, rubber-band up, momentum projection to dismiss, slight bounce only after a flick
  - exits the way it came; strongest blur
  - image, video, audio, PDF (iframe), text (first 1 MB via Range)
  - verified, including drag-to-dismiss
- [x] Trash (restore; the conflict message names the clash). Settings:
  - passkeys (add; remove is disabled for the last one)
  - device pairing code with a live countdown, device and browser sessions with revoke, sign out
  - activity log with readable labels (restore now logs the path, not the ID)
- [x] PWA manifest and icons. Bundle **27 KB gz JS + 3.7 KB CSS** (budget 60 KB).
- [x] **Deployed**: `drive.example.com` serves the app from the CDN with no console or CSP errors. The NAS server was updated.
- [x] **(you) First passkey registered** 2026-11-03 ("Windows PC", via the gateway). The NAS audit log shows real use through `drive.example.com`: a 5.4 MB upload (2 chunks), delete and restore of a Unicode-named file, a pairing code.
  Still to do: a second passkey (phone) under Settings.
- [x] **Large downloads work through the gateway** (2026-11-03): you downloaded the 5.4 MB `Würfel_struktur.3mf` at full size.
  Streamed responses are **not** capped at 4.5 MB (only request bodies are), so **no Service Worker needed**.
  Remaining ceiling: one request may run at most 800 s. At typical home upload speeds, single remote downloads of several GB could hit it.
  If that ever happens, the fix is the ranged Service Worker. The PC client never goes this way.
- [x] **Extras, 2026-11-05** (tested in the browser pane, server tests also on the NAS, deployed as `9264be2`):
  - **Version history** dialog (native modal): current version plus earlier ones, each with **Download** (`GET /api/v1/versions/file`) and **Restore**.
    Verified: restoring brings back the old text, and the replaced content becomes a version.
  - **Recent** view (`GET /api/v1/recent`, the 100 newest files; migration 2 adds an `mtime` index).
  - **Grid view** (list/grid toggle remembered per browser) with **thumbnails** from `GET /api/v1/thumb`:
    - JPEG ≤ 320 px, cached per content hash in `.aetheris/thumbs`, EXIF orientation applied
    - one decode at a time, images over 50 MP skipped
    - **12 MP photo on the J1900: 1.3 s first, 0.4 ms cached**
    - HEIC/huge/broken files fall back to the icon
    - *Shortcut:* the first view of a large photo folder fills slowly; pre-generating during `Scan` is the upgrade if that bothers you
  - **Folder upload**: drag a folder (`webkitGetAsEntry`, empty folders kept) or "Upload folder" (`webkitdirectory`). Existing folders merge.
    Verified with `Trip/Day 1|2/…`.
  - Toolbar: below 1100 px the secondary buttons go icon-only (with `aria-label`), so the breadcrumb stays readable.
  - Bundle **29.8 KB gz JS + 4.2 KB CSS**.
  - Dev note: Vite can serve a stale Svelte style transform after edits; restart it with `--force` if CSS changes don't show.
- [x] **Redesign "Quiet Sculpture"** (2026-11-07, `apple-design` skill, your `docs/design-language.md` + `assets/brand/showcase.html`). Built into `gateway/public`; live since `3b13d10`.
  - Framed porcelain stage on a seashell surround; rail = logo mark, coral "+" (upload files / folder, new folder), 4 destinations on a spring-tracked indicator (moves on press).
    Phones: full-bleed sheet, glass tab bar, floating "+", "‹ Parent" back link above the title.
  - Header: display title with faint ancestor crumbs, three icon-only tools (search pill that opens with `/`, gallery/list, Details). No column headers, no toolbar buttons.
  - Gallery is the default: nest folders, framed prints (thumbnails), paper sheets (PDF ribbon), a disc for audio, a film plate for video (`lib/Visual.svelte`). List: miniature, name, muted size.
  - **Inspector** (closed by default, glass, slides in from the right): kind with extension, size, modified, location, and Download / Versions / Rename / Trash.
  - **Quick Look** grows from the opened tile and returns to it (0.94 / 0.32 s); drag-to-dismiss keeps 1:1 tracking, rubber-band and projection, and can be caught mid-close.
  - Uploads: one ring + count; tap for the list; failures open it and stay until dismissed.
  - Verified in the browser pane: light, dark, 1200 px, 375 px phone; Inspector, add menu, list view, Quick Look open + drag-dismiss, Settings, Trash; desktop window (light, dark, busy, unpaired) with the mock bridge. No console errors. **29.8 → 38.3 KB gz JS** (budget 60), fonts ~85 KB.
  - Not taken from the spec: **Pinned** (the web has no pin state; Settings took the slot), the **storage hairline** (no capacity API yet), pin beads/rings. Each needs a server endpoint first. (Drag-a-tile-into-a-folder came later the same day.)
  - Dev data: `.dev/data/Design samples/` (jpg, png, wav, pdf, svg) shows every visual kind.
- [x] **File manager + share links** (2026-11-07, `apple-design` skill). Tested: 3 new server tests (also tamper cases), gateway filter cases, Node decrypt of a Go-sealed 4-chunk stream (byte-identical), and the UI in the browser pane. **Live 2026-11-07** (`1b95ce8`: NAS first, then the gateway).
  - **Multi-select**: click, Ctrl/⌘-click, Shift-click ranges, Ctrl+A, Shift+arrows, Escape. A **Select** tool (check circles, taps pick) for phones. Two or more selected → a glass **dock** at the bottom: download, copy, cut, compress, trash, Done.
  - **Context menu** on objects, the selection and the empty background: Quick Look (Space), Share link…, Download / Download as zip, Copy, Cut, Paste into folder, Duplicate, Compress, Rename, Version history, Get info, Move to Trash. Shortcut hints on the right; it blooms from the pointer and stays on screen.
  - **Copy / cut / paste** (`lib/clip.svelte.ts`) survives folder changes; the dock offers "Paste here" / "Move here". Cut objects dim until pasted. Name clashes keep both (`name (1)`), as uploads do.
  - **Drag onto a folder** (tile, row, or an ancestor in the title) moves; Ctrl (⌥ on a Mac) copies. Several objects drag as a small stack with a count. Internal drags never trigger the upload overlay. Touch has no drag: phones use cut/paste.
  - Server: `POST /copy` (files hashed as they copy, dates kept), `POST /compress` (a zip next to them; media stored, the rest deflated), `POST /zip` → one-use URL that streams a zip (folders and multi-downloads).
  - **Quick Look for every file**: kinds the browser can't show get the object, its kind and a Download button instead of a silent download. More text/code types show as plain text (`.xml .yaml .js .py .go .sql .srt …`), plus `.bmp .ico .ogv .aac`. HTML and SVG stay download-only. Android's Chrome has no inline PDF viewer (`navigator.pdfViewerEnabled`), so it gets Open + Download.
  - **Share links** (`docs/protocol.md` → Share links): views (none/once/5/no limit), downloads (same), expiry (1 h – 30 days); system share sheet (`navigator.share`: Android's menu), Email (opens the mail app with the link), Copy. Active links in Settings with Copy/Remove; views, downloads, creation and removal in the audit log.
    Encrypted with AES-256-GCM under a key that lives only in the link's `#fragment`: **the gateway and Vercel's logs see the id and ciphertext only**. The NAS keeps the key, so this protects the Vercel path, not files from the NAS. The recipient page (`Shared.svelte`) needs no account and loads no thumbnails (no names in URLs).
    *Shortcut:* the recipient's browser decrypts the whole file into memory (fine to a few hundred MB; multi-GB needs a ranged Service Worker). View-only stops the download button, not screenshots.
  - Not built: "Move to…" folder picker (cut/paste covers it), folder share links, marquee (rubber-band) selection, email sent by the server (no mail service; the mail app sends it).
  - `vercel.json` CSP: `frame-src` now allows `blob:` (the decrypted PDF of a share). Bundle **38.3 → 47.6 KB gz JS** (budget 60).
  - **Deploy order: NAS server first** (`scripts/deploy-nas.sh`; index migration 3 adds `shares`), then commit + push (gateway filter opens `GET /api/v1/shared/*`, CSP, UI).
- [x] **Drive-style redesign** (2026-11-10, `apple-design` skill, your call: closer to Google Drive, Material You icons). **Live 2026-11-10** as `5a156a0` (web only, no NAS change).
  - Shell: logo + wide search pill on top (searches the whole drive, `#/search/<text>`, Escape returns to the folder, `/` focuses it), "New" button and My files / Recent / Trash / Settings in the sidebar (spring indicator kept), rail under 1000 px, bottom navigation bar + floating "+" on phones.
  - My files: breadcrumb path (drop targets kept; the current folder opens the New menu), list/grid segmented toggle, details toggle. A Type filter chip, and in its place the **selection bar** (share, download, copy, cut, trash, more) once something is selected. List = Drive's table (Name / Modified / File size, sortable, folders always first). Grid = "Folders" chips, then "Files" cards with the name on top and the **real thumbnail** (cover) below, or a big type icon.
  - Details as a column (floats over the files under 1100 px). Busy/clipboard as a bottom-left snackbar; uploads as Drive's card (bar for the batch, ring per file, failures stay until closed).
  - Recent and Trash as tables with Location / date / size; Settings in outlined cards.
  - Verified in the browser pane (dev server): light, dark, 1280 px, 375 px phone; grid, list, sort, selection bar, context menu, details, search, Quick Look from a thumbnail, uploads (done + a refused `CON.txt`), Recent, Trash, Settings; the app window mock still renders with the new icons. Bundle **47.6 → 58.0 KB gz JS** (budget 60: little room left; the next feature may need the icon paths trimmed).
  - Removed: `lib/VisualDefs.svelte` (gradients of the old visuals), `.wrow` rows.
- [x] **Quick Look steps through files** (2026-11-10, your request). ← / → buttons beside the card (over its edges on narrow screens) and the arrow keys go to the previous / next file of the open view (folders skipped, no wrap-around, "4 of 5" in the caption); the new file slides in from that side. **Double-clicking an object behind the card shows it at once.** A single click beside the card still closes it, immediately on empty space, after 300 ms over an object (it may be the first half of a double-click; a slower one still catches the closing card). The background selection follows, so closing returns into the file that's showing. Recent gets the same arrows and now opens every file in Quick Look (it used to download unknown kinds). One card size for every kind now, so stepping from a photo to a song doesn't resize it under the arrows. Objects carry `data-path`, which is how Quick Look finds them. Verified in the browser pane (keys, buttons, a real double-click on a tile behind it, single-click close, text files while stepping, Recent). **Live 2026-11-10.** Not built: swipe left/right on phones.
- [ ] Still open in P5: list virtualization for 10k+ items in one folder, a full screen-reader pass.

### P6 NAS packaging & deploy
Findings: QTS 4.3.4 has **no `su`**, and BusyBox has **`start-stop-daemon`** (can switch user before exec).
`/etc/config/crontab` was **world-writable** (`rw-rw-rw-`): any local account could get root. Not used; the boot hook closes it.
QPKG dropped (needs QNAP's dev kit). The persistent root crontab plus a watchdog is simpler, and it also covers crashes and the late-mounting data volume.

- [x] `scripts/deploy-nas.sh` (Git Bash on the PC):
  - cross-compile with a `git describe` version stamp (`web/` is not embedded, see P5)
  - upload to `~/aetheris/`, keep `aetheris.prev`, restart
  - health check on LAN `:8443` for 60 s; **automatic rollback** otherwise
  - `AETHERIS_BINARY=<file>` installs a given file
  - **Rollback verified** with a deliberately broken build: back on `cb2eb1e` in 142 s, domain up
- [x] `scripts/nas/start.sh`: single instance (`pidof`), `umask 002`, log `~/aetheris/aetheris.log` rotated at 5 MB (one old copy).
  Runs `serve -data /share/Aetheris -state ~/.aetheris`.
- [x] **Switched to the real share**:
  - your test files moved into `/share/Aetheris` and made group-writable
  - `~/spike` deleted
  - new index in `/share/Aetheris/.aetheris` (`700`, new epoch)
  - auth state, passkey and tailnet node kept
- [x] Running `cb2eb1e` as **`aetheris` (uid 1001)**: RSS 37 MB, CPU ~3 % during the first scan, no warnings. Tailnet and `drive.example.com` answer.
  Harmless log noise: tsnet's `SO_BINDTODEVICE … not permitted` (root-only socket option, it falls back).
- [ ] **(you, as admin)**: `cmd /c "ssh admin@192.168.1.20 sh -s < scripts\nas\install-boot-hook.sh"` in the repo folder.
  - It installs the root-owned `/etc/config/aetheris-watchdog.sh` and a once-a-minute line in the root crontab.
  - It sets `chmod 644 /etc/config/crontab`.
  - Root never runs Aetheris code: `start-stop-daemon -c aetheris` switches user first.
  - Claude's safety check blocks Claude from installing root persistence, so you run it.
  You ran it 2026-11-03 22:56, and the NAS restarted at 23:15. **The watchdog never started the server**: on QTS `start-stop-daemon` is only a BusyBox applet with no command on cron's PATH.
  The site was down from then until 2026-11-05, when it was restarted by hand.
  QTS also **rewrote `/etc/config/crontab` as `rw-rw-rw-`** at 23:20 (our line survived).
  Fixed in the installer: `/bin/busybox start-stop-daemon`, absolute paths, and the watchdog re-applies `chmod 644` every minute. **Needs one more admin run.**
- [x] **Watchdog verified** 2026-11-05 after your second admin run:
  - killed the server, and the root watchdog restarted it **after 28 s as `aetheris` (uid 1001)**
  - `/etc/config/crontab` stays `644`
  - the domain answers
  - The boot case uses the same path (plus waiting for the volume). An optional reboot test is yours to schedule.

### P7 Windows client
- [x] `client-win/` skeleton: .NET 10, CsWin32 0.3 (cfapi, DPAPI, CreateFile; `allowMarshaling: false`), the SDK's WinRT projection (StorageProvider). Console exe for now: `Aetheris pair <code> | run | unregister [--dev]`.
  - [x] Single-file publish installed to `%LOCALAPPDATA%\Programs\Aetheris` by `scripts/install-client.ps1` (quits the running copy gracefully, then starts the new one). 26 MB, framework-dependent.
  - [ ] Native AOT (smaller, no .NET runtime needed); needs the MSVC linker on the build machine.
- [x] Pairing flow (code from Settings in the web UI), DPAPI token (`%LOCALAPPDATA%\Aetheris\<profile>\token.bin`, renewed from `X-Aetheris-Token`), endpoint selection (LAN → tailnet).
  - [x] Verified against the NAS over the LAN (2026-11-05): 55 files, placeholders in 109 ms, SHA-256 matches, NAS create/delete reach the PC in ~3 s, offline retries with backoff.
  - [ ] Tailnet route: untested, Tailscale was stopped on the PC. Start Tailscale, run with `AETHERIS_LAN=192.0.2.1:8443` (unreachable), open a file.
- [x] Register the sync root (icon, nav pane). Create placeholders from `/tree`. Apply `/changes` (put, mkdir, move, delete; 410 → resync). Reconcile on start.
- [x] Hydration (`FETCH_DATA`) with rev check, cancel. Logs which process asked for each file.
- [x] **Parallel ranged requests (3 parts for files ≥ 16 MB)**: Windows' TLS (SChannel) offers no ChaCha20, so the NAS runs AES-128-GCM in software (no AES-NI) at **~17 MB/s per connection** (measured on 107 and 378 MB files; client writes take < 3 % of the time, the NAS disk reads 216 MB/s).
  2026-11-06 over the LAN: 52 MB in 3.27 s with one connection (16 MB/s) → 55 MB in 1.2–1.4 s with three (40–46 MB/s). SHA-256 matches. One part failing stops the others and fails the whole range.
- [x] **Folders from this PC, setup, installer, updates** (2026-11-16, your request, `apple-design` skill). Tested on the dev server with a test folder: first sync, local edit/new/rename/delete, NAS new/edit/delete (downloaded at once), changes on both sides while the app was off, a 285-character path, a junction (skipped), the NAS dropping the folder (stopped syncing, files kept as plain files), adding it again with the NAS copy present (7 items adopted, no conflict copies), and the Aetheris folder unchanged both ways. Refusals: a Windows folder, a folder inside the Aetheris folder.
  Installer and updater tested on the dev flavor (`AetherisSetup.exe --dev` into `Programs\Aetheris Dev`, removed afterwards): install, reinstall over files from the old unpackaged install, the app window from the single exe, and a full self-update 1.0.0 to 1.0.1 from a local fake release (release URL and dev check changed for that test only). Web: Computers section in the browser pane (desktop, phone, light, dark); app window steps with the mock bridge.
  **Not tested**: real Desktop/Documents (your real profile was left alone), Explorer's pane with several packaged sync roots, the real GitHub release path (needs your token), a folder inside OneDrive.
  - [ ] **(you)** Make `realCmdData/aetheris` public, or updates can't be found. Before that, decide what may be public: PLAN.md (and its git history) names your home IP, LAN IP, tailnet name and email.
  - [x] Releases (2026-11-16): **v1.0.0** (your tag, `de8bc33`) and **v1.0.1** (`078f0cc`, adds the public README; same app). The workflow ran in about 2.5 min each; `AetherisSetup.exe` is 44.8 MB, and the v1.0.0 download matched its SHA-256 and reported version 1.0.0. GitHub warns that four actions still target Node 20 (they run on Node 24 anyway).
  - [ ] Install `AetherisSetup.exe` (v1.0.1) on this PC: it replaces the current install, keeps the pairing, and fixes the stalled uploads. The repo is private, so download it while signed in to GitHub.
  - [ ] Once the repo is public: an installed v1.0.0 should update itself to the newest release within a minute of starting.
- [x] **Self-hosting** (2026-11-16, your request): server `-origins` saved in state (no default; `rpid` from it), pairing answers carry `server`/`lan` (tsnet cert domain, LAN address from the routing table) and `web`; gateway `AETHERIS_NAS` or `aetheris.<MagicDNS suffix>`; web sign-in caption is the page's host, Settings › Pair a device shows Server and At home; Windows app connect screen asks Server + Code (address at home behind "Not using Tailscale on this PC?"); `deploy-nas.sh` takes `AETHERIS_NAS_SSH`/`AETHERIS_NAS_HOST`; README has "Run your own Aetheris".
  Your setup kept: `origins.txt` written on the NAS (`/share/homes/aetheris/.aetheris`), `server.txt` written on this PC (`%LOCALAPPDATA%\Aetheris\nas`). Tested: server setup with no/given/saved address (+ `TestWebOrigins`), gateway tests, dev pairing (right, wrong code, bad server name), the pairing card (fields injected: the dev server has no tailnet), the connect screen (mock), the taskbar icon with a dev-flavor install. **Not tested**: `CertDomains`/LAN address on the NAS (needs the deploy), the gateway's tailnet-name lookup on Vercel (it falls back to an error asking for `AETHERIS_NAS`).
  - [x] Deployed 2026-11-16 in this order: NAS server `v1.0.1-2-g9bf86ff` (no errors, RSS 44 MB, same gateway key); Vercel env `AETHERIS_NAS=aetheris.tail1234.ts.net` (production + preview), push `9bf86ff` (new bundle live, the API reaches the NAS, no session = 401); release **v1.1.0** (44.8 MB, download matched its SHA-256, version 1.1.0); installed here over 1.0.1: it read `server.txt`, compared 87,399 items with nothing to change and no errors, and the taskbar button shows the logo.
  - [ ] Pair a second device once to see Settings show the server and the address at home (the NAS now reports them; your passkey needed).
- [x] **A real setup** (2026-11-16, your report: the exe wasn't recognized as a program; there was no Start menu entry and the install was silent). `AetherisSetup.exe` now shows Windows' setup dialog (Install / Update and Cancel, *Add a shortcut to the desktop*; `--quiet` skips it), creates a Start menu entry (app ID `Aetheris`, the window's, so pins and the open window are one button; recreated at start if missing, e.g. after an auto-update) and the optional desktop shortcut; uninstall removes both. Tested with the dev flavor: dialog captured, Install and the checkbox pressed by their dialog messages, `Get-StartApps` lists it, shortcut target and arguments right; test install and shortcuts removed.
  - [ ] Release it (v1.2.0) and run its setup here: the dialog says *Update*, and Aetheris appears in Start.
- [x] **Your list of 2026-11-16 (evening)**, built and tested on the dev server, not deployed:
  - Web formatting: the table header's sort buttons were clipped on the left (their cells cut overflow; now only body cells do); times read "Today 8:47 PM", not "08:47 PM"; the sort menu no longer shows a check beside every option but the chosen one.
  - Sorting: Name, Modified, **Created**, File size, Type, and *Folders on top* (list; the grid keeps its Folders/Files sections); remembered per browser; the sort button shows in list view too. **Folders have sizes** (everything inside, from the index), so they sort by size. The list's date column shows Created when sorting by it; Details shows Created.
  - Created dates: server migration 4 adds `files.created` (unix ms; existing entries start at their modification time); commits take `created` (the Windows app sends the file's creation time), copies are born when copied, edits and moves keep it. Test `TestCreatedAndFolderSize`.
  - Folder icons: a synced PC folder's own icon (from its `desktop.ini`: Downloads' arrow) goes to the NAS as a PNG (`PUT /api/v1/icon`, kept in `.aetheris/icons`, test `TestFolderIcon`) and the web shows it under Computers. Found on the way: registering a folder as a sync root made Windows draw the Aetheris logo for it; it is now registered with its own icon, so Explorer keeps showing Downloads' arrow.
  - Long uploads: after 5 s of back-to-back uploads the status is "Long upload in progress" with an estimate (one background count of unsent files + the rate so far; the window shows files and bytes to go and an overall bar), instead of flicking to "Up to date" between files. Seen on 1,500 files.
  - **Free up space automatically** (app window, *On this PC*): Off / 7 / 14 / 30 / 60 / 90 days. A few minutes after start and every 6 h, files not opened (last access, else last change) for that long become online-only, in the Aetheris folder and the folders from this PC; pinned files, unsent edits and open files stay. In a folder from this PC the freed file is marked unpinned so it isn't fetched back; opening it brings it back and it stays. Seen: exactly the 6 backdated files freed, a recent one kept, an opened one stayed local. Windows records file opens on this PC (`DisableLastAccess = 2`); where it doesn't, the window says it goes by changes.
  - Web bundle **59.7 KB gz** of 60: the next web feature needs the icon set trimmed first.
  - [x] Deployed 2026-11-16: NAS server `v1.1.0-2-g788d2f3` (migration 4, no errors, RSS 43 MB), web live, **v1.2.0** released and installed here through its setup dialog (Start menu entry `Aetheris`).
- [x] **Found while installing v1.2.0: a conflict-copy loop in your synced Documents** (since you added it at 20:46). Rust's `target` folder (`Documents/…/app/target`) holds hard-linked build outputs; Windows' cloud files can't hold hard links (`0x8007018C`), so a file went up but was never marked as sent, went up again 30 s later, met itself on the NAS and became a conflict copy, hard-linked too: 786 copies (410 MB), all inside `target`, plus one identical copy in Downloads. Aetheris was quit to stop it.
  Fixed in **v1.2.1** (`8c868db`, released and installed 21:25): hard-linked files are skipped (logged once each; they stay on this PC only), and a re-upload of a file the NAS already has with the same size and time counts as synced. Checked after install: no conflict copies or uploads, 394 hard-linked files skipped, 422 NAS copies downloaded into `target`, quiet since.
  - [x] **Cleaned up** (your call: delete the identical copies, 2026-11-16 21:29). With Aetheris running all 788 copies could be read, and all 788 were byte-identical (SHA-256) to their originals: 410 MB. The 394 placeholder copies went to the **NAS trash** (Windows' Recycle Bin can't take cloud placeholders; Aetheris passed the delete on first, then removed them here). The 394 hard-linked plain copies went to **Windows' Recycle Bin**; their NAS copies went to the NAS trash at the next start (395 with the Downloads one). Left: 0 on this PC, 0 on the NAS. Lesson: recycling a placeholder fails with "not supported" after the provider has already deleted it.
  - [x] Web deployed 2026-11-16 (`de8bc33`, pushed to main): drive.example.com serves the bundle with Computers and the new download link, and the API reaches the NAS. Past sign-in not checked (your passkey). No NAS change was needed.
- [x] **Documentation** (2026-11-16, your request): `README.md` is now the index; pages in `docs/`: `windows-app.md`, `web-app.md`, `self-hosting.md` with six steps in `docs/self-hosting/` (Tailscale, server with systemd/NAS boot, Vercel gateway, first passkey, PCs, maintenance), `security.md`, `troubleshooting.md`, `development.md`. 17 screenshots in `docs/images` from a demo server with generated files (no personal data; the desktop mock now says DESKTOP-01). Fixed on the way: the old README kept the first `setup` link for step 4, but it expires in 15 minutes; the guide now runs `setup` again there. Private details were scrubbed afterwards (see the next item).
- [x] **Private details scrubbed for a public repo** (2026-11-16, your request): this installation's domain, LAN and tailnet addresses, tailnet name, PC and NAS names, Windows user path, login email, Vercel IDs, mail DNS and NAS sshd details are out of every tracked file. `PLAN.md` and the docs use example values; `CLAUDE.local.md` (git-ignored, loaded with `CLAUDE.md`) maps them back and keeps the removed facts word for word. `scripts/deploy-nas.sh` and `install-nas-key.ps1` read the NAS from `.dev/nas.env` (git-ignored); `.claude/launch.json` (absolute paths) is local now too. **Fresh start** (your choice): GitHub's `main` is now one root commit (`1079319`, "Aetheris 1.2.2") with the clean files; releases v1.0.0–v1.2.1, their tags and Actions runs are deleted (their binaries held the old values), and **v1.2.2** was released from the new commit. The old history is kept only in `.dev/history-before-public-2026-11-16.bundle` (all commits and tags; `git clone` it to look back). Commit hashes in this plan before that point refer to the bundle.
- [x] **Saves through a temp file made conflict copies** (2026-11-23, your report with a screenshot of the activity list). Claude Code, slicers and many editors save by writing `name.tmp…` and renaming it over the original. That leaves a plain file where the placeholder was, so the upload had no base revision, the NAS answered 409, and Aetheris made a conflict copy **and put the old NAS version back under the original name** at every save (seen 5 times on 2026-11-23 in your log). Fixed in `Local.Upload` + `Remote`: such a file is an edit of the NAS's current version when this PC had that version (its rev ≤ the change-feed cursor applied here, and no NAS change for that path was skipped because a plain file or an unsent edit stood in its place; at start, versions up to the last sync point count). Tested on the dev server with Node's write-temp-and-rename: two saves in a row went up as edits; with the app off, a file changed only here went up as an edit and a file changed on both sides still became a conflict copy (NAS version under the original name); the next save after that conflict was an edit again.
  - App window: an Activity entry expands on press (full name, folder, conflict copy name, exact time).
  - **Export support log…** (Account, `Support.cs`): a zip in `%LOCALAPPDATA%\Aetheris\<profile>\support\` with a summary (version, Windows, status, route, server, folders, transfers), the recent activity, the profile's `*.txt` settings and the newest 8 MB of `aetheris.log` and `.old`; never `token.bin`. Explorer opens with it selected. Tested in the dev app window through UI Automation (WebView2's DevTools port can't open while the installed app holds the shared `webview` folder).
  - [x] Released as **v1.2.3** (2026-11-23, your call). The repo is public, so installed copies update themselves (a minute after start, then every 6 h).
  - [ ] Install 1.2.3 on this PC (Aetheris is quit since 2026-11-23 17:1x; see the session log) and see it report 1.2.3 under Account.
- [ ] Progress UI check for big files; the cancel path (Windows only cancels from its own progress dialog: killing the reading app doesn't, the Full policy finishes the download).
- [x] Pin/unpin (2026-11-06). Explorer only flips the attribute; the client acts on it (the folder watcher includes attributes, and the NAS side hands it every placeholder it creates):
  "Always keep on this device" downloads the file (2 at a time), and files that later arrive in a pinned folder too. "Free up space" dehydrates once every edit is on the NAS, then resets the pin state, so a later open keeps the file (as OneDrive).
  A pinned file changed on the NAS: Windows refuses to dehydrate pinned files, so the client unpins for the update and pins again, which downloads the new bytes.
  Tested on the dev server (pin, free, pinned folder + new NAS file in ~4.5 s, NAS edit of a pinned file, free right after an edit: uploaded first) and on the NAS (freed all test downloads).
- [x] Local change detection → upload → conflict copies. Rename, move, delete both ways (2026-11-06).
  Dev server: new file, new folder with a file, edit (adds a NAS version), rename, move, delete (to the NAS trash), conflict while paused (both versions kept, notification), delete while offline (refused, file kept).
  NAS: folder + file created, folder renamed, folder deleted (to the NAS trash), no server errors.
  - [ ] Deletes and renames while the NAS is unreachable (app running) are refused (Windows shows "network unavailable") instead of queued.
  - [x] Deletes and renames while the app isn't running (2026-11-06): `synced.txt` keeps the last applied (epoch, cursor). At start, a NAS item with rev ≤ that cursor but missing here was deleted or renamed here: a placeholder elsewhere with its rev = rename (move on the NAS), else delete (NAS trash, plus a notification).
    Guard: more than 20 affected items (everything inside counts) and over 10 % of the tree = a wiped or moved folder: restore everything and notify instead.
  - [ ] A dropped connection restarts an upload from zero (resume via `GET upload/{id}`).
  - [ ] Restoring from the PC's Recycle Bin doesn't restore on the NAS: use the web UI's trash.
  - [x] Office safe-save (2026-11-06): Word renamed the file to `~WRL….tmp`, which moved it on the NAS, lost the version history and left the temp there. Now a rename to a scratch name leaves the NAS alone, the new file goes up as an edit of the original (versions kept), and the temp's delete only deletes the original if nothing replaced it. Word and Excel: each save = one version, no leftovers.
- [x] Tray icon and menu, notifications, pairing window, autostart (on after pairing, toggle in the menu), pause/resume, single instance. Tested on the dev server: wrong/right code, pause/resume, offline → back online, sign-out → pair again. On the NAS (2026-11-06): paired through the window, autostart set to the installed exe, LAN route, download matches by SHA-256.
- [x] App window + drive letter (2026-11-06). Designed in the browser pane (light, dark, busy, unpaired), then checked in the real window (`PrintWindow` of that window only: live state arrived, A: mapped). Mica itself can't be captured that way: **check it on screen**.
- [x] App window redesigned as the Quiet Sculpture plinth (2026-11-07): logo mark, state title in Canela, a hairline (full when synced, glint while comparing, true progress while files move), "Open Aetheris" with drive-letter and web circles, two switches (Start with Windows, Pause syncing). Storage, drive letter, activity and account sit below it. Window size and title-bar colours unchanged. **Rebuild + reinstall the client to see it** (`client-win/ui` was rebuilt locally).
- [x] Clean uninstall (2026-11-06): Aetheris is listed in Settings > Apps > Installed apps (HKCU Uninstall key, rewritten at every start).
  Uninstall runs `uninstall.ps1` next to the exe, which first runs `Aetheris uninstall`. That asks, quits the tray, counts files the NAS doesn't have yet, signs this PC out on the NAS, unregisters the folder (online-only files go, downloaded ones stay as plain files), removes autostart, the logo, the read-only flag, empty folders and the app's data, and says what stayed.
  The script then removes the package and the program files. It runs outside the package because removing a package ends that package's processes. It refuses to run from anywhere but the install folder: a build folder's copy would still act on the real profile.
  Tested on the dev profile: the downloaded file and an unsent new file stayed, everything else went, and the device session was revoked. **Not run on the real install** (it would sign this PC out and need a new pairing).
  - Debug builds get no package identity (`obj/dev.manifest` drops the `<msix>` line): with it, a build folder's exe ran as the installed app, and its file writes didn't reach `%LOCALAPPDATA%\Aetheris\dev`.
- [x] Tests on the dev server (2026-11-06):
  - **> 4 GB**: 4 GiB + 3 MiB + 123 bytes, every 1 MiB block numbered. Placeholder size exact, download (3 parts) and upload (16 MB chunks) both SHA-256 identical.
  - **10,000 files** (100 × 100): all placeholders 26 s after the server indexed them (via the changes feed); restart reconcile 1.1 s, 5 s CPU for the whole start, idle CPU 0.03 s per 10 s, 63 MB private / 116 MB working set; removal of the tree reached the PC in 7.7 s.
  - **Offline (app not running)**: edits and new files upload at start, an edit on both sides becomes a conflict copy, deletes and renames reach the NAS (see above), a wiped folder is restored.
  - Found and fixed in the server: on Windows the watcher could deadlock (fsnotify's Add waits for its reader, which waited for our loop); new folders are now watched from a goroutine. Linux (the NAS) adds watches directly and was never affected. Tests: `TestWatcherSeesNewTrees`, `TestWatcherSurvivesFolderDelete`.

### P8 Hardening, backup, polish
- [ ] Threat-model review against this plan. Fuzz path and name handling. Dependency audit.
- [ ] NAS: `/share/homes/aetheris` is `777` (QTS default; sshd doesn't enforce StrictModes there). Any other shell user could swap `~/.ssh` and take over this admin account.
  Make it `700` and recheck that QTS doesn't reset it. Switch sshd to key-only and drop `PermitRootLogin`.
- [ ] QNAP HBS 3 backup of the `Aetheris` share to USB or offsite. Test a restore.
- [ ] Optional: Tailnet Lock, code-signing certificate for the client, DMARC record for the mail domain.
- [ ] Performance pass: web LCP and bundle, NAS RSS/CPU, client idle CPU ≈ 0.

---

## Open items / risks
- **P1 can fail** (Vercel function cannot hold tsnet well, DERP-only throughput, cold starts too slow). The fallback is defined above.
- **QTS 4.3.4 / `genericModel`**: QPKG install, `/dev/net/tun`, and inotify limits are unverified. tsnet runs in userspace, so TUN isn't required.
- **Home upload bandwidth** caps every remote transfer. Nothing in the design can beat it.
- **Unsigned client** triggers SmartScreen until P8.
- **The J1900 is slow**: thumbnails are generated lazily and cached, and there is no video transcoding.
- **Dev server on Windows can't move folders** (`renameat … Access is denied`): its file watcher holds handles inside them. Linux (the NAS) is fine. Test folder moves on the NAS.
- **Windows hands the package identity to any exe with the `<msix>` manifest line** while the package is registered, and copies outside `Programs\Aetheris` then get a private view of `%LOCALAPPDATA%\Aetheris\...` (found 2026-11-16: the dev install looked unpaired, the installer's log line went elsewhere). Writes to `Programs\` are real, so installing works, and the installed copy is unaffected.
- **Git Bash/MSYS writes slip past the cloud filter**: Windows doesn't clear the placeholder's in-sync mark, so such an edit goes up only with the next normal write. Real apps are fine.
  **Seen for real 2026-11-23**: files copied back over the originals from Claude Code's Bash tool stayed newer on the PC than on the NAS with no upload. Until a normal write, a NAS-side edit or *Free up space* would replace them with the older NAS copy.
- **The installed 1.2.2 stalled on 2026-11-23** from about 10:20 until it was restarted at 17:03: no uploads, and no change-feed request (idle CPU, no connection to the NAS), while deletes from Explorer still reached the NAS. The pattern fits `Local.Gate` held by something that never returned; both loops wait on it. Restarted without capturing stacks (your call), so the cause is unknown. If it returns: `dotnet-stack report -p <pid>` before restarting, and the support zip should then also say what each loop is waiting on.
- **The Windows app's log rotates only at start** (`Log.Open`): it reached 36 MB between starts (2026-11-23), mostly hard-link notices repeated at every start. The support export takes the newest 8 MB.
- **Fixed 2026-11-16 (after v1.0.1)**: the window's taskbar button was a blank tile (with the package identity Windows took the package logo); the window now has an app ID of its own and its own icons. And a delete notice for a file mid-delete (`C:\$Extend\$Deleted\…`, our own delete) went unanswered, which can hold up whoever deletes for a minute; seen once in your log when you deleted node_modules folders (yours, in the NAS trash). Both fixed in v1.1.0.
- **Fixed 2026-11-16**: paths over 260 characters (deep `node_modules` in `All\lurkr-website`) made the uploader's direct `CreateFile` fail, and one failure stopped the whole upload pass. **Your installed copy (old build) has logged this every ~25 s since 2026-11-08 (7,228 times), so local changes behind that item may not have gone up.** The next install fixes it: the long-path form for Win32 calls, `longPathAware`, and a failing item now retries on its own.
- The web bundle is at **59.1 KB gz** of its 60 KB budget.

---

## Session log
- **2026-11-03, session 1**: Probed NAS (QTS 4.3.4, J1900), DNS (Namecheap), Vercel team, PC toolchain.
  User chose Vercel access, Files On-Demand, NAS-only web, single user. Wrote this plan and saved the logo. **Next: P0** (the "(you)" items first).
- **2026-11-03, session 1 (cont.)**: P0 Claude items: repo linked, marks + icon set + tokens + brand sheet, Go/.NET install.
  **Next**: the user's P0 items (Tailscale, NAS SSH/share/user, firmware check, stale A record, router), then the NAS facts over SSH, then P1.
- **2026-11-03, session 1 (cont.)**: User finished their P0 items. SSH key installed, NAS facts recorded (no TUN, stale CA store → embed roots, 8443 free). **P0 done. Next: P1 spike**, starting with the NAS tsnet test binary.
- **2026-11-03, session 1 (cont.)**: P1: NAS spike live on the tailnet, LAN vs tailnet measured (60.5 vs 42 MB/s), WhoIs works.
  Gateway deployed by the user (project `aetheris`) but the Vercel connector can't reach it. **Next**: widen the connector to `aetheris`, create a bypass secret, then measure the gateway (cold join, throughput, 4.5 MB limit) and decide go/no-go.
- **2026-11-03, session 1 (cont.)**: Connector widened. Gateway failed to join twice: instance freeze, then Tailscale's "network down" on Vercel's link-local interface. Both fixed.
  Measured: cold join 158 ms, warm 9 ms, thawed 55 ms, direct path, 193 Mbit/s NAS → Vercel. **P1 done: GO.**
  **Next: P2 server core**, starting with the `server/` skeleton and the storage layer on `os.OpenRoot`.
- **2026-11-03, session 1 (cont.)**: P2 done. BLAKE3 chosen (4.4× SHA-256 on the J1900). `server/` built with store, API, scanner and watcher. 10 tests pass on Windows and on the NAS.
  The watcher was verified live on the NAS. `docs/protocol.md` written.
  **Next: P3 auth**: first-run setup + passkeys, then sessions, device pairing, the tsnet/LAN listeners and safe file serving.
- **2026-11-03, session 1 (cont.)**: P3 done. `internal/auth` (passkeys, signed sessions, pairing, audit), safe previews, tailnet gate, listeners. Recovery codes dropped by design.
  Security tests use a software passkey and pass on Windows and the NAS. Live probes all `401` without a passkey; the gateway passes the gate. Fixed an `os.Args` parsing bug found live.
  **Next: P4 production gateway**: proxy `/api/*` with the token pre-filter, the `drive.example.com` DNS record, firewall rate limits, and a Tailscale OAuth client instead of the 7-day key.
- **2026-11-03, session 1 (cont.)**: P4 done. `gateway/` live on `drive.example.com` with the session pre-filter, Tailscale OAuth secret, firewall rate limits and DNS.
  Verified end to end through the domain. Removed the ignored-build-step command after it cancelled an env-change redeploy.
  **Next: P5 web UI**: Svelte 5 + Vite into `gateway/public/`. Start with the sign-in/setup screens so the first passkey can be registered at `/setup#token`, then the file browser.
- **2026-11-03, session 1 (cont.)**: P5 first slice built, tested in the browser pane (dark, light, phone) and deployed: sign-in/setup, files, uploads, live refresh, preview sheet, trash, settings.
  Accidentally committed the P1 spike binary (`.gitignore` entry replaced) and removed it in the next commit; it stays in history (21 MB, no secrets).
  **Next**: you register the first passkey; then check large downloads through the gateway, then versions/thumbnails/folder upload, or move on to P6 (NAS service on `/share/Aetheris`).
- **2026-11-03 → 05, session 1 (cont.)**: passkey registered, large downloads verified. P6 done: real share, `deploy-nas.sh` with verified rollback, root watchdog via crontab + `busybox start-stop-daemon`.
  The first watchdog was broken (applet not on PATH), which caused ~2 days of downtime after a NAS restart. It's fixed and verified with a kill/restart in 28 s.
  QTS keeps resetting the crontab to world-writable; the watchdog re-tightens it.
  **Next**: P5 extras (versions panel, thumbnails/grid, folder upload, Recent) or P7 (Windows client). Optional: a NAS reboot test.
- **2026-11-05**: Web extras done and deployed: version history, Recent, grid with thumbnails, folder upload.
  `git add -A` also committed your two logo SVGs from the repo root (`aetheris-stacked.svg`, `aetheris_standalone_vector_logo.svg`). Ask what they're for.
  **Next: P7 Windows client**, or virtualization/screen-reader polish.
- **2026-11-05 (cont.)**: P7 started. `client-win/` pairs, registers the Explorer cloud folder, creates placeholders, downloads on open, and follows `/changes`.
  Tested against the dev server: hashes match, only opened files download, a stale placeholder refuses to open, NAS edits/creates/moves/deletes show up locally, and restarts catch up without touching PC-only files.
  `Stop-Process -Name Aetheris` also kills the dev server (same exe name): stop the client by path.
  **Next**: pair with the NAS (`client-win\bin\Debug\net10.0-windows10.0.19041.0\Aetheris.exe pair <code>`) and verify the LAN/tailnet routes, then local changes → upload.
- **2026-11-05 (cont.)**: Client paired with the NAS and verified over the LAN: 55 files (1 GB) as placeholders, downloads match by SHA-256, NAS changes arrive in ~3 s, offline is handled.
  Big files download at only ~17 MB/s: SChannel has no ChaCha20, and the J1900 has no AES-NI. Tailnet route untested (Tailscale was stopped on the PC).
  The client runs in a console (`Aetheris.exe run`); files only open while it runs.
  **Next**: parallel ranged downloads, then local changes → upload; tray/autostart after that.
- **2026-11-06**: Tray app built and installed (`scripts/install-client.ps1` → `%LOCALAPPDATA%\Programs\Aetheris`): status menu, pairing window, notifications, autostart, pause/resume, one instance.
  All states tested on the dev server by window messages (no clicks on your screen). Status after an error recovers at once (the first poll after an error doesn't wait).
  The `run` command is gone: start `Aetheris.exe` (the tray). The NAS device was revoked on 2026-11-05, so the installed app asks to be paired.
  **Next**: you pair from the tray (Pair this PC…); then parallel ranged downloads, then local changes → upload.
- **2026-11-06 (cont.)**: You paired through the tray. Installed app verified on the NAS (LAN, autostart, download matches).
  `_preview_not_for_upload_2.png` is missing on the PC because you deleted it there (not a bug). PC-side deletes don't sync yet, so the next start puts the placeholder back.
- **2026-11-06 (cont.)**: PC → NAS sync built and installed: uploads (new, edited, new folders), conflict copies, deletes and renames through the Cloud Files callbacks. Tested on the dev server and the NAS.
  Fixed on the way: a NAS move of an edited file could have cut it to the NAS size (identity-only update now), a rename made in-sync files re-upload, and our own delete echoing back logged errors.
  Six "Aetheris client test" folders from the tests are in the NAS trash (gone after 30 days).
  **Next**: parallel ranged downloads (17 MB/s now), then pin/unpin, then the tailnet route test.
- **2026-11-06 (cont.)**: Parallel ranged downloads: 3 connections for files ≥ 16 MB, 16 → 40–46 MB/s over the LAN.
- **2026-11-06 (cont.)**: Pin/unpin works. `SyncRoot.Open` now throws instead of returning an invalid handle (which read as "not a placeholder"). All test downloads on your PC are online-only again.
  **Next**: the tailnet route test (start Tailscale on the PC first), then the P7 tests (offline edits, 10k-file tree, > 4 GB file), the Office safe-save check, and an uninstaller.
- **2026-11-06 (cont.)**: P7 tests done on the dev server: > 4 GB both ways, 10,000-file tree, offline edits/deletes/renames, Word and Excel saves. Fixed: Office safe-save (versions kept, no temp leftovers), deletes/renames while the app is off (synced point + wiped-folder guard), dev-server watcher deadlock on Windows.
  Installed; the real folder now has its sync point. **Open in P7**: tailnet route test (start Tailscale), uninstaller, Native AOT, progress UI check, queueing deletes/renames while the NAS is unreachable, resumable uploads, PC Recycle Bin restore.
- **2026-11-06 (cont.)**: Drive letter (Aetheris is `A:` in This PC), the app window (WebView2 + Mica, Svelte interface from the web design system, pairing included), and your logo SVG in both UIs (web sign-in, sidebar, empty states; app header and pairing).
  Fixed on the way: an emptied folder with a leftover sync point could have deleted ≤ 20 items on the NAS (now an empty folder is always a fresh start; `unregister` forgets the sync point).
  **Next**: you look at the window (tray left-click) and the Mica on screen; then the uninstaller, Native AOT, the tailnet route test.
- **2026-11-06 (cont.)**: Your feedback: the window was unreadable (Mica didn't apply, dark text colours on white) and A: looked like archived files. Fixed: solid themed window with a matching title bar; drive letter off by default (A: removed); the OneDrive-style Aetheris entry in Explorer's pane is the way in (needs one Explorer restart to appear); the leftover "Aetheris Dev" entry removed.
- **2026-11-06 (cont.)**: Explorer restarted (with your OK): no Aetheris pane entry on this build, also not with a hand-made one; cleaned up again. The folder now shows the Aetheris logo instead. **Next**: decide on package identity (sparse MSIX) for the pane entry.
- **2026-11-06 (cont.)**: Sparse package prepared (identity + cloud files extension), installer registers it in Developer Mode. Dev test found that unregistering a sync root deletes online-only placeholders: no automatic unregister anywhere; dev server data restored from its trash. **Next**: you turn on Developer Mode; then install, check the pane entry, and handle a refused re-registration if Windows refuses.
- **2026-11-06 (cont.)**: Developer Mode on (you), package registered, app runs with identity, folder re-registered under it: **Aetheris now has its own entry in Explorer's navigation pane** with the logo. **Next**: uninstaller (remove package last, after unregister), Native AOT, the tailnet route test.
- **2026-11-06 (cont.)**: Uninstaller: Installed apps entry, `uninstall.ps1` + `Aetheris uninstall` (downloaded and unsent files stay, everything else goes, device signed out). Debug builds no longer take the package identity. Tested on the dev profile; the real install was reinstalled, not uninstalled. **Next**: Native AOT, the tailnet route test.
- **2026-11-07**: Redesign "Quiet Sculpture" for the web app and the app window (tokens, Lexend self-hosted, Canela via local() only, wordless object visuals, Inspector, origin-anchored Quick Look, plinth window). `--text-3` darkened to pass AA. Verified in the browser pane; built, **not committed or deployed**. **Next**: you review it in the browser, then commit + push to deploy, reinstall the client; Canela licence if you want it on every device.
- **2026-11-07 (cont.)**: Your feature list: batch move/copy/cut/paste, drag onto folders, multi-select with a dock, a full context menu, Quick Look for every file (incl. unsupported kinds), zip compress + zip downloads, and **encrypted one-time share links** (views/downloads/expiry, Android share sheet, email, copy; the key stays in the link's fragment). Server + gateway tests pass; checked in the browser pane. **Not committed or deployed. Next**: you review; deploy the NAS server (`scripts/deploy-nas.sh`), then commit + push; then try a link from your phone.
- **2026-11-07 (deploy)**: File manager + share links live as `1b95ce8`: NAS server deployed first (migration 3, RSS 43 MB, no log errors, `GET /shared/<unknown>` 404, `/shares` 401 without a session), then pushed for the gateway. **Next**: open a share link on your phone (view + download, Android share sheet); check a shared PDF renders in a real browser.
- **2026-11-10**: Web UI redesigned closer to Google Drive with Material Symbols Rounded icons (your feedback: images, layout and icons were hard to use): top search, New + sidebar, Drive's table and folder/file cards with real thumbnails, selection bar, details column, upload card, snackbar. App window untouched except icons. Verified in the browser pane; built, **not committed or deployed**. **Next**: you review (`pnpm.cmd --dir web dev` against the dev server), then commit + push to deploy.
- **2026-11-10 (deploy)**: Drive-style web UI live as `5a156a0` (merged to main, pushed; Vercel built it in under a minute). drive.example.com serves the new bundle, the sign-in screen renders with the new icons, no console or CSP errors; the API reaches the NAS. Past sign-in was not checked (your passkey). **Next**: you sign in and look around, on the PC and the phone; then virtualization or the screen-reader pass.
- **2026-11-16**: Your request: OneDrive-style folders from the PC under Computers/<PC> on the web, a self-installing exe with versions and auto-update from GitHub, and a first-time setup for every user. You chose a public releases repo (the code stays private) and per-user installs. Built: multi-folder engine, Computers web section, setup steps in the app window, `Setup.cs`/`Update.cs`, release workflow. Tested on the dev server and a dev-flavor install (see P7). Found and fixed the long-path bug that has stalled your installed copy's uploads since 2026-11-08. Then, your call: releases go to `realCmdData/aetheris` itself, no separate repo and no token secret. Committed and pushed as `de8bc33`; the web is live. Then a public-facing README (`078f0cc`, no private details) and the first releases, v1.0.0 (your tag) and v1.0.1. **Next**: install v1.0.1 on this PC; make the repo public for updates and the web's download link.
- **2026-11-16 (cont.)**: You installed v1.0.1. Fixed: blank taskbar tile, an unanswered delete notice. Made everything self-hostable while keeping your values (written to the NAS state and this PC). Then deployed (NAS, Vercel env + push) and released **v1.1.0**, installed here. **Next**: make the repo public for updates; pair a device to see the server details in Settings.
- **2026-11-16 (cont.)**: The setup is now a real installer (Windows setup dialog, Start menu entry, optional desktop shortcut). Built and tested with the dev flavor. **Not committed or released. Next**: release v1.2.0, run its setup here.
- **2026-11-16 (evening)**: Your list: web formatting fixes, more sort options with folder sizes, Created dates, folder icons from the PC, a steady long-upload status with an estimate, and automatic free-up-space. All built and tested on the dev server. Deployed and released as v1.2.0; that install exposed the hard-link conflict loop, fixed in **v1.2.1** (installed). Then the 788 identical copies were cleaned up (NAS trash and Windows' Recycle Bin). **Next**: make the repo public for updates.
- **2026-11-16 (night)**: Your request: a fuller self-hosting guide with README as the index and demo screenshots. Written: 13 pages + 17 screenshots, all links and anchors checked. Committed and pushed (`e22c92c`).
- **2026-11-16 (late night)**: Your request: scrub private details so the repo can go public. Real values moved to git-ignored `CLAUDE.local.md` and `.dev/nas.env`; tracked files use example values. History replaced by one clean root commit (force-pushed), old releases/tags/runs deleted, v1.2.2 released. **Next**: you switch the repo to public (Settings › General › Danger Zone), clear or keep the repo's homepage link (your Vercel URL), and turn on private vulnerability reporting (`docs/security.md` links to it).
- **2026-11-10 (cont.)**: Quick Look got previous/next arrows (buttons and ← →) and switches to an object double-clicked behind it; the selection follows. Deployed the same day (web only). **Next**: you try it on the live site, PC and phone.
- **2026-11-23**: Your report: externally edited files became conflict copies. Cause: saves through a temp file left a plain file without a base revision. Fixed (the NAS version this PC had is the base), plus expandable Activity entries and **Export support log…** in the app window. Tested on the dev server and the dev app window. Your installed app had stalled since 10:20 (see Open items) and was restarted; the three files restored through Git Bash were rewritten with the same bytes so they go up. Still 1.2.2, that restart hit the bug on this session's own edits in this repo (150 conflict copies at 17:05: 3 source files, 147 in `client-win/obj`); the source edits were put back from the copies, every copy removed here, and **Aetheris was quit at 17:1x so it can't repeat it**. Released as **v1.2.3**. **Next**: install 1.2.3 here (AetherisSetup.exe from the release; starting 1.2.2 again would repeat the conflicts on files saved through temp files since 10:20), then check its first start: no conflict copies, the backlog (~415 files) goes up, the removed copies go to the NAS trash.
