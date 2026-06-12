package simulation

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"go.uber.org/zap"
)

// BotDetector analyzes request patterns to detect automated bot traffic.
//
// V2.0 Migration: Replaces the Python BotDetector microservice that ran as a
// separate FastAPI service. The Go version runs in-process for lower latency
// and avoids a network hop for every detection request.
type BotDetector struct {
	store  StorageReader
	logger *zap.Logger
}

// NewBotDetector creates a new bot detector.
func NewBotDetector(store StorageReader, logger *zap.Logger) *BotDetector {
	return &BotDetector{
		store:  store,
		logger: logger,
	}
}

// RequestRecord represents a single HTTP request for bot analysis.
type RequestRecord struct {
	// Timestamp is when the request was made.
	Timestamp time.Time `json:"timestamp"`
	// SourceIP is the client IP address.
	SourceIP string `json:"source_ip"`
	// UserAgent is the client's user-agent string.
	UserAgent string `json:"user_agent"`
	// Path is the request path.
	Path string `json:"path"`
	// Method is the HTTP method.
	Method string `json:"method"`
	// Status is the HTTP response status code.
	Status int `json:"status"`
	// SessionID is the session identifier (cookie or header).
	SessionID string `json:"session_id"`
	// LatencyMs is the response latency in milliseconds.
	LatencyMs float64 `json:"latency_ms"`
}

// DetectBotPatterns analyzes a batch of request records for bot signatures.
// Detection is based on four features:
//  1. Request regularity (bots tend to make requests at fixed intervals)
//  2. IP diversity (botnets use many IPs; single bots use few)
//  3. User-agent analysis (known bot patterns, missing or malformed UAs)
//  4. Session duration (bots have very short or very long sessions)
func (bd *BotDetector) DetectBotPatterns(ctx context.Context, records []RequestRecord) (*BotDetectionResult, error) {
	if len(records) == 0 {
		return &BotDetectionResult{
			IsBot:          false,
			Confidence:     0,
			Recommendation: "No request data available for analysis",
		}, nil
	}

	bd.logger.Debug("Analyzing request patterns for bot signatures",
		zap.Int("record_count", len(records)),
	)

	// Feature 1: Request regularity.
	regularity := bd.computeRequestRegularity(records)

	// Feature 2: IP diversity.
	ipDiversity := bd.computeIPDiversity(records)

	// Feature 3: User-agent analysis.
	suspiciousUAs := bd.analyzeUserAgents(records)

	// Feature 4: Session duration analysis.
	avgSessionDur := bd.analyzeSessionDuration(records)

	// Combine features into a bot confidence score.
	confidence := bd.computeConfidence(regularity, ipDiversity, suspiciousUAs, avgSessionDur, len(records))
	isBot := confidence > 0.7

	pattern := bd.describePattern(regularity, ipDiversity, suspiciousUAs, avgSessionDur, len(records))
	recommendation := bd.generateRecommendation(isBot, confidence, pattern)

	result := &BotDetectionResult{
		IsBot:                isBot,
		Confidence:           confidence,
		RequestRegularity:    regularity,
		IPDiversity:          ipDiversity,
		SuspiciousUserAgents: suspiciousUAs,
		AvgSessionDuration:   avgSessionDur,
		RequestPattern:       pattern,
		Recommendation:       recommendation,
	}

	bd.logger.Info("Bot detection completed",
		zap.Bool("is_bot", isBot),
		zap.Float64("confidence", confidence),
		zap.String("pattern", pattern),
	)

	return result, nil
}

// computeRequestRegularity measures how regular request intervals are.
// Returns a value from 0.0 (completely random) to 1.0 (perfectly regular).
func (bd *BotDetector) computeRequestRegularity(records []RequestRecord) float64 {
	if len(records) < 2 {
		return 0
	}

	// Sort records by timestamp.
	sorted := make([]RequestRecord, len(records))
	copy(sorted, records)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Timestamp.Before(sorted[j].Timestamp)
	})

	// Compute inter-request intervals.
	intervals := make([]float64, 0, len(sorted)-1)
	for i := 1; i < len(sorted); i++ {
		dur := sorted[i].Timestamp.Sub(sorted[i-1].Timestamp)
		intervals = append(intervals, float64(dur.Milliseconds()))
	}

	if len(intervals) == 0 {
		return 0
	}

	// Compute coefficient of variation (CV) of intervals.
	// Low CV = regular (bot-like), high CV = random (human-like).
	mean := 0.0
	for _, v := range intervals {
		mean += v
	}
	mean /= float64(len(intervals))

	if mean == 0 {
		return 1.0 // All requests at the same instant = perfectly regular.
	}

	variance := 0.0
	for _, v := range intervals {
		diff := v - mean
		variance += diff * diff
	}
	variance /= float64(len(intervals))
	stddev := math.Sqrt(variance)

	cv := stddev / mean

	// Convert CV to regularity score: CV of 0 → regularity 1, CV of 2+ → regularity 0.
	regularity := math.Max(0, 1.0-cv/2.0)
	return regularity
}

// computeIPDiversity measures the ratio of unique IPs to total requests.
// Returns a value from 0.0 (single IP) to 1.0 (every request from a different IP).
func (bd *BotDetector) computeIPDiversity(records []RequestRecord) float64 {
	if len(records) == 0 {
		return 0
	}

	uniqueIPs := make(map[string]bool)
	for _, r := range records {
		uniqueIPs[r.SourceIP] = true
	}

	return float64(len(uniqueIPs)) / float64(len(records))
}

// analyzeUserAgents identifies suspicious user-agent strings.
// Returns a list of suspicious user-agent strings found.
func (bd *BotDetector) analyzeUserAgents(records []RequestRecord) []string {
	botSignatures := []string{
		"bot", "crawler", "spider", "scraper", "curl", "wget",
		"python-requests", "go-http-client", "java/", "httpclient",
		"libwww", "scrapy", "selenium", "phantomjs", "headless",
	}

	seen := make(map[string]bool)
	var suspicious []string

	for _, r := range records {
		ua := strings.ToLower(r.UserAgent)

		// Empty user-agent is suspicious.
		if ua == "" {
			if !seen["(empty)"] {
				seen["(empty)"] = true
				suspicious = append(suspicious, "(empty)")
			}
			continue
		}

		// Check against known bot signatures.
		for _, sig := range botSignatures {
			if strings.Contains(ua, sig) {
				if !seen[r.UserAgent] {
					seen[r.UserAgent] = true
					suspicious = append(suspicious, r.UserAgent)
				}
				break
			}
		}
	}

	return suspicious
}

// analyzeSessionDuration computes the average session duration from grouped requests.
// Very short sessions (< 1 second) or very long sessions (> 1 hour) are bot-like.
func (bd *BotDetector) analyzeSessionDuration(records []RequestRecord) time.Duration {
	if len(records) == 0 {
		return 0
	}

	// Group by session ID.
	sessions := make(map[string][]time.Time)
	for _, r := range records {
		key := r.SessionID
		if key == "" {
			key = r.SourceIP
		}
		sessions[key] = append(sessions[key], r.Timestamp)
	}

	var totalDur time.Duration
	var count int

	for _, timestamps := range sessions {
		if len(timestamps) < 2 {
			continue
		}
		sort.Slice(timestamps, func(i, j int) bool {
			return timestamps[i].Before(timestamps[j])
		})
		dur := timestamps[len(timestamps)-1].Sub(timestamps[0])
		totalDur += dur
		count++
	}

	if count == 0 {
		return 0
	}
	return totalDur / time.Duration(count)
}

// computeConfidence combines all features into a single bot confidence score.
func (bd *BotDetector) computeConfidence(
	regularity float64,
	ipDiversity float64,
	suspiciousUAs []string,
	avgSessionDur time.Duration,
	recordCount int,
) float64 {
	// Weight each feature.
	weights := map[string]float64{
		"regularity": 0.30,
		"ua":         0.30,
		"ip":         0.20,
		"session":    0.20,
	}

	// Regularity score: high regularity → bot-like.
	regScore := regularity

	// User-agent score: more suspicious UAs → bot-like.
	uaScore := 0.0
	if recordCount > 0 && len(suspiciousUAs) > 0 {
		uaScore = math.Min(1.0, float64(len(suspiciousUAs))/3.0)
	}

	// IP diversity: very low (single IP, many requests) or very high (botnet) → bot-like.
	ipScore := 0.0
	if ipDiversity < 0.01 {
		ipScore = 0.8 // Single IP making many requests
	} else if ipDiversity > 0.9 {
		ipScore = 0.6 // Botnet: many IPs, one request each
	}
	perIPRequests := float64(recordCount) / maxFloat(float64(len(suspiciousUAs)+1), 1)
	if perIPRequests > 100 {
		ipScore = math.Max(ipScore, 0.7)
	}

	// Session score: very short or very long sessions are suspicious.
	sessionScore := 0.0
	if avgSessionDur > 0 {
		if avgSessionDur < 1*time.Second {
			sessionScore = 0.9
		} else if avgSessionDur > 1*time.Hour {
			sessionScore = 0.5
		}
	}

	confidence := weights["regularity"]*regScore +
		weights["ua"]*uaScore +
		weights["ip"]*ipScore +
		weights["session"]*sessionScore

	return math.Max(0, math.Min(1, confidence))
}

// describePattern generates a human-readable description of the detected pattern.
func (bd *BotDetector) describePattern(
	regularity float64,
	ipDiversity float64,
	suspiciousUAs []string,
	avgSessionDur time.Duration,
	recordCount int,
) string {
	var parts []string

	if regularity > 0.8 {
		parts = append(parts, "highly regular request intervals")
	} else if regularity > 0.5 {
		parts = append(parts, "somewhat regular request intervals")
	}

	if len(suspiciousUAs) > 0 {
		parts = append(parts, "bot-like user-agent signatures detected")
	}

	if ipDiversity < 0.01 {
		parts = append(parts, "single IP address")
	} else if ipDiversity > 0.9 {
		parts = append(parts, "highly distributed IP addresses (possible botnet)")
	}

	if avgSessionDur > 0 && avgSessionDur < 1*time.Second {
		parts = append(parts, "extremely short session durations")
	}

	if len(parts) == 0 {
		return "Normal traffic pattern"
	}
	return strings.Join(parts, "; ")
}

// generateRecommendation produces an actionable recommendation.
func (bd *BotDetector) generateRecommendation(isBot bool, confidence float64, pattern string) string {
	if !isBot {
		return "Traffic appears human — no action needed"
	}

	var recs []string
	recs = append(recs, "Bot traffic detected with high confidence")

	if strings.Contains(pattern, "single IP") {
		recs = append(recs, "Consider IP-based rate limiting for the offending address")
	}
	if strings.Contains(pattern, "distributed") {
		recs = append(recs, "Consider CAPTCHA or challenge-based protection for botnet traffic")
	}
	if strings.Contains(pattern, "user-agent") {
		recs = append(recs, "Block or challenge requests with known bot user-agent strings")
	}
	if strings.Contains(pattern, "regular") {
		recs = append(recs, "Implement jitter-based detection to identify automated request patterns")
	}

	return strings.Join(recs, ". ")
}

// DetectBotPatternsFromTopology analyzes metrics and network events from the
// store to detect bot traffic patterns across the topology.
func (bd *BotDetector) DetectBotPatternsFromTopology(ctx context.Context, tenantID string) (*BotDetectionResult, error) {
	agents, err := bd.store.GetAllAgentStates(ctx, tenantID)
	if err != nil {
		return nil, fmt.Errorf("get agent states: %w", err)
	}

	end := time.Now()
	start := end.Add(-1 * time.Hour)

	var allRecords []RequestRecord
	for _, agent := range agents {
		metrics, err := bd.store.QueryMetrics(ctx, tenantID, agent.ID, "network.requests_per_sec", start, end)
		if err != nil {
			continue
		}
		for _, m := range metrics {
			allRecords = append(allRecords, RequestRecord{
				Timestamp: m.Timestamp,
				SourceIP:  agent.IPAddress,
				Status:    200,
			})
		}
	}

	return bd.DetectBotPatterns(ctx, allRecords)
}
