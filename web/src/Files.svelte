<script lang="ts">
  import { flushSync } from 'svelte'
  import { fade, fly } from 'svelte/transition'
  import { cubicOut } from 'svelte/easing'
  import { SvelteSet } from 'svelte/reactivity'
  import Logo from './lib/Logo.svelte'
  import { api, upload, download, freeName, join, baseName, parentOf, COMPUTERS, WINDOWS_APP, type Entry } from './lib/api'
  import { size, when, kind, kindLabel, type Kind } from './lib/format'
  import { reducedMotion } from './lib/spring'
  import { clip } from './lib/clip.svelte'
  import Icon from './lib/Icon.svelte'
  import Visual from './lib/Visual.svelte'
  import Preview from './Preview.svelte'
  import Versions from './Versions.svelte'
  import ShareSheet from './ShareSheet.svelte'

  // query: search text from the top bar (#/search/…). addMenu: the "New" menu, opened by the sidebar's button.
  let { path, query = '', addMenu = $bindable(null) }: { path: string; query?: string; addMenu?: DOMRect | null } = $props()

  type Job = { name: string; size: number; sent: number; done?: boolean; error?: string }
  type Sort = 'name' | 'mtime' | 'created' | 'size' | 'type'
  let entries = $state<Entry[]>([])
  let loading = $state(true)
  let error = $state('')
  const picked = new SvelteSet<string>() // the selection, by path
  let anchor = $state('') // where Shift ranges start; also the keyboard's place
  let selecting = $state(false) // select mode: taps pick instead of open (phones, or by choice)
  let results = $state<Entry[] | null>(null)
  let renaming = $state('')
  let renameValue = $state('')
  let menu = $state<{ items: Entry[]; x: number; y: number } | null>(null)
  let chip = $state<{ of: 'type' | 'sort'; x: number; y: number } | null>(null)
  let previewing = $state<Entry | null>(null)
  let dragging = $state(false)
  let jobs = $state<Job[]>([])
  let collapsed = $state(false)
  let versioning = $state<Entry | null>(null)
  let sharing = $state<Entry | null>(null)
  let inspecting = $state(false)
  let busy = $state('') // a batch in progress, in words ("Moving 3 items")
  let dropOn = $state<string | null>(null) // the folder an internal drag is over
  let only = $state<Kind | ''>('') // the Type chip
  // Sorting, remembered per browser like the layout. mixed: folders sort in with the files (list only; the grid keeps its sections).
  let sort = $state<{ by: Sort; up: boolean; mixed?: boolean }>((() => {
    try { return JSON.parse(localStorage.getItem('aetheris.sort') ?? '') } catch { return { by: 'name', up: true } }
  })())
  $effect(() => { try { localStorage.setItem('aetheris.sort', JSON.stringify(sort)) } catch {} })
  let dragged: Entry[] = []
  let ghostCount = $state(0)
  let ghost: HTMLElement
  let picker: HTMLInputElement
  let folderPicker: HTMLInputElement
  let touch = false // the last press came from a finger: a tap opens, as in Drive's app

  // Grid or list, remembered per browser; the grid is the default. Storage can be unavailable (private mode).
  let grid = $state((() => { try { return localStorage.getItem('aetheris.view') !== 'list' } catch { return true } })())
  function setView(g: boolean) {
    grid = g
    try { localStorage.setItem('aetheris.view', g ? 'grid' : 'list') } catch {}
  }

  // Folders first, as Drive does; then the chosen column. Names compare as people read them ("2" before "10").
  const collator = new Intl.Collator(undefined, { numeric: true, sensitivity: 'base' })
  // Computers (folders synced from PCs, under Computers/<PC>/) is a section of its own: My files doesn't list it.
  const computers = $derived(path === COMPUTERS || path.startsWith(COMPUTERS + '/'))
  const shown = $derived.by(() => {
    const list = (query.trim() ? (results ?? []) : entries).filter((e) => (!only || kind(e.path, e.dir) === only) && (path || query.trim() || e.path !== COMPUTERS))
    const f = sort.up ? 1 : -1
    const ext = (e: Entry) => (e.dir ? '' : /\.([^./]+)$/.exec(e.path)?.[1].toLowerCase() ?? '')
    const by = (a: Entry, b: Entry) =>
      sort.by === 'name' ? collator.compare(baseName(a.path), baseName(b.path))
      : sort.by === 'mtime' ? a.mtime - b.mtime
      : sort.by === 'created' ? (a.created ?? a.mtime) - (b.created ?? b.mtime)
      : sort.by === 'type' ? collator.compare(ext(a), ext(b))
      : a.size - b.size
    const apart = !sort.mixed || grid
    return [...list].sort((a, b) => (apart ? Number(!!b.dir) - Number(!!a.dir) : 0) || f * by(a, b) || collator.compare(a.path, b.path))
  })
  const folders = $derived(shown.filter((e) => e.dir))
  const files = $derived(shown.filter((e) => !e.dir))
  // Dates and sizes start newest/largest, names and types A to Z.
  const sortBy = (by: Sort) => (sort = { ...sort, by, up: sort.by === by ? !sort.up : by === 'name' || by === 'type' })
  const sortNames: Record<Sort, string> = { name: 'Name', mtime: 'Modified', created: 'Created', size: 'File size', type: 'Type' }
  // The list's date column shows the date it is sorted by: created when chosen, else modified.
  const dateBy = $derived<Sort>(sort.by === 'created' ? 'created' : 'mtime')
  const types: [Kind, string, string][] = [['folder', 'Folders', 'folder'], ['image', 'Photos & images', 'image'], ['video', 'Videos', 'movie'],
    ['audio', 'Audio', 'audio_file'], ['pdf', 'PDFs', 'picture_as_pdf'], ['text', 'Text & code', 'description'], ['file', 'Other files', 'draft']]

  const crumbs = $derived(path ? path.split('/') : [])
  const ancestors = $derived(['', ...crumbs.slice(0, -1).map((_, i) => crumbs.slice(0, i + 1).join('/'))].filter((p) => !computers || p))
  const chosen = $derived(shown.filter((x) => picked.has(x.path)))
  const sel = $derived(chosen.length === 1 ? chosen[0] : null)
  const here = $derived<Entry>({ path: crumbs.at(-1) ?? 'My files', dir: true, size: 0, mtime: 0, rev: 0 })
  const href = (p: string) => '#/files/' + p.split('/').map(encodeURIComponent).join('/')
  const isMac = /Mac|iPhone|iPad/.test(navigator.platform)
  const MIME = 'application/x-aetheris' // our own drags, so they never look like file uploads
  const motion = (ms: number) => (reducedMotion() ? 0 : ms)

  async function load() {
    try {
      entries = await api.list(path)
      error = ''
    } catch (e: any) {
      if (path === COMPUTERS && e.status === 404) (entries = []), (error = '') // no PC has synced a folder yet
      else error = e.message
    }
    loading = false
  }

  $effect(() => {
    path // reload whenever the folder changes
    picked.clear()
    only = ''
    loading = true
    load()
  })

  $effect(() => {
    const t = query.trim()
    picked.clear()
    if (!t) return void (results = null)
    let stale = false
    api.search(t).then((r) => !stale && (results = r)).catch(() => !stale && (results = []))
    return () => (stale = true)
  })

  // Live updates: follow the change feed and reload when this folder is touched (PC client, SMB, another tab).
  $effect(() => {
    const ctl = new AbortController()
    ;(async () => {
      let head = await api.head().catch(() => null)
      while (head && !ctl.signal.aborted) {
        try {
          const r = await api.changes(head.epoch, head.cursor, ctl.signal)
          head.cursor = r.cursor
          if (r.changes.some((c) => [c.path, c.from ?? ''].some((p) => p && parentOf(p) === path))) load()
        } catch {
          if (ctl.signal.aborted) return
          await new Promise((r) => setTimeout(r, 5000)) // offline or index rebuilt: start again from now
          head = (await api.head().catch(() => null)) ?? head
        }
      }
    })()
    return () => ctl.abort()
  })

  // Folders open; every file opens in Quick Look, which also covers kinds the browser can't show.
  function open(e: Entry) {
    menu = null
    if (e.dir) location.hash = href(e.path)
    else previewing = e
  }
  // Quick Look stepped to another file: select it and bring it into view, so closing returns to it.
  function previewed(e: Entry) {
    pick(e.path)
    document.getElementById('row-' + e.path)?.scrollIntoView({ block: 'nearest' })
  }

  // Selection, as in Drive: click picks one, Ctrl/⌘ toggles, Shift takes the range, double-click opens.
  function pick(p: string) {
    picked.clear()
    picked.add(p)
    anchor = p
  }
  function toggle(p: string) {
    if (!picked.delete(p)) picked.add(p)
    anchor = p
  }
  function range(to: string) {
    const a = Math.max(0, shown.findIndex((x) => x.path === anchor))
    const b = shown.findIndex((x) => x.path === to)
    picked.clear()
    for (const x of shown.slice(Math.min(a, b), Math.max(a, b) + 1)) picked.add(x.path)
  }
  function press(ev: MouseEvent, e: Entry) {
    if (renaming) return
    if (ev.shiftKey) range(e.path)
    else if (selecting || ev.ctrlKey || ev.metaKey) toggle(e.path)
    else if (touch) open(e)
    else pick(e.path)
  }
  const selectAll = () => shown.forEach((x) => picked.add(x.path))
  function endSelect() {
    selecting = false
    picked.clear()
  }

  // One file downloads as itself; a folder or several items come as one zip, built on the NAS as it streams.
  async function fetchItems(items: Entry[]) {
    menu = null
    if (items.length === 1 && !items[0].dir) return download(items[0].path)
    const name = (items.length === 1 ? baseName(items[0].path) : (crumbs.at(-1) ?? 'My files')) + '.zip'
    try {
      download(name, (await api.zip(items.map((e) => e.path), name)).url)
    } catch (e: any) {
      error = e.message
    }
  }

  async function act(fn: () => Promise<unknown>) {
    try {
      await fn()
      await load()
    } catch (e: any) {
      error = e.message
    }
  }

  // Batches go one item at a time: the NAS stays responsive, and the first problem stops the rest.
  async function batch(label: string, items: Entry[], fn: (e: Entry) => Promise<unknown>) {
    menu = null
    busy = label
    error = ''
    try {
      for (const e of items) await fn(e)
    } catch (e: any) {
      error = e.message
    }
    busy = ''
    await load()
  }
  const count = (items: Entry[]) => (items.length === 1 ? `“${baseName(items[0].path)}”` : `${items.length} items`)

  /** Moves or copies e into folder dest. A name that's taken there becomes "name (1)", as with uploads. */
  function transfer(e: Entry, dest: string, move: boolean): Promise<unknown> {
    if (dest === e.path || dest.startsWith(e.path + '/')) return Promise.reject(new Error(`“${baseName(e.path)}” can't go inside itself.`))
    if (move && parentOf(e.path) === dest) return Promise.resolve() // already there
    return freeName(baseName(e.path), (name) => (move ? api.move : api.copy)(e.path, join(dest, name)))
  }

  function take(cut: boolean, items = chosen) {
    menu = null
    clip.cut = cut
    clip.items = [...items]
  }
  function paste(dest = path) {
    const { cut, items } = clip
    if (cut) clip.items = [] // a cut moves once; a copy can be pasted again
    batch(`${cut ? 'Moving' : 'Copying'} ${count(items)}`, items, (e) => transfer(e, dest, cut))
  }
  const duplicate = (items: Entry[]) => batch(`Duplicating ${count(items)}`, items, (e) => transfer(e, parentOf(e.path), false))

  function compress(items: Entry[]) {
    const name = items.length === 1 ? baseName(items[0].path) + '.zip' : 'Archive.zip' // Finder's names
    const paths = items.map((e) => e.path)
    batch(`Compressing ${count(items)}`, items.slice(0, 1), () => freeName(name, (n) => api.compress(paths, join(path, n))))
  }

  function newFolder() {
    addMenu = null
    let name = 'New folder'
    const taken = (n: string) => entries.some((e) => baseName(e.path).toLowerCase() === n.toLowerCase())
    for (let i = 2; taken(name); i++) name = `New folder ${i}`
    act(async () => startRename((await api.mkdir(join(path, name))).path))
  }

  function startRename(p: string) {
    menu = null
    renaming = p
    renameValue = baseName(p)
  }

  function commitRename() {
    const from = renaming
    const name = renameValue.trim()
    renaming = ''
    if (name && name !== baseName(from)) act(() => api.move(from, join(parentOf(from), name)))
  }

  function remove(items: Entry[]) {
    picked.clear()
    batch(`Moving ${count(items)} to Trash`, items, (e) => api.remove(e.path))
  }

  // An upload item is a file, or (no file) a folder, at a path relative to the folder it was dropped into.
  type Item = { rel: string; file?: File }

  async function uploadItems(items: Item[]) {
    const dir = path
    // Folders first, parents before children. Existing folders merge, like OneDrive.
    const dirs = new Set<string>()
    for (const it of items) {
      const parts = it.rel.split('/')
      for (let i = 1; i < parts.length + (it.file ? 0 : 1); i++) dirs.add(parts.slice(0, i).join('/'))
    }
    try {
      for (const d of [...dirs].sort((a, b) => a.split('/').length - b.split('/').length)) await api.mkdir(join(dir, d))
    } catch (e: any) {
      error = e.message
      return
    }
    const queue = items.filter((it) => it.file).map((it) => {
      jobs.push({ name: it.rel, size: it.file!.size, sent: 0 })
      return { f: it.file!, to: join(dir, parentOf(it.rel)), job: jobs[jobs.length - 1] }
    })
    const worker = async () => {
      for (let it = queue.shift(); it; it = queue.shift()) {
        const { f, job } = it
        try {
          await upload(f, it.to, (sent) => (job.sent = sent))
          job.sent = f.size
          job.done = true
        } catch (e: any) {
          job.error = e.message
        }
        if (dir === path) load()
      }
    }
    await Promise.all([worker(), worker(), worker()]) // three at a time: enough to fill the line, kind to the NAS
    load()
    setTimeout(() => (jobs = jobs.filter((j) => !j.done)), 2500)
  }

  // The upload card: bytes sent of bytes queued for the bar, one ring per file. Failures stay until closed.
  const failed = $derived(jobs.filter((j) => j.error))
  const active = $derived(jobs.filter((j) => !j.done && !j.error))
  const progress = $derived.by(() => {
    const total = jobs.reduce((n, j) => n + j.size, 0)
    return total ? jobs.reduce((n, j) => n + (j.error ? j.size : j.sent), 0) / total : 1
  })
  const plural = (n: number, one: string) => `${n} ${one}${n === 1 ? '' : 's'}`

  function pickFiles(input: HTMLInputElement) {
    addMenu = null
    input.click()
  }

  const hasFiles = (e: DragEvent) => !!e.dataTransfer?.types.includes('Files') && !e.dataTransfer.types.includes(MIME)

  // Dropped folders only come through webkitGetAsEntry, which must be read inside the drop event itself.
  function dropped(dt: DataTransfer) {
    const roots = [...dt.items].map((i) => i.webkitGetAsEntry()).filter((x): x is FileSystemEntry => !!x)
    if (!roots.some((r) => r.isDirectory)) return uploadItems([...dt.files].map((file) => ({ rel: file.name, file })))
    collect(roots).then(uploadItems).catch((e) => (error = e.message))
  }

  async function collect(entries: FileSystemEntry[], prefix = ''): Promise<Item[]> {
    const out: Item[] = []
    for (const en of entries) {
      const rel = prefix + en.name
      if (en.isFile) {
        out.push({ rel, file: await new Promise<File>((ok, no) => (en as FileSystemFileEntry).file(ok, no)) })
        continue
      }
      out.push({ rel }) // keeps empty folders too
      const reader = (en as FileSystemDirectoryEntry).createReader()
      const children: FileSystemEntry[] = []
      for (;;) { // readEntries hands out at most ~100 entries per call
        const batch = await new Promise<FileSystemEntry[]>((ok, no) => reader.readEntries(ok, no))
        if (!batch.length) break
        children.push(...batch)
      }
      out.push(...(await collect(children, rel + '/')))
    }
    return out
  }

  // Dragging objects onto a folder (or an ancestor in the path) moves them; Ctrl (⌥ on a Mac) copies.
  const copyKey = (ev: DragEvent) => (isMac ? ev.altKey : ev.ctrlKey)
  function dragStart(ev: DragEvent, e: Entry) {
    if (!picked.has(e.path)) pick(e.path)
    dragged = [...chosen]
    const dt = ev.dataTransfer!
    dt.setData(MIME, JSON.stringify(dragged.map((x) => x.path)))
    dt.effectAllowed = 'copyMove'
    if (dragged.length > 1) {
      flushSync(() => (ghostCount = dragged.length)) // the browser snapshots the ghost right after this handler
      dt.setDragImage(ghost, 34, 34)
    }
  }
  function dragOver(ev: DragEvent, dest: string) {
    const copy = copyKey(ev)
    if (!dragged.length || dragged.some((x) => dest === x.path || dest.startsWith(x.path + '/') || (!copy && parentOf(x.path) === dest))) return
    ev.preventDefault()
    ev.stopPropagation()
    ev.dataTransfer!.dropEffect = copy ? 'copy' : 'move'
    dropOn = dest
  }
  function dragLeave(ev: DragEvent, dest: string) {
    if (dropOn === dest && !(ev.currentTarget as Node).contains(ev.relatedTarget as Node)) dropOn = null
  }
  function dropInto(ev: DragEvent, dest: string) {
    if (dropOn !== dest) return
    ev.preventDefault()
    ev.stopPropagation()
    dropOn = null
    const copy = copyKey(ev)
    const items = dragged
    batch(`${copy ? 'Copying' : 'Moving'} ${count(items)}`, items, (e) => transfer(e, dest, !copy))
  }
  const dragEnd = () => ((dragged = []), (dropOn = null))

  function keys(e: KeyboardEvent) {
    if (renaming || e.target instanceof HTMLInputElement || (e.target as Element).closest?.('button:not([role=row])')) return
    const i = shown.findIndex((x) => x.path === anchor)
    const cur = shown[i]
    const step = { ArrowDown: 1, ArrowRight: 1, ArrowUp: -1, ArrowLeft: -1 }[e.key]
    if (step) {
      e.preventDefault()
      const next = shown[Math.min(Math.max(i + step, 0), shown.length - 1)]
      if (next) {
        if (e.shiftKey) (picked.add(next.path), (anchor = next.path))
        else pick(next.path)
        const el = document.getElementById('row-' + next.path)
        el?.focus({ preventScroll: true })
        el?.scrollIntoView({ block: 'nearest' })
      }
    } else if (e.key === 'Enter' && cur) open(cur)
    else if (e.key === ' ' && cur && !cur.dir) (e.preventDefault(), open(cur)) // Space is Quick Look
    else if (e.key === 'F2' && sel) startRename(sel.path)
    else if ((e.key === 'Delete' || (e.key === 'Backspace' && e.metaKey)) && chosen.length) remove(chosen)
    else if (e.key === 'Backspace' && path) location.hash = href(parentOf(path))
  }

  function windowKeys(e: KeyboardEvent) {
    const typing = e.target instanceof HTMLInputElement || e.target instanceof HTMLTextAreaElement
    const covered = !!(previewing || versioning || sharing || renaming)
    if (e.key === 'Escape') {
      if (menu || addMenu || chip) (menu = null), (addMenu = null), (chip = null)
      else if (!covered && !typing) endSelect()
    }
    if (typing || covered || !(isMac ? e.metaKey : e.ctrlKey)) return
    const k = e.key.toLowerCase()
    if (k === 'a') (e.preventDefault(), selectAll())
    else if (k === 'c' && chosen.length) take(false)
    else if (k === 'x' && chosen.length) take(true)
    else if (k === 'v' && clip.items.length) paste()
  }

  // Context menus act on the selection; right-clicking outside it selects that one object first.
  function showMenu(e: MouseEvent, entry: Entry | null) {
    e.preventDefault()
    e.stopPropagation()
    addMenu = null
    chip = null
    if (entry && !picked.has(entry.path)) pick(entry.path)
    const r = (e.currentTarget as HTMLElement).getBoundingClientRect()
    const items = entry ? [...chosen] : []
    menu = e.type === 'contextmenu' ? { items, x: e.clientX, y: e.clientY } : { items, x: r.right - 4, y: r.bottom + 4 }
  }
  function openChip(e: MouseEvent, of: 'type' | 'sort') {
    e.stopPropagation()
    menu = null
    addMenu = null
    const r = (e.currentTarget as HTMLElement).getBoundingClientRect()
    chip = chip?.of === of ? null : { of, x: of === 'sort' ? r.right : r.left, y: r.bottom + 4 }
  }

  type Act = { icon: string; label: string; run: () => unknown; danger?: boolean; key?: string } | null // null: a divider
  function actions(items: Entry[]): Act[] {
    const one = items.length === 1 ? items[0] : null
    const list: Act[] = []
    const add = (icon: string, label: string, run: () => unknown, more: { danger?: boolean; key?: string } = {}) => list.push({ icon, label, run, ...more })
    const gap = () => list.length && list.at(-1) && list.push(null)
    if (!items.length) {
      add('create_new_folder', 'New folder', newFolder)
      gap()
      add('upload_file', 'File upload', () => picker.click())
      add('drive_folder_upload', 'Folder upload', () => folderPicker.click())
      gap()
      if (clip.items.length) add('paste', clip.cut ? `Move ${count(clip.items)} here` : `Paste ${count(clip.items)}`, () => paste(), { key: 'V' })
      if (shown.length) add('select', 'Select all', selectAll, { key: 'A' })
      return list
    }
    if (one) add(one.dir ? 'folder' : 'visibility', one.dir ? 'Open' : 'Preview', () => open(one), { key: one.dir ? '' : 'Space' })
    if (one && !one.dir) add('share', 'Share link…', () => (sharing = one))
    add('download', one && !one.dir ? 'Download' : 'Download as zip', () => fetchItems(items))
    gap()
    add('copy', 'Copy', () => take(false, items), { key: 'C' })
    add('cut', 'Cut', () => take(true, items), { key: 'X' })
    if (one?.dir && clip.items.length) add('paste', `${clip.cut ? 'Move' : 'Paste'} into “${baseName(one.path)}”`, () => paste(one.path))
    add('file_copy', 'Make a copy', () => duplicate(items))
    add('archive', `Compress ${count(items)}`, () => compress(items))
    gap()
    if (one) add('rename', 'Rename', () => startRename(one.path), { key: 'F2' })
    if (one && !one.dir) add('history', 'Version history', () => (versioning = one))
    add('info', one?.dir ? 'Folder details' : 'File details', () => (inspecting = true))
    gap()
    add('trash', 'Move to Trash', () => remove(items), { danger: true, key: 'Del' })
    return list
  }
  const shortcut = (k: string) => (k.length === 1 ? (isMac ? '⌘' : 'Ctrl+') + k : k)

  // A menu opens where it was asked for, stays on screen, and blooms from that very point.
  function place(el: HTMLElement, at: { x: number; y: number; of?: string }) {
    const r = el.getBoundingClientRect()
    const x = at.of === 'sort' ? at.x - r.width : at.x // the sort menu hangs from its button's right edge
    const left = Math.max(8, Math.min(x, innerWidth - r.width - 8))
    const top = Math.max(8, Math.min(at.y, innerHeight - r.height - 8))
    Object.assign(el.style, { left: `${left}px`, top: `${top}px`, transformOrigin: `${at.x - left}px ${at.y - top}px` })
  }

  function focusSelect(el: HTMLInputElement) {
    el.focus()
    const dot = el.value.lastIndexOf('.')
    el.setSelectionRange(0, dot > 0 ? dot : el.value.length) // the name, not the extension
  }

  const ring = (f: number) => 56.55 * (1 - f) // dash offset of a 9 px ring at fraction f
</script>

<svelte:window
  onkeydown={windowKeys}
  onclick={() => ((menu = null), (addMenu = null), (chip = null))}
  ondragover={(e) => { if (hasFiles(e)) { e.preventDefault(); dragging = true } }}
  ondragleave={(e) => { if (!e.relatedTarget) dragging = false }}
  ondrop={(e) => { if (hasFiles(e)) { e.preventDefault(); dragging = false; dropped(e.dataTransfer!) } }}
/>

{#snippet item(e: Entry)}
  {@const card = grid && !e.dir}
  <!-- svelte-ignore a11y_click_events_have_key_events (the grid handles keys) -->
  <div class={grid ? (e.dir ? 'folder-card' : 'file-card') : 'trow row'} role="row" tabindex="-1" id={'row-' + e.path} data-path={e.path}
    aria-selected={picked.has(e.path)} aria-label={baseName(e.path)}
    class:drop-on={dropOn === e.path} class:cut={clip.cut && clip.items.some((c) => c.path === e.path)}
    draggable={renaming !== e.path} ondragstart={(ev) => dragStart(ev, e)} ondragend={dragEnd}
    ondragover={e.dir ? (ev) => dragOver(ev, e.path) : undefined} ondragenter={e.dir ? (ev) => dragOver(ev, e.path) : undefined}
    ondragleave={e.dir ? (ev) => dragLeave(ev, e.path) : undefined} ondrop={e.dir ? (ev) => dropInto(ev, e.path) : undefined}
    onpointerdown={(ev) => (touch = ev.pointerType === 'touch')}
    onclick={(ev) => press(ev, e)} ondblclick={() => !selecting && open(e)} oncontextmenu={(ev) => showMenu(ev, e)}>
    <span class="c-name" role="gridcell">
      {#if selecting}
        <span class="check" aria-hidden="true">{#if picked.has(e.path)}<Icon name="check" size={16} />{/if}</span>
      {:else}
        <span class="ico"><Visual entry={e} size="row" /></span>
      {/if}
      {#if renaming === e.path}
        <input class="field" aria-label="New name" bind:value={renameValue} use:focusSelect onblur={commitRename}
          onclick={(ev) => ev.stopPropagation()} ondblclick={(ev) => ev.stopPropagation()}
          onkeydown={(ev) => { ev.stopPropagation(); if (ev.key === 'Enter') commitRename(); if (ev.key === 'Escape') renaming = '' }} />
      {:else}
        <span class="nm" title={baseName(e.path)}>{baseName(e.path)}</span>
        {#if results && !grid}<span class="where caption">{parentOf(e.path) || 'My files'}</span>{/if}
      {/if}
    </span>
    {#if !grid}
      <span class="c-date num" role="gridcell">{when(dateBy === 'created' ? (e.created ?? e.mtime) : e.mtime)}</span>
      <span class="c-size num" role="gridcell">{e.dir && !e.size ? '—' : size(e.size)}</span>
    {/if}
    <span class="c-act" role="gridcell">
      <button class="icon-btn more" tabindex="-1" aria-label={'More actions for ' + baseName(e.path)} onclick={(ev) => showMenu(ev, e)}
        ondblclick={(ev) => ev.stopPropagation()}><Icon name="more" size={20} /></button>
    </span>
    {#if card}
      <span class="thumb"><Visual entry={e} size="tile" /></span>
    {/if}
  </div>
{/snippet}

<div class="files">
  <div class="col">
    <header class="head">
      {#if query.trim()}
        <h1 class="large-title current">Search results</h1>
      {:else}
        {#if crumbs.length && path !== COMPUTERS}
          <a class="icon-btn back" href={href(parentOf(path))} aria-label="Up to {parentOf(path) ? baseName(parentOf(path)) : 'My files'}"><Icon name="arrow_back" size={22} /></a>
        {/if}
        <nav class="crumbs" aria-label="Folder path">
          {#if crumbs.length}
            {#each ancestors as p}
              <a class="crumb" href={href(p)} class:drop-on={dropOn === p} ondragover={(ev) => dragOver(ev, p)} ondragenter={(ev) => dragOver(ev, p)}
                ondragleave={(ev) => dragLeave(ev, p)} ondrop={(ev) => dropInto(ev, p)}>{p ? baseName(p) : 'My files'}</a>
              <span class="sep" aria-hidden="true"><Icon name="chevron" size={22} /></span>
            {/each}
          {/if}
          <h1 class="large-title current">
            <button class="crumb here" aria-haspopup="menu" title="New in this folder"
              onclick={(ev) => { ev.stopPropagation(); menu = null; chip = null; addMenu = ev.currentTarget.getBoundingClientRect() }}>
              <span class="here-name">{crumbs.at(-1) ?? 'My files'}</span><Icon name="arrow_drop_down" size={24} />
            </button>
          </h1>
        </nav>
      {/if}
      <div class="tools">
        <button class="icon-btn select-tool" aria-label="Select" title="Select" aria-pressed={selecting} onclick={() => (selecting ? endSelect() : (selecting = true))}>
          <Icon name="select" size={22} />
        </button>
        <div class="seg" role="group" aria-label="Layout">
          <button aria-pressed={!grid} aria-label="List layout" title="List layout" onclick={() => setView(false)}>
            {#if !grid}<Icon name="check" size={18} />{/if}<Icon name="list" size={20} />
          </button>
          <button aria-pressed={grid} aria-label="Grid layout" title="Grid layout" onclick={() => setView(true)}>
            {#if grid}<Icon name="check" size={18} />{/if}<Icon name="grid" size={20} />
          </button>
        </div>
        <button class="icon-btn" aria-label="View details" title="View details" aria-pressed={inspecting} onclick={() => (inspecting = !inspecting)}>
          <Icon name="info" size={22} />
        </button>
      </div>
      <input bind:this={picker} type="file" multiple hidden
        onchange={() => { uploadItems([...picker.files!].map((file) => ({ rel: file.name, file }))); picker.value = '' }} />
      <input bind:this={folderPicker} type="file" webkitdirectory hidden
        onchange={() => { uploadItems([...folderPicker.files!].map((file) => ({ rel: file.webkitRelativePath || file.name, file }))); folderPicker.value = '' }} />
    </header>

    <!-- One row, two states: the Type chip and sorting, or the selection's actions (same height, so nothing jumps) -->
    <div class="bar">
      {#if chosen.length || selecting}
        <div class="selbar" role="toolbar" aria-label="Selection" in:fade={{ duration: motion(120) }}>
          <button class="icon-btn" aria-label="Clear selection" title="Clear selection" onclick={endSelect}><Icon name="close" size={20} /></button>
          <span class="sel-count num">{chosen.length} selected</span>
          {#if sel && !sel.dir}<button class="icon-btn" aria-label="Share link" title="Share link" onclick={() => (sharing = sel)}><Icon name="share" size={20} /></button>{/if}
          <button class="icon-btn" aria-label="Download" title="Download" disabled={!chosen.length} onclick={() => fetchItems(chosen)}><Icon name="download" size={20} /></button>
          <button class="icon-btn" aria-label="Copy" title="Copy" disabled={!chosen.length} onclick={() => take(false)}><Icon name="copy" size={20} /></button>
          <button class="icon-btn" aria-label="Cut" title="Cut" disabled={!chosen.length} onclick={() => take(true)}><Icon name="cut" size={20} /></button>
          <button class="icon-btn" aria-label="Move to Trash" title="Move to Trash" disabled={!chosen.length} onclick={() => remove(chosen)}><Icon name="trash" size={20} /></button>
          <button class="icon-btn" aria-label="More actions" title="More actions" disabled={!chosen.length}
            onclick={(ev) => { ev.stopPropagation(); const r = ev.currentTarget.getBoundingClientRect(); menu = { items: [...chosen], x: r.left, y: r.bottom + 4 } }}><Icon name="more" size={20} /></button>
        </div>
      {:else}
        <button class="chip" class:on={only} aria-haspopup="menu" onclick={(ev) => openChip(ev, 'type')}>
          {#if only}<Icon name={types.find((t) => t[0] === only)![2]} size={18} />{/if}
          {only ? types.find((t) => t[0] === only)![1] : 'Type'}<Icon name="arrow_drop_down" size={20} />
        </button>
        {#if only}<button class="icon-btn clear-chip" aria-label="Any type" title="Any type" onclick={() => (only = '')}><Icon name="close" size={18} /></button>{/if}
        <button class="sort-btn" aria-haspopup="menu" title="Sort" onclick={(ev) => openChip(ev, 'sort')}>
          {sortNames[sort.by]}<Icon name={sort.up ? 'arrow_upward' : 'arrow_downward'} size={18} />
        </button>
      {/if}
    </div>

    {#if error}<p class="error banner" role="alert"><Icon name="alert" size={18} />{error}</p>{/if}

    <!-- svelte-ignore a11y_click_events_have_key_events (keys are handled on the grid itself) -->
    <div class="objects" class:grid class:selecting role="grid" aria-label={results ? 'Search results' : 'Files'} aria-multiselectable="true"
      tabindex="0" onkeydown={keys} oncontextmenu={(ev) => showMenu(ev, null)}
      onclick={(ev) => { if (!(ev.target as Element).closest('[role="row"], .thead') && !selecting) picked.clear() }}>
      {#if grid}
        {#if folders.length}
          <div class="section" aria-hidden="true">Folders</div>
          <div class="folders" role="rowgroup" aria-label="Folders">{#each folders as e (e.path)}{@render item(e)}{/each}</div>
        {/if}
        {#if files.length}
          <div class="section" aria-hidden="true">Files</div>
          <div class="cards" role="rowgroup" aria-label="Files">{#each files as e (e.path)}{@render item(e)}{/each}</div>
        {/if}
      {:else if shown.length}
        <div class="thead" role="row">
          {#each [['name', 'c-name'], [dateBy, 'c-date'], ['size', 'c-size']] as [by, cls]}
            <span class="{cls}" role="columnheader" aria-sort={sort.by === by ? (sort.up ? 'ascending' : 'descending') : undefined}>
              <button class="th" onclick={() => sortBy(by as Sort)}>{sortNames[by as Sort]}
                {#if sort.by === by}<Icon name={sort.up ? 'arrow_upward' : 'arrow_downward'} size={18} />{/if}</button>
            </span>
          {/each}
          <span class="c-act"></span>
        </div>
        {#each shown as e (e.path)}{@render item(e)}{/each}
      {/if}
      {#if !shown.length && !loading && (!query.trim() || results)}
        {#if computers && crumbs.length <= 2 && !query.trim() && !only}
          <div class="empty">
            <span class="empty-mark computer"><Icon name="computer" size={96} /></span>
            <p class="title">{path === COMPUTERS ? 'No computers yet' : 'Nothing synced from this PC yet'}</p>
            <p class="callout">In the Aetheris app on a PC, choose folders to sync. They show up here under the PC’s name.</p>
            {#if path === COMPUTERS}<a class="btn plain get" href={WINDOWS_APP}><Icon name="download" size={18} />Get Aetheris for Windows</a>{/if}
          </div>
        {:else}
          <div class="empty">
            <span class="empty-mark"><Logo variant="mark" size={96} label="" /></span>
            <p class="title">{query.trim() ? 'No results' : only ? 'Nothing of this type here' : 'Drop files here'}</p>
            <p class="callout">{query.trim() ? 'Try another name.' : only ? 'Pick another type, or clear the filter.' : clip.items.length ? 'Or paste what you copied.' : 'Or use the “New” button.'}</p>
          </div>
        {/if}
      {/if}
    </div>
  </div>

  <!-- Details: a column beside the files (it floats over them in narrow windows); it leaves the way it came -->
  {#if inspecting}
    {@const subject = sel ?? here}
    <aside class="details" aria-label="Details" transition:fly={{ x: 28, duration: motion(220), easing: cubicOut }}>
      <header class="det-head">
        <span class="ico"><Visual entry={subject} size="row" /></span>
        <h2 class="title det-title">{sel ? baseName(sel.path) : chosen.length ? `${chosen.length} items` : (crumbs.at(-1) ?? 'My files')}</h2>
        <button class="icon-btn" aria-label="Close details" title="Close" onclick={() => (inspecting = false)}><Icon name="close" size={22} /></button>
      </header>
      <div class="det-preview"><Visual entry={subject} size="large" /></div>
      {#if sel}
        <h3 class="det-sub">{sel.dir ? 'Folder details' : 'File details'}</h3>
        <dl class="facts">
          <div><dt>Type</dt><dd>{kindLabel(sel.path, sel.dir)}</dd></div>
          {#if !sel.dir || sel.size}<div><dt>Size</dt><dd class="num">{size(sel.size)}</dd></div>{/if}
          <div><dt>Location</dt><dd><a href={href(parentOf(sel.path))}><Icon name="folder" fill size={18} />{parentOf(sel.path) ? baseName(parentOf(sel.path)) : 'My files'}</a></dd></div>
          <div><dt>Modified</dt><dd class="num">{when(sel.mtime)}</dd></div>
          {#if sel.created}<div><dt>Created</dt><dd class="num">{when(sel.created)}</dd></div>{/if}
        </dl>
      {:else}
        {@const of = chosen.length ? chosen : shown}
        <h3 class="det-sub">{chosen.length ? 'Selection' : 'Folder details'}</h3>
        <dl class="facts">
          <div><dt>Items</dt><dd class="num">{of.length}</dd></div>
          <div><dt>Size</dt><dd class="num">{size(of.reduce((n, x) => n + x.size, 0))}</dd></div>
        </dl>
      {/if}
    </aside>
  {/if}

  {#if dragging}
    <div class="drop" aria-hidden="true">
      <div class="drop-note"><Icon name="upload" size={20} />Drop files to upload them to <strong>{here.path}</strong></div>
    </div>
  {/if}
</div>

{#if menu}
  {@const m = menu}
  <!-- svelte-ignore a11y_click_events_have_key_events, a11y_interactive_supports_focus (Escape closes; items are buttons) -->
  <div class="menu glass ctx" role="menu" use:place={m} onclick={(ev) => ev.stopPropagation()} oncontextmenu={(ev) => ev.preventDefault()}>
    {#each actions(m.items) as a}
      {#if a}
        <button role="menuitem" class:danger={a.danger} onclick={() => { menu = null; a.run() }}>
          <Icon name={a.icon} size={20} /><span class="label">{a.label}</span>{#if a.key}<span class="kbd">{shortcut(a.key)}</span>{/if}
        </button>
      {:else}
        <hr />
      {/if}
    {/each}
  </div>
{/if}

{#if chip}
  {@const c = chip}
  <!-- svelte-ignore a11y_click_events_have_key_events, a11y_interactive_supports_focus (Escape closes; items are buttons) -->
  <div class="menu glass" role="menu" use:place={c} onclick={(ev) => ev.stopPropagation()}>
    {#if c.of === 'type'}
      {#each types as [k, label, icon]}
        <button role="menuitemradio" aria-checked={only === k} onclick={() => { only = only === k ? '' : k; chip = null }}>
          <span class="tone-{k}"><Icon name={icon} fill size={20} /></span>{label}
        </button>
      {/each}
    {:else}
      {#each Object.entries(sortNames) as [by, label]}
        <button role="menuitemradio" aria-checked={sort.by === by} onclick={() => { sortBy(by as Sort); chip = null }}>
          <span class="slot">{#if sort.by === by}<Icon name={sort.up ? 'arrow_upward' : 'arrow_downward'} size={20} />{/if}</span>{label}
        </button>
      {/each}
      {#if !grid}
        <hr />
        <button role="menuitemcheckbox" aria-checked={!sort.mixed} onclick={() => { sort = { ...sort, mixed: !sort.mixed }; chip = null }}>
          <span class="slot">{#if !sort.mixed}<Icon name="check" size={20} />{/if}</span>Folders on top
        </button>
      {/if}
    {/if}
  </div>
{/if}

{#if addMenu}
  {@const r = addMenu}
  {@const up = r.bottom > innerHeight - 220}
  <div class="menu glass" role="menu" style:left="{Math.max(8, Math.min(r.left, innerWidth - 216))}px"
    style:top={up ? undefined : `${r.bottom + 8}px`} style:bottom={up ? `${innerHeight - r.top + 8}px` : undefined}
    style:transform-origin={up ? 'bottom right' : 'top left'}>
    <button role="menuitem" onclick={(ev) => { ev.stopPropagation(); newFolder() }}><Icon name="create_new_folder" size={20} />New folder</button>
    <hr />
    <button role="menuitem" onclick={(ev) => { ev.stopPropagation(); pickFiles(picker) }}><Icon name="upload_file" size={20} />File upload</button>
    <button role="menuitem" onclick={(ev) => { ev.stopPropagation(); pickFiles(folderPicker) }}><Icon name="drive_folder_upload" size={20} />Folder upload</button>
  </div>
{/if}

<!-- The drag image for several objects: a small stack with the count, snapshotted by the browser -->
<div class="ghost" bind:this={ghost} aria-hidden="true"><span class="ghost-card"></span><span class="ghost-card"></span><span class="ghost-count num">{ghostCount}</span></div>

<!-- Snackbar, bottom left as in Drive: what's running, else what's on the clipboard -->
<div class="snack-row" aria-live="polite">
  {#if busy}
    <div class="snack" in:fly={{ y: 18, duration: motion(260), easing: cubicOut }} out:fade={{ duration: motion(140) }}>
      <span class="spinner" aria-hidden="true"></span><span class="snack-text">{busy}…</span>
    </div>
  {:else if clip.items.length}
    <div class="snack" in:fly={{ y: 18, duration: motion(260), easing: cubicOut }} out:fade={{ duration: motion(140) }}>
      <Icon name={clip.cut ? 'cut' : 'copy'} size={20} />
      <span class="snack-text">{clip.items.length === 1 ? baseName(clip.items[0].path) : `${clip.items.length} items`}</span>
      <button class="snack-act" onclick={() => paste()}>{clip.cut ? 'Move here' : 'Paste here'}</button>
      <button class="icon-btn" aria-label="Forget" title="Forget" onclick={() => (clip.items = [])}><Icon name="close" size={20} /></button>
    </div>
  {/if}
</div>

<!-- Uploads: Drive's card in the corner. One bar for the batch, a ring per file; failures stay until closed. -->
{#if jobs.length}
  <section class="uploads" aria-label="Uploads" in:fly={{ y: 24, duration: motion(260), easing: cubicOut }} out:fade={{ duration: motion(160) }}>
    <header class="up-head">
      <h2 class="up-title">{failed.length ? `${plural(failed.length, 'upload')} failed` : active.length ? `Uploading ${plural(active.length, 'item')}` : `${plural(jobs.length, 'upload')} complete`}</h2>
      <button class="icon-btn" aria-label={collapsed ? 'Show uploads' : 'Hide uploads'} aria-expanded={!collapsed} onclick={() => (collapsed = !collapsed)}>
        <Icon name={collapsed ? 'keyboard_arrow_up' : 'keyboard_arrow_down'} size={22} />
      </button>
      {#if !active.length}<button class="icon-btn" aria-label="Close" title="Close" onclick={() => (jobs = [])}><Icon name="close" size={22} /></button>{/if}
    </header>
    {#if active.length}<div class="up-bar" aria-hidden="true"><span style:transform="scaleX({progress})"></span></div>{/if}
    {#if !collapsed}
      <ul class="up-list">
        {#each jobs as j}
          <li class="up-row">
            <span class="ico"><Visual entry={{ path: j.name, size: j.size, mtime: 0, rev: 0 }} size="row" thumb={false} /></span>
            <span class="up-name">
              <span class="nm">{baseName(j.name)}</span>
              <span class="caption num">{j.error ? j.error : j.done ? size(j.size) : `${size(j.sent)} of ${size(j.size)}`}</span>
            </span>
            {#if j.error}<span class="st err"><Icon name="alert" fill size={24} /></span>
            {:else if j.done}<span class="st ok"><Icon name="check_circle" fill size={24} /></span>
            {:else}
              <svg class="ring" width="24" height="24" viewBox="0 0 24 24" fill="none" aria-hidden="true">
                <circle cx="12" cy="12" r="9" stroke="currentColor" stroke-opacity=".15" stroke-width="2.4" />
                <circle class="arc" cx="12" cy="12" r="9" stroke-width="2.4" stroke-linecap="round" stroke-dasharray="56.55"
                  stroke-dashoffset={ring(j.size ? j.sent / j.size : 0)} transform="rotate(-90 12 12)" />
              </svg>
            {/if}
          </li>
        {/each}
      </ul>
    {/if}
  </section>
{/if}

{#if previewing}
  <Preview entry={previewing} items={files} onclose={() => (previewing = null)} onshare={(e) => (sharing = e)} onchange={previewed} />
{/if}

{#if versioning}
  <Versions entry={versioning} onclose={() => (versioning = null)} onrestored={load} />
{/if}

{#if sharing}
  <ShareSheet entry={sharing} onclose={() => (sharing = null)} />
{/if}

<style>
  .files { position: relative; flex: 1; min-width: 0; display: flex; }
  .col { flex: 1; min-width: 0; display: flex; flex-direction: column; }

  /* Header: the folder path as Drive shows it; the current folder opens the "New" menu */
  .head { flex: none; display: flex; align-items: center; gap: .5rem; min-height: 4rem; padding: .5rem 1rem 0 1.25rem; }
  .crumbs { flex: 1; min-width: 0; display: flex; align-items: center; }
  .crumb {
    flex: 0 1000 auto; min-width: 2.5rem; height: 2.5rem; display: inline-flex; align-items: center; padding: 0 .65rem; border-radius: 999px;
    font: var(--type-large-title); color: var(--text-2); text-decoration: none; white-space: nowrap; overflow: hidden; text-overflow: ellipsis;
    transition: background-color 120ms ease-out;
  }
  a.crumb { display: block; line-height: 2.5rem; }
  .crumb:hover { background: var(--hover); }
  .crumb.drop-on { background: var(--container); color: var(--text); box-shadow: inset 0 0 0 2px var(--accent); }
  .sep { flex: none; display: grid; color: var(--text-3); }
  .current { flex: 0 1 auto; min-width: 0; margin: 0; display: flex; }
  .crumbs > .current:first-child { margin-left: -.65rem; } /* on the h1, not the button: a margin there would squeeze the name */
  .here { max-width: 100%; border: 0; background: none; color: var(--text); gap: .15rem; padding-right: .35rem; font: inherit; }
  .here-name { overflow: hidden; text-overflow: ellipsis; }
  .here :global(svg) { flex: none; color: var(--text-2); }
  h1.current:not(:has(button)) { padding: 0 .65rem 0 0; }
  .back { display: none; }

  .tools { display: flex; align-items: center; gap: .25rem; margin-left: auto; }
  .select-tool { display: none; }
  @media (hover: none) { .select-tool { display: inline-grid; } } /* touch: there is no Ctrl-click */
  /* Segmented button: the active half is tonal and carries a check */
  .seg { display: flex; height: 2.25rem; border: 1px solid var(--outline); border-radius: 999px; overflow: hidden; margin: 0 .25rem; }
  .seg button {
    display: flex; align-items: center; justify-content: center; gap: .15rem; min-width: 3.25rem; padding: 0 .75rem; border: 0;
    background: transparent; color: var(--text-2); transition: background-color 120ms ease-out;
  }
  .seg button + button { border-left: 1px solid var(--outline); }
  .seg button:hover { background: var(--hover); }
  .seg button[aria-pressed='true'] { background: var(--container); color: var(--text); }

  /* The bar under the path: chips, or the selection's actions */
  .bar { flex: none; display: flex; align-items: center; gap: .5rem; min-height: 3rem; padding: 0 1rem .25rem 1.25rem; }
  .chip {
    display: inline-flex; align-items: center; gap: .35rem; height: 2rem; padding: 0 .4rem 0 1rem; border-radius: .5rem;
    border: 1px solid var(--outline); background: transparent; font: 500 .875rem/1 var(--font); color: var(--text-2);
    transition: background-color 120ms ease-out;
  }
  .chip:hover { background: var(--hover); }
  .slot { display: grid; place-items: center; width: 20px; flex: none; } /* the selected option's arrow; the others keep its space */
  .chip.on { background: var(--container); border-color: transparent; color: var(--text); padding-left: .6rem; }
  .clear-chip { width: 2rem; height: 2rem; margin-left: -.35rem; }
  .sort-btn {
    margin-left: auto; display: inline-flex; align-items: center; gap: .35rem; height: 2.25rem; padding: 0 .75rem; border: 0; border-radius: 999px;
    background: transparent; font: 500 .875rem/1 var(--font); color: var(--text-2);
  }
  .sort-btn:hover { background: var(--hover); }
  .selbar {
    flex: 1; display: flex; align-items: center; gap: .125rem; height: 2.75rem; padding: 0 .5rem 0 .25rem; border-radius: 999px;
    background: var(--surface-2); min-width: 0; overflow-x: auto; scrollbar-width: none;
  }
  .selbar .icon-btn { width: 2.25rem; height: 2.25rem; }
  .selbar .icon-btn:disabled { opacity: .38; }
  .sel-count { font: 500 .875rem/1 var(--font); color: var(--text); margin: 0 1rem 0 .25rem; white-space: nowrap; }

  .banner { flex: none; margin: 0 1.25rem .75rem; }

  /* The scroll area. A short fade where objects slide under the header stands in for a divider. */
  .objects {
    flex: 1; min-height: 0; overflow-y: auto; outline: none; padding: 0 1rem 6rem 1.25rem; align-content: start;
    scrollbar-width: thin; scrollbar-color: var(--separator-strong) transparent;
  }
  .objects.grid { mask-image: linear-gradient(to bottom, transparent, #000 10px); }
  .section { margin: 1rem 0 .75rem; font: 500 .875rem/1.2 var(--font); color: var(--text); }
  .section:first-child { margin-top: .5rem; }
  .folders { display: grid; grid-template-columns: repeat(auto-fill, minmax(14rem, 1fr)); gap: .75rem 1rem; }
  .cards { display: grid; grid-template-columns: repeat(auto-fill, minmax(13.5rem, 1fr)); gap: 1rem; }

  /* Folder cards: a tonal chip with the folder, its name and its menu */
  .folder-card, .file-card {
    position: relative; min-width: 0; border-radius: .75rem; background: var(--surface-2); outline: none;
    transition: background-color 120ms ease-out, box-shadow 150ms ease-out, transform var(--press-duration) ease-out;
  }
  .folder-card { display: flex; align-items: center; height: 3rem; padding: 0 .25rem 0 1rem; }
  .folder-card .c-name, .file-card .c-name { flex: 1; min-width: 0; display: flex; align-items: center; gap: .85rem; }
  .folder-card:hover, .file-card:hover { background: var(--surface-3); }
  .folder-card:active, .file-card:active { transform: scale(.985); }
  /* File cards: name row on top, the picture (or a big type icon) below, as in Drive */
  .file-card { display: grid; grid-template-columns: minmax(0, 1fr) auto; align-items: center; padding: 0 .25rem .5rem; }
  .file-card .c-name { height: 3rem; padding-left: .75rem; }
  .file-card .ico { width: 1.25rem; height: 1.25rem; }
  .file-card .ico :global(svg) { width: 20px; height: 20px; }
  .thumb {
    grid-column: 1 / -1; margin: 0 .25rem; aspect-ratio: 4 / 3; border-radius: .5rem; overflow: hidden; background: var(--surface);
    display: grid; transition: opacity 200ms;
  }
  .folder-card[aria-selected='true'], .file-card[aria-selected='true'] { background: var(--container); }
  .file-card[aria-selected='true'] .thumb { box-shadow: 0 0 0 2px var(--accent); }

  /* A folder under a drag opens up to receive it */
  .folder-card.drop-on, .trow.drop-on { background: var(--container); box-shadow: inset 0 0 0 2px var(--accent); }
  /* What's been cut waits, dimmed, until it's pasted somewhere */
  .cut .ico, .cut .thumb { opacity: .4; }

  /* List: Drive's table. Selected rows are tonal; the keyboard's row gets a ring */
  .row { cursor: default; outline: none; scroll-margin-top: 3rem; }
  .row[aria-selected='true'], .row[aria-selected='true']:hover { background: var(--container); }
  .row .field { max-width: 26rem; }
  .row .where { white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
  [role='row']:focus-visible { box-shadow: inset 0 0 0 2px var(--accent); }
  .thead .c-name { gap: 0; }
  .th {
    display: inline-flex; align-items: center; gap: .3rem; height: 2rem; margin-left: -.6rem; padding: 0 .6rem; border: 0; border-radius: 999px;
    background: none; font: inherit; color: inherit;
  }
  .th:hover { background: var(--hover); color: var(--text); }
  [aria-sort] > .th { color: var(--text); }
  .more { color: var(--text-2); }
  .more:hover { background: var(--hover); }

  /* Select mode: a check circle stands in for the type icon */
  .check {
    flex: none; display: grid; place-items: center; width: 1.375rem; height: 1.375rem; border-radius: 50%;
    border: 2px solid var(--text-3); color: var(--on-accent);
  }
  [aria-selected='true'] .check { background: var(--accent-fill); border-color: var(--accent-fill); }

  .empty { display: grid; justify-items: center; gap: .5rem; padding: 4.5rem 1rem; text-align: center; }
  .empty p { margin: 0; }
  .empty-mark { opacity: .3; margin-bottom: .75rem; }
  .empty-mark.computer { display: grid; color: var(--text-3); opacity: .45; }
  .get { margin-top: 1rem; gap: .5rem; text-decoration: none; }

  /* Context menu: shortcuts in a whisper on the right */
  .ctx { min-width: 15rem; max-height: calc(100dvh - 16px); overflow-y: auto; }
  .ctx .label { flex: 1; }
  .ctx .kbd { font: 300 .75rem/1 var(--font); color: var(--text-3); letter-spacing: .02em; }
  .tone-folder { color: var(--tone-folder); display: grid; }
  .tone-image, .tone-video, .tone-audio, .tone-pdf { color: var(--tone-red); display: grid; }
  .tone-text { color: var(--tone-blue); display: grid; }
  .tone-file { color: var(--tone-gray); display: grid; }
  .menu :global(span[class^='tone-'] svg) { color: inherit; }

  /* Details column */
  .details {
    flex: none; width: 21rem; display: flex; flex-direction: column; gap: 1rem; padding: .5rem 1rem 1.5rem; overflow-y: auto;
    border-left: 1px solid var(--separator); background: var(--surface);
  }
  .det-head { display: flex; align-items: center; gap: .75rem; min-height: 3.5rem; }
  .det-title { flex: 1; min-width: 0; overflow-wrap: anywhere; }
  .det-preview { height: 12rem; flex: none; border-radius: .75rem; overflow: hidden; background: var(--surface-2); }
  .det-sub { margin: .5rem 0 0; font: 500 1rem/1.3 var(--font); }
  .facts { margin: 0; display: grid; gap: 1rem; }
  .facts div { display: grid; gap: .2rem; }
  .facts dt { font: 500 .75rem/1.2 var(--font); color: var(--text-2); }
  .facts dd { margin: 0; font: 300 .875rem/1.35 var(--font); color: var(--text); overflow-wrap: anywhere; }
  .facts a { display: inline-flex; align-items: center; gap: .35rem; padding: .25rem .6rem .25rem .4rem; margin-left: -.4rem; border-radius: .5rem;
    border: 1px solid var(--outline); color: var(--text); text-decoration: none; }
  .facts a:hover { background: var(--hover); }
  .facts a :global(svg) { color: var(--tone-folder); }

  /* Files dragged in from the computer: the surface lights up, a note says where they'll land */
  .drop {
    position: absolute; inset: 0; z-index: 40; border-radius: inherit; pointer-events: none;
    background: light-dark(rgb(229 75 75 / .06), rgb(255 169 135 / .08)); box-shadow: inset 0 0 0 2px var(--accent);
    display: flex; align-items: flex-end; justify-content: center; padding-bottom: 2rem;
  }
  .drop-note {
    display: flex; align-items: center; gap: .6rem; padding: .85rem 1.25rem; border-radius: .5rem;
    background: var(--inverse); color: var(--on-inverse); font: 400 .875rem/1.2 var(--font); box-shadow: var(--shadow-3);
  }
  .drop-note strong { font-weight: 500; }

  /* Off screen until a multi-object drag needs a picture of itself */
  .ghost { position: fixed; top: -200px; left: 0; width: 76px; height: 76px; pointer-events: none; }
  .ghost-card { position: absolute; inset: 10px; border-radius: 12px; background: var(--surface-2); border: 1px solid var(--outline); box-shadow: var(--shadow-1); }
  .ghost-card + .ghost-card { transform: translate(6px, -6px) rotate(4deg); }
  .ghost-count {
    position: absolute; top: 0; right: 0; min-width: 24px; height: 24px; padding: 0 7px; border-radius: 12px; display: grid; place-items: center;
    background: var(--accent-fill); color: var(--on-accent); font: 500 .78rem/1 var(--font);
  }

  /* Snackbar: the inverse surface, bottom left */
  .snack-row { position: fixed; z-index: 24; left: 1.5rem; bottom: 1.5rem; display: grid; pointer-events: none; }
  .snack {
    grid-area: 1 / 1; /* an outgoing snackbar and its successor cross-fade in one place */
    pointer-events: auto; display: flex; align-items: center; gap: .75rem; min-height: 3rem; padding: .25rem .5rem .25rem 1rem;
    border-radius: .5rem; background: var(--inverse); color: var(--on-inverse); box-shadow: var(--shadow-3); max-width: calc(100vw - 2rem);
  }
  .snack-text { font: 400 .875rem/1.3 var(--font); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; max-width: 18rem; }
  .snack .icon-btn { color: var(--on-inverse); }
  .snack .icon-btn:hover { background: rgb(128 128 128 / .2); color: var(--on-inverse); }
  .snack-act {
    height: 2.25rem; padding: 0 .75rem; border: 0; border-radius: 999px; background: none; color: var(--inverse-accent);
    font: 500 .875rem/1 var(--font); white-space: nowrap;
  }
  .snack-act:hover { background: rgb(128 128 128 / .2); }
  .spinner { width: 18px; height: 18px; flex: none; border-radius: 50%; border: 2px solid rgb(128 128 128 / .45); border-top-color: var(--inverse-accent); animation: spin .8s linear infinite; }
  @keyframes spin { to { transform: rotate(1turn); } }

  /* The upload card */
  .uploads {
    position: fixed; z-index: 25; right: 1.5rem; bottom: 0; width: min(22.5rem, calc(100vw - 2rem)); overflow: hidden;
    border-radius: 1rem 1rem 0 0; background: var(--surface); box-shadow: var(--shadow-3);
  }
  .up-head { display: flex; align-items: center; gap: .25rem; min-height: 3.5rem; padding: 0 .5rem 0 1.25rem; background: var(--surface-2); }
  .up-title { flex: 1; margin: 0; font: 500 .9375rem/1.3 var(--font); }
  .up-bar { height: 4px; background: var(--separator); }
  .up-bar span { display: block; height: 100%; background: var(--accent); transform-origin: left; transition: transform 200ms linear; }
  .up-list { list-style: none; margin: 0; padding: .25rem 0; max-height: 40dvh; overflow-y: auto; }
  .up-row { display: flex; align-items: center; gap: 1rem; min-height: 3.25rem; padding: .35rem 1.25rem; }
  .up-name { flex: 1; min-width: 0; display: grid; }
  .up-name .caption { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .st { display: grid; }
  .st.ok { color: light-dark(#1b8a55, #6dd5a0); }
  .st.err { color: var(--error); }
  .ring { flex: none; color: var(--text); }
  .ring .arc { stroke: var(--accent); transition: stroke-dashoffset 200ms linear; } /* progress moves continuously */

  @media (prefers-reduced-motion: reduce) {
    .spinner { animation: none; border-color: var(--inverse-accent); }
    .folder-card:active, .file-card:active { transform: none; }
  }
  @media (prefers-color-scheme: dark) {
    :root:not([data-theme='light']) .row[aria-selected='true'] :is(.c-date, .c-size) { color: var(--text-2); }
  }
  :root[data-theme='dark'] .row[aria-selected='true'] :is(.c-date, .c-size) { color: var(--text-2); }

  /* Narrow: the details float over the files instead of narrowing them */
  @media (max-width: 1100px) {
    .details { position: absolute; z-index: 15; top: 0; right: 0; bottom: 0; box-shadow: var(--shadow-3); border-left: 0; border-radius: 1rem 0 0 1rem; }
  }
  @media (max-width: 760px) {
    .head { padding: .25rem .5rem 0 .25rem; gap: .125rem; }
    .back { display: inline-grid; }
    .crumb:not(.here), .sep { display: none; }
    .crumbs > .current, .crumbs > .current:first-child { margin-left: 0; }
    h1.current:not(:has(button)) { padding-left: .75rem; }
    /* no room for both halves: one round button that switches to the other layout */
    .seg { border: 0; margin: 0; height: 2.5rem; }
    .seg button { width: 2.5rem; min-width: 0; padding: 0; border-radius: 50%; }
    .seg button + button { border-left: 0; }
    .seg button[aria-pressed='true'] { display: none; }
    .bar { padding: 0 .75rem .25rem; }
    .objects { padding: 0 .75rem calc(11rem + env(safe-area-inset-bottom)); } /* clear the nav bar and the plus */
    .banner { margin: 0 .75rem .75rem; }
    .folders { grid-template-columns: repeat(auto-fill, minmax(10rem, 1fr)); gap: .5rem; }
    .cards { grid-template-columns: repeat(auto-fill, minmax(10rem, 1fr)); gap: .5rem; }
    .row { padding-left: .75rem; }
    .details { width: 100%; border-radius: 1rem 1rem 0 0; padding-bottom: calc(6rem + env(safe-area-inset-bottom)); }
    .snack-row { left: .75rem; right: 5.5rem; bottom: calc(6.25rem + env(safe-area-inset-bottom)); }
    .uploads { right: .5rem; left: .5rem; width: auto; bottom: calc(5.5rem + env(safe-area-inset-bottom)); border-radius: 1rem; }
  }
</style>
