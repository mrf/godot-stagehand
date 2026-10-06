// Tests for the mod's PNG decoder (png.ts), the frame fallback in
// docs/design/claude-code-plugin.md (D7). Run with `claude plugin test
// integrations/claude-code`.
//
// How the fixtures were made: a throwaway Go program (not kept) wrote each
// chunk by hand with hash/crc32. Pixel (x, y) is `pattern(x, y)` below, and row
// y uses filter type y % 5 (None, Sub, Up, Average, Paeth). compress/zlib chose
// the deflate blocks: NoCompression for RGB_STORED, and BestCompression for the
// rest, which gave a fixed-Huffman block at 5×5 and dynamic ones at 48×32 and
// 16×12. The program checked each first block's BTYPE, and `firstBlockType`
// checks it again here. RGB_DYNAMIC also flushes halfway, so it holds several
// blocks, one of them an empty stored block, and its stream is split over two
// IDAT chunks. BAD_ADLER flips the last Adler-32 byte, and BAD_ZLIB_HEADER the
// header's check bits. Both keep valid chunk CRCs. INTERLACED is RGB_STORED's
// data under an IHDR that says Adam7. PALETTE and RGBA16 come from Go's
// image/png encoder. Python's zlib cross-checked the lot.
import { expect, test } from 'claude-code/testing'
import { decodePng, inflate, PngError, type PngResult } from './png.js'

const RGB_STORED =
  'iVBORw0KGgoAAAANSUhEUgAAAAUAAAAFCAIAAAACDbGyAAAAYElEQVR4AQBQAK//AAAHAEMUBYYhFMkuLQw7UAEfbANDDQVDDQ9DDRlDDSMCH2UJH2UJH2UJH2UJH2UJAz7OFTG5CjG5D7G5FDE5GQQfZRUfDQUfDQ9DDRUfDRUBAAD//zxHDn3bMO76AAAAAElFTkSuQmCC'
const RGBA_FIXED =
  'iVBORw0KGgoAAAANSUhEUgAAAAUAAAAFCAYAAACNbyblAAAAd0lEQVR42mJgYGfIdxZhvdemKOJ7Uk93D491gDajfA7zFGde1inOvPxTnHklpzjzKk9hkk/lVJVP5fSST+XMl0/lnCKfyrmT2e6caJPhTi4Rw5386ht3iuw0tJQ8wyKfKqoqz8vKLM/Lz+zMK8oszyvKDAgAAP//W0kYgGUxe78AAAAASUVORK5CYII='
const RGB_DYNAMIC =
  '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'
const RGBA_DYNAMIC =
  'iVBORw0KGgoAAAANSUhEUgAAABAAAAAMCAYAAABr5z2BAAACs0lEQVR42lzHQWhjRRjA8bRvM+6oHxFNdKOw35YYJhCfOvsWw2jRxA9XaZiagxHdxY0rfrjFarRF7GID2lrbYkEj00asitiDbT3YoKjPRkRFqcVqD/UakpMoCLVUQRAqgofi4Q+/fyRySaRK8Wj7pb64/e5Gv3X5baXsvffU3nit/JH86ZE/xo5V8z+fHZ+//+3Z/W+6Cw+dun5pe+mxNXvl6uc7L/y2ybs9OOw5gqgjiDmCpCNIOQLfEeQcQcERFB1B2RFUHMGQIxh1BDVHMO0I6q4XWaaR5QCyrCJLhyxDZNlGlh6yzCBLiyxHkGUDWbaQZRdZCmSZRZYl7/YfEhM6vDSuw5hqhvFQm+T32hxva5P6vWmWJ7Xxr9bmZEabT9abpn9Lm0JHm2/3tClOabPdOIKcSCNEPYSYR5Dw8FAEicz//j/XBEIii1AvRRYjxUwbSqW+aytjj6qxd947tbjxa2Fj1x88OPbU2Xz+wwsvX/jzmc6rZuKOT8dfWe588eZ1R3tXXr/5ro/xgZmv157v+YyHLUHUEsQsQdISpCyBbwlylqBgCYqWoGwJKpZgyBKMWoKaJZi2BHXbi6zSyGoAWVWRlUNWIbJqIysPWWWQlUVWI8iqgaxayKqLrASyyiKrkje3+ey5Zph8WofHJ3WYWtBhZlkbf12bk1va5B7Wpn9Pm8KUNqcb2hRXtSn1a1P+UZsz57Wp7B9BDtIE0QGEWBUh2UIIQgS/jRB4CEEGIbAIwQhC0DqUQAiy/zpyy183dC8K/VXrqvzSwYkHX7zTH+epW1dOb9zdUZfd13d08PzwL/Unv9zcee7E+9fMzM6dmf/7ibfevTjY+SByU6pVv6KnfO5xQRAVBDFBkBQEKUHgC4KcICgIgqIgKAuCiiAYEgSjgqAmCKYFQV38EwAA///v++jU7fDnXwAAAABJRU5ErkJggg=='
const BAD_ADLER =
  'iVBORw0KGgoAAAANSUhEUgAAAAMAAAACCAIAAAASFvFNAAAAIElEQVR42mJgYGdwFmFtUxRhlM9hduZldeblBwQAAP//FtkCnS1MShwAAAAASUVORK5CYII='
const BAD_ZLIB_HEADER =
  'iVBORw0KGgoAAAANSUhEUgAAAAMAAAACCAIAAAASFvFNAAAAIElEQVR422JgYGdwFmFtUxRhlM9hduZldeblBwQAAP//FtkCYg6pTUAAAAAASUVORK5CYII='
const INTERLACED =
  'iVBORw0KGgoAAAANSUhEUgAAAAUAAAAFCAIAAAF1CoEkAAAAYElEQVR4AQBQAK//AAAHAEMUBYYhFMkuLQw7UAEfbANDDQVDDQ9DDRlDDSMCH2UJH2UJH2UJH2UJH2UJAz7OFTG5CjG5D7G5FDE5GQQfZRUfDQUfDQ9DDRUfDRUBAAD//zxHDn3bMO76AAAAAElFTkSuQmCC'
const PALETTE =
  'iVBORw0KGgoAAAANSUhEUgAAAAIAAAACAQMAAABIeJ9nAAAABlBMVEUAAAD///+l2Z/dAAAAEElEQVR4nGJgYGAABAAA//8ABAAB8dAOyAAAAABJRU5ErkJggg=='
const RGBA16 =
  'iVBORw0KGgoAAAANSUhEUgAAAAIAAAACEAYAAAAiJtFnAAAAF0lEQVR4nGJSSWdgYGBoYMAJAAEAAP//H2oBDsRnQxIAAAAASUVORK5CYII='

const bytes = (base64: string): Uint8Array => Uint8Array.fromBase64(base64)

function pattern(x: number, y: number): number[] {
  return [(x * 67 + y * 31) & 255, (x * 13 + y * 101 + 7) & 255, (x * x * 5 + y * y * 3) & 255, ((x + 1) * (y + 3) * 37) & 255]
}

// The chunks of a PNG, as [type, data] pairs.
function chunks(png: Uint8Array): [string, Uint8Array][] {
  const view = new DataView(png.buffer, png.byteOffset, png.byteLength)
  const found: [string, Uint8Array][] = []
  for (let pos = 8; pos + 12 <= png.length; pos += 12 + view.getUint32(pos)) {
    found.push([String.fromCharCode(...png.subarray(pos + 4, pos + 8)), png.subarray(pos + 8, pos + 8 + view.getUint32(pos))])
  }
  return found
}

function zlibStream(png: Uint8Array): Uint8Array {
  const parts = chunks(png).flatMap(([type, data]) => (type === 'IDAT' ? [...data] : []))
  return Uint8Array.from(parts)
}

// The first deflate block's BTYPE (0 stored, 1 fixed, 2 dynamic): bits 1-2 of
// the byte after the 2-byte zlib header.
function firstBlockType(png: Uint8Array): number {
  return ((zlibStream(png)[2] ?? 0) >> 1) & 3
}

function reasonOf(result: PngResult): string {
  return result.kind === 'unsupported' ? result.reason : 'decoded'
}

function expectPattern(result: PngResult, width: number, height: number, channels: 3 | 4): void {
  expect(reasonOf(result)).toBe('decoded')
  if (result.kind !== 'decoded') return
  const { pixels } = result
  expect([pixels.width, pixels.height, pixels.channels]).toEqual([width, height, channels])
  const want: number[] = []
  for (let y = 0; y < height; y++) {
    for (let x = 0; x < width; x++) want.push(...pattern(x, y).slice(0, channels))
  }
  expect([...pixels.data]).toEqual(want)
}

test('decodes RGB from a stored block, every filter type', () => {
  const png = bytes(RGB_STORED)
  expect(firstBlockType(png)).toBe(0)
  expectPattern(decodePng(png), 5, 5, 3)
})

test('decodes RGBA from a fixed-Huffman block, every filter type', () => {
  const png = bytes(RGBA_FIXED)
  expect(firstBlockType(png)).toBe(1)
  expectPattern(decodePng(png), 5, 5, 4)
})

test('decodes RGB from dynamic-Huffman blocks split over two IDAT chunks', () => {
  const png = bytes(RGB_DYNAMIC)
  expect(firstBlockType(png)).toBe(2)
  expect(chunks(png).filter(([type]) => type === 'IDAT')).toHaveLength(2)
  expectPattern(decodePng(png), 48, 32, 3)
})

test('decodes RGBA from a dynamic-Huffman block', () => {
  const png = bytes(RGBA_DYNAMIC)
  expect(firstBlockType(png)).toBe(2)
  expectPattern(decodePng(png), 16, 12, 4)
})

test('a chunk with a bad CRC is unsupported', () => {
  const png = bytes(RGB_STORED)
  // Byte 45 is inside the IDAT data: 8 signature + 25 IHDR + 8 IDAT header, then 4 in.
  png[45] = (png[45] ?? 0) ^ 0x10
  expect(reasonOf(decodePng(png))).toMatch(/IDAT chunk fails its CRC check/)
})

test('a bad Adler-32 or zlib header is unsupported', () => {
  expect(reasonOf(decodePng(bytes(BAD_ADLER)))).toMatch(/Adler-32/)
  expect(reasonOf(decodePng(bytes(BAD_ZLIB_HEADER)))).toMatch(/zlib header/)
})

test('PNGs other than 8-bit RGB or RGBA, non-interlaced, are unsupported', () => {
  expect(reasonOf(decodePng(bytes(PALETTE)))).toMatch(/colour type 3/)
  expect(reasonOf(decodePng(bytes(RGBA16)))).toMatch(/bit depth 16/)
  expect(reasonOf(decodePng(bytes(INTERLACED)))).toMatch(/interlaced/)
})

test('a file that is not a whole PNG is unsupported', () => {
  expect(reasonOf(decodePng(new Uint8Array(0)))).toMatch(/not a PNG/)
  expect(reasonOf(decodePng(Uint8Array.of(1, 2, 3, 4, 5, 6, 7, 8, 9)))).toMatch(/not a PNG/)
  expect(reasonOf(decodePng(bytes(RGB_DYNAMIC).subarray(0, 1000)))).toMatch(/past the end of the file/)
  expect(reasonOf(decodePng(bytes(RGB_STORED).subarray(0, 33)))).toMatch(/before its IEND/)
})

test('a damaged file never throws: every single-byte change gives a result', () => {
  for (const fixture of [RGB_STORED, RGBA_FIXED]) {
    const original = bytes(fixture)
    for (let i = 0; i < original.length; i++) {
      const png = original.slice()
      png[i] = (png[i] ?? 0) ^ 0xa5
      expect(['decoded', 'unsupported']).toContain(decodePng(png).kind)
    }
  }
})

test('a damaged deflate stream fails as a PngError, never another error', () => {
  for (const fixture of [RGBA_FIXED, RGB_DYNAMIC, RGBA_DYNAMIC]) {
    const png = bytes(fixture)
    const stream = zlibStream(png)
    const [, header] = chunks(png)[0] ?? []
    const view = new DataView((header ?? new Uint8Array(13)).buffer, header?.byteOffset ?? 0, 13)
    const channels = view.getUint8(9) === 6 ? 4 : 3
    const length = view.getUint32(4) * (1 + view.getUint32(0) * channels)
    expect(inflate(stream, length)).toHaveLength(length)
    for (let i = 0; i < stream.length; i++) {
      for (const flip of [0x01, 0x10, 0x80]) {
        const damaged = stream.slice()
        damaged[i] = (damaged[i] ?? 0) ^ flip
        try {
          inflate(damaged, length)
        } catch (error) {
          expect(error instanceof PngError).toBe(true)
        }
      }
    }
  }
})
