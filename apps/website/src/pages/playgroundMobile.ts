import type { PlaygroundOrientation } from '../content/playground'

// Mobile play mode state and browser helpers for the playground. Kept free of React so the state transitions and fullscreen/orientation fallbacks can be tested directly.

export type PlayMode = 'inline' | 'fullscreen' | 'overlay'
export type ScreenOrientationKind = 'landscape' | 'portrait'

export interface MobilePlayState {
  coarse: boolean
  mode: PlayMode
  running: boolean
  preferredOrientation: PlaygroundOrientation
  screenOrientation: ScreenOrientationKind
  orientationLocked: boolean
  rotateDismissed: boolean
}

export type MobilePlayAction =
  | { type: 'coarseChanged'; coarse: boolean }
  | { type: 'screenOrientationChanged'; screenOrientation: ScreenOrientationKind }
  | { type: 'started' }
  | { type: 'enteredMode'; mode: 'fullscreen' | 'overlay'; orientationLocked: boolean }
  | { type: 'exitedMode' }
  | { type: 'dismissedRotate' }
  | { type: 'reset'; preferredOrientation: PlaygroundOrientation }

export function createMobilePlayState(
  preferredOrientation: PlaygroundOrientation,
  opts: { coarse?: boolean; screenOrientation?: ScreenOrientationKind } = {},
): MobilePlayState {
  return {
    coarse: opts.coarse ?? false,
    mode: 'inline',
    running: false,
    preferredOrientation,
    screenOrientation: opts.screenOrientation ?? 'landscape',
    orientationLocked: false,
    rotateDismissed: false,
  }
}

export function mobilePlayReducer(state: MobilePlayState, action: MobilePlayAction): MobilePlayState {
  switch (action.type) {
    case 'coarseChanged':
      return action.coarse === state.coarse ? state : { ...state, coarse: action.coarse }
    case 'screenOrientationChanged': {
      const rotateDismissed = action.screenOrientation === state.preferredOrientation ? false : state.rotateDismissed
      if (action.screenOrientation === state.screenOrientation && rotateDismissed === state.rotateDismissed) return state
      return { ...state, screenOrientation: action.screenOrientation, rotateDismissed }
    }
    case 'started':
      return state.running ? state : { ...state, running: true }
    case 'enteredMode':
      return {
        ...state,
        running: true,
        mode: action.mode,
        orientationLocked: action.orientationLocked,
        rotateDismissed: false,
      }
    case 'exitedMode':
      if (state.mode === 'inline' && !state.orientationLocked && !state.rotateDismissed) return state
      return { ...state, mode: 'inline', orientationLocked: false, rotateDismissed: false }
    case 'dismissedRotate':
      return state.rotateDismissed ? state : { ...state, rotateDismissed: true }
    case 'reset':
      return createMobilePlayState(action.preferredOrientation, {
        coarse: state.coarse,
        screenOrientation: state.screenOrientation,
      })
  }
}

export const isMobilePlayActive = (state: Pick<MobilePlayState, 'mode'>): boolean => state.mode !== 'inline'

export const shouldLockPageScroll = (state: Pick<MobilePlayState, 'mode'>): boolean => state.mode !== 'inline'

// shouldShowRotatePrompt is true when mobile play is active, the example asks for an orientation the screen does not match, the browser could not lock orientation, and the player has not dismissed the prompt.
export function shouldShowRotatePrompt(state: MobilePlayState): boolean {
  if (state.mode === 'inline') return false
  if (state.preferredOrientation === 'any') return false
  if (state.orientationLocked) return false
  if (state.rotateDismissed) return false
  return state.screenOrientation !== state.preferredOrientation
}

export function hintKind(state: Pick<MobilePlayState, 'coarse' | 'running'>): 'touch' | 'keyboard' | 'none' {
  if (!state.running) return 'none'
  return state.coarse ? 'touch' : 'keyboard'
}

// ---- Browser capability & DOM helpers ----

export interface FullscreenElementLike {
  requestFullscreen?: () => Promise<void> | void
  webkitRequestFullscreen?: () => Promise<void> | void
}

export interface FullscreenDocumentLike {
  fullscreenEnabled?: boolean
  webkitFullscreenEnabled?: boolean
  fullscreenElement?: unknown
  webkitFullscreenElement?: unknown
  exitFullscreen?: () => Promise<void> | void
  webkitExitFullscreen?: () => Promise<void> | void
}

export interface OrientationLockLike {
  orientation?: {
    lock?: (orientation: 'landscape' | 'portrait') => Promise<void> | void
    unlock?: () => void
  }
}

export function fullscreenElementOf(doc: FullscreenDocumentLike | undefined): unknown {
  if (!doc) return null
  return doc.fullscreenElement ?? doc.webkitFullscreenElement ?? null
}

export function canRequestFullscreen(el: FullscreenElementLike | null | undefined, doc: FullscreenDocumentLike | undefined): boolean {
  if (!el || !doc) return false
  if (typeof el.requestFullscreen === 'function' && doc.fullscreenEnabled !== false) return true
  if (typeof el.webkitRequestFullscreen === 'function' && doc.webkitFullscreenEnabled !== false) return true
  return false
}

export async function requestStageFullscreen(el: FullscreenElementLike | null | undefined, doc: FullscreenDocumentLike | undefined): Promise<boolean> {
  if (!el || !doc) return false
  const method =
    typeof el.requestFullscreen === 'function' && doc.fullscreenEnabled !== false
      ? el.requestFullscreen.bind(el)
      : typeof el.webkitRequestFullscreen === 'function' && doc.webkitFullscreenEnabled !== false
        ? el.webkitRequestFullscreen.bind(el)
        : undefined
  if (!method) return false
  try {
    await method()
    return true
  } catch {
    return false
  }
}

export async function exitStageFullscreen(doc: FullscreenDocumentLike | undefined): Promise<void> {
  if (!doc || !fullscreenElementOf(doc)) return
  const method =
    typeof doc.exitFullscreen === 'function'
      ? doc.exitFullscreen.bind(doc)
      : typeof doc.webkitExitFullscreen === 'function'
        ? doc.webkitExitFullscreen.bind(doc)
        : undefined
  if (!method) return
  try {
    await method()
  } catch {
    // Ignore when the browser already exited fullscreen.
  }
}

export async function tryLockOrientation(orientation: PlaygroundOrientation, screenObj: OrientationLockLike | undefined): Promise<boolean> {
  if (orientation === 'any') return false
  const api = screenObj?.orientation
  const lock = api?.lock
  if (!api || typeof lock !== 'function') return false
  try {
    await lock.call(api, orientation)
    return true
  } catch {
    return false
  }
}

export function tryUnlockOrientation(screenObj: OrientationLockLike | undefined): void {
  const api = screenObj?.orientation
  const unlock = api?.unlock
  if (!api || typeof unlock !== 'function') return
  try {
    unlock.call(api)
  } catch {
    // Ignore when no lock is held or the browser refuses.
  }
}

// enterMobilePlay enters native fullscreen on the stage when supported and falls back to the fixed 100dvh overlay (as on iPhone Safari) when unavailable or refused.
export async function enterMobilePlay(
  el: FullscreenElementLike | null | undefined,
  doc: FullscreenDocumentLike | undefined,
  screenObj: OrientationLockLike | undefined,
  orientation: PlaygroundOrientation,
): Promise<{ mode: 'fullscreen' | 'overlay'; orientationLocked: boolean }> {
  const enteredFullscreen = await requestStageFullscreen(el, doc)
  if (enteredFullscreen) {
    const orientationLocked = await tryLockOrientation(orientation, screenObj)
    return { mode: 'fullscreen', orientationLocked }
  }
  return { mode: 'overlay', orientationLocked: false }
}

export function readCoarsePointer(win: Pick<Window, 'matchMedia'> | undefined): boolean {
  if (!win || typeof win.matchMedia !== 'function') return false
  return win.matchMedia('(pointer: coarse)').matches
}

export function readScreenOrientation(win: Pick<Window, 'innerWidth' | 'innerHeight' | 'matchMedia'> | undefined): ScreenOrientationKind {
  if (!win) return 'landscape'
  if (typeof win.matchMedia === 'function') {
    if (win.matchMedia('(orientation: landscape)').matches) return 'landscape'
    if (win.matchMedia('(orientation: portrait)').matches) return 'portrait'
  }
  return win.innerWidth > win.innerHeight ? 'landscape' : 'portrait'
}

export const PLAYING_CLASS = 'playground-playing'

export function lockPageScroll(doc: Document, win: Window): () => void {
  const savedY = win.scrollY
  doc.documentElement.classList.add(PLAYING_CLASS)
  return () => {
    doc.documentElement.classList.remove(PLAYING_CLASS)
    if (savedY > 0 && win.scrollY !== savedY) {
      win.scrollTo(0, savedY)
    }
  }
}

export const FRAME_TOUCH_STYLE_ID = 'nsgo-playground-mobile-touch'

// lockFrameTouch sets touch-action: none and overscroll-behavior: none inside a same-origin game iframe while mobile play mode is active, preventing browser pan and overscroll from cancelling thumb drags before NSGo's asynchronous ItemBlockNative arrives.
export function lockFrameTouch(doc: Document | null | undefined): () => void {
  if (!doc) return () => {}
  try {
    const existing = doc.getElementById(FRAME_TOUCH_STYLE_ID)
    if (existing) {
      return () => {
        try {
          existing.remove()
        } catch {
          // Ignore if the iframe navigated away.
        }
      }
    }
    const style = doc.createElement('style')
    style.id = FRAME_TOUCH_STYLE_ID
    style.textContent = 'html, body, * { touch-action: none !important; overscroll-behavior: none !important; }'
    ;(doc.head ?? doc.documentElement).appendChild(style)
    return () => {
      try {
        style.remove()
      } catch {
        // Ignore if the iframe navigated away.
      }
    }
  } catch {
    return () => {}
  }
}
