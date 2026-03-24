// Package api는 Valkey Sentinel Manager의 REST API 핸들러를 구현한다.
// 모든 응답은 Response 구조체를 기반으로 한 JSON 형식으로 반환된다.
package api

import (
	"encoding/json"
	"net/http"
)

// Response는 모든 API 응답에 사용되는 표준 JSON 응답 봉투(envelope) 구조체다.
type Response struct {
	Status  string `json:"status"`
	Data    any    `json:"data,omitempty"`
	Message string `json:"message,omitempty"`
}

func writeJSON(w http.ResponseWriter, status int, resp Response) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(resp)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, Response{Status: "error", Message: msg})
}

func readJSON(r *http.Request, dst any) error {
	defer r.Body.Close()
	return json.NewDecoder(r.Body).Decode(dst)
}
