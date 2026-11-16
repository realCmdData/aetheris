<script lang="ts" module>
  import { kind, type Kind } from './format'

  // Drive-style type icons: one filled Material Symbol and one tone per kind; a few common extensions are told apart.
  const byKind: Record<Kind, [icon: string, tone: string]> = {
    folder: ['folder', 'folder'], image: ['image', 'red'], video: ['movie', 'red'], audio: ['audio_file', 'red'],
    pdf: ['picture_as_pdf', 'red'], text: ['description', 'blue'], file: ['draft', 'gray'],
  }
  const byExt: Record<string, [string, string]> = {}
  const add = (exts: string, icon: string, tone: string) => exts.split(' ').forEach((x) => (byExt[x] = [icon, tone]))
  add('svg heic heif tif tiff raw cr2 nef dng psd', 'image', 'red')
  add('zip 7z rar tar gz tgz bz2 xz', 'folder_zip', 'gray')
  add('xlsx xls ods csv tsv numbers', 'table', 'green')
  add('docx doc odt rtf pages', 'description', 'blue')
  add('pptx ppt odp key', 'slideshow', 'amber')
  add('3mf stl obj step stp fbx glb gltf blend', 'deployed_code', 'gray')
  add('js mjs ts tsx jsx css scss html htm py go rs c h cpp hpp cs java kt swift rb php sh bash ps1 bat cmd sql json xml yaml yml toml gcode', 'code', 'blue')

  export function look(path: string, dir?: boolean) {
    if (dir && /^Computers\/[^/]+$/.test(path)) return ['computer', 'folder'] // a PC in the Computers section
    return dir ? byKind.folder : (byExt[path.slice(path.lastIndexOf('.') + 1).toLowerCase()] ?? byKind[kind(path)])
  }
</script>

<script lang="ts">
  import { thumbURL, iconURL, type Entry } from './api'
  import Icon from './Icon.svelte'

  // The parent sizes the box. Rows show the type icon (as Drive's list does); tiles and the large view show the
  // photo itself when the NAS can thumbnail it (cover in tiles, whole in the large view), else a big type icon.
  let { entry, size = 'tile', thumb = true }: { entry: Entry; size?: 'row' | 'tile' | 'large'; thumb?: boolean } = $props()
  const l = $derived(look(entry.path, entry.dir))
  let broken = $state(false) // HEIC, huge or broken images have no thumbnail: show the icon
  let ready = $state(false)
  const pic = $derived(size !== 'row' && thumb && !broken && kind(entry.path, entry.dir) === 'image')
  // A folder synced from a PC (Computers/<PC>/<folder>) wears the icon it has there, when its PC sent one.
  let plain = $state(false)
  const own = $derived(!plain && !!entry.dir && /^Computers\/[^/]+\/[^/]+$/.test(entry.path))
  const px = $derived(size === 'row' ? 24 : size === 'tile' ? 64 : 88)
</script>

<span class="visual {size} tone-{l[1]}" class:pic aria-hidden="true">
  {#if pic}
    <img src={thumbURL(entry)} alt="" loading="lazy" decoding="async" class:ready onload={() => (ready = true)} onerror={() => (broken = true)} />
  {:else if own}
    <img class="own" src={iconURL(entry.path)} alt="" style:width="{px}px" style:height="{px}px" decoding="async" class:ready onload={() => (ready = true)} onerror={() => (plain = true)} />
  {:else}
    <Icon name={l[0]} fill size={px} />
  {/if}
</span>

<style>
  .visual { display: grid; place-items: center; width: 100%; height: 100%; overflow: hidden; }
  .visual :global(svg) { display: block; flex: none; }
  .tone-red { color: var(--tone-red); }
  .tone-blue { color: var(--tone-blue); }
  .tone-green { color: var(--tone-green); }
  .tone-amber { color: var(--tone-amber); }
  .tone-folder { color: var(--tone-folder); }
  .tone-gray { color: var(--tone-gray); }
  img { width: 100%; height: 100%; display: block; object-fit: cover; opacity: 0; transition: opacity 180ms ease-out; }
  img.ready { opacity: 1; } /* fade in once decoded, instead of painting in stripes */
  .large img { object-fit: contain; }
  img.own { object-fit: contain; }
</style>
