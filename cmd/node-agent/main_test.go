package main

import (
	"encoding/json"
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

func TestEnsureXrayStatsConfigMap_AddsStatsAPIAndIsIdempotent(t *testing.T) {
	cfg := map[string]interface{}{
		"routing": map[string]interface{}{
			"domainStrategy": "AsIs",
			"rules": []interface{}{
				map[string]interface{}{
					"type":        "field",
					"outboundTag": "blocked",
					"protocol":    []interface{}{"bittorrent"},
				},
			},
		},
		"inbounds": []interface{}{
			map[string]interface{}{
				"protocol": "vless",
				"port":     float64(443),
				"settings": map[string]interface{}{"clients": []interface{}{}, "decryption": "none"},
			},
		},
		"outbounds": []interface{}{
			map[string]interface{}{"protocol": "freedom", "tag": "direct"},
		},
	}

	changed, err := ensureXrayStatsConfigMap(cfg, "127.0.0.1:10085")
	if err != nil {
		t.Fatalf("ensureXrayStatsConfigMap returned error: %v", err)
	}
	if !changed {
		t.Fatal("changed = false, want true")
	}

	apiCfg, ok := cfg["api"].(map[string]interface{})
	if !ok || apiCfg["tag"] != "api" {
		t.Fatalf("api config = %#v, want api tag", cfg["api"])
	}
	if _, ok := cfg["stats"].(map[string]interface{}); !ok {
		t.Fatalf("stats config = %#v, want object", cfg["stats"])
	}

	inbounds := cfg["inbounds"].([]interface{})
	if findTaggedObject(inbounds, "api") < 0 {
		t.Fatalf("api inbound missing: %#v", inbounds)
	}
	rules := cfg["routing"].(map[string]interface{})["rules"].([]interface{})
	if !hasXrayAPIRoutingRule(rules) {
		t.Fatalf("api routing rule missing: %#v", rules)
	}
	outbounds := cfg["outbounds"].([]interface{})
	if findTaggedObject(outbounds, "blocked") < 0 {
		t.Fatalf("blocked outbound missing: %#v", outbounds)
	}

	encoded, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}
	var roundTripped map[string]interface{}
	if err := json.Unmarshal(encoded, &roundTripped); err != nil {
		t.Fatalf("unmarshal config: %v", err)
	}
	changed, err = ensureXrayStatsConfigMap(roundTripped, "127.0.0.1:10085")
	if err != nil {
		t.Fatalf("second ensure returned error: %v", err)
	}
	if changed {
		t.Fatal("second ensure changed config, want idempotent false")
	}
}

func TestXrayStatValueUnmarshal_AcceptsNumberAndString(t *testing.T) {
	var numberValue xrayStatValue
	if err := json.Unmarshal([]byte(`123`), &numberValue); err != nil {
		t.Fatalf("unmarshal number: %v", err)
	}
	if numberValue != 123 {
		t.Fatalf("numberValue = %d, want 123", numberValue)
	}

	var stringValue xrayStatValue
	if err := json.Unmarshal([]byte(`"456"`), &stringValue); err != nil {
		t.Fatalf("unmarshal string: %v", err)
	}
	if stringValue != 456 {
		t.Fatalf("stringValue = %d, want 456", stringValue)
	}
}
