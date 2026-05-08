package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

func TestParseCenterServerURLs_DedupesAndNormalizes(t *testing.T) {
	urls := parseCenterServerURLs(" https://api-a.example.com/ ,http://1.2.3.4:8080/base/ ", "https://api-a.example.com")

	want := []string{"https://api-a.example.com", "http://1.2.3.4:8080/base"}
	if strings.Join(urls, ",") != strings.Join(want, ",") {
		t.Fatalf("urls = %#v, want %#v", urls, want)
	}
}

func TestParseCenterServerURLs_AddsKnownFallback(t *testing.T) {
	urls := parseCenterServerURLs("", "http://154.219.106.105")

	want := []string{"http://154.219.106.105", "http://leiyunai.fun", "http://154.219.106.53"}
	if strings.Join(urls, ",") != strings.Join(want, ",") {
		t.Fatalf("urls = %#v, want %#v", urls, want)
	}
}

func TestParseCenterServerURLs_AddsKnownIPsForDomain(t *testing.T) {
	urls := parseCenterServerURLs("", "http://leiyunai.fun")

	want := []string{"http://leiyunai.fun", "http://154.219.106.105", "http://154.219.106.53"}
	if strings.Join(urls, ",") != strings.Join(want, ",") {
		t.Fatalf("urls = %#v, want %#v", urls, want)
	}
}

func TestPostJSON_FallsBackToNextCenterAndRemembersSuccess(t *testing.T) {
	var requestedHosts []string
	agent := NewAgent(&Config{
		CenterServerURLs: []string{"http://center-a.test", "http://center-b.test"},
	})
	agent.httpClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		requestedHosts = append(requestedHosts, r.URL.Host)
		if r.URL.Host == "center-a.test" {
			return nil, errors.New("dial failed")
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(`{"success":true}`)),
			Header:     make(http.Header),
		}, nil
	})}

	var resp map[string]interface{}
	if err := agent.postJSON(contextWithTimeout(t, time.Second), "/api/agent/heartbeat", map[string]string{"ok": "1"}, &resp); err != nil {
		t.Fatalf("postJSON returned error: %v", err)
	}

	if strings.Join(requestedHosts, ",") != "center-a.test,center-b.test" {
		t.Fatalf("requested hosts = %#v", requestedHosts)
	}
	if got := agent.activeCenterServerURL(); got != "http://center-b.test" {
		t.Fatalf("active center = %s, want center-b", got)
	}
}

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

func TestLoadMultiNodeConfig_NormalizesOutboundIP(t *testing.T) {
	t.Setenv("MULTI_NODE_CONFIG", `[
		{"node_id": 1, "ip": "0.0.0.0", "port": 443, "outbound_type": "socks5", "outbound_ip": " 203.0.113.88 ", "outbound_proxy_url": "socks5://u:p@example.com:3010"},
		{"node_id": 2, "ip": "0.0.0.0", "port": 8443, "outbound_ip": "not-an-ip"}
	]`)
	t.Setenv("MULTI_NODE_CONFIG_PATH", "")

	nodes, err := loadMultiNodeConfig()
	if err != nil {
		t.Fatalf("loadMultiNodeConfig returned error: %v", err)
	}
	if nodes[0].OutboundIP != "203.0.113.88" {
		t.Fatalf("nodes[0].OutboundIP = %q, want 203.0.113.88", nodes[0].OutboundIP)
	}
	if nodes[1].OutboundIP != "" {
		t.Fatalf("nodes[1].OutboundIP = %q, want empty", nodes[1].OutboundIP)
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

func TestBuildMultiExitXrayConfigMap_Socks5Outbound(t *testing.T) {
	cfg := buildMultiExitXrayConfigMap([]MultiExitNodeConfig{
		{
			NodeID:            150,
			IP:                "156.238.231.16",
			Port:              24465,
			Transport:         "tcp",
			OutboundType:      "socks5",
			OutboundIP:        "203.0.113.88",
			OutboundProxyURL:  "socks5://user:pass@us.arxlabs.io:3010",
			InboundTag:        "node_150_in",
			OutboundTag:       "node_150_out",
			XrayUserKeyPrefix: "node_150__",
		},
	}, multiExitReality{
		ServerName: "www.microsoft.com",
		PublicKey:  "pub",
		PrivateKey: "priv",
		ShortID:    "",
	}, nil, "127.0.0.1:10085")

	outbounds := cfg["outbounds"].([]interface{})
	var target map[string]interface{}
	for _, outbound := range outbounds {
		item := outbound.(map[string]interface{})
		if item["tag"] == "node_150_out" {
			target = item
			break
		}
	}
	if target == nil {
		t.Fatalf("node_150_out outbound missing: %#v", outbounds)
	}
	if target["protocol"] != "socks" {
		t.Fatalf("protocol = %#v, want socks", target["protocol"])
	}
	if target["sendThrough"] != "203.0.113.88" {
		t.Fatalf("sendThrough = %#v, want 203.0.113.88", target["sendThrough"])
	}
	settings := target["settings"].(map[string]interface{})
	servers := settings["servers"].([]interface{})
	server := servers[0].(map[string]interface{})
	if server["address"] != "us.arxlabs.io" {
		t.Fatalf("address = %#v, want us.arxlabs.io", server["address"])
	}
	if server["port"] != 3010 {
		t.Fatalf("port = %#v, want 3010", server["port"])
	}
	users := server["users"].([]interface{})
	user := users[0].(map[string]interface{})
	if user["user"] != "user" || user["pass"] != "pass" {
		t.Fatalf("users = %#v, want user/pass", users)
	}
}

func TestBuildNodeOutbound_Socks5RequiresProxyURL(t *testing.T) {
	_, err := buildNodeOutbound(MultiExitNodeConfig{
		NodeID:       150,
		OutboundTag:  "node_150_out",
		OutboundType: "socks5",
	})
	if err == nil {
		t.Fatal("expected error for empty outbound_proxy_url")
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
	httpClient := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/api/agent/multi/traffic" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		var req MultiTrafficReportReq
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		received = append(received, req)
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(`{}`)),
			Header:     make(http.Header),
		}, nil
	})}

	queuePath := filepath.Join(t.TempDir(), "multi_traffic_queue.json")
	agent := NewAgent(&Config{
		CenterServerURL:   "http://center.test",
		AgentRole:         "multi_exit",
		NodeHostID:        9,
		NodeHostToken:     "host-token",
		XrayConfigPath:    filepath.Join(t.TempDir(), "config.json"),
		TrafficQueuePath:  queuePath,
		TrafficQueueLimit: 10,
	})
	agent.httpClient = httpClient

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
	httpClient := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
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
			return &http.Response{
				StatusCode: http.StatusBadGateway,
				Body:       io.NopCloser(strings.NewReader(`temporary failure`)),
				Header:     make(http.Header),
			}, nil
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(`{}`)),
			Header:     make(http.Header),
		}, nil
	})}

	queuePath := filepath.Join(t.TempDir(), "traffic_queue.json")
	agent := NewAgent(&Config{
		CenterServerURL:   "http://center.test",
		NodeID:            7,
		NodeToken:         "node-token",
		XrayConfigPath:    filepath.Join(t.TempDir(), "config.json"),
		TrafficQueuePath:  queuePath,
		TrafficQueueLimit: 10,
	})
	agent.httpClient = httpClient

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
