// A 7x7 pixel mark: a small sprite, drawn as one path so it scales crisply.
const ROWS = ['..#.#..', '.#####.', '##.#.##', '#######', '#.#.#.#', '..#.#..', '.#...#.']

const PATH = ROWS.flatMap((row, y) => [...row].map((cell, x) => (cell === '#' ? `M${x} ${y}h1v1h-1z` : ''))).join('')

export function Logo({ size = 24 }: { size?: number }) {
  return (
    <svg className="logo" width={size} height={size} viewBox="0 0 7 7" aria-hidden="true" focusable="false" shapeRendering="crispEdges">
      <path d={PATH} fill="currentColor" />
    </svg>
  )
}
