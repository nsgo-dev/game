import type { PagesCopy } from './en.ts'

export const zhPages: PagesCopy = {
  "nav": {
    "tutorial": "教程",
    "packages": "包",
    "examples": "示例",
    "getStarted": "快速开始",
    "search": "搜索"
  },
  "home": {
    "eyebrow": "早期阶段：API 还会变化",
    "title": [
      "用 Go 写游戏，",
      "一份代码跑遍六个平台"
    ],
    "lead": "game 是 NSGo 的游戏层：固定步长循环、输入动作、精灵、瓦片地图、场景与声音。游戏画在一个 Canvas 里，HUD 和菜单仍是原生 NSGo 视图。",
    "start": "快速开始",
    "playground": "在 Playground 试玩",
    "live": "LIVE · WEB BUILD",
    "readouts": [
      "固定步长循环",
      "虚拟分辨率，自动缩放",
      "1000 个精灵的一帧"
    ],
    "platforms": "一份 Go 代码，构建到 →",
    "consoles": "主机平台（Switch、PlayStation、Xbox）不在范围内：它们的 SDK 受保密协议约束且需要 C 工具链，而本项目只用 Go。",
    "howTitle": "游戏是画出来的，界面是原生的",
    "how": [
      {
        "title": "在一个视图里画游戏",
        "body": "游戏画进一个 NSGo 视图：Canvas、像素缓冲区或着色器。循环、精灵批次和瓦片地图都围绕它工作。"
      },
      {
        "title": "HUD 和菜单用原生视图",
        "body": "分数、暂停菜单、设置页都是普通的 NSGo 视图，叠在游戏上方，自带原生文字渲染、无障碍与输入法。"
      },
      {
        "title": "只链接你用到的部分",
        "body": "只使用 NSGo 的公开 API。没有导入它的应用不会链接其中任何代码，每个包也都可以单独导入。"
      }
    ],
    "hudNote": "HUD · 原生 NSGo 视图",
    "canvasNote": "游戏画面 · core.Canvas",
    "stagesTitle": "两个阶段，共用一套底座",
    "stagesLead": "循环、输入动作、声音和场景切换由两个阶段共用，所以 3D 只是增加绘制，不会替换它们。",
    "stages": [
      {
        "status": "可用",
        "body": "固定步长循环、输入动作、精灵、瓦片地图、场景和声音，以 60 fps 或显示器的刷新率绘制。"
      },
      {
        "status": "进行中",
        "body": "在 NSGo 为此新增的 GPU 视图上，带深度地绘制网格、相机、光照和 glTF 模型，可从 Blender 导入。"
      }
    ],
    "codeTitle": "一个组件，就是一个游戏",
    "codeLead": "游戏是一个 NSGo 组件：绑定一次输入动作，在作用域上运行 game.Loop，每个固定步推进动作，并在 Draw 中重绘 Canvas。右边是 Cave Run 的骨架，精简到一个精灵。",
    "codePoints": [
      "固定步长更新，绘制帧之间插值",
      "键盘与手柄绑定到同一个动作",
      "保存即热重载：go tool nsgo dev"
    ],
    "packagesTitle": "{n} 个包，按需导入",
    "packagesLead": "从精灵到专用服务器，每个包都有指南，并标明稳定或实验状态。",
    "packagesLink": "浏览全部包",
    "examplesTitle": "示例游戏",
    "examplesLead": "示例都能在桌面、移动端和浏览器中运行，源码就在仓库里。",
    "allExamples": "查看全部示例",
    "fieldmedic": "自带场景编辑器与编辑器插件的小游戏。",
    "starCatcher": "教程：九章，从空项目到发布一款完整游戏。",
    "startTitle": "三条命令，开始做游戏",
    "steps": [
      "创建项目",
      "在本机运行，保存即热重载",
      "换一个目标平台"
    ],
    "requirements": "需要 Go 1.27+ 与 NSGo v0.3.4+；构建 iOS 需要 macOS 与 Xcode。",
    "migrate": "从其他引擎迁移：",
    "finalTitle": "还在早期，但已经能做完一整款游戏",
    "finalLead": "项目尚未发布。API 稳定性策略说明了哪些包已经稳定、哪些仍会变化。",
    "finalStart": "阅读快速开始",
    "finalRoadmap": "查看路线图"
  },
  "categories": [
    "核心",
    "玩法",
    "画面与界面",
    "工具链",
    "联机与服务",
    "3D"
  ],
  "genres": {
    "caverun": "横版平台跳跃",
    "cryptquest": "地牢冒险",
    "starburst": "竞技场射击",
    "fountain": "粒子压力测试",
    "slidegem": "滑动三消",
    "lantern3d": "2D ↔ 3D 场景切换"
  },
  "blurbs": {
    "caverun": "横版平台跳跃：探险者穿越三个洞穴，收集宝石、跳过史莱姆与岩浆，抵达出口传送门。游戏画在一个 Canvas 里，标题、暂停与结算界面都是原生视图。",
    "cryptquest": "俯视角瓦片地牢冒险，覆盖全部六个目标平台。",
    "starburst": "俯视角双摇杆竞技场射击，粒子、物理与输入齐上阵。",
    "fountain": "粒子压力测试：四座喷泉维持 5000 个存活粒子。",
    "slidegem": "触屏优先的 6×6 滑动消除，没有一个外部素材文件。",
    "lantern3d": "在 3D 灯笼神殿与 2D 庭院地图之间淡入淡出切换：glTF 场景、PBR 光照和原生 HUD，两个场景共用同一套输入与声音。"
  },
  "packages": {
    "title": "{n} 个包，按需导入",
    "lead": "从精灵到专用服务器，每个包都有指南，并标明稳定或实验状态。",
    "search": "搜索包名或功能",
    "all": "全部",
    "guide": "指南",
    "noMatch": "没有匹配的包"
  },
  "examples": {
    "title": "示例游戏",
    "lead": "示例都能在桌面、移动端和浏览器中运行，源码就在仓库里。",
    "used": "用到的包",
    "run": "运行",
    "play": "在浏览器试玩",
    "source": "查看源码",
    "lanternCaption": "图中是 2D 庭院地图，3D 神殿请点“试玩”查看"
  },
  "tutorial": {
    "eyebrow": "九章内容",
    "start": "从第 1 章开始",
    "source": "查看源码",
    "run": "在本仓库运行各章"
  },
  "footer": {
    "tagline": "用 Go 编写的 NSGo 游戏层。2D 现已可用，3D 正在路上。",
    "columns": [
      "DOCS",
      "PROJECT",
      "COMMUNITY"
    ],
    "gettingStarted": "快速开始",
    "tutorial": "教程：Star Catcher",
    "packages": "包",
    "stability": "API 稳定性",
    "playground": "Playground",
    "examples": "示例",
    "roadmap": "路线图",
    "changelog": "更新日志",
    "github": "GitHub",
    "nsgo": "NSGo",
    "contributing": "贡献指南",
    "feedback": "反馈",
    "license": "Apache-2.0 许可 · © 2026 NSGo",
    "llms": "面向 LLM 的文档（llms.txt）",
    "llmsFull": "面向 LLM 的完整文档（llms-full.txt）",
    "showcase": "作品展示"
  },
  "docsNotice": ""
}
