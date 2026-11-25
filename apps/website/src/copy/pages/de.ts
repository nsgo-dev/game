import type { PagesCopy } from './en.ts'

export const dePages: PagesCopy = {
  "nav": {
    "tutorial": "Tutorial",
    "packages": "Pakete",
    "examples": "Beispiele",
    "getStarted": "Loslegen",
    "search": "Suchen"
  },
  "home": {
    "eyebrow": "Frühes Stadium: APIs ändern sich noch",
    "title": [
      "Spiele in Go schreiben.",
      "Ein Code, sechs Plattformen."
    ],
    "lead": "game ist die Spieleschicht für NSGo: Fixed-Step-Loop, Eingabeaktionen, Sprites, Tilemaps, Szenen und Sound. Das Spiel zeichnet in ein Canvas; HUD und Menüs bleiben native NSGo-Views.",
    "start": "Loslegen",
    "playground": "Im Playground spielen",
    "live": "LIVE · WEB BUILD",
    "readouts": [
      "Fixed-Step-Loop",
      "Virtuelle Auflösung, skaliert",
      "Ein Frame mit 1000 Sprites"
    ],
    "platforms": "Ein Go-Code, gebaut für →",
    "consoles": "Konsolen (Switch, PlayStation, Xbox) sind nicht vorgesehen: Ihre SDKs stehen unter NDA und brauchen C-Toolchains, dieses Projekt ist reines Go.",
    "howTitle": "Das Spiel wird gezeichnet. Die Oberfläche ist nativ.",
    "how": [
      {
        "title": "Das Spiel in einer View zeichnen",
        "body": "Ein Spiel zeichnet in eine NSGo-View: ein Canvas, einen Pixelpuffer oder einen Shader. Loop, Sprite-Batches und Tilemaps arbeiten darum herum."
      },
      {
        "title": "Native Views für HUD und Menüs",
        "body": "Punktestand, Pausenmenü und Einstellungen sind gewöhnliche NSGo-Views über dem Spiel – mit nativer Schrift, Barrierefreiheit und Eingabemethoden."
      },
      {
        "title": "Nur linken, was du nutzt",
        "body": "Es nutzt nur die öffentliche API von NSGo. Eine App, die es nicht importiert, linkt nichts davon, und jedes Paket lässt sich einzeln importieren."
      }
    ],
    "hudNote": "HUD · native NSGo-Views",
    "canvasNote": "Spiel · core.Canvas",
    "stagesTitle": "Zwei Stufen, ein gemeinsames Fundament.",
    "stagesLead": "Loop, Eingabeaktionen, Sound und Szenenwechsel teilen sich beide Stufen – 3D ergänzt das Zeichnen, statt sie zu ersetzen.",
    "stages": [
      {
        "status": "Verfügbar",
        "body": "Fixed-Step-Loop, Eingabeaktionen, Sprites, Tilemaps, Szenen und Sound, gezeichnet mit 60 fps oder der Bildwiederholrate des Displays."
      },
      {
        "status": "In Arbeit",
        "body": "Meshes, Kameras, Lichter und glTF-Modelle mit Tiefe auf der GPU-View, die NSGo dafür ergänzt – Modelle lassen sich aus Blender importieren."
      }
    ],
    "codeTitle": "Eine Komponente ist ein Spiel.",
    "codeLead": "Ein Spiel ist eine NSGo-Komponente: Es bindet seine Aktionen einmal, startet einen game.Loop auf seinem Scope, schaltet die Aktionen in jedem festen Schritt weiter und zeichnet in Draw ein Canvas neu. Das ist das Gerüst von Cave Run, reduziert auf ein Sprite.",
    "codePoints": [
      "Fixed-Step-Updates, interpolierte Frames",
      "Tastatur und Gamepad auf derselben Aktion",
      "Hot Reload beim Speichern: go tool nsgo dev"
    ],
    "packagesTitle": "{n} Pakete. Importiere nur, was du brauchst.",
    "packagesLead": "Von Sprites bis zu dedizierten Servern: Jedes Paket hat eine Anleitung und ist als stabil oder experimentell markiert.",
    "packagesLink": "Alle Pakete ansehen",
    "examplesTitle": "Beispielspiele",
    "examplesLead": "Die Beispiele laufen auf Desktop, Mobilgeräten und im Web, der Quellcode liegt im Repository.",
    "allExamples": "Alle Beispiele ansehen",
    "fieldmedic": "Ein kleines Spiel mit eigenem Szeneneditor und Editor-Plugin.",
    "starCatcher": "Das Tutorial: neun Kapitel vom leeren Projekt bis zum fertigen Spiel.",
    "startTitle": "Drei Befehle bis zum ersten Spiel.",
    "steps": [
      "Projekt anlegen",
      "Lokal starten, Hot Reload beim Speichern",
      "Auf eine andere Plattform wechseln"
    ],
    "requirements": "Benötigt Go 1.27+ und NSGo v0.3.4+. iOS-Builds brauchen macOS und Xcode.",
    "migrate": "Umstieg von einer anderen Engine:",
    "finalTitle": "Noch früh – und schon genug für ein ganzes Spiel.",
    "finalLead": "Noch ist nichts veröffentlicht. Die API-Stabilitätsrichtlinie zeigt, welche Pakete stabil sind und welche sich noch ändern können.",
    "finalStart": "Erste Schritte lesen",
    "finalRoadmap": "Roadmap ansehen"
  },
  "categories": [
    "Kern",
    "Gameplay",
    "Grafik & UI",
    "Werkzeuge",
    "Online",
    "3D"
  ],
  "genres": {
    "caverun": "Plattformer",
    "cryptquest": "Dungeon-Abenteuer",
    "starburst": "Arena-Shooter",
    "fountain": "Partikel-Stresstest",
    "slidegem": "Match-3-Puzzle",
    "lantern3d": "2D ↔ 3D-Szenen"
  },
  "blurbs": {
    "caverun": "Ein Side-Scrolling-Plattformer: Ein Entdecker durchquert drei Höhlen, sammelt Edelsteine, springt über Schleime und Lava und erreicht das Ausgangsportal. Er zeichnet in ein Canvas; Titel-, Pause- und Endbildschirme sind native Views.",
    "cryptquest": "Ein Tile-Dungeon-Abenteuer von oben, auf allen sechs Zielplattformen.",
    "starburst": "Ein Twin-Stick-Arena-Shooter von oben, mit Partikeln, Physik und Eingabe.",
    "fountain": "Ein Partikel-Stresstest: Vier Fontänen halten 5000 lebende Partikel.",
    "slidegem": "Ein Touch-first-Match-3 auf 6×6 Feldern, ganz ohne externe Asset-Dateien.",
    "lantern3d": "Blendet zwischen einem 3D-Laternenschrein und einer 2D-Hofkarte über: ein glTF-Diorama, PBR-Beleuchtung und ein natives HUD, beide Szenen mit gemeinsamer Eingabe und gemeinsamem Sound."
  },
  "packages": {
    "title": "{n} Pakete. Importiere nur, was du brauchst.",
    "lead": "Von Sprites bis zu dedizierten Servern: Jedes Paket hat eine Anleitung und ist als stabil oder experimentell markiert.",
    "search": "Pakete durchsuchen",
    "all": "Alle",
    "guide": "Anleitung",
    "noMatch": "Keine passenden Pakete"
  },
  "examples": {
    "title": "Beispielspiele",
    "lead": "Die Beispiele laufen auf Desktop, Mobilgeräten und im Web, der Quellcode liegt im Repository.",
    "used": "Verwendete Pakete",
    "run": "Ausführen",
    "play": "Im Browser spielen",
    "source": "Quellcode ansehen",
    "lanternCaption": "Im Bild die 2D-Hofkarte. Zum 3D-Schrein: einfach spielen."
  },
  "tutorial": {
    "eyebrow": "Neun Kapitel",
    "start": "Mit Kapitel 1 beginnen",
    "source": "Quellcode ansehen",
    "run": "Kapitel aus dem Repository starten"
  },
  "footer": {
    "tagline": "Die Spieleschicht für NSGo, geschrieben in Go. 2D heute, 3D bald.",
    "columns": [
      "DOCS",
      "PROJECT",
      "COMMUNITY"
    ],
    "gettingStarted": "Erste Schritte",
    "tutorial": "Tutorial: Star Catcher",
    "packages": "Pakete",
    "stability": "API-Stabilität",
    "playground": "Playground",
    "examples": "Beispiele",
    "roadmap": "Roadmap",
    "changelog": "Changelog",
    "github": "GitHub",
    "nsgo": "NSGo",
    "contributing": "Mitwirken",
    "feedback": "Feedback",
    "license": "Apache-2.0-Lizenz · © 2026 NSGo",
    "llms": "Doku für LLMs (llms.txt)",
    "llmsFull": "Vollständige LLM-Doku (llms-full.txt)",
    "showcase": "Showcase"
  },
  "docsNotice": "Diese Seite ist auf Englisch verfügbar."
}
