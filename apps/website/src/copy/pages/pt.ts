import type { PagesCopy } from './en.ts'

export const ptPages: PagesCopy = {
  "nav": {
    "tutorial": "Tutorial",
    "packages": "Pacotes",
    "examples": "Exemplos",
    "getStarted": "Começar",
    "search": "Buscar"
  },
  "home": {
    "eyebrow": "Fase inicial: as APIs vão mudar",
    "title": [
      "Crie jogos em Go.",
      "Um só código, seis plataformas."
    ],
    "lead": "game é a camada de jogos do NSGo: loop de passo fixo, ações de entrada, sprites, tilemaps, cenas e som. O jogo é desenhado em um Canvas; o HUD e os menus continuam sendo views nativas do NSGo.",
    "start": "Começar",
    "playground": "Jogar no Playground",
    "live": "LIVE · WEB BUILD",
    "readouts": [
      "Loop de passo fixo",
      "Resolução virtual escalonada",
      "Um quadro com 1000 sprites"
    ],
    "platforms": "Um só código Go, compilado para →",
    "consoles": "Consoles (Switch, PlayStation, Xbox) estão fora do escopo: seus SDKs são protegidos por NDA e exigem toolchains em C, e este projeto é só Go.",
    "howTitle": "O jogo é desenhado. A interface é nativa.",
    "how": [
      {
        "title": "Desenhe o jogo em uma view",
        "body": "O jogo é desenhado em uma view do NSGo: um Canvas, um buffer de pixels ou um shader. O loop, os lotes de sprites e os tilemaps giram em torno dela."
      },
      {
        "title": "Views nativas para HUD e menus",
        "body": "Placar, menu de pausa e configurações são views comuns do NSGo sobre o jogo, com texto, acessibilidade e métodos de entrada nativos."
      },
      {
        "title": "Vincule só o que usar",
        "body": "Usa apenas a API pública do NSGo: um app que não o importa não vincula nada dele, e cada pacote pode ser importado sozinho."
      }
    ],
    "hudNote": "HUD · views nativas do NSGo",
    "canvasNote": "Jogo · core.Canvas",
    "stagesTitle": "Duas etapas, uma base comum.",
    "stagesLead": "O loop, as ações de entrada, o som e a troca de cenas são comuns às duas etapas, então o 3D acrescenta desenho sem substituí-los.",
    "stages": [
      {
        "status": "Disponível",
        "body": "Loop de passo fixo, ações de entrada, sprites, tilemaps, cenas e som, desenhados a 60 fps ou na taxa de atualização da tela."
      },
      {
        "status": "Em andamento",
        "body": "Malhas, câmeras, luzes e modelos glTF desenhados com profundidade na view de GPU que o NSGo adiciona para isso, com importação do Blender."
      }
    ],
    "codeTitle": "Um componente é um jogo.",
    "codeLead": "Um jogo é um componente do NSGo: vincula suas ações uma vez, roda um game.Loop no seu escopo, avança as ações a cada passo fixo e redesenha um Canvas no Draw. Este é o esqueleto do Cave Run, reduzido a um sprite.",
    "codePoints": [
      "Atualizações de passo fixo, quadros interpolados",
      "Teclado e controle na mesma ação",
      "Hot reload ao salvar: go tool nsgo dev"
    ],
    "packagesTitle": "{n} pacotes. Importe só o que precisar.",
    "packagesLead": "De sprites a servidores dedicados, cada pacote tem um guia e é marcado como estável ou experimental.",
    "packagesLink": "Ver todos os pacotes",
    "examplesTitle": "Jogos de exemplo",
    "examplesLead": "Os exemplos rodam no desktop, no celular e na web, com o código no repositório.",
    "allExamples": "Ver todos os exemplos",
    "fieldmedic": "Um pequeno jogo que traz seu próprio editor de cenas e um plugin de editor.",
    "starCatcher": "O tutorial: nove capítulos, do projeto vazio ao jogo publicado.",
    "startTitle": "Três comandos até o seu primeiro jogo.",
    "steps": [
      "Crie um projeto",
      "Rode aqui, hot reload ao salvar",
      "Troque de plataforma"
    ],
    "requirements": "Requer Go 1.27+ e NSGo v0.3.0+. Builds para iOS exigem macOS e Xcode.",
    "migrate": "Vindo de outra engine:",
    "finalTitle": "Ainda no começo, e já dá para lançar um jogo inteiro.",
    "finalLead": "Nada foi lançado ainda. A política de estabilidade da API mostra quais pacotes são estáveis e quais ainda podem mudar.",
    "finalStart": "Ler o guia inicial",
    "finalRoadmap": "Ver o roadmap"
  },
  "categories": [
    "Núcleo",
    "Gameplay",
    "Gráficos e UI",
    "Ferramentas",
    "Online",
    "3D"
  ],
  "genres": {
    "caverun": "Plataforma",
    "cryptquest": "Aventura em masmorra",
    "starburst": "Shooter de arena",
    "fountain": "Teste de partículas",
    "slidegem": "Quebra-cabeça match-3",
    "lantern3d": "Cenas 2D ↔ 3D"
  },
  "blurbs": {
    "caverun": "Um jogo de plataforma com rolagem lateral: um explorador atravessa três cavernas, coleta gemas, pula slimes e lava e chega ao portal de saída. É desenhado em um Canvas; as telas de título, pausa e fim são views nativas.",
    "cryptquest": "Uma aventura em masmorra com tiles vista de cima, nos seis alvos.",
    "starburst": "Um shooter de arena visto de cima, com dois analógicos, partículas, física e entrada.",
    "fountain": "Teste de estresse: quatro fontes mantêm 5000 partículas vivas.",
    "slidegem": "Um match-3 deslizante 6×6 feito para toque, sem nenhum arquivo de asset externo.",
    "lantern3d": "Alterna com um fade entre um santuário de lanternas em 3D e um mapa do pátio em 2D: um diorama glTF, iluminação PBR e um HUD nativo, com as duas cenas compartilhando entrada e som."
  },
  "packages": {
    "title": "{n} pacotes. Importe só o que precisar.",
    "lead": "De sprites a servidores dedicados, cada pacote tem um guia e é marcado como estável ou experimental.",
    "search": "Buscar pacotes",
    "all": "Todos",
    "guide": "Guia",
    "noMatch": "Nenhum pacote encontrado"
  },
  "examples": {
    "title": "Jogos de exemplo",
    "lead": "Os exemplos rodam no desktop, no celular e na web, com o código no repositório.",
    "used": "Pacotes usados",
    "run": "Executar",
    "play": "Jogar no navegador",
    "source": "Ver código",
    "lanternCaption": "Na imagem, o mapa 2D do pátio. Jogue para ver o santuário 3D."
  },
  "tutorial": {
    "eyebrow": "Nove capítulos",
    "start": "Comece pelo capítulo 1",
    "source": "Ver código",
    "run": "Rode os capítulos a partir do repositório"
  },
  "footer": {
    "tagline": "A camada de jogos do NSGo, escrita em Go. 2D hoje, 3D a caminho.",
    "columns": [
      "DOCS",
      "PROJECT",
      "COMMUNITY"
    ],
    "gettingStarted": "Primeiros passos",
    "tutorial": "Tutorial: Star Catcher",
    "packages": "Pacotes",
    "stability": "Estabilidade da API",
    "playground": "Playground",
    "examples": "Exemplos",
    "roadmap": "Roadmap",
    "changelog": "Changelog",
    "github": "GitHub",
    "nsgo": "NSGo",
    "contributing": "Como contribuir",
    "feedback": "Feedback",
    "license": "Licença Apache-2.0 · © 2026 NSGo",
    "llms": "Docs para LLMs (llms.txt)",
    "llmsFull": "Docs completos para LLMs (llms-full.txt)",
    "showcase": "Vitrine"
  },
  "docsNotice": "Esta página está disponível em inglês."
}
