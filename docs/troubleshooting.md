# Troubleshooting

[← Documentation](../README.md#documentation)

Start with the log of the part that misbehaves:

| Part | Log |
|---|---|
| Server | Its standard output: `journalctl -u aetheris` with systemd, `aetheris.log` next to the binary with `scripts/nas/start.sh` |
| Gateway | Vercel project › **Logs** |
| Windows app | `%LOCALAPPDATA%\Aetheris\nas\aetheris.log` |
| Web app | The browser's developer tools (<kbd>F12</kbd>) › **Console** |

## The server

### It stops with "not set yet: run aetheris setup -origins …"

It doesn't know its web address yet. Run `aetheris setup -origins https://drive.example.com` once, with the same
`-state` ([step 2](self-hosting/02-server.md#tell-it-your-web-address)).

### No Tailscale login link, or the machine never appears

- The machine needs outgoing internet access (HTTPS, and UDP for direct connections).
- The link only appears on the first start. If the state folder already has a `tsnet` folder from an earlier
  attempt, the server reuses it; delete `tsnet` (with the server stopped) to start over.
- If Tailscale refuses the tag, your account isn't in `tagOwners` for `tag:aetheris`
  ([step 1](self-hosting/01-tailscale.md#tags-and-access-policy)).

### "tailnet listener" or certificate errors

HTTPS certificates aren't enabled for the tailnet: turn on **DNS › HTTPS Certificates** in the Tailscale console.

### A listener fails with "address already in use"

Another program uses port 8443. Pick another with `-lan :9443`; PCs learn the new port when you pair them again.

## The web app

### It says "Not set up yet"

There's no passkey yet. Run `aetheris setup` on the server and open the link it prints within 15 minutes
([step 4](self-hosting/04-first-sign-in.md)).

### The web app says "Your NAS isn't reachable"

The gateway couldn't reach the server over the tailnet. Check, in order:

1. **Is the server running?** From a PC on the tailnet: `curl https://aetheris.tail1234.ts.net/api/v1/auth/status`.
2. **Does the gateway join the tailnet?** After you open the page, the Tailscale console's **Machines** should list an
   **aetheris-gateway** with the tag **gateway**. If not, `TS_AUTHKEY` is wrong or expired, or the OAuth client
   lacks the `tag:gateway` tag or the **Auth Keys: Write** scope. Fix it in Vercel, then redeploy.
3. **May it reach the server?** The access policy needs the grant from `tag:gateway` to `tag:aetheris` on `tcp:443`.
4. **Does it look for the right name?** If the server runs with another `-hostname`, set `AETHERIS_NAS` to its full
   name (`myname.tail1234.ts.net`) in Vercel and redeploy. The gateway's log says so if it can't work the name out.

### It keeps asking me to sign in, or signing in fails right after the passkey

`AETHERIS_PUBKEY` in Vercel doesn't match the server's key, typically after the state folder was recreated. Run
`aetheris pubkey` on the server, update the variable, and redeploy.

### The passkey dialog says it isn't allowed here, or finds no passkey

Passkeys belong to the address the server was set up with. Open the web app at exactly that address (not the
`*.vercel.app` one). If you've moved to another domain, see
[Moving](self-hosting/06-maintenance.md#moving).

### The setup link says it's invalid

It's older than 15 minutes or was already used. Run `aetheris setup` again for a new one.

## The Windows app

### Windows says it protected your PC

The app isn't code-signed yet. Choose **More info**, then **Run anyway**.

### The window stays blank

The window needs Microsoft Edge WebView2. Windows 11 has it; on Windows 10,
[install it from Microsoft](https://developer.microsoft.com/microsoft-edge/webview2/).

### "That code didn't work"

Pairing codes last 10 minutes and work once. Make a new one under **Settings › Devices › Pair a device**, and type the
server exactly as Settings shows it.

### It can't connect at home

- On a PC without Tailscale, the **address at home** must be the server's address on your home network, as Settings
  shows it. If the server's address changed, pair the PC again (and give the server a fixed address,
  [step 2](self-hosting/02-server.md#your-home-network)).
- The server's firewall must allow port 8443 from your home network.
- Test from the PC: `curl.exe --resolve aetheris.tail1234.ts.net:8443:192.168.1.20 https://aetheris.tail1234.ts.net:8443/api/v1/auth/status`.

### It's offline when I'm away

Away from home, the PC needs Tailscale, signed in to your tailnet. Without it, Aetheris catches up when the PC is
home again. Online-only files can't open while it's offline; choose **Always keep on this device** for the ones you
need on the road.

### A file in a folder from this PC doesn't sync

Aetheris leaves out symbolic links, junctions and files with more than one hard link, and can't sync folders that
another app such as OneDrive syncs ([details](windows-app.md#folders-from-this-pc)). For hard-linked files the log
says `not synced: it has hard links`.

### Files named "… (conflict PC-NAME date time)"

The file changed on the server and on this PC at the same time, so Aetheris kept both
([details](windows-app.md#when-two-pcs-change-the-same-file)). Compare them, keep the one you want and delete the
other.

### Aetheris doesn't appear in Explorer's navigation pane

That needs Windows' Developer Mode (**Settings › System › For developers**), then running AetherisSetup.exe again. Without it, the
folder is still in your user folder, with all its status icons.

### It doesn't update

It checks for a new release a minute after it starts and every six hours, and waits until nothing is transferring.
The log says `updating to …` when it switches, and `update: …` when a check fails. Your version is at the bottom of
the window.

## Still stuck?

Open an [issue](https://github.com/realCmdData/aetheris/issues) with what you did, what you expected, and the relevant
log lines. Leave out tokens, keys and your addresses.
