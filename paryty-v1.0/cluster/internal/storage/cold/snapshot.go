package cold

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/klauspost/compress/zstd"
	"github.com/minio/minio-go/v7"
	"github.com/paryty/paryty-v1.0/cluster/internal/models"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

const bucketSnapshots = "paryty-snapshots"

var ErrSnapshotNotFound = fmt.Errorf("snapshot not found")

type SnapshotConfig struct {
	Interval        time.Duration `yaml:"interval" json:"interval"`
	Compression     bool          `yaml:"compression" json:"compression"`
	RetentionDays   int           `yaml:"retention_days" json:"retention_days"`
	MaxSnapshotSize int64         `yaml:"max_snapshot_size" json:"max_snapshot_size"`
}

func (c *SnapshotConfig) applyDefaults() {
	if c.Interval == 0 {
		c.Interval = 5 * time.Minute
	}
	if !c.Compression {
		c.Compression = true
	}
	if c.RetentionDays == 0 {
		c.RetentionDays = 7
	}
	if c.MaxSnapshotSize == 0 {
		c.MaxSnapshotSize = 50 * 1024 * 1024
	}
}

type Snapshot struct {
	ID         string                   `json:"id"`
	TenantID   string                   `json:"tenant_id"`
	Timestamp  time.Time                `json:"timestamp"`
	Topology   *models.Topology         `json:"topology"`
	Agents     []models.AgentInfo       `json:"agents"`
	Metrics    map[string]MetricSummary `json:"metrics"`
	Alerts     []models.Alert           `json:"alerts"`
	Graph      *GraphSnapshot           `json:"graph"`
	Metadata   SnapshotMetadata         `json:"metadata"`
	SizeBytes  int64                    `json:"size_bytes"`
	Compressed bool                     `json:"compressed"`
}

type MetricSummary struct {
	AgentID string                  `json:"agent_id"`
	CPU     *models.CPUMetrics      `json:"cpu,omitempty"`
	Memory  *models.MemoryMetrics   `json:"memory,omitempty"`
	Disk    []models.DiskMetrics    `json:"disk,omitempty"`
	Network []models.NetworkMetrics `json:"network,omitempty"`
}

type GraphSnapshot struct {
	Nodes []models.TopologyNode `json:"nodes"`
	Edges []models.TopologyEdge `json:"edges"`
}

type SnapshotMetadata struct {
	PipelineVersion string    `json:"pipeline_version"`
	AgentCount      int       `json:"agent_count"`
	MetricCount     int       `json:"metric_count"`
	AlertCount      int       `json:"alert_count"`
	CreatedAt       time.Time `json:"created_at"`
}

// SnapshotMeta is a lightweight snapshot summary for listing.
type SnapshotMeta struct {
	ID         string    `json:"id"`
	TenantID   string    `json:"tenant_id"`
	Timestamp  time.Time `json:"timestamp"`
	AgentCount int       `json:"agent_count"`
	MetricCount int      `json:"metric_count"`
	AlertCount int       `json:"alert_count"`
	SizeBytes  int64     `json:"size_bytes"`
	Compressed bool      `json:"compressed"`
}

type HotStoreReader interface {
	GetTopology(ctx context.Context, tenant string) (*models.Topology, error)
	GetAllAgentStates(ctx context.Context, tenant string) ([]models.AgentInfo, error)
	GetLatestMetrics(ctx context.Context, tenant, agentID string) (*models.MetricBatch, error)
	GetActiveAlerts(ctx context.Context, tenant string) ([]models.Alert, error)
}

type SnapshotManager struct {
	config    SnapshotConfig
	store     *Client
	hotStore  HotStoreReader
	dragonfly *redis.Client
	questdb   *pgxpool.Pool
	logger    *zap.Logger
}

func NewSnapshotManager(config SnapshotConfig, store *Client, hotStore HotStoreReader, dragonfly *redis.Client, questdb *pgxpool.Pool, logger *zap.Logger) *SnapshotManager {
	if store == nil {
		panic("snapshot: store must not be nil")
	}
	if hotStore == nil {
		panic("snapshot: hotStore must not be nil")
	}
	if logger == nil {
		logger = zap.NewNop()
	}
	config.applyDefaults()
	return &SnapshotManager{config: config, store: store, hotStore: hotStore, dragonfly: dragonfly, questdb: questdb, logger: logger}
}

func (m *SnapshotManager) TakeSnapshot(ctx context.Context, tenant string) (*Snapshot, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("snapshot: context cancelled: %w", err)
	}
	snapshotID := uuid.New().String()
	now := time.Now()

	snapshot, err := m.assembleSnapshot(ctx, tenant, snapshotID, now)
	if err != nil {
		return nil, fmt.Errorf("snapshot: assemble: %w", err)
	}
	jsonData, err := json.Marshal(snapshot)
	if err != nil {
		return nil, fmt.Errorf("snapshot: marshal: %w", err)
	}

	uploadData, contentType, suffix := jsonData, "application/json", ".json"
	if m.config.Compression {
		compressed, cerr := compressData(jsonData)
		if cerr != nil {
			return nil, fmt.Errorf("snapshot: compress: %w", cerr)
		}
		uploadData, contentType, suffix = compressed, "application/octet-stream", ".json.zst"
		snapshot.Compressed = true
	}
	snapshot.SizeBytes = int64(len(uploadData))

	key := snapshotObjectKey(tenant, now, snapshotID, suffix)
	if err := m.uploadSnapshot(ctx, key, uploadData, contentType); err != nil {
		return nil, fmt.Errorf("snapshot: upload: %w", err)
	}
	if err := m.cacheLatestSnapshot(ctx, tenant, snapshot); err != nil {
		m.logger.Warn("failed to cache latest snapshot", zap.String("tenant", tenant), zap.Error(err))
	}
	if err := m.recordSnapshotMetadata(ctx, snapshot); err != nil {
		m.logger.Warn("failed to record snapshot metadata", zap.String("tenant", tenant), zap.Error(err))
	}
	m.logger.Info("snapshot taken", zap.String("tenant", tenant), zap.String("snapshot_id", snapshotID), zap.Int64("size_bytes", snapshot.SizeBytes), zap.Int("agents", snapshot.Metadata.AgentCount))
	return snapshot, nil
}

func (m *SnapshotManager) GetSnapshot(ctx context.Context, tenant, id string) (*Snapshot, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("snapshot: context cancelled: %w", err)
	}
	snapshot, err := m.getCachedSnapshot(ctx, tenant, id)
	if err == nil && snapshot != nil {
		return snapshot, nil
	}
	snapshot, err = m.fetchSnapshotFromCold(ctx, tenant, id)
	if err != nil {
		return nil, fmt.Errorf("snapshot: get %s: %w", id, err)
	}
	_ = m.cacheSnapshotByID(ctx, tenant, id, snapshot)
	return snapshot, nil
}

// ListSnapshots queries the topology_snapshots metadata table for snapshots
// within the given time range, ordered by timestamp descending.
func (m *SnapshotManager) ListSnapshots(ctx context.Context, tenant string, start, end time.Time, limit int) ([]SnapshotMeta, error) {
	if m.questdb == nil {
		return nil, fmt.Errorf("snapshot: questdb not configured")
	}
	if limit <= 0 {
		limit = 50
	}
	if limit > 1000 {
		limit = 1000
	}

	rows, err := m.questdb.Query(ctx,
		`SELECT snapshot_id, tenant_id, timestamp, agent_count, metric_count, alert_count, size_bytes, compressed
		FROM topology_snapshots
		WHERE tenant_id=$1 AND timestamp>=$2 AND timestamp<=$3
		ORDER BY timestamp DESC
		LIMIT $4`,
		tenant, start, end, limit)
	if err != nil {
		return nil, fmt.Errorf("snapshot: list: %w", err)
	}
	defer rows.Close()

	var metas []SnapshotMeta
	for rows.Next() {
		var m SnapshotMeta
		if err := rows.Scan(&m.ID, &m.TenantID, &m.Timestamp, &m.AgentCount, &m.MetricCount, &m.AlertCount, &m.SizeBytes, &m.Compressed); err != nil {
			continue
		}
		metas = append(metas, m)
	}
	return metas, rows.Err()
}

func (m *SnapshotManager) GetNearestSnapshot(ctx context.Context, tenant string, target time.Time) (*Snapshot, error) {
	if m.questdb == nil {
		return nil, fmt.Errorf("snapshot: questdb not configured")
	}
	id, _, err := m.queryNearestSnapshotID(ctx, tenant, target)
	if err != nil {
		return nil, fmt.Errorf("snapshot: nearest to %v: %w", target, err)
	}
	return m.GetSnapshot(ctx, tenant, id)
}

func (m *SnapshotManager) ReconstructState(ctx context.Context, tenant string, target time.Time) (*Snapshot, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("snapshot: context cancelled: %w", err)
	}
	snapshot, err := m.GetNearestSnapshot(ctx, tenant, target)
	if err != nil {
		return nil, fmt.Errorf("snapshot: reconstruct: load base: %w", err)
	}
	if target.Sub(snapshot.Timestamp) <= m.config.Interval {
		return snapshot, nil
	}
	events, err := m.queryEventsBetween(ctx, tenant, snapshot.Timestamp, target)
	if err != nil {
		m.logger.Warn("failed to query events, returning base", zap.Error(err))
		return snapshot, nil
	}
	if len(events) == 0 {
		return snapshot, nil
	}
	return m.replayEvents(snapshot, events, target), nil
}

func (m *SnapshotManager) CleanupExpired(ctx context.Context, tenant string) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, fmt.Errorf("snapshot: context cancelled: %w", err)
	}
	cutoff := time.Now().AddDate(0, 0, -m.config.RetentionDays)
	prefix := fmt.Sprintf("snapshots/%s/", tenant)
	objects := m.store.minio.ListObjects(ctx, bucketSnapshots, minio.ListObjectsOptions{Prefix: prefix, Recursive: true})
	deleted := 0
	for obj := range objects {
		if obj.Err != nil {
			continue
		}
		if obj.LastModified.After(cutoff) {
			continue
		}
		if err := m.store.minio.RemoveObject(ctx, bucketSnapshots, obj.Key, minio.RemoveObjectOptions{}); err != nil {
			m.logger.Error("delete expired failed", zap.String("key", obj.Key), zap.Error(err))
			continue
		}
		if sid := extractSnapshotIDFromKey(obj.Key); sid != "" && m.dragonfly != nil {
			m.dragonfly.Del(ctx, snapshotCacheKey(tenant, sid))
			m.dragonfly.Del(ctx, snapshotLatestCacheKey(tenant))
		}
		deleted++
	}
	return deleted, nil
}

// ---- Key generation ----

func snapshotObjectKey(tenant string, ts time.Time, id, suffix string) string {
	return fmt.Sprintf("snapshots/%s/%s/%s%s", tenant, ts.Format("2006/01/02"), id, suffix)
}

func snapshotCacheKey(tenant, id string) string {
	return fmt.Sprintf("paryty:%s:snapshot:%s", tenant, id)
}

func snapshotLatestCacheKey(tenant string) string {
	return fmt.Sprintf("paryty:%s:snapshot:latest", tenant)
}

func extractSnapshotIDFromKey(key string) string {
	parts := strings.Split(key, "/")
	if len(parts) < 5 {
		return ""
	}
	name := parts[len(parts)-1]
	name = strings.TrimSuffix(name, ".json.zst")
	name = strings.TrimSuffix(name, ".json")
	return name
}

// ---- Assembly ----

func (m *SnapshotManager) assembleSnapshot(ctx context.Context, tenant, id string, now time.Time) (*Snapshot, error) {
	topology, err := m.hotStore.GetTopology(ctx, tenant)
	if err != nil {
		m.logger.Debug("topology not available", zap.String("tenant", tenant), zap.Error(err))
		topology = &models.Topology{}
	}
	agents, err := m.hotStore.GetAllAgentStates(ctx, tenant)
	if err != nil {
		m.logger.Debug("agents not available", zap.String("tenant", tenant), zap.Error(err))
		agents = []models.AgentInfo{}
	}
	metrics := make(map[string]MetricSummary, len(agents))
	for _, agent := range agents {
		if summary, err := m.buildMetricSummary(ctx, tenant, agent.ID); err == nil {
			metrics[agent.ID] = summary
		}
	}
	alerts, err := m.hotStore.GetActiveAlerts(ctx, tenant)
	if err != nil {
		alerts = []models.Alert{}
	}
	graph := &GraphSnapshot{}
	if topology != nil {
		graph.Nodes = topology.Nodes
		graph.Edges = topology.Edges
	}
	return &Snapshot{
		ID: id, TenantID: tenant, Timestamp: now,
		Topology: topology, Agents: agents, Metrics: metrics,
		Alerts: alerts, Graph: graph,
		Metadata: SnapshotMetadata{PipelineVersion: "1.0.0", AgentCount: len(agents), MetricCount: len(metrics), AlertCount: len(alerts), CreatedAt: now},
	}, nil
}

func (m *SnapshotManager) buildMetricSummary(ctx context.Context, tenant, agentID string) (MetricSummary, error) {
	batch, err := m.hotStore.GetLatestMetrics(ctx, tenant, agentID)
	if err != nil {
		return MetricSummary{}, err
	}
	s := MetricSummary{AgentID: agentID}
	if len(batch.CPU) > 0 {
		s.CPU = &batch.CPU[len(batch.CPU)-1]
	}
	if len(batch.Memory) > 0 {
		s.Memory = &batch.Memory[len(batch.Memory)-1]
	}
	if len(batch.Disk) > 0 {
		s.Disk = batch.Disk
	}
	if len(batch.Network) > 0 {
		s.Network = batch.Network
	}
	return s, nil
}

// ---- SeaweedFS ----

func (m *SnapshotManager) uploadSnapshot(ctx context.Context, key string, data []byte, ct string) error {
	_, err := m.store.minio.PutObject(ctx, bucketSnapshots, key, bytes.NewReader(data), int64(len(data)), minio.PutObjectOptions{ContentType: ct})
	return err
}

func (m *SnapshotManager) fetchSnapshotFromCold(ctx context.Context, tenant, id string) (*Snapshot, error) {
	now := time.Now()
	for d := 0; d <= m.config.RetentionDays; d++ {
		date := now.AddDate(0, 0, -d)
		for _, suffix := range []string{".json.zst", ".json"} {
			key := snapshotObjectKey(tenant, date, id, suffix)
			obj, err := m.store.minio.GetObject(ctx, bucketSnapshots, key, minio.GetObjectOptions{})
			if err != nil {
				continue
			}
			data, err := io.ReadAll(obj)
			obj.Close()
			if err != nil {
				continue
			}
			if suffix == ".json.zst" {
				data, err = decompressData(data)
				if err != nil {
					continue
				}
			}
			var snap Snapshot
			if err := json.Unmarshal(data, &snap); err != nil {
				return nil, fmt.Errorf("unmarshal: %w", err)
			}
			return &snap, nil
		}
	}
	return nil, ErrSnapshotNotFound
}

// ---- Dragonfly cache ----

func (m *SnapshotManager) cacheLatestSnapshot(ctx context.Context, tenant string, snap *Snapshot) error {
	if m.dragonfly == nil {
		return nil
	}
	data, err := json.Marshal(snap)
	if err != nil {
		return err
	}
	return m.dragonfly.Set(ctx, snapshotLatestCacheKey(tenant), data, m.config.Interval*2).Err()
}

func (m *SnapshotManager) cacheSnapshotByID(ctx context.Context, tenant, id string, snap *Snapshot) error {
	if m.dragonfly == nil {
		return nil
	}
	data, err := json.Marshal(snap)
	if err != nil {
		return err
	}
	return m.dragonfly.Set(ctx, snapshotCacheKey(tenant, id), data, 15*time.Minute).Err()
}

func (m *SnapshotManager) getCachedSnapshot(ctx context.Context, tenant, id string) (*Snapshot, error) {
	if m.dragonfly == nil {
		return nil, fmt.Errorf("no cache")
	}
	data, err := m.dragonfly.Get(ctx, snapshotCacheKey(tenant, id)).Bytes()
	if err != nil {
		return nil, err
	}
	var snap Snapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		return nil, err
	}
	return &snap, nil
}

// ---- QuestDB ----

func (m *SnapshotManager) recordSnapshotMetadata(ctx context.Context, snap *Snapshot) error {
	if m.questdb == nil {
		return nil
	}
	_, err := m.questdb.Exec(ctx,
		`INSERT INTO topology_snapshots (snapshot_id, tenant_id, timestamp, agent_count, metric_count, alert_count, size_bytes, compressed) VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`,
		snap.ID, snap.TenantID, snap.Timestamp, snap.Metadata.AgentCount, snap.Metadata.MetricCount, snap.Metadata.AlertCount, snap.SizeBytes, snap.Compressed)
	return err
}

func (m *SnapshotManager) queryNearestSnapshotID(ctx context.Context, tenant string, target time.Time) (string, time.Time, error) {
	var id string
	var ts time.Time
	err := m.questdb.QueryRow(ctx,
		`SELECT snapshot_id, timestamp FROM topology_snapshots WHERE tenant_id=$1 AND timestamp<=$2 ORDER BY timestamp DESC LIMIT 1`,
		tenant, target).Scan(&id, &ts)
	return id, ts, err
}

func (m *SnapshotManager) queryEventsBetween(ctx context.Context, tenant string, from, to time.Time) ([]models.Event, error) {
	if m.questdb == nil {
		return nil, nil
	}
	rows, err := m.questdb.Query(ctx,
		`SELECT id, agent_id, source, category, severity, title, description, timestamp FROM events WHERE tenant_id=$1 AND timestamp>$2 AND timestamp<=$3 ORDER BY timestamp`,
		tenant, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var events []models.Event
	for rows.Next() {
		var e models.Event
		if err := rows.Scan(&e.ID, &e.AgentID, &e.Source, &e.Category, &e.Severity, &e.Title, &e.Description, &e.Timestamp); err != nil {
			continue
		}
		events = append(events, e)
	}
	return events, rows.Err()
}

// ---- Event replay ----

func (m *SnapshotManager) replayEvents(base *Snapshot, events []models.Event, target time.Time) *Snapshot {
	r := copySnapshot(base)
	r.Timestamp = target
	for _, ev := range events {
		if ev.Timestamp.After(target) {
			break
		}
		switch {
		case ev.Category == "deployment" && strings.Contains(ev.Title, "agent registered"):
			m.replayAgentAdded(r, ev)
		case ev.Category == "deployment" && strings.Contains(ev.Title, "agent deregistered"):
			m.replayAgentRemoved(r, ev)
		case ev.Severity == models.EventSeverityCritical || ev.Severity == models.EventSeverityError:
			m.replayAlertFromEvent(r, ev)
		}
	}
	r.Metadata.CreatedAt = target
	r.ID = uuid.New().String()
	r.Compressed = false
	r.SizeBytes = 0
	return r
}

func (m *SnapshotManager) replayAgentAdded(snap *Snapshot, ev models.Event) {
	aid := ev.AgentID
	if aid == "" {
		aid = ev.Labels["agent_id"]
	}
	if aid == "" {
		return
	}
	for _, a := range snap.Agents {
		if a.ID == aid {
			return
		}
	}
	snap.Agents = append(snap.Agents, models.AgentInfo{
		ID: aid, Hostname: ev.Labels["hostname"], IPAddress: ev.Labels["ip_address"],
		OS: ev.Labels["os"], Status: models.AgentStatusOnline, RegisteredAt: ev.Timestamp, Labels: ev.Labels,
	})
	snap.Metadata.AgentCount = len(snap.Agents)
}

func (m *SnapshotManager) replayAgentRemoved(snap *Snapshot, ev models.Event) {
	aid := ev.AgentID
	if aid == "" {
		aid = ev.Labels["agent_id"]
	}
	for i, a := range snap.Agents {
		if a.ID == aid {
			snap.Agents = append(snap.Agents[:i], snap.Agents[i+1:]...)
			snap.Metadata.AgentCount = len(snap.Agents)
			delete(snap.Metrics, aid)
			snap.Metadata.MetricCount = len(snap.Metrics)
			return
		}
	}
}

func (m *SnapshotManager) replayAlertFromEvent(snap *Snapshot, ev models.Event) {
	snap.Alerts = append(snap.Alerts, models.Alert{
		ID: ev.ID, Name: ev.Title, Description: ev.Description,
		Severity: models.AlertSeverity(ev.Severity), Status: models.AlertStatusFiring,
		Source: ev.Source, AgentID: ev.AgentID, Labels: ev.Labels,
		StartsAt: ev.Timestamp, UpdatedAt: ev.Timestamp,
	})
	snap.Metadata.AlertCount = len(snap.Alerts)
}

// ---- Compression ----

func compressData(data []byte) ([]byte, error) {
	var buf bytes.Buffer
	enc, err := zstd.NewWriter(&buf)
	if err != nil {
		return nil, fmt.Errorf("zstd encoder: %w", err)
	}
	if _, err := enc.Write(data); err != nil {
		enc.Close()
		return nil, fmt.Errorf("zstd write: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("zstd close: %w", err)
	}
	return buf.Bytes(), nil
}

func decompressData(data []byte) ([]byte, error) {
	dec, err := zstd.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("zstd decoder: %w", err)
	}
	defer dec.Close()
	out, err := io.ReadAll(dec)
	if err != nil {
		return nil, fmt.Errorf("zstd read: %w", err)
	}
	return out, nil
}

// ---- Deep copy ----

func copySnapshot(src *Snapshot) *Snapshot {
	dst := &Snapshot{ID: src.ID, TenantID: src.TenantID, Timestamp: src.Timestamp, SizeBytes: src.SizeBytes, Compressed: src.Compressed}
	if src.Topology != nil {
		t := *src.Topology
		t.Nodes = make([]models.TopologyNode, len(src.Topology.Nodes))
		copy(t.Nodes, src.Topology.Nodes)
		t.Edges = make([]models.TopologyEdge, len(src.Topology.Edges))
		copy(t.Edges, src.Topology.Edges)
		dst.Topology = &t
	}
	if len(src.Agents) > 0 {
		dst.Agents = make([]models.AgentInfo, len(src.Agents))
		copy(dst.Agents, src.Agents)
		for i := range src.Agents {
			if src.Agents[i].Labels != nil {
				dst.Agents[i].Labels = make(map[string]string, len(src.Agents[i].Labels))
				for k, v := range src.Agents[i].Labels {
					dst.Agents[i].Labels[k] = v
				}
			}
		}
	}
	if len(src.Metrics) > 0 {
		dst.Metrics = make(map[string]MetricSummary, len(src.Metrics))
		for k, v := range src.Metrics {
			ms := v
			if v.CPU != nil {
				c := *v.CPU
				ms.CPU = &c
			}
			if v.Memory != nil {
				m2 := *v.Memory
				ms.Memory = &m2
			}
			dst.Metrics[k] = ms
		}
	}
	if len(src.Alerts) > 0 {
		dst.Alerts = make([]models.Alert, len(src.Alerts))
		copy(dst.Alerts, src.Alerts)
		for i := range src.Alerts {
			if src.Alerts[i].Labels != nil {
				dst.Alerts[i].Labels = make(map[string]string, len(src.Alerts[i].Labels))
				for k, v := range src.Alerts[i].Labels {
					dst.Alerts[i].Labels[k] = v
				}
			}
			if src.Alerts[i].Annotations != nil {
				dst.Alerts[i].Annotations = make(map[string]string, len(src.Alerts[i].Annotations))
				for k, v := range src.Alerts[i].Annotations {
					dst.Alerts[i].Annotations[k] = v
				}
			}
		}
	}
	if src.Graph != nil {
		g := &GraphSnapshot{}
		g.Nodes = make([]models.TopologyNode, len(src.Graph.Nodes))
		copy(g.Nodes, src.Graph.Nodes)
		g.Edges = make([]models.TopologyEdge, len(src.Graph.Edges))
		copy(g.Edges, src.Graph.Edges)
		dst.Graph = g
	}
	dst.Metadata = src.Metadata
	return dst
}
