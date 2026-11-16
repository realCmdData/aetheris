# 6. Looking after it

[← 5. Your PCs](05-windows-pcs.md) · [Run your own Aetheris](../self-hosting.md)

Aetheris needs little care. This page covers updates, backups, moving things around and starting over.

## Updating

| Part | How it updates |
|---|---|
| **Windows app** | By itself: it checks GitHub for a new release a minute after it starts and every six hours, verifies the download's SHA-256 and switches over once nothing is transferring. |
| **Web app and gateway** | When your fork changes: Vercel redeploys every push to its main branch. On GitHub, **Sync fork** brings in the latest version. |
| **Server** | By hand: build the new version and swap the binary. |

### The server

Build the new version as in [step 2](02-server.md#build-it), then on the server keep the old binary for a quick way
back, swap, and restart:

```bash
scp aetheris you@192.168.1.20:/tmp/
ssh you@192.168.1.20 '
  sudo cp -p /opt/aetheris/aetheris /opt/aetheris/aetheris.prev &&
  sudo install -m 755 /tmp/aetheris /opt/aetheris/aetheris &&
  sudo systemctl restart aetheris'
```

Check it answers ([step 2](02-server.md#check)) and look for the new version in its log:

```console
$ journalctl -u aetheris | grep '"msg":"serving"' | tail -1
{"time":"…","level":"INFO","msg":"serving","version":"v1.3.0","data":"/srv/aetheris/files","gatewayKey":"MCowBQYDK2VwAyEA…"}
```

Index changes migrate by themselves on start. Keep `aetheris.prev` until you're happy with the new version. Going
back to an older server after a migration isn't tested, so before an update that mentions one, also keep a copy of
`.aetheris/index.db` (taken while the server is stopped).

[`scripts/deploy-nas.sh`](../../scripts/deploy-nas.sh) does all of this from Git Bash (build, copy, swap, check the
server answers, roll back if it doesn't), for a server reachable over SSH:

```bash
AETHERIS_NAS_SSH=aetheris@192.168.1.20 AETHERIS_NAS_HOST=aetheris.tail1234.ts.net scripts/deploy-nas.sh
```

To skip typing them, put both settings in `.dev/nas.env` (one `NAME=value` per line), which git ignores.

It expects the server's layout from [`scripts/nas/start.sh`](../../scripts/nas/start.sh); adapt the restart for
systemd.

### Update the server first

When a release changes both sides, the server goes first: newer apps and web apps are written to work with the
server version they come with. The release notes say when that matters.

### Your own Windows app releases

The Windows app updates from this repository's releases. If you change the app and publish your own builds from
your fork, change `Latest` in [`client-win/Update.cs`](../../client-win/Update.cs) to your repository first, or your
PCs will update back to the upstream version. Releases are built by
[`.github/workflows/release.yml`](../../.github/workflows/release.yml) when you push a version tag:

```bash
git tag v1.3.0
git push origin v1.3.0
```

## Backups

Aetheris is not a backup. Earlier versions and the trash help with mistakes, not with a failed disk or a stolen
NAS. Back up two folders:

| Folder | Why |
|---|---|
| **Your files** (`-data`), including `.aetheris` | Your files, plus their earlier versions, the trash and the index. Without `.aetheris` you still have every file; you'd only lose the versions and the trash. |
| **Private state** (`-state`) | Your passkeys, paired PCs and the server's tailnet identity. Treat it like a password: it holds the key that signs sessions. |

Any tool that copies files works (rsync, Hyper Backup, HBS 3, restic). The index is a SQLite database: for a copy
you can rely on, stop the server while `.aetheris` is backed up, or make sure `index.db` is copied together with
`index.db-wal` next to it.

### If the state folder is lost

Start fresh: `aetheris setup -origins https://drive.example.com` with a new `-state`, sign the server in to
Tailscale again (delete the old **aetheris** machine first so the name stays free), put the new `aetheris pubkey`
into Vercel's `AETHERIS_PUBKEY` and redeploy, create a passkey and pair your PCs again. Your files and their history
are untouched.

## Moving

- **To a new machine**: stop the server, copy both folders over (keeping permissions), and start it there with the
  same flags. It keeps its tailnet name, so PCs and the gateway find it again; PCs at home re-learn the home address
  when you pair them again, and meanwhile use the tailnet.
- **The files to another disk**: stop, move the folder (with `.aetheris`), start with the new `-data`.
- **To another domain**: passkeys belong to the old one. Run `aetheris reset -origins https://new.example.com`, add the
  new domain in Vercel, and create new passkeys with the link `reset` prints.

## Health checks

- **Is it up?** `curl https://aetheris.tail1234.ts.net/api/v1/auth/status` from a PC on the tailnet.
- **Who did what?** **Settings › Activity** in the web app lists sign-ins (and refused ones), pairings, passkey
  changes, setup links, deletes and restores, and every view or download of a shared file.
- **Memory**: it stays under 100 MB. On Linux: `ps -o rss,cmd -C aetheris`.
- **Logs**: JSON lines on standard output (`journalctl -u aetheris` with systemd).
