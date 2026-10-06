// Tests for the frame fallback's downscale maths (raster.ts), D7 in
// docs/design/claude-code-plugin.md. Run with `claude plugin test
// integrations/claude-code`.
import { expect, test } from 'claude-code/testing'
import type { Pixels } from './png.js'
import { encodeCells, halfBlockCells, rasterGrid, thumbnail } from './raster.js'

const UPPER_HALF = 0x2580
const SPACE = 0x20
const DEFAULT = 0x01000000
const BLANK = [SPACE, DEFAULT, DEFAULT]

// An RGB image whose pixel (x, y) is colourAt(x, y), as 0xRRGGBB.
function image(width: number, height: number, colourAt: (x: number, y: number) => number): Pixels {
  const data = new Uint8Array(width * height * 3)
  for (let y = 0; y < height; y++) {
    for (let x = 0; x < width; x++) {
      const colour = colourAt(x, y)
      data.set([(colour >> 16) & 255, (colour >> 8) & 255, colour & 255], (y * width + x) * 3)
    }
  }
  return { width, height, channels: 3, data }
}

const solid = (width: number, height: number): Pixels => image(width, height, () => 0x336699)

function cellsOf(words: Uint32Array): number[][] {
  const cells: number[][] = []
  for (let i = 0; i < words.length; i += 3) cells.push([...words.subarray(i, i + 3)])
  return cells
}

test('the thumbnail keeps at most 512 px on the long side, aspect ratio kept', () => {
  const size = (pixels: Pixels): number[] => {
    const small = thumbnail(pixels)
    return [small.width, small.height, small.channels]
  }
  expect(size(solid(1024, 10))).toEqual([512, 5, 3])
  expect(size(solid(600, 1200))).toEqual([256, 512, 3])
  expect(size(solid(1152, 648))).toEqual([512, 288, 3])
  // Never scaled up.
  expect(size(solid(300, 200))).toEqual([300, 200, 3])
})

test('the thumbnail composites RGBA over black', () => {
  const pixels: Pixels = { width: 1, height: 1, channels: 4, data: Uint8Array.of(255, 0, 0, 128) }
  expect([...thumbnail(pixels).data]).toEqual([128, 0, 0])
})

test('the box filter averages every source pixel under an output pixel', () => {
  const black = 0x000000
  const white = 0xffffff
  const pixels = image(4, 2, (x) => (x % 2 === 0 ? black : white))
  // 4×2 down to 2×1: each output pixel is two black and two white pixels.
  expect([...thumbnail(pixels, 2).data]).toEqual([128, 128, 128, 128, 128, 128])
})

test('a cell is ▀ with the top pixel as foreground and the bottom pixel as background', () => {
  // Four 2×2 quadrants, filtered down to a 2×2-pixel grid of one row.
  const colours = [0xff0000, 0x00ff00, 0x0000ff, 0xffff00]
  const quadrants = image(4, 4, (x, y) => colours[(y < 2 ? 0 : 2) + (x < 2 ? 0 : 1)] ?? 0)
  expect(cellsOf(halfBlockCells(quadrants, 2, 1))).toEqual([
    [UPPER_HALF, 0xff0000, 0x0000ff],
    [UPPER_HALF, 0x00ff00, 0xffff00],
  ])
})

test('a wide frame is centred vertically in whole cells, the rest blank', () => {
  const cells = cellsOf(halfBlockCells(solid(8, 2), 4, 3))
  expect(cells.slice(0, 4)).toEqual([BLANK, BLANK, BLANK, BLANK])
  expect(cells.slice(4, 8).every(([glyph]) => glyph === UPPER_HALF)).toBe(true)
  expect(cells.slice(8)).toEqual([BLANK, BLANK, BLANK, BLANK])
})

test('a tall frame is centred horizontally', () => {
  const cells = cellsOf(halfBlockCells(solid(2, 8), 4, 2))
  for (const row of [0, 1]) {
    expect(cells.slice(row * 4, row * 4 + 4).map(([glyph]) => glyph)).toEqual([SPACE, UPPER_HALF, SPACE, SPACE])
  }
  expect(cells[1]).toEqual([UPPER_HALF, 0x336699, 0x336699])
})

test('the pane grid takes the full width and the rows it needs, within the limits', () => {
  // 1152×648 at 80 columns is 80×45 square pixels: 23 rows of two.
  expect(rasterGrid(1152, 648, 80, 26)).toEqual({ columns: 80, rows: 23 })
  expect(rasterGrid(1152, 648, 80, 10)).toEqual({ columns: 80, rows: 10 })
  // A Raster is at most 512 columns by 256 rows, and at least one cell.
  expect(rasterGrid(100, 100, 900, 900)).toEqual({ columns: 512, rows: 256 })
  expect(rasterGrid(100, 100, 0, 0)).toEqual({ columns: 1, rows: 1 })
})

test('cells encode as padded base64 of little-endian u32 triplets', () => {
  const encoded = encodeCells(Uint32Array.of(UPPER_HALF, 0xff8800, DEFAULT))
  expect(encoded).toBe(Uint8Array.of(0x80, 0x25, 0, 0, 0x00, 0x88, 0xff, 0, 0, 0, 0, 1).toBase64())
})
