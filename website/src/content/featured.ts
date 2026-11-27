// The example games the home and examples pages feature, by their directory under examples/, with the recording or still each shows from public/media/. Those the playground builds get a play button (content/playground.ts); the packages each imports are read from its source at build time (plugins/site.ts). This file imports nothing, so the Vite plugin can read it too.

export const FEATURED_IDS = ['caverun', 'cryptquest', 'starburst', 'fountain', 'slidegem', 'lantern3d'] as const

export type FeaturedId = (typeof FEATURED_IDS)[number]

export interface FeaturedExample {
  id: FeaturedId
  title: string
  // video is a looping recording of the game's web build, with poster as its first frame; image is a still.
  video?: string
  poster?: string
  image?: string
  // pixelated is set for pixel art, scaled without smoothing.
  pixelated?: boolean
}

export const FEATURED: readonly FeaturedExample[] = [
  { id: 'caverun', title: 'Cave Run', video: 'media/caverun.mp4', poster: 'media/caverun-poster.webp', pixelated: true },
  { id: 'cryptquest', title: 'CryptQuest', video: 'media/cryptquest.mp4', poster: 'media/cryptquest-poster.webp', pixelated: true },
  { id: 'starburst', title: 'Starburst', video: 'media/starburst.mp4', poster: 'media/starburst-poster.webp' },
  { id: 'fountain', title: 'Fountain', video: 'media/fountain.mp4', poster: 'media/fountain-poster.webp' },
  { id: 'slidegem', title: 'SlideGem', image: 'media/slidegem.webp' },
  { id: 'lantern3d', title: 'Lantern 3D', image: 'media/lantern3d.webp' },
]

export const featured = (id: FeaturedId): FeaturedExample => FEATURED.find((e) => e.id === id)!
