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
    "eyebrow": "v0.1.0 · Pure Go",
    "title": [
      "Write games in Go.",
      "Ship one codebase to six platforms."
    ],
    "lead": "game is the 2D and 3D game layer for NSGo: a fixed-step loop, input actions, sprites, glTF rendering, a visual scene editor and plain-text YAML formats. Your game draws into one NSGo view; its HUD and menus stay native.",
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
    "stagesTitle": "2D and 3D on one shared foundation.",
    "stagesLead": "The loop, input actions, sound and scene switching are shared by both 2D and 3D, so you can mix scenes freely without rewriting your game.",
    "stages": [
      {
        "status": "Available",
        "body": "A fixed-step loop, input actions, sprites, tilemaps, scenes, physics, lighting and sound, drawn at 60 fps or the display’s refresh rate."
      },
      {
        "status": "Available",
        "body": "Meshes, cameras, PBR lighting, skeletal animation, 3D physics and glTF models on NSGo’s GPU view, with models imported from Blender."
      }
    ],
    "codeTitle": "One component is one game.",
    "codeLead": "A game is an NSGo component. It binds its actions once, runs a game.Loop on its scope, steps the actions each fixed update and redraws a Canvas from Draw. This is Cave Run’s skeleton, cut down to one sprite.",
    "codePoints": [
      "Fixed-step updates, interpolated draw frames",
      "Keys and gamepads bound to the same action",
      "Hot reload on save: go tool nsgo dev"
    ],
    "workflowTitle": "Plain-text projects. Visual editor. Built for AI agents.",
    "workflowLead": "Every scene, prefab, material, animation and data table is versioned YAML that humans and coding agents can read, diff and edit cleanly.",
    "workflow": [
      {
        "tag": "EDITOR",
        "title": "Visual 2D & 3D scene editor",
        "body": "Launch game editor for a live viewport, hierarchy, inspector, transform gizmos and asset browser — or extend it with custom Go plugins.",
        "link": "Explore the editor"
      },
      {
        "tag": "YAML",
        "title": "Plain-text, schema-validated files",
        "body": "No opaque binary scenes. Every format carries a version, rejects unknown fields, ships a JSON Schema and round-trips cleanly in Git.",
        "link": "Browse file formats"
      },
      {
        "tag": "TEMPLATES & AI",
        "title": "16 starter templates & MCP tools",
        "body": "Scaffold platformers, RPGs, roguelikes, 3D action, FPS or racing games with game new -template, and inspect live scenes via MCP and Agent Skills.",
        "link": "See all 16 templates"
      }
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
    "requirements": "Needs Go 1.27+ and NSGo v0.3.4+. iOS builds need macOS and Xcode.",
    "migrate": "Coming from another engine:",
    "finalTitle": "v0.1.0 is here. Build and ship your game in pure Go.",
    "finalLead": "Every package is documented and marked stable or experimental in the API stability policy. Start from a template or explore games built with NSGo Game.",
    "finalStart": "Read Getting Started",
    "finalShowcase": "Explore the Showcase"
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
    "tagline": "The pure-Go game layer for NSGo: 2D, 3D, visual scene editor and plain-text YAML across six platforms.",
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
    "editor": "Scene Editor",
    "templates": "Starter Templates",
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
