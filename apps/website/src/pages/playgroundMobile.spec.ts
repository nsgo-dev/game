import assert from 'node:assert/strict'
import { describe, test } from 'node:test'
import { PLAYGROUND_ENTRIES, PLAYGROUND_IDS, playgroundEntry } from '../content/playground.ts'
import {
  canRequestFullscreen,
  createMobilePlayState,
  enterMobilePlay,
  exitStageFullscreen,
  FRAME_TOUCH_STYLE_ID,
  fullscreenElementOf,
  hintKind,
  isMobilePlayActive,
  lockFrameTouch,
  lockPageScroll,
  mobilePlayReducer,
  PLAYING_CLASS,
  readCoarsePointer,
  readScreenOrientation,
  shouldLockPageScroll,
  shouldShowRotatePrompt,
  tryLockOrientation,
  tryUnlockOrientation,
} from './playgroundMobile.ts'

describe('playground entries orientation', () => {
  test('every playground example declares a valid orientation', () => {
    assert.equal(PLAYGROUND_ENTRIES.length, PLAYGROUND_IDS.length)
    for (const id of PLAYGROUND_IDS) {
      const entry = playgroundEntry(id)
      assert.equal(entry.id, id)
      assert.ok(['landscape', 'portrait', 'any'].includes(entry.orientation), `${id} orientation`)
    }
    assert.equal(playgroundEntry('caverun').orientation, 'landscape')
    assert.equal(playgroundEntry('slidegem').orientation, 'portrait')
    assert.equal(playgroundEntry('cryptquest').orientation, 'landscape')
    assert.equal(playgroundEntry('starburst').orientation, 'landscape')
  })
})

describe('mobile play mode state', () => {
  test('starts inline and switches hint copy between touch and keyboard', () => {
    let state = createMobilePlayState('landscape', { coarse: false, screenOrientation: 'landscape' })
    assert.equal(isMobilePlayActive(state), false)
    assert.equal(shouldLockPageScroll(state), false)
    assert.equal(hintKind(state), 'none')

    state = mobilePlayReducer(state, { type: 'started' })
    assert.equal(hintKind(state), 'keyboard')

    state = mobilePlayReducer(state, { type: 'coarseChanged', coarse: true })
    assert.equal(hintKind(state), 'touch')
  })

  test('shows rotate prompt for Cave Run held upright when orientation lock is unavailable', () => {
    let state = createMobilePlayState('landscape', { coarse: true, screenOrientation: 'portrait' })
    // Inline mode does not cover the page with a rotate prompt.
    assert.equal(shouldShowRotatePrompt(state), false)

    // Entering overlay mode (iPhone Safari) without an orientation lock shows the prompt.
    state = mobilePlayReducer(state, { type: 'enteredMode', mode: 'overlay', orientationLocked: false })
    assert.equal(isMobilePlayActive(state), true)
    assert.equal(shouldLockPageScroll(state), true)
    assert.equal(shouldShowRotatePrompt(state), true)

    // Dismissing hides the prompt until the player rotates to landscape and back.
    state = mobilePlayReducer(state, { type: 'dismissedRotate' })
    assert.equal(shouldShowRotatePrompt(state), false)

    state = mobilePlayReducer(state, { type: 'screenOrientationChanged', screenOrientation: 'landscape' })
    assert.equal(shouldShowRotatePrompt(state), false)

    state = mobilePlayReducer(state, { type: 'screenOrientationChanged', screenOrientation: 'portrait' })
    assert.equal(shouldShowRotatePrompt(state), true)

    // Exiting mobile play returns to inline and unlocks scroll.
    state = mobilePlayReducer(state, { type: 'exitedMode' })
    assert.equal(isMobilePlayActive(state), false)
    assert.equal(shouldLockPageScroll(state), false)
    assert.equal(shouldShowRotatePrompt(state), false)
  })

  test('suppresses rotate prompt when orientation is locked or example accepts any orientation', () => {
    let locked = createMobilePlayState('landscape', { coarse: true, screenOrientation: 'portrait' })
    locked = mobilePlayReducer(locked, { type: 'enteredMode', mode: 'fullscreen', orientationLocked: true })
    assert.equal(shouldShowRotatePrompt(locked), false)

    let anyOrientation = createMobilePlayState('any', { coarse: true, screenOrientation: 'portrait' })
    anyOrientation = mobilePlayReducer(anyOrientation, { type: 'enteredMode', mode: 'overlay', orientationLocked: false })
    assert.equal(shouldShowRotatePrompt(anyOrientation), false)

    let slidegem = createMobilePlayState('portrait', { coarse: true, screenOrientation: 'portrait' })
    slidegem = mobilePlayReducer(slidegem, { type: 'enteredMode', mode: 'overlay', orientationLocked: false })
    assert.equal(shouldShowRotatePrompt(slidegem), false)

    slidegem = mobilePlayReducer(slidegem, { type: 'screenOrientationChanged', screenOrientation: 'landscape' })
    assert.equal(shouldShowRotatePrompt(slidegem), true)
  })
})

describe('fullscreen, orientation lock and scroll lock helpers', () => {
  test('enterMobilePlay uses native fullscreen and orientation lock on Android-like browsers', async () => {
    let lockedTo: string | undefined
    const el = {
      requestFullscreen: async () => {},
    }
    const doc = { fullscreenEnabled: true }
    const scr = {
      orientation: {
        lock: async (orientation: 'landscape' | 'portrait') => {
          lockedTo = orientation
        },
        unlock: () => {},
      },
    }
    assert.equal(canRequestFullscreen(el, doc), true)
    const result = await enterMobilePlay(el, doc, scr, 'landscape')
    assert.deepEqual(result, { mode: 'fullscreen', orientationLocked: true })
    assert.equal(lockedTo, 'landscape')
  })

  test('enterMobilePlay falls back to overlay when fullscreen is unavailable (iPhone Safari) or refused', async () => {
    const iphoneResult = await enterMobilePlay({}, { fullscreenEnabled: false }, {}, 'landscape')
    assert.deepEqual(iphoneResult, { mode: 'overlay', orientationLocked: false })

    const refusedResult = await enterMobilePlay(
      {
        requestFullscreen: async () => {
          throw new Error('denied')
        },
      },
      { fullscreenEnabled: true },
      {},
      'landscape',
    )
    assert.deepEqual(refusedResult, { mode: 'overlay', orientationLocked: false })
  })

  test('enterMobilePlay reports orientationLocked=false when fullscreen succeeds but lock rejects', async () => {
    const result = await enterMobilePlay(
      { requestFullscreen: async () => {} },
      { fullscreenEnabled: true },
      {
        orientation: {
          lock: async () => {
            throw new Error('NotSupportedError')
          },
        },
      },
      'landscape',
    )
    assert.deepEqual(result, { mode: 'fullscreen', orientationLocked: false })
  })

  test('fullscreen exit, webkit fallback and orientation unlock tolerate missing or throwing APIs', async () => {
    let exited = false
    const fakeEl = {}
    const doc = {
      webkitFullscreenElement: fakeEl,
      webkitExitFullscreen: async () => {
        exited = true
      },
    }
    assert.equal(fullscreenElementOf(doc), fakeEl)
    await exitStageFullscreen(doc)
    assert.equal(exited, true)

    assert.equal(await tryLockOrientation('any', undefined), false)
    let unlocked = false
    tryUnlockOrientation({
      orientation: {
        unlock: () => {
          unlocked = true
        },
      },
    })
    assert.equal(unlocked, true)
    tryUnlockOrientation({
      orientation: {
        unlock: () => {
          throw new Error('ignored')
        },
      },
    })
  })

  test('readCoarsePointer and readScreenOrientation read matchMedia and fall back to viewport size', () => {
    const mm = (query: string) => ({ matches: query === '(pointer: coarse)' || query === '(orientation: portrait)' }) as MediaQueryList
    assert.equal(readCoarsePointer({ matchMedia: mm }), true)
    assert.equal(readScreenOrientation({ innerWidth: 390, innerHeight: 844, matchMedia: mm }), 'portrait')
    assert.equal(readScreenOrientation({ innerWidth: 844, innerHeight: 390, matchMedia: undefined as unknown as Window['matchMedia'] }), 'landscape')
  })

  test('lockPageScroll toggles root class and restores prior scroll offset', () => {
    const classes = new Set<string>()
    let scrollY = 320
    const doc = {
      documentElement: {
        classList: {
          add: (c: string) => classes.add(c),
          remove: (c: string) => classes.delete(c),
        },
      },
    } as unknown as Document
    const win = {
      get scrollY() {
        return scrollY
      },
      scrollTo: (_x: number, y: number) => {
        scrollY = y
      },
    } as unknown as Window

    const unlock = lockPageScroll(doc, win)
    assert.equal(classes.has(PLAYING_CLASS), true)
    scrollY = 0
    unlock()
    assert.equal(classes.has(PLAYING_CLASS), false)
    assert.equal(scrollY, 320)
  })

  test('lockFrameTouch injects and removes the iframe touch-action style cleanly', () => {
    const appended: Array<{ id: string; textContent: string; removed?: boolean; remove: () => void }> = []
    const doc = {
      getElementById: (id: string) => appended.find((n) => n.id === id && !n.removed) ?? null,
      createElement: () => {
        const node = {
          id: '',
          textContent: '',
          removed: false,
          remove() {
            node.removed = true
          },
        }
        return node
      },
      head: {
        appendChild: (node: (typeof appended)[number]) => {
          appended.push(node)
          return node
        },
      },
    } as unknown as Document

    const unlock = lockFrameTouch(doc)
    assert.equal(appended.length, 1)
    assert.equal(appended[0]!.id, FRAME_TOUCH_STYLE_ID)
    assert.match(appended[0]!.textContent, /touch-action:\s*none/)
    assert.match(appended[0]!.textContent, /overscroll-behavior:\s*none/)

    unlock()
    assert.equal(appended[0]!.removed, true)
  })
})
