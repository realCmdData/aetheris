# Aetheris protocol v1

The contract between the NAS server (`server/`), the Vercel gateway, the web UI and the Windows client.
All paths are relative, slash-separated and case-preserving, e.g. `Photos/2026/a.jpg`. `""` is the root.
JSON everywhere except file bytes. Times are unix **milliseconds**.

## Auth

Every `/api/v1/` request needs a session, except `auth/status`, `auth/login/*`, `auth/register/*` (with a setup token) and `devices/claim`.

- **Browsers** get the cookie `__Host-aetheris` (`Secure; HttpOnly; SameSite=Strict`) after a passkey sign-in.
  - Cookie requests other than GET/HEAD must carry an `Origin` from the configured list, otherwise `403`.
  - The session lasts 30 days and is renewed (new cookie) past half its life.
- **Devices** (the Windows client) send `Authorization: Bearer <token>`. The token lasts a year.
  - When the server renews it, the response carries `X-Aetheris-Token`. Store that and use it from then on.
- **Tokens** are `base64url(json{sid,exp}) "." base64url(Ed25519 signature)`.
  - The gateway can verify them with `aetheris pubkey`; the NAS additionally checks the session still exists (revocation).
- **On the tailnet listener** only untagged devices (yours) and `tag:gateway` get through. `X-Forwarded-For` is trusted from the gateway only.
- **Missing or invalid session:** `401`.

| Request | Response |
|---|---|
| `GET /api/v1/auth/status` | `{"signedIn","hasPasskeys"}` |
| `POST /api/v1/auth/register/begin` `{"setupToken"?}` | `{"ceremony","options"}`. Without a setup token a signed-in session is required (adding a passkey). |
| `POST /api/v1/auth/register/finish?ceremony=&name=` body = `PublicKeyCredential` JSON | `{"ok"}`. With a setup token it also signs this browser in and spends the token. |
| `POST /api/v1/auth/login/begin` | `{"ceremony","options"}` (discoverable passkey, user verification required) |
| `POST /api/v1/auth/login/finish?ceremony=` body = assertion JSON | `{"ok"}` + cookie |
| `POST /api/v1/auth/logout` | `204` |
| `POST /api/v1/devices/pair` | `{"code","expires"}`: 8 characters, 10 minutes, shown in the web UI |
| `POST /api/v1/devices/claim` `{"code","name"}` | `{"token"}`. Max 10 tries a minute overall; 5 wrong codes void all open codes. |
| `GET /api/v1/sessions` · `DELETE /api/v1/sessions/{id}` | list (`current` marks yours) · revoke a browser or device |
| `GET /api/v1/passkeys` · `DELETE /api/v1/passkeys/{id}` | list · remove (never the last one: `409`) |
| `GET /api/v1/audit` | `{"events":[{"ts","event","detail","source"}]}`, newest 200 |

The first passkey needs a setup link. Run `aetheris setup` on the NAS (SSH, LAN only) and open the printed link within 15 minutes.
`aetheris reset` removes every passkey and session and prints a new link.

## Names

The server rejects (`400`) any name a Windows client could not store:
- `<>:"\|?*`, control characters, a trailing `.` or space
- `CON`, `PRN`, `AUX`, `NUL`, `COM1-9`, `LPT1-9` (with or without an extension)
- `.`, `..`, empty segments, over 255 bytes per name

Two names in one folder may not differ only in case (`409`). Internal folders (`.aetheris`, `@Recycle`,
`@Recently-Snapshot`, `.@__thumb`, …) are invisible and cannot be created.

### Computers (a convention of the clients, not the server)
`Computers/<PC name>/<folder>` holds folders the Windows app syncs from elsewhere on a PC (Desktop, Documents, …),
two-way, with every file kept on that PC. To the server these are ordinary paths. The web shows `Computers` as a
section of its own and leaves it out of My files; the Windows app leaves it out of its Aetheris folder and maps
only its own `Computers/<this PC>/<folder>` paths to the folders it syncs. Deleting or moving such a folder (or
`Computers/<PC>` above it) on the NAS makes that PC stop syncing it; its files stay on the PC. Such a folder can have
an icon of its own (see `/api/v1/icon`).

## Entry

```json
{ "path": "docs/a.txt", "dir": false, "size": 11, "mtime": 1793739183243, "created": 1793739100000, "hash": "<BLAKE3 hex>", "rev": 42 }
```
`rev` is the journal sequence of the entry's last change. It is the file's ETag (`"42"`) and the
`baseRev` a client sends when it changes the file. Folders have no `hash`. `created` (unix ms) is when the item came to
be: the creation time the uploader sent, else its first modification time; edits and moves keep it. In a folder
listing (`/tree?path=`), a folder's `size` is everything inside it; elsewhere folders have size 0.

## Reading

| Request | Response |
|---|---|
| `GET /api/v1/tree?path=docs` | `{"entries":[Entry…]}`: children, folders first |
| `GET /api/v1/tree?recursive=1` | NDJSON: every Entry, parents before children, then `{"epoch","cursor","done":true}`. A stream without the last line was cut off: retry. |
| `GET /api/v1/changes?epoch=E&cursor=N&wait=30` | `{"epoch","cursor","changes":[Change…]}`. Long-polls up to `wait` s (max 55) when nothing is new. `410` = index rebuilt: resync from the recursive tree. |
| `GET /api/v1/file?path=docs/a.txt[&inline=1]` | bytes. Supports `Range`, `If-None-Match`, `If-Range`. `503` = file changing on disk: retry.<br>Downloaded (`attachment`, `application/octet-stream`) unless `inline=1` **and** the type is a safe preview: image, video, audio, PDF, plain text. Previews get a fixed `Content-Type` and, except PDF, a CSP `sandbox`. HTML and SVG are never shown inline. |
| `GET /api/v1/search?q=report` | `{"entries":[…]}`: case-insensitive substring of the path, max 200 |
| `GET /api/v1/recent` | `{"entries":[…]}`: the 100 most recently modified files, newest first |
| `GET /api/v1/thumb?path=&rev=` | JPEG, longest side ≤ 320 px, EXIF orientation applied, cached per content hash. Only for jpg/png/gif/webp up to 50 MP; otherwise `404` (show the icon). Put the entry's `rev` in the URL: responses are cached for a year. |
| `GET /api/v1/icon?path=` | PNG: a folder's own icon (the Windows app sends Downloads' arrow for a synced Downloads folder); `404` when it has none. `PUT` with a PNG body (≤ 256 KB) sets it. Kept in `.aetheris/icons`, never among the files. |
| `GET /api/v1/changes/head` | `{"epoch","cursor"}`: start following changes from now (the web UI). Sync clients use the recursive tree instead. |

`Change`: `{"seq","op":"put|mkdir|delete|move","path","from"?,"entry"?}`.
- `entry` is the path's **current** state; it is omitted when the path is gone by now.
- `move` carries `from`. Folders move with their whole subtree.
- `delete` of a folder covers its subtree.

**Client sync loop:** read the recursive tree once and keep `epoch` + `cursor`. Then call `changes` forever, applying each change and storing the returned cursor.

## Writing

Uploads are resumable and content lands atomically. Nobody ever sees a half-written file.

1. `POST /api/v1/upload` → `201 {"id","chunkSize"}`.
2. `PUT /api/v1/upload/{id}?offset=N` with raw bytes → `{"offset"}`. A chunk must start exactly at the server's offset, otherwise `409 {"error":"offset","offset":N}`. Resume from that.
   - `GET /api/v1/upload/{id}` returns the current offset (after a dropped connection or a server restart).
   - Max 64 MB per request. Use `chunkSize` (16 MB) direct, and ≤ 4 MB through the Vercel gateway.
3. `POST /api/v1/upload/{id}/commit` `{"path","baseRev","mtime"?,"created"?}` → Entry. `created`: the file's creation time where it came from (unix ms).
   - `baseRev` is the rev the client last saw, `0` for a new file.
   - If the server's copy changed since: `409 {"error":"conflict","current":Entry|null}`. Nothing is written, and the upload stays committable. Keep both copies, e.g. `a (conflict DESKTOP-01 2026-11-03).txt`.
   - The replaced content becomes a version.
4. `DELETE /api/v1/upload/{id}` aborts. Untouched uploads expire after 7 days.

| Request | Response |
|---|---|
| `POST /api/v1/mkdir` `{"path"}` | Entry; idempotent for existing folders. The parent must exist. |
| `POST /api/v1/move` `{"from","to","baseRev"?}` | Entry at `to`. The target must not exist; case-only renames are allowed. |
| `POST /api/v1/copy` `{"from","to"}` | Entry at `to`; folders copy whole. Each file is copied to a temp file, hashed and placed like an upload, keeping its date. The target must not exist (`409`). |
| `POST /api/v1/compress` `{"paths":[…],"to"}` | Entry of the new zip at `to` (must not exist). Each path keeps its own name at the top of the archive; media that is already compressed is stored, the rest deflated. |
| `POST /api/v1/zip` `{"paths":[…],"name":"x.zip"}` | `{"url"}`: a one-use download URL, valid 1 minute. `GET` it for the zip, streamed while it is built. Two steps because downloads are plain GETs and a long selection doesn't fit a URL. |
| `POST /api/v1/delete` `{"path","baseRev"?}` | `204`; moves to the trash (folders whole) |
| `GET /api/v1/versions?path=` | `{"versions":[{"id","created","size","hash"}]}`, newest first |
| `GET /api/v1/versions/file?path=&id=` | the bytes of that version (download) |
| `POST /api/v1/versions/restore` `{"path","id"}` | Entry; the replaced content becomes a version too |
| `GET /api/v1/trash` | `{"items":[{"id","path","dir","size","deleted"}]}` |
| `POST /api/v1/trash/restore` `{"id","path"?}` | `204`; back to the old path or to `path` |

## Share links

A link opens one file without an account: `https://<your web address>/#/s/<id>.<key>`. `id` is 128 random bits (the capability),
`key` an AES-256 key (base64url). The key is in the `#fragment`, so browsers never send it: the gateway and its logs only see `id`
and ciphertext. The NAS keeps the key (it must encrypt), so this protects the path through Vercel, not the files from the NAS.

| Request | Response |
|---|---|
| `POST /api/v1/shares` `{"path","views","downloads","hours"}` | Share `{"id","key","path","created","expires","views","downloads"}`. Counts are what's left, `-1` = unlimited, not both `0`. `hours` 1–2160. Views only for previewable types. |
| `GET /api/v1/shares` | `{"shares":[Share…]}`: links that still work, newest first |
| `DELETE /api/v1/shares/{id}` | `204` |
| `GET /api/v1/shared/{id}` (no session) | `{"meta","views","downloads","expires"}`. `meta` = base64url(12-byte nonce ‖ AES-GCM(`{"name","size","mtime"}`)). |
| `GET /api/v1/shared/{id}/content?mode=view\|download` (no session) | The sealed file; spends one view or download (given back if sending fails). |

Expired, used up, revoked or its file gone: all `404`, so a visitor can't tell which. Links follow their file when it moves.
Sealed stream: 8-byte random nonce prefix, 8-byte big-endian plaintext size, then 256 KiB chunks, each AES-GCM with
nonce = prefix ‖ uint32 BE (index, `| 1<<31` on the last). Cutting, extending or editing the stream fails authentication.
Readers: `web/src/lib/sealed.ts`, and `openStream` in `server/internal/api/share_test.go`.

Retention: the trash keeps items for 30 days. Versions older than 30 days are purged beyond the newest 10 per file.

## Errors

`{"error": "…"}` with:

| Status | Meaning |
|---|---|
| `400` | bad name or request |
| `404` | not found |
| `409` | conflict, offset, exists, missing parent, or a file/folder mismatch |
| `410` | epoch changed |
| `413` | chunk too large |
| `503` | busy |
| `500` | server fault (details only in the server log) |

## Outside edits

Files changed directly on the share (SMB, File Station) appear in `changes` within ~2 s via the filesystem
watcher, and at the latest at the hourly full rescan.
