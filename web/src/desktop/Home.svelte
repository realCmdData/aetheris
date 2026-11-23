<script lang="ts">
  import { SvelteSet } from 'svelte/reactivity'
  import Icon from '../lib/Icon.svelte'
  import Logo from '../lib/Logo.svelte'
  import { size } from '../lib/format'
  import { send, type State, type Event } from './bridge'
  import { glide, ago, base, folder } from './glide'

  let { app, freed, onfreed, folderError, exported, onexport }: {
    app: State; freed: number | null; onfreed: () => void; folderError: string | null
    exported: { path: string | null; error: string | null } | null; onexport: () => void
  } = $props()

  let now = $state(Date.now())
  let freeing = $state(false)
  let confirming = $state(false)
  let stopping = $state<string | null>(null) // the folder whose "Stop syncing" waits for a yes
  const open = new SvelteSet<string>() // activity entries shown in full
  let exporting = $state(false)

  $effect(() => {
    const t = setInterval(() => (now = Date.now()), 15_000)
    return () => clearInterval(t)
  })
  $effect(() => {
    if (freed !== null) freeing = false
  })
  $effect(() => {
    if (exported !== null) exporting = false
  })

  function exportLog() {
    exporting = true
    onexport()
    send({ type: 'support' })
  }

  // The plinth answers one question: what is the folder doing right now. One title, one hairline, one line of fact.
  const done = $derived(app.transfers.reduce((n, x) => n + x.done, 0))
  const total = $derived(app.transfers.reduce((n, x) => n + x.size, 0))
  // A long upload (a folder just added, say) gets one steady line with an estimate instead of a title per file.
  const big = $derived(app.longUpload)
  const eta = (s: number) => (s < 60 ? 'Less than a minute' : s < 3600 ? `About ${Math.ceil(s / 60)} min` : `About ${Math.floor(s / 3600)} h ${Math.round((s % 3600) / 60)} min`)
  const hero = $derived.by(() => {
    if (big) return { line: 'progress', sub: big.left == null ? 'Counting what’s left…' : `${eta(big.left)} left · ${big.files.toLocaleString()} file${big.files === 1 ? '' : 's'}, ${size(big.bytes)} to go` }
    if (app.transfers.length) return { line: 'progress', sub: `${size(done)} of ${size(total)}` }
    switch (app.phase) {
      case 'UpToDate': return { line: 'full', sub: `Synced ${ago(app.lastSync, now)}${app.route ? ` · ${app.route}` : ''}` }
      case 'Paused': return { line: 'faint', sub: 'Nothing syncs in the background until you resume.' }
      case 'Offline': return { line: 'faint', sub: 'Changes on this PC wait here and go up once it’s back.' }
      case 'Failed': return { line: 'error', sub: app.detail }
      default: return { line: 'busy', sub: 'Comparing this PC with your NAS' }
    }
  })

  const kinds: Record<string, { icon: string; label: (e: Event) => string; tone?: string }> = {
    up: { icon: 'upload', label: () => 'Uploaded' },
    down: { icon: 'download', label: () => 'Downloaded' },
    delete: { icon: 'trash', label: () => 'Deleted · in the NAS trash' },
    move: { icon: 'rename', label: (e) => (folder(e.path) === folder(e.to ?? '') ? `Renamed to ${base(e.to ?? '')}` : `Moved to ${folder(e.to ?? '') || 'Aetheris'}`) },
    free: { icon: 'cloud', label: () => 'Space freed on this PC' },
    keep: { icon: 'pin', label: () => 'Kept on this PC' },
    conflict: { icon: 'alert', label: (e) => `Changed on both sides · yours is “${base(e.to ?? '')}”`, tone: 'warn' },
    new: { icon: 'plus', label: () => 'New on your NAS' },
    changed: { icon: 'sync', label: () => 'Changed on your NAS' },
    removed: { icon: 'trash', label: () => 'Deleted on your NAS' },
    moved: { icon: 'rename', label: (e) => `Moved on your NAS to ${folder(e.to ?? '') || 'Aetheris'}` },
    added: { icon: 'computer', label: () => 'Now syncing from this PC' },
    stopped: { icon: 'computer', label: () => 'Stopped syncing · files kept' },
  }
  // A synced folder's items live on the NAS under Computers/<this PC>/: here the folder's own name is enough.
  const mine = $derived(`Computers/${app.computer}/`)
  const shown = (e: Event): Event => ({ ...e, path: e.path.replace(mine, ''), to: e.to?.replace(mine, '') ?? null })

  const here = $derived(app.storage.files + app.storage.online)
  const paused = $derived(app.phase === 'Paused')

  function free() {
    freeing = true
    onfreed()
    send({ type: 'free' })
  }
</script>

<main>
  <section class="plinth card" aria-live="polite">
    <span class="emblem"><Logo variant="mark" size={68} label="" /></span>
    <h1 class="large-title">{big ? 'Long upload in progress' : app.status}</h1>
    <span class="line {hero.line}" aria-hidden="true" use:glide={big ? 100 * big.done : total ? (100 * done) / total : 0}><span></span></span>
    {#if app.phase === 'Failed' && !app.transfers.length}
      <p class="error sub"><Icon name="alert" size={15} />{hero.sub}</p>
    {:else}
      <p class="caption num sub">{hero.sub}</p>
    {/if}
    <div class="opens">
      <button class="btn plain open" onclick={() => send({ type: 'open', what: 'folder' })}><Icon name="folder" size={17} />Open Aetheris</button>
      {#if app.drive.letter}
        <button class="circle-btn letter" onclick={() => send({ type: 'open', what: 'drive' })} aria-label="Open drive {app.drive.letter}:" title="Drive {app.drive.letter}:">{app.drive.letter}:</button>
      {/if}
      {#if app.web}<button class="circle-btn" onclick={() => send({ type: 'open', what: 'web' })} aria-label="Open the web app" title="Web"><Icon name="globe" size={17} /></button>{/if}
    </div>
    <div class="switches">
      <div class="switch-row">
        <span id="autostart">Start with Windows</span>
        <button class="switch" role="switch" aria-checked={app.autostart} aria-labelledby="autostart"
          onclick={() => send({ type: 'autostart', on: !app.autostart })}><span></span></button>
      </div>
      <div class="switch-row">
        <span id="pause">Pause syncing</span>
        <button class="switch" role="switch" aria-checked={paused} aria-labelledby="pause"
          onclick={() => send({ type: paused ? 'resume' : 'pause' })}><span></span></button>
      </div>
    </div>
  </section>

  {#if app.transfers.length}
    <section class="group">
      {#each app.transfers as t (t.dir + t.path)}
        <div class="row transfer">
          <Icon name={t.dir === 'down' ? 'download' : 'upload'} size={18} />
          <div class="what">
            <span class="title-line">{base(t.path)}</span>
            <span class="bar" use:glide={t.size ? (100 * t.done) / t.size : 0}><span></span></span>
            <span class="caption num">{size(t.done)} of {size(t.size)}</span>
          </div>
        </div>
      {/each}
    </section>
  {/if}

  <h2>On this PC</h2>
  <section class="group">
    <div class="row storage">
      <div class="what">
        <span class="figure num">{size(app.storage.bytes)}</span>
        <span class="caption num">{app.storage.files} of {here} files on this PC</span>
        <span class="caption num">{app.storage.online} online-only{app.storage.pinned ? ` · ${app.storage.pinned} always kept here` : ''}</span>
        <span class="share" style:--p={here ? (100 * app.storage.files) / here : 0} aria-hidden="true"><span></span></span>
      </div>
      <button class="btn plain" onclick={free} disabled={freeing || app.storage.files === app.storage.pinned}>{freeing ? 'Freeing…' : 'Free up space'}</button>
    </div>
    {#if freed !== null}<p class="row note caption">Freed {size(freed)}. Files download again when you open them.</p>{/if}
    <label class="row">
      <Icon name="cloud" size={18} />
      <span class="what">
        <span class="title-line">Free up space automatically</span>
        <span class="caption">{app.autoFree
          ? `Files not opened for ${app.autoFree} days become online-only, here and in your folders from this PC. Your NAS keeps them, and opening one brings it back.`
          : 'Everything you have here stays here.'}{app.autoFree && !app.recordsOpens ? ' Windows doesn’t record when files are opened on this PC, so it goes by when they last changed.' : ''}</span>
      </span>
      <select class="select" value={String(app.autoFree)} onchange={(e) => send({ type: 'autoFree', days: Number((e.target as HTMLSelectElement).value) })}>
        <option value="0">Off</option>
        {#each [7, 14, 30, 60, 90] as d}<option value={String(d)}>{d} days</option>{/each}
      </select>
    </label>
    <label class="row">
      <Icon name="drive" size={18} />
      <span class="what"><span class="title-line">Also as a drive letter</span><span class="caption">A shortcut, without status icons</span></span>
      <select class="select" value={app.drive.letter ?? ''} onchange={(e) => send({ type: 'drive', letter: (e.target as HTMLSelectElement).value || null })}>
        <option value="">Off</option>
        {#each app.drive.free as l}<option value={l}>{l}:</option>{/each}
      </select>
    </label>
  </section>

  <h2>Folders from this PC</h2>
  <section class="group">
    {#each app.folders as f (f.path)}
      {#if stopping === f.path}
        <div class="row confirm">
          <span class="what"><span class="title-line">Stop syncing “{f.name}”?</span><span class="caption">Its files stay on this PC and on your NAS.</span></span>
          <button class="btn quiet" onclick={() => (stopping = null)}>Cancel</button>
          <button class="btn danger-fill" onclick={() => (send({ type: 'removeFolder', path: f.path }), (stopping = null))}>Stop syncing</button>
        </div>
      {:else}
        <div class="row">
          <Icon name="folder" fill size={18} />
          <button class="what plain-btn" onclick={() => send({ type: 'open', what: 'folder', path: f.path })} title="Open in Explorer">
            <span class="title-line">{f.name}</span>
            <span class="caption">{f.here ? f.path : `Not on this PC right now · ${f.path}`}</span>
          </button>
          <button class="icon-btn" aria-label="Stop syncing {f.name}" title="Stop syncing" onclick={() => (stopping = f.path)}><Icon name="close" size={18} /></button>
        </div>
      {/if}
    {/each}
    <button class="row link" onclick={() => send({ type: 'addFolder' })}>
      <Icon name="create_new_folder" size={18} />Sync a folder from this PC…
    </button>
    {#if folderError}<p class="row note error" role="alert"><Icon name="alert" size={16} />{folderError}</p>{/if}
    {#if app.folders.length && app.web}
      <button class="row link quiet-link" onclick={() => send({ type: 'open', what: 'computer' })}>
        <Icon name="globe" size={18} /><span class="what"><span class="title-line">See them on the web</span><span class="caption">Computers › {app.computer}</span></span>
      </button>
    {/if}
  </section>

  <h2>Activity</h2>
  <section class="group">
    {#each app.activity.map(shown) as e (e.at + e.kind + e.path)}
      {@const k = kinds[e.kind] ?? kinds.changed}
      {@const key = e.at + e.kind + e.path}
      <!-- A press shows the whole entry (names and folders cut short with …) and its exact time. -->
      <button class="row event" aria-expanded={open.has(key)} onclick={() => (open.has(key) ? open.delete(key) : open.add(key))}>
        <span class="glyph {k.tone ?? ''}"><Icon name={k.icon} size={16} /></span>
        <span class="what">
          <span class="title-line">{e.kind === 'free' ? e.path : base(e.path)}</span>
          <span class="caption">{k.label(e)}{folder(e.path) && e.kind !== 'free' ? ` · ${folder(e.path)}` : ''} · {open.has(key) ? new Date(e.at).toLocaleString() : ago(e.at, now)}</span>
        </span>
        {#if e.size}<span class="caption num">{size(e.size)}</span>{/if}
      </button>
    {:else}
      <p class="row note caption">Nothing yet. Changes in your folder and on your NAS show up here.</p>
    {/each}
  </section>

  <h2>Account</h2>
  <section class="group">
    <div class="row">
      <Icon name="key" size={18} />
      <span class="what"><span class="title-line">Paired with your server</span><span class="caption">{app.host}{app.home ? ` · at home ${app.home}` : ''}</span></span>
    </div>
    <div class="row">
      <Icon name="info" size={18} />
      <span class="what">
        <span class="title-line num">Aetheris {app.version === '0.0.0' ? '(local build)' : app.version}</span>
        <span class="caption">{app.update ? `Updating to ${app.update}…` : app.version === '0.0.0' ? 'Updates come with released versions' : 'Updates install by themselves'}</span>
      </span>
    </div>
    <button class="row link quiet-link support" onclick={exportLog} disabled={exporting}>
      <Icon name="description" size={18} />
      <span class="what">
        <span class="title-line">{exporting ? 'Exporting…' : 'Export support log…'}</span>
        <span class="caption">The log, settings and recent activity in one zip. Nothing that signs in.</span>
      </span>
    </button>
    {#if exported?.path}<p class="row note caption saved">Saved as {exported.path}. Explorer shows it.</p>{/if}
    {#if exported?.error}<p class="row note error" role="alert"><Icon name="alert" size={16} />{exported.error}</p>{/if}
    {#if confirming}
      <div class="row confirm">
        <span class="what"><span class="title-line">Sign this PC out?</span><span class="caption">It stops syncing. Your files on the NAS stay as they are.</span></span>
        <button class="btn quiet" onclick={() => (confirming = false)}>Cancel</button>
        <button class="btn danger-fill" onclick={() => send({ type: 'signout' })}>Sign out</button>
      </div>
    {:else}
      <button class="row link danger" onclick={() => (confirming = true)}><Icon name="logout" size={18} />Sign out of this PC…</button>
    {/if}
  </section>
</main>

<style>
  main { max-width: 23rem; margin: 0 auto; padding: 1.5rem 1.25rem 2rem; }

  /* The plinth: emblem, one sculptural state title, a hairline, one action, two switches */
  .plinth { display: flex; flex-direction: column; align-items: center; gap: 1.1rem; padding: 2.1rem 1.75rem 1.6rem; border-radius: 26px; }
  .emblem { display: grid; margin-bottom: -.2rem; }
  .plinth h1 { text-align: center; font-size: 2.1rem; }
  .sub { margin: -.35rem 0 0; text-align: center; max-width: 100%; }
  .sub.error { justify-content: center; }

  /* Hairline state: full when synced, a travelling glint while comparing, true progress while moving files */
  .line { position: relative; display: block; width: 100%; height: 2px; border-radius: 999px; background: var(--separator); overflow: hidden; }
  .line > span { position: absolute; inset: 0; border-radius: inherit; background: var(--brand-gradient); transform-origin: left; }
  .line.progress > span { transform: scaleX(calc(var(--p, 0) / 100)); }
  .line.busy > span { width: 45%; animation: glint 1.35s cubic-bezier(.4, 0, .2, 1) infinite; }
  .line.faint > span { background: var(--text-3); opacity: .25; }
  .line.error > span { background: var(--error); opacity: .6; }
  @keyframes glint { from { transform: translateX(-100%); } to { transform: translateX(240%); } }

  .opens { display: flex; gap: .5rem; width: 100%; }
  .open { flex: 1; min-height: 2.65rem; }
  .letter { font: 400 .8rem/1 var(--font); color: var(--text); }

  .switches { width: 100%; display: grid; gap: .9rem; padding-top: .35rem; }
  .switch-row { display: flex; align-items: center; justify-content: space-between; gap: 1rem; font: 300 .84rem/1.3 var(--font); color: var(--text-2); }

  .storage .caption, label.row .caption, label.row .title-line, .support .caption { white-space: normal; } /* facts and settings, not names: let them wrap */

  .figure { font: var(--type-title); letter-spacing: var(--track-display); }
  .storage { align-items: flex-start; padding-top: .9rem; padding-bottom: .9rem; }
  .storage + .row::before { left: 1rem; }
  .storage .btn { align-self: center; flex: none; min-height: 2.15rem; padding: 0 .85rem; font-size: .8rem; }
  .share, .bar { display: block; height: 3px; border-radius: 999px; background: var(--separator-strong); overflow: hidden; margin: .45rem 0 .1rem; }
  .share > span, .bar > span { display: block; height: 100%; width: 100%; border-radius: inherit; background: var(--brand-gradient);
    transform-origin: left; transform: scaleX(calc(var(--p, 0) / 100)); }
  .bar { margin: .3rem 0 .15rem; }

  .glyph { display: grid; place-items: center; width: 1.9rem; height: 1.9rem; border-radius: 50%; flex: none; background: var(--hover); color: var(--text-3); }
  .glyph.warn { background: light-dark(rgb(180 35 24 / .08), rgb(249 112 102 / .14)); color: var(--error); }
  .event .caption.num { flex: none; }
  .event { cursor: pointer; }
  .event:hover { background: var(--card-hover); }
  .event:active { background: var(--hover); }
  .event[aria-expanded='true'] .title-line, .event[aria-expanded='true'] .what .caption { white-space: normal; overflow-wrap: anywhere; }

  .select {
    flex: none; min-height: 2rem; padding: 0 1.9rem 0 .75rem; border-radius: var(--radius-s); border: 1px solid var(--separator-strong);
    background: var(--card) url("data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' width='12' height='12' viewBox='0 0 24 24' fill='none' stroke='%23888' stroke-width='2.5' stroke-linecap='round'%3E%3Cpath d='M6 9l6 6 6-6'/%3E%3C/svg%3E") no-repeat right .6rem center;
    appearance: none; color: var(--text); font: 400 .82rem/1 var(--font); cursor: pointer;
  }
  .select:focus-visible { outline: 2px solid var(--accent); outline-offset: 1px; }

  /* A folder's name and path open it; the row itself stays still */
  .plain-btn { border: 0; padding: 0; background: none; text-align: left; color: inherit; cursor: pointer; }
  .plain-btn:hover .title-line { text-decoration: underline; text-decoration-color: var(--separator-strong); text-underline-offset: 3px; }
  .row .icon-btn { flex: none; width: 2rem; height: 2rem; }
  .quiet-link { color: var(--text-2); }
  .error.note { gap: .5rem; }
  .saved { overflow-wrap: anywhere; }

  @media (prefers-reduced-motion: reduce) {
    .line.busy > span { animation: none; width: 100%; opacity: .35; } /* no travelling glint, just a quiet line */
  }
</style>
