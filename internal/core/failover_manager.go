package core

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"strings"
	"time"

	"github.com/chals-go/valkey-sentinel-manager/internal/dns"
	"github.com/chals-go/valkey-sentinel-manager/internal/models"
	"github.com/chals-go/valkey-sentinel-manager/internal/store"
)

// FailoverManager orchestrates event processing and DNS updates.
type FailoverManager struct {
	store          store.Store
	eventProcessor *EventProcessor
	dnsProviders   map[string]dns.Provider
}

// NewFailoverManager creates a FailoverManager.
func NewFailoverManager(s store.Store, ep *EventProcessor, providers map[string]dns.Provider) *FailoverManager {
	return &FailoverManager{
		store:          s,
		eventProcessor: ep,
		dnsProviders:   providers,
	}
}

// HandleEvent processes a failover event and performs DNS updates if needed.
func (fm *FailoverManager) HandleEvent(ctx context.Context, event *models.FailoverEvent) (*EventProcessResult, error) {
	cluster, err := fm.store.GetCluster(ctx, event.MasterName)
	if err != nil {
		slog.Warn("unregistered cluster event", "group", event.GroupName, "master", event.MasterName)
		return fm.eventProcessor.Process(ctx, event, true, 2)
	}

	result, err := fm.eventProcessor.Process(ctx, event, cluster.QuorumMode, cluster.QuorumThreshold)
	if err != nil {
		return nil, err
	}

	if result.ShouldUpdateDNS {
		switch event.EventType {
		case models.EventTypeFailover:
			fm.handleFailover(ctx, cluster, event)
		case models.EventTypeReplicaDown:
			fm.handleReplicaDown(ctx, cluster, event)
		case models.EventTypeReplicaUp:
			fm.handleReplicaUp(ctx, cluster, event)
		}
		fm.sendNotification(ctx, event, cluster)
	}

	return result, nil
}

func (fm *FailoverManager) sendNotification(ctx context.Context, event *models.FailoverEvent, cluster *models.Cluster) {
	webhookURL, err := fm.store.GetSlackWebhookURL(ctx)
	if err != nil || webhookURL == "" {
		return
	}
	channel, _ := fm.store.GetSlackChannel(ctx)
	SendSlackNotification(ctx, webhookURL, event, cluster, channel)
}

func validateIP(ip string) bool {
	return net.ParseIP(ip) != nil
}

func (fm *FailoverManager) getProvider(cluster *models.Cluster) dns.Provider {
	p, ok := fm.dnsProviders[cluster.DNSProvider]
	if !ok {
		slog.Error("DNS provider not found", "provider", cluster.DNSProvider, "cluster", cluster.GroupName)
		return nil
	}
	return p
}

// handleFailover updates primary DNS to the new master IP, then syncs replica DNS from Sentinel.
func (fm *FailoverManager) handleFailover(ctx context.Context, cluster *models.Cluster, event *models.FailoverEvent) {
	if !validateIP(event.ToIP) {
		slog.Error("invalid IP address", "ip", event.ToIP)
		return
	}

	provider := fm.getProvider(cluster)
	if provider == nil {
		return
	}

	lockKey := fmt.Sprintf("%s:%s:failover", cluster.GroupName, cluster.MasterName)
	acquired, _ := fm.store.AcquireLock(ctx, lockKey, 30*time.Second)
	if !acquired {
		slog.Info("lock not acquired, another instance handling", "key", lockKey)
		return
	}
	defer fm.store.ReleaseLock(ctx, lockKey)

	// 1. Update primary DNS.
	pdns := cluster.PrimaryDNS
	slog.Info("updating primary DNS", "record", pdns.RecordName+"."+pdns.Zone, "ip", event.ToIP)
	if err := provider.UpdateRecord(ctx, pdns.Zone, pdns.RecordName, pdns.RecordType, event.ToIP, pdns.TTL); err != nil {
		slog.Error("primary DNS update failed", "error", err)
		return
	}

	if _, err := provider.VerifyRecord(ctx, pdns.Zone, pdns.RecordName, event.ToIP); err != nil {
		slog.Warn("primary DNS verify failed, propagation delay possible", "error", err)
	}

	// 2. Update replica DNS using Sentinel state.
	if cluster.ReplicaDNS != nil {
		rdns := cluster.ReplicaDNS
		detail := GetMasterDetail(ctx, cluster.SentinelAddrs, cluster.MasterName, cluster.SentinelPassword)

		if detail != nil && len(detail.Slaves) > 0 {
			// Filter out both the Sentinel-reported master AND the event's new master IP.
			// During failover transition, Sentinel may not yet report the new master correctly.
			newMasterIP := event.ToIP
			var slaveIPs []string
			for _, s := range detail.Slaves {
				if s.IP == detail.MasterIP || s.IP == newMasterIP {
					continue // skip master IPs
				}
				if !strings.Contains(s.Flags, "s_down") {
					slaveIPs = append(slaveIPs, s.IP)
				}
			}
			// If no healthy slaves after filtering, try all slaves except master.
			if len(slaveIPs) == 0 {
				for _, s := range detail.Slaves {
					if s.IP != detail.MasterIP && s.IP != newMasterIP {
						slaveIPs = append(slaveIPs, s.IP)
					}
				}
			}
			if len(slaveIPs) > 0 {
				slog.Info("replica DNS reset from sentinel", "record", rdns.RecordName+"."+rdns.Zone, "ips", slaveIPs, "excluded_master", newMasterIP)
				provider.UpdateRecordValues(ctx, rdns.Zone, rdns.RecordName, rdns.RecordType, slaveIPs, rdns.TTL)
			} else {
				// All slaves are master IPs — old master demoted to slave is the only replica.
				if validateIP(event.FromIP) && event.FromIP != newMasterIP {
					slog.Info("replica DNS set to demoted master", "record", rdns.RecordName+"."+rdns.Zone, "ip", event.FromIP)
					provider.UpdateRecord(ctx, rdns.Zone, rdns.RecordName, rdns.RecordType, event.FromIP, rdns.TTL)
				} else {
					slog.Warn("no healthy slaves, keeping replica DNS", "record", rdns.RecordName+"."+rdns.Zone)
				}
			}
		} else if !cluster.MultiReplica && validateIP(event.FromIP) {
			slog.Info("replica DNS fallback (single)", "record", rdns.RecordName+"."+rdns.Zone, "ip", event.FromIP)
			provider.UpdateRecord(ctx, rdns.Zone, rdns.RecordName, rdns.RecordType, event.FromIP, rdns.TTL)
		}
	}

	slog.Info("failover completed", "cluster", cluster.GroupName, "new_master", event.ToIP)
}

// handleReplicaDown removes a downed replica IP from the replica DNS.
func (fm *FailoverManager) handleReplicaDown(ctx context.Context, cluster *models.Cluster, event *models.FailoverEvent) {
	if !validateIP(event.FromIP) || cluster.ReplicaDNS == nil {
		return
	}

	if !cluster.MultiReplica {
		slog.Info("single replica down detected, no DNS change", "cluster", cluster.GroupName, "ip", event.FromIP)
		return
	}

	provider := fm.getProvider(cluster)
	if provider == nil {
		return
	}

	lockKey := fmt.Sprintf("%s:replica_dns", cluster.GroupName)
	acquired, _ := fm.store.AcquireLock(ctx, lockKey, 30*time.Second)
	if !acquired {
		return
	}
	defer fm.store.ReleaseLock(ctx, lockKey)

	slog.Info("handling replica_down", "cluster", cluster.GroupName, "down_ip", event.FromIP)

	rdns := cluster.ReplicaDNS
	detail := GetMasterDetail(ctx, cluster.SentinelAddrs, cluster.MasterName, cluster.SentinelPassword)

	if detail != nil && len(detail.Slaves) > 0 {
		// Get healthy slaves, exclude master AND the downed replica.
		var slaveIPs []string
		for _, s := range detail.Slaves {
			if s.IP == detail.MasterIP {
				continue // skip master
			}
			if s.IP == event.FromIP {
				continue // skip the downed replica
			}
			if !strings.Contains(s.Flags, "s_down") {
				slaveIPs = append(slaveIPs, s.IP)
			}
		}
		if len(slaveIPs) > 0 {
			slog.Info("replica DNS reset (replica_down)", "record", rdns.RecordName+"."+rdns.Zone, "ips", slaveIPs, "removed", event.FromIP)
			provider.UpdateRecordValues(ctx, rdns.Zone, rdns.RecordName, rdns.RecordType, slaveIPs, rdns.TTL)
		} else {
			slog.Warn("no healthy slaves after removing down replica, keeping replica DNS", "down_ip", event.FromIP)
		}
	} else {
		// Sentinel query failed — fallback: remove specific IP.
		slog.Info("sentinel query failed, fallback remove", "ip", event.FromIP)
		provider.RemoveRecordValue(ctx, rdns.Zone, rdns.RecordName, rdns.RecordType, event.FromIP)
	}
}

// handleReplicaUp adds a recovered replica IP to the replica DNS.
func (fm *FailoverManager) handleReplicaUp(ctx context.Context, cluster *models.Cluster, event *models.FailoverEvent) {
	if !validateIP(event.FromIP) || cluster.ReplicaDNS == nil {
		return
	}

	provider := fm.getProvider(cluster)
	if provider == nil {
		return
	}

	lockKey := fmt.Sprintf("%s:replica_dns", cluster.GroupName)
	acquired, _ := fm.store.AcquireLock(ctx, lockKey, 30*time.Second)
	if !acquired {
		return
	}
	defer fm.store.ReleaseLock(ctx, lockKey)

	rdns := cluster.ReplicaDNS
	detail := GetMasterDetail(ctx, cluster.SentinelAddrs, cluster.MasterName, cluster.SentinelPassword)

	if detail != nil && len(detail.Slaves) > 0 && cluster.MultiReplica {
		slaveIPs := healthySlaveIPs(detail)
		if len(slaveIPs) == 0 {
			slaveIPs = allSlaveIPs(detail)
		}
		if len(slaveIPs) > 0 {
			slog.Info("replica DNS reset (replica_up)", "record", rdns.RecordName+"."+rdns.Zone, "ips", slaveIPs)
			provider.UpdateRecordValues(ctx, rdns.Zone, rdns.RecordName, rdns.RecordType, slaveIPs, rdns.TTL)
		}
	} else if !cluster.MultiReplica {
		if detail != nil && event.FromIP == detail.MasterIP {
			slog.Warn("replica_up IP is current primary, skipping", "ip", event.FromIP)
			return
		}
		slog.Info("replica DNS replace (single)", "record", rdns.RecordName+"."+rdns.Zone, "ip", event.FromIP)
		provider.UpdateRecord(ctx, rdns.Zone, rdns.RecordName, rdns.RecordType, event.FromIP, rdns.TTL)
	} else {
		if detail != nil && event.FromIP == detail.MasterIP {
			slog.Warn("replica_up IP is current primary, skipping", "ip", event.FromIP)
			return
		}
		provider.AddRecordValue(ctx, rdns.Zone, rdns.RecordName, rdns.RecordType, event.FromIP, rdns.TTL)
	}
}

// healthySlaveIPs returns IPs of slaves that are not s_down and not the master.
func healthySlaveIPs(detail *MasterDetail) []string {
	var ips []string
	for _, s := range detail.Slaves {
		if s.IP != detail.MasterIP && !strings.Contains(s.Flags, "s_down") {
			ips = append(ips, s.IP)
		}
	}
	return ips
}

// allSlaveIPs returns all slave IPs except the master.
func allSlaveIPs(detail *MasterDetail) []string {
	var ips []string
	for _, s := range detail.Slaves {
		if s.IP != detail.MasterIP {
			ips = append(ips, s.IP)
		}
	}
	return ips
}
