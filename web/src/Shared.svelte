<script lang="ts">
  import { onMount, onDestroy } from 'svelte'
  import Logo from './lib/Logo.svelte'
  import Icon from './lib/Icon.svelte'
  import Visual from './lib/Visual.svelte'
  import { importKey, openMeta, openStream, type Meta } from './lib/sealed'
  import { kind, kindLabel, mimeOf, size, until, allowance } from './lib/format'

  // What someone sees when they open a share link: no account, no app chrome. Everything the NAS sends
  // is decrypted here with the key from the link's #fragment, which never leaves this browser.
  let { id, secret }: { id: string; secret: string } = $props()
  let key: CryptoKey
  let meta = $state<Meta | null>(null)
  let left = $state({ views: 0, downloads: 0, expires: 0 })
  let phase = $state<'loading' | 'ready' | 'gone' | 'broken'>('loading')
  let error = $state('')
  let progress = $state<number | null>(null)
  let shown = $state<{ url: string; text?: string } | null>(null)
  const k = $derived(meta ? kind(meta.name) : 'file')
  const entry = $derived({ path: meta?.name ?? '', size: meta?.size ?? 0, mtime: meta?.mtime ?? 0, rev: 0 })

  onMount(async () => {
    try {
      key = await importKey(secret)
      const r = await fetch(`/api/v1/shared/${id}`)
      if (r.status === 404) return void (phase = 'gone')
      if (!r.ok) throw new Error((await r.json().catch(() => null))?.error ?? 'The drive is not reachable right now.')
      const info = await r.json()
      meta = await openMeta(key, info.meta) // a damaged key fails here, before anything is spent
      left = info
      phase = 'ready'
    } catch (e: any) {
      phase = 'broken'
      error = e?.name === 'OperationError' || e?.name === 'DataError' ? 'This link is incomplete. Copy the whole link and try again.' : e.message
    }
  })
  onDestroy(() => shown && URL.revokeObjectURL(shown.url))

  // shortcut: the whole file is decrypted into memory before it shows or saves. Fine to a few hundred MB;
  // multi-GB shares would need a Service Worker that decrypts ranges as they stream.
  async function get(mode: 'view' | 'download') {
    progress = 0
    error = ''
    try {
      const r = await fetch(`/api/v1/shared/${id}/content?mode=${mode}`)
      if (r.status === 404) return void (phase = 'gone')
      if (!r.ok || !r.body) throw new Error('The drive is not reachable right now. Try again in a moment.')
      const parts = await openStream(key, r.body, (done, total) => (progress = total ? done / total : 1))
      const blob = new Blob(parts as BlobPart[], { type: mode === 'view' ? mimeOf(meta!.name) : 'application/octet-stream' })
      if (mode === 'view') {
        if (left.views > 0) left.views--
        shown = { url: URL.createObjectURL(blob), text: k === 'text' ? await blob.text() : undefined }
      } else {
        if (left.downloads > 0) left.downloads--
        const a = document.createElement('a')
        a.href = URL.createObjectURL(blob)
        a.download = meta!.name
        a.click()
        setTimeout(() => URL.revokeObjectURL(a.href), 60_000)
      }
    } catch (e: any) {
      error = e.message
    }
    progress = null
  }
</script>

<main class="page">
  <div class="card sheet" class:wide={shown}>
    <span class="brand"><Logo variant="mark" size={34} label="Aetheris" /></span>
    {#if phase === 'loading'}
      <p class="callout">Opening…</p>
    {:else if phase === 'gone'}
      <span class="object dim"><Logo variant="mark" size={72} label="" /></span>
      <h1 class="title">This link is no longer available</h1>
      <p class="callout">It may have expired, been used up, or been removed by the person who shared it.</p>
    {:else if phase === 'broken'}
      <h1 class="title">This link can't be opened</h1>
      <p class="error" role="alert"><Icon name="alert" size={16} />{error}</p>
    {:else if meta}
      {#if shown}
        <div class="view {k}">
          {#if k === 'image'}
            <img src={shown.url} alt={meta.name} />
          {:else if k === 'video'}
            <!-- svelte-ignore a11y_media_has_caption -->
            <video src={shown.url} controls autoplay playsinline></video>
          {:else if k === 'audio'}
            <span class="object"><Visual {entry} size="large" thumb={false} /></span>
            <audio src={shown.url} controls autoplay></audio>
          {:else if k === 'pdf' && navigator.pdfViewerEnabled === false}
            <a class="btn plain" href={shown.url} target="_blank" rel="noopener">Open the PDF</a>
          {:else if k === 'pdf'}
            <iframe src={shown.url} title={meta.name}></iframe>
          {:else}
            <pre>{shown.text}</pre>
          {/if}
        </div>
      {:else}
        <span class="object"><Visual {entry} size="large" thumb={false} /></span>
      {/if}
      <h1 class="title name">{meta.name}</h1>
      <p class="caption num">{kindLabel(meta.name)} · {size(meta.size)}</p>
      <p class="caption num">{left.views || left.downloads ? `${allowance(left.views, left.downloads)} · expires ${until(left.expires)}` : 'This was the last use of this link.'}</p>

      <div class="actions">
        {#if !shown && left.views !== 0}
          <button class="btn" disabled={progress !== null} onclick={() => get('view')}><Icon name="eye" size={16} />View</button>
        {/if}
        {#if left.downloads !== 0}
          <button class={left.views !== 0 && !shown ? 'btn plain' : 'btn'} disabled={progress !== null} onclick={() => get('download')}>
            <Icon name="download" size={16} />Download
          </button>
        {/if}
      </div>
      {#if progress !== null}
        <div class="bar" role="progressbar" aria-label="Receiving" aria-valuenow={Math.round(progress * 100)}><span style:transform="scaleX({progress})"></span></div>
      {/if}
      {#if error}<p class="error" role="alert"><Icon name="alert" size={16} />{error}</p>{/if}
    {/if}
    <p class="note"><Icon name="lock" size={14} />Encrypted with a key that exists only in this link</p>
  </div>
</main>

<style>
  .page { min-height: 100dvh; display: grid; place-items: center; padding: 1.5rem; background: var(--ambient); }
  .sheet {
    width: min(27rem, 100%); padding: 2rem 2rem 1.4rem; display: grid; justify-items: center; gap: .55rem; text-align: center;
    transition: width 340ms var(--ease-out);
  }
  .sheet.wide { width: min(64rem, 100%); }
  .brand { opacity: .8; margin-bottom: .75rem; }
  .object { width: 200px; height: 150px; margin-bottom: .75rem; }
  .dim { opacity: .35; display: grid; place-items: center; }
  .title, .callout, .caption, .error { margin: 0; }
  .name { overflow-wrap: anywhere; }
  .actions { display: flex; gap: .6rem; margin-top: 1rem; }
  .bar { width: min(16rem, 100%); height: 3px; border-radius: 2px; background: var(--separator); overflow: hidden; margin-top: .4rem; }
  .bar span { display: block; height: 100%; background: var(--accent); transform-origin: left; transition: transform 160ms linear; }
  .error { justify-content: center; }
  .note { display: flex; align-items: center; gap: .4rem; margin: 1.4rem 0 0; font: var(--type-caption); color: var(--text-3); }

  .view { width: 100%; display: grid; place-items: center; gap: 1rem; margin-bottom: .75rem; }
  .view img, .view video { max-width: 100%; max-height: 70dvh; border-radius: 12px; box-shadow: var(--shadow-object); }
  .view iframe { width: 100%; height: 72dvh; border: 0; border-radius: 12px; background: #fff; }
  .view audio { width: min(22rem, 100%); }
  .view pre {
    width: 100%; max-height: 70dvh; overflow: auto; margin: 0; padding: 1.1rem 1.25rem; border-radius: 12px; text-align: left;
    background: var(--card); box-shadow: 0 0 0 1px var(--separator); color: var(--text);
    white-space: pre-wrap; word-break: break-word; font: .84rem/1.6 ui-monospace, 'Cascadia Code', Consolas, monospace; /* file content, not chrome */
  }
  @media (max-width: 760px) { .sheet { padding: 1.6rem 1.1rem 1.2rem; } }
  @media (prefers-reduced-motion: reduce) { .sheet, .bar span { transition: none; } }
</style>
