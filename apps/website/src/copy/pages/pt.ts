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
    "eyebrow": "v0.1.0 · Go puro",
    "title": [
      "Crie jogos em Go.",
      "Um só código, seis plataformas."
    ],
    "lead": "game é a camada de jogos 2D e 3D do NSGo: loop de passo fixo, ações de entrada, sprites, renderização glTF, editor visual de cenas e formatos YAML em texto simples. O jogo é desenhado em uma view do NSGo; o HUD e os menus continuam nativos.",
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
    "stagesTitle": "2D e 3D sobre uma base comum.",
    "stagesLead": "O loop, as ações de entrada, o som e a troca de cenas são compartilhados por 2D e 3D, permitindo combinar cenas livremente sem reescrever o jogo.",
    "stages": [
      {
        "status": "Disponível",
        "body": "Loop de passo fixo, ações de entrada, sprites, tilemaps, cenas, física, iluminação e som, desenhados a 60 fps ou na taxa de atualização da tela."
      },
      {
        "status": "Disponível",
        "body": "Malhas, câmeras, iluminação PBR, animação esquelética, física 3D e modelos glTF na view de GPU do NSGo, com importação direta do Blender."
      }
    ],
    "codeTitle": "Um componente é um jogo.",
    "codeLead": "Um jogo é um componente do NSGo: vincula suas ações uma vez, roda um game.Loop no seu escopo, avança as ações a cada passo fixo e redesenha um Canvas no Draw. Este é o esqueleto do Cave Run, reduzido a um sprite.",
    "codePoints": [
      "Atualizações de passo fixo, quadros interpolados",
      "Teclado e controle na mesma ação",
      "Hot reload ao salvar: go tool nsgo dev"
    ],
    "workflowTitle": "Projetos em texto simples. Editor visual. Pronto para agentes de IA.",
    "workflowLead": "Cada cena, prefab, material, animação e tabela de dados é um arquivo YAML versionado que pessoas e agentes de código podem ler, comparar e editar com clareza.",
    "workflow": [
      {
        "tag": "EDITOR",
        "title": "Editor visual de cenas 2D e 3D",
        "body": "Execute game editor para abrir um viewport ao vivo, hierarquia, inspetor, gizmos de transformação e navegador de assets — extensível com plugins em Go.",
        "link": "Explorar o editor"
      },
      {
        "tag": "YAML",
        "title": "Arquivos em texto com validação por esquema",
        "body": "Sem cenas binárias opacas. Cada formato tem versão, rejeita campos desconhecidos, inclui JSON Schema e mantém diffs limpos no Git.",
        "link": "Ver formatos de arquivo"
      },
      {
        "tag": "MODELOS E IA",
        "title": "16 modelos iniciais e ferramentas MCP",
        "body": "Crie jogos de plataforma, RPG, roguelike, ação 3D, FPS ou corrida com game new -template e inspecione cenas em execução via MCP e Agent Skills.",
        "link": "Ver todos os 16 modelos"
      }
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
    "requirements": "Requer Go 1.27+ e NSGo v0.3.4+. Builds para iOS exigem macOS e Xcode.",
    "migrate": "Vindo de outra engine:",
    "finalTitle": "v0.1.0 chegou. Crie e publique seu jogo em Go puro.",
    "finalLead": "Cada pacote tem um guia e está marcado como estável ou experimental na política de estabilidade da API. Comece por um modelo ou explore a vitrine.",
    "finalStart": "Ler o guia inicial",
    "finalShowcase": "Explorar a vitrine"
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
    "tagline": "A camada de jogos em Go puro para o NSGo: 2D, 3D, editor visual de cenas e YAML em texto simples em seis plataformas.",
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
    "editor": "Editor de cenas",
    "templates": "Modelos iniciais",
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
