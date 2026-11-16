<script lang="ts">
  import { untrack } from 'svelte'
  import Icon from '../lib/Icon.svelte'
  import { send, type State } from './bridge'

  // The setup's second step, right after pairing: which of this PC's folders sync to the NAS (Computers › this PC).
  let { app, answer, ondone }: { app: State; answer: { error: string | null } | null; ondone: () => void } = $props()

  // One list that doesn't reshuffle while you choose: the usual folders, then any you pick.
  const first = untrack(() => [...app.suggested, ...app.folders.map(({ name, path }) => ({ name, path }))])
  const rows = $derived([...first, ...app.folders.filter((f) => !first.some((r) => r.path === f.path))])
  const synced = (path: string) => app.folders.some((f) => f.path === path)

  // Switches move on the press; the app's answer settles them (or puts one back, with the reason below).
  let pending = $state<Record<string, boolean>>({})
  const on = (path: string) => pending[path] ?? synced(path)
  $effect(() => {
    const failed = !!answer?.error
    app.folders
    for (const path of Object.keys(untrack(() => pending)))
      if (failed || synced(path) === pending[path]) delete pending[path]
  })

  function toggle(path: string) {
    const next = !on(path)
    pending[path] = next
    send(next ? { type: 'addFolder', path } : { type: 'removeFolder', path })
  }

  const count = $derived(app.folders.length)
</script>

<main>
  <header>
    <span class="emblem" aria-hidden="true"><Icon name="computer" size={34} /></span>
    <h1 class="title">Sync folders from this PC</h1>
    <p class="callout">They stay where they are, and your NAS keeps them up to date both ways. On the web they’re under <strong>Computers › {app.computer}</strong>.</p>
  </header>

  <section class="group">
    {#each rows as r (r.path)}
      <div class="row">
        <Icon name="folder" fill size={20} />
        <span class="what"><span class="title-line">{r.name}</span><span class="caption">{r.path}</span></span>
        <button class="switch" role="switch" aria-checked={on(r.path)} aria-label="Sync {r.name}" onclick={() => toggle(r.path)}><span></span></button>
      </div>
    {/each}
    <button class="row link" onclick={() => send({ type: 'addFolder' })}><Icon name="create_new_folder" size={20} />Choose another folder…</button>
  </section>
  {#if answer?.error}<p class="error" role="alert"><Icon name="alert" size={16} />{answer.error}</p>{/if}

  <button class="btn wide" onclick={ondone}>{count ? 'Done' : 'Skip for now'}</button>
  <p class="caption foot">{count ? `${count} folder${count === 1 ? '' : 's'} syncing.` : 'Nothing chosen yet.'} You can change this any time in Aetheris.</p>
</main>

<style>
  main { max-width: 23rem; min-height: 100vh; margin: 0 auto; padding: 2.25rem 1.25rem 2rem; display: flex; flex-direction: column; gap: 1rem; justify-content: center; }
  header { display: flex; flex-direction: column; align-items: center; gap: .7rem; text-align: center; margin-bottom: .4rem; }
  .emblem { display: grid; place-items: center; width: 4.25rem; height: 4.25rem; border-radius: 22px; color: var(--accent); background: var(--card); box-shadow: var(--shadow-object); }
  .callout { margin: 0; max-width: 20rem; }
  .callout strong { font-weight: 400; color: var(--text); }
  .group { margin-top: 0; }
  .row > :global(svg:first-child) { color: var(--tone-folder, var(--text-3)); }
  .error { margin: 0 .35rem; }
  .wide { width: 100%; min-height: 2.75rem; margin-top: .4rem; }
  .foot { margin: 0; text-align: center; }
</style>
