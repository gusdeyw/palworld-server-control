package main

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	networkStatusDisabled   = "disabled"
	networkStatusCollecting = "collecting"
	networkStatusHealthy    = "healthy"
	networkStatusDegraded   = "degraded"
	networkStatusCritical   = "critical"
)

type NetworkHealth struct {
	Enabled    bool                  `json:"enabled"`
	Status     string                `json:"status"`
	LatencyMS  float64               `json:"latencyMs"`
	PacketLoss float64               `json:"packetLoss"`
	Sent       int                   `json:"sent"`
	Received   int                   `json:"received"`
	WindowSize int                   `json:"windowSize"`
	UpdatedAt  *time.Time            `json:"updatedAt,omitempty"`
	Targets    []NetworkTargetHealth `json:"targets"`
}

type NetworkTargetHealth struct {
	Target     string  `json:"target"`
	LatencyMS  float64 `json:"latencyMs"`
	PacketLoss float64 `json:"packetLoss"`
	Sent       int     `json:"sent"`
	Received   int     `json:"received"`
}

type networkProbeSample struct {
	At        time.Time
	Target    string
	LatencyMS float64
	Success   bool
}

type networkProbeFunc func(context.Context, string, time.Duration) (time.Duration, error)

type NetworkMonitor struct {
	targets      []string
	interval     time.Duration
	timeout      time.Duration
	window       int
	degradedLoss float64
	criticalLoss float64
	mock         bool
	probe        networkProbeFunc

	mu      sync.RWMutex
	samples []networkProbeSample
}

func NewNetworkMonitor(
	targets []string,
	interval time.Duration,
	timeout time.Duration,
	window int,
	degradedLoss float64,
	criticalLoss float64,
	mock bool,
) *NetworkMonitor {
	normalized := normalizeNetworkTargets(targets)
	if interval < time.Second {
		interval = 5 * time.Second
	}
	if timeout < 100*time.Millisecond {
		timeout = 2 * time.Second
	}
	if window < 4 {
		window = 20
	}
	if degradedLoss <= 0 || degradedLoss >= 100 {
		degradedLoss = 5
	}
	if criticalLoss <= degradedLoss || criticalLoss > 100 {
		criticalLoss = 20
	}
	return &NetworkMonitor{
		targets:      normalized,
		interval:     interval,
		timeout:      timeout,
		window:       window,
		degradedLoss: degradedLoss,
		criticalLoss: criticalLoss,
		mock:         mock,
		probe:        probeDNSTarget,
		samples:      make([]networkProbeSample, 0, window*max(1, len(normalized))),
	}
}

func (m *NetworkMonitor) Run(ctx context.Context) {
	if m == nil || m.mock || len(m.targets) == 0 {
		return
	}
	m.probeCycle(ctx)
	timer := time.NewTimer(m.interval)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			m.probeCycle(ctx)
			timer.Reset(m.interval)
		}
	}
}

func (m *NetworkMonitor) probeCycle(parent context.Context) {
	type result struct {
		target  string
		elapsed time.Duration
		err     error
	}
	results := make(chan result, len(m.targets))
	for _, target := range m.targets {
		go func(target string) {
			ctx, cancel := context.WithTimeout(parent, m.timeout)
			defer cancel()
			elapsed, err := m.probe(ctx, target, m.timeout)
			results <- result{target: target, elapsed: elapsed, err: err}
		}(target)
	}
	for range m.targets {
		item := <-results
		m.record(networkProbeSample{
			At:        time.Now().UTC(),
			Target:    item.target,
			LatencyMS: float64(item.elapsed.Microseconds()) / 1000,
			Success:   item.err == nil,
		})
	}
}

func (m *NetworkMonitor) record(sample networkProbeSample) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.samples = append(m.samples, sample)
	limit := m.window * max(1, len(m.targets))
	if len(m.samples) > limit {
		m.samples = append([]networkProbeSample(nil), m.samples[len(m.samples)-limit:]...)
	}
}

func (m *NetworkMonitor) Health() NetworkHealth {
	if m == nil {
		return NetworkHealth{Status: networkStatusDisabled, Targets: []NetworkTargetHealth{}}
	}
	if m.mock {
		now := time.Now().UTC()
		return NetworkHealth{
			Enabled:    true,
			Status:     networkStatusHealthy,
			LatencyMS:  34.8,
			PacketLoss: 0,
			Sent:       40,
			Received:   40,
			WindowSize: 40,
			UpdatedAt:  &now,
			Targets: []NetworkTargetHealth{
				{Target: "1.1.1.1:53", LatencyMS: 33.9, PacketLoss: 0, Sent: 20, Received: 20},
				{Target: "8.8.8.8:53", LatencyMS: 35.7, PacketLoss: 0, Sent: 20, Received: 20},
			},
		}
	}
	if len(m.targets) == 0 {
		return NetworkHealth{Status: networkStatusDisabled, Targets: []NetworkTargetHealth{}}
	}

	m.mu.RLock()
	samples := append([]networkProbeSample(nil), m.samples...)
	m.mu.RUnlock()

	health := NetworkHealth{
		Enabled:    true,
		Status:     networkStatusCollecting,
		Sent:       len(samples),
		WindowSize: m.window * len(m.targets),
		Targets:    make([]NetworkTargetHealth, 0, len(m.targets)),
	}
	if len(samples) == 0 {
		for _, target := range m.targets {
			health.Targets = append(health.Targets, NetworkTargetHealth{Target: target})
		}
		return health
	}

	type targetAggregate struct {
		sent       int
		received   int
		latencySum float64
	}
	byTarget := make(map[string]*targetAggregate, len(m.targets))
	var latencySum float64
	for _, sample := range samples {
		aggregate := byTarget[sample.Target]
		if aggregate == nil {
			aggregate = &targetAggregate{}
			byTarget[sample.Target] = aggregate
		}
		aggregate.sent++
		if sample.Success {
			health.Received++
			aggregate.received++
			latencySum += sample.LatencyMS
			aggregate.latencySum += sample.LatencyMS
		}
		if health.UpdatedAt == nil || sample.At.After(*health.UpdatedAt) {
			updatedAt := sample.At
			health.UpdatedAt = &updatedAt
		}
	}
	health.PacketLoss = roundedPercent(health.Sent-health.Received, health.Sent)
	if health.Received > 0 {
		health.LatencyMS = roundOneDecimal(latencySum / float64(health.Received))
	}

	for _, target := range m.targets {
		aggregate := byTarget[target]
		targetHealth := NetworkTargetHealth{Target: target}
		if aggregate != nil {
			targetHealth.Sent = aggregate.sent
			targetHealth.Received = aggregate.received
			targetHealth.PacketLoss = roundedPercent(aggregate.sent-aggregate.received, aggregate.sent)
			if aggregate.received > 0 {
				targetHealth.LatencyMS = roundOneDecimal(aggregate.latencySum / float64(aggregate.received))
			}
		}
		health.Targets = append(health.Targets, targetHealth)
	}

	minimumSamples := min(health.WindowSize, 10)
	if health.Sent < minimumSamples {
		return health
	}
	switch {
	case health.PacketLoss >= m.criticalLoss:
		health.Status = networkStatusCritical
	case health.PacketLoss >= m.degradedLoss:
		health.Status = networkStatusDegraded
	default:
		health.Status = networkStatusHealthy
	}
	return health
}

func (m *NetworkMonitor) Issue() string {
	health := m.Health()
	switch health.Status {
	case networkStatusCritical:
		if health.Received == 0 {
			return fmt.Sprintf("Network: critical (%.1f%% packet loss, no probe replies)", health.PacketLoss)
		}
		return fmt.Sprintf("Network: critical (%.1f%% packet loss, %.1f ms)", health.PacketLoss, health.LatencyMS)
	case networkStatusDegraded:
		return fmt.Sprintf("Network: degraded (%.1f%% packet loss, %.1f ms)", health.PacketLoss, health.LatencyMS)
	default:
		return ""
	}
}

func normalizeNetworkTargets(targets []string) []string {
	seen := make(map[string]bool, len(targets))
	normalized := make([]string, 0, len(targets))
	for _, raw := range targets {
		target := strings.TrimSpace(raw)
		if target == "" {
			continue
		}
		if _, _, err := net.SplitHostPort(target); err != nil {
			target = net.JoinHostPort(target, "53")
		}
		if seen[target] {
			continue
		}
		seen[target] = true
		normalized = append(normalized, target)
	}
	return normalized
}

var dnsQueryID atomic.Uint32

func probeDNSTarget(ctx context.Context, target string, timeout time.Duration) (time.Duration, error) {
	dialer := net.Dialer{}
	started := time.Now()
	connection, err := dialer.DialContext(ctx, "udp", target)
	if err != nil {
		return 0, err
	}
	defer connection.Close()
	if err := connection.SetDeadline(time.Now().Add(timeout)); err != nil {
		return 0, err
	}

	queryID := uint16(dnsQueryID.Add(1))
	query := dnsProbeQuery(queryID)
	if _, err := connection.Write(query); err != nil {
		return 0, err
	}
	response := make([]byte, 512)
	count, err := connection.Read(response)
	elapsed := time.Since(started)
	if err != nil {
		return 0, err
	}
	if count < 12 {
		return 0, errors.New("short DNS probe response")
	}
	if binary.BigEndian.Uint16(response[:2]) != queryID {
		return 0, errors.New("DNS probe response ID mismatch")
	}
	if response[2]&0x80 == 0 {
		return 0, errors.New("DNS probe response flag is missing")
	}
	return elapsed, nil
}

func dnsProbeQuery(queryID uint16) []byte {
	query := make([]byte, 0, 29)
	header := make([]byte, 12)
	binary.BigEndian.PutUint16(header[0:2], queryID)
	binary.BigEndian.PutUint16(header[2:4], 0x0100)
	binary.BigEndian.PutUint16(header[4:6], 1)
	query = append(query, header...)
	query = append(query, 7)
	query = append(query, "example"...)
	query = append(query, 3)
	query = append(query, "com"...)
	query = append(query, 0)
	query = append(query, 0, 1)
	query = append(query, 0, 1)
	return query
}

func roundedPercent(failed, total int) float64 {
	if total <= 0 {
		return 0
	}
	return roundOneDecimal(float64(failed) / float64(total) * 100)
}

func roundOneDecimal(value float64) float64 {
	return math.Round(value*10) / 10
}
