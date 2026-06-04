package processing

import (
	"fmt"
	"regexp"
	"strings"
	"sync"

	"go.uber.org/zap"
)

// ServiceMappingRule defines a single process-to-service mapping rule.
// Match is compared as a prefix against process names unless IsRegex is true,
// in which case it is compiled as a Go regular expression.
type ServiceMappingRule struct {
	// Match is the pattern to match against process names.
	Match string `yaml:"match"`

	// ServiceName is the service name to resolve to on match.
	ServiceName string `yaml:"service_name"`

	// IsRegex indicates whether Match should be interpreted as a regular expression.
	IsRegex bool `yaml:"is_regex"`
}

// DefaultServiceMappingRules provides the built-in process-to-service mappings
// used when no custom rules are supplied.
var DefaultServiceMappingRules = []ServiceMappingRule{
	{Match: "nginx", ServiceName: "nginx"},
	{Match: "postgres", ServiceName: "postgresql"},
	{Match: "redis-server", ServiceName: "redis"},
	{Match: "mongod", ServiceName: "mongodb"},
	{Match: "node", ServiceName: "nodejs"},
	{Match: "python3", ServiceName: "python"},
	{Match: "java", ServiceName: "java"},
	{Match: "etcd", ServiceName: "etcd"},
	{Match: "kube-apiserver", ServiceName: "kubernetes-api"},
	{Match: "kubelet", ServiceName: "kubelet"},
}

// compiledRule holds a pre-compiled regex rule together with its target service name.
type compiledRule struct {
	re          *regexp.Regexp
	serviceName string
}

// ServiceMap is a thread-safe process-to-service name resolver.
// It supports four resolution strategies evaluated in order:
//  1. Prefix-match custom overrides (longest prefix wins)
//  2. Prefix-match against rules
//  3. Regex-match against compiled patterns
//  4. Fallback: return the input process name unchanged
type ServiceMap struct {
	rules    []ServiceMappingRule
	custom   map[string]string
	compiled []compiledRule
	mu       sync.RWMutex
	logger   *zap.Logger
}

// NewServiceMap creates a ServiceMap from the provided rules.
// If rules is nil the DefaultServiceMappingRules are loaded.
// All rules with IsRegex == true are pre-compiled; an error is returned
// if any pattern is invalid.
func NewServiceMap(rules []ServiceMappingRule, logger *zap.Logger) (*ServiceMap, error) {
	if logger == nil {
		return nil, fmt.Errorf("logger must not be nil")
	}

	if rules == nil {
		rules = DefaultServiceMappingRules
	}

	sm := &ServiceMap{
		rules:  rules,
		custom: make(map[string]string),
		logger: logger,
	}

	for _, r := range rules {
		if !r.IsRegex {
			continue
		}
		re, err := regexp.Compile(r.Match)
		if err != nil {
			return nil, fmt.Errorf("invalid regex pattern %q: %w", r.Match, err)
		}
		sm.compiled = append(sm.compiled, compiledRule{
			re:          re,
			serviceName: r.ServiceName,
		})
	}

	sm.logger.Info("ServiceMap initialized",
		zap.Int("total_rules", len(rules)),
		zap.Int("regex_rules", len(sm.compiled)),
	)

	return sm, nil
}

// Resolve maps a process name to a service name using the four-level
// resolution strategy. An empty process name returns an empty string.
//
// Resolution order:
//  1. Longest prefix-match in custom overrides
//  2. Prefix-match against rules (first match wins)
//  3. Regex-match against compiled patterns (first match wins)
//  4. Fallback: return the process name as-is
func (sm *ServiceMap) Resolve(processName string) string {
	if processName == "" {
		return ""
	}

	sm.mu.RLock()
	defer sm.mu.RUnlock()

	// Level 1: prefix-match custom overrides (longest prefix wins).
	bestCustomLen := 0
	bestCustomSvc := ""
	for prefix, svc := range sm.custom {
		if strings.HasPrefix(processName, prefix) && len(prefix) > bestCustomLen {
			bestCustomLen = len(prefix)
			bestCustomSvc = svc
		}
	}
	if bestCustomLen > 0 {
		return bestCustomSvc
	}

	// Level 2: prefix-match against rules (first match wins).
	for _, r := range sm.rules {
		if !r.IsRegex && strings.HasPrefix(processName, r.Match) {
			return r.ServiceName
		}
	}

	// Level 3: regex-match against compiled patterns (first match wins).
	for _, cr := range sm.compiled {
		if cr.re.MatchString(processName) {
			return cr.serviceName
		}
	}

	// Level 4: fallback — return the process name as-is.
	return processName
}

// AddCustom adds or overwrites a custom prefix-match mapping.
// Custom mappings take precedence over all rule-based resolution.
func (sm *ServiceMap) AddCustom(processName, serviceName string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	sm.custom[processName] = serviceName

	sm.logger.Debug("Custom mapping added",
		zap.String("process", processName),
		zap.String("service", serviceName),
	)
}

// RemoveCustom removes a custom prefix-match mapping.
// If no mapping existed for the given process name this is a no-op.
func (sm *ServiceMap) RemoveCustom(processName string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	delete(sm.custom, processName)

	sm.logger.Debug("Custom mapping removed",
		zap.String("process", processName),
	)
}
