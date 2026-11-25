import type { CSSProperties } from 'react'

// A segmented bar, a segment per issue, filled for the closed ones. Decorative: its numbers are given in words beside it.
export function Segments({ closed, total }: { closed: number; total: number }) {
  return (
    <span className="segments" style={{ '--segments': total } as CSSProperties} aria-hidden="true">
      {Array.from({ length: total }, (_, i) => (
        <span key={i} className={i < closed ? 'is-on' : undefined} />
      ))}
    </span>
  )
}
