<script lang="ts">
  import { api, baseName, parentOf, type Trashed } from './lib/api'
  import { size, when } from './lib/format'
  import Icon from './lib/Icon.svelte'
  import Visual from './lib/Visual.svelte'

  let items = $state<Trashed[] | null>(null)
  let error = $state('')
  const load = () => api.trash().then((t) => (items = t)).catch((e) => (error = e.message))
  load()

  async function restore(t: Trashed) {
    try {
      await api.restore(t.id)
      await load()
    } catch (e: any) {
      error = e.status === 409 ? `Something named “${baseName(t.path)}” is already there, or its folder is gone.` : e.message
    }
  }
</script>

<header class="page-head">
  <h1 class="large-title">Trash</h1>
</header>
<p class="note callout"><Icon name="info" size={18} />Items in the trash are deleted forever after 30 days.</p>
{#if error}<p class="error" role="alert"><Icon name="alert" size={18} />{error}</p>{/if}
{#if items?.length}
  <div role="table" aria-label="Trash">
    <div class="thead" role="row">
      <span class="c-name" role="columnheader">Name</span><span class="c-where" role="columnheader">Original location</span>
      <span class="c-date" role="columnheader">Date trashed</span><span class="c-size" role="columnheader">File size</span><span class="c-restore"></span>
    </div>
    {#each items as t (t.id)}
      <div class="trow" role="row">
        <span class="c-name" role="cell"><span class="ico"><Visual entry={{ path: t.path, dir: t.dir, size: t.size, mtime: t.deleted, rev: 0 }} size="row" thumb={false} /></span><span class="nm">{baseName(t.path)}</span></span>
        <span class="c-where" role="cell">{parentOf(t.path) ? baseName(parentOf(t.path)) : 'My files'}</span>
        <span class="c-date num" role="cell">{when(t.deleted)}</span>
        <span class="c-size num" role="cell">{t.dir ? '—' : size(t.size)}</span>
        <span class="c-restore" role="cell"><button class="btn quiet" onclick={() => restore(t)}><Icon name="restore" size={20} />Restore</button></span>
      </div>
    {/each}
  </div>
{:else if items}
  <p class="empty callout">The trash is empty.</p>
{/if}

<style>
  .note { display: flex; align-items: center; gap: .6rem; margin: 0 0 .75rem; padding: .75rem 1rem; border-radius: .75rem; background: var(--surface-2); color: var(--text-2); }
  .c-restore { width: 7.5rem; flex: none; display: flex; justify-content: flex-end; }
  .btn { min-height: 2.25rem; padding: 0 .85rem 0 .6rem; color: var(--accent-text); }
  .empty { padding: 2rem 0; }
  .error { margin: 0 0 1rem; }
</style>
