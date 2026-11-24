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
]
