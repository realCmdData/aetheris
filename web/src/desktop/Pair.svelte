<script lang="ts">
  import Icon from '../lib/Icon.svelte'
  import Logo from '../lib/Logo.svelte'

  // The setup's first step. The web app's Settings → Pair a device shows all of it: the server, the code, and the
  // server's address at home (only needed on a PC without Tailscale).
  let { web, server: known, home: knownHome, error, busy, onpair }: {
    web: string | null; server: string | null; home: string | null; error: string | null; busy: boolean
    onpair: (server: string, home: string, code: string) => void
  } = $props()

  let server = $state(known ?? '')
  let home = $state(knownHome ?? '')
  let showHome = $state(!!knownHome)
  let code = $state('')
  let input: HTMLInputElement
  const ready = $derived(server.trim().length > 3 && code.replace('-', '').length === 8)

  // Codes look like ABCD-EFGH: capitals and digits, the dash put in for you.
  function format(e: Event) {
    const raw = (e.target as HTMLInputElement).value.toUpperCase().replace(/[^A-Z0-9]/g, '').slice(0, 8)
    code = raw.length > 4 ? `${raw.slice(0, 4)}-${raw.slice(4)}` : raw
  }

  function submit(e: SubmitEvent) {
    e.preventDefault()
    if (ready && !busy) onpair(server.trim(), showHome ? home.trim() : '', code)
  }

  $effect(() => {
    if (error) input?.select() // retype at once
  })
</script>

<main>
  <section class="plinth card">
    <Logo size={132} />
    <h1 class="title">Connect this PC</h1>
    <p class="callout">
      In the Aetheris web app{#if web}{' '}at <a href={web} target="_blank" rel="noreferrer">{new URL(web).host}</a>{/if}, open Settings and choose
      <strong>Pair a device</strong>. Type what it shows.
    </p>

    <form onsubmit={submit}>
      <label class="field-label" for="server">Server</label>
      <input id="server" class="text" bind:value={server} placeholder="aetheris.tail1234.ts.net" autocomplete="off" spellcheck="false" disabled={busy} />

      {#if showHome}
        <label class="field-label" for="home">Address at home <span class="caption">for this PC without Tailscale</span></label>
        <input id="home" class="text num" bind:value={home} placeholder="192.168.1.20" autocomplete="off" spellcheck="false" disabled={busy} />
      {:else}
        <button type="button" class="more" onclick={() => (showHome = true)}>Not using Tailscale on this PC?</button>
      {/if}

      <label class="field-label" for="code">Code</label>
      <input id="code" bind:this={input} class="code num" value={code} oninput={format} placeholder="ABCD-EFGH"
        autocomplete="off" autocapitalize="characters" spellcheck="false" maxlength="9" disabled={busy}
        aria-invalid={error ? 'true' : undefined} aria-describedby={error ? 'pair-error' : undefined} />
      {#if error}
        <p class="error" id="pair-error" role="alert"><Icon name="alert" size={16} />{error}</p>
      {/if}
      <button class="btn wide" type="submit" disabled={!ready || busy}>{busy ? 'Connecting…' : 'Connect'}</button>
    </form>
  </section>

  <p class="caption">Aetheris connects to your server at home or over Tailscale, never through the internet.</p>
</main>

<style>
  main { min-height: 100vh; display: flex; flex-direction: column; align-items: center; justify-content: center; gap: 1.1rem; padding: 1.5rem 1.25rem; text-align: center; }
  .plinth { width: 100%; max-width: 23rem; display: flex; flex-direction: column; align-items: center; gap: .85rem; padding: 1.75rem 1.75rem 1.6rem; border-radius: 26px; }
  .callout { margin: 0; max-width: 19rem; }
  .callout a { color: var(--accent-text); text-decoration: none; }
  .callout a:hover { text-decoration: underline; }
  .callout strong { font-weight: 400; color: var(--text); }
  form { width: 100%; display: flex; flex-direction: column; gap: .45rem; margin-top: .3rem; text-align: left; }
  .field-label { font: 400 .78rem/1.2 var(--font); color: var(--text-3); margin: .45rem .2rem 0; display: flex; gap: .4rem; align-items: baseline; }
  .text, .code {
    width: 100%; border-radius: var(--radius-m); border: 1px solid var(--separator-strong); background: var(--card); color: var(--text);
    transition: border-color 120ms ease-out, box-shadow 120ms ease-out;
  }
  .text { height: 2.65rem; padding: 0 .85rem; font: 400 .9rem/1 var(--font); }
  .code { height: 3.4rem; text-align: center; font: 400 1.5rem/1 var(--font); letter-spacing: .14em; text-indent: .14em; /* centre despite the tracking */ }
  .text::placeholder, .code::placeholder { color: var(--text-3); opacity: .55; }
  .text:focus, .code:focus { outline: none; border-color: var(--accent); box-shadow: 0 0 0 3px var(--selection); }
  .code[aria-invalid='true'] { border-color: var(--error); }
  .more { align-self: flex-start; border: 0; background: none; padding: .2rem; margin-top: .1rem; color: var(--accent-text); font: 400 .8rem/1.2 var(--font); }
  .more:hover { text-decoration: underline; }
  .error { margin: .2rem 0 0; text-align: left; }
  .wide { width: 100%; min-height: 2.75rem; margin-top: .6rem; }
  .caption { margin: 0; max-width: 20rem; }
</style>
