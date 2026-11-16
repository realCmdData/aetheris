import { Spring } from '../lib/spring'

// Progress arrives ten times a second; a critically damped spring glides between the steps instead of
// jumping, and always continues from what is on screen. Sets --p (0–100) on the element.
export function glide(node: HTMLElement, percent: number) {
  const spring = new Spring(percent, (v) => node.style.setProperty('--p', String(v)))
  spring.set(percent)
  return { update: (next: number) => spring.to(next, { response: 0.45 }) }
}

/** "just now", "4 min ago", "3 h ago", then the date. */
export function ago(ms: number, now = Date.now()) {
  const s = (now - ms) / 1000
  if (s < 45) return 'just now'
  if (s < 3600) return `${Math.max(1, Math.round(s / 60))} min ago`
  if (s < 86_400) return `${Math.round(s / 3600)} h ago`
  return new Intl.DateTimeFormat(undefined, { day: 'numeric', month: 'short' }).format(ms)
}

export const base = (p: string) => p.slice(p.lastIndexOf('/') + 1)
export const folder = (p: string) => (p.includes('/') ? p.slice(0, p.lastIndexOf('/')) : '')
