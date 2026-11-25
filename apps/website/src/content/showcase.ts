import type { Lang } from '../locales.ts'
import { repoTreeUrl } from '../links.ts'

export type ShowcaseKind = 'example' | 'template' | 'community'
export type ShowcaseTarget = 'macOS' | 'iOS' | 'Windows' | 'Linux' | 'Android' | 'Web'

export interface ShowcaseEntry {
  id: string
  title: Record<Lang, string>
  description: Record<Lang, string>
  author: string
  kind: ShowcaseKind
  tags: string[]
  targets: ShowcaseTarget[]
  sourceUrl: string
  playUrl?: string
  license: string
}

const ALL_TARGETS: ShowcaseTarget[] = ['macOS', 'iOS', 'Windows', 'Linux', 'Android', 'Web']

const sameTitle = (title: string): Record<Lang, string> => ({
  en: title,
  zh: title,
  'zh-tw': title,
  ja: title,
  ko: title,
  es: title,
  fr: title,
  de: title,
  pt: title,
  ru: title,
})

// Games and starters built on nsgo-dev/game. Add community entries here once accepted via the showcase submission issue form.
export const SHOWCASE_ENTRIES: ShowcaseEntry[] = [
  {
    id: 'caverun',
    title: sameTitle('Cave Run'),
    description: {
      en: 'Side-scrolling 2D platformer demonstrating fixed-step physics, tilemap collision, sprite animations, and sound effects on a single Canvas.',
      zh: '横版 2D 平台跳跃示例，展示在单个 Canvas 上使用固定步长物理、瓦片地图碰撞、精灵动画和音效。',
      'zh-tw': '橫向 2D 平台跳躍範例，展示在單一 Canvas 上結合固定步長物理、瓦片地圖碰撞、精靈動畫與音效。',
      ja: '固定ステップ物理、タイルマップ衝突判定、スプライトアニメーション、効果音を単一の Canvas 上で示す横スクロール 2D プラットフォーマー。',
      ko: '단일 Canvas 위에서 고정 스텝 물리, 타일맵 충돌, 스프라이트 애니메이션, 효과음을 보여주는 횡스크롤 2D 플랫포머.',
      es: 'Juego de plataformas 2D de desplazamiento lateral que demuestra física de paso fijo, colisiones de mapas de tiles, animaciones de sprites y efectos de sonido en un solo Canvas.',
      fr: 'Jeu de plateforme 2D à défilement latéral illustrant la physique à pas fixe, les collisions de carte de tuiles, les animations de sprites et les effets sonores sur un seul Canvas.',
      de: '2D-Sidescrolling-Plattformer mit Festschritt-Physik, Tilemap-Kollision, Sprite-Animationen und Soundeffekten auf einem einzelnen Canvas.',
      pt: 'Plataforma 2D de rolagem lateral demonstrando física de passo fixo, colisão em tilemap, animações de sprites e efeitos sonoros em um único Canvas.',
      ru: '2D-платформер с боковой прокруткой, демонстрирующий физику с фиксированным шагом, столкновения на тайловой карте, спрайтовые анимации и звуковые эффекты на одном Canvas.',
    },
    author: 'NSGo',
    kind: 'example',
    tags: ['Platformer', 'Tilemap', 'Sprite', 'Sound'],
    targets: ALL_TARGETS,
    sourceUrl: repoTreeUrl('examples/caverun'),
    license: 'Apache-2.0',
  },
  {
    id: 'slidegem',
    title: sameTitle('SlideGem'),
    description: {
      en: 'Match-3 puzzle game with grid input, chained clear animations, score combos, and layered audio feedback under a native NSGo HUD.',
      zh: '三消益智游戏示例，结合网格输入、连锁消除动画、连击计分、分层音效以及原生 NSGo HUD。',
      'zh-tw': '三消益智遊戲範例，結合網格輸入、連鎖消除動畫、連擊計分、分層音效與原生 NSGo HUD。',
      ja: 'グリッド入力、連鎖消去アニメーション、コンボスコア、階層的なオーディオ演出をネイティブ NSGo HUD の下に統合したマッチ 3 パズル。',
      ko: '네이티브 NSGo HUD 아래에서 그리드 입력, 연쇄 제거 애니메이션, 콤보 점수, 다층 오디오 피드백을 결합한 매치-3 퍼즐 게임.',
      es: 'Juego de puzle match-3 con entrada en cuadrícula, animaciones de eliminación en cadena, combos de puntuación y audio por capas bajo un HUD nativo de NSGo.',
      fr: 'Jeu de réflexion match-3 combinant saisie sur grille, animations d’élimination en chaîne, combos de score et retour audio superposé sous un HUD NSGo natif.',
      de: 'Match-3-Puzzlespiel mit Rastereingabe, Ketten-Animationen, Punkte-Combos und mehrschichtigem Audio-Feedback unter einem nativen NSGo-HUD.',
      pt: 'Quebra-cabeça match-3 com entrada em grade, animações de eliminação em cadeia, combos de pontuação e áudio em camadas sob um HUD nativo do NSGo.',
      ru: 'Головоломка «три в ряд» с вводом по сетке, цепными анимациями, комбо-очками и многослойным звуком под нативным HUD NSGo.',
    },
    author: 'NSGo',
    kind: 'example',
    tags: ['Puzzle', 'Match-3', 'Sprite', 'Sound'],
    targets: ALL_TARGETS,
    sourceUrl: repoTreeUrl('examples/slidegem'),
    license: 'Apache-2.0',
  },
  {
    id: 'cryptquest',
    title: sameTitle('CryptQuest'),
    description: {
      en: 'Top-down dungeon crawler exercising camera tracking, tiled rooms, scene transitions, hit testing, and action-based controls.',
      zh: '俯视角地牢探险示例，演示相机跟随、瓦片房间、场景切换、命中测试与基于动作的输入控制。',
      'zh-tw': '俯視角地牢探險範例，展示攝影機跟隨、瓦片房間、場景切換、命中測試與基於動作的輸入控制。',
      ja: 'カメラ追従、タイル構成の部屋、シーン遷移、ヒットテスト、アクションベースの操作を実践する見下ろし型ダンジョン探索ゲーム。',
      ko: '카메라 추적, 타일 방 구성, 씬 전환, 히트 테스트, 액션 기반 조작을 다루는 탑다운 던전 크롤러.',
      es: 'Explorador de mazmorras con vista cenital que pone en práctica seguimiento de cámara, salas de tiles, transiciones de escena, hit testing y controles basados en acciones.',
      fr: 'Dungeon crawler en vue de dessus mettant en œuvre suivi de caméra, salles en tuiles, transitions de scènes, tests de contact et contrôles basés sur des actions.',
      de: 'Top-Down-Dungeon-Crawler für Kameraverfolgung, gekachelte Räume, Szenenwechsel, Hit-Testing und aktionsbasierte Steuerung.',
      pt: 'Dungeon crawler com visão de cima exercitando acompanhamento de câmera, salas em tiles, transições de cena, hit testing e controles baseados em ações.',
      ru: 'Подземное приключение с видом сверху: слежение камеры, комнаты из тайлов, переходы между сценами, проверка попаданий и управление на основе действий.',
    },
    author: 'NSGo',
    kind: 'example',
    tags: ['Dungeon', 'Top-Down', 'Tilemap', 'Scene'],
    targets: ALL_TARGETS,
    sourceUrl: repoTreeUrl('examples/cryptquest'),
    license: 'Apache-2.0',
  },
  {
    id: 'starburst',
    title: sameTitle('Starburst'),
    description: {
      en: 'Fast arcade space shooter stress-testing batched sprite rendering, pooled projectiles, particle bursts, and fixed-step responsiveness.',
      zh: '快节奏街机太空射击示例，用于验证精灵批量绘制、弹幕对象池、粒子爆发和固定步长响应。',
      'zh-tw': '快節奏街機太空射擊範例，用於驗證精靈批次繪製、彈幕物件池、粒子爆發與固定步長反應。',
      ja: 'スプライトの一括描画、弾のオブジェクトプール、パーティクルバースト、固定ステップの応答性を検証する高速アーケード宇宙シューター。',
      ko: '스프라이트 일괄 렌더링, 투사체 풀링, 파티클 폭발, 고정 스텝 반응성을 검증하는 빠른 아케이드 우주 슈팅 게임.',
      es: 'Shooter espacial arcade de ritmo rápido que pone a prueba el renderizado de sprites en lotes, proyectiles agrupados, ráfagas de partículas y respuesta en paso fijo.',
      fr: 'Jeu de tir spatial d’arcade rapide mettant à l’épreuve le rendu de sprites par lots, les projectiles en pool, les salves de particules et la réactivité à pas fixe.',
      de: 'Schneller Arcade-Weltraum-Shooter als Belastungstest für gebatchtes Sprite-Rendering, Projektil-Pooling, Partikel-Bursts und Festschritt-Reaktionszeit.',
      pt: 'Jogo de tiro espacial arcade acelerado testando renderização de sprites em lote, pool de projéteis, explosões de partículas e resposta em passo fixo.',
      ru: 'Динамичный аркадный космический шутер для проверки пакетной отрисовки спрайтов, пула снарядов, вспышек частиц и отзывчивости фиксированного шага.',
    },
    author: 'NSGo',
    kind: 'example',
    tags: ['Arcade', 'Shooter', 'Sprite', 'Sound'],
    targets: ALL_TARGETS,
    sourceUrl: repoTreeUrl('examples/starburst'),
    license: 'Apache-2.0',
  },
  {
    id: 'template',
    title: sameTitle('Starter Template'),
    description: {
      en: 'Minimal multi-target starter project wired with game.Loop, a Canvas view, native HUD overlay, and deterministic nsgotest setup.',
      zh: '最小化的多平台入门项目模板，已接好 game.Loop、Canvas 视图、原生 HUD 叠层以及确定性的 nsgotest 测试结构。',
      'zh-tw': '最小化的多平台入門專案範本，已接妥 game.Loop、Canvas 視圖、原生 HUD 疊層以及具確定性的 nsgotest 測試架構。',
      ja: 'game.Loop、Canvas ビュー、ネイティブ HUD オーバーレイ、決定論的な nsgotest 構成を備えた最小構成のマルチターゲットスタータープロジェクト。',
      ko: 'game.Loop, Canvas 뷰, 네이티브 HUD 오버레이, 결정론적 nsgotest 구성을 갖춘 최소형 멀티 타깃 스타터 프로젝트.',
      es: 'Proyecto inicial multiplataforma mínimo con game.Loop, una vista Canvas, HUD nativo superpuesto y configuración determinista de nsgotest.',
      fr: 'Projet de départ multi-cible minimal équipé de game.Loop, d’une vue Canvas, d’un HUD natif superposé et d’une configuration déterministe nsgotest.',
      de: 'Minimales Multi-Target-Starterprojekt mit game.Loop, Canvas-View, nativem HUD-Overlay und deterministischem nsgotest-Setup.',
      pt: 'Projeto inicial multiplataforma mínimo configurado com game.Loop, uma view Canvas, HUD nativo sobreposto e testes determinísticos com nsgotest.',
      ru: 'Минимальный мультиплатформенный стартовый шаблон с game.Loop, представлением Canvas, нативным HUD поверх него и детерминированной настройкой nsgotest.',
    },
    author: 'NSGo',
    kind: 'template',
    tags: ['Starter', 'Loop', 'HUD', 'nsgotest'],
    targets: ALL_TARGETS,
    sourceUrl: repoTreeUrl('template'),
    license: 'Apache-2.0',
  },
  {
    id: 'cards2d',
    title: sameTitle('Suit Ladders'),
    description: {
      en: 'Single-player card game template: drag cards between a draw pile, a hand, a discard pile and four suit ladders, with rules in code, the deck and table in YAML, numbered deals, saves, and nsgotest playthroughs to a win and a loss.',
      zh: '单人纸牌游戏模板：在抽牌堆、手牌、弃牌堆和四座花色梯子之间拖放卡牌，规则写在代码中，牌组和牌桌写在 YAML 中，带编号发牌、存档，以及用 nsgotest 打到一胜一负的完整对局测试。',
      'zh-tw': '單人紙牌遊戲範本：在抽牌堆、手牌、棄牌堆和四座花色梯子之間拖放卡牌，規則寫在程式碼中，牌組和牌桌寫在 YAML 中，具編號發牌、存檔，以及用 nsgotest 打到一勝一負的完整對局測試。',
      ja: '一人用カードゲームのテンプレート。山札、手札、捨て札と四つのスートのはしごの間でカードをドラッグし、ルールはコードに、デッキと卓は YAML に置き、番号付きの配り、セーブ、勝ちと負けまでを nsgotest で通しプレイするテストを備える。',
      ko: '1인용 카드 게임 템플릿: 뽑을 더미, 손패, 버린 더미와 네 개의 무늬 사다리 사이로 카드를 끌어 놓으며, 규칙은 코드에, 덱과 테이블은 YAML에 두고, 번호가 붙은 배분, 저장, nsgotest로 승리와 패배까지 끝까지 플레이하는 테스트를 갖춘다.',
      es: 'Plantilla de juego de cartas para un jugador: arrastra cartas entre un mazo, una mano, una pila de descarte y cuatro escaleras de palo, con reglas en código, mazo y mesa en YAML, repartos numerados, partidas guardadas y pruebas nsgotest que juegan hasta una victoria y una derrota.',
      fr: 'Modèle de jeu de cartes en solo : faites glisser des cartes entre une pioche, une main, une défausse et quatre échelles de couleur, avec les règles dans le code, le paquet et la table en YAML, des donnes numérotées, des sauvegardes et des parties nsgotest jouées jusqu’à une victoire et une défaite.',
      de: 'Vorlage für ein Kartenspiel für eine Person: Karten zwischen Nachziehstapel, Hand, Ablagestapel und vier Farbleitern ziehen, mit Regeln im Code, Deck und Tisch in YAML, nummerierten Partien, Spielständen und nsgotest-Durchläufen bis zu einem Sieg und einer Niederlage.',
      pt: 'Modelo de jogo de cartas para um jogador: arraste cartas entre um monte de compra, uma mão, uma pilha de descarte e quatro escadas de naipe, com regras em código, baralho e mesa em YAML, distribuições numeradas, salvamentos e partidas nsgotest jogadas até uma vitória e uma derrota.',
      ru: 'Шаблон пасьянса для одного игрока: перетаскивание карт между колодой, рукой, сбросом и четырьмя лестницами мастей, правила в коде, колода и стол в YAML, нумерованные раздачи, сохранения и прохождения nsgotest до победы и поражения.',
    },
    author: 'NSGo',
    kind: 'template',
    tags: ['Cards', 'Drag and drop', 'Save', 'nsgotest'],
    targets: ALL_TARGETS,
    sourceUrl: repoTreeUrl('template/cards2d/template'),
    license: 'Apache-2.0',
  },
  {
    id: 'template-rpg2d',
    title: sameTitle('Hollowbrook (RPG Template)'),
    description: {
      en: 'Top-down 2D RPG template: tile-by-tile maps in YAML, branching Yarn dialogue with portraits, an inventory with equipment, a quest, turn-based battles and three save slots, played through the app by its tests.',
      zh: '俯视角 2D RPG 模板：以 YAML 编写的逐格移动地图、带立绘的 Yarn 分支对话、带装备的背包、一个任务、回合制战斗和三个存档位，测试通过应用完整地玩一遍。',
      'zh-tw': '俯視角 2D RPG 範本：以 YAML 撰寫的逐格移動地圖、附立繪的 Yarn 分支對話、含裝備的背包、一個任務、回合制戰鬥與三個存檔欄位，測試透過應用完整地玩一遍。',
      ja: 'トップダウン 2D RPG テンプレート。YAML で書くマス目移動のマップ、立ち絵付きの Yarn 分岐会話、装備付きのインベントリ、クエスト、ターン制バトル、3 つのセーブスロットを備え、テストがアプリを通して遊び切ります。',
      ko: '탑다운 2D RPG 템플릿: YAML로 작성한 칸 단위 이동 맵, 초상화가 있는 Yarn 분기 대화, 장비가 포함된 인벤토리, 퀘스트, 턴제 전투, 세 개의 저장 슬롯을 갖추고 테스트가 앱을 통해 끝까지 플레이합니다.',
      es: 'Plantilla de RPG 2D cenital: mapas en YAML recorridos casilla a casilla, diálogos ramificados en Yarn con retratos, inventario con equipo, una misión, combates por turnos y tres ranuras de guardado, que sus pruebas juegan a través de la app.',
      fr: 'Modèle de RPG 2D en vue de dessus : cartes en YAML parcourues case par case, dialogues Yarn à embranchements avec portraits, inventaire avec équipement, une quête, combats au tour par tour et trois emplacements de sauvegarde, joué de bout en bout par ses tests via l’app.',
      de: 'Top-down-2D-RPG-Vorlage: Karten in YAML, Feld für Feld begehbar, verzweigte Yarn-Dialoge mit Porträts, Inventar mit Ausrüstung, eine Quest, rundenbasierte Kämpfe und drei Speicherplätze, von den Tests durch die App durchgespielt.',
      pt: 'Modelo de RPG 2D em visão de cima: mapas em YAML percorridos casa a casa, diálogos ramificados em Yarn com retratos, inventário com equipamento, uma missão, batalhas por turnos e três espaços de salvamento, jogado pelos testes através do app.',
      ru: 'Шаблон 2D RPG с видом сверху: карты в YAML с пошаговым перемещением по клеткам, ветвящиеся диалоги Yarn с портретами, инвентарь со снаряжением, квест, пошаговые бои и три слота сохранений; тесты проходят игру через приложение.',
    },
    author: 'NSGo',
    kind: 'template',
    tags: ['RPG', 'Tilemap', 'Dialogue', 'Save'],
    targets: ALL_TARGETS,
    sourceUrl: repoTreeUrl('template/rpg2d'),
    license: 'Apache-2.0',
  },
]
