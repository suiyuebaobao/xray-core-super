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
		"profile_web_page_url":"/subscription"
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
}
