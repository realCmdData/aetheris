<script lang="ts">
  import { api, baseName, parentOf, type Entry } from './lib/api'
  import { size, when } from './lib/format'
  import Icon from './lib/Icon.svelte'
  import Visual from './lib/Visual.svelte'
  import Preview from './Preview.svelte'

  let items = $state<Entry[] | null>(null)
  let error = $state('')
  let previewing = $state<Entry | null>(null) // every file opens in Quick Look, as in My files
  api.recent().then((r) => (items = r)).catch((e) => (error = e.message))

  const folder = (p: string) => '#/files/' + p.split('/').map(encodeURIComponent).join('/')
</script>

<header class="page-head"><h1 class="large-title">Recent</h1></header>
{#if error}<p class="error" role="alert"><Icon name="alert" size={18} />{error}</p>{/if}
{#if items?.length}
  <div role="table" aria-label="Recent files">
    <div class="thead" role="row">
      <span class="c-name" role="columnheader">Name</span><span class="c-where" role="columnheader">Location</span>
      <span class="c-date" role="columnheader">Modified</span><span class="c-size" role="columnheader">File size</span>
    </div>
    {#each items as e (e.path)}
      <div class="trow" role="row" data-path={e.path}>
        <span class="c-name" role="cell"><span class="ico"><Visual entry={e} size="row" /></span><button class="nm" onclick={() => (previewing = e)}>{baseName(e.path)}</button></span>
        <a class="c-where" role="cell" href={folder(parentOf(e.path))}><Icon name="folder" fill size={18} />{parentOf(e.path) ? baseName(parentOf(e.path)) : 'My files'}</a>
        <span class="c-date num" role="cell">{when(e.mtime)}</span>
        <span class="c-size num" role="cell">{size(e.size)}</span>
      </div>
    {/each}
  </div>
{:else if items}
  <p class="empty callout">Nothing here yet. Files you upload or change show up here.</p>
{/if}

{#if previewing}<Preview entry={previewing} items={items ?? []} onclose={() => (previewing = null)} />{/if}

<style>
  .empty { padding: 2rem 0; }
  .error { margin: 0 0 1rem; }
</style>
