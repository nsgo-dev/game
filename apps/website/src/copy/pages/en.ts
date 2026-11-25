// The words of the header, the footer and the home, tutorial, packages and examples pages in English, the canonical shape every other language under src/copy/pages/ matches. {n} in a title is the number of packages.

export const enPages = {
  "nav": {
    "tutorial": "Tutorial",
    "packages": "Packages",
    "examples": "Examples",
    "getStarted": "Get started",
    "search": "Search"
  },
  "home": {
    "eyebrow": "Early: APIs will change",
    "title": [
      "Write games in Go.",
      "Ship one codebase to six platforms."
    ],
    "lead": "game is the game layer for NSGo: a fixed-step loop, input actions, sprites, tilemaps, scenes and sound. Your game draws into one Canvas; its HUD and menus stay native NSGo views.",
    "start": "Get started",
    "playground": "Try the Playground",
    "live": "LIVE · WEB BUILD",
    "readouts": [
      "Fixed-step loop",
      "Virtual resolution, scaled",
      "A frame of 1000 sprites"
    ],
    "platforms": "One Go codebase, built for →",
    "consoles": "Consoles (Switch, PlayStation, Xbox) are out of scope: their SDKs are under NDA and need C toolchains, and this project is Go only.",
    "howTitle": "The game is drawn. The interface is native.",
    "how": [
      {
        "title": "Draw the game in one view",
        "body": "A game draws into one NSGo view: a Canvas, a pixel buffer or a shader. The loop, sprite batches and tilemaps all work around it."
      },
      {
        "title": "Native views for HUD and menus",
        "body": "Scores, pause menus and settings are ordinary NSGo views on top of the game, with native text, accessibility and input methods."
      },
      {
        "title": "Link only what you use",
        "body": "It uses only NSGo’s public API, so an app that does not import it links none of it, and every package imports on its own."
      }
    ],
    "hudNote": "HUD · native NSGo views",
    "canvasNote": "Game · core.Canvas",
    "stagesTitle": "Two stages, one shared foundation.",
    "stagesLead": "The loop, input actions, sound and scene switching are shared by both stages, so 3D adds drawing rather than replacing them.",
    "stages": [
      {
        "status": "Available",
        "body": "A fixed-step loop, input actions, sprites, tilemaps, scenes and sound, drawn at 60 fps or the display’s refresh rate."
      },
      {
        "status": "In progress",
        "body": "Meshes, cameras, lights and glTF models drawn with depth on the GPU view NSGo adds for it, with models imported from Blender."
      }
    ],
    "codeTitle": "One component is one game.",
    "codeLead": "A game is an NSGo component. It binds its actions once, runs a game.Loop on its scope, steps the actions each fixed update and redraws a Canvas from Draw. This is Cave Run’s skeleton, cut down to one sprite.",
    "codePoints": [
      "Fixed-step updates, interpolated draw frames",
      "Keys and gamepads bound to the same action",
      "Hot reload on save: go tool nsgo dev"
    ],
    "packagesTitle": "{n} packages. Import only what you need.",
    "packagesLead": "From sprites to dedicated servers, every package has a guide and is marked stable or experimental.",
    "packagesLink": "Browse all packages",
    "examplesTitle": "Example games",
    "examplesLead": "The examples run on desktop, mobile and the web, with their source in the repository.",
    "allExamples": "See all examples",
    "fieldmedic": "A small game that ships its own scene editor and editor plugin.",
    "starCatcher": "The tutorial: nine chapters from an empty project to a shipped game.",
    "startTitle": "Three commands to your first game.",
    "steps": [
      "Create a project",
      "Run it here, hot reload on save",
      "Switch to another target"
    ],
    "requirements": "Needs Go 1.27+ and NSGo v0.3.0+. iOS builds need macOS and Xcode.",
    "migrate": "Coming from another engine:",
    "finalTitle": "Early, and already enough to ship a whole game.",
    "finalLead": "Nothing is released yet. The API stability policy says which packages are stable and which may still change.",
    "finalStart": "Read Getting Started",
    "finalRoadmap": "See the roadmap"
  },
  "categories": [
    "Core",
    "Gameplay",
    "Graphics & UI",
    "Tooling",
    "Online",
    "3D"
  ],
  "genres": {
    "caverun": "Platformer",
    "cryptquest": "Dungeon adventure",
    "starburst": "Arena shooter",
    "fountain": "Particle stress test",
    "slidegem": "Match-3 puzzle",
    "lantern3d": "2D ↔ 3D scenes"
  },
  "blurbs": {
    "caverun": "A side-scrolling platformer: an explorer crosses three cave chambers, collects gems, hops over slimes and lava and reaches the exit portal. It draws into one Canvas; its title, pause and end screens are native views.",
    "cryptquest": "A top-down tile dungeon adventure across all six targets.",
    "starburst": "A top-down twin-stick arena shooter with particles, physics and input.",
    "fountain": "A particle stress scene: four fountains keep 5000 live particles.",
    "slidegem": "A touch-first 6×6 sliding match-3 with zero external asset files.",
    "lantern3d": "Fades between a 3D lantern shrine and a 2D courtyard map: a glTF diorama, PBR lighting and a native HUD, with both scenes sharing one set of input and sound."
  },
  "packages": {
    "title": "{n} packages. Import only what you need.",
    "lead": "From sprites to dedicated servers, every package has a guide and is marked stable or experimental.",
    "search": "Search packages",
    "all": "All",
    "guide": "Guide",
    "noMatch": "No matching packages"
  },
  "examples": {
    "title": "Example games",
    "lead": "The examples run on desktop, mobile and the web, with their source in the repository.",
    "used": "Packages used",
    "run": "Run it",
    "play": "Play in the browser",
    "source": "View source",
    "lanternCaption": "Shown: the 2D courtyard map. Play it to see the 3D shrine."
  },
  "tutorial": {
    "eyebrow": "Nine chapters",
    "start": "Start with chapter 1",
    "source": "View source",
    "run": "Run the chapters from this repository"
  },
  "footer": {
    "tagline": "The game layer for NSGo, written in Go. 2D today, 3D on the way.",
    "columns": [
      "DOCS",
      "PROJECT",
      "COMMUNITY"
    ],
    "gettingStarted": "Getting Started",
    "tutorial": "Tutorial: Star Catcher",
    "packages": "Packages",
    "stability": "API Stability",
    "playground": "Playground",
    "examples": "Examples",
    "roadmap": "Roadmap",
    "changelog": "Changelog",
    "github": "GitHub",
    "nsgo": "NSGo",
    "contributing": "Contributing",
    "feedback": "Feedback",
    "license": "Apache-2.0 licensed · © 2026 NSGo",
    "llms": "Docs for LLMs (llms.txt)",
    "llmsFull": "Full docs for LLMs (llms-full.txt)",
    "showcase": "Showcase"
  },
  "docsNotice": ""
}

export type PagesCopy = typeof enPages
