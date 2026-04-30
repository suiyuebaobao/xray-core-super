package main

import (
	"strings"
	"testing"
)

func TestBuildHAProxyConfig_IncludesStatsSocketAndRelayBackend(t *testing.T) {
	config, enabledCount, err := buildHAProxyConfig([]RelayBackendPayload{
		{
			ID:         7,
			ExitNodeID: 1,
			ListenPort: 24443,
			TargetHost: "154.219.97.219",
			TargetPort: 443,
			IsEnabled:  true,
		},
	}, "/tmp/test-haproxy.sock")
	if err != nil {
		t.Fatalf("buildHAProxyConfig returned error: %v", err)
	}
	if enabledCount != 1 {
		t.Fatalf("enabledCount = %d, want 1", enabledCount)
	}

	for _, want := range []string{
		"stats socket /tmp/test-haproxy.sock mode 600 level admin",
		"frontend relay_7_24443",
		"bind *:24443",
		"server exit_1 154.219.97.219:443 check",
	} {
		if !strings.Contains(config, want) {
			t.Fatalf("config missing %q:\n%s", want, config)
		}
	}
}

func TestParseHAProxyStats_ExtractsRelayFrontendTraffic(t *testing.T) {
	stats := []byte(`# pxname,svname,qcur,qmax,bin,bout
relay_7_24443,FRONTEND,0,0,1234,5678
backend_7,BACKEND,0,0,9999,8888
web,FRONTEND,0,0,1,2
`)

	items, err := parseHAProxyStats(stats)
	if err != nil {
		t.Fatalf("parseHAProxyStats returned error: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("len(items) = %d, want 1", len(items))
	}
	item := items[0]
	if item.RelayBackendID == nil || *item.RelayBackendID != 7 {
		t.Fatalf("RelayBackendID = %v, want 7", item.RelayBackendID)
	}
	if item.ListenPort != 24443 || item.BytesInTotal != 1234 || item.BytesOutTotal != 5678 {
		t.Fatalf("item = %+v, want listen_port=24443 bytes=1234/5678", item)
	}
}
