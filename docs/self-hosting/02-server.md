# 2. The server

[← 1. Tailscale](01-tailscale.md) · [Run your own Aetheris](../self-hosting.md) · Next: [3. The web app and gateway →](03-web-gateway.md)

The server is one static binary of about 30 MB. It serves one folder, keeps its index next to the files, joins your
tailnet by itself and stays under 100 MB of memory, so a modest NAS runs it comfortably.

## Build it

On any PC with [Go](https://go.dev/dl/) 1.27 or later and Git:

```bash
git clone https://github.com/realCmdData/aetheris.git
cd aetheris/server
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=$(git describe --always)" -o aetheris ./cmd/aetheris
```

In Windows PowerShell, set the variables first:

```powershell
$env:GOOS = 'linux'; $env:GOARCH = 'amd64'; $env:CGO_ENABLED = '0'
go build -trimpath -ldflags "-s -w -X main.version=$(git describe --always)" -o aetheris ./cmd/aetheris
```

The result runs on any x86-64 Linux, old kernels included. For an ARM machine, use `GOARCH=arm64` (it builds, but
hasn't been tested on real hardware yet).

## Choose two folders

| Folder | Example | What it holds |
|---|---|---|
| **Your files** (`-data`) | `/srv/aetheris/files` | Your files, as plain files. It may already contain files: the server indexes them on its first start. Aetheris adds one hidden folder, `.aetheris`, with its index, earlier versions and the trash. |
| **Private state** (`-state`) | `/var/lib/aetheris` | The key that signs sessions, the passkeys, the server's tailnet identity, the web address. **Keep it outside the files folder** and out of any network share. |

Run the server as its own user that owns both folders. On most Linux systems:

```bash
sudo useradd --system --create-home --shell /usr/sbin/nologin aetheris
sudo mkdir -p /opt/aetheris /srv/aetheris/files /var/lib/aetheris
sudo chown aetheris: /srv/aetheris/files /var/lib/aetheris
sudo chmod 700 /var/lib/aetheris
```

If the files folder is also a network share (SMB, NFS), give the `aetheris` user write access to it and keep the
share's own users in a group with write access too, so files created by either side stay editable by both.

## Copy it over

```bash
scp aetheris you@192.168.1.20:/tmp/
ssh you@192.168.1.20 'sudo install -o root -g root -m 755 /tmp/aetheris /opt/aetheris/aetheris'
```

## Tell it your web address

Once, with the address the web app will have:

```bash
sudo -u aetheris /opt/aetheris/aetheris setup -origins https://drive.example.com -state /var/lib/aetheris
```

It saves the address in the state folder (later starts find it there) and prints a link for your first passkey.
That link only works for 15 minutes and the web app doesn't exist yet, so let it go: [step 4](04-first-sign-in.md)
makes a fresh one.

> [!NOTE]
> Passkeys belong to this address. If you move to another domain later, you'll need to `reset` and create new
> passkeys, so pick the address you'll keep.

## First start

Start it in the foreground once, to connect it to Tailscale:

```bash
sudo -u aetheris /opt/aetheris/aetheris serve -data /srv/aetheris/files -state /var/lib/aetheris
```

It logs JSON lines. One of the first contains a link like `https://login.tailscale.com/a/1a2b3c4d5e`:

1. Open that link and sign in to Tailscale as an admin of your tailnet.
2. In the admin console under **Machines**, the server now shows as **aetheris** with the tag **aetheris**.
   Tagged machines normally don't expire; if it shows an expiry date, choose **Disable key expiry** in its menu.
3. Back in the terminal, a line with `"msg":"serving"` says it's ready. It also shows `gatewayKey`, which you'll
   need in the next step (`aetheris pubkey` prints it again any time).

Stop it with <kbd>Ctrl</kbd>+<kbd>C</kbd> once you've set it up to start on boot.

<details>
<summary>No browser on that machine? Use an auth key</summary>

Under **Settings › Keys** in the Tailscale console, generate a one-off auth key (not ephemeral) tagged
`tag:aetheris`, and give it to the first start only:

```bash
sudo -u aetheris env TS_AUTHKEY=tskey-auth-… /opt/aetheris/aetheris serve -data /srv/aetheris/files -state /var/lib/aetheris
```

The node's identity is then saved in the state folder; later starts don't need the key.
</details>

## Start it on boot

### Linux with systemd

Most Linux distributions. Save this as `/etc/systemd/system/aetheris.service`:

```ini
[Unit]
Description=Aetheris server
Wants=network-online.target
After=network-online.target
RequiresMountsFor=/srv/aetheris/files

[Service]
User=aetheris
ExecStart=/opt/aetheris/aetheris serve -data /srv/aetheris/files -state /var/lib/aetheris
Restart=on-failure
RestartSec=5
# Files Aetheris creates stay writable for the group, e.g. SMB users of the same folder
UMask=0002

[Install]
WantedBy=multi-user.target
```

```bash
sudo systemctl daemon-reload
sudo systemctl enable --now aetheris
journalctl -u aetheris -f      # its log
```

### A NAS

NAS systems differ in how they start things at boot. What Aetheris needs is simple: run the `serve` command above
as the `aetheris` user, after the data volume is mounted, and again if it stops.

- **QNAP**: [`scripts/nas/`](../../scripts/nas) has what the author uses on QTS 4.3: `start.sh` starts the server in
  the background with its log next to it, and `install-boot-hook.sh` adds a small watchdog to the root crontab that
  starts it as the `aetheris` user whenever it isn't running (after boot, the data volume mounts late, so it simply
  waits). Edit the paths in both files before you use them.
- **Synology**: in **Control Panel › Task Scheduler**, create a **Triggered Task › User-defined script** for the
  **Boot-up** event, run as the `aetheris` user, with the `serve` command as the script.
- **Anything else**: whatever your system offers for services or boot scripts.

## Your home network

PCs at home connect to the server directly on port **8443** (with the same Tailscale certificate), which is faster
than going through the tailnet and works for PCs without Tailscale.

- **Give the server a fixed address** on your home network, for example with a DHCP reservation in your router.
  PCs learn it when they're paired.
- **Allow port 8443 from your home network** if the server has a firewall. With `ufw`:

  ```bash
  sudo ufw allow from 192.168.1.0/24 to any port 8443 proto tcp
  ```

- Don't forward the port in your router. The server is only meant to be reached from inside your home and over the
  tailnet.

Prefer not to listen on the home network at all? Start it with `-lan=` (empty). PCs then always use the tailnet.

## Check

From a PC on the tailnet:

```console
$ curl https://aetheris.tail1234.ts.net/api/v1/auth/status
{"hasPasskeys":false,"signedIn":false}
```

From any PC at home, Tailscale or not (on Windows, type `curl.exe`: in PowerShell, `curl` means something else):

```console
$ curl --resolve aetheris.tail1234.ts.net:8443:192.168.1.20 https://aetheris.tail1234.ts.net:8443/api/v1/auth/status
{"hasPasskeys":false,"signedIn":false}
```

Both answer with the same JSON over a valid certificate. `hasPasskeys` turns `true` after [step 4](04-first-sign-in.md).

## Reference

### Commands

| Command | Does |
|---|---|
| `aetheris serve` | Runs the server (the default when no command is given). |
| `aetheris setup` | Prints a link, valid for 15 minutes, that registers a passkey without signing in. Saves `-origins` when given. |
| `aetheris reset` | Removes every passkey and signs out every browser and PC, then does what `setup` does. For when you've lost all your passkeys. |
| `aetheris pubkey` | Prints the public half of the session-signing key, for the gateway's `AETHERIS_PUBKEY`. |

`setup`, `reset` and `pubkey` need the same `-state` as the server, and can run while it's running.

### Flags

| Flag | Default | Meaning |
|---|---|---|
| `-data` | (required for `serve`) | The folder to serve. |
| `-state` | `~/.aetheris` | Private state. Keep it outside `-data`. |
| `-origins` | (saved) | The web app's address, e.g. `https://drive.example.com`. Several, comma-separated, are allowed; the first is used in links. Given once, then remembered in `-state`. |
| `-rpid` | the first origin's host | The WebAuthn relying party ID passkeys are bound to. Leave it unless you know you need it. |
| `-hostname` | `aetheris` | The server's name on the tailnet. Empty turns off the tailnet and home network listeners (development only). If you change it, set `AETHERIS_NAS` on the gateway. |
| `-lan` | `:8443` | The home network listener. Empty turns it off. |
| `-dev` | (off) | A plain-HTTP listener on a loopback address, e.g. `127.0.0.1:8080`, for development. |
| `-rescan` | `1h` | How often to check the whole folder for changes made outside Aetheris. Changes are also noticed as they happen; the rescan catches anything missed. |

The server keeps its heap near 96 MB by default. Set the `GOMEMLIMIT` environment variable to choose another limit.

### What's on disk

```
/srv/aetheris/files/            your files, exactly as you see them
└── .aetheris/
    ├── index.db                the index and change journal (SQLite)
    ├── versions/               earlier versions: the last 10 of each file, or 30 days, whichever keeps more
    ├── trash/                  deleted files and folders, for 30 days
    ├── icons/                  folder icons sent by the Windows app
    └── tmp/                    uploads in progress

/var/lib/aetheris/              private state
├── origins.txt                 the web address
├── session.key                 signs sessions (its public half goes to the gateway)
├── auth.db                     passkeys, sessions, paired PCs, the activity log
└── tsnet/                      the server's tailnet identity
```

You can add, change and delete files in the folder directly (over SMB, say). The server notices and the change
reaches every PC. Just leave `.aetheris` alone.

Next: [3. The web app and gateway →](03-web-gateway.md)
