package service

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestExtractRealityConfig_WithPublicKey(t *testing.T) {
	raw := `{
	  "inbounds": [{
	    "protocol": "vless",
	    "streamSettings": {
	      "security": "reality",
	      "realitySettings": {
	        "serverNames": ["www.microsoft.com"],
	        "publicKey": "pub-key",
	        "privateKey": "private-key",
	        "shortIds": [""]
	      }
	    }
	  }]
	}`

	cfg, err := extractRealityConfig(raw)

	require.NoError(t, err)
	require.Equal(t, "www.microsoft.com", cfg.ServerName)
	require.Equal(t, "pub-key", cfg.PublicKey)
	require.Equal(t, "private-key", cfg.PrivateKey)
	require.Equal(t, "", cfg.ShortID)
}

func TestExtractRealityConfig_MissingRealityInbound(t *testing.T) {
	_, err := extractRealityConfig(`{"inbounds":[{"protocol":"http"}]}`)

	require.Error(t, err)
	require.Contains(t, err.Error(), "vless reality inbound not found")
}

func TestNormalizeDeployTransportOptions_MultipleTransports(t *testing.T) {
	req := &DeployRequest{
		Transport: "tcp",
		Transports: []string{
			"xhttp",
			"tcp",
		},
		TCPPort:   24430,
		XHTTPPort: 24432,
		XHTTPPath: "raypilot",
		XHTTPHost: "cdn.example.test",
		XHTTPMode: "stream-up",
	}

	options, err := normalizeDeployTransportOptions(req)

	require.NoError(t, err)
	require.Len(t, options, 2)
	require.Equal(t, "tcp", options[0].Transport)
	require.EqualValues(t, 24430, options[0].Port)
	require.Equal(t, "xtls-rprx-vision", options[0].Flow)
	require.Equal(t, "xhttp", options[1].Transport)
	require.EqualValues(t, 24432, options[1].Port)
	require.Equal(t, "/raypilot", options[1].XHTTPPath)
	require.Equal(t, "cdn.example.test", options[1].XHTTPHost)
	require.Equal(t, "stream-up", options[1].XHTTPMode)
	require.Empty(t, options[1].Flow)
	require.Equal(t, []string{"tcp", "xhttp"}, req.Transports)
}

func TestNormalizeDeployTransportOptions_RejectsDuplicatePorts(t *testing.T) {
	req := &DeployRequest{
		Transports: []string{"tcp", "xhttp"},
		TCPPort:    443,
		XHTTPPort:  443,
	}

	_, err := normalizeDeployTransportOptions(req)

	require.Error(t, err)
	require.Contains(t, err.Error(), "不能使用同一个端口")
}

func TestListenEndpointKey_AllowsSamePortOnDifferentIPs(t *testing.T) {
	endpoints := map[string]struct{}{}
	for _, ip := range []string{"198.51.100.10", "198.51.100.20"} {
		endpoints[listenEndpointKey(ip, 443)] = struct{}{}
	}

	require.Len(t, endpoints, 2)
	require.Contains(t, endpoints, "198.51.100.10:443")
	require.Contains(t, endpoints, "198.51.100.20:443")
}

func TestNormalizeDeployUint64IDs_DedupesAndSorts(t *testing.T) {
	ids, err := normalizeDeployUint64IDs([]uint64{3, 1, 3, 2})

	require.NoError(t, err)
	require.Equal(t, []uint64{1, 2, 3}, ids)
}

func TestNormalizeDeployUint64IDs_RejectsZero(t *testing.T) {
	_, err := normalizeDeployUint64IDs([]uint64{1, 0})

	require.Error(t, err)
}

func TestNormalizeCenterURLList_DedupesPrimaryAndFallbacks(t *testing.T) {
	values := normalizeCenterURLList("https://api.example.com/", []string{
		"https://api.example.com",
		"http://1.2.3.4:80",
		"https://backup.example.net, http://1.2.3.4:80",
		"ftp://ignored.example.test",
	})

	require.Equal(t, []string{
		"https://api.example.com",
		"http://1.2.3.4:80",
		"https://backup.example.net",
	}, values)
	require.Equal(t, "https://api.example.com,http://1.2.3.4:80,https://backup.example.net", centerURLsEnvValue("https://api.example.com/", values[1:]))
}

func TestNormalizeDeployCenterRequest_UsesFirstValidAsPrimary(t *testing.T) {
	req := &DeployRequest{
		CenterURL:  "not-a-url",
		CenterURLs: []string{"http://center.example.com/", "http://203.0.113.10/", "http://203.0.113.11"},
	}

	require.NoError(t, normalizeDeployCenterRequest(req))
	require.Equal(t, "http://center.example.com", req.CenterURL)
	require.Equal(t, []string{"http://203.0.113.10", "http://203.0.113.11"}, req.CenterURLs)
}

func TestNormalizeCenterURLList_AddsConfiguredControlPlaneFallback(t *testing.T) {
	t.Setenv("CENTER_SERVER_FALLBACK_URLS", "http://center.example.com,http://203.0.113.10,http://203.0.113.11")

	values := normalizeCenterURLList("http://center.example.com", nil)

	require.Equal(t, []string{"http://center.example.com", "http://203.0.113.10", "http://203.0.113.11"}, values)
}

func TestNormalizeCenterURLList_DoesNotAddUnconfiguredFallback(t *testing.T) {
	values := normalizeCenterURLList("http://center.example.com", nil)

	require.Equal(t, []string{"http://center.example.com"}, values)
}

func TestNodeAgentImageCandidates_EnvPathFirstAndDeduped(t *testing.T) {
	customPath := filepath.Join(t.TempDir(), "node-agent-image.tar.gz")
	t.Setenv("NODE_AGENT_IMAGE_PATH", customPath)

	values := nodeAgentImageCandidates()

	require.NotEmpty(t, values)
	require.Equal(t, customPath, values[0])
	seen := map[string]bool{}
	for _, value := range values {
		require.NotEmpty(t, value)
		require.False(t, seen[value], "duplicate candidate %s", value)
		seen[value] = true
	}
	require.Contains(t, values, "/root/raypilot-artifacts/node-agent-image.tar.gz")
	require.Contains(t, values, "/root/node-agent-image.tar.gz")
}

func TestNodeAgentImageCandidates_EmptyEnvIgnored(t *testing.T) {
	t.Setenv("NODE_AGENT_IMAGE_PATH", " ")

	values := nodeAgentImageCandidates()

	require.NotContains(t, values, "")
	require.NotContains(t, values, " ")
}

func TestPushImageReportsAllCheckedPathsWhenMissing(t *testing.T) {
	missingPath := filepath.Join(t.TempDir(), "missing.tar.gz")
	t.Setenv("NODE_AGENT_IMAGE_PATH", missingPath)

	_, err := os.Stat(missingPath)
	require.ErrorIs(t, err, os.ErrNotExist)

	candidates := nodeAgentImageCandidates()
	require.Equal(t, missingPath, candidates[0])

	_, _, err = locateNodeAgentImage()
	require.Error(t, err)
	require.Contains(t, err.Error(), missingPath)
	require.Contains(t, err.Error(), "make node-agent-image")
}

func TestLocateNodeAgentImage_RejectsEmptyFile(t *testing.T) {
	imagePath := filepath.Join(t.TempDir(), "empty.tar.gz")
	require.NoError(t, os.WriteFile(imagePath, nil, 0644))
	t.Setenv("NODE_AGENT_IMAGE_PATH", imagePath)

	_, _, err := locateNodeAgentImage()

	require.Error(t, err)
	require.Contains(t, err.Error(), "is empty")
}

func TestLocateNodeAgentImage_FindsEnvPath(t *testing.T) {
	imagePath := filepath.Join(t.TempDir(), "node-agent-image.tar.gz")
	require.NoError(t, os.WriteFile(imagePath, []byte("fake-image"), 0644))
	t.Setenv("NODE_AGENT_IMAGE_PATH", imagePath)

	path, size, err := locateNodeAgentImage()

	require.NoError(t, err)
	require.Equal(t, imagePath, path)
	require.EqualValues(t, len("fake-image"), size)
}

func TestUniqueDeployPorts_DedupesAndSorts(t *testing.T) {
	ports := uniqueDeployPorts([]uint32{8443, 0, 443, 8443, 2053})

	require.Equal(t, []uint32{443, 2053, 8443}, ports)
}

func TestDeployPortsForOptions_IncludesProxyOffsets(t *testing.T) {
	ports := deployPortsForOptions([]deployTransportOption{
		{Transport: "tcp", Port: 443},
		{Transport: "xhttp", Port: 8443},
	}, 3)

	require.Equal(t, []uint32{443, 444, 445, 8443, 8444, 8445}, ports)
}

func TestDeployListenEndpointsForOptions_AllowsSamePortOnDifferentIPs(t *testing.T) {
	endpoints := deployListenEndpointsForOptions([]string{"203.0.113.10", "203.0.113.11"}, true, []deployTransportOption{
		{Transport: "tcp", Port: 443},
	}, 1)

	require.Equal(t, []deployListenEndpoint{
		{IP: "203.0.113.10", Port: 443},
		{IP: "203.0.113.11", Port: 443},
	}, endpoints)
	require.Equal(t, "[203.0.113.10:443 203.0.113.11:443]", formatDeployListenEndpoints(endpoints))
}

func TestDeployPortsFromEndpoints_DedupesAndSorts(t *testing.T) {
	ports := deployPortsFromEndpoints([]deployListenEndpoint{
		{IP: "203.0.113.11", Port: 8443},
		{IP: "203.0.113.10", Port: 443},
		{IP: "203.0.113.12", Port: 8443},
		{IP: "203.0.113.13", Port: 0},
	})

	require.Equal(t, []uint32{443, 8443}, ports)
}

func TestDeployEndpointAwkPattern_BindsSpecificIPOrWildcard(t *testing.T) {
	require.Equal(t, `203\.0\.113\.10:443$`, deployEndpointAwkPattern("203.0.113.10", 443))
	require.Equal(t, `(^|:|\\])443$`, deployEndpointAwkPattern("0.0.0.0", 443))
	require.Equal(t, `(^|:|\\])443$`, deployEndpointAwkPattern("", 443))
}

func TestTruncateForDeployStep(t *testing.T) {
	require.Equal(t, "abc", truncateForDeployStep(" abc ", 10))
	require.Equal(t, "abc...(truncated)", truncateForDeployStep("abcdef", 3))
}

func TestNormalizeDeployOutboundProxyURLs_MultipleLines(t *testing.T) {
	values := normalizeDeployOutboundProxyURLs("socks5", " socks5://u1:p1@h1:3010 \n\nsocks5://u2:p2@h2:3011\r\n")

	require.Equal(t, []string{
		"socks5://u1:p1@h1:3010",
		"socks5://u2:p2@h2:3011",
	}, values)
}

func TestNormalizeDeployUDPEnabled_DefaultsByOutboundType(t *testing.T) {
	require.True(t, normalizeDeployUDPEnabled("direct", nil))
	require.True(t, normalizeDeployUDPEnabled("", nil))
	require.False(t, normalizeDeployUDPEnabled("socks5", nil))

	enabled := true
	disabled := false
	require.True(t, normalizeDeployUDPEnabled("socks5", &enabled))
	require.False(t, normalizeDeployUDPEnabled("direct", &disabled))
}

func TestNormalizeOptionalIPv4_AcceptsAutoAndIPv4(t *testing.T) {
	require.Equal(t, "203.0.113.88", normalizeOptionalIPv4(" 203.0.113.88 "))
	require.Empty(t, normalizeOptionalIPv4("auto"))
	require.Empty(t, normalizeOptionalIPv4("AUTO"))
	require.Empty(t, normalizeOptionalIPv4("not-an-ip"))
}

func TestDeployProxyNodeName_WithMultipleProxyAndTransport(t *testing.T) {
	name := deployProxyNodeName("美国家宽", "198.51.100.20", 1, 3, deployTransportOption{Transport: "xhttp"}, 2)

	require.Equal(t, "美国家宽-2-XHTTP", name)
}
