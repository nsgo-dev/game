import type { PagesCopy } from './en.ts'

export const zhTwPages: PagesCopy = {
  "nav": {
    "tutorial": "教學",
    "packages": "套件",
    "examples": "範例",
    "getStarted": "快速開始",
    "search": "搜尋"
  },
  "home": {
    "eyebrow": "v0.1.0 · 純 Go",
    "title": [
      "用 Go 寫遊戲，",
      "一份程式碼跑遍六個平台"
    ],
    "lead": "game 是 NSGo 的 2D 與 3D 遊戲層：固定步長迴圈、輸入動作、精靈圖、glTF 繪製、視覺化場景編輯器與純文字 YAML 格式。遊戲畫在一個 NSGo 視圖裡，HUD 和選單仍是原生視圖。",
    "start": "快速開始",
    "playground": "在 Playground 試玩",
    "live": "LIVE · WEB BUILD",
    "readouts": [
      "固定步長迴圈",
      "虛擬解析度，自動縮放",
      "1000 個精靈圖的一個影格"
    ],
    "platforms": "一份 Go 程式碼，建置到 →",
    "consoles": "遊戲主機（Switch、PlayStation、Xbox）不在範圍內：它們的 SDK 受保密協議約束且需要 C 工具鏈，而本專案只用 Go。",
    "howTitle": "遊戲是畫出來的，介面是原生的",
    "how": [
      {
        "title": "在一個視圖裡畫遊戲",
        "body": "遊戲畫進一個 NSGo 視圖：Canvas、像素緩衝區或著色器。迴圈、精靈圖批次和圖塊地圖都圍繞它運作。"
      },
      {
        "title": "HUD 和選單用原生視圖",
        "body": "分數、暫停選單、設定頁都是普通的 NSGo 視圖，疊在遊戲上方，具備原生文字繪製、無障礙與輸入法。"
      },
      {
        "title": "只連結你用到的部分",
        "body": "只使用 NSGo 的公開 API。沒有匯入它的應用程式不會連結其中任何程式碼，每個套件也都能單獨匯入。"
      }
    ],
    "hudNote": "HUD · 原生 NSGo 視圖",
    "canvasNote": "遊戲畫面 · core.Canvas",
    "stagesTitle": "2D 與 3D，共用一套基礎",
    "stagesLead": "迴圈、輸入動作、音效和場景切換由 2D 與 3D 共用，可自由混用與切換場景，無須重寫遊戲架構。",
    "stages": [
      {
        "status": "可用",
        "body": "固定步長迴圈、輸入動作、精靈圖、圖塊地圖、場景、物理、光照與音效，以 60 fps 或螢幕的更新率繪製。"
      },
      {
        "status": "可用",
        "body": "在 NSGo GPU 視圖上繪製網格、攝影機、PBR 光照、骨骼動畫、3D 物理與 glTF 模型，支援從 Blender 直接匯入。"
      }
    ],
    "codeTitle": "一個元件，就是一款遊戲",
    "codeLead": "遊戲是一個 NSGo 元件：綁定一次輸入動作，在作用域上執行 game.Loop，每個固定步推進動作，並在 Draw 中重繪 Canvas。右邊是 Cave Run 的骨架，精簡到一個精靈圖。",
    "codePoints": [
      "固定步長更新，繪製影格之間插值",
      "鍵盤與手把綁定到同一個動作",
      "存檔即熱重載：go tool nsgo dev"
    ],
    "workflowTitle": "純文字專案，視覺化編輯器，專為 AI 代理打造",
    "workflowLead": "場景、預製體、材質、動畫與資料表皆為帶版本的 YAML，人與程式開發代理都能直接閱讀、比對與編輯。",
    "workflow": [
      {
        "tag": "EDITOR",
        "title": "視覺化 2D 與 3D 場景編輯器",
        "body": "執行 game editor 開啟即時視口、層級樹、屬性檢查器、變換控制柄與資源瀏覽器，並可使用 Go 外掛擴充。",
        "link": "探索場景編輯器"
      },
      {
        "tag": "YAML",
        "title": "純文字、附 Schema 驗證的格式",
        "body": "沒有不透明的二進位場景檔。每種格式皆含版本號、拒絕未知欄位、提供 JSON Schema，在 Git 中能乾淨審閱與往返儲存。",
        "link": "瀏覽檔案格式"
      },
      {
        "tag": "TEMPLATES & AI",
        "title": "16 套入門範本與 MCP 工具",
        "body": "使用 game new -template 一鍵建立平台跳躍、RPG、Roguelike、3D 動作、FPS 或賽車專案，並透過 MCP 與 Agent Skills 檢查執行中的場景。",
        "link": "查看全部 16 套範本"
      }
    ],
    "packagesTitle": "{n} 個套件，按需匯入",
    "packagesLead": "從精靈圖到專用伺服器，每個套件都有指南，並標明穩定或實驗狀態。",
    "packagesLink": "瀏覽全部套件",
    "examplesTitle": "範例遊戲",
    "examplesLead": "範例都能在桌面、行動裝置和瀏覽器中執行，原始碼就在儲存庫裡。",
    "allExamples": "查看全部範例",
    "fieldmedic": "自帶場景編輯器與編輯器外掛的小遊戲。",
    "starCatcher": "教學：九章，從空專案到發布一款完整遊戲。",
    "startTitle": "三行指令，開始做遊戲",
    "steps": [
      "建立專案",
      "在本機執行，存檔即熱重載",
      "換一個目標平台"
    ],
    "requirements": "需要 Go 1.27+ 與 NSGo v0.3.4+；建置 iOS 需要 macOS 與 Xcode。",
    "migrate": "從其他引擎遷移：",
    "finalTitle": "v0.1.0 現已發布，用純 Go 打造並發布你的遊戲",
    "finalLead": "每個套件都有完整指南，並在 API 穩定性政策中標明穩定或實驗狀態。從範本開始，或瀏覽以 NSGo Game 打造的作品展示。",
    "finalStart": "閱讀快速開始",
    "finalShowcase": "瀏覽作品展示"
  },
  "categories": [
    "核心",
    "玩法",
    "畫面與介面",
    "工具鏈",
    "連線與服務",
    "3D"
  ],
  "genres": {
    "caverun": "橫向平台跳躍",
    "cryptquest": "地城冒險",
    "starburst": "競技場射擊",
    "fountain": "粒子壓力測試",
    "slidegem": "滑動三消",
    "lantern3d": "2D ↔ 3D 場景切換"
  },
  "blurbs": {
    "caverun": "橫向捲軸平台跳躍：探險者穿越三個洞穴，收集寶石、跳過史萊姆與岩漿，抵達出口傳送門。遊戲畫在一個 Canvas 裡，標題、暫停與結算畫面都是原生視圖。",
    "cryptquest": "俯視角圖塊地城冒險，涵蓋全部六個目標平台。",
    "starburst": "俯視角雙搖桿競技場射擊，粒子、物理與輸入全員到齊。",
    "fountain": "粒子壓力測試：四座噴泉維持 5000 個存活粒子。",
    "slidegem": "觸控優先的 6×6 滑動消除，沒有任何外部素材檔。",
    "lantern3d": "在 3D 燈籠神殿與 2D 庭院地圖之間淡入淡出切換：glTF 場景、PBR 光照和原生 HUD，兩個場景共用同一套輸入與聲音。"
  },
  "packages": {
    "title": "{n} 個套件，按需匯入",
    "lead": "從精靈圖到專用伺服器，每個套件都有指南，並標明穩定或實驗狀態。",
    "search": "搜尋套件名稱或功能",
    "all": "全部",
    "guide": "指南",
    "noMatch": "沒有符合的套件"
  },
  "examples": {
    "title": "範例遊戲",
    "lead": "範例都能在桌面、行動裝置和瀏覽器中執行，原始碼就在儲存庫裡。",
    "used": "用到的套件",
    "run": "執行",
    "play": "在瀏覽器試玩",
    "source": "查看原始碼",
    "lanternCaption": "圖中是 2D 庭院地圖，3D 神殿請點「試玩」查看"
  },
  "tutorial": {
    "eyebrow": "九章內容",
    "start": "從第 1 章開始",
    "source": "查看原始碼",
    "run": "在本儲存庫執行各章"
  },
  "footer": {
    "tagline": "以純 Go 撰寫的 NSGo 遊戲層：2D、3D、視覺化場景編輯器與純文字 YAML，涵蓋六個平台。",
    "columns": [
      "DOCS",
      "PROJECT",
      "COMMUNITY"
    ],
    "gettingStarted": "快速開始",
    "tutorial": "教學：Star Catcher",
    "packages": "套件",
    "stability": "API 穩定性",
    "playground": "Playground",
    "examples": "範例",
    "editor": "場景編輯器",
    "templates": "入門範本",
    "changelog": "更新日誌",
    "github": "GitHub",
    "nsgo": "NSGo",
    "contributing": "貢獻指南",
    "feedback": "意見回饋",
    "license": "Apache-2.0 授權 · © 2026 NSGo",
    "llms": "面向 LLM 的文件（llms.txt）",
    "llmsFull": "面向 LLM 的完整文件（llms-full.txt）",
    "showcase": "作品展示"
  },
  "docsNotice": ""
}
