package main

import (
	"context"
	"net"
	"strings"
	"testing"
	"time"
)

func TestNetworkMonitorClassifiesPacketLoss(t *testing.T) {
	tests := []struct {
		name     string
		failures int
		status   string
		loss     float64
	}{
		{name: "healthy", failures: 0, status: networkStatusHealthy, loss: 0},
		{name: "degraded at five percent", failures: 1, status: networkStatusDegraded, loss: 5},
		{name: "critical at twenty percent", failures: 4, status: networkStatusCritical, loss: 20},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			monitor := NewNetworkMonitor(
				[]string{"1.1.1.1:53"},
				5*time.Second,
				2*time.Second,
				20,
				5,
				20,
				false,
			)
			for index := 0; index < 20; index++ {
				monitor.record(networkProbeSample{
					At:        time.Now().UTC(),
					Target:    "1.1.1.1:53",
					LatencyMS: 35,
					Success:   index >= test.failures,
				})
			}
			health := monitor.Health()
			if health.Status != test.status {
				t.Fatalf("expected status %q, got %q", test.status, health.Status)
			}
			if health.PacketLoss != test.loss {
				t.Fatalf("expected loss %.1f, got %.1f", test.loss, health.PacketLoss)
			}
			if health.LatencyMS != 35 {
				t.Fatalf("expected 35 ms latency, got %.1f", health.LatencyMS)
			}
		})
	}
}

func TestNetworkMonitorCollectsBeforeWarning(t *testing.T) {
	monitor := NewNetworkMonitor(
		[]string{"1.1.1.1:53"},
		5*time.Second,
		2*time.Second,
		20,
		5,
		20,
		false,
	)
	for index := 0; index < 3; index++ {
		monitor.record(networkProbeSample{
			At:      time.Now().UTC(),
			Target:  "1.1.1.1:53",
			Success: false,
		})
	}
	if health := monitor.Health(); health.Status != networkStatusCollecting {
		t.Fatalf("expected collecting status, got %q", health.Status)
	}
	if issue := monitor.Issue(); issue != "" {
		t.Fatalf("expected no issue during collection, got %q", issue)
	}
}

func TestNetworkMonitorUsesRollingWindow(t *testing.T) {
	monitor := NewNetworkMonitor(
		[]string{"1.1.1.1:53"},
		5*time.Second,
		2*time.Second,
		4,
		5,
		20,
		false,
	)
	monitor.record(networkProbeSample{At: time.Now().UTC(), Target: "1.1.1.1:53", Success: false})
	for index := 0; index < 4; index++ {
		monitor.record(networkProbeSample{
			At:        time.Now().UTC(),
			Target:    "1.1.1.1:53",
			LatencyMS: 28,
			Success:   true,
		})
	}
	health := monitor.Health()
	if health.Sent != 4 || health.PacketLoss != 0 || health.Status != networkStatusHealthy {
		t.Fatalf("unexpected rolling health: %#v", health)
	}
}

func TestNetworkMonitorIssueDescribesCriticalLoss(t *testing.T) {
	monitor := NewNetworkMonitor(
		[]string{"1.1.1.1:53"},
		5*time.Second,
		2*time.Second,
		4,
		5,
		20,
		false,
	)
	for index := 0; index < 4; index++ {
		monitor.record(networkProbeSample{
			At:      time.Now().UTC(),
			Target:  "1.1.1.1:53",
			Success: false,
		})
	}
	issue := monitor.Issue()
	if !strings.Contains(issue, "critical") || !strings.Contains(issue, "100.0% packet loss") {
		t.Fatalf("unexpected issue %q", issue)
	}
}

func TestProbeDNSTargetAcceptsMatchingResponse(t *testing.T) {
	server, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()

	serverDone := make(chan error, 1)
	go func() {
		buffer := make([]byte, 512)
		count, address, err := server.ReadFrom(buffer)
		if err != nil {
			serverDone <- err
			return
		}
		response := append([]byte(nil), buffer[:count]...)
		response[2] |= 0x80
		_, err = server.WriteTo(response, address)
		serverDone <- err
	}()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	elapsed, err := probeDNSTarget(ctx, server.LocalAddr().String(), time.Second)
	if err != nil {
		t.Fatalf("probe failed: %v", err)
	}
	if elapsed <= 0 {
		t.Fatalf("expected positive elapsed time, got %s", elapsed)
	}
	if err := <-serverDone; err != nil {
		t.Fatalf("DNS test server failed: %v", err)
	}
}

func TestNormalizeNetworkTargetsAddsDNSPortAndDeduplicates(t *testing.T) {
	targets := normalizeNetworkTargets([]string{"1.1.1.1", "1.1.1.1:53", " 8.8.8.8 "})
	if len(targets) != 2 {
		t.Fatalf("expected two targets, got %#v", targets)
	}
	if targets[0] != "1.1.1.1:53" || targets[1] != "8.8.8.8:53" {
		t.Fatalf("unexpected targets %#v", targets)
	}
}
