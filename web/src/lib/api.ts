// Client for the NAS API (docs/protocol.md). Same origin: the gateway forwards /api/ to the NAS.

/** created: when the item came to be (unix ms); size: for folders, everything inside. */
export type Entry = { path: string; dir?: boolean; size: number; mtime: number; created?: number; hash?: string; rev: number }
export type Trashed = { id: string; path: string; dir?: boolean; size: number; deleted: number }
export type Session = { id: string; kind: 'web' | 'device'; name: string; created: number; lastSeen: number; current?: boolean }
export type Passkey = { id: string; name: string; created: number; lastUsed: number }
export type AuditEvent = { ts: number; event: string; detail: string; source: string }
export type Change = { seq: number; op: string; path: string; from?: string }
export type Version = { id: string; created: number; size: number; hash: string }
export type Share = { id: string; key: string; path: string; created: number; expires: number; views: number; downloads: number }

export class ApiError extends Error {
  constructor(public status: number, message: string, public body: any = null) {
    super(message)
  }
}

let signedOut = () => {}
export const onSignedOut = (fn: () => void) => (signedOut = fn)

async function call<T = any>(method: string, url: string, body?: unknown): Promise<T> {
  const init: RequestInit = { method }
  if (body instanceof Blob) init.body = body
  else if (body !== undefined) {
    init.body = JSON.stringify(body)
    init.headers = { 'Content-Type': 'application/json' }
  }
  const r = await fetch(url, init)
  if (r.status === 204) return null as T
  const data = r.headers.get('Content-Type')?.includes('json') ? await r.json() : null
  if (!r.ok) {
    if (r.status === 401 && !url.startsWith('/api/v1/auth/')) signedOut()
    throw new ApiError(r.status, data?.error ?? r.statusText, data)
  }
  return data
}

const q = encodeURIComponent
/** Folders synced from PCs (the Windows app) live under Computers/<PC>/; the web shows them as a section of their own. */
export const COMPUTERS = 'Computers'
/** The Windows app's installer, newest release (it installs itself per user and keeps itself up to date). */
export const WINDOWS_APP = 'https://github.com/realCmdData/aetheris/releases/latest/download/AetherisSetup.exe'
export const join = (dir: string, name: string) => (dir ? `${dir}/${name}` : name)
export const baseName = (p: string) => p.slice(p.lastIndexOf('/') + 1)
export const parentOf = (p: string) => p.slice(0, Math.max(p.lastIndexOf('/'), 0))
export const fileURL = (path: string, inline = false) => `/api/v1/file?path=${q(path)}${inline ? '&inline=1' : ''}`
// rev in the URL: thumbnails are cached for a year, a changed file gets a new URL
export const thumbURL = (e: Entry) => `/api/v1/thumb?path=${q(e.path)}&rev=${e.rev}`
/** A synced PC folder's own icon (Downloads' arrow, say), as the Windows app sent it; 404 when there is none. */
export const iconURL = (path: string) => `/api/v1/icon?path=${q(path)}`
export const versionURL = (path: string, id: string) => `/api/v1/versions/file?path=${q(path)}&id=${q(id)}`

export function download(path: string, href = fileURL(path)) {
  const a = document.createElement('a')
  a.href = href
  a.download = baseName(path)
  a.click()
}

/** The link a recipient opens. The key sits in the #fragment: browsers never send it anywhere. */
export const shareURL = (s: Share) => `${location.origin}/#/s/${s.id}.${s.key}`

/** Runs fn with name, then "name (1)", "name (2)"… while the server says the name is taken. */
export async function freeName<T>(name: string, fn: (name: string) => Promise<T>): Promise<T> {
  for (let n = 0; ; n++) {
    try {
      return await fn(n === 0 ? name : withSuffix(name, n))
    } catch (e) {
      const taken = e instanceof ApiError && e.status === 409 && (e.body?.error === 'conflict' || e.message === 'already exists')
      if (!taken || n >= 99) throw e
    }
  }
}

export const api = {
  status: () => call<{ signedIn: boolean; hasPasskeys: boolean }>('GET', '/api/v1/auth/status'),
  list: (path: string) => call('GET', `/api/v1/tree?path=${q(path)}`).then((r) => r.entries as Entry[]),
  search: (text: string) => call('GET', `/api/v1/search?q=${q(text)}`).then((r) => r.entries as Entry[]),
  recent: () => call('GET', '/api/v1/recent').then((r) => r.entries as Entry[]),
  versions: (path: string) => call('GET', `/api/v1/versions?path=${q(path)}`).then((r) => r.versions as Version[]),
  restoreVersion: (path: string, id: string) => call<Entry>('POST', '/api/v1/versions/restore', { path, id }),
  mkdir: (path: string) => call<Entry>('POST', '/api/v1/mkdir', { path }),
  move: (from: string, to: string) => call<Entry>('POST', '/api/v1/move', { from, to }),
  copy: (from: string, to: string) => call<Entry>('POST', '/api/v1/copy', { from, to }),
  compress: (paths: string[], to: string) => call<Entry>('POST', '/api/v1/compress', { paths, to }),
  zip: (paths: string[], name: string) => call<{ url: string }>('POST', '/api/v1/zip', { paths, name }),
  shares: () => call('GET', '/api/v1/shares').then((r) => r.shares as Share[]),
  share: (path: string, views: number, downloads: number, hours: number) => call<Share>('POST', '/api/v1/shares', { path, views, downloads, hours }),
  unshare: (id: string) => call('DELETE', `/api/v1/shares/${q(id)}`),
  remove: (path: string) => call('POST', '/api/v1/delete', { path }),
  trash: () => call('GET', '/api/v1/trash').then((r) => r.items as Trashed[]),
  restore: (id: string) => call('POST', '/api/v1/trash/restore', { id }),
  head: () => call<{ epoch: string; cursor: number }>('GET', '/api/v1/changes/head'),
  changes: (epoch: string, cursor: number, signal: AbortSignal) =>
    fetch(`/api/v1/changes?epoch=${epoch}&cursor=${cursor}&wait=50`, { signal }).then(async (r) => {
      if (!r.ok) throw new ApiError(r.status, r.statusText)
      return (await r.json()) as { cursor: number; changes: Change[] }
    }),
  sessions: () => call('GET', '/api/v1/sessions').then((r) => r.sessions as Session[]),
  revoke: (id: string) => call('DELETE', `/api/v1/sessions/${q(id)}`),
  passkeys: () => call('GET', '/api/v1/passkeys').then((r) => r.passkeys as Passkey[]),
  removePasskey: (id: string) => call('DELETE', `/api/v1/passkeys/${q(id)}`),
  /** server and lan: what the Windows app connects to (the NAS's tailnet address, and its address at home). */
  pair: () => call<{ code: string; expires: number; server?: string; lan?: string }>('POST', '/api/v1/devices/pair'),
  audit: () => call('GET', '/api/v1/audit').then((r) => r.events as AuditEvent[]),
  logout: () => call('POST', '/api/v1/auth/logout'),
}

// Passkeys. The browser's JSON helpers handle every base64url conversion.
const PKC = PublicKeyCredential as any

export function deviceName() {
  const ua = navigator.userAgent
  if (/iPhone/.test(ua)) return 'iPhone'
  if (/iPad/.test(ua)) return 'iPad'
  if (/Android/.test(ua)) return 'Android phone'
  if (/Mac OS/.test(ua)) return 'Mac'
  if (/Windows/.test(ua)) return 'Windows PC'
  return 'Passkey'
}

export async function registerPasskey(setupToken = '') {
  const begin = await call('POST', '/api/v1/auth/register/begin', { setupToken })
  const cred = await navigator.credentials.create({ publicKey: PKC.parseCreationOptionsFromJSON(begin.options.publicKey) })
  await call('POST', `/api/v1/auth/register/finish?ceremony=${begin.ceremony}&name=${q(deviceName())}`, (cred as any).toJSON())
}

export async function signIn() {
  const begin = await call('POST', '/api/v1/auth/login/begin')
  const cred = await navigator.credentials.get({ publicKey: PKC.parseRequestOptionsFromJSON(begin.options.publicKey) })
  await call('POST', `/api/v1/auth/login/finish?ceremony=${begin.ceremony}`, (cred as any).toJSON())
}

// Uploads go in 4 MB chunks: the most one request may carry through the Vercel gateway.
const CHUNK = 4 << 20
const sleep = (ms: number) => new Promise((r) => setTimeout(r, ms))

function withSuffix(name: string, n: number) {
  const dot = name.lastIndexOf('.')
  return dot > 0 ? `${name.slice(0, dot)} (${n})${name.slice(dot)}` : `${name} (${n})`
}

/** Uploads file into folder dir, resuming after network drops. A name clash keeps both: "a (1).txt". */
export async function upload(file: File, dir: string, progress: (sent: number) => void): Promise<Entry> {
  const { id } = await call('POST', '/api/v1/upload')
  let offset = 0
  let retries = 0
  while (offset < file.size) {
    try {
      offset = (await call('PUT', `/api/v1/upload/${id}?offset=${offset}`, file.slice(offset, offset + CHUNK))).offset
      retries = 0
      progress(offset)
    } catch (e) {
      if (e instanceof ApiError && e.body?.error === 'offset') offset = e.body.offset
      else if (e instanceof ApiError || ++retries > 5) throw e
      else {
        await sleep(1000 * retries) // the connection dropped: ask the server where to resume
        offset = (await call('GET', `/api/v1/upload/${id}`)).offset
      }
    }
  }
  return freeName(file.name, (name) => call<Entry>('POST', `/api/v1/upload/${id}/commit`, { path: join(dir, name), baseRev: 0, mtime: file.lastModified }))
}
