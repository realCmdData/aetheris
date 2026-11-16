# Development

[← Documentation](../README.md#documentation)

## The repository

| Part | Folder | Built with |
|---|---|---|
| Server | `server/` | Go, one static binary with Tailscale built in (tsnet), SQLite (pure Go, no cgo) |
| Web gateway | `gateway/` | Go on Vercel; serves the built web app from `gateway/public` and forwards the API over WireGuard |
| Web app | `web/` | Svelte 5 and Vite. `web/src/desktop` is the Windows app's window, sharing the same design system. |
| Windows app | `client-win/` | C# on .NET 10, the Cloud Files API, WebView2, plain Win32 through CsWin32 |
| Scripts | `scripts/` | Deploying the server to a NAS over SSH, installing a local build of the Windows app |

Further reading:

- [`docs/protocol.md`](protocol.md): the API shared by the server, the gateway, the web app and the Windows app
- [`PLAN.md`](../PLAN.md): architecture, decisions, the design system, and progress

## What you need

- [Go](https://go.dev/dl/) 1.27 or later
- [Node.js](https://nodejs.org) 24 and [pnpm](https://pnpm.io) 10, for the web app
- The [.NET 10 SDK](https://dotnet.microsoft.com/download), on Windows, for the Windows app

## Run it locally

The server has a plain-HTTP development listener on loopback, so you can run everything on one PC without Tailscale.

```bash
# The server, serving .dev/data, with its state in .dev/state
cd server && go build -o ../.dev/aetheris.exe ./cmd/aetheris && cd ..
.dev/aetheris.exe serve -data .dev/data -state .dev/state -hostname= -dev 127.0.0.1:8080 -rpid localhost -origins http://localhost:5173

# The web app on http://localhost:5173 (it forwards /api to :8080)
pnpm --dir web install
pnpm --dir web dev
```

To sign in, either create a passkey (`.dev/aetheris.exe setup -state .dev/state` prints a link; browsers allow
passkeys on `localhost`), or skip passkeys with a development session:

```bash
cd server && go run ./cmd/devsession -state ../.dev/state
```

and set the token it prints as a cookie in the browser's console on `http://localhost:5173`:

```js
document.cookie = "__Host-aetheris=<token>; Path=/; Secure"
```

### The Windows app's window

`pnpm --dir web dev:desktop` serves the window on `http://localhost:5174` with a mock in place of the app, for
designing in a browser. Add a hash to see other states: `#unpaired` (first-time setup), `#busy` (transfers),
`#long` (long upload), `#update` (an update waiting).

### The Windows app

With `--dev`, the app talks to the development server on `http://127.0.0.1:8080` and uses a separate
`Aetheris Dev` folder and settings, so it never touches a real installation:

```powershell
pnpm --dir web build:desktop      # the window, into client-win/ui
dotnet run --project client-win -- --dev --show
```

`scripts/install-client.ps1` builds the app as a release does and installs it for your user. A local build is
version 0.0.0, so it never updates itself.

## Tests

```bash
cd server && go test ./...
cd gateway && go test ./...
```

The web app and the Windows app are checked by building them and using them against the development server.

## Building

```bash
pnpm --dir web build                     # the web app, into gateway/public (commit it: Vercel serves it as is)
pnpm --dir web build:desktop             # the Windows app's window, into client-win/ui
dotnet publish client-win -c Release     # one self-contained exe that installs itself
```

Keep the web app's JavaScript under 60 KB gzipped; the build prints the size.

## Releases

Push a version tag, and [`.github/workflows/release.yml`](../.github/workflows/release.yml) builds
`AetherisSetup.exe`, publishes the release with it, and the Windows apps out there update themselves:

```bash
git tag v1.3.0
git push origin v1.3.0
```

The server isn't part of the release: build and deploy it as in
[Looking after it](self-hosting/06-maintenance.md#the-server).

## Documentation screenshots

The pictures in `docs/images` come from a development server with generated sample files (nothing personal), the
web app at 1280×800 and phone size, and the window's mock at its real size, all at 2× scale.
