<script lang="ts">
  import Logo from './lib/Logo.svelte'
  import Icon from './lib/Icon.svelte'
  import { Spring } from './lib/spring'
  import { COMPUTERS } from './lib/api'
  import Files from './Files.svelte'
  import Trash from './Trash.svelte'
  import Recent from './Recent.svelte'
  import Settings from './Settings.svelte'

  let hash = $state(location.hash)
  $effect(() => {
    const update = () => ((hash = location.hash), (pressed = null))
    addEventListener('hashchange', update)
    return () => removeEventListener('hashchange', update)
  })
  // Routes: #/files/<path>, #/search/<text>, #/recent, #/trash, #/settings
  const route = $derived.by(() => {
    const h = hash.replace(/^#\/?/, '')
    if (h.startsWith('search/')) return { view: 'search', path: '', query: decodeURIComponent(h.slice(7)) }
    if (h.startsWith('recent')) return { view: 'recent', path: '', query: '' }
    if (h.startsWith('trash')) return { view: 'trash', path: '', query: '' }
    if (h.startsWith('settings')) return { view: 'settings', path: '', query: '' }
    const rest = h.replace(/^files\/?/, '').replace(/\/$/, '')
    return { view: 'files', path: rest ? rest.split('/').map(decodeURIComponent).join('/') : '', query: '' }
  })
  // Computers is the files view at Computers/…, with its own place in the sidebar.
  const section = $derived(route.view === 'files' && (route.path === COMPUTERS || route.path.startsWith(COMPUTERS + '/')) ? 'computers' : route.view)
  const nav = [
    { view: 'files', href: '#/files/', icon: 'folder', label: 'My files' },
    { view: 'computers', href: `#/files/${COMPUTERS}`, icon: 'computer', label: 'Computers' },
    { view: 'recent', href: '#/recent', icon: 'schedule', label: 'Recent' },
    { view: 'trash', href: '#/trash', icon: 'delete', label: 'Trash' },
    { view: 'settings', href: '#/settings', icon: 'settings', label: 'Settings' },
  ]

  // The indicator answers the press, not the release, and glides on a critically damped spring from
  // wherever it is on screen. A press that is dragged away and cancelled hands it back to the route.
  let pressed = $state<string | null>(null)
  let links: HTMLAnchorElement[] = $state([])
  let indicatorY = $state(0)
  let indicatorOn = $state(false) // search results belong to no destination
  const indicator = new Spring(0, (v) => (indicatorY = v))
  let placed = false
  $effect(() => {
    const el = links[nav.findIndex((n) => n.view === (pressed ?? section))]
    indicatorOn = !!el
    if (!el) return
    if (placed) indicator.to(el.offsetTop, { response: 0.26 })
    else (indicator.set(el.offsetTop), (placed = true))
  })

  // Search lives in the top bar, as in Drive: typing shows results for the whole drive (#/search/<text>),
  // clearing it goes back to the folder you were in.
  let q = $state('')
  let searchEl: HTMLInputElement
  let lastFolder = '#/files/'
  $effect(() => {
    q = route.query
    if (route.view === 'files') lastFolder = hash || '#/files/'
  })
  let timer = 0
  function typed() {
    clearTimeout(timer)
    timer = setTimeout(() => {
      if (!q.trim()) return void (route.view === 'search' && (location.hash = lastFolder))
      const next = '#/search/' + encodeURIComponent(q) // untrimmed: the route writes q back, mid-typing
      if (route.view !== 'search') location.hash = next
      else (history.replaceState(null, '', next), (hash = next)) // one history entry per search, not per keystroke
    }, 250)
  }
  function clear() {
    clearTimeout(timer)
    q = ''
    if (route.view === 'search') location.hash = lastFolder
    searchEl.blur()
  }
  function keydown(e: KeyboardEvent) {
    const typing = e.target instanceof HTMLInputElement || e.target instanceof HTMLTextAreaElement
    if (e.key === '/' && !typing && !document.querySelector('[aria-modal="true"], dialog[open]')) (e.preventDefault(), searchEl.focus())
  }

  // "New" belongs to the open folder (upload, new folder); elsewhere it opens My files with the menu showing.
  let addMenu = $state<DOMRect | null>(null)
  function add(e: MouseEvent) {
    e.stopPropagation() // the window click that closes menus must not see this one
    addMenu = (e.currentTarget as HTMLElement).getBoundingClientRect()
    if (route.view !== 'files' && route.view !== 'search') location.hash = '#/files/'
  }
</script>

<svelte:window onkeydown={keydown} />

<div class="app">
  <header class="top">
    <a class="brand" href="#/files/" aria-label="Aetheris, my files"><Logo variant="mark" size={40} label="" /><span class="word">Aetheris</span></a>
    <form class="search" role="search" onsubmit={(e) => e.preventDefault()}>
      <button type="button" class="icon-btn" aria-label="Search" tabindex="-1" onclick={() => searchEl.focus()}><Icon name="search" size={24} /></button>
      <input bind:this={searchEl} type="search" placeholder="Search in Aetheris" aria-label="Search all files" title="Search (/)"
        bind:value={q} oninput={typed} onkeydown={(e) => e.key === 'Escape' && clear()} />
      {#if q}<button type="button" class="icon-btn" aria-label="Clear search" onclick={clear}><Icon name="close" size={22} /></button>{/if}
    </form>
  </header>

  <nav class="side" aria-label="Main">
    <button class="new" aria-haspopup="menu" aria-label="New" onclick={add}><Icon name="add" size={24} /><span>New</span></button>
    <div class="nav">
      <span class="indicator" class:on={indicatorOn} aria-hidden="true" style:transform="translateY({indicatorY}px)"></span>
      {#each nav as n, i}
        {@const here = section === n.view}
        <a class="nav-item" href={n.href} bind:this={links[i]} aria-current={here ? 'page' : undefined}
          onpointerdown={() => (pressed = n.view)} onpointercancel={() => (pressed = null)}
          onpointerleave={(e) => e.buttons && (pressed = null)}>
          <span class="nav-ico"><Icon name={n.icon} fill={here} size={22} /></span><span class="nav-label">{n.label}</span>
        </a>
      {/each}
    </div>
  </nav>

  <main class:pane={route.view === 'files' || route.view === 'search'}>
    {#if route.view === 'files' || route.view === 'search'}
      <Files path={route.path} query={route.query} bind:addMenu />
    {:else if route.view === 'recent'}
      <Recent />
    {:else if route.view === 'trash'}
      <Trash />
    {:else}
      <Settings />
    {/if}
  </main>
</div>

<style>
  /* Drive's frame: a warm top bar and sidebar around one white surface that holds the content */
  .app {
    height: 100dvh; display: grid; grid-template: 'top top' 4rem 'side main' minmax(0, 1fr) / 16rem minmax(0, 1fr);
    background: var(--frame); overflow: hidden;
  }

  .top { grid-area: top; display: flex; align-items: center; gap: .5rem; padding: .5rem 1rem .5rem .75rem; min-width: 0; }
  .brand {
    width: 14.75rem; flex: none; display: flex; align-items: center; gap: .6rem; padding: 0 .5rem; border-radius: 999px;
    color: var(--text-2); text-decoration: none; font: 400 1.375rem/1 var(--font); letter-spacing: 0;
  }
  .search {
    flex: 0 1 45rem; min-width: 0; height: 3rem; display: flex; align-items: center; padding: 0 .25rem; border-radius: 999px;
    background: var(--surface-2); transition: background-color 150ms ease-out, box-shadow 150ms ease-out;
  }
  .search:hover { background: var(--surface-3); }
  .search:focus-within { background: var(--surface); box-shadow: var(--shadow-1); }
  .search input { flex: 1; min-width: 0; height: 100%; border: 0; outline: none; background: transparent; padding: 0 .5rem; font: 400 1rem/1 var(--font); }
  .search input::placeholder { color: var(--text-3); }
  .search input::-webkit-search-cancel-button { display: none; } /* our own clear button */

  .side { grid-area: side; display: flex; flex-direction: column; gap: 1rem; padding: .5rem .75rem 1rem 1rem; min-height: 0; overflow-y: auto; }
  /* The one main action: a raised white button with the brand's coral plus */
  .new {
    align-self: flex-start; display: inline-flex; align-items: center; gap: .75rem; height: 3.5rem; padding: 0 1.25rem 0 1rem;
    border: 0; border-radius: 1rem; background: var(--surface); color: var(--text); font: 500 .875rem/1 var(--font);
    box-shadow: var(--shadow-1); transition: box-shadow 180ms ease-out, background-color 150ms ease-out, transform var(--press-duration) ease-out;
  }
  .new :global(svg) { color: var(--accent); }
  .new:hover { box-shadow: var(--shadow-2); }
  .new:active { transform: scale(.97); }

  .nav { position: relative; display: flex; flex-direction: column; }
  .indicator {
    position: absolute; top: 0; left: 0; right: 0; height: 2.5rem; border-radius: 999px; pointer-events: none; will-change: transform;
    background: var(--container); opacity: 0; transition: opacity 120ms ease-out;
  }
  .indicator.on { opacity: 1; }
  .nav-item {
    position: relative; display: flex; align-items: center; gap: .85rem; height: 2.5rem; padding: 0 1rem; border-radius: 999px;
    color: var(--text-2); text-decoration: none; font: 400 .875rem/1 var(--font); letter-spacing: 0; transition: background-color 120ms ease-out;
  }
  .nav-item:not([aria-current]):hover { background: var(--hover); }
  .nav-item[aria-current='page'] { color: var(--text); font-weight: 500; }
  .nav-ico { display: grid; }

  main {
    grid-area: main; min-width: 0; min-height: 0; overflow-y: auto; margin: 0 1rem 1rem 0; padding: 0 1.5rem 1.5rem;
    background: var(--surface); border-radius: 1rem; scrollbar-width: thin; scrollbar-color: var(--separator-strong) transparent;
  }
  /* Files keeps its header still and scrolls only the objects beneath it */
  main.pane { position: relative; overflow: hidden; display: flex; padding: 0; }

  /* Narrow windows: a navigation rail (icons, the plus alone) */
  @media (max-width: 1000px) {
    .app { grid-template-columns: 5.5rem minmax(0, 1fr); }
    .brand { width: 4.75rem; justify-content: center; }
    .word, .new span { display: none; }
    .side { align-items: center; padding: .5rem .5rem 1rem; }
    .new { align-self: center; width: 3.5rem; padding: 0; justify-content: center; }
    .nav { width: 3.5rem; gap: .25rem; }
    .nav-item { justify-content: center; padding: 0; height: 2.5rem; }
    .nav-label { position: absolute; width: 1px; height: 1px; overflow: hidden; clip-path: inset(50%); }
  }

  /* Phones: search on top, the content full-bleed, a navigation bar at the bottom, the plus floating above it */
  @media (max-width: 760px) {
    .app { grid-template: 'top' 4rem 'main' minmax(0, 1fr) / minmax(0, 1fr); }
    .top { padding: .5rem .75rem; }
    .brand { width: auto; padding: 0; }
    .search { flex: 1; }
    main { margin: 0; border-radius: 1rem 1rem 0 0; padding: 0 1rem calc(6rem + env(safe-area-inset-bottom)); }
    main.pane { padding: 0; }
    .side {
      position: fixed; inset: auto 0 0 0; z-index: 20; flex-direction: row; padding: .75rem .5rem calc(.75rem + env(safe-area-inset-bottom));
      background: var(--surface-2); overflow: visible;
    }
    .new { position: fixed; right: 1rem; bottom: calc(6.25rem + env(safe-area-inset-bottom)); width: 3.5rem; box-shadow: var(--shadow-3); }
    .indicator { display: none; }
    .nav { flex: 1; width: auto; flex-direction: row; justify-content: space-around; gap: 0; }
    .nav-item { flex: 1; flex-direction: column; gap: .25rem; height: auto; padding: 0; background: none !important; font-size: .75rem; font-weight: 500; }
    .nav-ico { width: 4rem; height: 2rem; place-items: center; border-radius: 999px; transition: background-color 150ms ease-out; }
    .nav-item[aria-current='page'] .nav-ico { background: color-mix(in srgb, var(--accent) 24%, var(--surface)); } /* --container fades into the bar */
    .nav-label { position: static; width: auto; height: auto; clip-path: none; }
  }
</style>
