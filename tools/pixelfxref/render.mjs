#!/usr/bin/env node
// Renders pixelfx's effects with pixelfx itself, at the pinned commit, into the reference PNGs the Go port's parity tests compare against. Run by hand (see README.md); nothing in the Go build runs it.
//
//   node tools/pixelfxref/render.mjs --pixelfx <pixelfx clone> fire/torch explosions/small-explosion
//   node tools/pixelfxref/render.mjs --pixelfx <pixelfx clone> fire            every effect of a category
//   node tools/pixelfxref/render.mjs --pixelfx <pixelfx clone> rig core        the core's own references: the rig, the painter and the simulation
//
// It writes packages/game/pixelfx/<category>/testdata/ref/<name>/<case>-<frame>.png, and for rig and core packages/game/pixelfx/testdata/ref/rig and testdata/ref/core (or under --out <dir>). The cases and frames are the ones packages/game/pixelfx/internal/pixelfxtest renders: keep the two in step.

import { spawnSync } from "node:child_process";
import { existsSync, mkdirSync, readdirSync, rmSync, writeFileSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";

// The pixelfx commit the port follows.
const PINNED = "5294518ce7c72e48b6100a744596e0acf57f2826";

// The share of the way through an effect each sampled frame is.
const SAMPLES = [0.25, 0.5, 0.75];
// The frame rate an act is rendered at, as pixelfx's tools/act.mjs renders it.
const ACT_FPS = 60;

const here = dirname(fileURLToPath(import.meta.url));

function usage(msg) {
  if (msg) console.error(msg);
  console.error("usage: node tools/pixelfxref/render.mjs --pixelfx <pixelfx clone> [--out <dir>] <category|category/name>...");
  process.exit(2);
}

function parseArgs(argv) {
  const out = { ids: [] };
  for (let i = 0; i < argv.length; i++) {
    const a = argv[i];
    if (a === "--pixelfx") out.pixelfx = argv[++i];
    else if (a === "--out") out.out = argv[++i];
    else if (a === "--one") out.one = { id: argv[++i], kase: argv[++i] };
    else if (a.startsWith("--")) usage(`unknown flag ${a}`);
    else out.ids.push(a);
  }
  return out;
}

// The effects an argument names: one id, or every effect of a category (files starting with _ are helpers).
function idsOf(dir, arg) {
  if (arg.includes("/")) {
    if (!existsSync(join(dir, "src", "fx", arg + ".js"))) usage(`no effect ${arg} in ${dir}`);
    return [arg];
  }
  const cat = join(dir, "src", "fx", arg);
  if (!existsSync(cat)) usage(`no category ${arg} in ${dir}`);
  return readdirSync(cat).filter((f) => f.endsWith(".js") && !f.startsWith("_")).sort().map((f) => `${arg}/${f.slice(0, -3)}`);
}

// The frames sampled from an effect's n sheet frames.
function sheetSamples(n) {
  return [...new Set(SAMPLES.map((f) => Math.min(n - 1, Math.round(n * f))))];
}

// The poses the rig reference draws: every weapon, both hands, turns, the air and the switches. packages/game/pixelfx/rig_test.go has the same list: keep the two in step.
function rigPoses(READY) {
  return {
    stand: {},
    ready: READY,
    sword: { ...READY, weapon: "sword", sa: -0.6, fx: 12, fy: -24 },
    "sword-two": { weapon: "sword", two: true, sa: -1.2, fx: 6, fy: -30, crouch: 2 },
    greatsword: { weapon: "greatsword", two: true, sa: -2.2, fx: -4, fy: -36, lean: -2 },
    dagger: { weapon: "dagger", offWeapon: "dagger", sa: 0.3, sb: 2.6, fx: 13, fy: -20, bx: -8, by: -22 },
    spear: { weapon: "spear", two: true, sa: 0.1, fx: 10, fy: -22, lean: 3, crouch: 3 },
    hammer: { weapon: "hammer", two: true, sa: -1.9, fx: 2, fy: -38, yaw: 0.4 },
    scythe: { weapon: "scythe", two: true, sa: -1.4, fx: 4, fy: -34, face: -1 },
    claw: { weapon: "claw", offWeapon: "claw", sa: 0.2, fx: 14, fy: -18, bx: 9, by: -26, armsFront: true },
    bow: { weapon: "bow", pull: true, nocked: true, sa: 0.05, fx: 14, fy: -26, bx: 2, by: -26 },
    pistol: { weapon: "pistol", sa: 0, fx: 15, fy: -27, toeF: true },
    launcher: { weapon: "launcher", two: true, sa: 0.15, fx: 8, fy: -27, wl: 0.8, wz: -1 },
    handle: { weapon: "handle", sa: -0.4, fx: 10, fy: -20, hx: 1, hy: -1, twist: 2, turn: 0.3 },
    air: { y: -20, crouch: -2, fx: -8, fy: -40, bx: 10, by: -42, ffx: 5, ffy: -9, fbx: -4, fby: -6, toeB: true },
    spin: { yaw: 2.4, weapon: "sword", sa: 1.2, fx: 10, fy: -20, fz: 6, bz: -6, fzz: 3, bzz: -3 },
  };
}

// The rig reference: each pose's pixels with its feet at (RIG_X, RIG_Y) of a RIG_W x RIG_H image, and beside it <pose>-src.png, each pixel's source in the standing character plus 1 in red (low byte) and green (high byte), blue 255 for ink.
const RIG_W = 200, RIG_H = 140, RIG_X = 100, RIG_Y = 110;
async function renderRig(dir, out) {
  const core = await import(pathToFileURL(join(dir, "src", "core", "index.js")).href);
  const { encodePNG } = await import(pathToFileURL(join(dir, "tools", "lib", "png.mjs")).href);
  const dest = join(out, "testdata", "ref", "rig");
  mkdirSync(dest, { recursive: true });
  for (const [name, pose] of Object.entries(rigPoses(core.READY))) {
    const r = core.posed(pose), color = new Uint8ClampedArray(RIG_W * RIG_H * 4), src = new Uint8ClampedArray(RIG_W * RIG_H * 4);
    for (let i = 0; i < r.n; i++) {
      const x = RIG_X + r.xs[i], y = RIG_Y + r.ys[i];
      if (x < 0 || y < 0 || x >= RIG_W || y >= RIG_H) continue;
      const j = (y * RIG_W + x) * 4, c = r.color[i], s = r.src[i] + 1;
      color[j] = c & 255; color[j + 1] = (c >>> 8) & 255; color[j + 2] = (c >>> 16) & 255; color[j + 3] = 255;
      src[j] = s & 255; src[j + 1] = s >>> 8; src[j + 2] = r.ink[i] ? 255 : 0; src[j + 3] = 255;
    }
    writeFileSync(join(dest, `${name}.png`), encodePNG(RIG_W, RIG_H, color));
    writeFileSync(join(dest, `${name}-src.png`), encodePNG(RIG_W, RIG_H, src));
  }
}

// The core reference: every shape of the painter, and every particle kind, debris kind, light and blaze of the simulation, drawn without an effect around them. packages/game/pixelfx/core_test.go draws the same: keep the two in step.
function drawShapes(core, p) {
  const { RAMP, WHITE, PATCH, INK, star, ngon, sparkle, flare, band, crystal, stockActor } = core;
  const F = RAMP.fire, I = RAMP.ice;
  p.circle(-50, -30, 9, F[5]);
  p.circle(-50, -30, 6, (d) => (d < 0.5 ? WHITE : F[3]), (d) => 1 - d * 0.5);
  p.ellipse(-20, -32, 12, 5, I[4], 0.8);
  p.ring(10, -30, 10, 3, (v) => (v > 0.5 ? I[6] : I[3]));
  p.arc(40, -28, 14, 4, -2.5, 0.5, (u) => (u < 0.5 ? F[6] : F[4]), (u) => 1 - u * 0.6);
  p.arc(40, 0, 12, (u) => 4 * Math.sin(u * Math.PI), 0, 3, F[7]);
  p.beam(-60, 10, -20, 30, 5, (u, v) => (v < 0.4 ? WHITE : I[5]), (u) => 1 - u);
  p.poly(star(0, 15, 5, 12, 5, -Math.PI / 2), RAMP.gold[5]);
  p.tri(20, 30, 35, 10, 45, 35, RAMP.poison[4], 0.6);
  p.rect(-70, 35, 20, 6, RAMP.stone[4], 0.9);
  p.line(-75, -45, 70, 40, (u) => (u < 0.5 ? PATCH : INK), (u) => 1 - u * 0.7);
  p.polyline(ngon(60, 25, 8, 6, 0.2), WHITE);
  p.disc(-30, 5, 7, (d) => (d < 0.5 ? RAMP.smoke[5] : RAMP.smoke[3]), (d) => 0.8 * (1 - d));
  p.glow(-50, -30, 14, F[5], 0.6);
  p.glowLine(-60, 10, -20, 30, 6, I[5], 0.4);
  p.glowRing(10, -30, 10, 4, I[6], 0.5);
  p.sprite([".##.", "#oo#", "#oo#", ".##."], { "#": "#222034", o: "#f53cd1" }, 50, -50, { scale: 2, flip: true, a: 0.7 });
  p.text("PIXEL 42!", 0, 40, F[6], { font: "big", align: "center", outline: INK, shadow: RAMP.night[3] });
  const A = stockActor(1);
  p.mask(A.mask, -10, -10, (mx, my) => ((mx + my) % 3 ? I[5] : I[2]), 0.85);
  p.outline(A.mask, -10, -10, WHITE);
  sparkle(p, 30, -45, 6, RAMP.gold[5], RAMP.gold[3], 0.7);
  flare(p, -40, 45, 9, 0.5, RAMP.arcane);
  band(p, 0, 50, 30, 0.16, 3, 1, RAMP.ice, (g) => Math.sin(g) >= 0, true);
  crystal(p, 65, 50, 22, 9, 3, RAMP.ice, { a: 0.95, glint: 0.4 });
  p.blend(5, 5, PATCH, 0.4);
  p.light(6, 6, WHITE, 0.5);
}

function makeSim(core) {
  const { Particles, Bits, Lights, Blazes, P, PF, Rng, RAMP } = core;
  const R = new Rng(11), floor = () => 30;
  let blazes;
  const ps = new Particles({ rng: R, ground: floor, haze: "#6a8cff", onLand: (x, y) => blazes.add(x, y, 1.2, 0.8) });
  blazes = new Blazes(ps);
  const bits = new Bits({ ground: floor }), lights = new Lights();
  const kinds = [P.SPARK, P.MOTE, P.STREAK, P.FLAKE, P.VAPOUR, P.DUST, P.STAR, P.EMBER, P.SMOKE, P.FIRE, P.BRAND, P.GRIT];
  for (let i = 0; i < 96; i++) {
    const k = kinds[i % kinds.length], a = R.range(0, Math.PI * 2), sp = R.range(20, 160);
    ps.spawn(k, R.range(-20, 20), R.range(-10, 10), Math.cos(a) * sp, Math.sin(a) * sp - 40, R.range(0.4, 1.6), i % 5 ? RAMP.ice[(i * 3) % 8] : 0, R.range(0.5, 3), i % 7 === 0 ? R.range(0, 0.3) : 0, i % 4 === 0 ? PF.SMOKES | PF.BIG : i % 6 === 0 ? PF.BACK : 0);
  }
  for (let i = 0; i < 18; i++)
    bits.add({ kind: i % 3, x: R.range(-30, 30), y: R.range(-20, 0), vx: R.range(-80, 80), vy: R.range(-200, -40), a: R.range(0, 6), va: R.range(-10, 10), s: R.int(1, 4), c: RAMP.stone[5], g: 0.8, drag: 0.3, bounce: i % 2 ? 0.4 : 0, life: R.range(0.8, 2) });
  lights.add(0, 0, 40, RAMP.fire[5], 0.7, 1.2);
  blazes.add(-25, 30, 1.5, 0.9);
  return {
    update(dt) { bits.update(dt); blazes.update(dt); ps.update(dt); lights.update(dt); },
    draw(p) { blazes.drawBack(p); ps.draw(p, "back"); bits.draw(p); ps.draw(p, "front"); lights.draw(p); },
  };
}

// The core references, 160 x 120 with the painter's origin at (80, 60): shapes-<case>.png placed plainly, mirrored and turned by 0.3 radians, and sim-<step>.png after that many steps of 1/120 s.
async function renderCore(dir, out) {
  const core = await import(pathToFileURL(join(dir, "src", "core", "index.js")).href);
  const { encodePNG } = await import(pathToFileURL(join(dir, "tools", "lib", "png.mjs")).href);
  const dest = join(out, "testdata", "ref", "core");
  mkdirSync(dest, { recursive: true });
  const W = 160, H = 120, frame = core.createFrame(W, H);
  const save = (name) => writeFileSync(join(dest, name), encodePNG(W, H, core.composite(frame, new Uint8ClampedArray(W * H * 4), null)));
  for (const [name, flip, angle] of [["plain", false, 0], ["flip", true, 0], ["turned", false, 0.3]]) {
    core.clearFrame(frame);
    drawShapes(core, core.painterFor(frame).place(80, 60, flip, angle));
    save(`shapes-${name}.png`);
  }
  const sim = makeSim(core);
  for (let step = 1; step <= 120; step++) {
    sim.update(1 / 120);
    if (step === 30 || step === 72 || step === 120) {
      core.clearFrame(frame);
      sim.draw(core.painterFor(frame).place(80, 60, false, 0));
      save(`sim-${String(step).padStart(3, "0")}.png`);
    }
  }
}

// Renders one case of one effect in this process: a fresh process per case, so the rig's pose cache starts empty, as the Go port's does.
async function renderOne(dir, out, id, kase) {
  if (id === "rig") return renderRig(dir, out);
  if (id === "core") return renderCore(dir, out);
  const lib = await import(pathToFileURL(join(dir, "tools", "lib", "fxlib.mjs")).href);
  const core = await import(pathToFileURL(join(dir, "src", "core", "index.js")).href);
  const { encodePNG } = await import(pathToFileURL(join(dir, "tools", "lib", "png.mjs")).href);
  const fx = await lib.loadEffect(id);
  const [cat, name] = id.split("/");
  const dest = join(out, cat, "testdata", "ref", name);
  mkdirSync(dest, { recursive: true });
  const save = (frame, rgba, w, h) => writeFileSync(join(dest, `${kase}-${String(frame).padStart(3, "0")}.png`), encodePNG(w, h, rgba));
  const options = kase === "flip" ? { flip: true } : {};

  if (kase === "act") {
    // tools/lib/fxlib.mjs renderAct, without the backdrop or the shake
    const { meta } = fx, b = core.actBounds(fx.mod), pad = 6;
    const w = b.x1 - b.x0 + pad * 2, h = b.y1 - b.y0 + pad * 2, X = pad - b.x0, Y = pad - b.y0;
    const frame = core.createFrame(w, h);
    const act = core.createAct(fx.mod, { x: X, y: Y, ...options });
    const span = core.actSpan(fx.mod), end = meta.loop ? meta.duration * 2 : span.to;
    const targets = SAMPLES.map((f) => span.from + f * (end - span.from));
    const max = Math.round((meta.loop ? meta.duration * 2 - act.t : 8) * ACT_FPS);
    let i = 0, next = 0;
    // every frame is drawn as the tools draw it: drawing is part of an act's state (it remembers the pose it drew)
    const show = () => {
      core.clearFrame(frame);
      act.draw(frame, { body: true, target: true });
      if (next < targets.length && act.t >= targets[next]) {
        save(i, core.composite(frame, new Uint8ClampedArray(w * h * 4), null), w, h);
        while (next < targets.length && act.t >= targets[next]) next++;
      }
      i++;
    };
    show();
    for (let k = 0; k < max && !act.done(); k++) {
      act.update(1 / ACT_FPS);
      show();
    }
    if (!meta.loop) for (let k = 0; k < ACT_FPS * 0.3; k++) {
      act.update(1 / ACT_FPS);
      show();
    }
    return;
  }

  // tools/lib/fxlib.mjs renderFrames for a sprite sheet: no margin, no backdrop, light faded at the edges
  const { meta } = fx, [W, H] = meta.size, [OX, OY] = meta.origin, fps = meta.fps;
  const n = Math.max(1, Math.round(meta.duration * fps) + (meta.loop ? 0 : 1));
  const want = new Set(sheetSamples(n));
  const frame = core.createFrame(W, H, 0, 0);
  const inst = core.createFx(fx.mod, { x: OX, y: OY, ...options });
  for (let i = 0; i < n; i++) {
    if (i > 0) inst.update(1 / fps);
    core.clearFrame(frame);
    inst.draw(frame);
    if (!lib.isScreen(meta)) lib.edgeFade(frame);
    if (want.has(i)) save(i, core.composite(frame, new Uint8ClampedArray(W * H * 4), null), W, H);
  }
}

const args = parseArgs(process.argv.slice(2));
if (!args.pixelfx) usage("--pixelfx is required");
const dir = resolve(args.pixelfx);
const out = resolve(args.out ?? join(here, "..", "..", "packages", "game", "pixelfx"));

if (args.one) {
  await renderOne(dir, out, args.one.id, args.one.kase);
} else {
  if (!args.ids.length) usage();
  const head = spawnSync("git", ["-C", dir, "rev-parse", "HEAD"], { encoding: "utf8" });
  if (head.status !== 0) usage(`${dir} is not a git clone of pixelfx`);
  if (head.stdout.trim() !== PINNED) usage(`${dir} is at ${head.stdout.trim()}, not the pinned commit ${PINNED}; move it there with git -C ${dir} switch --detach ${PINNED}`);
  let failed = 0;
  // the core's own references, written to testdata/ref/rig and testdata/ref/core of the pixelfx package
  for (const own of ["rig", "core"].filter((a) => args.ids.includes(a))) {
    rmSync(join(out, "testdata", "ref", own), { recursive: true, force: true });
    const r = spawnSync(process.execPath, [fileURLToPath(import.meta.url), "--pixelfx", dir, "--out", out, "--one", own, own], { stdio: "inherit" });
    if (r.status !== 0) failed++;
    console.log(`${own} -> ${join(out, "testdata", "ref", own)}`);
  }
  for (const id of args.ids.filter((a) => a !== "rig" && a !== "core").flatMap((a) => idsOf(dir, a))) {
    const [cat, name] = id.split("/");
    const dest = join(out, cat, "testdata", "ref", name);
    rmSync(dest, { recursive: true, force: true });
    const mod = await import(pathToFileURL(join(dir, "src", "fx", id + ".js")).href);
    const cases = ["default", "flip", ...(mod.act ? ["act"] : [])];
    for (const kase of cases) {
      const r = spawnSync(process.execPath, [fileURLToPath(import.meta.url), "--pixelfx", dir, "--out", out, "--one", id, kase], { stdio: "inherit" });
      if (r.status !== 0) {
        failed++;
        console.error(`${id} ${kase}: failed`);
      }
    }
    console.log(`${id}: ${cases.join(", ")} -> ${dest}`);
  }
  process.exit(failed ? 1 : 0);
}
