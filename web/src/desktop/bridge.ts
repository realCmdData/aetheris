// Messages between this page and the Windows app (client-win/AppWindow.cs) over WebView2.
// Outside the app (designing in a browser), a mock with sample data answers instead.

export type Transfer = { dir: 'up' | 'down'; path: string; done: number; size: number }
export type Event = { kind: string; path: string; to: string | null; size: number; at: number }
/** A folder from elsewhere on this PC that syncs to Computers/<computer>/<name>; here: connected right now. */
export type Synced = { name: string; path: string; here: boolean }

export type State = {
  name: string
  /** The server's tailnet name, and its address at home; null before the setup. */
  host: string | null
  home: string | null
  /** The web app; null when the server didn't say. */
  web: string | null
  route: string | null
  paired: boolean
  phase: 'Starting' | 'NotPaired' | 'Syncing' | 'UpToDate' | 'Offline' | 'Paused' | 'Failed'
  status: string
  detail: string
  lastSync: number
  autostart: boolean
  drive: { letter: string | null; free: string[] }
  storage: { bytes: number; files: number; online: number; pinned: number }
  transfers: Transfer[]
  activity: Event[]
  computer: string
  folders: Synced[]
  suggested: { name: string; path: string }[]
  version: string
  update: string | null
  /** Free up space automatically: days a file may go unopened before it becomes online-only; 0 = off. */
  autoFree: number
  /** Whether Windows records when files are opened (else "not opened" can only mean "not changed"). */
  recordsOpens: boolean
  /** One upload after another for over 5 s: seconds left (null while counting), what is left, and how far along (0–1). */
  longUpload: { left: number | null; files: number; bytes: number; done: number } | null
}

export type Message =
  | ({ type: 'state' } & State)
  | { type: 'paired'; error: string | null }
  | { type: 'freed'; bytes: number }
  | { type: 'folder'; error: string | null }
  | { type: 'error'; message: string }

type Listener = (m: Message) => void
const host = (window as any).chrome?.webview
const listeners: Listener[] = []

export function listen(fn: Listener) {
  if (host) host.addEventListener('message', (e: MessageEvent) => fn(e.data))
  else listeners.push(fn)
}

export function send(message: { type: string; [k: string]: unknown }) {
  if (host) host.postMessage(message)
  else mock(message)
}

// ---- mock: a believable PC, for designing without the app ----
const now = Date.now()
const state: State = {
  name: 'Aetheris',
  host: location.hash.includes('unpaired') ? null : 'aetheris.tail1234.ts.net',
  home: null,
  web: location.hash.includes('unpaired') ? null : 'https://drive.example.com',
  route: 'Home network',
  paired: !location.hash.includes('unpaired'),
  phase: 'UpToDate',
  status: 'Up to date',
  detail: '',
  lastSync: now - 40_000,
  autostart: true,
  drive: { letter: 'A', free: ['A', 'D', 'E', 'F', 'G', 'X', 'Y', 'Z'] },
  storage: { bytes: 1_284_000_000, files: 12, online: 43, pinned: 3 },
  transfers: location.hash.includes('busy')
    ? [
        { dir: 'down', path: 'vortex-setup-2.9.0.exe', done: 161_000_000, size: 378_742_064 },
        { dir: 'up', path: 'Documents/Quarterly report.docx', done: 2_100_000, size: 5_400_000 },
      ]
    : [],
  activity: [
    { kind: 'up', path: 'Documents/Notes.md', to: null, size: 2_048, at: now - 90_000 },
    { kind: 'down', path: 'Photos/2026/Holiday/IMG_2041.jpg', to: null, size: 4_812_000, at: now - 6 * 60_000 },
    { kind: 'conflict', path: 'Documents/Budget.xlsx', to: 'Documents/Budget (conflict DESKTOP-01 2026-11-06 17.05).xlsx', size: 0, at: now - 40 * 60_000 },
    { kind: 'changed', path: 'Documents/Meeting notes.md', to: null, size: 40_117, at: now - 2 * 3_600_000 },
    { kind: 'move', path: 'Photos/Trip', to: 'Photos/Trip 2026', size: 0, at: now - 5 * 3_600_000 },
    { kind: 'free', path: '9 files', to: null, size: 912_000_000, at: now - 26 * 3_600_000 },
    { kind: 'delete', path: 'Downloads/old-installer.msi', to: null, size: 0, at: now - 50 * 3_600_000 },
  ],
  computer: 'DESKTOP-01',
  folders: location.hash.includes('unpaired') ? [] : ['Documents', 'Downloads'].map((name) => ({ name, path: `C:\\Users\\you\\${name}`, here: true })),
  suggested: [],
  version: '1.2.1',
  update: location.hash.includes('update') ? '1.3.0' : null,
  autoFree: 0,
  recordsOpens: true,
  longUpload: location.hash.includes('long') ? { left: 754, files: 1204, bytes: 2_480_000_000, done: 0.37 } : null,
}
const usual = ['Desktop', 'Documents', 'Pictures'].map((name) => ({ name, path: `C:\\Users\\you\\${name}` }))
const suggest = () => (state.suggested = usual.filter((u) => !state.folders.some((f) => f.path === u.path)))
suggest()

function emit(m: Message) {
  setTimeout(() => listeners.forEach((fn) => fn(m)), 120)
}

function mock(m: { type: string; [k: string]: unknown }) {
  switch (m.type) {
    case 'pair':
      if (String(m.code).replace('-', '').length === 8 && m.code !== 'AAAA-AAAA' && String(m.server).includes('.')) {
        Object.assign(state, { paired: true, host: m.server, home: m.home || '192.168.1.20:8443', web: 'https://drive.example.com' })
        emit({ type: 'paired', error: null })
      } else emit({ type: 'paired', error: "That code didn't work. Codes last 10 minutes: make a new one in Settings." })
      break
    case 'pause': Object.assign(state, { phase: 'Paused', status: 'Paused' }); break
    case 'resume': Object.assign(state, { phase: 'UpToDate', status: 'Up to date' }); break
    case 'autostart': state.autostart = Boolean(m.on); break
    case 'drive': state.drive.letter = (m.letter as string) ?? null; break
    case 'free': emit({ type: 'freed', bytes: state.storage.bytes }); state.storage = { ...state.storage, bytes: 0, online: state.storage.online + state.storage.files - state.storage.pinned, files: state.storage.pinned }; break
    case 'signout': state.paired = false; break
    case 'addFolder': {
      const path = (m.path as string) ?? 'D:\\Projects'
      if (path.startsWith('C:\\Windows')) emit({ type: 'folder', error: "Windows and app folders can't sync." })
      else {
        state.folders.push({ name: path.slice(path.lastIndexOf('\\') + 1), path, here: true })
        emit({ type: 'folder', error: null })
      }
      suggest()
      break
    }
    case 'autoFree': state.autoFree = Number(m.days); break
    case 'removeFolder':
      state.folders = state.folders.filter((f) => f.path !== m.path)
      emit({ type: 'folder', error: null })
      suggest()
      break
  }
  emit({ type: 'state', ...structuredClone(state) })
}
