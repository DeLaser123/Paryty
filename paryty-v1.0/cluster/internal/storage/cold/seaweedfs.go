// Package cold implements the cold storage tier using SeaweedFS (S3-compatible).
// This tier stores long-term archival data for compliance and historical analysis.
package cold

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"github.com/paryty/paryty-v1.0/cluster/internal/models"
)

const (
	// Bucket names
	bucketMetrics = "paryty-metrics"
	bucketTraces  = "paryty-traces"
	bucketEvents  = "paryty-events"
)

// Config contains configuration for the SeaweedFS client.
type Config struct {
	Endpoint  string `yaml:"endpoint" json:"endpoint"`
	AccessKey string `yaml:"access_key" json:"access_key"`
	SecretKey string `yaml:"secret_key" json:"secret_key"`
	UseSSL    bool   `yaml:"use_ssl" json:"use_ssl"`
	Region    string `yaml:"region" json:"region"`
}

// Client is the SeaweedFS cold storage client.
type Client struct {
	minio *minio.Client
	cfg   Config
}

// New creates a new SeaweedFS client.
func New(ctx context.Context, cfg Config) (*Client, error) {
	minioClient, err := minio.New(cfg.Endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, ""),
		Secure: cfg.UseSSL,
		Region: cfg.Region,
	})
	if err != nil {
		return nil, fmt.Errorf("create minio client: %w", err)
	}

	client := &Client{
		minio: minioClient,
		cfg:   cfg,
	}

	// Ensure buckets exist
	if err := client.ensureBuckets(ctx); err != nil {
		return nil, fmt.Errorf("ensure buckets: %w", err)
	}

	return client, nil
}

// ensureBuckets creates required buckets if they don't exist.
func (c *Client) ensureBuckets(ctx context.Context) error {
	buckets := []string{bucketMetrics, bucketTraces, bucketEvents}
	for _, bucket := range buckets {
		exists, err := c.minio.BucketExists(ctx, bucket)
		if err != nil {
			return fmt.Errorf("check bucket %s: %w", bucket, err)
		}
		if !exists {
			if err := c.minio.MakeBucket(ctx, bucket, minio.MakeBucketOptions{}); err != nil {
				return fmt.Errorf("create bucket %s: %w", bucket, err)
			}
		}
	}
	return nil
}

// Close is a no-op for the SeaweedFS client.
func (c *Client) Close() error {
	return nil
}

// ---- Metric Operations ----

// StoreMetricBatch stores a metric batch in cold storage.
func (c *Client) StoreMetricBatch(ctx context.Context, batch *models.MetricBatch) error {
	data, err := json.Marshal(batch)
	if err != nil {
		return fmt.Errorf("marshal batch: %w", err)
	}

	// Use date-based path: metrics/2024/01/15/agent-id/timestamp.json
	key := fmt.Sprintf("metrics/%s/%s.json",
		batch.Timestamp.Format("2006/01/02"),
		batch.AgentID)

	_, err = c.minio.PutObject(ctx, bucketMetrics, key, bytes.NewReader(data), int64(len(data)),
		minio.PutObjectOptions{ContentType: "application/json"})
	if err != nil {
		return fmt.Errorf("put object: %w", err)
	}
	return nil
}

// RetrieveMetricBatch retrieves a metric batch from cold storage.
func (c *Client) RetrieveMetricBatch(ctx context.Context, agentID string, date time.Time) (*models.MetricBatch, error) {
	key := fmt.Sprintf("metrics/%s/%s.json",
		date.Format("2006/01/02"),
		agentID)

	obj, err := c.minio.GetObject(ctx, bucketMetrics, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, fmt.Errorf("get object: %w", err)
	}
	defer obj.Close()

	data, err := io.ReadAll(obj)
	if err != nil {
		return nil, fmt.Errorf("read object: %w", err)
	}

	var batch models.MetricBatch
	if err := json.Unmarshal(data, &batch); err != nil {
		return nil, fmt.Errorf("unmarshal batch: %w", err)
	}
	return &batch, nil
}

// ---- Trace Operations ----

// StoreTrace stores a trace in cold storage.
func (c *Client) StoreTrace(ctx context.Context, trace *models.Trace) error {
	data, err := json.Marshal(trace)
	if err != nil {
		return fmt.Errorf("marshal trace: %w", err)
	}

	key := fmt.Sprintf("traces/%s/%s.json",
		trace.StartTime.Format("2006/01/02"),
		trace.TraceID)

	_, err = c.minio.PutObject(ctx, bucketTraces, key, bytes.NewReader(data), int64(len(data)),
		minio.PutObjectOptions{ContentType: "application/json"})
	if err != nil {
		return fmt.Errorf("put object: %w", err)
	}
	return nil
}

// RetrieveTrace retrieves a trace from cold storage.
func (c *Client) RetrieveTrace(ctx context.Context, traceID string, date time.Time) (*models.Trace, error) {
	key := fmt.Sprintf("traces/%s/%s.json",
		date.Format("2006/01/02"),
		traceID)

	obj, err := c.minio.GetObject(ctx, bucketTraces, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, fmt.Errorf("get object: %w", err)
	}
	defer obj.Close()

	data, err := io.ReadAll(obj)
	if err != nil {
		return nil, fmt.Errorf("read object: %w", err)
	}

	var trace models.Trace
	if err := json.Unmarshal(data, &trace); err != nil {
		return nil, fmt.Errorf("unmarshal trace: %w", err)
	}
	return &trace, nil
}

// ---- Event Operations ----

// StoreEvents stores events in cold storage.
func (c *Client) StoreEvents(ctx context.Context, events []models.Event) error {
	data, err := json.Marshal(events)
	if err != nil {
		return fmt.Errorf("marshal events: %w", err)
	}

	key := fmt.Sprintf("events/%s/events.json",
		time.Now().Format("2006/01/02"))

	_, err = c.minio.PutObject(ctx, bucketEvents, key, bytes.NewReader(data), int64(len(data)),
		minio.PutObjectOptions{ContentType: "application/json"})
	if err != nil {
		return fmt.Errorf("put object: %w", err)
	}
	return nil
}

// ListObjects lists objects in a bucket with a prefix.
func (c *Client) ListObjects(ctx context.Context, bucket, prefix string) ([]string, error) {
	var keys []string
	for obj := range c.minio.ListObjects(ctx, bucket, minio.ListObjectsOptions{
		Prefix:    prefix,
		Recursive: true,
	}) {
		if obj.Err != nil {
			return nil, fmt.Errorf("list object: %w", obj.Err)
		}
		keys = append(keys, obj.Key)
	}
	return keys, nil
}
