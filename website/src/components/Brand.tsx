import { type ReactNode, useId } from 'react'

// NSGo's mark, its speed lines and the rounded G in NSGo's blues, with the site's name beside it.
export function Logo({ size = 30 }: { size?: number }) {
  const gradient = useId()
  return (
    <span className="logo">
      <svg width={size} height={size} viewBox="-16.51 -2.63 129.26 129.26" aria-hidden="true" focusable="false">
        <defs>
          <linearGradient id={gradient} x1="0" y1="0" x2="0" y2="1">
            <stop offset="0" stopColor="#b3dbff" />
            <stop offset="1" stopColor="#4da6ff" />
          </linearGradient>
        </defs>
        <g transform="skewX(-10)" fill="#7cc0ff">
          <rect x="12.5" y="40.5" width="26" height="11" rx="5.5" />
          <rect x="2.5" y="56.5" width="36" height="11" rx="5.5" />
          <rect x="16.5" y="72.5" width="22" height="11" rx="5.5" />
        </g>
        <g transform="skewX(-10)">
          <path
            d="M68 23H88A25 25 0 0 1 113 48V76A25 25 0 0 1 88 101H68A25 25 0 0 1 43 76V48A25 25 0 0 1 68 23ZM68 37A11 11 0 0 0 57 48V76A11 11 0 0 0 68 87H88A11 11 0 0 0 99 76V48A11 11 0 0 0 88 37Z"
            fill={`url(#${gradient})`}
          />
        </g>
      </svg>
      <span>
        NSGo <span className="logo__game">Game</span>
      </span>
    </span>
  )
}

// The three speed lines of NSGo's mark.
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
