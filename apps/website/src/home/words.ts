// The home page's own words, in every language of the site. Words the old pages already had in every language (the headline, the stages, the examples) come from src/copy/ and src/content/showcase.ts instead. {n} in a title stands for a number the page counts at build time.
import type { Lang } from '../locales.ts';

export interface Words {
	// Two short lines; the second takes the accent.
	headline: [string, string];
	start: string;
	play: string;
	ticker: string[];
	features: { eyebrow: string; title: string };
	// The loop, sprites and tilemaps, physics, sound, input, and shaders and lights, in this order.
	effects: [string, string][];
	code: { eyebrow: string; title: string; text: string };
	text: { eyebrow: string; title: string; text: string; items: [string, string][] };
	// Genres, game systems, multiplayer, 3D, tooling and reach, in this order.
	packages: { eyebrow: string; title: string; text: string; items: [string, string][] };
	examples: { eyebrow: string; title: string; text: string; source: string };
	end: { title: string; text: string; site: string };
}

const en: Words = {
	headline: ['Games', 'in Go.'],
	start: 'Get started',
	play: 'Playground',
	ticker: ['Games in Go', 'No cgo, no hand-written C', 'Fixed-step loop', 'Every file is text', '2D, then 3D', 'Six targets', 'Apache-2.0'],
	features: { eyebrow: 'The core', title: 'Six parts of every frame' },
	effects: [
		['Loop', 'Fixed-step updates on the display’s frames and one interpolated draw per frame, paused in the background.'],
		['Sprites and tilemaps', 'Atlases from TexturePacker and Aseprite, frame animations, Z-ordered batches, and Tiled maps drawn only where the camera sees.'],
		['Physics', 'Box and circle overlaps, MoveAndSlide against tiles, and a platformer controller with coyote time and wall jumps.'],
		['Sound', 'A mixer with saved master and group volumes, per-sound limits, and music that crossfades.'],
		['Input', 'Named actions and axes from keys, gamepads, touch regions, the mouse and pointer lock.'],
		['Shaders and lights', 'Kage shader passes over the frame, 2D lights with shadows, and seeded particles.'],
	],
	code: { eyebrow: 'Usage', title: 'A frame in Go', text: 'A game is an NSGo component. Its loop updates at a fixed rate and sets a signal its Canvas reads, so the canvas draws again without committing the view tree.' },
	text: {
		eyebrow: 'Formats',
		title: 'Every file you write is text',
		text: 'Scenes, prefabs, materials, input bindings and data tables are YAML that a person and an AI can read, edit and diff in review.',
		items: [
			['Versioned schemas', 'Every format has a version field and refuses unknown fields; JSON Schemas let editors and AI tools check it.'],
			['Stable for review', 'Fixed key order and no IDs that change on every save, so a load and a save change nothing.'],
			['Other tools as they write', 'Tiled and LDtk maps, Aseprite and TexturePacker atlases and glTF are read as those tools export them.'],
		],
	},
	packages: {
		eyebrow: 'Packages',
		title: '{n} packages, one module',
		text: 'Each is its own import of github.com/nsgo-dev/game/packages/game, documented and tested; an app links only the ones it imports.',
		items: [
			['Genres', 'Platformers, RTS crowds, fighting moves, card boards, rhythm charts, visual novels and idle numbers.'],
			['Game systems', 'Inventories, abilities, quests, loot tables, stats, achievements and versioned save slots.'],
			['Multiplayer', 'WebSocket and UDP transports, rollback netcode, dedicated servers, accounts, cloud saves and leaderboards.'],
			['3D', 'Scene trees, cameras, animation blending, terrain, voxel worlds and a GPU renderer with instancing.'],
			['Tooling', 'The game command, a scene editor, MCP tools for agents and replays that test a game deterministically.'],
			['Reach', 'Accessibility options, localization with plurals and right-to-left text, and quality presets per device.'],
		],
	},
	examples: { eyebrow: 'Examples', title: 'Play them in your browser', text: 'Sample games built for the web, with their source beside them.', source: 'Source' },
	end: { title: 'Built for NSGo', text: 'The game layer uses only NSGo’s public API. NSGo is a cross-platform native UI engine with a Go backend.', site: 'NSGo website' },
};

const zh: Words = {
	headline: ['用 Go，', '写游戏。'],
	start: '开始使用',
	play: '在线试玩',
	ticker: ['用 Go 写游戏', '不用 cgo，不写 C', '固定步长循环', '每个文件都是文本', '先 2D，再 3D', '六个目标平台', 'Apache-2.0'],
	features: { eyebrow: '核心', title: '每一帧的六个部分' },
	effects: [
		['循环', '在显示帧上按固定步长更新，每帧插值绘制一次，应用在后台时自动暂停。'],
		['精灵与瓦片地图', '读取 TexturePacker 和 Aseprite 图集，帧动画、按 Z 排序的批次，以及只绘制相机可见部分的 Tiled 地图。'],
		['物理', '方形和圆形重叠检测、贴着瓦片的 MoveAndSlide，以及带土狼时间和蹬墙跳的平台跳跃控制器。'],
		['声音', '混音器保存主音量和分组音量，限制每个声音的同时播放数，音乐可以交叉淡入淡出。'],
		['输入', '把按键、手柄、触摸区域、鼠标和指针锁定读作命名的动作和轴。'],
		['着色器与光照', '在画面上叠加 Kage 着色器，带阴影的 2D 光照，以及可复现的粒子。'],
	],
	code: { eyebrow: '用法', title: '用 Go 写一帧', text: '游戏就是一个 NSGo 组件。它的循环按固定步长更新，并设置一个 Canvas 读取的信号，画布重绘时不必提交视图树。' },
	text: {
		eyebrow: '格式',
		title: '你写的每个文件都是文本',
		text: '场景、预制体、材质、输入绑定和数据表都是 YAML，人和 AI 都能阅读、编辑，并在评审时对比差异。',
		items: [
			['带版本的结构', '每种格式都有版本字段并拒绝未知字段；JSON Schema 让编辑器和 AI 工具能够检查它。'],
			['便于评审', '键的顺序固定，没有每次保存都变的 ID，读入再保存不会改动任何内容。'],
			['其他工具原样读取', 'Tiled 和 LDtk 地图、Aseprite 和 TexturePacker 图集以及 glTF，都按这些工具导出的样子读取。'],
		],
	},
	packages: {
		eyebrow: '包',
		title: '{n} 个包，一个模块',
		text: '每个包都是 github.com/nsgo-dev/game/packages/game 下单独的导入路径，有文档、有测试；应用只链接它导入的包。',
		items: [
			['游戏类型', '平台跳跃、RTS 群体、格斗招式、卡牌桌面、节奏谱面、视觉小说和放置游戏的大数。'],
			['游戏系统', '背包、技能、任务、掉落表、属性、成就和带版本的存档槽。'],
			['多人游戏', 'WebSocket 和 UDP 传输、回滚网络同步、专用服务器、账号、云存档和排行榜。'],
			['3D', '场景树、相机、动画混合、地形、体素世界，以及支持实例化的 GPU 渲染器。'],
			['工具', 'game 命令、场景编辑器、供智能体使用的 MCP 工具，以及确定性测试游戏的回放。'],
			['覆盖更多玩家', '无障碍选项、带复数规则和从右到左文字的本地化，以及按设备选择的画质预设。'],
		],
	},
	examples: { eyebrow: '示例', title: '在浏览器里试玩', text: '为网页构建的示例游戏，源码就在旁边。', source: '源码' },
	end: { title: '为 NSGo 而建', text: '游戏层只使用 NSGo 的公开 API。NSGo 是以 Go 为后端的跨平台原生 UI 引擎。', site: 'NSGo 官网' },
};

const zhTw: Words = {
	headline: ['用 Go，', '寫遊戲。'],
	start: '開始使用',
	play: '線上試玩',
	ticker: ['用 Go 寫遊戲', '不用 cgo，不寫 C', '固定步長迴圈', '每個檔案都是文字', '先 2D，再 3D', '六個目標平台', 'Apache-2.0'],
	features: { eyebrow: '核心', title: '每一幀的六個部分' },
	effects: [
		['迴圈', '在顯示幀上以固定步長更新，每幀內插繪製一次，應用程式在背景時自動暫停。'],
		['精靈與瓦片地圖', '讀取 TexturePacker 與 Aseprite 圖集，幀動畫、依 Z 排序的批次，以及只繪製相機可見部分的 Tiled 地圖。'],
		['物理', '方形與圓形重疊偵測、貼著瓦片的 MoveAndSlide，以及帶有土狼時間與蹬牆跳的平台跳躍控制器。'],
		['聲音', '混音器保存主音量與群組音量，限制每個聲音的同時播放數，音樂可以交叉淡入淡出。'],
		['輸入', '把按鍵、手把、觸控區域、滑鼠與指標鎖定讀作具名的動作與軸。'],
		['著色器與光照', '在畫面上疊加 Kage 著色器，帶陰影的 2D 光照，以及可重現的粒子。'],
	],
	code: { eyebrow: '用法', title: '用 Go 寫一幀', text: '遊戲就是一個 NSGo 元件。它的迴圈以固定步長更新，並設定一個 Canvas 讀取的訊號，畫布重繪時不必提交視圖樹。' },
	text: {
		eyebrow: '格式',
		title: '你寫的每個檔案都是文字',
		text: '場景、預製物、材質、輸入綁定與資料表都是 YAML，人和 AI 都能閱讀、編輯，並在審查時比對差異。',
		items: [
			['帶版本的結構', '每種格式都有版本欄位並拒絕未知欄位；JSON Schema 讓編輯器與 AI 工具能夠檢查它。'],
			['便於審查', '鍵的順序固定，沒有每次儲存都會變的 ID，讀入再儲存不會改動任何內容。'],
			['其他工具原樣讀取', 'Tiled 與 LDtk 地圖、Aseprite 與 TexturePacker 圖集以及 glTF，都依這些工具匯出的樣子讀取。'],
		],
	},
	packages: {
		eyebrow: '套件',
		title: '{n} 個套件，一個模組',
		text: '每個套件都是 github.com/nsgo-dev/game/packages/game 下獨立的匯入路徑，有文件、有測試；應用程式只連結它匯入的套件。',
		items: [
			['遊戲類型', '平台跳躍、RTS 群體、格鬥招式、卡牌桌面、節奏譜面、視覺小說與放置遊戲的大數。'],
			['遊戲系統', '背包、技能、任務、掉落表、屬性、成就與帶版本的存檔欄位。'],
			['多人遊戲', 'WebSocket 與 UDP 傳輸、回滾網路同步、專用伺服器、帳號、雲端存檔與排行榜。'],
			['3D', '場景樹、相機、動畫混合、地形、體素世界，以及支援實例化的 GPU 算繪器。'],
			['工具', 'game 指令、場景編輯器、供代理使用的 MCP 工具，以及確定性測試遊戲的重播。'],
			['觸及更多玩家', '無障礙選項、支援複數規則與由右至左文字的在地化，以及依裝置選擇的畫質預設。'],
		],
	},
	examples: { eyebrow: '範例', title: '在瀏覽器裡試玩', text: '為網頁建置的範例遊戲，原始碼就在旁邊。', source: '原始碼' },
	end: { title: '為 NSGo 而建', text: '遊戲層只使用 NSGo 的公開 API。NSGo 是以 Go 為後端的跨平台原生 UI 引擎。', site: 'NSGo 官網' },
};

const ja: Words = {
	headline: ['Go で、', 'ゲームを。'],
	start: 'はじめる',
	play: 'プレイグラウンド',
	ticker: ['Go でゲームを', 'cgo も手書きの C もなし', '固定ステップのループ', 'すべてのファイルはテキスト', '2D、そして 3D', '6 つのターゲット', 'Apache-2.0'],
	features: { eyebrow: 'コア', title: 'フレームを支える 6 つの部品' },
	effects: [
		['ループ', 'ディスプレイのフレームで固定ステップ更新し、フレームごとに補間して一度描画。バックグラウンドでは一時停止します。'],
		['スプライトとタイルマップ', 'TexturePacker と Aseprite のアトラス、フレームアニメーション、Z 順のバッチ、カメラに映る部分だけを描く Tiled マップ。'],
		['物理', '矩形と円の重なり判定、タイルに沿った MoveAndSlide、コヨーテタイムと壁ジャンプを備えたプラットフォーマー用コントローラー。'],
		['サウンド', 'マスターとグループの音量を保存するミキサー、サウンドごとの同時再生数の制限、クロスフェードする音楽。'],
		['入力', 'キー、ゲームパッド、タッチ領域、マウス、ポインターロックを名前付きのアクションと軸として読み取ります。'],
		['シェーダーとライト', 'フレームに重ねる Kage シェーダー、影を落とす 2D ライト、シード付きのパーティクル。'],
	],
	code: { eyebrow: '使い方', title: 'Go で書く 1 フレーム', text: 'ゲームは NSGo のコンポーネントです。ループが固定レートで更新し、Canvas が読むシグナルを設定するので、ビューツリーをコミットせずに再描画できます。' },
	text: {
		eyebrow: 'フォーマット',
		title: '書くファイルはすべてテキスト',
		text: 'シーン、プレハブ、マテリアル、入力バインド、データテーブルは YAML。人も AI も読み、編集し、レビューで差分を見られます。',
		items: [
			['バージョン付きスキーマ', 'どのフォーマットにもバージョンがあり、未知のフィールドは拒否。JSON Schema でエディターや AI ツールが検証できます。'],
			['レビューしやすい', 'キーの順序は固定で、保存のたびに変わる ID もないので、読み込んで保存しても何も変わりません。'],
			['他のツールはそのまま', 'Tiled と LDtk のマップ、Aseprite と TexturePacker のアトラス、glTF を各ツールの出力のまま読み込みます。'],
		],
	},
	packages: {
		eyebrow: 'パッケージ',
		title: '{n} のパッケージ、1 つのモジュール',
		text: 'それぞれが github.com/nsgo-dev/game/packages/game 以下の独立したインポートで、ドキュメントとテストつき。アプリはインポートしたものだけをリンクします。',
		items: [
			['ジャンル', 'プラットフォーマー、RTS の群衆、格闘の技、カードの盤面、リズム譜面、ビジュアルノベル、放置ゲームの巨大な数。'],
			['ゲームシステム', 'インベントリ、アビリティ、クエスト、ドロップテーブル、ステータス、実績、バージョン付きのセーブスロット。'],
			['マルチプレイ', 'WebSocket と UDP の通信、ロールバックネットコード、専用サーバー、アカウント、クラウドセーブ、リーダーボード。'],
			['3D', 'シーンツリー、カメラ、アニメーションブレンド、地形、ボクセルワールド、インスタンシング対応の GPU レンダラー。'],
			['ツール', 'game コマンド、シーンエディター、エージェント向けの MCP ツール、ゲームを決定的にテストするリプレイ。'],
			['より多くのプレイヤーへ', 'アクセシビリティ設定、複数形と右から左の文字に対応したローカライズ、デバイスごとの画質プリセット。'],
		],
	},
	examples: { eyebrow: 'サンプル', title: 'ブラウザで遊ぶ', text: 'Web 向けにビルドしたサンプルゲームを、ソースと並べて。', source: 'ソース' },
	end: { title: 'NSGo のために', text: 'ゲームレイヤーは NSGo の公開 API だけを使います。NSGo は Go をバックエンドにしたクロスプラットフォームのネイティブ UI エンジンです。', site: 'NSGo サイト' },
};

const ko: Words = {
	headline: ['Go로', '게임을.'],
	start: '시작하기',
	play: '플레이그라운드',
	ticker: ['Go로 만드는 게임', 'cgo도, 손으로 쓴 C도 없이', '고정 스텝 루프', '모든 파일은 텍스트', '2D, 그다음 3D', '여섯 가지 타깃', 'Apache-2.0'],
	features: { eyebrow: '코어', title: '모든 프레임의 여섯 부분' },
	effects: [
		['루프', '디스플레이 프레임에 맞춰 고정 스텝으로 업데이트하고 프레임마다 보간해 한 번 그리며, 백그라운드에서는 멈춥니다.'],
		['스프라이트와 타일맵', 'TexturePacker와 Aseprite 아틀라스, 프레임 애니메이션, Z 순서 배치, 카메라에 보이는 곳만 그리는 Tiled 맵.'],
		['물리', '사각형과 원의 겹침 판정, 타일을 따라 미끄러지는 MoveAndSlide, 코요테 타임과 벽 점프를 갖춘 플랫포머 컨트롤러.'],
		['사운드', '마스터와 그룹 볼륨을 저장하는 믹서, 사운드별 동시 재생 제한, 크로스페이드되는 음악.'],
		['입력', '키, 게임패드, 터치 영역, 마우스, 포인터 잠금을 이름 붙은 액션과 축으로 읽습니다.'],
		['셰이더와 조명', '프레임 위에 겹치는 Kage 셰이더, 그림자를 드리우는 2D 조명, 시드 기반 파티클.'],
	],
	code: { eyebrow: '사용법', title: 'Go로 쓰는 한 프레임', text: '게임은 NSGo 컴포넌트입니다. 루프가 고정 속도로 업데이트하고 Canvas가 읽는 시그널을 설정하므로, 뷰 트리를 커밋하지 않고 다시 그립니다.' },
	text: {
		eyebrow: '포맷',
		title: '작성하는 모든 파일은 텍스트',
		text: '씬, 프리팹, 머티리얼, 입력 바인딩, 데이터 테이블은 YAML이라 사람도 AI도 읽고 고치고 리뷰에서 diff를 볼 수 있습니다.',
		items: [
			['버전이 있는 스키마', '모든 포맷에 버전 필드가 있고 알 수 없는 필드는 거부합니다. JSON Schema로 에디터와 AI 도구가 검사할 수 있습니다.'],
			['리뷰하기 쉽게', '키 순서가 고정되고 저장할 때마다 바뀌는 ID가 없어, 불러와 저장해도 아무것도 바뀌지 않습니다.'],
			['다른 도구는 그대로', 'Tiled와 LDtk 맵, Aseprite와 TexturePacker 아틀라스, glTF를 각 도구가 내보낸 그대로 읽습니다.'],
		],
	},
	packages: {
		eyebrow: '패키지',
		title: '{n}개 패키지, 하나의 모듈',
		text: '각각 github.com/nsgo-dev/game/packages/game 아래의 독립된 임포트이며 문서와 테스트가 있습니다. 앱은 임포트한 것만 링크합니다.',
		items: [
			['장르', '플랫포머, RTS 군중, 격투 기술, 카드 보드, 리듬 채보, 비주얼 노벨, 방치형 게임의 큰 수.'],
			['게임 시스템', '인벤토리, 스킬, 퀘스트, 드롭 테이블, 스탯, 업적, 버전이 있는 세이브 슬롯.'],
			['멀티플레이', 'WebSocket과 UDP 전송, 롤백 넷코드, 전용 서버, 계정, 클라우드 세이브, 리더보드.'],
			['3D', '씬 트리, 카메라, 애니메이션 블렌딩, 지형, 복셀 월드, 인스턴싱을 지원하는 GPU 렌더러.'],
			['도구', 'game 명령, 씬 에디터, 에이전트용 MCP 도구, 게임을 결정적으로 테스트하는 리플레이.'],
			['더 많은 플레이어에게', '접근성 옵션, 복수형과 오른쪽에서 왼쪽 텍스트를 지원하는 현지화, 기기별 화질 프리셋.'],
		],
	},
	examples: { eyebrow: '예제', title: '브라우저에서 플레이', text: '웹용으로 빌드한 예제 게임과 그 소스.', source: '소스' },
	end: { title: 'NSGo를 위해', text: '게임 레이어는 NSGo의 공개 API만 사용합니다. NSGo는 Go 백엔드를 가진 크로스 플랫폼 네이티브 UI 엔진입니다.', site: 'NSGo 웹사이트' },
};

const es: Words = {
	headline: ['Juegos', 'en Go.'],
	start: 'Empezar',
	play: 'Playground',
	ticker: ['Juegos en Go', 'Sin cgo ni C escrito a mano', 'Bucle de paso fijo', 'Cada archivo es texto', '2D, luego 3D', 'Seis plataformas', 'Apache-2.0'],
	features: { eyebrow: 'El núcleo', title: 'Seis partes de cada fotograma' },
	effects: [
		['Bucle', 'Actualizaciones de paso fijo en los fotogramas de la pantalla y un dibujo interpolado por fotograma, en pausa en segundo plano.'],
		['Sprites y tilemaps', 'Atlas de TexturePacker y Aseprite, animaciones por fotogramas, lotes ordenados por Z y mapas de Tiled dibujados solo donde ve la cámara.'],
		['Física', 'Solapamientos de cajas y círculos, MoveAndSlide contra los tiles y un controlador de plataformas con coyote time y salto de pared.'],
		['Sonido', 'Un mezclador con volúmenes general y de grupo guardados, límites por sonido y música con fundido cruzado.'],
		['Entrada', 'Acciones y ejes con nombre a partir de teclas, mandos, zonas táctiles, el ratón y el bloqueo del puntero.'],
		['Shaders y luces', 'Pasadas de shaders Kage sobre el fotograma, luces 2D con sombras y partículas con semilla.'],
	],
	code: { eyebrow: 'Uso', title: 'Un fotograma en Go', text: 'Un juego es un componente de NSGo. Su bucle actualiza a un ritmo fijo y fija una señal que lee su Canvas, que se redibuja sin confirmar el árbol de vistas.' },
	text: {
		eyebrow: 'Formatos',
		title: 'Cada archivo que escribes es texto',
		text: 'Escenas, prefabs, materiales, asignaciones de entrada y tablas de datos son YAML que una persona y una IA pueden leer, editar y comparar en una revisión.',
		items: [
			['Esquemas con versión', 'Cada formato tiene un campo de versión y rechaza campos desconocidos; los JSON Schema permiten que editores y herramientas de IA lo comprueben.'],
			['Estables para revisar', 'Orden de claves fijo y sin IDs que cambian al guardar, así que cargar y guardar no cambia nada.'],
			['Otras herramientas tal cual', 'Mapas de Tiled y LDtk, atlas de Aseprite y TexturePacker y glTF se leen como esas herramientas los exportan.'],
		],
	},
	packages: {
		eyebrow: 'Paquetes',
		title: '{n} paquetes, un módulo',
		text: 'Cada uno es su propia importación de github.com/nsgo-dev/game/packages/game, documentada y probada; una app enlaza solo los que importa.',
		items: [
			['Géneros', 'Plataformas, multitudes de RTS, movimientos de lucha, tableros de cartas, partituras de ritmo, novelas visuales y números de juegos idle.'],
			['Sistemas de juego', 'Inventarios, habilidades, misiones, tablas de botín, estadísticas, logros y ranuras de guardado con versión.'],
			['Multijugador', 'Transportes WebSocket y UDP, netcode con rollback, servidores dedicados, cuentas, guardado en la nube y clasificaciones.'],
			['3D', 'Árboles de escena, cámaras, mezcla de animaciones, terreno, mundos de vóxeles y un renderizador GPU con instancias.'],
			['Herramientas', 'El comando game, un editor de escenas, herramientas MCP para agentes y repeticiones que prueban un juego de forma determinista.'],
			['Alcance', 'Opciones de accesibilidad, localización con plurales y texto de derecha a izquierda, y ajustes de calidad por dispositivo.'],
		],
	},
	examples: { eyebrow: 'Ejemplos', title: 'Juégalos en el navegador', text: 'Juegos de ejemplo compilados para la web, con su código al lado.', source: 'Código' },
	end: { title: 'Hecho para NSGo', text: 'La capa de juegos usa solo la API pública de NSGo. NSGo es un motor de interfaz nativa multiplataforma con backend en Go.', site: 'Sitio de NSGo' },
};

const fr: Words = {
	headline: ['Des jeux', 'en Go.'],
	start: 'Commencer',
	play: 'Playground',
	ticker: ['Des jeux en Go', 'Ni cgo ni C écrit à la main', 'Boucle à pas fixe', 'Chaque fichier est du texte', '2D, puis 3D', 'Six cibles', 'Apache-2.0'],
	features: { eyebrow: 'Le cœur', title: 'Six parties de chaque image' },
	effects: [
		['Boucle', 'Des mises à jour à pas fixe sur les images de l’écran et un dessin interpolé par image, en pause en arrière-plan.'],
		['Sprites et tilemaps', 'Atlas de TexturePacker et Aseprite, animations image par image, lots triés en Z et cartes Tiled dessinées seulement là où la caméra voit.'],
		['Physique', 'Chevauchements de boîtes et de cercles, MoveAndSlide contre les tuiles et un contrôleur de plateforme avec coyote time et saut mural.'],
		['Son', 'Un mixeur aux volumes général et de groupe enregistrés, des limites par son et une musique en fondu enchaîné.'],
		['Entrées', 'Des actions et des axes nommés à partir des touches, des manettes, des zones tactiles, de la souris et du verrouillage du pointeur.'],
		['Shaders et lumières', 'Des passes de shaders Kage sur l’image, des lumières 2D avec ombres et des particules à graine.'],
	],
	code: { eyebrow: 'Utilisation', title: 'Une image en Go', text: 'Un jeu est un composant NSGo. Sa boucle met à jour à cadence fixe et règle un signal que lit son Canvas, qui se redessine sans valider l’arbre de vues.' },
	text: {
		eyebrow: 'Formats',
		title: 'Chaque fichier que vous écrivez est du texte',
		text: 'Scènes, prefabs, matériaux, liaisons d’entrées et tables de données sont en YAML, qu’une personne et une IA lisent, modifient et comparent en revue.',
		items: [
			['Schémas versionnés', 'Chaque format a un champ de version et refuse les champs inconnus ; des JSON Schema permettent aux éditeurs et aux outils d’IA de le vérifier.'],
			['Stables en revue', 'Ordre des clés fixe et aucun identifiant qui change à chaque enregistrement : charger puis enregistrer ne change rien.'],
			['Les autres outils tels quels', 'Cartes Tiled et LDtk, atlas Aseprite et TexturePacker et glTF sont lus tels que ces outils les exportent.'],
		],
	},
	packages: {
		eyebrow: 'Paquets',
		title: '{n} paquets, un module',
		text: 'Chacun est son propre import de github.com/nsgo-dev/game/packages/game, documenté et testé ; une app ne lie que ceux qu’elle importe.',
		items: [
			['Genres', 'Plateformes, foules de RTS, coups de jeux de combat, plateaux de cartes, partitions de rythme, romans visuels et grands nombres de jeux idle.'],
			['Systèmes de jeu', 'Inventaires, capacités, quêtes, tables de butin, statistiques, succès et emplacements de sauvegarde versionnés.'],
			['Multijoueur', 'Transports WebSocket et UDP, netcode à rollback, serveurs dédiés, comptes, sauvegardes en ligne et classements.'],
			['3D', 'Arbres de scène, caméras, mélange d’animations, terrain, mondes en voxels et un moteur de rendu GPU avec instanciation.'],
			['Outils', 'La commande game, un éditeur de scènes, des outils MCP pour les agents et des replays qui testent un jeu de façon déterministe.'],
			['Portée', 'Options d’accessibilité, localisation avec pluriels et texte de droite à gauche, et préréglages de qualité par appareil.'],
		],
	},
	examples: { eyebrow: 'Exemples', title: 'Jouez-y dans le navigateur', text: 'Des jeux d’exemple compilés pour le web, avec leur source à côté.', source: 'Source' },
	end: { title: 'Conçu pour NSGo', text: 'La couche de jeu n’utilise que l’API publique de NSGo. NSGo est un moteur d’interface native multiplateforme avec un backend en Go.', site: 'Site de NSGo' },
};

const de: Words = {
	headline: ['Spiele', 'in Go.'],
	start: 'Loslegen',
	play: 'Playground',
	ticker: ['Spiele in Go', 'Kein cgo, kein handgeschriebenes C', 'Schleife mit festem Schritt', 'Jede Datei ist Text', 'Erst 2D, dann 3D', 'Sechs Zielplattformen', 'Apache-2.0'],
	features: { eyebrow: 'Der Kern', title: 'Sechs Teile jedes Frames' },
	effects: [
		['Schleife', 'Updates mit festem Schritt auf den Frames des Displays und ein interpoliertes Zeichnen pro Frame, pausiert im Hintergrund.'],
		['Sprites und Tilemaps', 'Atlanten aus TexturePacker und Aseprite, Frame-Animationen, nach Z sortierte Batches und Tiled-Karten, nur gezeichnet, wo die Kamera hinsieht.'],
		['Physik', 'Überlappungen von Boxen und Kreisen, MoveAndSlide an Tiles und ein Plattformer-Controller mit Coyote Time und Wandsprüngen.'],
		['Sound', 'Ein Mixer mit gespeicherter Gesamt- und Gruppenlautstärke, Limits pro Sound und Musik mit Überblendung.'],
		['Eingabe', 'Benannte Aktionen und Achsen aus Tasten, Gamepads, Touch-Bereichen, der Maus und Pointer Lock.'],
		['Shader und Licht', 'Kage-Shader-Durchgänge über dem Frame, 2D-Lichter mit Schatten und Partikel mit Seed.'],
	],
	code: { eyebrow: 'Verwendung', title: 'Ein Frame in Go', text: 'Ein Spiel ist eine NSGo-Komponente. Seine Schleife aktualisiert mit fester Rate und setzt ein Signal, das sein Canvas liest, sodass es neu zeichnet, ohne den View-Baum zu committen.' },
	text: {
		eyebrow: 'Formate',
		title: 'Jede Datei, die du schreibst, ist Text',
		text: 'Szenen, Prefabs, Materialien, Eingabebelegungen und Datentabellen sind YAML, das Mensch und KI lesen, bearbeiten und im Review vergleichen können.',
		items: [
			['Versionierte Schemas', 'Jedes Format hat ein Versionsfeld und lehnt unbekannte Felder ab; JSON Schemas lassen Editoren und KI-Werkzeuge es prüfen.'],
			['Stabil fürs Review', 'Feste Schlüsselreihenfolge und keine IDs, die sich bei jedem Speichern ändern: Laden und Speichern ändert nichts.'],
			['Andere Werkzeuge, wie sie schreiben', 'Tiled- und LDtk-Karten, Aseprite- und TexturePacker-Atlanten und glTF werden so gelesen, wie diese Werkzeuge sie exportieren.'],
		],
	},
	packages: {
		eyebrow: 'Pakete',
		title: '{n} Pakete, ein Modul',
		text: 'Jedes ist ein eigener Import von github.com/nsgo-dev/game/packages/game, dokumentiert und getestet; eine App linkt nur, was sie importiert.',
		items: [
			['Genres', 'Plattformer, RTS-Massen, Kampfspiel-Moves, Kartenbretter, Rhythmus-Charts, Visual Novels und die großen Zahlen von Idle-Spielen.'],
			['Spielsysteme', 'Inventare, Fähigkeiten, Quests, Beutetabellen, Werte, Erfolge und versionierte Spielstände.'],
			['Mehrspieler', 'WebSocket- und UDP-Transport, Rollback-Netcode, dedizierte Server, Konten, Cloud-Spielstände und Bestenlisten.'],
			['3D', 'Szenenbäume, Kameras, Animationsblending, Terrain, Voxelwelten und ein GPU-Renderer mit Instancing.'],
			['Werkzeuge', 'Der game-Befehl, ein Szeneneditor, MCP-Werkzeuge für Agenten und Replays, die ein Spiel deterministisch testen.'],
			['Reichweite', 'Barrierefreiheit, Lokalisierung mit Pluralformen und Rechts-nach-links-Text sowie Qualitätsstufen je Gerät.'],
		],
	},
	examples: { eyebrow: 'Beispiele', title: 'Im Browser spielen', text: 'Beispielspiele, für das Web gebaut, mit ihrem Quellcode daneben.', source: 'Quellcode' },
	end: { title: 'Gebaut für NSGo', text: 'Die Spieleschicht nutzt nur NSGos öffentliche API. NSGo ist eine plattformübergreifende native UI-Engine mit Go-Backend.', site: 'NSGo-Website' },
};

const pt: Words = {
	headline: ['Jogos', 'em Go.'],
	start: 'Começar',
	play: 'Playground',
	ticker: ['Jogos em Go', 'Sem cgo nem C escrito à mão', 'Loop de passo fixo', 'Todo arquivo é texto', '2D, depois 3D', 'Seis plataformas', 'Apache-2.0'],
	features: { eyebrow: 'O núcleo', title: 'Seis partes de cada quadro' },
	effects: [
		['Loop', 'Atualizações de passo fixo nos quadros da tela e um desenho interpolado por quadro, pausado em segundo plano.'],
		['Sprites e tilemaps', 'Atlas do TexturePacker e do Aseprite, animações por quadro, lotes ordenados por Z e mapas do Tiled desenhados só onde a câmera vê.'],
		['Física', 'Sobreposições de caixas e círculos, MoveAndSlide contra tiles e um controle de plataforma com coyote time e salto na parede.'],
		['Som', 'Um mixer com volumes geral e de grupo salvos, limites por som e música com crossfade.'],
		['Entrada', 'Ações e eixos nomeados a partir de teclas, controles, áreas de toque, o mouse e a captura do ponteiro.'],
		['Shaders e luzes', 'Passes de shaders Kage sobre o quadro, luzes 2D com sombras e partículas com semente.'],
	],
	code: { eyebrow: 'Uso', title: 'Um quadro em Go', text: 'Um jogo é um componente NSGo. Seu loop atualiza a uma taxa fixa e define um sinal que o Canvas lê, então ele redesenha sem confirmar a árvore de views.' },
	text: {
		eyebrow: 'Formatos',
		title: 'Todo arquivo que você escreve é texto',
		text: 'Cenas, prefabs, materiais, mapeamentos de entrada e tabelas de dados são YAML que uma pessoa e uma IA podem ler, editar e comparar na revisão.',
		items: [
			['Esquemas versionados', 'Todo formato tem um campo de versão e recusa campos desconhecidos; JSON Schemas permitem que editores e ferramentas de IA o verifiquem.'],
			['Estáveis para revisão', 'Ordem de chaves fixa e nenhum ID que muda a cada salvamento, então carregar e salvar não muda nada.'],
			['Outras ferramentas como são', 'Mapas do Tiled e do LDtk, atlas do Aseprite e do TexturePacker e glTF são lidos como essas ferramentas os exportam.'],
		],
	},
	packages: {
		eyebrow: 'Pacotes',
		title: '{n} pacotes, um módulo',
		text: 'Cada um é sua própria importação de github.com/nsgo-dev/game/packages/game, documentada e testada; um app vincula só os que importa.',
		items: [
			['Gêneros', 'Plataforma, multidões de RTS, golpes de luta, mesas de cartas, partituras de ritmo, visual novels e números de jogos idle.'],
			['Sistemas de jogo', 'Inventários, habilidades, missões, tabelas de loot, atributos, conquistas e slots de save versionados.'],
			['Multijogador', 'Transportes WebSocket e UDP, netcode com rollback, servidores dedicados, contas, saves na nuvem e placares.'],
			['3D', 'Árvores de cena, câmeras, mistura de animações, terreno, mundos de voxels e um renderizador GPU com instanciação.'],
			['Ferramentas', 'O comando game, um editor de cenas, ferramentas MCP para agentes e replays que testam um jogo de forma determinística.'],
			['Alcance', 'Opções de acessibilidade, localização com plurais e texto da direita para a esquerda, e presets de qualidade por dispositivo.'],
		],
	},
	examples: { eyebrow: 'Exemplos', title: 'Jogue no navegador', text: 'Jogos de exemplo compilados para a web, com o código ao lado.', source: 'Código' },
	end: { title: 'Feito para o NSGo', text: 'A camada de jogos usa só a API pública do NSGo. NSGo é um motor de interface nativa multiplataforma com backend em Go.', site: 'Site do NSGo' },
};

const ru: Words = {
	headline: ['Игры', 'на Go.'],
	start: 'Начать',
	play: 'Песочница',
	ticker: ['Игры на Go', 'Без cgo и рукописного C', 'Цикл с фиксированным шагом', 'Каждый файл — текст', 'Сначала 2D, потом 3D', 'Шесть платформ', 'Apache-2.0'],
	features: { eyebrow: 'Ядро', title: 'Шесть частей каждого кадра' },
	effects: [
		['Цикл', 'Обновления с фиксированным шагом на кадрах дисплея и одна интерполированная отрисовка за кадр; в фоне цикл стоит.'],
		['Спрайты и тайлмапы', 'Атласы из TexturePacker и Aseprite, покадровая анимация, пакеты по Z и карты Tiled, отрисованные только там, куда смотрит камера.'],
		['Физика', 'Пересечения прямоугольников и кругов, MoveAndSlide по тайлам и контроллер платформера с «временем койота» и прыжками от стен.'],
		['Звук', 'Микшер с сохраняемой общей и групповой громкостью, лимитами на звук и музыкой с перекрёстным затуханием.'],
		['Ввод', 'Именованные действия и оси из клавиш, геймпадов, сенсорных зон, мыши и захвата указателя.'],
		['Шейдеры и свет', 'Проходы шейдеров Kage поверх кадра, 2D-свет с тенями и частицы с сидом.'],
	],
	code: { eyebrow: 'Использование', title: 'Кадр на Go', text: 'Игра — это компонент NSGo. Её цикл обновляется с фиксированной частотой и задаёт сигнал, который читает её Canvas, так что он перерисовывается без коммита дерева представлений.' },
	text: {
		eyebrow: 'Форматы',
		title: 'Каждый файл, который вы пишете, — текст',
		text: 'Сцены, префабы, материалы, привязки ввода и таблицы данных — это YAML, который человек и ИИ могут читать, править и сравнивать на ревью.',
		items: [
			['Схемы с версиями', 'У каждого формата есть поле версии, а неизвестные поля отвергаются; JSON Schema позволяют редакторам и ИИ-инструментам его проверять.'],
			['Удобно для ревью', 'Фиксированный порядок ключей и никаких ID, меняющихся при каждом сохранении: загрузка и сохранение ничего не меняют.'],
			['Чужие форматы как есть', 'Карты Tiled и LDtk, атласы Aseprite и TexturePacker и glTF читаются в том виде, в каком их экспортируют эти инструменты.'],
		],
	},
	packages: {
		eyebrow: 'Пакеты',
		title: '{n} пакетов, один модуль',
		text: 'Каждый — отдельный импорт github.com/nsgo-dev/game/packages/game, с документацией и тестами; приложение линкует только то, что импортирует.',
		items: [
			['Жанры', 'Платформеры, толпы RTS, приёмы файтингов, карточные столы, ритм-карты, визуальные новеллы и большие числа idle-игр.'],
			['Игровые системы', 'Инвентари, способности, квесты, таблицы добычи, характеристики, достижения и сохранения с версиями.'],
			['Мультиплеер', 'Транспорты WebSocket и UDP, неткод с откатом, выделенные серверы, аккаунты, облачные сохранения и таблицы рекордов.'],
			['3D', 'Деревья сцен, камеры, смешивание анимаций, ландшафт, воксельные миры и GPU-рендерер с инстансингом.'],
			['Инструменты', 'Команда game, редактор сцен, MCP-инструменты для агентов и реплеи, детерминированно тестирующие игру.'],
			['Доступность', 'Настройки доступности, локализация с формами множественного числа и текстом справа налево, пресеты качества по устройству.'],
		],
	},
	examples: { eyebrow: 'Примеры', title: 'Играйте в браузере', text: 'Примеры игр, собранные для веба, рядом с их исходным кодом.', source: 'Исходный код' },
	end: { title: 'Создано для NSGo', text: 'Игровой слой использует только публичный API NSGo. NSGo — кроссплатформенный нативный UI-движок с бэкендом на Go.', site: 'Сайт NSGo' },
};

export const words: Record<Lang, Words> = { en, zh, 'zh-tw': zhTw, ja, ko, es, fr, de, pt, ru };
