// Half-block drawing for the pane's frame fallback (docs/design/claude-code-plugin.md,
// D7 "Frame fallback"). A frame is box-filtered to a cell grid, two pixels per
// cell: each cell is ▀ with the top pixel as its foreground and the bottom
// pixel as its background. A cell is about twice as tall as it is wide, so the
// two pixels are about square.
import type { Pixels } from './png.js'

// The thumbnail kept per frame, so a resized pane redraws without the full frame.
export const THUMBNAIL_SIDE = 512
// The largest Raster Claude Code draws.
const MAX_COLUMNS = 512
const MAX_ROWS = 256
const UPPER_HALF = 0x2580
const SPACE = 0x20
// A Raster colour that means the terminal's own default.
const DEFAULT_COLOUR = 0x01000000

const clamp = (n: number, low: number, high: number): number => Math.min(high, Math.max(low, n))

// RGB pixels of `source` box-filtered to width × height: each output pixel is
// the mean of the source pixels it covers, RGBA composited over black first.
function resample(source: Pixels, width: number, height: number): Uint8Array {
  const { channels, data } = source
  const span = (i: number, from: number, to: number): [number, number] => {
    const start = Math.floor((i * from) / to)
    return [start, Math.max(start + 1, Math.floor(((i + 1) * from) / to))]
  }
  const columns = Array.from({ length: width }, (_, ox) => span(ox, source.width, width))
  const out = new Uint8Array(width * height * 3)
  let o = 0
  for (let oy = 0; oy < height; oy++) {
    const [y0, y1] = span(oy, source.height, height)
    for (const [x0, x1] of columns) {
      let r = 0
      let g = 0
      let b = 0
      for (let y = y0; y < y1; y++) {
        const end = (y * source.width + x1) * channels
        for (let at = (y * source.width + x0) * channels; at < end; at += channels) {
          const alpha = channels === 4 ? (data[at + 3] ?? 0) : 255
          r += (data[at] ?? 0) * alpha
          g += (data[at + 1] ?? 0) * alpha
          b += (data[at + 2] ?? 0) * alpha
        }
      }
      const total = (y1 - y0) * (x1 - x0) * 255
      out[o++] = Math.round(r / total)
      out[o++] = Math.round(g / total)
      out[o++] = Math.round(b / total)
    }
  }
  return out
}

// The frame at most `side` pixels on its long side, aspect ratio kept, as RGB.
export function thumbnail(pixels: Pixels, side = THUMBNAIL_SIDE): Pixels {
  const scale = Math.min(1, side / Math.max(pixels.width, pixels.height))
  const width = Math.max(1, Math.round(pixels.width * scale))
  const height = Math.max(1, Math.round(pixels.height * scale))
  return { width, height, channels: 3, data: resample(pixels, width, height) }
}

// The cell grid the pane draws a width × height frame in: all the columns it
// may use, and as many rows as that width needs, within what it may use.
export function rasterGrid(
  width: number,
  height: number,
  maxColumns: number,
  maxRows: number,
): { columns: number; rows: number } {
  const columns = clamp(maxColumns, 1, MAX_COLUMNS)
  const rows = clamp(Math.ceil((columns * height) / width / 2), 1, clamp(maxRows, 1, MAX_ROWS))
  return { columns, rows }
}

// Every cell of a columns × rows Raster, row-major [codePoint, foreground,
// background] triplets: the image fitted with its aspect ratio kept and
// centred in whole cells. The cells around it are blank.
export function halfBlockCells(image: Pixels, columns: number, rows: number): Uint32Array {
  const scale = Math.min(columns / image.width, (rows * 2) / image.height)
  const width = clamp(Math.round(image.width * scale), 1, columns)
  const height = clamp(Math.round((image.height * scale) / 2), 1, rows) * 2
  const rgb = resample(image, width, height)
  const left = Math.floor((columns - width) / 2)
  const top = Math.floor((rows - height / 2) / 2)
  const colour = (x: number, y: number): number => {
    const at = (y * width + x) * 3
    return ((rgb[at] ?? 0) << 16) | ((rgb[at + 1] ?? 0) << 8) | (rgb[at + 2] ?? 0)
  }
  const words = new Uint32Array(columns * rows * 3)
  for (let row = 0; row < rows; row++) {
    for (let column = 0; column < columns; column++) {
      const x = column - left
      const y = (row - top) * 2
      const inside = x >= 0 && x < width && y >= 0 && y < height
      words.set(
        inside ? [UPPER_HALF, colour(x, y), colour(x, y + 1)] : [SPACE, DEFAULT_COLOUR, DEFAULT_COLOUR],
        (row * columns + column) * 3,
      )
    }
  }
  return words
}

// A Raster's `cells`: padded base64 of the triplets as little-endian u32s.
export function encodeCells(words: Uint32Array): string {
  const bytes = new Uint8Array(words.length * 4)
  const view = new DataView(bytes.buffer)
  words.forEach((word, i) => {
    view.setUint32(i * 4, word, true)
  })
  return bytes.toBase64()
}
