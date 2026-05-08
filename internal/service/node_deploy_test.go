package service

import (
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

func TestNormalizeDeployUint64IDs_DedupesAndSorts(t *testing.T) {
	ids, err := normalizeDeployUint64IDs([]uint64{3, 1, 3, 2})

	require.NoError(t, err)
	require.Equal(t, []uint64{1, 2, 3}, ids)
}

func TestNormalizeDeployUint64IDs_RejectsZero(t *testing.T) {
	_, err := normalizeDeployUint64IDs([]uint64{1, 0})

	require.Error(t, err)
}

func TestNormalizeDeployOutboundProxyURLs_MultipleLines(t *testing.T) {
	values := normalizeDeployOutboundProxyURLs("socks5", " socks5://u1:p1@h1:3010 \n\nsocks5://u2:p2@h2:3011\r\n")

	require.Equal(t, []string{
		"socks5://u1:p1@h1:3010",
		"socks5://u2:p2@h2:3011",
	}, values)
}

func TestDeployProxyNodeName_WithMultipleProxyAndTransport(t *testing.T) {
	name := deployProxyNodeName("美国家宽", "156.238.231.16", 1, 3, deployTransportOption{Transport: "xhttp"}, 2)

	require.Equal(t, "美国家宽-2-XHTTP", name)
}
