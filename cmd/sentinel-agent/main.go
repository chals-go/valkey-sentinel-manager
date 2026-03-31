// Package main is the entry point for the sentinel-agent CLI.
//
// Usage:
//
//	sentinel-agent reconfig <master-name> <role> <state> <from-ip> <from-port> <to-ip> <to-port>
//	sentinel-agent notify <event-type> <event-description...>
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/chals-go/valkey-sentinel-manager/internal/agent"
)

// version is set by -ldflags at build time.
var version = "dev"

const usage = `Usage: sentinel-agent <command> [args...]

Commands:
  reconfig <master-name> <role> <state> <from-ip> <from-port> <to-ip> <to-port>
      client-reconfig-script handler. Sends failover events to Monitor.

  notify <event-type> <event-description>
      notification-script handler. Sends +sdown/-sdown slave events to Monitor.

Configuration:
  YAML file (default: /etc/valkey/sentinel-agent.yaml)
  Override config path with SMGR_AGENT_CONFIG env var.
  Environment variables override YAML values.

  SMGR_AGENT_CONFIG        Config file path (default: /etc/valkey/sentinel-agent.yaml)
  SMGR_MONITOR_URL         Monitor server URL (e.g. http://10.0.0.100:8000)
  SMGR_API_KEY             API auth token
  SMGR_SENTINEL_NODE_NAME  Unique Sentinel node ID
  SMGR_GROUP_NAME          Sentinel group name registered in Monitor
  SMGR_TIMEOUT_SECONDS     HTTP timeout (default: 10)
  SMGR_RETRY_COUNT         Retry count (default: 2)
`

func main() {
	// Auto-detect subcommand from binary name (symlink support).
	// sentinel-agent-reconfig → reconfig
	// sentinel-agent-notify   → notify
	binName := filepath.Base(os.Args[0])
	var command string
	var args []string

	if strings.HasSuffix(binName, "-reconfig") {
		command = "reconfig"
		args = os.Args[1:]
	} else if strings.HasSuffix(binName, "-notify") {
		command = "notify"
		args = os.Args[1:]
	} else if len(os.Args) >= 2 {
		command = os.Args[1]
		args = os.Args[2:]
	} else {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}

	var exitCode int
	switch command {
	case "reconfig":
		exitCode = agent.CmdReconfig(args)
	case "notify":
		exitCode = agent.CmdNotify(args)
	default:
		fmt.Fprint(os.Stderr, usage)
		exitCode = 2
	}
	os.Exit(exitCode)
}
