import { useEffect, useRef } from 'react'
import type { FeaturedExample } from '../content/featured'

const asset = (file: string) => `${import.meta.env.BASE_URL}${file}`

// ExampleMedia shows a featured example's recording, playing muted while it is on screen and the reader allows motion, or its still. Without script, or with less motion asked for, the recording's first frame stands in for it.
export function ExampleMedia({ example, className, playing = true, eager = false }: { example: FeaturedExample; className?: string; playing?: boolean; eager?: boolean }) {
  const video = useRef<HTMLVideoElement>(null)
  const classes = [className, example.pixelated && 'pixelated'].filter(Boolean).join(' ') || undefined

  useEffect(() => {
    const element = video.current
    if (!element) return
    const still = window.matchMedia('(prefers-reduced-motion: reduce)')
    let visible = false
    const update = () => {
      if (visible && playing && !still.matches) void element.play().catch(() => {})
      else element.pause()
    }
    const observer = new IntersectionObserver((entries) => {
      visible = entries.some((entry) => entry.isIntersecting)
      update()
    })
    observer.observe(element)
    still.addEventListener('change', update)
    update()
    return () => {
      observer.disconnect()
      still.removeEventListener('change', update)
    }
  }, [playing])

  if (example.video) {
    return (
      <video
        ref={video}
        className={classes}
        src={asset(example.video)}
        poster={example.poster ? asset(example.poster) : undefined}
        muted
        loop
        playsInline
        preload={eager ? 'auto' : 'none'}
        aria-label={example.title}
      />
    )
  }
  return <img className={classes} src={asset(example.image ?? '')} alt={example.title} loading={eager ? 'eager' : 'lazy'} />
}

export const mediaUrl = asset
