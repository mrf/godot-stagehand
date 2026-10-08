// The godot-stagehand mod (docs/design/claude-code-plugin.md, D7): a status
// band above the prompt and a /stagehand-view pane with the last game frame.
//
// It only observes. The tool.call hook passes every call on unchanged and
// reads the results of Claude's own Stagehand calls; it never calls the server
// itself, because $.mcp.call fires tool.call again and, inside a turn, needs a
// permission grant (design Q7). Only the pane's buttons call the server, from
// outside a turn. The MCP server and the skill work without this mod.
import type { EngineInterface, On } from 'claude-code'
import { decodePng, type Pixels } from './png.js'
import { encodeCells, halfBlockCells, rasterGrid, thumbnail } from './raster.js'

const SERVER = 'plugin:godot-stagehand:stagehand'
const TOOL_PREFIX = 'mcp__plugin_godot-stagehand_stagehand__'
const PANE_ID = 'stagehand-view'
// The pane's frame element, an Image or a Raster; the key the probe blits.
const FRAME_KEY = 'frame'
// The Image element takes PNGs of at most 2 MiB decoded.
const IMAGE_LIMIT_BYTES = 2 * 1024 * 1024
// godot_connect's defaults, for a successful call that left them out.
const DEFAULT_INSTANCE = 'default'
const DEFAULT_HOST = '127.0.0.1'
const DEFAULT_PORT = 26700

interface Instance {
  id: string
  state: string
  host: string
  port: number
}

interface Frame {
  png: string
  bytes: number
  width: number
  height: number
  capturedAt: Date
  // At most 512 px on the long side, for a Raster; the full decode is dropped.
  // Undefined when the decoder rejected the frame or no Raster can need it.
  thumbnail: Pixels | undefined
  // The Raster last drawn from it, kept until the pane's size changes.
  raster?: { columns: number; rows: number; cells: string }
  // $.ui.blit probes made while it was the frame drawn.
  probes: number
}

// State lives in module variables: the band rebuilds from the next Stagehand
// call, so nothing has to survive a reload.
const instances = new Map<string, Instance>()
let frame: Frame | undefined
let paneError: string | undefined

// How the pane draws a frame on the terminal (design D7, "Frame fallback").
// `Image` draws pixels only where the terminal can (kitty, Ghostty) and its alt
// elsewhere, such as inside tmux. Which one it does is known only once the
// Image is mounted: $.ui.blit with the same source then answers {} or a deny
// naming the alt. So the pane starts `probing`, settles on `image` for the
// session when a blit is taken, and on `raster` (half blocks) when one is
// denied. A refused call leaves it on `image`, as the pane drew before.
let frameMode: 'probing' | 'image' | 'raster' = 'probing'
let probeInFlight = false
// A blit made in the same pass as the drawing finds nothing mounted yet: on
// 2.1.288 it answers 'no Image of its own is mounted under key "frame" in
// stagehand-view', and a blit from the next drawing gets the real answer about
// 25 ms later. That deny alone means "ask again".
const NOT_MOUNTED = 'is mounted under key'
const MAX_PROBES = 8
// Under a frame drawn as blocks. Claude Code draws pictures in kitty and
// Ghostty; inside tmux it never does (README, "Claude Code").
const BLOCKS_WHY =
  "Blocks, because this terminal can't show pictures here. kitty or Ghostty, outside tmux, show the full frame."

// ── Reading tool results ────────────────────────────────────────────────────

function asRecord(value: unknown): Readonly<Record<string, unknown>> | undefined {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
    ? (value as Readonly<Record<string, unknown>>)
    : undefined
}

function stringField(record: Readonly<Record<string, unknown>> | undefined, key: string): string | undefined {
  const value = record?.[key]
  return typeof value === 'string' ? value : undefined
}

function numberField(record: Readonly<Record<string, unknown>> | undefined, key: string): number | undefined {
  const value = record?.[key]
  return typeof value === 'number' && Number.isFinite(value) ? value : undefined
}

function parseJSON(text: string | undefined): Readonly<Record<string, unknown>> | undefined {
  if (text === undefined) return undefined
  try {
    return asRecord(JSON.parse(text))
  } catch {
    // Not every result is JSON (godot_connect answers in prose); not ours to read.
    return undefined
  }
}

// What a successful call left for the model: its text, and its content blocks.
interface Observed {
  text: string | undefined
  blocks: readonly unknown[]
}

// A tool.call result as core gives it: `{ result, text }`, or `isError` / `deny`.
function fromToolCall(result: unknown): Observed | undefined {
  const record = asRecord(result)
  if (record === undefined || record['isError'] === true || record['deny'] !== undefined) return undefined
  const inner = record['result']
  const text = stringField(record, 'text') ?? (typeof inner === 'string' ? inner : undefined)
  return { text, blocks: Array.isArray(inner) ? inner : [] }
}

// A $.mcp.call result: `{ content, isError }`.
function fromMcpCall(result: unknown): Observed | undefined {
  const record = asRecord(result)
  if (record === undefined || record['isError'] === true) return undefined
  const content = record['content']
  const blocks: readonly unknown[] = Array.isArray(content) ? content : []
  const texts = blocks.flatMap((block) => {
    const text = stringField(asRecord(block), 'text')
    return text === undefined ? [] : [text]
  })
  return { text: texts.length > 0 ? texts.join('\n') : undefined, blocks }
}

function replaceInstances(list: unknown, stateOf: (item: Readonly<Record<string, unknown>>) => string | undefined): void {
  if (!Array.isArray(list)) return
  instances.clear()
  for (const entry of list) {
    const item = asRecord(entry)
    const id = stringField(item, 'id')
    const host = stringField(item, 'host')
    const port = numberField(item, 'port')
    const state = item === undefined ? undefined : stateOf(item)
    if (id !== undefined && host !== undefined && port !== undefined && state !== undefined) {
      instances.set(id, { id, state, host, port })
    }
  }
}

// PNG width and height from the IHDR chunk: bytes 16-23 of the file, which
// are base64 characters 0-31.
function pngSize(png: string): { width: number; height: number } {
  const alphabet = 'ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/'
  const bytes: number[] = []
  for (let i = 0; i + 3 < 32 && i + 3 < png.length; i += 4) {
    const n = [0, 1, 2, 3].reduce((acc, k) => acc * 64 + Math.max(0, alphabet.indexOf(png.charAt(i + k))), 0)
    bytes.push((n >> 16) & 0xff, (n >> 8) & 0xff, n & 0xff)
  }
  const word = (at: number): number =>
    ((bytes[at] ?? 0) * 0x1000000) + ((bytes[at + 1] ?? 0) << 16) + ((bytes[at + 2] ?? 0) << 8) + (bytes[at + 3] ?? 0)
  return { width: word(16), height: word(20) }
}

function decodedLength(base64: string): number {
  const padding = base64.endsWith('==') ? 2 : base64.endsWith('=') ? 1 : 0
  return Math.floor((base64.length * 3) / 4) - padding
}

// Decodes a frame once, as it arrives, never while drawing. Not when it is
// over the Image limit, which keeps the text summary, nor once the terminal
// has drawn an Image, since no Raster will be drawn then.
function thumbnailOf(png: string, bytes: number): Pixels | undefined {
  if (bytes > IMAGE_LIMIT_BYTES || frameMode === 'image') return undefined
  try {
    const decoded = decodePng(Uint8Array.fromBase64(png))
    return decoded.kind === 'decoded' ? thumbnail(decoded.pixels) : undefined
  } catch {
    // Not base64, or out of memory: the frame keeps its text summary.
    return undefined
  }
}

function frameFrom(blocks: readonly unknown[]): Frame | undefined {
  for (const entry of blocks) {
    const block = asRecord(entry)
    if (stringField(block, 'type') !== 'image') continue
    // Claude Code hands MCP images on in the Messages API shape
    // ({ source: { type: 'base64', media_type, data } }); accept the MCP shape too.
    const source = asRecord(block?.['source'])
    const png = stringField(source, 'data') ?? stringField(block, 'data')
    const mime = stringField(source, 'media_type') ?? stringField(block, 'mimeType')
    if (png === undefined || mime !== 'image/png') continue
    const bytes = decodedLength(png)
    return { png, bytes, ...pngSize(png), capturedAt: new Date(), thumbnail: thumbnailOf(png, bytes), probes: 0 }
  }
  return undefined
}

// Updates the module state from one successful Stagehand call. Returns
// whether anything a drawing shows changed.
function observe(tool: string, args: Readonly<Record<string, unknown>>, observed: Observed): boolean {
  switch (tool) {
    case 'godot_status': {
      const report = parseJSON(observed.text)
      if (report === undefined) return false
      replaceInstances(report['instances'], (item) => stringField(item, 'state'))
      return true
    }
    case 'godot_list_instances': {
      const report = parseJSON(observed.text)
      if (report === undefined) return false
      replaceInstances(report['instances'], (item) =>
        item['connected'] === true ? 'connected' : 'disconnected',
      )
      return true
    }
    case 'godot_launch': {
      const launched = parseJSON(observed.text)
      const host = stringField(launched, 'host')
      const port = numberField(launched, 'port')
      if (host === undefined || port === undefined) return false
      const id = stringField(launched, 'instance_id') ?? DEFAULT_INSTANCE
      instances.set(id, { id, state: 'connected', host, port })
      return true
    }
    case 'godot_connect': {
      // godot_connect answers in prose, so read the call's own arguments.
      const id = stringField(args, 'instance_id') ?? DEFAULT_INSTANCE
      const host = stringField(args, 'host') ?? DEFAULT_HOST
      const port = numberField(args, 'port') ?? DEFAULT_PORT
      instances.set(id, { id, state: 'connected', host, port })
      return true
    }
    case 'godot_disconnect': {
      const id = stringField(args, 'instance_id')
      return id !== undefined && instances.delete(id)
    }
    case 'godot_screenshot': {
      const captured = frameFrom(observed.blocks)
      if (captured === undefined) return false
      frame = captured
      return true
    }
    default:
      return false
  }
}

// ── Drawing ─────────────────────────────────────────────────────────────────

function statusLine(): string | undefined {
  const connected = [...instances.values()].filter((instance) => instance.state === 'connected')
  const [only] = connected
  if (only === undefined) return undefined
  const address = (instance: Instance): string => `${instance.id} ${instance.host}:${String(instance.port)}`
  if (connected.length === 1) return `stagehand · ${address(only)} · connected`
  return `stagehand · ${String(connected.length)} connected · ${connected.map(address).join(', ')}`
}

function describeSize(bytes: number): string {
  return bytes >= 1024 * 1024 ? `${(bytes / (1024 * 1024)).toFixed(1)} MiB` : `${String(Math.ceil(bytes / 1024))} KiB`
}

function clock(date: Date): string {
  const two = (n: number): string => String(n).padStart(2, '0')
  return `${two(date.getHours())}:${two(date.getMinutes())}:${two(date.getSeconds())}`
}

// Terminal cells are about twice as tall as they are wide.
function imageCells(width: number, height: number, maxColumns: number, maxRows: number): { columns: number; rows: number } {
  const clamp = (n: number, max: number): number => Math.max(1, Math.min(255, max, Math.round(n)))
  const aspect = width > 0 && height > 0 ? height / width : 9 / 16
  let columns = clamp(maxColumns, maxColumns)
  let rows = clamp((columns * aspect) / 2, maxRows)
  if ((columns * aspect) / 2 > maxRows) columns = clamp((rows * 2) / aspect, maxColumns)
  rows = clamp(rows, maxRows)
  return { columns, rows }
}

// The Raster cells for a frame in a columns × rows grid, from its thumbnail.
function rasterOf(shown: Frame, small: Pixels, columns: number, rows: number): string {
  if (shown.raster?.columns !== columns || shown.raster.rows !== rows) {
    shown.raster = { columns, rows, cells: encodeCells(halfBlockCells(small, columns, rows)) }
  }
  return shown.raster.cells
}

// Asks whether the terminal drew the Image's pixels by blitting it the source
// it already has. Started while drawing and not awaited there; a deny for an
// Image not mounted yet asks again from the next drawing.
async function probeImage($: EngineInterface, drawn: Frame): Promise<void> {
  if (probeInFlight || drawn.probes >= MAX_PROBES) return
  probeInFlight = true
  drawn.probes++
  let deny: string | undefined
  try {
    const answer = await $.ui.blit({ requestId: PANE_ID, key: FRAME_KEY, source: { png: drawn.png } })
    deny = answer.deny
  } catch {
    // The call itself was refused (another mod, or no blit in this build):
    // keep the Image for this frame.
    drawn.probes = MAX_PROBES
    return
  } finally {
    probeInFlight = false
  }
  if (deny === undefined) {
    frameMode = 'image'
    return
  }
  if (!deny.includes(NOT_MOUNTED)) frameMode = 'raster'
  // Draw again: as a Raster, or as the Image with the next probe.
  $.ui.invalidate('ui.render')
}

// ── Pane buttons: the only server calls, made outside a turn ────────────────

async function callFromPane($: EngineInterface, tool: 'godot_screenshot' | 'godot_status'): Promise<void> {
  try {
    const observed = fromMcpCall(await $.mcp.call(SERVER, tool, {}))
    paneError = observed === undefined ? `${tool} failed; ask Claude to check the game.` : undefined
    if (observed !== undefined) observe(tool, {}, observed)
  } catch (error) {
    paneError = error instanceof Error ? error.message : String(error)
  }
  $.ui.invalidate('ui.render')
}

// ── Hooks ───────────────────────────────────────────────────────────────────

export function register(on: On): void {
  on('tool.call', async ($, e, next) => {
    const result = await next(e)
    if (e.tool.startsWith(TOOL_PREFIX)) {
      const observed = fromToolCall(result)
      const args = asRecord(e) ?? {}
      if (observed !== undefined && observe(e.tool.slice(TOOL_PREFIX.length), args, observed)) {
        $.ui.invalidate('ui.render')
      }
    }
    return result
  })

  on('ui.render', { component: 'AbovePrompt' }, async ($, e, next) => {
    const line = statusLine()
    if (line === undefined || e.props.hasSurvey) return next(e)
    const { Box, Text } = $.ui.resolve(e)
    // Keep what the mods after this one drew.
    const theirs = await next(e)
    return Box({
      flexDirection: 'column',
      children: [Text({ dimColor: true, wrap: 'truncate-end', children: [line] }), theirs],
    })
  })

  on('ui.render', { component: 'Pane' }, async ($, e, next) => {
    if (e.requestId !== PANE_ID) return next(e)
    const { Box, Text, Button } = $.ui.resolve(e)

    const rows = [
      Text({ bold: true, wrap: 'truncate-end', children: [statusLine() ?? 'stagehand · no game connected'] }),
    ]
    if (frame === undefined) {
      rows.push(Text({ dimColor: true, children: ['No frame yet. Press r, or ask Claude for a screenshot.'] }))
    } else {
      const summary = `${String(frame.width)}×${String(frame.height)} frame, ${describeSize(frame.bytes)}, captured ${clock(frame.capturedAt)}`
      const maxRows = Math.max(1, e.props.scroll.bodyRows - 4)
      const { thumbnail: small } = frame
      if (e.surface !== 'terminal' || frame.bytes > IMAGE_LIMIT_BYTES || (frameMode === 'raster' && small === undefined)) {
        const why =
          e.surface !== 'terminal'
            ? 'this app cannot draw it'
            : frame.bytes > IMAGE_LIMIT_BYTES
              ? 'too large to draw here'
              : 'this terminal cannot draw it'
        rows.push(Text({ children: [`Last ${summary}: ${why}; ask Claude for a screenshot to see it.`] }))
      } else if (frameMode === 'raster' && small !== undefined) {
        // Only the terminal's element table has Raster and Image (the Desktop app has neither).
        const { Raster } = $.ui.resolve(e)
        const grid = rasterGrid(small.width, small.height, e.props.bodyColumns, maxRows)
        rows.push(Raster({ key: FRAME_KEY, ...grid, cells: rasterOf(frame, small, grid.columns, grid.rows) }))
        rows.push(Text({ dimColor: true, children: [summary] }))
        rows.push(Text({ dimColor: true, children: [BLOCKS_WHY] }))
      } else {
        const { Image } = $.ui.resolve(e)
        const cells = imageCells(frame.width, frame.height, e.props.bodyColumns, maxRows)
        // The alt text stands in for the picture until the probe has switched
        // a terminal that cannot draw one to the Raster, right above the summary.
        rows.push(Image({ key: FRAME_KEY, source: { png: frame.png }, ...cells, alt: 'Last game frame' }))
        rows.push(Text({ dimColor: true, children: [summary] }))
        if (frameMode === 'probing') void probeImage($, frame)
      }
    }
    if (paneError !== undefined) rows.push(Text({ color: 'error', children: [paneError] }))
    rows.push(
      Box({
        flexDirection: 'row',
        columnGap: 3,
        children: [
          Button({
            key: 'refresh',
            label: 'Refresh frame',
            hotkey: 'r',
            plain: true,
            onPress: () => {
              void callFromPane($, 'godot_screenshot')
            },
          }),
          Button({
            key: 'status',
            label: 'Re-read status',
            hotkey: 's',
            plain: true,
            onPress: () => {
              void callFromPane($, 'godot_status')
            },
          }),
        ],
      }),
    )
    return Box({ flexDirection: 'column', children: rows })
  })

  on('command.run', { command: 'stagehand-view' }, async ($) => {
    await $.ui.open({ id: PANE_ID, title: 'Stagehand', focus: true, closeOnEscape: true })
    return {}
  })

  // Last, and guarded: $.command.register throws on a taken name, and the
  // band above must keep working when it does.
  on('session.start', async ($, e, next) => {
    try {
      await $.command.register({
        name: 'stagehand-view',
        description: 'Show the last game frame Claude captured and the Stagehand connection',
        immediate: true,
      })
    } catch {
      // Another plugin or the user owns /stagehand-view; the band still works.
    }
    return next(e)
  })
}
