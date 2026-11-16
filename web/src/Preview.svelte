<script lang="ts">
  import { onMount } from 'svelte'
  import { fly } from 'svelte/transition'
  import { cubicOut } from 'svelte/easing'
  import { fileURL, baseName, type Entry } from './lib/api'
  import { kind, kindLabel, size, when } from './lib/format'
  import { Spring, project, rubberband, reducedMotion } from './lib/spring'
  import Icon from './lib/Icon.svelte'
  import Visual from './lib/Visual.svelte'

  // Quick Look grows out of the object that was opened and goes back into the one showing when it closes, on
  // springs that start from whatever is on screen, so a close can be grabbed and reversed mid-flight.
  // items: what the arrows step through (the files of the open view, in their order). The objects behind carry
  // data-path: that's where the card grows from and returns to, and how a double-click on one is found.
  let { entry: opened, items = [], onclose, onshare, onchange }: {
    entry: Entry; items?: Entry[]; onclose: () => void; onshare?: (e: Entry) => void; onchange?: (e: Entry) => void
  } = $props()
  // svelte-ignore state_referenced_locally (on purpose: it starts at the opened file, then follows the arrows)
  let entry = $state(opened) // the one showing; arrows and double-clicks change it
  let dir = $state(0) // the way the last step went: new content slides in from that side
  const k = $derived(kind(entry.path))
  // Android's Chrome has no inline PDF viewer: offer to open it in the phone's own viewer instead.
  const bare = $derived(k === 'file' || (k === 'pdf' && navigator.pdfViewerEnabled === false))
  const src = $derived(fileURL(entry.path, true))
  const name = $derived(baseName(entry.path))
  const at = $derived(items.findIndex((e) => e.path === entry.path))
  const prev = $derived(at > 0 ? items[at - 1] : null)
  const next = $derived(at >= 0 && at < items.length - 1 ? items[at + 1] : null)
  let card: HTMLElement
  let text = $state<string | null>(null)
  let p = $state(0) // 0 = resting in its tile, 1 = open
  let y = $state(0) // vertical drag offset, px
  let box = $state({ dx: 0, dy: 0, s: 0.4, h: 600 })
  let closing = false
  let returnFocus: HTMLElement | null = null
  // Spring's rest threshold is in px, so the open progress runs 0..1000 and p is that / 1000.
  const grow = new Spring(0, (v) => (p = v / 1000))
  const drag = new Spring(0, (v) => (y = v))

  // Where an object sits on screen. Off screen (scrolled away) it has no place to grow from: rise from below.
  function originOf(e: Entry) {
    const el = document.querySelector(`[data-path="${CSS.escape(e.path)}"]`)
    const r = (el?.querySelector('.thumb .visual') ?? el?.querySelector('.visual'))?.getBoundingClientRect()
    return r?.width && r.bottom > 0 && r.top < innerHeight ? r : null
  }
  function measure() {
    const o = originOf(entry)
    const w = card.offsetWidth, h = card.offsetHeight // layout size, unaffected by the transform
    box = o
      ? { dx: o.x + o.width / 2 - innerWidth / 2, dy: o.y + o.height / 2 - innerHeight / 2, s: Math.min(1, Math.max(0.12, o.width / w)), h }
      : { dx: 0, dy: innerHeight * 0.3, s: 0.9, h }
  }

  onMount(() => {
    returnFocus = document.activeElement as HTMLElement | null
    measure()
    grow.to(1000, { dampingRatio: 0.94, response: 0.32 })
    card.focus()
  })

  $effect(() => {
    text = null
    if (k !== 'text') return
    const ctl = new AbortController() // stepping on before it arrives drops the old file's text
    fetch(src, { headers: { Range: 'bytes=0-1048575' }, signal: ctl.signal }) // first 1 MB is plenty for a preview
      .then((r) => r.text()).then((t) => (text = t)).catch(() => !ctl.signal.aborted && (text = ''))
    return () => ctl.abort()
  })

  function close(velocity = 0) {
    clearTimeout(pending)
    closing = true
    measure() // back into the object showing now, which may not be the one that was opened
    drag.to(0, { response: 0.32, velocity }) // keeps the gesture's speed, then settles into the tile
    grow.to(0, { response: 0.3, onRest: () => (onclose(), returnFocus?.focus()) })
  }

  /** Shows another object in the same card; d is the direction it came from (-1 left, 1 right, 0 in place). */
  function show(e: Entry, d = 0) {
    clearTimeout(pending)
    if (closing) (closing = false), grow.to(1000, { dampingRatio: 0.94, response: 0.32 }) // caught on its way out
    if (e.path === entry.path) return
    dir = d
    entry = e
    drag.to(0)
    onchange?.(e)
  }

  // A click beside the card closes it. Over an object it waits a beat, since it may be the first half of a
  // double-click, which opens that object here at once (a slow double-click still catches the closing card).
  let pending = 0
  function scrimClick(ev: MouseEvent) {
    const hit = objectAt(ev.clientX, ev.clientY)
    clearTimeout(pending)
    if (!hit) return close()
    if (ev.detail >= 2) return show(hit)
    pending = setTimeout(close, 300)
  }
  function objectAt(x: number, y: number) {
    for (const el of document.elementsFromPoint(x, y)) {
      const path = el.closest('[data-path]')?.getAttribute('data-path')
      if (path) return items.find((e) => e.path === path) ?? null // a folder isn't previewable: no hit
    }
    return null
  }

  function keydown(e: KeyboardEvent) {
    if (e.key === 'Escape') return close()
    if (e.target instanceof HTMLMediaElement) return // arrows seek in a focused player
    if (e.key === 'ArrowLeft' && prev) (e.preventDefault(), show(prev, -1))
    else if (e.key === 'ArrowRight' && next) (e.preventDefault(), show(next, 1))
  }

  // Dragging: 1:1 from where it was grabbed, rubber-band upwards, momentum decides on release.
  let grab = 0
  let samples: { t: number; y: number }[] = []
  function down(e: PointerEvent) {
    if (e.button !== 0 || (e.target as HTMLElement).closest('a, button, video, audio')) return
    ;(e.currentTarget as HTMLElement).setPointerCapture(e.pointerId)
    if (closing) (closing = false), grow.to(1000, { dampingRatio: 0.94, response: 0.32 }) // caught on its way out
    grab = e.clientY - drag.value
    samples = [{ t: e.timeStamp, y: drag.value }]
    drag.set(drag.value) // a grab mid-flight stops the motion where it is
  }
  function move(e: PointerEvent) {
    if (!samples.length) return
    const raw = e.clientY - grab
    drag.set(raw < 0 ? rubberband(raw, box.h) : raw)
    samples.push({ t: e.timeStamp, y: drag.value })
    samples = samples.filter((s) => e.timeStamp - s.t < 100)
  }
  function up() {
    if (!samples.length) return
    const first = samples[0]
    const last = samples[samples.length - 1]
    const velocity = last.t > first.t ? ((last.y - first.y) / (last.t - first.t)) * 1000 : 0
    samples = []
    if (drag.value + project(velocity) > box.h * 0.3) close(velocity)
    else drag.to(0, { dampingRatio: 0.8, response: 0.3, velocity }) // a flick earns a little bounce
  }

  const rest = $derived(1 - p)
  const lift = $derived(Math.max(0, y) / box.h)
</script>

<svelte:window onkeydown={keydown} />

<!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_static_element_interactions (Escape closes) -->
<div class="scrim" style:opacity={p * (1 - Math.min(1, lift))} onclick={scrimClick}></div>
<div class="ql card {k}" class:bare bind:this={card} role="dialog" aria-modal="true" aria-label={name} tabindex="-1"
  style:opacity={Math.min(1, p * 1.4)}
  style:transform="translate3d({box.dx * rest}px, {box.dy * rest + y}px, 0) scale({box.s + (1 - box.s) * p - lift * 0.08})">
  <!-- dragging is a pointer enhancement; keyboard users close with Escape or the button -->
  {#key entry.path}
    <div class="stage" role="presentation" onpointerdown={k === 'image' || k === 'audio' || bare ? down : undefined}
      onpointermove={move} onpointerup={up} onpointercancel={up}
      in:fly={{ x: 56 * dir, duration: dir && !reducedMotion() ? 240 : 0, easing: cubicOut }}>
      {#if bare}
        <span class="disc"><Visual {entry} size="large" /></span>
        <p class="callout">{k === 'pdf' ? 'This browser shows PDFs in its own viewer.' : `${kindLabel(entry.path)} · no preview for this kind`}</p>
        <div class="bare-actions">
          {#if k === 'pdf'}<a class="btn plain" href={src} target="_blank" rel="noopener">Open</a>{/if}
          <a class="btn" href={fileURL(entry.path)} download={name}><Icon name="download" size={18} />Download</a>
        </div>
      {:else if k === 'image'}
        <img {src} alt={name} draggable="false" />
      {:else if k === 'video'}
        <!-- svelte-ignore a11y_media_has_caption -->
        <video {src} controls autoplay playsinline></video>
      {:else if k === 'audio'}
        <span class="disc"><Visual {entry} size="large" /></span>
        <audio {src} controls autoplay></audio>
      {:else if k === 'pdf'}
        <iframe {src} title={name}></iframe>
      {:else if text !== null}
        <pre>{text}</pre>
      {/if}
    </div>
  {/key}
  <footer class="foot" role="presentation" onpointerdown={down} onpointermove={move} onpointerup={up} onpointercancel={up}>
    <div class="meta">
      <h2 class="title">{name}</h2>
      <span class="caption num">{size(entry.size)} · {when(entry.mtime)}{items.length > 1 && at >= 0 ? ` · ${at + 1} of ${items.length}` : ''}</span>
    </div>
    {#if onshare}
      <button class="circle-btn" aria-label="Share link" title="Share link" onclick={() => (close(), onshare(entry))}><Icon name="share" size={20} /></button>
    {/if}
    <a class="circle-btn" href={fileURL(entry.path)} download={name} aria-label="Download" title="Download"><Icon name="download" size={20} /></a>
    <button class="circle-btn" aria-label="Close" title="Close (Esc)" onclick={() => close()}><Icon name="close" size={20} /></button>
  </footer>
</div>
{#if prev}
  <button class="step prev" style:opacity={p} aria-label="Previous file" title="Previous (←)" onclick={() => show(prev!, -1)}><Icon name="chevron_left" size={30} /></button>
{/if}
{#if next}
  <button class="step next" style:opacity={p} aria-label="Next file" title="Next (→)" onclick={() => show(next!, 1)}><Icon name="chevron_right" size={30} /></button>
{/if}

<style>
  .scrim {
    position: fixed; inset: 0; z-index: 50; background: rgb(24 22 24 / .28); touch-action: manipulation; /* a double-tap is two taps, not a zoom */
    backdrop-filter: blur(14px); -webkit-backdrop-filter: blur(14px);
  }
  /* One frame for every kind, so stepping from a photo to a song doesn't resize the card under the arrows */
  .ql {
    position: fixed; z-index: 51; inset: 0; margin: auto; width: min(64rem, calc(100vw - 2rem)); height: min(88dvh, 52rem);
    display: grid; grid-template-rows: minmax(0, 1fr) auto; padding: 18px 18px 0; overflow: hidden; outline: none;
    will-change: transform, opacity;
  }
  .stage { grid-area: 1 / 1; min-height: 0; display: grid; place-items: center; overflow: auto; border-radius: 16px; }
  .audio .stage, .bare .stage { gap: 1.25rem; align-content: center; touch-action: none; cursor: grab; }
  .bare .callout { margin: 0; text-align: center; }
  .bare-actions { display: flex; gap: .6rem; }
  .disc { width: 220px; height: 170px; }
  .foot { display: flex; align-items: center; gap: 10px; padding: 16px 6px 18px 8px; touch-action: none; cursor: grab; user-select: none; }
  .foot:active { cursor: grabbing; }
  .meta { flex: 1; min-width: 0; display: grid; gap: 3px; }
  .meta h2 { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  img, video { max-width: 100%; max-height: 100%; border-radius: 12px; box-shadow: var(--shadow-object); }
  img { touch-action: none; user-select: none; -webkit-user-drag: none; cursor: grab; }
  iframe { width: 100%; height: 100%; border: 0; border-radius: 12px; background: #fff; }
  audio { width: min(22rem, 100%); }
  pre {
    align-self: start; width: 100%; min-height: 100%; margin: 0; padding: 1.1rem 1.25rem; border-radius: 12px;
    background: var(--card); box-shadow: 0 0 0 1px var(--separator); color: var(--text);
    white-space: pre-wrap; word-break: break-word; font: .84rem/1.6 ui-monospace, 'Cascadia Code', Consolas, monospace; /* file content, not chrome */
  }
  /* Previous / next: just outside the card where there's room, over its edges where there isn't */
  .step {
    position: fixed; z-index: 52; top: 50%; width: 3rem; height: 3rem; margin-top: -1.5rem; padding: 0; display: grid; place-items: center;
    border: 0; border-radius: 50%; background: var(--surface); color: var(--text); box-shadow: var(--shadow-2);
    transition: transform var(--press-duration) ease-out, background-color 150ms ease-out;
  }
  .step:hover { background: var(--surface-2); }
  .step:active { transform: scale(.92); }
  .prev { left: max(.75rem, calc(50vw - 36.5rem)); }
  .next { right: max(.75rem, calc(50vw - 36.5rem)); }
  @media (max-width: 760px) {
    .ql { width: calc(100vw - 1rem); height: calc(100dvh - 1rem); padding: 10px 10px 0; border-radius: 22px; }
    .step { width: 2.5rem; height: 2.5rem; margin-top: -1.25rem; }
  }
</style>
