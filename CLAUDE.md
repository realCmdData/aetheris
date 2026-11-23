# Aetheris

Self-hosted Drive/OneDrive-style storage: Windows PC ⇄ a home NAS, and a web UI on your own domain via a Vercel gateway.

**Start every session by reading `PLAN.md`** (decisions, facts, design system, phases).
Work the first unchecked item of the current phase. Before the session ends: tick finished items,
update the Status table, and add one line to the Session log, so the next session can continue.

`PLAN.md`, the docs and code comments use example values (`drive.example.com`, `192.168.1.20`, `tail1234.ts.net`,
`DESKTOP-01`, `NAS-01`). An installation's real values, and how to reach its NAS, belong in `CLAUDE.local.md`
(git-ignored): never commit them.

Ground rules:
- Never expose the NAS or QTS to the internet: no port forwards, no UPnP. Only the `aetheris` process joins the tailnet.
- The NAS is weak (J1900, 2 GB RAM, QTS 4.3.4). One static Go binary, no Docker, RSS < 100 MB.
- Files stay plain files on the `Aetheris` share. Never introduce a blob store or proprietary format.
- The PC client never routes through Vercel (LAN first, then tailnet).
- Claude never types NAS, Tailscale, Vercel or DNS registrar passwords. The user does logins; SSH uses key auth.
- UI work follows the Design system in `PLAN.md` and the `apple-design` skill.
- `.env` (git-ignored) is the user's. Never print or use its password values.

Diagnosing the Windows app: its live log is `%LOCALAPPDATA%\Aetheris\<profile>\aetheris.log` (`nas`, or `dev`; the previous
run's is `.old`), and the zips from **Export support log…** land in `%LOCALAPPDATA%\Aetheris\<profile>\support\`.

Reaching the NAS over SSH: send scripts as a heredoc (`ssh … sh -s <<'EOF'`), so PowerShell's CRLF never reaches the
NAS shell. The NAS has no usable CA store and no `/dev/net/tun`. Go binaries embed fallback roots and use tsnet in
userspace.
