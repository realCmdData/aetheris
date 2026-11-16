<script lang="ts">
  import { fade, scale } from 'svelte/transition'
  import { listen, send, type State } from './bridge'
  import Pair from './Pair.svelte'
  import Folders from './Folders.svelte'
  import Home from './Home.svelte'

  let app = $state<State | null>(null)
  let pairError = $state<string | null>(null)
  let pairing = $state(false)
  let freed = $state<number | null>(null)
  let setup = $state(false) // just paired: the setup goes on to the folders step
  let answer = $state<{ error: string | null } | null>(null) // the app's last word on adding or removing a folder

  listen((m) => {
    if (m.type === 'state') app = m
    else if (m.type === 'paired') {
      pairing = false
      pairError = m.error
      if (!m.error) setup = true
    } else if (m.type === 'freed') freed = m.bytes
    else if (m.type === 'folder') answer = { error: m.error }
  })
  send({ type: 'ready' })

  function pair(server: string, home: string, code: string) {
    pairing = true
    pairError = null
    send({ type: 'pair', server, home, code })
  }

  // First-time setup: connect this PC, then choose its folders. After that, Home.
  const screen = $derived(!app?.paired ? 'pair' : setup ? 'folders' : 'home')
</script>

{#if app}
  {#key screen}
    <!-- The new screen materializes (fade with a slight scale) rather than sliding in from nowhere. -->
    <div class="screen" in:scale={{ start: 0.985, duration: 260, opacity: 0 }} out:fade={{ duration: 120 }}>
      {#if screen === 'home'}
        <Home {app} {freed} onfreed={() => (freed = null)} folderError={answer?.error ?? null} />
      {:else if screen === 'folders'}
        <Folders {app} {answer} ondone={() => ((setup = false), (answer = null))} />
      {:else}
        <Pair web={app.web} server={app.host} home={app.home} error={pairError} busy={pairing} onpair={pair} />
      {/if}
    </div>
  {/key}
{/if}

<style>
  .screen { min-height: 100vh; }
</style>
