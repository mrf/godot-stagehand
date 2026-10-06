// PNG decoding for the pane's frame fallback (docs/design/claude-code-plugin.md,
// D7 "Frame fallback"): 8-bit RGB and RGBA, non-interlaced, which is what
// Godot's save_png_to_buffer writes. A mod has no Node APIs and no
// DecompressionStream, so this carries its own zlib inflate (RFC 1950, 1951).
//
// decodePng never throws: any PNG it cannot read, or a damaged one, comes back
// as an `unsupported` result with the reason.

export interface Pixels {
  readonly width: number
  readonly height: number
  // Bytes per pixel: 3 for RGB, 4 for RGBA. Rows are packed, top row first.
  readonly channels: 3 | 4
  readonly data: Uint8Array
}

export type PngResult =
  | { readonly kind: 'decoded'; readonly pixels: Pixels }
  | { readonly kind: 'unsupported'; readonly reason: string }

// Why a PNG or zlib stream was refused. Only decodePng's callers see the text.
export class PngError extends Error {}

// The largest frame decoded, in pixels: a 3840×2160 screenshot fits.
export const MAX_PIXELS = 1 << 23

export function decodePng(png: Uint8Array): PngResult {
  try {
    return { kind: 'decoded', pixels: decode(png) }
  } catch (error) {
    const reason = error instanceof PngError ? error.message : `the decoder failed: ${String(error)}`
    return { kind: 'unsupported', reason }
  }
}

// ── PNG ─────────────────────────────────────────────────────────────────────

const SIGNATURE = [137, 80, 78, 71, 13, 10, 26, 10]

interface Header {
  width: number
  height: number
  channels: 3 | 4
}

function decode(png: Uint8Array): Pixels {
  if (png.length < SIGNATURE.length || SIGNATURE.some((byte, i) => png[i] !== byte)) {
    throw new PngError('not a PNG file')
  }
  const view = new DataView(png.buffer, png.byteOffset, png.byteLength)
  let header: Header | undefined
  const idat: Uint8Array[] = []
  for (let pos = SIGNATURE.length; ; ) {
    if (pos + 12 > png.length) throw new PngError('the PNG file ends before its IEND chunk')
    const length = view.getUint32(pos)
    const type = String.fromCharCode(...png.subarray(pos + 4, pos + 8))
    const end = pos + 8 + length
    if (end + 4 > png.length) throw new PngError(`the ${type} chunk runs past the end of the file`)
    if (crc32(png.subarray(pos + 4, end)) !== view.getUint32(end)) {
      throw new PngError(`the ${type} chunk fails its CRC check`)
    }
    const data = png.subarray(pos + 8, end)
    if (header === undefined) {
      if (type !== 'IHDR') throw new PngError('the PNG file does not start with IHDR')
      header = readHeader(data)
    } else if (type === 'IDAT') {
      idat.push(data)
    } else if (type === 'IEND') {
      return pixelsOf(header, concat(idat))
    } else if (type !== 'PLTE' && (type.charCodeAt(0) & 0x20) === 0) {
      // An unknown critical chunk changes how the image reads; PLTE in a
      // truecolour image is only a suggested palette.
      throw new PngError(`the PNG file has a critical ${type} chunk this decoder does not read`)
    }
    pos = end + 4
  }
}

function readHeader(data: Uint8Array): Header {
  if (data.length !== 13) throw new PngError('the IHDR chunk is not 13 bytes')
  const view = new DataView(data.buffer, data.byteOffset, data.byteLength)
  const width = view.getUint32(0)
  const height = view.getUint32(4)
  const [depth, colourType, compression, filter, interlace] = data.subarray(8)
  if (colourType !== 2 && colourType !== 6) {
    throw new PngError(`colour type ${String(colourType)} is not supported (8-bit RGB or RGBA only)`)
  }
  if (depth !== 8) throw new PngError(`bit depth ${String(depth)} is not supported (8-bit RGB or RGBA only)`)
  if (compression !== 0 || filter !== 0) throw new PngError('the PNG uses an unknown compression or filter method')
  if (interlace !== 0) throw new PngError('interlaced PNGs are not supported')
  if (width === 0 || height === 0) throw new PngError('the PNG has no pixels')
  if (width * height > MAX_PIXELS) {
    throw new PngError(`${String(width)}×${String(height)} is over the ${String(MAX_PIXELS)}-pixel limit`)
  }
  return { width, height, channels: colourType === 6 ? 4 : 3 }
}

function concat(parts: readonly Uint8Array[]): Uint8Array {
  const whole = new Uint8Array(parts.reduce((sum, part) => sum + part.length, 0))
  let at = 0
  for (const part of parts) {
    whole.set(part, at)
    at += part.length
  }
  return whole
}

// Undoes each row's filter (PNG spec, section 9). A filtered row is its filter
// type byte, then `stride` bytes.
function pixelsOf({ width, height, channels }: Header, stream: Uint8Array): Pixels {
  const stride = width * channels
  const raw = inflate(stream, height * (stride + 1))
  const data = new Uint8Array(stride * height)
  // The row above the first is all zeros.
  const zeros = new Uint8Array(stride)
  for (let y = 0; y < height; y++) {
    const filter = raw[y * (stride + 1)]
    const line = raw.subarray(y * (stride + 1) + 1, (y + 1) * (stride + 1))
    const row = data.subarray(y * stride, (y + 1) * stride)
    const up = y > 0 ? data.subarray((y - 1) * stride, y * stride) : zeros
    // Uint8Array stores wrap modulo 256, as the filters need.
    switch (filter) {
      case 0:
        row.set(line)
        break
      case 1:
        for (let i = 0; i < stride; i++) row[i] = (line[i] ?? 0) + (i >= channels ? (row[i - channels] ?? 0) : 0)
        break
      case 2:
        for (let i = 0; i < stride; i++) row[i] = (line[i] ?? 0) + (up[i] ?? 0)
        break
      case 3:
        for (let i = 0; i < stride; i++) {
          row[i] = (line[i] ?? 0) + (((i >= channels ? (row[i - channels] ?? 0) : 0) + (up[i] ?? 0)) >> 1)
        }
        break
      case 4:
        // Paeth, inlined: a call per byte costs about 20 times as much in the
        // mod worker (measured on 2.1.288: 700 ms against 35 ms for 6 MB).
        for (let i = 0; i < stride; i++) {
          const a = i >= channels ? (row[i - channels] ?? 0) : 0
          const b = up[i] ?? 0
          const c = i >= channels ? (up[i - channels] ?? 0) : 0
          // |p - a|, |p - b| and |p - c| for p = a + b - c.
          const pa = b > c ? b - c : c - b
          const pb = a > c ? a - c : c - a
          const pc = a + b > c + c ? a + b - c - c : c + c - a - b
          row[i] = (line[i] ?? 0) + (pa <= pb && pa <= pc ? a : pb <= pc ? b : c)
        }
        break
      default:
        throw new PngError(`row ${String(y)} has unknown filter type ${String(filter)}`)
    }
  }
  return { width, height, channels, data }
}

const CRC_TABLE = Uint32Array.from({ length: 256 }, (_, n) => {
  let c = n
  for (let k = 0; k < 8; k++) c = c & 1 ? 0xedb88320 ^ (c >>> 1) : c >>> 1
  return c >>> 0
})

function crc32(bytes: Uint8Array): number {
  let c = 0xffffffff
  for (const byte of bytes) c = (CRC_TABLE[(c ^ byte) & 0xff] ?? 0) ^ (c >>> 8)
  return (c ^ 0xffffffff) >>> 0
}

// ── zlib inflate ────────────────────────────────────────────────────────────

const LENGTH_BASE = [3, 4, 5, 6, 7, 8, 9, 10, 11, 13, 15, 17, 19, 23, 27, 31, 35, 43, 51, 59, 67, 83, 99, 115, 131, 163, 195, 227, 258]
const LENGTH_EXTRA = [0, 0, 0, 0, 0, 0, 0, 0, 1, 1, 1, 1, 2, 2, 2, 2, 3, 3, 3, 3, 4, 4, 4, 4, 5, 5, 5, 5, 0]
const DISTANCE_BASE = [
  1, 2, 3, 4, 5, 7, 9, 13, 17, 25, 33, 49, 65, 97, 129, 193, 257, 385, 513, 769, 1025, 1537, 2049, 3073, 4097, 6145,
  8193, 12289, 16385, 24577,
]
const DISTANCE_EXTRA = [0, 0, 0, 0, 1, 1, 2, 2, 3, 3, 4, 4, 5, 5, 6, 6, 7, 7, 8, 8, 9, 9, 10, 10, 11, 11, 12, 12, 13, 13]
// The order a dynamic block lists its code-length code lengths in.
const CODE_LENGTH_ORDER = [16, 17, 18, 0, 8, 7, 9, 6, 10, 5, 11, 4, 12, 3, 13, 2, 14, 1, 15]
const END_OF_BLOCK = 256

// A canonical Huffman code as a lookup table indexed by the next `bits` input
// bits: each entry is `symbol << 4 | code length`, and 0 where no code is.
interface Huffman {
  table: Uint32Array
  bits: number
}

function huffman(lengths: Uint8Array): Huffman {
  const counts = new Uint16Array(16)
  let bits = 0
  for (const length of lengths) {
    counts[length] = (counts[length] ?? 0) + 1
    bits = Math.max(bits, length)
  }
  let left = 1
  for (let length = 1; length <= 15; length++) {
    left = left * 2 - (counts[length] ?? 0)
    if (left < 0) throw new PngError('the zlib stream has an over-subscribed Huffman code')
  }
  const next = new Uint16Array(16)
  for (let length = 1; length < 15; length++) next[length + 1] = ((next[length] ?? 0) + (counts[length] ?? 0)) << 1
  const table = new Uint32Array(1 << bits)
  lengths.forEach((length, symbol) => {
    if (length === 0) return
    const code = next[length] ?? 0
    next[length] = code + 1
    // Deflate packs codes from their most significant bit, and the reader
    // takes bits least significant first, so the table is indexed reversed.
    let reversed = 0
    for (let k = 0; k < length; k++) reversed |= ((code >> k) & 1) << (length - 1 - k)
    for (let index = reversed; index < table.length; index += 1 << length) table[index] = (symbol << 4) | length
  })
  return { table, bits }
}

let fixed: [Huffman, Huffman] | undefined

function fixedCodes(): [Huffman, Huffman] {
  if (fixed === undefined) {
    const lengths = new Uint8Array(288)
    lengths.fill(8, 0, 144)
    lengths.fill(9, 144, 256)
    lengths.fill(7, 256, 280)
    lengths.fill(8, 280, 288)
    fixed = [huffman(lengths), huffman(new Uint8Array(30).fill(5))]
  }
  return fixed
}

// Reads a deflate stream's bits, least significant first. Past the end it
// reads zeros and throws as soon as one of them is used.
class BitReader {
  private pos: number
  private buffer = 0
  private count = 0
  private padding = 0

  constructor(
    private readonly bytes: Uint8Array,
    start: number,
  ) {
    this.pos = start
  }

  read(n: number): number {
    this.fill(n)
    const value = this.buffer & ((1 << n) - 1)
    this.drop(n)
    return value
  }

  symbol({ table, bits }: Huffman): number {
    this.fill(bits)
    const entry = table[this.buffer & (table.length - 1)] ?? 0
    const length = entry & 15
    if (length === 0) throw new PngError('the zlib stream has an invalid Huffman code')
    this.drop(length)
    return entry >>> 4
  }

  // Skips to the next byte boundary.
  align(): void {
    this.drop(this.count & 7)
  }

  private fill(n: number): void {
    while (this.count < n) {
      if (this.pos < this.bytes.length) {
        this.buffer |= (this.bytes[this.pos] ?? 0) << this.count
        this.pos++
      } else {
        this.padding++
      }
      this.count += 8
    }
  }

  private drop(n: number): void {
    this.buffer >>>= n
    this.count -= n
    // The padding is the top of the buffer: it has been read once fewer bits
    // are left than it holds.
    if (this.count < this.padding * 8) throw new PngError('the zlib stream ends early')
  }
}

// Inflates a zlib stream that must hold exactly `length` bytes.
export function inflate(stream: Uint8Array, length: number): Uint8Array {
  const [cmf = 0, flg = 0] = stream
  if ((cmf & 15) !== 8 || cmf >> 4 > 7 || ((cmf << 8) | flg) % 31 !== 0) throw new PngError('bad zlib header')
  if (flg & 0x20) throw new PngError('the zlib stream asks for a preset dictionary')
  const out = new Uint8Array(length)
  let at = 0
  const bits = new BitReader(stream, 2)
  for (let last = false; !last; ) {
    last = bits.read(1) === 1
    const type = bits.read(2)
    if (type === 0) {
      bits.align()
      const size = bits.read(16)
      if ((size ^ 0xffff) !== bits.read(16)) throw new PngError('a stored zlib block has a bad length')
      if (at + size > length) throw new PngError('the zlib stream holds more data than the image')
      for (let k = 0; k < size; k++) out[at++] = bits.read(8)
      continue
    }
    if (type === 3) throw new PngError('the zlib stream has a reserved block type')
    const [literals, distances] = type === 1 ? fixedCodes() : dynamicCodes(bits)
    for (;;) {
      const symbol = bits.symbol(literals)
      if (symbol < END_OF_BLOCK) {
        if (at >= length) throw new PngError('the zlib stream holds more data than the image')
        out[at++] = symbol
        continue
      }
      if (symbol === END_OF_BLOCK) break
      // A length code, its extra bits, then a distance code and its extra bits.
      const lengthBase = LENGTH_BASE[symbol - 257]
      if (lengthBase === undefined) throw new PngError('the zlib stream has an invalid length code')
      const size = lengthBase + bits.read(LENGTH_EXTRA[symbol - 257] ?? 0)
      const distanceCode = bits.symbol(distances)
      const distanceBase = DISTANCE_BASE[distanceCode]
      if (distanceBase === undefined) throw new PngError('the zlib stream has an invalid distance code')
      const distance = distanceBase + bits.read(DISTANCE_EXTRA[distanceCode] ?? 0)
      if (distance > at) throw new PngError('a zlib back-reference reaches before the start')
      if (at + size > length) throw new PngError('the zlib stream holds more data than the image')
      if (distance >= size) {
        out.copyWithin(at, at - distance, at - distance + size)
        at += size
      } else {
        // Overlapping: byte by byte repeats the last `distance` bytes.
        for (const end = at + size; at < end; at++) out[at] = out[at - distance] ?? 0
      }
    }
  }
  if (at !== length) throw new PngError(`the image data is ${String(at)} bytes, not ${String(length)}`)
  bits.align()
  const adler = ((bits.read(8) << 24) | (bits.read(8) << 16) | (bits.read(8) << 8) | bits.read(8)) >>> 0
  if (adler !== adler32(out)) throw new PngError('the zlib stream fails its Adler-32 check')
  return out
}

function dynamicCodes(bits: BitReader): [Huffman, Huffman] {
  const literalCount = bits.read(5) + 257
  const distanceCount = bits.read(5) + 1
  const codeLengthCount = bits.read(4) + 4
  if (literalCount > 286 || distanceCount > 30) throw new PngError('a zlib block declares too many codes')
  const codeLengths = new Uint8Array(19)
  for (let i = 0; i < codeLengthCount; i++) codeLengths[CODE_LENGTH_ORDER[i] ?? 0] = bits.read(3)
  const codeLengthCode = huffman(codeLengths)
  const lengths = new Uint8Array(literalCount + distanceCount)
  for (let i = 0; i < lengths.length; ) {
    const symbol = bits.symbol(codeLengthCode)
    if (symbol < 16) {
      lengths[i++] = symbol
      continue
    }
    if (symbol === 16 && i === 0) throw new PngError('a zlib block repeats a code length before the first')
    const value = symbol === 16 ? (lengths[i - 1] ?? 0) : 0
    const repeat = symbol === 16 ? 3 + bits.read(2) : symbol === 17 ? 3 + bits.read(3) : 11 + bits.read(7)
    if (i + repeat > lengths.length) throw new PngError('a zlib block repeats past its code lengths')
    lengths.fill(value, i, i + repeat)
    i += repeat
  }
  if (lengths[END_OF_BLOCK] === 0) throw new PngError('a zlib block has no end-of-block code')
  return [huffman(lengths.subarray(0, literalCount)), huffman(lengths.subarray(literalCount))]
}

function adler32(bytes: Uint8Array): number {
  let a = 1
  let b = 0
  // 5552 bytes is the most that can be summed before the sums need reducing.
  for (let start = 0; start < bytes.length; start += 5552) {
    const end = Math.min(start + 5552, bytes.length)
    for (let i = start; i < end; i++) {
      a += bytes[i] ?? 0
      b += a
    }
    a %= 65521
    b %= 65521
  }
  return ((b << 16) | a) >>> 0
}
