<script lang="ts">
  import { onMount } from 'svelte'
  import { api, versionURL, baseName, type Entry, type Version } from './lib/api'
  import { size, when } from './lib/format'
  import Icon from './lib/Icon.svelte'

  let { entry, onclose, onrestored }: { entry: Entry; onclose: () => void; onrestored: () => void } = $props()
  let versions = $state<Version[] | null>(null)
  let error = $state('')
  let busy = $state(false)
  let dialog: HTMLDialogElement

  onMount(() => {
    dialog.showModal() // native modal: focus trap, Escape, and the dimmed backdrop for free
    api.versions(entry.path).then((v) => (versions = v)).catch((e) => (error = e.message))
  })

  async function restore(v: Version) {
    busy = true
    try {
      await api.restoreVersion(entry.path, v.id)
      onrestored()
      dialog.close()
    } catch (e: any) {
      error = e.message
      busy = false
    }
  }
</script>

<dialog bind:this={dialog} class="card" aria-labelledby="vh-title" onclose={onclose}>
  <header>
    <div class="meta">
      <h2 id="vh-title" class="title">Versions</h2>
      <span class="caption">{baseName(entry.path)}</span>
    </div>
    <button class="circle-btn" aria-label="Close" onclick={() => dialog.close()}><Icon name="close" size={17} /></button>
  </header>
  <ul>
    <li class="current">
      <span class="dot" aria-hidden="true"></span>
      <span class="what"><span class="nm">Current</span><span class="caption num">{when(entry.mtime)} · {size(entry.size)}</span></span>
    </li>
    {#each versions ?? [] as v (v.id)}
      <li>
        <span class="dot" aria-hidden="true"></span>
        <span class="what"><span class="nm">Replaced {when(v.created)}</span><span class="caption num">{size(v.size)}</span></span>
        <a class="icon-btn" href={versionURL(entry.path, v.id)} download={baseName(entry.path)} aria-label="Download this version" title="Download"><Icon name="download" size={17} /></a>
        <button class="btn plain" disabled={busy} onclick={() => restore(v)}>Restore</button>
      </li>
    {:else}
      {#if versions}
        <li class="callout">No earlier versions yet. Whenever this file is replaced, the old content stays here for at least 30 days.</li>
      {/if}
    {/each}
  </ul>
  {#if error}<p class="error" role="alert"><Icon name="alert" size={16} />{error}</p>{/if}
</dialog>

<style>
  dialog {
    width: min(30rem, calc(100vw - 2rem)); max-height: min(36rem, 85dvh); padding: 0; color: var(--text);
    animation: appear 260ms var(--ease-out);
  }
  dialog::backdrop { background: rgb(24 22 24 / .28); backdrop-filter: blur(14px); -webkit-backdrop-filter: blur(14px); }
  header { display: flex; align-items: center; gap: .75rem; padding: 1.5rem 1.25rem .75rem 1.6rem; }
  .meta { flex: 1; min-width: 0; display: grid; gap: .2rem; }
  .meta .caption { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  ul { list-style: none; margin: 0; padding: .25rem 1.25rem 1.25rem; overflow: auto; }
  li { display: flex; align-items: center; gap: .85rem; min-height: 3.25rem; padding: .4rem .35rem; border-top: 1px solid var(--separator); }
  li:first-child { border-top: 0; }
  .dot { width: 8px; height: 8px; border-radius: 50%; flex: none; background: var(--separator-strong); }
  .current .dot { background: var(--coral); box-shadow: 0 0 8px rgb(229 75 75 / .5); }
  .what { flex: 1; min-width: 0; display: grid; gap: .1rem; }
  .nm { font: var(--type-name); }
  .btn { min-height: 2.1rem; padding: 0 .9rem; font-size: .82rem; }
  .error { padding: 0 1.6rem 1.25rem; margin: 0; }
  @keyframes appear { from { opacity: 0; transform: scale(.96); } }
</style>
