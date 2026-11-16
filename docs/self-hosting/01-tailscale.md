# 1. Tailscale

[← Run your own Aetheris](../self-hosting.md) · Next: [2. The server →](02-server.md)

Tailscale is the private network that connects the pieces. The server and the web gateway join it by themselves; you
only prepare the tailnet. Everything here happens in the [Tailscale admin console](https://login.tailscale.com/admin).

## Create a tailnet

Sign up at [tailscale.com](https://tailscale.com) if you haven't. Then install Tailscale on the PCs and phones you'll
use Aetheris from, and sign them in with the same account. PCs on the tailnet reach your server from anywhere; a PC
without Tailscale can still use Aetheris, but only at home ([step 5](05-windows-pcs.md) explains).

Note your tailnet's DNS name: it's on the **DNS** page and looks like `tail1234.ts.net`. Your server will be
`aetheris.tail1234.ts.net`.

## Turn on MagicDNS and HTTPS

On the **DNS** page:

1. **MagicDNS**: on (it is on by default in new tailnets).
2. **HTTPS Certificates**: choose **Enable HTTPS**.

The server uses a Tailscale certificate for `aetheris.tail1234.ts.net`, on the tailnet and on your home network
alike. Certificate names are published in public Certificate Transparency logs, so the name becomes public. That is
harmless: nothing answers it from the internet.

## Tags and access policy

Aetheris uses two [tags](https://tailscale.com/kb/1068/tags): `tag:aetheris` for the server and `tag:gateway` for
the web gateway. Open **Access controls** and make sure your policy contains these parts:

```json
{
  "tagOwners": {
    "tag:aetheris": ["autogroup:admin"],
    "tag:gateway":  ["autogroup:admin"]
  },
  "grants": [
    // Your own devices (Windows app, phone) reach the server
    { "src": ["autogroup:member"], "dst": ["tag:aetheris"], "ip": ["tcp:443"] },
    // The web gateway reaches the server, and nothing else
    { "src": ["tag:gateway"], "dst": ["tag:aetheris"], "ip": ["tcp:443"] }
  ]
}
```

What this means:

- Only tailnet admins can put these tags on a machine.
- Your devices and the gateway may open HTTPS connections to the server. Nothing may connect *from* the server, and
  the gateway can't reach anything else on your tailnet.
- The server checks every connection again: it accepts your own (untagged) devices and `tag:gateway`, and refuses
  any other tagged machine.

> [!IMPORTANT]
> A new tailnet starts with a policy that lets every device reach every other. If you replace the whole policy with
> the example above, that goes away: devices that used to reach each other no longer will. If you use Tailscale for
> other things, keep your existing rules and add the `tagOwners` entries and the two grants to them.

## A key for the gateway

The gateway on Vercel starts fresh many times a day, so it signs in with a credential instead of a person:

1. In the admin console's **Settings**, open **OAuth clients** and choose **Generate OAuth client**.
2. Give it the **Auth Keys** scope with **Write** access, and add the tag `tag:gateway`.
3. Copy the **client secret**. It starts with `tskey-client-` and is shown only once.

You'll paste it into Vercel in [step 3](03-web-gateway.md). With it, every gateway instance joins as a short-lived
(ephemeral), pre-approved `tag:gateway` machine and disappears from your machine list when Vercel stops it.

<details>
<summary>Using an auth key instead</summary>

An auth key works too: under **Settings › Keys**, generate one that is **Reusable**, **Ephemeral** and
**Pre-approved**, tagged `tag:gateway`. Auth keys expire after at most 90 days, though, and the web app stops
reaching your server when it does. An OAuth client secret doesn't expire.
</details>

## Check

- The **DNS** page shows MagicDNS on and HTTPS enabled.
- **Access controls** saves without errors.
- You have a `tskey-client-…` secret put away somewhere safe.

The server itself shows up in the machine list in the next step.

Next: [2. The server →](02-server.md)
