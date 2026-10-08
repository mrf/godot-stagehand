// Tests for the godot-stagehand mod (docs/design/claude-code-plugin.md, D7).
// Run with `claude plugin test integrations/claude-code`.
import type { RenderElement } from 'claude-code'
import { expect, test, type FoundElement } from 'claude-code/testing'

const PLUGIN = 'godot-stagehand'
const TOOL = 'mcp__plugin_godot-stagehand_stagehand__'
const SERVER = 'plugin:godot-stagehand:stagehand'

// A 1x1 PNG.
const PNG_1X1 =
  'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNkYPhfDwAChwGA60e6kgAAAABJRU5ErkJggg=='

const CONNECTED_STATUS = JSON.stringify({
  instances: [{ id: 'default', state: 'connected', host: '127.0.0.1', port: 26700, pid: 42, launched: true }],
  note: 'Each MCP client runs its own godot-stagehand process.',
})
const EMPTY_STATUS = JSON.stringify({ instances: [], hint: 'Use godot_connect…', note: '…' })

// What core answers a godot_status call with in Claude Code 2.1.288: the
// structured content as JSON text (design Q8).
const statusResult = (json: string) => ({ result: json, text: json })

const screenshotResult = (png: string) => ({
  result: [
    { type: 'image', source: { type: 'base64', media_type: 'image/png', data: png } },
    { type: 'text', text: '[Image: source: /tmp/frame.png]' },
  ],
  text: '[Image: source: /tmp/frame.png]',
})

const BAND = {
  plugin: PLUGIN,
  component: 'AbovePrompt',
  props: {
    hasSurvey: false,
    isWorking: true,
    maxRows: 10,
    bodyColumns: 100,
    scroll: { offset: 0, bodyRows: 10 },
    view: {},
  },
} as const

const PANE = {
  plugin: PLUGIN,
  component: 'Pane',
  requestId: 'stagehand-view',
  props: {
    title: 'Stagehand',
    isFocused: true,
    bodyColumns: 80,
    placement: 'inline',
    scroll: { offset: 0, bodyRows: 30 },
    view: {},
  },
} as const

// Stands for what later mods and Claude Code draw at a site.
const OTHERS: RenderElement = { type: 'Text', props: {}, children: ['drawn by others'] }

// The deny reasons Claude Code 2.1.288 gives: before the Image is mounted, and
// once it is mounted on a terminal that cannot draw pictures (design D7).
const NOT_MOUNTED = 'no Image of its own is mounted under key "frame" in stagehand-view'
const DRAWS_ALT =
  'the Image draws its alt here: the terminal draws no placeholder images (env: terminal=tmux, not asked yet, no answer)'

// The line under a frame drawn as blocks.
const BLOCKS_WHY = "Blocks, because this terminal can't show pictures here. kitty or Ghostty, outside tmux, show the full frame."

// PNG_1X1 with one byte of its image data changed, so its CRC check fails.
const PNG_DAMAGED = (() => {
  const png = Uint8Array.fromBase64(PNG_1X1)
  png[45] = (png[45] ?? 0) ^ 0xff
  return png.toBase64()
})()

// The pane probes with $.ui.blit after the drawing that made the Image, and
// that answer lands after the mount resolves; draw again until it has.
async function settle(ui: { redraw: () => Promise<void> }): Promise<void> {
  for (let i = 0; i < 5; i++) await ui.redraw()
}

// A Raster's cells as [codePoint, foreground, background] triplets.
function rasterCells(element: FoundElement | undefined): number[][] {
  const cells = element?.props['cells']
  const png = Uint8Array.fromBase64(typeof cells === 'string' ? cells : '')
  const view = new DataView(png.buffer, png.byteOffset, png.byteLength)
  const triplets: number[][] = []
  for (let at = 0; at + 12 <= png.length; at += 12) {
    triplets.push([view.getUint32(at, true), view.getUint32(at + 4, true), view.getUint32(at + 8, true)])
  }
  return triplets
}

test('the band draws nothing until an instance is connected', async ($, on) => {
  on('ui.render', () => OTHERS)
  on('tool.call', () => statusResult(EMPTY_STATUS))

  await $.tool.call({ tool: `${TOOL}godot_status` })
  const ui = await $.ui.mount({ ...BAND, surface: 'terminal' })
  expect(await ui.find({ type: 'Text', text: /stagehand/ })).toBeUndefined()
  expect(await ui.find({ text: 'drawn by others' })).toBeDefined()
})

test('the band shows the instance an observed godot_status reported', async ($, on) => {
  on('ui.render', () => OTHERS)
  on('tool.call', () => statusResult(CONNECTED_STATUS))

  await $.tool.call({ tool: `${TOOL}godot_status` })
  for (const surface of ['terminal', 'desktop'] as const) {
    const ui = await $.ui.mount({ ...BAND, surface })
    expect(await ui.find({ type: 'Text', text: 'stagehand · default 127.0.0.1:26700 · connected' })).toBeDefined()
    await ui.unmount()
  }
})

test('the band keeps what later mods drew', async ($, on) => {
  on('ui.render', () => OTHERS)
  on('tool.call', () => statusResult(CONNECTED_STATUS))

  await $.tool.call({ tool: `${TOOL}godot_status` })
  const ui = await $.ui.mount({ ...BAND, surface: 'terminal' })
  expect(await ui.find({ type: 'Text', text: /stagehand ·/ })).toBeDefined()
  expect(await ui.find({ text: 'drawn by others' })).toBeDefined()
})

test('the band counts several instances and follows connect and disconnect', async ($, on) => {
  on('ui.render', () => OTHERS)
  on('tool.call', (_$, e) => {
    if (e.tool === `${TOOL}godot_launch`) {
      const launched = JSON.stringify({ instance_id: 'default', pid: 7, host: '127.0.0.1', port: 41000 })
      return { result: launched, text: launched }
    }
    return { result: 'Connected to Godot', text: 'Connected to Godot' }
  })

  await $.tool.call({ tool: `${TOOL}godot_launch`, project_path: '/game' })
  await $.tool.call({ tool: `${TOOL}godot_connect`, instance_id: 'beta', port: 27000 })
  const ui = await $.ui.mount({ ...BAND, surface: 'terminal' })
  expect(
    await ui.find({ type: 'Text', text: 'stagehand · 2 connected · default 127.0.0.1:41000, beta 127.0.0.1:27000' }),
  ).toBeDefined()

  await $.tool.call({ tool: `${TOOL}godot_disconnect`, instance_id: 'default' })
  await ui.redraw()
  expect(await ui.find({ type: 'Text', text: 'stagehand · beta 127.0.0.1:27000 · connected' })).toBeDefined()
})

test('a failed tool call changes nothing', async ($, on) => {
  on('ui.render', () => OTHERS)
  on('tool.call', () => ({ isError: true, result: 'Error: refused', text: 'refused' }))

  await $.tool.call({ tool: `${TOOL}godot_connect`, port: 27000 })
  const ui = await $.ui.mount({ ...BAND, surface: 'terminal' })
  expect(await ui.find({ type: 'Text', text: /stagehand/ })).toBeUndefined()
})

test('every tool.call result passes through identical', async ($, on) => {
  const answers: Record<string, unknown> = {
    [`${TOOL}godot_status`]: statusResult(CONNECTED_STATUS),
    [`${TOOL}godot_screenshot`]: screenshotResult(PNG_1X1),
    [`${TOOL}godot_connect`]: { isError: true, result: 'Error: no', text: 'no' },
    Bash: { result: { stdout: 'ok', stderr: '', interrupted: false }, text: 'ok' },
  }
  on('tool.call', (_$, e) => answers[e.tool] as { result: unknown })

  expect(await $.tool.call({ tool: `${TOOL}godot_status` })).toEqual(answers[`${TOOL}godot_status`])
  expect(await $.tool.call({ tool: `${TOOL}godot_screenshot` })).toEqual(answers[`${TOOL}godot_screenshot`])
  expect(await $.tool.call({ tool: `${TOOL}godot_connect`, port: 1 })).toEqual(answers[`${TOOL}godot_connect`])
  expect(await $.tool.call({ tool: 'Bash', command: 'ls' })).toEqual(answers['Bash'])
})

test('the tool.call hook never calls the server', async ($, on) => {
  const calls: unknown[] = []
  on('mcp.call', (_$, e) => {
    calls.push(e)
    return { value: { content: [], isError: false } }
  })
  on('tool.call', () => statusResult(CONNECTED_STATUS))

  await $.tool.call({ tool: `${TOOL}godot_status` })
  await $.tool.call({ tool: `${TOOL}godot_click`, selector: 'Button' })
  expect(calls).toEqual([])
})

test('the pane draws the last frame as an Image on the terminal', async ($, on) => {
  on('tool.call', () => screenshotResult(PNG_1X1))

  await $.tool.call({ tool: `${TOOL}godot_screenshot` })
  const ui = await $.ui.mount({ ...PANE, surface: 'terminal' })
  const image = await ui.find({ type: 'Image' })
  expect(image).toBeDefined()
  expect(image?.props['source']).toEqual({ png: PNG_1X1 })
  // A terminal that cannot draw pictures (tmux) shows the alt text in the
  // Image's place, right above the summary line, so it must not repeat it.
  expect(image?.props['alt']).not.toMatch(/1×1 frame/)
})

test('the pane describes the frame in text on desktop', async ($, on) => {
  on('tool.call', () => screenshotResult(PNG_1X1))

  await $.tool.call({ tool: `${TOOL}godot_screenshot` })
  const ui = await $.ui.mount({ ...PANE, surface: 'desktop' })
  expect(await ui.find({ type: 'Image' })).toBeUndefined()
  expect(await ui.find({ type: 'Text', text: /1×1 frame/ })).toBeDefined()
  expect(await ui.find({ type: 'Text', text: /ask Claude for a screenshot/ })).toBeDefined()
})

test('the pane describes a frame over 2 MiB in text', async ($, on) => {
  const big = PNG_1X1.slice(0, 32) + 'A'.repeat(2_900_000)
  on('tool.call', () => screenshotResult(big))

  await $.tool.call({ tool: `${TOOL}godot_screenshot` })
  const ui = await $.ui.mount({ ...PANE, surface: 'terminal' })
  expect(await ui.find({ type: 'Image' })).toBeUndefined()
  expect(await ui.find({ type: 'Text', text: /too large to draw here/ })).toBeDefined()
})

test('a blit that finds the Image drawing its alt switches the pane to a Raster for the session', async ($, on) => {
  const blits: unknown[] = []
  on('ui.blit', (_$, e) => {
    blits.push(e)
    return { value: { deny: DRAWS_ALT } }
  })
  on('tool.call', () => screenshotResult(PNG_1X1))

  await $.tool.call({ tool: `${TOOL}godot_screenshot` })
  const ui = await $.ui.mount({ ...PANE, surface: 'terminal' })
  await settle(ui)
  // The probe swaps the drawn Image to the same source.
  expect(blits).toEqual([
    expect.objectContaining({ requestId: 'stagehand-view', key: 'frame', source: { png: PNG_1X1 } }),
  ])
  expect(await ui.find({ type: 'Image' })).toBeUndefined()
  const raster = await ui.find({ type: 'Raster' })
  expect(raster?.props).toMatchObject({ key: 'frame', columns: 80, rows: 26 })
  // PNG_1X1 is one RGBA pixel (0, 0, 255, 127): blue at half alpha, over black.
  const cells = rasterCells(raster)
  expect(cells).toHaveLength(80 * 26)
  expect(cells.filter(([glyph]) => glyph === 0x2580).every(([, top, bottom]) => top === 0x7f && bottom === 0x7f)).toBe(true)
  expect(cells.every(([glyph]) => glyph === 0x2580 || glyph === 0x20)).toBe(true)
  expect(await ui.find({ type: 'Text', text: /1×1 frame/ })).toBeDefined()
  // Says why the frame is blocks, and where the full picture shows.
  expect(await ui.find({ type: 'Text', text: BLOCKS_WHY })).toBeDefined()

  // The next frame is drawn as a Raster straight away, with no second probe.
  await $.tool.call({ tool: `${TOOL}godot_screenshot` })
  await settle(ui)
  expect(await ui.find({ type: 'Raster' })).toBeDefined()
  expect(blits).toHaveLength(1)
})

test('the Raster follows a resized pane from the kept thumbnail', async ($, on) => {
  on('ui.blit', () => ({ value: { deny: DRAWS_ALT } }))
  on('tool.call', () => screenshotResult(PNG_1X1))

  await $.tool.call({ tool: `${TOOL}godot_screenshot` })
  const ui = await $.ui.mount({ ...PANE, surface: 'terminal' })
  await settle(ui)
  await ui.redraw({ ...PANE.props, bodyColumns: 40, scroll: { offset: 0, bodyRows: 12 } })
  expect((await ui.find({ type: 'Raster' }))?.props).toMatchObject({ columns: 40, rows: 8 })
})

test('the terminal keeps the Image when the blit is taken', async ($, on) => {
  const blits: unknown[] = []
  on('ui.blit', (_$, e) => {
    blits.push(e)
    return { value: {} }
  })
  on('tool.call', () => screenshotResult(PNG_1X1))

  await $.tool.call({ tool: `${TOOL}godot_screenshot` })
  const ui = await $.ui.mount({ ...PANE, surface: 'terminal' })
  await settle(ui)
  expect(await ui.find({ type: 'Image' })).toBeDefined()
  expect(await ui.find({ type: 'Raster' })).toBeUndefined()
  expect(await ui.find({ type: 'Text', text: BLOCKS_WHY })).toBeUndefined()

  // Once the terminal has drawn a picture, it is not asked again.
  await $.tool.call({ tool: `${TOOL}godot_screenshot` })
  await settle(ui)
  expect(await ui.find({ type: 'Image' })).toBeDefined()
  expect(blits).toHaveLength(1)
})

test('a blit made before the Image is mounted asks again on the next drawing', async ($, on) => {
  const answers = [NOT_MOUNTED, DRAWS_ALT]
  let asked = 0
  on('ui.blit', () => ({ value: { deny: answers[Math.min(asked++, answers.length - 1)] ?? DRAWS_ALT } }))
  on('tool.call', () => screenshotResult(PNG_1X1))

  await $.tool.call({ tool: `${TOOL}godot_screenshot` })
  const ui = await $.ui.mount({ ...PANE, surface: 'terminal' })
  await settle(ui)
  expect(asked).toBe(2)
  expect(await ui.find({ type: 'Raster' })).toBeDefined()
})

test('a refused blit call keeps the Image', async ($, on) => {
  on('ui.blit', () => ({ deny: 'refused by another mod' }))
  on('tool.call', () => screenshotResult(PNG_1X1))

  await $.tool.call({ tool: `${TOOL}godot_screenshot` })
  const ui = await $.ui.mount({ ...PANE, surface: 'terminal' })
  await settle(ui)
  expect(await ui.find({ type: 'Image' })).toBeDefined()
  expect(await ui.find({ type: 'Raster' })).toBeUndefined()
})

test('a frame the decoder rejects keeps the text summary after the switch', async ($, on) => {
  on('ui.blit', () => ({ value: { deny: DRAWS_ALT } }))
  on('tool.call', () => screenshotResult(PNG_DAMAGED))

  await $.tool.call({ tool: `${TOOL}godot_screenshot` })
  const ui = await $.ui.mount({ ...PANE, surface: 'terminal' })
  await settle(ui)
  expect(await ui.find({ type: 'Raster' })).toBeUndefined()
  expect(await ui.find({ type: 'Image' })).toBeUndefined()
  expect(await ui.find({ type: 'Text', text: /1×1 frame.*cannot draw it.*ask Claude for a screenshot/ })).toBeDefined()
})

test('desktop never probes and keeps the text summary', async ($, on) => {
  const blits: unknown[] = []
  on('ui.blit', (_$, e) => {
    blits.push(e)
    return { value: { deny: DRAWS_ALT } }
  })
  on('tool.call', () => screenshotResult(PNG_1X1))

  await $.tool.call({ tool: `${TOOL}godot_screenshot` })
  const ui = await $.ui.mount({ ...PANE, surface: 'desktop' })
  await settle(ui)
  expect(blits).toEqual([])
  expect(await ui.find({ type: 'Raster' })).toBeUndefined()
  expect(await ui.find({ type: 'Text', text: BLOCKS_WHY })).toBeUndefined()
  expect(await ui.find({ type: 'Text', text: /1×1 frame.*this app cannot draw it/ })).toBeDefined()
})

test('the pane says when there is no frame yet', async ($) => {
  const ui = await $.ui.mount({ ...PANE, surface: 'terminal' })
  expect(await ui.find({ type: 'Text', text: /No frame yet/ })).toBeDefined()
})

test('Refresh calls mcp.call for godot_screenshot and shows the new frame', async ($, on) => {
  const calls: { server: string; tool: string }[] = []
  on('mcp.call', (_$, e) => {
    calls.push({ server: e.server, tool: e.tool })
    return {
      value: {
        content: [{ type: 'image', source: { type: 'base64', media_type: 'image/png', data: PNG_1X1 } }],
        isError: false,
      },
    }
  })

  const ui = await $.ui.mount({ ...PANE, surface: 'terminal' })
  await ui.press({ key: 'refresh' })
  expect(calls).toEqual([{ server: SERVER, tool: 'godot_screenshot' }])
  expect(await ui.find({ type: 'Image' })).toBeDefined()
})

test('Re-read status calls mcp.call for godot_status and updates the pane', async ($, on) => {
  const calls: { server: string; tool: string }[] = []
  on('mcp.call', (_$, e) => {
    calls.push({ server: e.server, tool: e.tool })
    return { value: { content: [{ type: 'text', text: CONNECTED_STATUS }], isError: false } }
  })

  const ui = await $.ui.mount({ ...PANE, surface: 'terminal' })
  await ui.press({ key: 'status' })
  expect(calls).toEqual([{ server: SERVER, tool: 'godot_status' }])
  expect(await ui.find({ type: 'Text', text: /default 127\.0\.0\.1:26700 · connected/ })).toBeDefined()
})

test('a refused button call is shown in the pane', async ($, on) => {
  on('mcp.call', () => ({ deny: 'not connected' }))

  const ui = await $.ui.mount({ ...PANE, surface: 'terminal' })
  await ui.press({ key: 'refresh' })
  expect(await ui.find({ type: 'Text', text: /not connected/ })).toBeDefined()
})

test('/stagehand-view opens the pane and prints nothing', async ($, on) => {
  const opened: unknown[] = []
  on('ui.open', (_$, e) => {
    opened.push(e)
    return { value: { isPlaced: true } }
  })

  const answer = await $.command.run({
    command: 'stagehand-view',
    args: '',
    origin: { kind: 'composer' },
    presentation: { isFullscreen: false, columns: 120 },
  })
  expect(answer).toEqual({})
  expect(opened).toEqual([expect.objectContaining({ id: 'stagehand-view' })])
})

test('a refused command.register does not break the band', async ($, on) => {
  on('session.start', () => ({ cwd: '/work' }))
  on('command.register', () => ({ deny: '"/stagehand-view" refused: it is taken' }))
  on('ui.render', () => OTHERS)
  on('tool.call', () => statusResult(CONNECTED_STATUS))

  await $.session.start({ surface: 'terminal', isInteractive: true, cwd: '/work' })
  await $.tool.call({ tool: `${TOOL}godot_status` })
  const ui = await $.ui.mount({ ...BAND, surface: 'terminal' })
  expect(await ui.find({ type: 'Text', text: /stagehand · default/ })).toBeDefined()
})
