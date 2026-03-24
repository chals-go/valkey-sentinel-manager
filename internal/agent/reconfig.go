package agent

import (
	"log"
	"strconv"
)

// CmdReconfig handles the client-reconfig-script subcommand.
// Args: <master-name> <role> <state> <from-ip> <from-port> <to-ip> <to-port>
// Exit codes: 0=success, 1=send failed (retry), 2=config error (no retry).
func CmdReconfig(args []string) int {
	if len(args) < 7 {
		log.Printf("[ERROR] not enough args: need 7 (master-name role state from-ip from-port to-ip to-port), got %d: %v", len(args), args)
		return 2
	}

	masterName := args[0]
	role := args[1]
	state := args[2]
	fromIP := args[3]
	fromPort, _ := strconv.Atoi(args[4])
	toIP := args[5]
	toPort, _ := strconv.Atoi(args[6])

	log.Printf("[INFO] reconfig: master=%s role=%s state=%s %s:%d -> %s:%d",
		masterName, role, state, fromIP, fromPort, toIP, toPort)

	cfg := LoadConfig()
	if cfg.SentinelNodeName == "" || cfg.GroupName == "" {
		log.Printf("[ERROR] sentinel_node_name or group_name not configured")
		return 2
	}

	payload := map[string]any{
		"group_name":         cfg.GroupName,
		"master_name":        masterName,
		"event_type":         "failover",
		"role":               role,
		"state":              state,
		"from_ip":            fromIP,
		"from_port":          fromPort,
		"to_ip":              toIP,
		"to_port":            toPort,
		"sentinel_node_name": cfg.SentinelNodeName,
	}

	if SendEvent(cfg, payload) {
		return 0
	}
	return 1
}
