export function size(bytes: number) {
  if (bytes < 1024) return `${bytes} B`
  const units = ['KB', 'MB', 'GB', 'TB']
  let v = bytes / 1024
  let i = 0
  for (; v >= 1024 && i < units.length - 1; i++) v /= 1024
  return `${v < 10 ? v.toFixed(1) : Math.round(v)} ${units[i]}`
}

const day = 86_400_000
const time = new Intl.DateTimeFormat(undefined, { hour: 'numeric', minute: '2-digit' }) // 8:47 PM, not 08:47 PM
const date = new Intl.DateTimeFormat(undefined, { day: 'numeric', month: 'short', year: 'numeric' })

export function when(ms: number) {
  const today = new Date().setHours(0, 0, 0, 0)
  if (ms >= today) return `Today ${time.format(ms)}`
  if (ms >= today - day) return `Yesterday ${time.format(ms)}`
  return date.format(ms)
}

export type Kind = 'folder' | 'image' | 'video' | 'audio' | 'pdf' | 'text' | 'file'

const kinds: Record<string, Kind> = {}
// Keep in sync with previewTypes in server/internal/api/api.go: these are exactly what it shows inline.
const types: Record<string, string> = {
  jpg: 'image/jpeg', jpeg: 'image/jpeg', png: 'image/png', gif: 'image/gif', webp: 'image/webp', avif: 'image/avif', bmp: 'image/bmp', ico: 'image/x-icon',
  mp4: 'video/mp4', m4v: 'video/mp4', webm: 'video/webm', mov: 'video/quicktime', ogv: 'video/ogg',
  mp3: 'audio/mpeg', m4a: 'audio/mp4', ogg: 'audio/ogg', opus: 'audio/ogg', flac: 'audio/flac', wav: 'audio/wav', aac: 'audio/aac',
  pdf: 'application/pdf',
}
for (const [e, t] of Object.entries(types)) kinds[e] = t === 'application/pdf' ? 'pdf' : (t.split('/')[0] as Kind)
for (const e of `txt md log csv tsv json xml yaml yml toml ini cfg conf env js mjs ts tsx jsx css scss py go rs c h cpp hpp cs java kt
  swift rb php sh bash ps1 bat cmd sql srt vtt tex gcode`.split(/\s+/)) kinds[e] = 'text'

const ext = (path: string) => path.slice(path.lastIndexOf('.') + 1).toLowerCase()
/** The type a previewable file is shown as (shared links build their preview from it). */
export const mimeOf = (path: string) => types[ext(path)] ?? (kinds[ext(path)] === 'text' ? 'text/plain' : 'application/octet-stream')

const kindNames: Record<Kind, string> = { folder: 'Folder', image: 'image', video: 'video', audio: 'audio', pdf: 'document', text: 'text', file: 'file' }

/** "JPG image", "3MF file", "Folder": the extension is told here, in the Inspector, never on the tile. */
export function kindLabel(path: string, dir?: boolean) {
  const k = kind(path, dir)
  const dot = path.lastIndexOf('.')
  const ext = !dir && dot > path.lastIndexOf('/') + 1 ? path.slice(dot + 1).toUpperCase() : ''
  return k === 'folder' ? kindNames.folder : ext ? `${ext} ${kindNames[k]}` : 'File'
}

/** Kind decides the icon and whether the server will show it inline (its preview allow-list). */
export function kind(path: string, dir?: boolean): Kind {
  if (dir) return 'folder'
  return kinds[ext(path)] ?? 'file'
}

const rel = new Intl.RelativeTimeFormat(undefined, { numeric: 'auto' })
/** "in 7 days", "in 3 hours": how long until a moment. */
export function until(ms: number) {
  const h = (ms - Date.now()) / 3.6e6
  if (Math.round(h * 60) < 60) return rel.format(Math.max(1, Math.round(h * 60)), 'minute')
  return h < 48 ? rel.format(Math.round(h), 'hour') : rel.format(Math.round(h / 24), 'day')
}

/** "1 view and unlimited downloads left": what a share link still allows (-1 = unlimited). */
export function allowance(views: number, downloads: number) {
  const n = (c: number, what: string) => (c < 0 ? `unlimited ${what}s` : `${c} ${what}${c === 1 ? '' : 's'}`)
  return [views && n(views, 'view'), downloads && n(downloads, 'download')].filter(Boolean).join(' and ') + ' left'
}
