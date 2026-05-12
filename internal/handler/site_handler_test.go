package handler_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"suiyue/internal/handler"
	"suiyue/internal/model"
	"suiyue/internal/repository"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func setupSiteConfigHandlerTest(t *testing.T) (*gin.Engine, *gorm.DB) {
	t.Helper()

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		DisableForeignKeyConstraintWhenMigrating: true,
	})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.SiteSetting{}))

	siteHandler := handler.NewSiteConfigHandler(repository.NewSiteSettingRepository(db))
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/api/admin/site/subscription", siteHandler.GetSubscriptionConfig)
	r.PUT("/api/admin/site/subscription", siteHandler.UpdateSubscriptionConfig)
	return r, db
}

func TestSiteConfigHandler_SubscriptionConfig_DefaultAndUpdate(t *testing.T) {
	r, _ := setupSiteConfigHandlerTest(t)

	req := httptest.NewRequest(http.MethodGet, "/api/admin/site/subscription", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), `"profile_name":"RayPilot"`)
	assert.Contains(t, w.Body.String(), `"include_user_info":true`)

	body := []byte(`{
		"profile_name":"LeiYun/VPN",
		"custom_rules":["DOMAIN-SUFFIX,openai.com,PROXY","GEOIP,CN,DIRECT"],
		"include_user_info":false,
		"profile_update_interval":6,
		"profile_web_page_url":"/subscription",
		"node_name_template":"{{flag}} {{region}} {{name}}",
		"include_region_icon":true,
		"enable_url_test_group":true,
		"health_check_url":"https://cp.cloudflare.com/generate_204",
		"url_test_interval":300,
		"proxy_groups":[
			{"name":"PROXY","type":"select","include_all":true,"include_auto":true,"include_direct":true},
			{"name":"美国节点","type":"url-test","node_ids":[2,3],"include_all":false,"include_auto":false,"include_direct":false}
		]
	}`)
	req = httptest.NewRequest(http.MethodPut, "/api/admin/site/subscription", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), `"profile_name":"LeiYun-VPN"`)
	assert.Contains(t, w.Body.String(), `"DOMAIN-SUFFIX,openai.com,PROXY"`)
	assert.Contains(t, w.Body.String(), `"MATCH,PROXY"`)
	assert.Contains(t, w.Body.String(), `"include_user_info":false`)
	assert.Contains(t, w.Body.String(), `"profile_update_interval":6`)
	assert.Contains(t, w.Body.String(), `"profile_web_page_url":"/subscription"`)
	assert.Contains(t, w.Body.String(), `"node_name_template":"{{flag}} {{region}} {{name}}"`)
	assert.Contains(t, w.Body.String(), `"enable_url_test_group":true`)
	assert.Contains(t, w.Body.String(), `"health_check_url":"https://cp.cloudflare.com/generate_204"`)
	assert.Contains(t, w.Body.String(), `"proxy_groups"`)
	assert.Contains(t, w.Body.String(), `"美国节点"`)
}
