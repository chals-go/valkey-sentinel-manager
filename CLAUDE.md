# CLAUDE.md

## 코딩 스타일

- Go 코드 작성 시 Google Go Style Guide를 따른다: https://google.github.io/styleguide/go/

## 언어

- 모든 대답은 한국어로 한다.

## 개발 프로세스

- 기능 추가 개발 시 Golang 전문가, 웹디자이너, 웹개발자, 기획자 협의 후 진행할 것
- 모든 기능 추가 후 코드 리뷰 진행
- 코드 리뷰 완료 후 테스트 코드 작성
- 테스트 완료 후 git commit & push

## 용어 규칙

- Replication Group: Valkey primary + replica 노드 그룹 (UI에서 "Cluster"로 축약하지 않음)
- Sentinel Cluster: 같은 그룹에 속한 Sentinel 노드 묶음
- Monitoring Name: Sentinel에서 마스터를 식별하는 이름 (= MasterName)
- DNS Provider: Route53, Azure DNS, BIND 등 DNS 관리 프로바이더
- Webhook Endpoint: 알림을 전송할 대상 (Slack, Discord, Teams, 카카오워크, Custom)

## Go 코딩 규칙

### Store 인터페이스 메서드 네이밍
- 생성/저장: `Save{Resource}` (SaveWebhook, SaveCluster 등)
- 조회: `Get{Resource}`, `List{Resources}`
- 삭제: `Delete{Resource}`
- 특수 단건 값: `Get{Key}`, `Set{Key}` (GetAPIToken, SetAPIToken)

### 웹 핸들러 네이밍
- 페이지 렌더링: `{Resource}Page` (ClusterFormPage, ClusterEditPage)
- 목록 페이지: `{Resources}` (Clusters, Sentinels, Events)
- POST 처리: `{Resource}{Action}` (ClusterCreate, ClusterDelete, WebhookToggle)
- 설정 저장: `Settings{Section}Save` (SettingsServerSave, SettingsAccountSave)

### 에러 처리 패턴
- 웹 핸들러 (사용자 입력 오류): flash 메시지 + render (같은 페이지에 에러 표시)
- 웹 핸들러 (시스템 오류): slog.Error + redirect (500 방지)
- API 핸들러: writeError(w, status, message)
- ParseForm 실패: http.Error(w, "invalid form data", 400) + return

## 프론트엔드 규칙

### i18n
- 모든 UI 텍스트는 i18n 키를 통해 표시 (하드코딩 금지)
- 영어(en)와 한국어(ko) 번역 모두 필수
- 키 이름: snake_case (예: register_replication_group)

### Tailwind CSS 클래스 패턴
- Primary 버튼: `px-4 py-2 bg-indigo-600 text-white text-sm font-medium rounded-lg hover:bg-indigo-700`
- Secondary 버튼: `px-4 py-2 bg-white text-slate-600 text-sm font-medium rounded-lg border border-gray-300 hover:bg-gray-50`
- Danger 버튼: `px-4 py-2 bg-red-600 text-white text-sm font-medium rounded-lg hover:bg-red-700`
- 입력 필드: `w-full px-3 py-2 border border-gray-300 rounded-lg text-sm focus:ring-2 focus:ring-indigo-500/20 focus:border-indigo-500 outline-none`
- 배지 (성공): `bg-emerald-50 text-emerald-700 border-emerald-200`
- 배지 (에러): `bg-red-50 text-red-700 border-red-200`
- 배지 (경고): `bg-amber-50 text-amber-700 border-amber-200`
- 라벨: `block text-sm font-medium text-slate-700 mb-1`

### 모달 구조
- 오버레이: `class="modal-overlay" role="dialog" aria-modal="true"`
- 컨테이너: `bg-white rounded-2xl p-8 w-full max-w-{size} max-h-[90vh] overflow-y-auto shadow-2xl`
- 헤더: 제목(h2) + 닫기(X) 버튼
- 푸터: Cancel(secondary) + 확인(primary/danger)

### JavaScript
- 인라인 이벤트 핸들러 금지 → data 속성 + app.js 이벤트 위임 사용
- innerHTML에 동적 데이터 삽입 시 escapeHtml() 필수
