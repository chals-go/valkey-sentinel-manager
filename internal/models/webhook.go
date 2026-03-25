// Package models는 클러스터, 이벤트, 센티널 등 도메인 모델을 정의한다.
package models

// WebhookEndpoint는 알림을 전송할 웹훅 엔드포인트 설정을 담는 구조체이다.
type WebhookEndpoint struct {
	// ID는 웹훅의 고유 식별자이다 (예: "wh_a1b2c3d4").
	ID string `json:"id"`
	// Name은 사용자가 식별하기 위한 이름이다 (예: "운영팀 Slack").
	Name string `json:"name"`
	// Type은 웹훅 유형이다: slack, discord, teams, kakaowork, custom.
	Type string `json:"type"`
	// URL은 웹훅 전송 대상 URL이다.
	URL string `json:"url"`
	// Enabled가 true이면 이벤트 발생 시 이 웹훅으로 알림을 전송한다.
	Enabled bool `json:"enabled"`
	// Channel은 Slack 전용 채널 이름이다 (optional).
	Channel string `json:"channel,omitempty"`
	// AppKey는 카카오워크 Bot App Key이다.
	AppKey string `json:"app_key,omitempty"`
	// ConversationID는 카카오워크 메시지 전송 대상 채팅방 ID이다.
	ConversationID string `json:"conversation_id,omitempty"`
	// PayloadMode는 custom 타입 전용: "text" 또는 "json".
	PayloadMode string `json:"payload_mode,omitempty"`
	// BodyKey는 custom text 모드에서 메시지를 담을 JSON 키 이름이다 (기본값: "text").
	BodyKey string `json:"body_key,omitempty"`
	// CustomHeaders는 custom 타입에서 추가할 HTTP 헤더이다.
	CustomHeaders map[string]string `json:"custom_headers,omitempty"`
}

const (
	WebhookTypeSlack     = "slack"
	WebhookTypeDiscord   = "discord"
	WebhookTypeTeams     = "teams"
	WebhookTypeKakaoWork = "kakaowork"
	WebhookTypeCustom    = "custom"
)
