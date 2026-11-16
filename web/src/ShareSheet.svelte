<script lang="ts">
  import { onMount } from 'svelte'
  import { api, shareURL, baseName, type Entry, type Share } from './lib/api'
  import { kind, allowance, until } from './lib/format'
  import Icon from './lib/Icon.svelte'
  import Visual from './lib/Visual.svelte'

  // A link that opens one file without an account. You choose how often it may be viewed and
  // downloaded and when it ends; then send it through the system share sheet, email, or a copy.
  let { entry, onclose }: { entry: Entry; onclose: () => void } = $props()
  const name = $derived(baseName(entry.path))
  const viewable = $derived(kind(entry.path) !== 'file') // only what the browser can show has a "view"
  // svelte-ignore state_referenced_locally (the sheet opens for one file; this is only the starting choice)
  let views = $state(viewable ? 1 : 0)
  let downloads = $state(1)
  let hours = $state(24 * 7)
  let link = $state<Share | null>(null)
  let error = $state('')
  let busy = $state(false)
  let copied = $state(false)
  let to = $state('')
  let dialog: HTMLDialogElement
  const url = $derived(link ? shareURL(link) : '')
  const canShare = typeof navigator.share === 'function'
  const emails = $derived(to.split(/[\s,;]+/).filter(Boolean))
  const emailsOK = $derived(emails.length > 0 && emails.every((a) => /^[^\s@,;?&#]+@[^\s@,;?&#]+\.[^\s@,;?&#]+$/.test(a)))

  onMount(() => dialog.showModal()) // native modal: focus trap, Escape, backdrop

  type Choice = [number, string]
  const counts: Choice[] = [[0, 'None'], [1, 'Once'], [5, '5 times'], [-1, 'No limit']]
  const expiries: Choice[] = [[1, '1 hour'], [24, '1 day'], [24 * 7, '7 days'], [24 * 30, '30 days']]

  async function create() {
    busy = true
    error = ''
    try {
      link = await api.share(entry.path, views, downloads, hours)
    } catch (e: any) {
      error = e.message
    }
    busy = false
  }

  async function copy() {
    try {
      await navigator.clipboard.writeText(url)
      copied = true
      setTimeout(() => (copied = false), 1800)
    } catch {
      error = 'Copying was blocked. Select the link and copy it yourself.'
    }
  }

  // On Android (and Windows, iOS) this is the system share sheet: Messages, Mail, WhatsApp, Nearby…
  const systemShare = () => navigator.share({ title: name, text: name, url }).catch(() => {})

  function email(ev: SubmitEvent) {
    ev.preventDefault()
    if (!emailsOK) return
    const body = `${name}\n${url}\n\n${link ? `${allowance(link.views, link.downloads)}, expires ${until(link.expires)}.` : ''}`
    location.href = `mailto:${emails.join(',')}?subject=${encodeURIComponent(name)}&body=${encodeURIComponent(body)}`
  }
</script>

{#snippet segmented(label: string, options: Choice[], value: number, set: (v: number) => void, off: (v: number) => boolean = () => false)}
  <fieldset class="row">
    <legend>{label}</legend>
    <div class="seg">
      {#each options as [v, text]}
        <label class:on={value === v} class:off={off(v)}>
          <input type="radio" name={label} checked={value === v} disabled={off(v)} onchange={() => set(v)} />{text}
        </label>
      {/each}
    </div>
  </fieldset>
{/snippet}

<dialog bind:this={dialog} class="card" aria-labelledby="share-title" onclose={onclose}>
  <header>
    <span class="mini"><Visual {entry} size="row" /></span>
    <div class="meta">
      <h2 id="share-title" class="title">Share link</h2>
      <span class="caption">{name}</span>
    </div>
    <button class="circle-btn" aria-label="Close" onclick={() => dialog.close()}><Icon name="close" size={17} /></button>
  </header>

  {#if !link}
    <div class="body">
      {#if viewable}
        {@render segmented('Views', counts, views, (v) => (views = v), (v) => v === 0 && downloads === 0)}
      {/if}
      {@render segmented('Downloads', counts, downloads, (v) => (downloads = v), (v) => v === 0 && views === 0)}
      {@render segmented('Expires after', expiries, hours, (v) => (hours = v))}
      <p class="note"><Icon name="lock" size={15} />Anyone with the link can open it, no account needed. It's encrypted with a key that exists only in the link.</p>
      <button class="btn go" disabled={busy} onclick={create}><Icon name="link" size={16} />{busy ? 'Creating…' : 'Create link'}</button>
    </div>
  {:else}
    <div class="body">
      <div class="link-row">
        <input class="field" readonly value={url} aria-label="Share link" onfocus={(e) => e.currentTarget.select()} />
        <button class="btn plain" onclick={copy}><Icon name={copied ? 'check' : 'copy'} size={16} />{copied ? 'Copied' : 'Copy'}</button>
      </div>
      <p class="caption num">{allowance(link.views, link.downloads)} · expires {until(link.expires)}</p>
      {#if canShare}
        <button class="btn go" onclick={systemShare}><Icon name="share" size={16} />Share…</button>
      {/if}
      <form class="mail" onsubmit={email}>
        <input class="field" type="text" inputmode="email" autocomplete="email" placeholder="Email to name@example.com" aria-label="Email addresses" bind:value={to} />
        <button class="btn plain" type="submit" disabled={!emailsOK}><Icon name="mail" size={16} />Email</button>
      </form>
      <p class="note"><Icon name="lock" size={15} />Your mail app sends it. Remove the link any time in Settings.</p>
    </div>
  {/if}
  {#if error}<p class="error" role="alert"><Icon name="alert" size={16} />{error}</p>{/if}
</dialog>

<style>
  dialog {
    width: min(30rem, calc(100vw - 2rem)); max-height: min(40rem, 90dvh); padding: 0; color: var(--text);
    animation: appear 260ms var(--ease-out);
  }
  dialog::backdrop { background: rgb(24 22 24 / .28); backdrop-filter: blur(14px); -webkit-backdrop-filter: blur(14px); }
  header { display: flex; align-items: center; gap: .85rem; padding: 1.4rem 1.25rem .5rem 1.4rem; }
  .mini { width: 40px; height: 40px; flex: none; }
  .meta { flex: 1; min-width: 0; display: grid; gap: .2rem; }
  .meta .caption { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .body { display: grid; gap: 1rem; padding: .75rem 1.4rem 1.5rem; }

  .row { border: 0; margin: 0; padding: 0; display: grid; gap: .45rem; min-width: 0; }
  legend { padding: 0; margin-bottom: .45rem; font: var(--type-caption); color: var(--text-3); }
  /* Segmented control: the chosen option is a raised porcelain thumb */
  .seg { display: grid; grid-auto-flow: column; grid-auto-columns: 1fr; gap: 2px; padding: 3px; border-radius: 12px; background: var(--card-subtle); border: 1px solid var(--separator); }
  .seg label {
    position: relative; display: grid; place-items: center; min-height: 2.1rem; padding: 0 .4rem; border-radius: 9px; cursor: pointer;
    font: 400 .8rem/1.1 var(--font); color: var(--text-2); text-align: center;
    transition: background-color 180ms var(--ease-out), box-shadow 180ms var(--ease-out), color 150ms, transform var(--press-duration) ease-out;
  }
  .seg label:active:not(.off) { transform: scale(.96); }
  .seg label.on { background: var(--card); color: var(--text); box-shadow: 0 1px 4px light-dark(rgb(30 30 36 / .1), rgb(0 0 0 / .35)), 0 0 0 1px var(--separator); }
  .seg label.off { opacity: .4; cursor: default; }
  .seg input { position: absolute; opacity: 0; pointer-events: none; }
  .seg label:has(input:focus-visible) { outline: 2px solid var(--accent); outline-offset: 1px; }

  .note { display: flex; gap: .55rem; align-items: flex-start; margin: 0; font: var(--type-caption); color: var(--text-3); }
  .note :global(svg) { flex: none; margin-top: 1px; }
  .go { justify-self: stretch; }
  .link-row, .mail { display: flex; gap: .5rem; }
  .link-row .field, .mail .field { flex: 1; min-width: 0; }
  .link-row .field { font: 300 .8rem/1 var(--font); color: var(--text-2); }
  .link-row .btn, .mail .btn { min-height: 2.25rem; padding: 0 .9rem; font-size: .82rem; }
  .body > .caption { margin: -.4rem 0 0; }
  .error { padding: 0 1.4rem 1.25rem; margin: 0; }
  @keyframes appear { from { opacity: 0; transform: scale(.96); } }
  @media (prefers-reduced-motion: reduce) { dialog { animation: none; } }
</style>
