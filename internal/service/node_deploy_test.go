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
