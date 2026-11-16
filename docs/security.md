# Security

[← Documentation](../README.md#documentation)

Aetheris is built so that your home network accepts nothing from the internet, and so that every request is checked
more than once before it touches a file. This page explains how, and where the limits are.

## What can reach what

```
 internet ──HTTPS──▶ Vercel: web app + gateway ──WireGuard──▶ tailnet ──▶ aetheris process ──▶ your files
                      │ lets through only sign-in               │ policy: your devices      │ checks every
                      │ and signed sessions                     │ and the gateway, port 443 │ request again
 home network ──────────────────────────────────────────────────────────────────▶ port 8443 ┘
```

| Layer | Stops |
|---|---|
| **No open ports at home** | Anyone on the internet reaching the server directly. The server only makes outgoing connections; there is nothing to port-forward. |
| **The tailnet policy** | Other machines on your tailnet. Only your own devices and the gateway may connect, only to port 443, and the server may connect to nothing. ([step 1](self-hosting/01-tailscale.md#tags-and-access-policy)) |
| **The server's tailnet check** | The same, a second time: on the tailnet it accepts your own (untagged) devices and `tag:gateway`, and refuses any other tagged machine. |
| **Only the Aetheris process joins the tailnet** | Reaching the rest of the machine. The server has Tailscale built in, so the NAS's own admin pages, SSH and shares stay off the tailnet. |
| **The gateway's filter** | Requests without a session. The gateway checks each session token's Ed25519 signature against your server's public key and forwards only those, plus sign-in itself. |
| **The server's session check** | Everything else. The server looks up every session and device it receives in its own database, so a revoked one stops at once. |
| **Path confinement** | A request escaping the files folder. Every file access goes through Go's `os.Root`, which refuses `..` and symbolic links pointing outside. |

## Signing in

- **Passkeys (WebAuthn)**, bound to your domain. There are no passwords to phish, guess or reuse, and the server
  stores only public keys.
- **The first passkey** comes from a setup link that the server prints on demand. It works once, for 15 minutes, and
  its secret sits after the `#`, which the browser never sends anywhere.
- **Sessions** are `HttpOnly`, `Secure`, `SameSite=Strict` cookies that last 30 days and renew while you use them.
  Requests that change something must also come from your web address.
- **Each PC has its own sign-in**, made with a 10-minute pairing code from the web app. It lasts a year, renews while
  the PC is in use, is stored encrypted for that Windows user (DPAPI), and can be revoked under
  **Settings › Devices**.
- **Sign-in is rate-limited** by the server. Add a rate limit at Vercel too
  ([step 3](self-hosting/03-web-gateway.md#rate-limit-sign-in-recommended)).
- **Settings › Activity** records sign-ins and refused attempts, pairings, passkey changes, setup links, deletes,
  restores and every use of a share link.

## The Windows app

- It talks to your server directly, at home or over Tailscale, and **never through Vercel**.
- At home it connects to the server's home address but still verifies the server's real Tailscale certificate, so
  another machine at that address can't pass for your server.
- Updates come from this repository's GitHub releases. The app checks each download against the SHA-256 GitHub
  publishes with the release before switching over. That protects against a damaged or tampered download, not against
  someone who can publish releases here. The app isn't code-signed yet.

## Share links

A share link carries a random key after the `#`, which browsers never send anywhere. Your server encrypts the file
with it (AES-GCM) before it leaves home, and the recipient's browser decrypts it, so Vercel, the gateway and
everything between only ever see encrypted bytes. The key is kept on your server, so you can copy the link again, and
in the link itself. Each link can be limited in views, downloads and lifetime, and removed at any time under
**Settings › Shared links**.

## The web app

The web app is served with a strict Content Security Policy (scripts only from its own origin, no frames from
elsewhere, no plugins), `X-Content-Type-Options: nosniff`, `Referrer-Policy: no-referrer`, and a Permissions Policy
that allows passkeys and nothing else (no camera, microphone or location). See
[`gateway/vercel.json`](../gateway/vercel.json).

## Limits: what Aetheris does not protect against

- **The web app passes through Vercel.** HTTPS from your browser ends at Vercel, and the gateway forwards to your
  server over an encrypted tailnet connection. Files you open in the web app (share links aside) are therefore
  readable by the gateway while it forwards them, as with any hosted web app. The Windows app doesn't go through Vercel at all.
- **Files are not encrypted at rest.** They are plain files on your server, so anyone with access to that machine or
  its network share can read them. Protect the machine and its admin accounts.
- **The state folder is a key.** It holds the key that signs sessions. Keep it out of shared folders and back it up
  like a password.
- **Aetheris is not a backup.** Versions and the trash help with mistakes, not with a failed disk, ransomware on the
  server or a stolen NAS. Keep a separate backup ([how](self-hosting/06-maintenance.md#backups)).
- **Whoever controls your Tailscale admin account controls access.** Use a strong sign-in with two-factor
  authentication for it, and for Vercel and GitHub.

## Reporting a problem

Found a security issue? Please don't open a public issue. Use GitHub's
[private vulnerability reporting](https://github.com/realCmdData/aetheris/security/advisories/new) for this
repository.
