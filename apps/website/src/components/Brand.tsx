import type { CSSProperties, ReactNode } from 'react'
import darkLogo from '../assets/logo-dark.svg?url'
import lightLogo from '../assets/logo-light.svg?url'

// NSGo's mark, in the colors of the page's appearance, as nsgo.dev shows it.
export function Logo({ size = 26 }: { size?: number }) {
  return (
    <span className="logo" style={{ '--logo-size': `${size}px` } as CSSProperties}>
      <img className="logo--dark" src={darkLogo} width={size} height={size} alt="" draggable={false} />
      <img className="logo--light" src={lightLogo} width={size} height={size} alt="" draggable={false} />
    </span>
  )
}

// The three speed lines of NSGo's mark, the site's motif.
export function SpeedLines({ size = 14, className }: { size?: number; className?: string }) {
  return (
    <svg className={className ? `speed-lines ${className}` : 'speed-lines'} viewBox="-9 40 40 44" width={size} height={size} aria-hidden="true" focusable="false">
      <g transform="skewX(-10)" fill="currentColor">
        <rect x="12.5" y="40.5" width="26" height="11" rx="5.5" />
        <rect x="2.5" y="56.5" width="36" height="11" rx="5.5" />
        <rect x="16.5" y="72.5" width="22" height="11" rx="5.5" />
      </g>
    </svg>
  )
}

export function Eyebrow({ children }: { children: ReactNode }) {
  return (
    <span className="eyebrow">
      <SpeedLines size={13} />
      {children}
    </span>
  )
}
