import type { PagesCopy } from './en.ts'

export const koPages: PagesCopy = {
  "nav": {
    "tutorial": "튜토리얼",
    "packages": "패키지",
    "examples": "예제",
    "getStarted": "시작하기",
    "search": "검색"
  },
  "home": {
    "eyebrow": "초기 단계: API는 변경될 수 있습니다",
    "title": [
      "Go로 게임을 만드세요.",
      "코드 하나로 여섯 플랫폼에."
    ],
    "lead": "game은 NSGo의 게임 레이어입니다. 고정 스텝 루프, 입력 액션, 스프라이트, 타일맵, 씬, 사운드. 게임은 하나의 Canvas에 그려지고, HUD와 메뉴는 네이티브 NSGo 뷰 그대로입니다.",
    "start": "시작하기",
    "playground": "Playground에서 플레이",
    "live": "LIVE · WEB BUILD",
    "readouts": [
      "고정 스텝 루프",
      "가상 해상도, 자동 스케일",
      "스프라이트 1000개의 한 프레임"
    ],
    "platforms": "Go 코드 하나로 빌드 →",
    "consoles": "콘솔(Switch, PlayStation, Xbox)은 범위에 포함되지 않습니다. SDK가 NDA로 보호되고 C 툴체인이 필요하지만, 이 프로젝트는 Go만 사용합니다.",
    "howTitle": "게임은 그리고, UI는 네이티브로.",
    "how": [
      {
        "title": "하나의 뷰에 게임 그리기",
        "body": "게임은 하나의 NSGo 뷰(Canvas, 픽셀 버퍼 또는 셰이더)에 그려집니다. 루프, 스프라이트 배치, 타일맵 모두 이를 중심으로 동작합니다."
      },
      {
        "title": "HUD와 메뉴는 네이티브 뷰",
        "body": "점수, 일시정지 메뉴, 설정 화면은 게임 위에 겹쳐진 평범한 NSGo 뷰로, 네이티브 텍스트 렌더링과 접근성, 입력기를 그대로 씁니다."
      },
      {
        "title": "쓰는 만큼만 링크",
        "body": "NSGo의 공개 API만 사용하므로, 가져오지 않은 앱에는 전혀 링크되지 않습니다. 각 패키지도 따로 가져올 수 있습니다."
      }
    ],
    "hudNote": "HUD · 네이티브 NSGo 뷰",
    "canvasNote": "게임 화면 · core.Canvas",
    "stagesTitle": "두 단계, 하나의 공통 기반.",
    "stagesLead": "루프, 입력 액션, 사운드, 씬 전환은 두 단계가 공유합니다. 3D는 그리기를 더할 뿐, 이를 대체하지 않습니다.",
    "stages": [
      {
        "status": "사용 가능",
        "body": "고정 스텝 루프, 입력 액션, 스프라이트, 타일맵, 씬, 사운드를 60 fps 또는 디스플레이 주사율로 그립니다."
      },
      {
        "status": "진행 중",
        "body": "NSGo가 새로 추가하는 GPU 뷰에서 메시, 카메라, 조명, glTF 모델을 깊이와 함께 그리며, Blender에서 가져올 수 있습니다."
      }
    ],
    "codeTitle": "컴포넌트 하나가 게임 하나.",
    "codeLead": "게임은 NSGo 컴포넌트입니다. 액션을 한 번 바인딩하고, 스코프에서 game.Loop를 실행하고, 고정 스텝마다 액션을 진행하며, Draw에서 Canvas를 다시 그립니다. 오른쪽은 Cave Run의 뼈대를 스프라이트 하나로 줄인 것입니다.",
    "codePoints": [
      "고정 스텝 업데이트, 그리기 프레임 보간",
      "키보드와 게임패드를 같은 액션에",
      "저장하면 핫 리로드: go tool nsgo dev"
    ],
    "packagesTitle": "{n}개 패키지, 필요한 것만.",
    "packagesLead": "스프라이트부터 전용 서버까지, 모든 패키지에 가이드가 있고 안정 또는 실험 상태가 표시됩니다.",
    "packagesLink": "모든 패키지 보기",
    "examplesTitle": "예제 게임",
    "examplesLead": "예제는 데스크톱, 모바일, 웹에서 실행되며 소스는 저장소에 있습니다.",
    "allExamples": "모든 예제 보기",
    "fieldmedic": "자체 씬 에디터와 에디터 플러그인을 내장한 작은 게임.",
    "starCatcher": "튜토리얼: 빈 프로젝트에서 출시까지 9개 챕터.",
    "startTitle": "명령어 세 줄로 첫 게임을.",
    "steps": [
      "프로젝트 만들기",
      "이 컴퓨터에서 실행, 저장하면 핫 리로드",
      "다른 타깃으로 전환"
    ],
    "requirements": "Go 1.27+와 NSGo v0.3.0+가 필요합니다. iOS 빌드에는 macOS와 Xcode가 필요합니다.",
    "migrate": "다른 엔진에서 옮겨오기:",
    "finalTitle": "아직 초기지만, 게임 한 편을 끝까지 만들 수 있습니다.",
    "finalLead": "아직 출시 전입니다. API 안정성 정책에서 어떤 패키지가 안정적이고 어떤 패키지가 바뀔 수 있는지 확인하세요.",
    "finalStart": "시작 가이드 읽기",
    "finalRoadmap": "로드맵 보기"
  },
  "categories": [
    "코어",
    "게임플레이",
    "그래픽 & UI",
    "도구",
    "온라인",
    "3D"
  ],
  "genres": {
    "caverun": "플랫포머",
    "cryptquest": "던전 어드벤처",
    "starburst": "아레나 슈터",
    "fountain": "파티클 스트레스 테스트",
    "slidegem": "매치 3 퍼즐",
    "lantern3d": "2D ↔ 3D 씬 전환"
  },
  "blurbs": {
    "caverun": "횡스크롤 플랫포머: 탐험가가 세 개의 동굴을 지나 보석을 모으고, 슬라임과 용암을 뛰어넘어 출구 포털에 도달합니다. 하나의 Canvas에 그려지며, 타이틀·일시정지·종료 화면은 네이티브 뷰입니다.",
    "cryptquest": "여섯 개 타깃 모두에서 실행되는 탑다운 타일 던전 어드벤처.",
    "starburst": "파티클, 물리, 입력을 총동원한 탑다운 트윈스틱 아레나 슈터.",
    "fountain": "파티클 스트레스 테스트: 분수 네 개가 파티클 5000개를 유지.",
    "slidegem": "외부 에셋 파일 없이 만든 터치 우선 6×6 슬라이드 매치 3.",
    "lantern3d": "3D 등불 사당과 2D 안뜰 지도 사이를 페이드로 오갑니다. glTF 디오라마, PBR 조명, 네이티브 HUD를 갖추고 두 씬이 같은 입력과 사운드를 공유합니다."
  },
  "packages": {
    "title": "{n}개 패키지, 필요한 것만.",
    "lead": "스프라이트부터 전용 서버까지, 모든 패키지에 가이드가 있고 안정 또는 실험 상태가 표시됩니다.",
    "search": "패키지 검색",
    "all": "전체",
    "guide": "가이드",
    "noMatch": "일치하는 패키지가 없습니다"
  },
  "examples": {
    "title": "예제 게임",
    "lead": "예제는 데스크톱, 모바일, 웹에서 실행되며 소스는 저장소에 있습니다.",
    "used": "사용한 패키지",
    "run": "실행",
    "play": "브라우저에서 플레이",
    "source": "소스 보기",
    "lanternCaption": "이미지는 2D 안뜰 지도입니다. 3D 사당은 직접 플레이해 확인하세요."
  },
  "tutorial": {
    "eyebrow": "9개 챕터",
    "start": "1장부터 시작하기",
    "source": "소스 보기",
    "run": "저장소에서 각 챕터 실행"
  },
  "footer": {
    "tagline": "Go로 작성된 NSGo 게임 레이어. 2D는 지금, 3D는 곧.",
    "columns": [
      "DOCS",
      "PROJECT",
      "COMMUNITY"
    ],
    "gettingStarted": "시작하기",
    "tutorial": "튜토리얼: Star Catcher",
    "packages": "패키지",
    "stability": "API 안정성",
    "playground": "Playground",
    "examples": "예제",
    "roadmap": "로드맵",
    "changelog": "변경 로그",
    "github": "GitHub",
    "nsgo": "NSGo",
    "contributing": "기여 가이드",
    "feedback": "피드백",
    "license": "Apache-2.0 라이선스 · © 2026 NSGo",
    "llms": "LLM용 문서 (llms.txt)",
    "llmsFull": "LLM용 전체 문서 (llms-full.txt)",
    "showcase": "쇼케이스"
  },
  "docsNotice": "이 페이지는 영어로 제공됩니다."
}
