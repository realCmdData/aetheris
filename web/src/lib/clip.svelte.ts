import type { Entry } from './api'

// What Copy or Cut picked up. It lives outside the folder view so it travels with you between folders.
export const clip = $state<{ cut: boolean; items: Entry[] }>({ cut: false, items: [] })
