<script lang="ts">
  import Logo from './lib/Logo.svelte'
  import { api, onSignedOut } from './lib/api'
  import Auth from './Auth.svelte'
  import Shell from './Shell.svelte'
  import Shared from './Shared.svelte'

  let view = $state<'loading' | 'auth' | 'app' | 'offline'>('loading')
  let hasPasskeys = $state(true)
  // The setup link is /#setup=<token>; it never reaches the server (fragment), only this page.
  let setupToken = $state(new URLSearchParams(location.hash.slice(1)).get('setup') ?? '')

  async function check() {
    view = 'loading'
    try {
      const s = await api.status()
      hasPasskeys = s.hasPasskeys
      view = s.signedIn && !setupToken ? 'app' : 'auth'
    } catch {
      view = 'offline'
    }
  }
  // A share link (/#/s/<id>.<key>) is a page of its own: no sign-in, no app.
  const shared = location.hash.match(/^#\/s\/([0-9a-f]{32})\.([\w-]{43})$/)
  onSignedOut(() => (view = 'auth'))
  if (!shared) check()

  function signedIn() {
    setupToken = ''
    history.replaceState(null, '', '/#/files/')
    view = 'app'
  }
</script>

{#if shared}
  <Shared id={shared[1]} secret={shared[2]} />
{:else if view === 'app'}
  <Shell />
{:else if view === 'auth'}
  <Auth {hasPasskeys} {setupToken} onsignedin={signedIn} />
{:else if view === 'offline'}
  <main class="center">
    <div class="card panel">
      <span class="mark"><Logo variant="mark" size={72} /></span>
      <h1 class="title">Your NAS isn't reachable</h1>
      <p class="callout">Aetheris couldn't reach it through the gateway. It may be restarting or offline.</p>
      <button class="btn plain" onclick={check}>Try again</button>
    </div>
  </main>
{/if}

<style>
  .center { min-height: 100dvh; display: grid; place-items: center; padding: 1.5rem; background: var(--ambient); }
  .panel { width: min(25rem, 100%); padding: 2rem; display: grid; justify-items: center; gap: .8rem; text-align: center; }
  .mark { opacity: .6; margin-bottom: .25rem; }
  .panel p { margin: 0 0 .4rem; }
</style>
