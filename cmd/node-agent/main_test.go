package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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

func TestBuildMultiExitXrayConfigMap_BindsListenAndSendThroughPerNode(t *testing.T) {
	cfg := buildMultiExitXrayConfigMap([]MultiExitNodeConfig{
		{
			NodeID:            1,
			IP:                "154.219.106.105",
			Port:              443,
			InboundTag:        "node_1_in",
			OutboundTag:       "node_1_out",
			XrayUserKeyPrefix: "node_1__",
		},
		{
			NodeID:            2,
			IP:                "154.219.106.106",
			Port:              443,
			InboundTag:        "node_2_in",
			OutboundTag:       "node_2_out",
			XrayUserKeyPrefix: "node_2__",
		},
	}, multiExitReality{
		ServerName: "www.microsoft.com",
		PublicKey:  "pub",
		PrivateKey: "priv",
		ShortID:    "",
	}, map[string][]interface{}{
		"node_1_in": {
			map[string]interface{}{"id": "uuid", "email": "node_1__user@test"},
		},
	}, "127.0.0.1:10085")

	inbounds := cfg["inbounds"].([]interface{})
	if findTaggedObject(inbounds, "node_1_in") < 0 || findTaggedObject(inbounds, "node_2_in") < 0 {
		t.Fatalf("multi node inbounds missing: %#v", inbounds)
	}
	outbounds := cfg["outbounds"].([]interface{})
	node1Out := outbounds[1].(map[string]interface{})
	if node1Out["tag"] != "node_1_out" || node1Out["sendThrough"] != "154.219.106.105" {
		t.Fatalf("node 1 outbound = %#v, want sendThrough 154.219.106.105", node1Out)
	}
	node2Out := outbounds[2].(map[string]interface{})
	if node2Out["tag"] != "node_2_out" || node2Out["sendThrough"] != "154.219.106.106" {
		t.Fatalf("node 2 outbound = %#v, want sendThrough 154.219.106.106", node2Out)
	}
}

func TestLoadMultiNodeConfig_AllowsSameIPWithDifferentPorts(t *testing.T) {
	t.Setenv("MULTI_NODE_CONFIG", `[
		{"node_id": 1, "ip": "154.219.106.105", "port": 443, "transport": "tcp"},
		{"node_id": 2, "ip": "154.219.106.105", "port": 8443, "transport": "xhttp", "xhttp_path": "raypilot"}
	]`)
	t.Setenv("MULTI_NODE_CONFIG_PATH", "")

	nodes, err := loadMultiNodeConfig()
	if err != nil {
		t.Fatalf("loadMultiNodeConfig returned error: %v", err)
	}
	if len(nodes) != 2 {
		t.Fatalf("len(nodes) = %d, want 2", len(nodes))
	}
	if nodes[0].Transport != "tcp" || nodes[0].Port != 443 {
		t.Fatalf("tcp node = %+v", nodes[0])
	}
	if nodes[1].Transport != "xhttp" || nodes[1].Port != 8443 || nodes[1].XHTTPPath != "/raypilot" {
		t.Fatalf("xhttp node = %+v", nodes[1])
	}
}

func TestBuildMultiExitXrayConfigMap_XHTTPStreamSettings(t *testing.T) {
	cfg := buildMultiExitXrayConfigMap([]MultiExitNodeConfig{
		{
			NodeID:      3,
			IP:          "154.219.106.105",
			Port:        443,
			Transport:   "xhttp",
			XHTTPPath:   "raypilot-xhttp",
			XHTTPHost:   "cdn.example.com",
			XHTTPMode:   "stream-up",
			InboundTag:  "node_3_in",
			OutboundTag: "node_3_out",
		},
	}, multiExitReality{
		ServerName: "www.microsoft.com",
		PublicKey:  "pub",
		PrivateKey: "priv",
		ShortID:    "sid",
	}, nil, "127.0.0.1:10085")

	inbounds := cfg["inbounds"].([]interface{})
	idx := findTaggedObject(inbounds, "node_3_in")
	if idx < 0 {
		t.Fatalf("xhttp inbound missing: %#v", inbounds)
	}
	inbound := inbounds[idx].(map[string]interface{})
	stream := inbound["streamSettings"].(map[string]interface{})
	if stream["network"] != "xhttp" {
		t.Fatalf("network = %#v, want xhttp", stream["network"])
	}
	xhttpOpts := stream["xhttpSettings"].(map[string]interface{})
	if xhttpOpts["path"] != "/raypilot-xhttp" || xhttpOpts["mode"] != "stream-up" || xhttpOpts["host"] != "cdn.example.com" {
		t.Fatalf("xhttpSettings = %#v", xhttpOpts)
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

func TestTrafficQueueFlush_MultiExitReportsNodeIDToMultiEndpoint(t *testing.T) {
	var received []MultiTrafficReportReq
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/agent/multi/traffic" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		var req MultiTrafficReportReq
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		received = append(received, req)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()

	queuePath := filepath.Join(t.TempDir(), "multi_traffic_queue.json")
	agent := NewAgent(&Config{
		CenterServerURL:   server.URL,
		AgentRole:         "multi_exit",
		NodeHostID:        9,
		NodeHostToken:     "host-token",
		XrayConfigPath:    filepath.Join(t.TempDir(), "config.json"),
		TrafficQueuePath:  queuePath,
		TrafficQueueLimit: 10,
	})

	collectedAt := time.Now().UTC().Truncate(time.Second)
	agent.enqueueTrafficReport(TrafficReportBatch{
		NodeID:      7,
		CollectedAt: collectedAt,
		Items:       []TrafficItem{{XrayUserKey: "user@test", UplinkTotal: 3}},
	})
	agent.flushTrafficQueue(contextWithTimeout(t, time.Second))

	if len(received) != 1 {
		t.Fatalf("received %d requests, want 1", len(received))
	}
	req := received[0]
	if req.NodeHostID != 9 || req.Token != "host-token" {
		t.Fatalf("request auth = host %d token %q, want host 9 token host-token", req.NodeHostID, req.Token)
	}
	if len(req.Reports) != 1 || req.Reports[0].NodeID != 7 {
		t.Fatalf("reports = %+v, want node_id=7", req.Reports)
	}
	if req.Reports[0].CollectedAt != collectedAt {
		t.Fatalf("collected_at = %s, want %s", req.Reports[0].CollectedAt, collectedAt)
	}
	if len(agent.trafficQueue) != 0 {
		t.Fatalf("queue len = %d, want 0", len(agent.trafficQueue))
	}
}

func TestTrafficQueueFlush_ReplaysOldestFirstAndKeepsFailedBatch(t *testing.T) {
	var received []TrafficReportReq
	failFirst := true
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/agent/traffic" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		var req TrafficReportReq
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		received = append(received, req)
		if failFirst {
			failFirst = false
			http.Error(w, "temporary failure", http.StatusBadGateway)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()

	queuePath := filepath.Join(t.TempDir(), "traffic_queue.json")
	agent := NewAgent(&Config{
		CenterServerURL:   server.URL,
		NodeID:            7,
		NodeToken:         "node-token",
		XrayConfigPath:    filepath.Join(t.TempDir(), "config.json"),
		TrafficQueuePath:  queuePath,
		TrafficQueueLimit: 10,
	})

	firstAt := time.Now().Add(-2 * time.Minute).UTC().Truncate(time.Second)
	secondAt := firstAt.Add(time.Minute)
	agent.enqueueTrafficReport(TrafficReportBatch{CollectedAt: firstAt, Items: []TrafficItem{{XrayUserKey: "a@test", UplinkTotal: 1}}})
	agent.enqueueTrafficReport(TrafficReportBatch{CollectedAt: secondAt, Items: []TrafficItem{{XrayUserKey: "a@test", UplinkTotal: 2}}})

	agent.flushTrafficQueue(contextWithTimeout(t, time.Second))
	if len(received) != 1 {
		t.Fatalf("received %d requests, want first failed request only", len(received))
	}
	if received[0].CollectedAt != firstAt {
		t.Fatalf("first collected_at = %s, want %s", received[0].CollectedAt, firstAt)
	}
	if len(agent.trafficQueue) != 2 {
		t.Fatalf("queue len after failed flush = %d, want 2", len(agent.trafficQueue))
	}

	agent.flushTrafficQueue(contextWithTimeout(t, time.Second))
	if len(received) != 3 {
		t.Fatalf("received %d requests, want retry first plus second", len(received))
	}
	if received[1].CollectedAt != firstAt || received[2].CollectedAt != secondAt {
		t.Fatalf("replay order = %s then %s, want %s then %s", received[1].CollectedAt, received[2].CollectedAt, firstAt, secondAt)
	}
	if len(agent.trafficQueue) != 0 {
		t.Fatalf("queue len after successful flush = %d, want 0", len(agent.trafficQueue))
	}
	if _, err := os.Stat(queuePath); !os.IsNotExist(err) {
		t.Fatalf("queue file should be removed, stat err=%v", err)
	}
}

func contextWithTimeout(t *testing.T, timeout time.Duration) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	t.Cleanup(cancel)
	return ctx
}
