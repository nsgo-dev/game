import type { PagesCopy } from './en.ts'

export const esPages: PagesCopy = {
  "nav": {
    "tutorial": "Tutorial",
    "packages": "Paquetes",
    "examples": "Ejemplos",
    "getStarted": "Empezar",
    "search": "Buscar"
  },
  "home": {
    "eyebrow": "v0.1.0 · Go puro",
    "title": [
      "Crea juegos en Go.",
      "Un solo código, seis plataformas."
    ],
    "lead": "game es la capa de juegos 2D y 3D de NSGo: bucle de paso fijo, acciones de entrada, sprites, renderizado glTF, editor visual de escenas y formatos YAML en texto plano. El juego se dibuja en una vista NSGo; su HUD y menús siguen siendo nativos.",
    "start": "Empezar",
    "playground": "Probar el Playground",
    "live": "LIVE · WEB BUILD",
    "readouts": [
      "Bucle de paso fijo",
      "Resolución virtual escalada",
      "Un fotograma con 1000 sprites"
    ],
    "platforms": "Un solo código Go, compilado para →",
    "consoles": "Las consolas (Switch, PlayStation, Xbox) quedan fuera del alcance: sus SDK están bajo NDA y requieren herramientas en C, y este proyecto es solo Go.",
    "howTitle": "El juego se dibuja. La interfaz es nativa.",
    "how": [
      {
        "title": "Dibuja el juego en una vista",
        "body": "El juego se dibuja en una vista de NSGo: un Canvas, un búfer de píxeles o un shader. El bucle, los lotes de sprites y los mapas de tiles giran en torno a ella."
      },
      {
        "title": "Vistas nativas para HUD y menús",
        "body": "Puntuaciones, menús de pausa y ajustes son vistas normales de NSGo sobre el juego, con texto, accesibilidad y métodos de entrada nativos."
      },
      {
        "title": "Enlaza solo lo que usas",
        "body": "Solo usa la API pública de NSGo: una app que no lo importa no enlaza nada de él, y cada paquete se importa por separado."
      }
    ],
    "hudNote": "HUD · vistas nativas de NSGo",
    "canvasNote": "Juego · core.Canvas",
    "stagesTitle": "2D y 3D sobre una misma base.",
    "stagesLead": "El bucle, las acciones de entrada, el sonido y el cambio de escena se comparten entre 2D y 3D para combinar escenas libremente sin reescribir el juego.",
    "stages": [
      {
        "status": "Disponible",
        "body": "Bucle de paso fijo, acciones de entrada, sprites, mapas de tiles, escenas, física, iluminación y sonido a 60 fps o a la frecuencia de la pantalla."
      },
      {
        "status": "Disponible",
        "body": "Mallas, cámaras, iluminación PBR, animación esquelética, física 3D y modelos glTF sobre la vista GPU de NSGo, con importación desde Blender."
      }
    ],
    "codeTitle": "Un componente es un juego.",
    "codeLead": "Un juego es un componente de NSGo: vincula sus acciones una vez, ejecuta un game.Loop en su ámbito, avanza las acciones en cada paso fijo y redibuja un Canvas desde Draw. Este es el esqueleto de Cave Run, reducido a un sprite.",
    "codePoints": [
      "Actualizaciones de paso fijo, fotogramas interpolados",
      "Teclado y mando vinculados a la misma acción",
      "Recarga en caliente al guardar: go tool nsgo dev"
    ],
    "workflowTitle": "Proyectos en texto plano. Editor visual. Listo para agentes de IA.",
    "workflowLead": "Cada escena, prefab, material, animación y tabla de datos es YAML versionado que tanto personas como agentes de código pueden leer, comparar y editar con claridad.",
    "workflow": [
      {
        "tag": "EDITOR",
        "title": "Editor visual de escenas 2D y 3D",
        "body": "Ejecuta game editor para abrir un viewport en vivo, jerarquía, inspector, gizmos de transformación y explorador de recursos, extensible con plugins en Go.",
        "link": "Explorar el editor"
      },
      {
        "tag": "YAML",
        "title": "Archivos en texto plano con esquema",
        "body": "Sin escenas binarias opacas. Cada formato incluye versión, rechaza campos desconocidos, trae JSON Schema y conserva diffs limpios en Git.",
        "link": "Ver formatos de archivo"
      },
      {
        "tag": "PLANTILLAS E IA",
        "title": "16 plantillas iniciales y herramientas MCP",
        "body": "Genera plataformas, RPG, roguelikes, acción 3D, FPS o carreras con game new -template e inspecciona escenas en vivo con MCP y Agent Skills.",
        "link": "Ver las 16 plantillas"
      }
    ],
    "packagesTitle": "{n} paquetes. Importa solo lo que necesites.",
    "packagesLead": "De los sprites a los servidores dedicados, cada paquete tiene su guía y se marca como estable o experimental.",
    "packagesLink": "Ver todos los paquetes",
    "examplesTitle": "Juegos de ejemplo",
    "examplesLead": "Los ejemplos funcionan en escritorio, móvil y web, con su código en el repositorio.",
    "allExamples": "Ver todos los ejemplos",
    "fieldmedic": "Un pequeño juego que incluye su propio editor de escenas y un plugin de editor.",
    "starCatcher": "El tutorial: nueve capítulos, de un proyecto vacío a un juego publicado.",
    "startTitle": "Tres comandos para tu primer juego.",
    "steps": [
      "Crea un proyecto",
      "Ejecútalo aquí, recarga en caliente al guardar",
      "Cambia a otro destino"
    ],
    "requirements": "Requiere Go 1.27+ y NSGo v0.3.4+. Compilar para iOS requiere macOS y Xcode.",
    "migrate": "¿Vienes de otro motor?",
    "finalTitle": "v0.1.0 ya está aquí. Crea y publica tu juego en Go puro.",
    "finalLead": "Cada paquete tiene su guía y está marcado como estable o experimental en la política de estabilidad de la API. Empieza desde una plantilla o explora la galería.",
    "finalStart": "Leer la guía de inicio",
    "finalShowcase": "Explorar la galería"
  },
  "categories": [
    "Núcleo",
    "Jugabilidad",
    "Gráficos e IU",
    "Herramientas",
    "En línea",
    "3D"
  ],
  "genres": {
    "caverun": "Plataformas",
    "cryptquest": "Aventura de mazmorras",
    "starburst": "Shooter de arena",
    "fountain": "Prueba de partículas",
    "slidegem": "Puzle match-3",
    "lantern3d": "Escenas 2D ↔ 3D"
  },
  "blurbs": {
    "caverun": "Un plataformas de desplazamiento lateral: un explorador cruza tres cavernas, recoge gemas, salta babosas y lava y llega al portal de salida. Se dibuja en un Canvas; sus pantallas de título, pausa y final son vistas nativas.",
    "cryptquest": "Una aventura de mazmorras con tiles en vista cenital, en los seis destinos.",
    "starburst": "Un shooter de arena cenital de doble stick con partículas, física y entrada.",
    "fountain": "Prueba de estrés de partículas: cuatro fuentes mantienen 5000 partículas vivas.",
    "slidegem": "Un match-3 deslizante de 6×6 pensado para táctil, sin un solo archivo de recursos externo.",
    "lantern3d": "Pasa con un fundido entre un santuario de farolillos en 3D y un mapa del patio en 2D: un diorama glTF, iluminación PBR y un HUD nativo, con ambas escenas compartiendo entrada y sonido."
  },
  "packages": {
    "title": "{n} paquetes. Importa solo lo que necesites.",
    "lead": "De los sprites a los servidores dedicados, cada paquete tiene su guía y se marca como estable o experimental.",
    "search": "Buscar paquetes",
    "all": "Todos",
    "guide": "Guía",
    "noMatch": "No hay paquetes que coincidan"
  },
  "examples": {
    "title": "Juegos de ejemplo",
    "lead": "Los ejemplos funcionan en escritorio, móvil y web, con su código en el repositorio.",
    "used": "Paquetes usados",
    "run": "Ejecutar",
    "play": "Jugar en el navegador",
    "source": "Ver código",
    "lanternCaption": "En la imagen, el mapa 2D del patio. Juega para ver el santuario 3D."
  },
  "tutorial": {
    "eyebrow": "Nueve capítulos",
    "start": "Empieza por el capítulo 1",
    "source": "Ver código",
    "run": "Ejecuta los capítulos desde el repositorio"
  },
  "footer": {
    "tagline": "La capa de juegos en Go puro para NSGo: 2D, 3D, editor visual de escenas y YAML en texto plano para seis plataformas.",
    "columns": [
      "DOCS",
      "PROJECT",
      "COMMUNITY"
    ],
    "gettingStarted": "Primeros pasos",
    "tutorial": "Tutorial: Star Catcher",
    "packages": "Paquetes",
    "stability": "Estabilidad de la API",
    "playground": "Playground",
    "examples": "Ejemplos",
    "editor": "Editor de escenas",
    "templates": "Plantillas iniciales",
    "changelog": "Registro de cambios",
    "github": "GitHub",
    "nsgo": "NSGo",
    "contributing": "Cómo contribuir",
    "feedback": "Comentarios",
    "license": "Licencia Apache-2.0 · © 2026 NSGo",
    "llms": "Docs para LLM (llms.txt)",
    "llmsFull": "Docs completos para LLM (llms-full.txt)",
    "showcase": "Galería"
  },
  "docsNotice": "Esta página está disponible en inglés."
}
