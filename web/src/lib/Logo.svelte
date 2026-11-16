<script lang="ts">
  // The logo is the artwork itself (assets/brand/logo-stacked.svg), inlined rather than an <img> so its
  // wordmark takes the text colour and stays legible in dark mode. variant="mark" crops to the symbol
  // tightly (the drawing spans x 180–618, y 139–466, plus its strokes) for small places, without the wordmark.
  import artwork from '../../../assets/brand/logo-stacked.svg?raw'

  let { variant = 'full', size = 96, label = 'Aetheris' }: { variant?: 'full' | 'mark'; size?: number; label?: string } = $props()

  const markup = $derived(
    artwork.replace(/viewBox="[^"]*"/, `viewBox="${variant === 'mark' ? '155 57 490 490' : '0 0 800 720'}" aria-hidden="true"`),
  )
</script>

<!-- label="" when a visible name sits next to it: then screen readers skip the picture -->
<span class="logo" class:mark={variant === 'mark'} style:width="{size}px" role={label ? 'img' : undefined}
  aria-label={label || undefined} aria-hidden={label ? undefined : 'true'}>{@html markup}</span>

<style>
  .logo { display: inline-block; flex: none; line-height: 0; aspect-ratio: 800 / 720; }
  .logo.mark { aspect-ratio: 1; }
  .logo :global(svg) { display: block; width: 100%; height: 100%; }
  .logo :global(.brand-wordmark), .logo :global(.brand-wordmark text) { fill: var(--text); }
  .logo.mark :global(.brand-wordmark) { display: none; }
</style>
