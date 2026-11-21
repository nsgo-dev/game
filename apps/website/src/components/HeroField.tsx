import { useEffect, useRef, useState } from 'react'
import { copies } from '../copy'
import { useLang } from '../i18n'
import { THEME_EVENT } from '../lib/theme'

// The hero's background, ported from nsgo.dev's HeroField and made a small playfield. The dot grid is drawn by a WebGL shader, with a slow noise field lighting patches of dots blue, brightest around the heading, and a ring of lit dots spreading from the heading every few seconds. On it a few glowing square sprites step from dot to dot on a fixed 60 Hz update, drawn between steps as game.Loop draws, turning at random dots and leaving trails of lit dots. The pointer is the player: dots light up around it and part as through a lens, some sprites steer towards it and others away, and a click or tap sends out a burst of sparks and shakes the field a little. A strip of readouts under the field shows what it is doing: the fixed step it updates on, the frame rate it measures, its sprites and how many dots the visitor has lit. The grid and sprites fade under the copy's words so they keep a quiet ground.
// The prerendered page shows the CSS dots of `.hero__glow`; the shader starts after hydration, once the browser is idle, and the CSS dots give way to it. Without WebGL the CSS dots stay; with less motion asked for, one still frame is drawn, the sprites stand still and clicks send nothing. Either way the glow centres on the heading.

const vertex = `
  attribute vec2 position;
  void main() {
    gl_Position = vec4(position, 0.0, 1.0);
  }
`

const SPRITES = 5
const SPARKS = 16

// Works in CSS pixels so the grid matches the 22-pixel CSS dots it replaces. Outputs premultiplied alpha over the page's background.
const fragment = `
  precision highp float;
  uniform vec2 size;
  uniform float scale;
  uniform float time;
  uniform vec3 dotColor;
  uniform vec3 accent;
  uniform float strength;
  uniform vec2 pointer;
  uniform float reach;
  uniform vec4 quiet;
  uniform vec3 focus;
  uniform vec4 ripples[2];
  uniform sampler2D heat;
  uniform vec2 grid;
  uniform vec2 shake;
  uniform vec4 sprites[${SPRITES}];
  uniform vec4 sparks[${SPARKS}];
  uniform int spriteCount;
  uniform int sparkCount;

  const float cell = 22.0;

  float hash(vec2 p) {
    p = fract(p * vec2(123.34, 456.21));
    p += dot(p, p + 45.32);
    return fract(p.x * p.y);
  }

  float noise(vec3 p) {
    vec3 i = floor(p);
    vec3 f = fract(p);
    f = f * f * (3.0 - 2.0 * f);
    vec2 o = vec2(i.z * 17.0, i.z * 31.0);
    float a = mix(mix(hash(i.xy + o), hash(i.xy + vec2(1.0, 0.0) + o), f.x),
                  mix(hash(i.xy + vec2(0.0, 1.0) + o), hash(i.xy + vec2(1.0, 1.0) + o), f.x), f.y);
    o += vec2(17.0, 31.0);
    float b = mix(mix(hash(i.xy + o), hash(i.xy + vec2(1.0, 0.0) + o), f.x),
                  mix(hash(i.xy + vec2(0.0, 1.0) + o), hash(i.xy + vec2(1.0, 1.0) + o), f.x), f.y);
    return mix(a, b, f.z);
  }

  float fbm(vec3 p) {
    float sum = 0.0;
    float amp = 0.5;
    for (int i = 0; i < 4; i++) {
      sum += amp * noise(p);
      p = p * 2.03 + vec3(1.7, 9.2, 0.0);
      amp *= 0.5;
    }
    return sum;
  }

  // A ring spreading from the heading: its centre, the radius it starts at and its age in seconds.
  float ripple(vec4 r, vec2 at) {
    float age = r.w;
    if (age < 0.0 || age > 4.0) return 0.0;
    float radius = r.z + age * 210.0;
    float band = (length(at - r.xy) - radius) / 34.0;
    float fade = smoothstep(0.0, 0.25, age) * exp(-age * 0.9);
    return exp(-band * band) * fade;
  }

  void main() {
    vec2 p = vec2(gl_FragCoord.x, size.y - gl_FragCoord.y) / scale - shake;
    vec2 id = floor(p / cell + 0.5);

    // The pointer lights the dots around it and pushes them a little outward, as through a lens.
    vec2 away = id * cell - pointer;
    float gap = length(away);
    float near = reach * exp(-gap * gap / 60000.0) * 0.5;
    vec2 offset = p - id * cell - away / max(gap, 1.0) * near * 4.0;
    float d = length(offset);

    // A slow field drifting across the grid.
    float field = fbm(vec3(id * 0.07 + vec2(time * 0.12, 0.0), time * 0.08));
    float patch = smoothstep(0.48, 0.72, field);
    vec2 fromFocus = (p - focus.xy) / focus.z;
    patch *= mix(0.3, 1.0, exp(-dot(fromFocus, fromFocus)));

    float ring = max(ripple(ripples[0], id * cell), ripple(ripples[1], id * cell));

    // The dots the sprites passed (luminance) and the visitor lit (alpha), fading.
    vec4 lights = texture2D(heat, (id + 0.5) / grid);

    // Faint under the copy's words, brightening gradually from inside their box to well past its edge.
    vec2 q = abs(p - quiet.xy) - quiet.zw;
    float outside = length(max(q, 0.0)) + min(max(q.x, q.y), 0.0);
    float calm = mix(0.3, 1.0, smoothstep(-48.0, 180.0, outside));

    float lit = max(max(patch * 0.45, ring * 0.8), max(near, max(lights.r * 0.7, lights.a * 0.9)));

    // Lit dots grow and glow.
    float radius = 0.55 + lit * 0.5;
    float dotMask = 1.0 - smoothstep(radius, radius + 0.8, d);
    float glow = exp(-d * d / 10.0) * lit * 0.18;

    float base = dotMask * strength * mix(1.0, 1.0 / max(strength, 0.01), lit);
    vec3 color = mix(dotColor, accent, lit);
    float alpha = min(base + glow * (1.0 - dotMask), 1.0);
    vec4 dots = vec4(color * base + accent * glow * (1.0 - dotMask), alpha);

    // The sprites: rounded squares at their place between steps, with a soft halo; a zero size is no sprite.
    float body = 0.0;
    float halo = 0.0;
    for (int i = 0; i < ${SPRITES}; i++) {
      if (i >= spriteCount) break;
      vec4 s = sprites[i];
      vec2 b = abs(p - s.xy) - vec2(s.z - 2.5);
      float sd = length(max(b, 0.0)) + min(max(b.x, b.y), 0.0) - 2.5;
      body = max(body, 1.0 - smoothstep(-0.5, 0.5, sd));
      float edge = max(sd, 0.0);
      halo = max(halo, exp(-edge * edge / 40.0) * 0.32);
    }

    // The sparks of a click's burst: small points fading out.
    for (int i = 0; i < ${SPARKS}; i++) {
      if (i >= sparkCount) break;
      vec4 s = sparks[i];
      float g = length(p - s.xy);
      body = max(body, (1.0 - smoothstep(1.2, 2.2, g)) * s.z);
      halo = max(halo, exp(-g * g / 14.0) * 0.4 * s.z);
    }

    float cover = min(body + halo * (1.0 - body), 1.0);
    vec4 shapes = vec4(accent * cover, cover);
    gl_FragColor = (shapes + dots * (1.0 - cover)) * calm;
  }
`

// nsgo.dev lights a platform's tile 900 ms after load and the next every 2.2 s, each sending out a ring; the heading sends its rings on the same beat.
const FIRST_RING = 900
const RING_EVERY = 2200

const CELL = 22
// The fixed update rate and step, as game.Loop's 60 Hz, and the most steps one frame catches up on.
const RATE = 60
const STEP = 1000 / RATE
const MAX_STEPS = 8
// How long lit dots take to fade: the sprites' trails and the visitor's.
const TRAIL_FADE = 1.4
const VISITOR_FADE = 1.0
// A dot counts as lit by the visitor when its light crosses this.
const LIT = 0.5
// How far a sprite notices the pointer, in CSS pixels.
const SIGHT = 240

const DIRECTIONS: [number, number][] = [
  [1, 0],
  [-1, 0],
  [0, 1],
  [0, -1],
]

type Sprite = {
  col: number
  row: number
  dx: number
  dy: number
  // How far along to the next dot, 0–1, now and one step before, for drawing between steps.
  progress: number
  // The place drawn last step, for interpolation.
  px: number
  py: number
  x: number
  y: number
  // Dots per second.
  speed: number
  // Steers towards the pointer, or away from it.
  bold: boolean
}

// What the readouts show. fps is null while the field is not animating: before it starts, without WebGL, with less motion asked for, or paused.
export type FieldStats = { fps: number | null; sprites: number; score: number }

// How often the measured frame rate is sampled.
const FPS_WINDOW = 500

type Spark = { x: number; y: number; vx: number; vy: number; age: number; life: number }

// The box around the copy's words, its text, buttons and command, which is narrower than the column holding them.
function words(copy: Element): DOMRect | null {
  const boxes = '.pill, .btn, .command'
  const rects: DOMRect[] = Array.from(copy.querySelectorAll(boxes), (element) => element.getBoundingClientRect())
  const walker = document.createTreeWalker(copy, NodeFilter.SHOW_TEXT)
  const range = document.createRange()
  for (let node = walker.nextNode(); node; node = walker.nextNode()) {
    if (!node.textContent?.trim() || node.parentElement?.closest(boxes)) continue
    range.selectNodeContents(node)
    rects.push(...Array.from(range.getClientRects()))
  }
  if (!rects.length) return null
  const left = Math.min(...rects.map((rect) => rect.left))
  const top = Math.min(...rects.map((rect) => rect.top))
  const right = Math.max(...rects.map((rect) => rect.right))
  const bottom = Math.max(...rects.map((rect) => rect.bottom))
  return new DOMRect(left, top, right - left, bottom - top)
}

function compile(gl: WebGLRenderingContext, type: number, source: string): WebGLShader | null {
  const shader = gl.createShader(type)
  if (!shader) return null
  gl.shaderSource(shader, source)
  gl.compileShader(shader)
  return gl.getShaderParameter(shader, gl.COMPILE_STATUS) ? shader : null
}

// Resolves a CSS color, such as a custom property, to 0–1 RGB.
function rgb(value: string): [number, number, number] {
  const probe = document.createElement('span')
  probe.style.color = value
  document.body.append(probe)
  const parts = getComputedStyle(probe).color.match(/[\d.]+/g)?.map(Number) ?? [0, 0, 0]
  probe.remove()
  return [(parts[0] ?? 0) / 255, (parts[1] ?? 0) / 255, (parts[2] ?? 0) / 255]
}

// Centres the CSS glow on the heading, with or without WebGL. Returns its cleanup.
function placeGlow(hero: HTMLElement, layer: HTMLElement): () => void {
  const place = () => {
    const heading = hero.querySelector('h1')
    if (!heading) return
    const box = layer.getBoundingClientRect()
    const rect = heading.getBoundingClientRect()
    layer.style.setProperty('--glow-x', `${rect.left - box.left + rect.width / 2}px`)
    layer.style.setProperty('--glow-y', `${rect.top - box.top + rect.height / 2}px`)
  }
  const resizes = new ResizeObserver(place)
  resizes.observe(hero)
  place()
  return () => resizes.disconnect()
}

// Starts the field on the canvas. Returns its cleanup, or null where WebGL is missing or the shader does not build, leaving the CSS dots.
function startField(canvas: HTMLCanvasElement, hero: HTMLElement, onStats: (stats: FieldStats) => void, onLost: () => void): (() => void) | null {
  const gl = canvas.getContext('webgl', { alpha: true, premultipliedAlpha: true, antialias: false })
  if (!gl) return null
  const release = () => gl.getExtension('WEBGL_lose_context')?.loseContext()
  const vs = compile(gl, gl.VERTEX_SHADER, vertex)
  const fs = compile(gl, gl.FRAGMENT_SHADER, fragment)
  const program = gl.createProgram()
  if (!vs || !fs || !program) {
    release()
    return null
  }
  gl.attachShader(program, vs)
  gl.attachShader(program, fs)
  gl.linkProgram(program)
  if (!gl.getProgramParameter(program, gl.LINK_STATUS)) {
    release()
    return null
  }
  gl.useProgram(program)

  const buffer = gl.createBuffer()
  gl.bindBuffer(gl.ARRAY_BUFFER, buffer)
  gl.bufferData(gl.ARRAY_BUFFER, new Float32Array([-1, -1, 3, -1, -1, 3]), gl.STATIC_DRAW)
  const position = gl.getAttribLocation(program, 'position')
  gl.enableVertexAttribArray(position)
  gl.vertexAttribPointer(position, 2, gl.FLOAT, false, 0, 0)

  // One texel per dot: the sprites' trails in luminance, the visitor's light in alpha.
  const texture = gl.createTexture()
  gl.activeTexture(gl.TEXTURE0)
  gl.bindTexture(gl.TEXTURE_2D, texture)
  gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MIN_FILTER, gl.NEAREST)
  gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MAG_FILTER, gl.NEAREST)
  gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_S, gl.CLAMP_TO_EDGE)
  gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_T, gl.CLAMP_TO_EDGE)
  gl.pixelStorei(gl.UNPACK_ALIGNMENT, 1)

  const uniform = (name: string) => gl.getUniformLocation(program, name)
  const size = uniform('size')
  const scale = uniform('scale')
  const time = uniform('time')
  const dotColor = uniform('dotColor')
  const accent = uniform('accent')
  const strength = uniform('strength')
  const pointer = uniform('pointer')
  const reach = uniform('reach')
  const quiet = uniform('quiet')
  const focus = uniform('focus')
  const ripplesAt = uniform('ripples')
  const gridAt = uniform('grid')
  const shakeAt = uniform('shake')
  const spritesAt = uniform('sprites')
  const sparksAt = uniform('sparks')
  const spriteCount = uniform('spriteCount')
  const sparkCount = uniform('sparkCount')
  gl.uniform1i(uniform('heat'), 0)

  const still = window.matchMedia('(prefers-reduced-motion: reduce)')
  const origin = performance.now() - 40_000
  let frame = 0
  let visible = true
  let nextRing = performance.now() + FIRST_RING
  let last = -1
  let behind = 0

  // The pointer's place in the canvas and how much it shows, eased towards where it is and whether it is over the hero.
  const target = { x: 0, y: 0, reach: 0 }
  const eased = { x: 0, y: 0, reach: 0 }
  const rings = [
    { x: 0, y: 0, r: 0, start: -1e9 },
    { x: 0, y: 0, r: 0, start: -1e9 },
  ]

  // The playfield, in dots: its size, where sprites may go, and the copy's box they keep off.
  let cols = 0
  let rows = 0
  let trail = new Float32Array(0)
  let visitor = new Float32Array(0)
  let texels = new Uint8Array(0)
  const bounds = { top: 0, bottom: 0, left: 0, right: 0 }
  const calm = { x: -1000, y: -1000, w: 0, h: 0 }
  const sprites: Sprite[] = []
  const sparks: Spark[] = []
  let shakeUntil = 0
  const spriteData = new Float32Array(SPRITES * 4)
  const sparkData = new Float32Array(SPARKS * 4)
  let count = 0
  // Frames drawn since the window began, and the rate the last window measured.
  let frames = 0
  let windowStart = -1
  let fps: number | null = null
  let told: FieldStats = { fps: null, sprites: 0, score: 0 }
  const report = () => {
    if (told.fps === fps && told.sprites === sprites.length && told.score === count) return
    told = { fps, sprites: sprites.length, score: count }
    onStats(told)
  }

  const inside = (col: number, row: number) => col >= bounds.left && col <= bounds.right && row >= bounds.top && row <= bounds.bottom
  const quietAt = (col: number, row: number) => Math.abs(col * CELL - calm.x) < calm.w + CELL && Math.abs(row * CELL - calm.y) < calm.h + CELL

  // Lights the dots around a point for the visitor, counting each that comes alight.
  const lightAt = (x: number, y: number, spread: number) => {
    const c0 = Math.max(0, Math.floor((x - spread) / CELL))
    const c1 = Math.min(cols - 1, Math.ceil((x + spread) / CELL))
    const r0 = Math.max(0, Math.floor((y - spread) / CELL))
    const r1 = Math.min(rows - 1, Math.ceil((y + spread) / CELL))
    for (let row = r0; row <= r1; row++) {
      for (let col = c0; col <= c1; col++) {
        const distance = Math.hypot(col * CELL - x, row * CELL - y)
        if (distance > spread) continue
        const light = 1 - (distance / spread) * 0.45
        const at = row * cols + col
        const was = visitor[at] ?? 0
        if (light > was) {
          if (was < LIT && light >= LIT) count++
          visitor[at] = light
        }
      }
    }
  }

  const place = (sprite: Sprite) => {
    sprite.x = sprite.px = sprite.col * CELL
    sprite.y = sprite.py = sprite.row * CELL
  }

  // A free dot in bounds, off the copy where there is room.
  const spawn = (): [number, number] => {
    for (let tries = 0; tries < 60; tries++) {
      const col = bounds.left + Math.floor(Math.random() * (bounds.right - bounds.left + 1))
      const row = bounds.top + Math.floor(Math.random() * (bounds.bottom - bounds.top + 1))
      if (!quietAt(col, row)) return [col, row]
    }
    return [bounds.left, bounds.top]
  }

  // At each dot a sprite picks its way: on along its line mostly, turning at random, and near the pointer towards or away from it. It keeps off the copy unless there is no other way.
  const turn = (sprite: Sprite) => {
    const reverse = (d: [number, number]) => d[0] === -sprite.dx && d[1] === -sprite.dy
    const onField = DIRECTIONS.filter(([dx, dy]) => inside(sprite.col + dx, sprite.row + dy))
    let choices = onField.filter((d) => !reverse(d) && !quietAt(sprite.col + d[0], sprite.row + d[1]))
    if (!choices.length) choices = onField.filter((d) => !reverse(d))
    if (!choices.length) choices = onField
    if (!choices.length) return
    let pick: [number, number] | undefined
    const seen = target.reach > 0.5 && Math.hypot(sprite.x - target.x, sprite.y - target.y) < SIGHT
    if (seen && Math.random() < 0.85) {
      const score = ([dx, dy]: [number, number]) => Math.hypot((sprite.col + dx) * CELL - target.x, (sprite.row + dy) * CELL - target.y)
      pick = choices.reduce((best, d) => ((sprite.bold ? score(d) < score(best) : score(d) > score(best)) ? d : best))
    } else {
      const ahead = choices.find(([dx, dy]) => dx === sprite.dx && dy === sprite.dy)
      pick = ahead && Math.random() < 0.7 ? ahead : choices[Math.floor(Math.random() * choices.length)]
    }
    if (!pick) return
    sprite.dx = pick[0]
    sprite.dy = pick[1]
  }

  // One fixed step of the playfield.
  const update = () => {
    const dt = STEP / 1000
    const trailFade = Math.exp(-dt / TRAIL_FADE)
    const visitorFade = Math.exp(-dt / VISITOR_FADE)
    for (let i = 0; i < trail.length; i++) {
      trail[i] = (trail[i] ?? 0) * trailFade
      visitor[i] = (visitor[i] ?? 0) * visitorFade
    }
    for (const sprite of sprites) {
      sprite.px = sprite.x
      sprite.py = sprite.y
      const seen = target.reach > 0.5 && Math.hypot(sprite.x - target.x, sprite.y - target.y) < SIGHT
      sprite.progress += sprite.speed * (seen ? 1.4 : 1) * dt
      while (sprite.progress >= 1) {
        sprite.progress -= 1
        sprite.col += sprite.dx
        sprite.row += sprite.dy
        const at = sprite.row * cols + sprite.col
        if (at >= 0 && at < trail.length) trail[at] = 1
        turn(sprite)
      }
      sprite.x = (sprite.col + sprite.dx * sprite.progress) * CELL
      sprite.y = (sprite.row + sprite.dy * sprite.progress) * CELL
    }
    for (const spark of sparks) {
      spark.age += dt
      const drag = Math.exp(-dt * 3)
      spark.vx *= drag
      spark.vy *= drag
      spark.x += spark.vx * dt
      spark.y += spark.vy * dt
      if (spark.age < spark.life) lightAt(spark.x, spark.y, CELL * 0.75)
    }
    for (let i = sparks.length - 1; i >= 0; i--) if ((sparks[i]?.age ?? 0) >= (sparks[i]?.life ?? 0)) sparks.splice(i, 1)
    if (target.reach > 0.5) lightAt(target.x, target.y, CELL * 1.6)
  }

  const upload = () => {
    for (let i = 0; i < trail.length; i++) {
      texels[i * 2] = Math.round(Math.min(trail[i] ?? 0, 1) * 255)
      texels[i * 2 + 1] = Math.round(Math.min(visitor[i] ?? 0, 1) * 255)
    }
    gl.texSubImage2D(gl.TEXTURE_2D, 0, 0, 0, cols, rows, gl.LUMINANCE_ALPHA, gl.UNSIGNED_BYTE, texels)
  }

  const draw = (now: number) => {
    frame = 0
    const moving = !still.matches
    if (moving) {
      if (now >= nextRing) {
        const heading = hero.querySelector('h1')
        if (heading) {
          const box = canvas.getBoundingClientRect()
          const rect = heading.getBoundingClientRect()
          rings.unshift({ x: rect.left - box.left + rect.width / 2, y: rect.top - box.top + rect.height / 2, r: rect.height / 2, start: now })
          rings.length = 2
        }
        nextRing = now + RING_EVERY
      }
      // Fixed steps for the time since the last frame, as game.Loop runs them; after a pause the field picks up where it was.
      behind = last < 0 ? STEP : behind + Math.min(now - last, STEP * MAX_STEPS)
      last = now
      while (behind >= STEP) {
        update()
        behind -= STEP
      }
      frames++
      if (windowStart < 0) windowStart = now
      else if (now - windowStart >= FPS_WINDOW) {
        fps = Math.round((frames * 1000) / (now - windowStart))
        frames = 0
        windowStart = now
      }
    } else {
      last = -1
      fps = null
      windowStart = -1
      frames = 0
    }
    // Drawn between the last step and the next.
    const blend = moving ? behind / STEP : 1
    spriteData.fill(0)
    sprites.forEach((sprite, i) => {
      spriteData[i * 4] = sprite.px + (sprite.x - sprite.px) * blend
      spriteData[i * 4 + 1] = sprite.py + (sprite.y - sprite.py) * blend
      spriteData[i * 4 + 2] = 5
    })
    sparkData.fill(0)
    sparks.slice(0, SPARKS).forEach((spark, i) => {
      sparkData[i * 4] = spark.x
      sparkData[i * 4 + 1] = spark.y
      sparkData[i * 4 + 2] = 1 - spark.age / spark.life
    })
    const shaking = moving && now < shakeUntil
    const amount = shaking ? 3 * ((shakeUntil - now) / 260) : 0
    upload()

    const ease = moving ? 0.035 : 1
    eased.x += (target.x - eased.x) * ease
    eased.y += (target.y - eased.y) * ease
    eased.reach += (target.reach - eased.reach) * ease
    const settled = Math.abs(target.x - eased.x) + Math.abs(target.y - eased.y) < 0.5 && Math.abs(target.reach - eased.reach) < 0.005
    gl.uniform1f(time, moving ? (now - origin) / 1000 : 40)
    gl.uniform2f(pointer, eased.x, eased.y)
    gl.uniform1f(reach, eased.reach)
    // The latest ring and the one before it, which is still fading.
    gl.uniform4fv(
      ripplesAt,
      rings.flatMap((ring) => [ring.x, ring.y, ring.r, moving ? (now - ring.start) / 1000 : -1]),
    )
    gl.uniform4fv(spritesAt, spriteData)
    gl.uniform4fv(sparksAt, sparkData)
    gl.uniform1i(spriteCount, sprites.length)
    gl.uniform1i(sparkCount, Math.min(sparks.length, SPARKS))
    gl.uniform2f(shakeAt, (Math.random() * 2 - 1) * amount, (Math.random() * 2 - 1) * amount)
    gl.drawArrays(gl.TRIANGLES, 0, 3)
    report()
    if (visible && !document.hidden && (moving || !settled)) frame = requestAnimationFrame(draw)
    else {
      // Stopped until the next redraw, which starts a new window.
      windowStart = -1
      frames = 0
    }
  }
  const redraw = () => {
    if (frame) cancelAnimationFrame(frame)
    frame = requestAnimationFrame(draw)
  }

  const colors = () => {
    const dark = document.documentElement.dataset.theme === 'dark'
    gl.uniform3fv(dotColor, rgb('var(--border-strong)'))
    gl.uniform3fv(accent, rgb('var(--blue)'))
    gl.uniform1f(strength, dark ? 0.85 : 0.9)
    redraw()
  }

  const copy = hero.querySelector('.hero__copy')
  const resize = () => {
    const ratio = Math.min(window.devicePixelRatio || 1, 2)
    const width = Math.round(canvas.clientWidth * ratio)
    const height = Math.round(canvas.clientHeight * ratio)
    if (canvas.width !== width || canvas.height !== height) {
      canvas.width = width
      canvas.height = height
    }
    gl.viewport(0, 0, width, height)
    gl.uniform2f(size, width, height)
    gl.uniform1f(scale, ratio)
    const box = canvas.getBoundingClientRect()
    const block = copy ? words(copy) : null
    if (block) {
      calm.x = block.left - box.left + block.width / 2
      calm.y = block.top - box.top + block.height / 2
      calm.w = block.width / 2 + 16
      calm.h = block.height / 2 + 16
    } else {
      calm.x = calm.y = -1000
      calm.w = calm.h = 0
    }
    gl.uniform4f(quiet, calm.x, calm.y, calm.w, calm.h)
    const heading = hero.querySelector('h1')?.getBoundingClientRect()
    if (heading) {
      gl.uniform3f(focus, heading.left - box.left + heading.width / 2, heading.top - box.top + heading.height / 2, Math.max(heading.width, heading.height) * 0.8)
    } else {
      gl.uniform3f(focus, box.width / 2, 260, 1e6)
    }

    // The playfield follows the canvas: a texel per dot, and the sprites kept where the mask shows the grid, below the header's fade and above the hero's.
    const nextCols = Math.ceil(box.width / CELL) + 1
    const nextRows = Math.ceil(box.height / CELL) + 1
    if (nextCols !== cols || nextRows !== rows) {
      cols = nextCols
      rows = nextRows
      trail = new Float32Array(cols * rows)
      visitor = new Float32Array(cols * rows)
      texels = new Uint8Array(cols * rows * 2)
      gl.texImage2D(gl.TEXTURE_2D, 0, gl.LUMINANCE_ALPHA, cols, rows, 0, gl.LUMINANCE_ALPHA, gl.UNSIGNED_BYTE, texels)
      gl.uniform2f(gridAt, cols, rows)
    }
    bounds.left = 1
    bounds.right = Math.max(1, cols - 3)
    bounds.top = Math.max(1, Math.ceil((box.height * 0.16) / CELL))
    bounds.bottom = Math.max(bounds.top, Math.floor((box.height * 0.7) / CELL))
    const wanted = box.width < 700 ? 3 : SPRITES
    while (sprites.length > wanted) sprites.pop()
    while (sprites.length < wanted) {
      const [col, row] = spawn()
      const [dx, dy] = DIRECTIONS[Math.floor(Math.random() * 4)] ?? [1, 0]
      const sprite: Sprite = { col, row, dx, dy, progress: 0, px: 0, py: 0, x: 0, y: 0, speed: 4.5 + Math.random() * 2, bold: sprites.length % 2 === 0 }
      place(sprite)
      sprites.push(sprite)
    }
    for (const sprite of sprites) {
      if (inside(sprite.col, sprite.row)) continue
      ;[sprite.col, sprite.row] = spawn()
      sprite.progress = 0
      place(sprite)
    }
    redraw()
  }

  const point = (event: PointerEvent) => {
    const box = canvas.getBoundingClientRect()
    target.x = event.clientX - box.left
    target.y = event.clientY - box.top
    if (eased.reach < 0.01) {
      eased.x = target.x
      eased.y = target.y
    }
    target.reach = target.y < box.height ? 1 : 0
    // With less motion asked for nothing fades on its own: the dots around the pointer are the lit ones.
    if (still.matches) {
      visitor.fill(0)
      if (target.reach) lightAt(target.x, target.y, CELL * 1.6)
    }
    if (!frame) redraw()
  }
  // A click or tap sends a ring of sparks out from it and shakes the field a little; with less motion asked for, it only lights the dots as the pointer does.
  const press = (event: PointerEvent) => {
    point(event)
    if (still.matches || !target.reach) return
    const sparksEach = 12
    const turnBy = Math.random() * Math.PI * 2
    for (let i = 0; i < sparksEach; i++) {
      const angle = turnBy + (i / sparksEach) * Math.PI * 2
      const speed = 300 + Math.random() * 80
      sparks.push({ x: target.x, y: target.y, vx: Math.cos(angle) * speed, vy: Math.sin(angle) * speed, age: 0, life: 0.7 + Math.random() * 0.15 })
    }
    if (sparks.length > SPARKS) sparks.splice(0, sparks.length - SPARKS)
    shakeUntil = performance.now() + 260
    if (!frame) redraw()
  }
  const leave = () => {
    target.reach = 0
    if (still.matches) visitor.fill(0)
    if (!frame) redraw()
  }
  const shown = () => {
    if (!document.hidden) redraw()
    else {
      fps = null
      report()
    }
  }
  const lost = (event: Event) => {
    event.preventDefault()
    if (frame) cancelAnimationFrame(frame)
    frame = 0
    onLost()
  }

  // Passive: the page's own taps, on buttons and links, go on as they would.
  hero.addEventListener('pointermove', point, { passive: true })
  hero.addEventListener('pointerdown', press, { passive: true })
  hero.addEventListener('pointerleave', leave, { passive: true })
  canvas.addEventListener('webglcontextlost', lost)

  const resizes = new ResizeObserver(resize)
  resizes.observe(canvas)
  if (copy) resizes.observe(copy)
  const sight = new IntersectionObserver(([entry]) => {
    visible = entry?.isIntersecting ?? true
    if (visible) redraw()
  })
  sight.observe(canvas)
  // The words' box moves once the web fonts arrive.
  let alive = true
  document.fonts?.ready.then(() => alive && resize())
  document.addEventListener('visibilitychange', shown)
  document.addEventListener(THEME_EVENT, colors)
  still.addEventListener('change', redraw)

  colors()
  resize()

  return () => {
    alive = false
    if (frame) cancelAnimationFrame(frame)
    frame = 0
    hero.removeEventListener('pointermove', point)
    hero.removeEventListener('pointerdown', press)
    hero.removeEventListener('pointerleave', leave)
    canvas.removeEventListener('webglcontextlost', lost)
    resizes.disconnect()
    sight.disconnect()
    document.removeEventListener('visibilitychange', shown)
    document.removeEventListener(THEME_EVENT, colors)
    still.removeEventListener('change', redraw)
    gl.deleteTexture(texture)
    gl.deleteBuffer(buffer)
    gl.deleteProgram(program)
    gl.deleteShader(vs)
    gl.deleteShader(fs)
    release()
  }
}

// The glow layer, the canvas over it, the viewport's frame and the strip of readouts, placed inside `.hero`.
export function HeroField() {
  const hud = copies[useLang()].home.hud
  const glow = useRef<HTMLDivElement>(null)
  const canvas = useRef<HTMLCanvasElement>(null)
  const [shaded, setShaded] = useState(false)
  const [stats, setStats] = useState<FieldStats>({ fps: null, sprites: 0, score: 0 })

  useEffect(() => {
    const layer = glow.current
    const field = canvas.current
    const hero = field?.closest<HTMLElement>('.hero')
    if (!layer || !field || !hero) return
    const unplace = placeGlow(hero, layer)
    let stop: (() => void) | null = null
    // Off the hydration's critical path: the CSS dots show until the browser is idle.
    const begin = () => {
      stop = startField(field, hero, setStats, () => {
        setShaded(false)
        setStats({ fps: null, sprites: 0, score: 0 })
      })
      if (stop) setShaded(true)
    }
    // Safari has no idle callback; a timeout still lets hydration finish first.
    const idles = typeof window.requestIdleCallback === 'function'
    const idle = idles ? window.requestIdleCallback(begin, { timeout: 1000 }) : window.setTimeout(begin, 0)
    return () => {
      if (idles) window.cancelIdleCallback(idle)
      else window.clearTimeout(idle)
      unplace()
      stop?.()
    }
  }, [])

  // Without the field, before it starts or where WebGL is missing, every readout is empty rather than made up.
  const step = shaded ? `${STEP.toFixed(1)} ms` : null
  const fps = shaded && stats.fps !== null ? String(stats.fps).padStart(2, '0') : null
  const sprites = shaded ? String(stats.sprites) : null
  return (
    <>
      <div ref={glow} className={shaded ? 'hero__glow hero__glow--shaded' : 'hero__glow'} aria-hidden="true" />
      <canvas ref={canvas} className="hero-field" aria-hidden="true" />
      <div className="hero__frame" aria-hidden="true" />
      <div className="container hero__hud">
        <dl className="hud-strip" aria-label={hud.label}>
          <Readout label={hud.step} name={hud.stepName} idle={hud.idle} value={step} spoken={step} />
          <Readout label={hud.fps} name={hud.fpsName} idle={hud.idle} value={fps} spoken={stats.fps === null ? null : String(stats.fps)} />
          <Readout label={hud.sprites} name={hud.spritesName} idle={hud.idle} value={sprites} spoken={sprites} />
          <Readout
            label={hud.score}
            name={hud.scoreName}
            idle={hud.idle}
            value={shaded ? String(stats.score).padStart(4, '0') : null}
            spoken={shaded ? String(stats.score) : null}
            score
          />
        </dl>
      </div>
    </>
  )
}

// One readout: the short label and the value shown, and for screen readers the readout's name and the value without padding.
function Readout({ label, name, idle, value, spoken, score }: { label: string; name: string; idle: string; value: string | null; spoken: string | null; score?: boolean }) {
  return (
    <div className={score ? 'hud-strip__item hud-strip__item--score' : 'hud-strip__item'}>
      <dt>
        <span aria-hidden="true">{label}</span>
        <span className="sr-only">{name}</span>
      </dt>
      <dd>
        <span aria-hidden="true">{value ?? (score ? '----' : '--')}</span>
        <span className="sr-only">{spoken ?? idle}</span>
      </dd>
    </div>
  )
}
