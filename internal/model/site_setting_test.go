package model

import "testing"

func TestDefaultSalesLandingConfig_UsesShortTitle(t *testing.T) {
	cfg := DefaultSalesLandingConfig()
	if cfg.Title != "高速 VPN 节点" {
		t.Fatalf("expected short sales title, got %q", cfg.Title)
	}
}

func TestNormalizeSalesLandingConfig_FiltersEmptyItems(t *testing.T) {
	cfg := NormalizeSalesLandingConfig(SalesLandingConfig{
		Brand:      "  LeiYun  ",
		Title:      "  稳定节点  ",
		NavLinks:   []SalesLandingLink{{Label: "  套餐  ", To: "  #plans  "}, {}},
		Badges:     []string{" 高速 ", " "},
		PrimaryCTA: SalesLandingLink{Label: "购买", To: "/register"},
		Plans: []SalesLandingPlan{
			{Name: "推荐", Features: []string{" 可用 ", " "}},
			{},
		},
		FinalCTA: SalesLandingCTA{
			Title:       "开通",
			Text:        "选择套餐",
			ButtonLabel: "注册",
			ButtonTo:    "/register",
			FooterLinks: []SalesLandingLink{{Label: "登录", To: "/login"}, {}},
		},
		FooterText: " 服务 ",
	})

	if cfg.Brand != "LeiYun" || cfg.Title != "稳定节点" || cfg.FooterText != "服务" {
		t.Fatalf("basic text was not trimmed: %#v", cfg)
	}
	if len(cfg.NavLinks) != 1 || cfg.NavLinks[0].Label != "套餐" || cfg.NavLinks[0].To != "#plans" {
		t.Fatalf("nav links were not normalized: %#v", cfg.NavLinks)
	}
	if len(cfg.Badges) != 1 || cfg.Badges[0] != "高速" {
		t.Fatalf("badges were not normalized: %#v", cfg.Badges)
	}
	if len(cfg.Plans) != 1 || len(cfg.Plans[0].Features) != 1 || cfg.Plans[0].Features[0] != "可用" {
		t.Fatalf("plans were not normalized: %#v", cfg.Plans)
	}
	if len(cfg.FinalCTA.FooterLinks) != 1 || cfg.FinalCTA.FooterLinks[0].Label != "登录" {
		t.Fatalf("footer links were not normalized: %#v", cfg.FinalCTA.FooterLinks)
	}
}

func TestNormalizeSalesLandingConfig_EmptyPlanFeaturesRemainEditable(t *testing.T) {
	cfg := NormalizeSalesLandingConfig(SalesLandingConfig{
		Title: "节点",
		Plans: []SalesLandingPlan{
			{Name: "无权益卡片"},
		},
	})
	if cfg.Plans[0].Features == nil {
		t.Fatalf("expected empty features slice, got nil")
	}
}

func TestNormalizeSalesLandingConfig_SanitizesUnsafeLinks(t *testing.T) {
	cfg := NormalizeSalesLandingConfig(SalesLandingConfig{
		NavLinks:     []SalesLandingLink{{Label: "危险", To: "javascript:alert(1)"}, {Label: "官网", To: "https://example.com/path"}},
		PrimaryCTA:   SalesLandingLink{Label: "购买", To: "javascript:alert(1)"},
		SecondaryCTA: SalesLandingLink{Label: "登录", To: "/login"},
		FinalCTA: SalesLandingCTA{
			Title:       "开通",
			Text:        "选择套餐",
			ButtonLabel: "注册",
			ButtonTo:    "data:text/html,boom",
			FooterLinks: []SalesLandingLink{{Label: "锚点", To: "#plans"}, {Label: "坏链接", To: "vbscript:msgbox(1)"}},
		},
	})

	if cfg.NavLinks[0].To != "#" {
		t.Fatalf("expected unsafe nav link to become #, got %q", cfg.NavLinks[0].To)
	}
	if cfg.NavLinks[1].To != "https://example.com/path" {
		t.Fatalf("expected https nav link to pass, got %q", cfg.NavLinks[1].To)
	}
	if cfg.PrimaryCTA.To != DefaultSalesLandingConfig().PrimaryCTA.To {
		t.Fatalf("expected unsafe primary CTA to fallback, got %q", cfg.PrimaryCTA.To)
	}
	if cfg.FinalCTA.ButtonTo != DefaultSalesLandingConfig().FinalCTA.ButtonTo {
		t.Fatalf("expected unsafe final CTA to fallback, got %q", cfg.FinalCTA.ButtonTo)
	}
	if cfg.FinalCTA.FooterLinks[0].To != "#plans" || cfg.FinalCTA.FooterLinks[1].To != "#" {
		t.Fatalf("footer links were not sanitized: %#v", cfg.FinalCTA.FooterLinks)
	}
}

func TestParseSalesLandingConfig_InvalidJSONReturnsDefault(t *testing.T) {
	cfg := ParseSalesLandingConfig("{bad json")
	if cfg.Title != DefaultSalesLandingConfig().Title {
		t.Fatalf("expected default config on invalid json, got %#v", cfg)
	}
}

func TestNormalizeSubscriptionConfig_FiltersRulesAndKeepsCatchAll(t *testing.T) {
	cfg := NormalizeSubscriptionConfig(SubscriptionConfig{
		ProfileName:           "  My/VPN.yaml  ",
		CustomRules:           []string{" # comment ", "DOMAIN-SUFFIX,openai.com,PROXY", "domain-suffix,openai.com,PROXY", "GEOIP,CN,DIRECT"},
		IncludeUserInfo:       true,
		ProfileUpdateInterval: 999,
		ProfileWebPageURL:     "javascript:alert(1)",
	})

	if cfg.ProfileName != "My-VPN" {
		t.Fatalf("expected sanitized profile name, got %q", cfg.ProfileName)
	}
	if len(cfg.CustomRules) != 3 {
		t.Fatalf("expected deduplicated rules with fallback catch-all, got %#v", cfg.CustomRules)
	}
	if cfg.CustomRules[2] != "MATCH,PROXY" {
		t.Fatalf("expected fallback MATCH rule, got %#v", cfg.CustomRules)
	}
	if cfg.ProfileUpdateInterval != 168 {
		t.Fatalf("expected capped update interval, got %d", cfg.ProfileUpdateInterval)
	}
	if cfg.ProfileWebPageURL != "" {
		t.Fatalf("expected unsafe web page url to be stripped, got %q", cfg.ProfileWebPageURL)
	}
}

func TestParseSubscriptionConfig_MissingIncludeUserInfoDefaultsTrue(t *testing.T) {
	cfg := ParseSubscriptionConfig(`{"profile_name":"LeiYun","custom_rules":["MATCH,PROXY"]}`)
	if cfg.ProfileName != "LeiYun" {
		t.Fatalf("expected parsed profile name, got %q", cfg.ProfileName)
	}
	if !cfg.IncludeUserInfo {
		t.Fatalf("expected include_user_info to default true")
	}
	if !cfg.EnableURLTestGroup {
		t.Fatalf("expected url-test group to default true")
	}
}

func TestParseSubscriptionConfig_CanDisableUserInfo(t *testing.T) {
	cfg := ParseSubscriptionConfig(`{"profile_name":"LeiYun","custom_rules":["MATCH,PROXY"],"include_user_info":false}`)
	if cfg.IncludeUserInfo {
		t.Fatalf("expected include_user_info to be disabled")
	}
}

func TestParseSubscriptionConfig_ProxyGroupsAndURLTest(t *testing.T) {
	cfg := ParseSubscriptionConfig(`{
		"profile_name":"LeiYun",
		"custom_rules":["MATCH,PROXY"],
		"include_region_icon":true,
		"enable_url_test_group":true,
		"node_name_template":"{{flag}} {{region}} {{name}}",
		"health_check_url":"https://cp.cloudflare.com/generate_204",
		"url_test_interval":300,
		"proxy_groups":[
			{"name":"PROXY","type":"select","include_all":true,"include_auto":true,"include_direct":true},
			{"name":"美国节点","type":"url-test","node_ids":[2,2,3],"include_direct":false}
		]
	}`)
	if !cfg.EnableURLTestGroup {
		t.Fatalf("expected url-test group to be enabled")
	}
	if cfg.NodeNameTemplate != "{{flag}} {{region}} {{name}}" {
		t.Fatalf("unexpected node name template: %q", cfg.NodeNameTemplate)
	}
	if len(cfg.ProxyGroups) != 2 {
		t.Fatalf("expected two proxy groups, got %#v", cfg.ProxyGroups)
	}
	if cfg.ProxyGroups[1].Type != "url-test" || len(cfg.ProxyGroups[1].NodeIDs) != 2 {
		t.Fatalf("expected normalized manual url-test group, got %#v", cfg.ProxyGroups[1])
	}
}

func TestNormalizeRegion_BuiltInAndCustom(t *testing.T) {
	if len(RegionOptions) < 80 {
		t.Fatalf("expected broad region catalog, got %d options", len(RegionOptions))
	}
	us := NormalizeRegion("us", "", "")
	if us.Code != "US" || us.Name != "美国" || us.Flag != "🇺🇸" {
		t.Fatalf("expected built-in US region, got %#v", us)
	}
	global := NormalizeRegion("global", "", "")
	if global.Code != "GLOBAL" || global.Flag != "🌐" {
		t.Fatalf("expected global region option, got %#v", global)
	}
	custom := NormalizeRegion("", "火星", "🚀")
	if custom.Code != "CUSTOM" || custom.Name != "火星" || custom.Flag != "🚀" {
		t.Fatalf("expected custom region to be preserved, got %#v", custom)
	}
}
