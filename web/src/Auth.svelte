<script lang="ts">
  import Logo from './lib/Logo.svelte'
  import { registerPasskey, signIn } from './lib/api'
  import Icon from './lib/Icon.svelte'

  let { hasPasskeys, setupToken, onsignedin }: { hasPasskeys: boolean; setupToken: string; onsignedin: () => void } = $props()
  let busy = $state(false)
  let error = $state('')
  const setup = $derived(setupToken !== '')

  async function go() {
    busy = true
    error = ''
    try {
      if (setup) await registerPasskey(setupToken)
      else await signIn()
      onsignedin()
    } catch (e: any) {
      // The browser reports a cancelled or timed-out prompt as NotAllowedError: not worth alarming anyone.
      error = e?.name === 'NotAllowedError' ? '' : (e?.message ?? 'Something went wrong')
    } finally {
      busy = false
    }
  }
</script>

<main>
  <div class="card panel">
    <div class="brand"><Logo size={190} /></div>
    {#if setup}
      <h1 class="title">Create your passkey</h1>
      <p class="callout">Your device will ask for your fingerprint, face or PIN. No password is ever stored.</p>
    {:else if !hasPasskeys}
      <h1 class="title">Not set up yet</h1>
      <p class="callout">Run <code>aetheris setup</code> on your NAS and open the link it prints.</p>
    {:else}
      <h1 class="title">Welcome back</h1>
      <p class="callout">Sign in with the passkey on this device or your phone.</p>
    {/if}
    {#if setup || hasPasskeys}
      <button class="btn wide" onclick={go} disabled={busy}>
        <Icon name="key" size={18} />{busy ? 'Waiting for your device…' : setup ? 'Create passkey' : 'Sign in with passkey'}
      </button>
    {/if}
    {#if error}<p class="error" role="alert"><Icon name="alert" size={16} />{error}</p>{/if}
  </div>
  <p class="caption">{location.host}</p>
</main>

<style>
  /* the same framed porcelain: a seashell surround with one sheet resting in it */
  main { min-height: 100dvh; display: grid; place-content: center; justify-items: center; gap: 1.1rem; padding: 1.5rem; background: var(--ambient); }
  .panel { width: min(25rem, 100%); padding: 2rem 2rem 2.1rem; display: grid; gap: .8rem; text-align: center; justify-items: center; }
  .brand { display: grid; justify-items: center; margin-bottom: .25rem; }
  p { margin: 0; max-width: 20rem; }
  .wide { width: 100%; margin-top: .6rem; }
  .error { justify-content: center; }
  code { font-size: .85em; padding: .1em .35em; border-radius: var(--radius-s); background: var(--hover); }
</style>
