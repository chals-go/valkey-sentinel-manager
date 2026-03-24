package api

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/chals-go/valkey-sentinel-manager/internal/models"
	"github.com/chals-go/valkey-sentinel-manager/internal/store"
)

type dnsMappingReq struct {
	Zone       string `json:"zone"`
	RecordName string `json:"record_name"`
	RecordType string `json:"record_type"`
	TTL        int    `json:"ttl"`
}

type clusterCreateRequest struct {
	GroupName        string         `json:"group_name"`
	MasterName       string         `json:"master_name"`
	SentinelAddrs    []string       `json:"sentinel_addrs"`
	DNSProvider      string         `json:"dns_provider"`
	PrimaryDNS       dnsMappingReq  `json:"primary_dns"`
	ReplicaDNS       *dnsMappingReq `json:"replica_dns,omitempty"`
	MultiReplica     bool           `json:"multi_replica"`
	RedisPassword    string         `json:"redis_password"`
	SentinelPassword string         `json:"sentinel_password"`
	QuorumMode       bool           `json:"quorum_mode"`
	QuorumThreshold  int            `json:"quorum_threshold"`
}

// ListClustersHandler returns all registered clusters.
func ListClustersHandler(s store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		clusters, err := s.ListClusters(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to list clusters")
			return
		}
		writeJSON(w, http.StatusOK, Response{Status: "ok", Data: clusters, Message: "ok"})
	}
}

// CreateClusterHandler registers a new cluster.
func CreateClusterHandler(s store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req clusterCreateRequest
		if err := readJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}

		_, err := s.GetCluster(r.Context(), req.MasterName)
		if err == nil {
			writeError(w, http.StatusConflict, "cluster already registered: "+req.MasterName)
			return
		}

		cluster := &models.Cluster{
			GroupName:     req.GroupName,
			MasterName:    req.MasterName,
			SentinelAddrs: req.SentinelAddrs,
			DNSProvider:   req.DNSProvider,
			PrimaryDNS: models.DNSMapping{
				Zone: req.PrimaryDNS.Zone, RecordName: req.PrimaryDNS.RecordName,
				RecordType: req.PrimaryDNS.RecordType, TTL: req.PrimaryDNS.TTL,
			},
			MultiReplica:     req.MultiReplica,
			RedisPassword:    req.RedisPassword,
			SentinelPassword: req.SentinelPassword,
			QuorumMode:       req.QuorumMode,
			QuorumThreshold:  req.QuorumThreshold,
		}
		if req.ReplicaDNS != nil {
			cluster.ReplicaDNS = &models.DNSMapping{
				Zone: req.ReplicaDNS.Zone, RecordName: req.ReplicaDNS.RecordName,
				RecordType: req.ReplicaDNS.RecordType, TTL: req.ReplicaDNS.TTL,
			}
		}

		if err := s.RegisterCluster(r.Context(), cluster); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to register cluster")
			return
		}
		slog.Info("cluster registered", "group", cluster.GroupName, "master", cluster.MasterName)
		writeJSON(w, http.StatusCreated, Response{Status: "ok", Data: cluster, Message: "cluster registered"})
	}
}

// GetClusterHandler returns a cluster by master name.
func GetClusterHandler(s store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		masterName := r.PathValue("masterName")
		cluster, err := s.GetCluster(r.Context(), masterName)
		if err != nil {
			if errors.Is(err, store.ErrNotFound) {
				writeError(w, http.StatusNotFound, "cluster not found: "+masterName)
				return
			}
			writeError(w, http.StatusInternalServerError, "failed to get cluster")
			return
		}
		writeJSON(w, http.StatusOK, Response{Status: "ok", Data: cluster, Message: "ok"})
	}
}

// DeleteClusterHandler unregisters a cluster.
func DeleteClusterHandler(s store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		masterName := r.PathValue("masterName")
		removed, err := s.UnregisterCluster(r.Context(), masterName)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to delete cluster")
			return
		}
		if !removed {
			writeError(w, http.StatusNotFound, "cluster not found: "+masterName)
			return
		}
		slog.Info("cluster unregistered", "master", masterName)
		writeJSON(w, http.StatusOK, Response{Status: "ok", Message: "cluster unregistered"})
	}
}
