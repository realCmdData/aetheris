// Derives mark.svg from the vector logo (logo.svg) and renders the PNG + ICO set used by the web UI
// (favicon, PWA) and the Windows client.
import assert from 'node:assert/strict';
import { mkdirSync, readFileSync, writeFileSync } from 'node:fs';
import { Resvg } from '@resvg/resvg-js';

// The mark is the logo without its wordmark (the app sets "Aetheris" as real text: the SVG names fonts Windows
// lacks), cropped to a square around the symbol with room for its shadows.
const MARK_BOX = '130 55 540 540';
const logo = readFileSync(new URL('./logo.svg', import.meta.url), 'utf8');
const svg = Buffer.from(logo.replace(/<text[\s\S]*?<\/text>/g, '').replace(/ width="100%" height="100%"/, '')
  .replace(/viewBox="[^"]*"/, `viewBox="${MARK_BOX}"`));
assert(!svg.includes('<text') && svg.includes(MARK_BOX), 'mark derivation failed: logo.svg changed shape?');
writeFileSync(new URL('./mark.svg', import.meta.url), svg);

const small = readFileSync(new URL('./mark-small.svg', import.meta.url)); // the metal ribbon blurs at 32 px and below
const out = new URL('./icons/', import.meta.url);
mkdirSync(out, { recursive: true });

const png = (size, source = size <= 32 ? small : svg) =>
  new Resvg(source, { fitTo: { mode: 'width', value: size } }).render().asPng();

// Home-screen icons need an opaque tile (iOS fills transparency with black) and padding for maskable crops.
const inner = svg.toString().replace(/<svg[^>]*>|<\/svg>/g, '');
const tile = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 100 100"><rect width="100" height="100" fill="#F7EBE8"/>` +
  `<svg x="12" y="12" width="76" height="76" viewBox="${MARK_BOX}">${inner}</svg></svg>`;

for (const size of [16, 32, 48]) writeFileSync(new URL(`icon-${size}.png`, out), png(size));
// 44 / 50 / 150 = the Windows app package's logos (client-win/package)
for (const size of [44, 50, 150]) writeFileSync(new URL(`icon-${size}.png`, out), png(size));
// 180 = apple-touch-icon, 192/512 = PWA manifest
for (const size of [180, 192, 512]) writeFileSync(new URL(`icon-${size}.png`, out), png(size, tile));

// ICO with embedded PNGs (Windows Vista+). A directory size byte of 0 means 256.
const sizes = [16, 20, 24, 32, 48, 256];
const images = sizes.map((size) => png(size));
const dir = Buffer.alloc(6 + 16 * sizes.length);
dir.writeUInt16LE(1, 2); // type: icon
dir.writeUInt16LE(sizes.length, 4);
let offset = dir.length;
sizes.forEach((size, i) => {
  const e = 6 + 16 * i;
  dir.writeUInt8(size % 256, e);
  dir.writeUInt8(size % 256, e + 1);
  dir.writeUInt16LE(1, e + 4); // planes
  dir.writeUInt16LE(32, e + 6); // bits per pixel
  dir.writeUInt32LE(images[i].length, e + 8);
  dir.writeUInt32LE(offset, e + 12);
  offset += images[i].length;
});
const ico = Buffer.concat([dir, ...images]);
writeFileSync(new URL('aetheris.ico', out), ico);

// self-check: every directory entry points at a PNG signature inside the file
assert.equal(ico.length, offset);
for (let i = 0; i < sizes.length; i++) {
  const at = ico.readUInt32LE(6 + 16 * i + 12);
  assert.equal(ico.toString('latin1', at + 1, at + 4), 'PNG');
}
console.log(`icons written: ${sizes.length}-size aetheris.ico, 6 PNGs`);
