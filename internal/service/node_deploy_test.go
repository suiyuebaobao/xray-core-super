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
