// Reads share links, which arrive encrypted (AES-256-GCM) under the key in the link's #fragment.
// The format is described in server/internal/api/share.go; keep both in sync.

const CHUNK = 256 << 10
const bytes = (b64url: string) => Uint8Array.from(atob(b64url.replace(/-/g, '+').replace(/_/g, '/')), (c) => c.charCodeAt(0))

export const importKey = (key: string) => crypto.subtle.importKey('raw', bytes(key), 'AES-GCM', false, ['decrypt'])

export type Meta = { name: string; size: number; mtime: number }

export async function openMeta(key: CryptoKey, sealed: string): Promise<Meta> {
  const b = bytes(sealed)
  const plain = await crypto.subtle.decrypt({ name: 'AES-GCM', iv: b.subarray(0, 12) }, key, b.subarray(12))
  return JSON.parse(new TextDecoder().decode(plain))
}

/** Decrypts a content stream chunk by chunk as it arrives. A cut, extended or altered stream throws. */
export async function openStream(key: CryptoKey, body: ReadableStream<Uint8Array>, progress: (done: number, total: number) => void) {
  const reader = body.getReader()
  const parts: Uint8Array[] = []
  let head: Uint8Array | null = null
  let size = 0, n = 0, i = 0, got = 0
  let buf = new Uint8Array(16), fill = 0
  for (;;) {
    const { value, done } = await reader.read()
    if (done) break
    for (let off = 0; off < value.length; ) {
      if (head && i >= n) throw new Error('The download carried extra data.')
      const take = Math.min(buf.length - fill, value.length - off)
      buf.set(value.subarray(off, off + take), fill)
      fill += take
      off += take
      if (fill < buf.length) continue
      if (!head) {
        head = buf // 8-byte nonce prefix, then the plaintext size
        size = Number(new DataView(head.buffer).getBigUint64(8))
        n = Math.max(1, Math.ceil(size / CHUNK))
      } else {
        const iv = new Uint8Array(12)
        iv.set(head.subarray(0, 8))
        new DataView(iv.buffer).setUint32(8, (i | (i === n - 1 ? 0x80000000 : 0)) >>> 0) // the last chunk is marked
        parts.push(new Uint8Array(await crypto.subtle.decrypt({ name: 'AES-GCM', iv }, key, buf)))
        got += buf.length - 16
        i++
        progress(got, size)
      }
      buf = new Uint8Array(16 + (i < n - 1 ? CHUNK : size - (n - 1) * CHUNK))
      fill = 0
    }
  }
  if (!head || i !== n) throw new Error('The download was cut off. Try again.')
  return parts
}
