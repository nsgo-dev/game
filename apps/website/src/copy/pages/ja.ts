import type { PagesCopy } from './en.ts'

export const jaPages: PagesCopy = {
  "nav": {
    "tutorial": "チュートリアル",
    "packages": "パッケージ",
    "examples": "サンプル",
    "getStarted": "はじめる",
    "search": "検索"
  },
  "home": {
    "eyebrow": "初期開発段階：API は変更されます",
    "title": [
      "Go でゲームを書く。",
      "ひとつのコードで 6 プラットフォームへ。"
    ],
    "lead": "game は NSGo のゲームレイヤーです。固定ステップのループ、入力アクション、スプライト、タイルマップ、シーン、サウンド。ゲームは 1 つの Canvas に描画され、HUD とメニューはネイティブの NSGo ビューのままです。",
    "start": "はじめる",
    "playground": "Playground で遊ぶ",
    "live": "LIVE · WEB BUILD",
    "readouts": [
      "固定ステップのループ",
      "仮想解像度、自動スケール",
      "スプライト 1000 個の 1 フレーム"
    ],
    "platforms": "ひとつの Go コードから →",
    "consoles": "家庭用ゲーム機（Switch、PlayStation、Xbox）は対象外です。SDK が NDA の下にあり C ツールチェーンを必要としますが、本プロジェクトは Go のみです。",
    "howTitle": "ゲームは描画、UI はネイティブ。",
    "how": [
      {
        "title": "ひとつのビューにゲームを描く",
        "body": "ゲームは 1 つの NSGo ビュー（Canvas、ピクセルバッファ、シェーダー）に描画されます。ループもスプライトバッチもタイルマップも、その周りで動きます。"
      },
      {
        "title": "HUD とメニューはネイティブビュー",
        "body": "スコア、ポーズメニュー、設定画面はゲームの上に重なる普通の NSGo ビュー。ネイティブの文字描画、アクセシビリティ、IME がそのまま使えます。"
      },
      {
        "title": "使う分だけリンク",
        "body": "NSGo の公開 API だけを使うため、インポートしないアプリには一切リンクされません。各パッケージも単独でインポートできます。"
      }
    ],
    "hudNote": "HUD · ネイティブ NSGo ビュー",
    "canvasNote": "ゲーム画面 · core.Canvas",
    "stagesTitle": "2 つのステージ、共通の土台。",
    "stagesLead": "ループ、入力アクション、サウンド、シーン切り替えは両ステージで共通。3D は描画を追加するだけで、それらを置き換えません。",
    "stages": [
      {
        "status": "利用可能",
        "body": "固定ステップのループ、入力アクション、スプライト、タイルマップ、シーン、サウンドを 60 fps またはディスプレイのリフレッシュレートで描画します。"
      },
      {
        "status": "開発中",
        "body": "NSGo が追加する GPU ビュー上で、メッシュ、カメラ、ライト、glTF モデルを深度付きで描画。Blender からのインポートにも対応します。"
      }
    ],
    "codeTitle": "ひとつのコンポーネントが、ひとつのゲーム。",
    "codeLead": "ゲームは NSGo コンポーネントです。アクションを一度バインドし、スコープ上で game.Loop を動かし、固定ステップごとにアクションを進め、Draw で Canvas を再描画します。右は Cave Run の骨格をスプライト 1 つに絞ったものです。",
    "codePoints": [
      "固定ステップで更新、描画フレームは補間",
      "キーボードとゲームパッドを同じアクションに",
      "保存でホットリロード：go tool nsgo dev"
    ],
    "packagesTitle": "{n} のパッケージ、必要な分だけ。",
    "packagesLead": "スプライトから専用サーバーまで。すべてのパッケージにガイドがあり、安定版か実験版かが明示されています。",
    "packagesLink": "すべてのパッケージを見る",
    "examplesTitle": "サンプルゲーム",
    "examplesLead": "サンプルはデスクトップ、モバイル、ブラウザで動き、ソースはリポジトリにあります。",
    "allExamples": "すべてのサンプルを見る",
    "fieldmedic": "独自のシーンエディタとエディタプラグインを同梱した小さなゲーム。",
    "starCatcher": "チュートリアル：空のプロジェクトからリリースまで全 9 章。",
    "startTitle": "3 つのコマンドで、ゲーム作りを始めよう。",
    "steps": [
      "プロジェクトを作成",
      "この環境で実行、保存でホットリロード",
      "別のターゲットへ"
    ],
    "requirements": "Go 1.27+ と NSGo v0.3.0+ が必要です。iOS のビルドには macOS と Xcode が必要です。",
    "migrate": "他のエンジンから移行：",
    "finalTitle": "まだ初期段階。それでも、ゲームを 1 本作りきれます。",
    "finalLead": "まだリリース前です。API 安定性ポリシーで、どのパッケージが安定版で、どれがまだ変わりうるかを確認できます。",
    "finalStart": "はじめかたを読む",
    "finalRoadmap": "ロードマップを見る"
  },
  "categories": [
    "コア",
    "ゲームプレイ",
    "グラフィックスとUI",
    "ツール",
    "オンライン",
    "3D"
  ],
  "genres": {
    "caverun": "横スクロールアクション",
    "cryptquest": "ダンジョン探索",
    "starburst": "アリーナシューター",
    "fountain": "パーティクル負荷テスト",
    "slidegem": "マッチ3パズル",
    "lantern3d": "2D ↔ 3D シーン切り替え"
  },
  "blurbs": {
    "caverun": "横スクロールのプラットフォーマー。探検家が 3 つの洞窟を抜け、宝石を集め、スライムと溶岩を飛び越えて出口のポータルを目指します。ゲームは 1 つの Canvas に描画され、タイトルやポーズ画面はネイティブビューです。",
    "cryptquest": "6 つのターゲットすべてで動く見下ろし型タイルダンジョン。",
    "starburst": "パーティクル、物理、入力をフル活用した見下ろし型ツインスティックシューター。",
    "fountain": "パーティクル負荷テスト：4 つの噴水が 5000 個の粒子を維持。",
    "slidegem": "タッチ優先の 6×6 スライド式マッチ 3。外部アセットファイルはゼロ。",
    "lantern3d": "3D の灯籠の祠と 2D の中庭マップをフェードで行き来します。glTF のジオラマ、PBR ライティング、ネイティブ HUD を備え、両シーンが同じ入力とサウンドを共有します。"
  },
  "packages": {
    "title": "{n} のパッケージ、必要な分だけ。",
    "lead": "スプライトから専用サーバーまで。すべてのパッケージにガイドがあり、安定版か実験版かが明示されています。",
    "search": "パッケージを検索",
    "all": "すべて",
    "guide": "ガイド",
    "noMatch": "一致するパッケージはありません"
  },
  "examples": {
    "title": "サンプルゲーム",
    "lead": "サンプルはデスクトップ、モバイル、ブラウザで動き、ソースはリポジトリにあります。",
    "used": "使用パッケージ",
    "run": "実行",
    "play": "ブラウザで遊ぶ",
    "source": "ソースを見る",
    "lanternCaption": "画像は 2D の中庭マップです。3D の祠はプレイして確認してください。"
  },
  "tutorial": {
    "eyebrow": "全9章",
    "start": "第1章から始める",
    "source": "ソースを見る",
    "run": "このリポジトリで各章を実行"
  },
  "footer": {
    "tagline": "Go で書かれた NSGo のゲームレイヤー。2D は今すぐ、3D はまもなく。",
    "columns": [
      "DOCS",
      "PROJECT",
      "COMMUNITY"
    ],
    "gettingStarted": "はじめかた",
    "tutorial": "チュートリアル：Star Catcher",
    "packages": "パッケージ",
    "stability": "API 安定性",
    "playground": "Playground",
    "examples": "サンプル",
    "roadmap": "ロードマップ",
    "changelog": "変更履歴",
    "github": "GitHub",
    "nsgo": "NSGo",
    "contributing": "コントリビュート",
    "feedback": "フィードバック",
    "license": "Apache-2.0 ライセンス · © 2026 NSGo",
    "llms": "LLM 向けドキュメント（llms.txt）",
    "llmsFull": "LLM 向け全ドキュメント（llms-full.txt）",
    "showcase": "ショーケース"
  },
  "docsNotice": "このページは英語版を表示しています。"
}
