# 3. The web app and gateway

[← 2. The server](02-server.md) · [Run your own Aetheris](../self-hosting.md) · Next: [4. Your first passkey →](04-first-sign-in.md)

The web app runs on Vercel, together with a small gateway that forwards its API calls to your server over the
tailnet. The gateway only lets two kinds of request through: sign-in itself, and requests whose session token your
server has signed. Anything else is turned away at Vercel and never reaches your home.

## Fork the repository

On GitHub, **fork** [realCmdData/aetheris](https://github.com/realCmdData/aetheris). Vercel deploys from your
fork, and you decide when to bring in updates (see [Looking after it](06-maintenance.md#updating)).

The built web app is checked in under `gateway/public`, so nothing needs building unless you change it.

## Create the Vercel project

1. In Vercel, choose **Add New › Project** and import your fork.
2. **Root Directory**: `gateway`.
3. **Framework Preset**: **Go** (Vercel usually detects it from `main.go`). Leave the build and output settings as
   they are.
4. Open **Environment Variables** and add:

   | Name | Value | Notes |
   |---|---|---|
   | `TS_AUTHKEY` | the `tskey-client-…` secret from [step 1](01-tailscale.md#a-key-for-the-gateway) | Mark it **Sensitive**. |
   | `AETHERIS_PUBKEY` | what `aetheris pubkey` prints on the server | The public half of the session-signing key. Without it, every request fails closed. |
   | `AETHERIS_NAS` | `aetheris.tail1234.ts.net` | *Optional.* Only needed if you gave the server another `-hostname`. By default the gateway finds `aetheris` in its own tailnet. |

   Get the public key on the server:

   ```console
   $ sudo -u aetheris /opt/aetheris/aetheris pubkey -state /var/lib/aetheris
   MCowBQYDK2VwAyEA3m1v…
   ```

5. Choose **Deploy**.

Changing an environment variable later takes a redeploy (**Deployments**, the latest one's menu, **Redeploy**).

### Choose a region near home

Every API call travels from Vercel to your server and back, so put the gateway close to it: in the project's
**Settings › Functions**, set the **Function Region** to the one nearest your home (for example `fra1` for Germany,
`iad1` for the US east coast), then redeploy.

## Add your domain

In the project's **Settings › Domains**, add `drive.example.com`. Vercel shows the DNS record to create at your domain
registrar (a `CNAME` for a subdomain); once DNS has caught up, Vercel issues the certificate by itself.

The domain must be exactly the address you gave the server with `setup -origins`. Passkeys only work there, not on
the `*.vercel.app` address.

## Rate-limit sign-in (recommended)

Sign-in requests are the only ones that reach your server without a session. Your server limits them as well, but
stopping floods at Vercel is cheaper. If your Vercel plan offers firewall rules, add one in the project's
**Firewall** tab:

- **If** the request path starts with `/api/v1/auth/`
- **Then** rate limit: 30 requests per 60 seconds per IP address, then deny.

## Check

```console
$ curl https://drive.example.com/api/v1/auth/status
{"hasPasskeys":false,"signedIn":false}
```

That answer came from your server, through Vercel and the tailnet. Opening `https://drive.example.com` in a browser
shows **Not set up yet**, which is right: there's no passkey yet. In the Tailscale console, the gateway shows up under
**Machines** as **aetheris-gateway**, an ephemeral machine with the tag **gateway**.

If the page says **Your NAS isn't reachable** instead, the gateway couldn't reach the server: see
[Troubleshooting](../troubleshooting.md#the-web-app-says-your-nas-isnt-reachable).

Next: [4. Your first passkey →](04-first-sign-in.md)
