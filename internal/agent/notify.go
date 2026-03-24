package agent

import (
	"log"
	"strconv"
	"strings"
)

// parseSdownDescription parses "+sdown/-sdown" event descriptions.
// Format: "slave <ip>:<port> <ip> <port> @ <master-name> <master-ip> <master-port>"
// Returns nil if not a slave event.
func parseSdownDescription(description string) map[string]any {
	parts := strings.Fields(description)
	if len(parts) < 5 || parts[0] != "slave" {
		return nil
	}

	atIdx := -1
	for i, p := range parts {
		if p == "@" {
			atIdx = i
			break
		}
	}
	if atIdx < 0 || atIdx+1 >= len(parts) {
		return nil
	}

	masterName := parts[atIdx+1]
	addr := parts[1]
	lastColon := strings.LastIndex(addr, ":")
	if lastColon < 0 {
		return nil
	}
	ip := addr[:lastColon]
	port, err := strconv.Atoi(addr[lastColon+1:])
	if err != nil {
		return nil
	}

	return map[string]any{
		"ip":          ip,
		"port":        port,
		"master_name": masterName,
	}
}

// CmdNotify handles the notification-script subcommand.
// Args: <event-type> <event-description...>
// Only +sdown/-sdown slave events are forwarded. Others are logged and ignored.
// Exit codes: 0=success or ignored, 1=send failed.
func CmdNotify(args []string) int {
	if len(args) < 2 {
		log.Printf("[WARN] not enough args: need at least 2 (event-type event-description)")
		return 0
	}

	eventType := args[0]
	eventDescription := strings.Join(args[1:], " ")

	log.Printf("[INFO] notify: type=%s description=%s", eventType, eventDescription)

	if eventType != "+sdown" && eventType != "-sdown" {
		return 0
	}

	parsed := parseSdownDescription(eventDescription)
	if parsed == nil {
		return 0
	}

	cfg := LoadConfig()
	if cfg.SentinelNodeName == "" || cfg.GroupName == "" {
		log.Printf("[ERROR] sentinel_node_name or group_name not configured")
		return 0
	}

	monitorEventType := "replica_down"
	if eventType == "-sdown" {
		monitorEventType = "replica_up"
	}

	log.Printf("[INFO] %s event: master=%s ip=%s:%d",
		monitorEventType, parsed["master_name"], parsed["ip"], parsed["port"])

	payload := map[string]any{
		"group_name":         cfg.GroupName,
		"master_name":        parsed["master_name"],
		"event_type":         monitorEventType,
		"from_ip":            parsed["ip"],
		"from_port":          parsed["port"],
		"sentinel_node_name": cfg.SentinelNodeName,
	}

	if SendEvent(cfg, payload) {
		return 0
	}
	return 1
}
