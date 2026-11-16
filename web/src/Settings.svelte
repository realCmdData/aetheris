<script lang="ts">
  import { onDestroy } from 'svelte'
  import { api, registerPasskey, shareURL, baseName, type Passkey, type Session, type AuditEvent, type Share } from './lib/api'
  import { when, until, allowance } from './lib/format'
  import Icon from './lib/Icon.svelte'

  let passkeys = $state<Passkey[]>([])
  let sessions = $state<Session[]>([])
  let events = $state<AuditEvent[]>([])
  let shares = $state<Share[]>([])
  let copied = $state('')
  let pairing = $state<{ code: string; expires: number; server?: string; lan?: string } | null>(null)
  let now = $state(Date.now())
  let error = $state('')
  const tick = setInterval(() => (now = Date.now()), 1000)
  onDestroy(() => clearInterval(tick))

  const devices = $derived(sessions.filter((s) => s.kind === 'device'))
  const browsers = $derived(sessions.filter((s) => s.kind === 'web'))
  const left = $derived(pairing ? Math.max(0, Math.round((pairing.expires - now) / 1000)) : 0)

  async function load() {
    try {
      ;[passkeys, sessions, events, shares] = await Promise.all([api.passkeys(), api.sessions(), api.audit(), api.shares()])
    } catch (e: any) {
      error = e.message
    }
  }
  load()

  async function run(fn: () => Promise<unknown>) {
    error = ''
    try {
      await fn()
      await load()
    } catch (e: any) {
      if (e?.name !== 'NotAllowedError') error = e.message
    }
  }

  const labels: Record<string, string> = {
    login: 'Signed in', logout: 'Signed out', login_failed: 'Sign-in refused', passkey_registered: 'Passkey added',
    passkey_removed: 'Passkey removed', passkey_rejected: 'Passkey refused', pairing_started: 'Pairing code shown',
    device_paired: 'Device paired', pairing_failed: 'Wrong pairing code', session_revoked: 'Signed out remotely',
    setup_token_created: 'Setup link created', setup_token_rejected: 'Invalid setup link used', reset: 'All passkeys reset',
    delete: 'Moved to Trash', restore_trash: 'Restored from Trash', restore_version: 'Version restored',
    share_created: 'Share link created', share_revoked: 'Share link removed', share_view: 'Shared file viewed', share_download: 'Shared file downloaded',
  }

  async function copyLink(s: Share) {
    await navigator.clipboard.writeText(shareURL(s)).catch(() => {})
    copied = s.id
    setTimeout(() => (copied = ''), 1800)
  }
</script>

<header class="page-head">
  <h1 class="large-title">Settings</h1>
</header>
{#if error}<p class="error" role="alert"><Icon name="alert" size={16} />{error}</p>{/if}

<section>
  <h2 class="title">Passkeys</h2>
  <p class="callout">Keep at least two, for example your phone and this PC, so losing one never locks you out.</p>
  <ul class="group">
    {#each passkeys as p (p.id)}
      <li><Icon name="key" size={18} /><span class="what"><span class="nm">{p.name}</span><span class="caption num">added {when(p.created)} · last used {when(p.lastUsed)}</span></span>
        <button class="btn danger" disabled={passkeys.length < 2} title={passkeys.length < 2 ? 'Add another passkey first' : ''}
          onclick={() => run(() => api.removePasskey(p.id))}>Remove</button></li>
    {/each}
  </ul>
  <button class="btn plain" onclick={() => run(() => registerPasskey())}><Icon name="plus" size={16} />Add a passkey</button>
</section>

<section>
  <h2 class="title">Devices</h2>
  <p class="callout">The Aetheris app on your PC signs in with a pairing code. It only connects at home or over Tailscale.</p>
  {#if pairing && left > 0}
    <div class="code" aria-live="polite">
      <span class="digits num">{pairing.code.slice(0, 4)}-{pairing.code.slice(4)}</span>
      <span class="caption num">Type this into the Aetheris app · expires in {Math.floor(left / 60)}:{String(left % 60).padStart(2, '0')}</span>
      {#if pairing.server}
        <!-- What the app asks for along with the code: the server, and for PCs without Tailscale its address at home -->
        <dl class="where">
          <div><dt>Server</dt><dd>{pairing.server.replace(/^https:\/\//, '')}</dd></div>
          {#if pairing.lan}<div><dt>At home</dt><dd class="num">{pairing.lan}</dd></div>{/if}
        </dl>
      {/if}
    </div>
  {:else}
    <button class="btn plain" onclick={() => run(async () => (pairing = await api.pair()))}><Icon name="laptop" size={16} />Pair a device</button>
  {/if}
  {#if devices.length}
    <ul class="group">
      {#each devices as s (s.id)}
        <li><Icon name="laptop" size={18} /><span class="what"><span class="nm">{s.name}</span><span class="caption num">paired {when(s.created)} · last seen {when(s.lastSeen)}</span></span>
          <button class="btn danger" onclick={() => run(() => api.revoke(s.id))}>Remove</button></li>
      {/each}
    </ul>
  {/if}
</section>

<section>
  <h2 class="title">Shared links</h2>
  <p class="callout">Each opens one file without an account, encrypted with a key that exists only in the link. Create them from a file's menu.</p>
  {#if shares.length}
    <ul class="group">
      {#each shares as s (s.id)}
        <li><Icon name="link" size={18} /><span class="what"><span class="nm">{baseName(s.path)}</span><span class="caption num">{allowance(s.views, s.downloads)} · expires {until(s.expires)}</span></span>
          <button class="btn quiet small" onclick={() => copyLink(s)}>{copied === s.id ? 'Copied' : 'Copy'}</button>
          <button class="btn danger" onclick={() => run(() => api.unshare(s.id))}>Remove</button></li>
      {/each}
    </ul>
  {/if}
</section>

<section>
  <h2 class="title">Browsers</h2>
  <ul class="group">
    {#each browsers as s (s.id)}
      <li><Icon name="globe" size={18} /><span class="what"><span class="nm">{s.name}{s.current ? ' · this browser' : ''}</span><span class="caption num">signed in {when(s.created)} · last seen {when(s.lastSeen)}</span></span>
        {#if !s.current}<button class="btn danger" onclick={() => run(() => api.revoke(s.id))}>Sign out</button>{/if}</li>
    {/each}
  </ul>
  <button class="btn plain" onclick={() => run(api.logout).then(() => location.reload())}><Icon name="logout" size={16} />Sign out of this browser</button>
</section>

<section>
  <h2 class="title">Activity</h2>
  <ul class="group log">
    {#each events.slice(0, 40) as e}
      <li><span class="caption num stamp">{when(e.ts)}</span><span class="what"><span class="nm">{labels[e.event] ?? e.event}{e.detail ? ` · ${e.detail}` : ''}</span><span class="caption">{e.source}</span></span></li>
    {/each}
  </ul>
</section>

<style>
  section { padding: 0 0 2.25rem; max-width: 46rem; display: grid; gap: .75rem; justify-items: start; }
  section p, .error { margin: 0; }
  .error { margin-bottom: 1rem; }
  /* grouped rows on one outlined card, hairlines between them */
  .group {
    list-style: none; margin: 0; padding: 0 .5rem 0 1rem; width: 100%; border-radius: .75rem;
    background: var(--surface); border: 1px solid var(--separator-strong);
  }
  li :global(svg) { color: var(--text-2); flex: none; }
  .group:empty { display: none; }
  li { display: flex; align-items: center; gap: .85rem; min-height: 3.4rem; padding: .55rem 0; color: var(--text-3); }
  li + li { border-top: 1px solid var(--separator); }
  .what { flex: 1; min-width: 0; display: grid; gap: .1rem; }
  .nm { font: var(--type-name); color: var(--text); overflow-wrap: anywhere; }
  .stamp { width: 8.5rem; flex: none; }
  .log li { align-items: start; }
  .btn.danger, .btn.small { min-height: 2.1rem; padding: 0 .75rem; font-size: .82rem; }
  .code {
    display: grid; gap: .35rem; padding: 1.1rem 1.4rem; border-radius: .75rem; background: var(--surface-2);
  }
  .digits { font: 400 2rem/1.1 var(--font); letter-spacing: .1em; color: var(--accent-text); }
  .where { display: grid; gap: .3rem; margin: .5rem 0 0; font: var(--type-callout); }
  .where div { display: flex; gap: .75rem; }
  .where dt { width: 4.5rem; flex: none; color: var(--text-3); }
  .where dd { margin: 0; min-width: 0; overflow-wrap: anywhere; color: var(--text); user-select: all; }
  @media (max-width: 760px) { .stamp { width: 6.5rem; } }
</style>
