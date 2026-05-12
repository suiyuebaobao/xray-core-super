package model

import "strings"

type RegionOption struct {
	Code string `json:"code"`
	Name string `json:"name"`
	Flag string `json:"flag"`
}

var RegionOptions = withRegionFlags([]RegionOption{
	{Code: "HK", Name: "香港"},
	{Code: "US", Name: "美国"},
	{Code: "JP", Name: "日本"},
	{Code: "SG", Name: "新加坡"},
	{Code: "TW", Name: "台湾"},
	{Code: "KR", Name: "韩国"},
	{Code: "GB", Name: "英国"},
	{Code: "DE", Name: "德国"},
	{Code: "FR", Name: "法国"},
	{Code: "CA", Name: "加拿大"},
	{Code: "AU", Name: "澳大利亚"},
	{Code: "TH", Name: "泰国"},
	{Code: "VN", Name: "越南"},
	{Code: "ID", Name: "印尼"},
	{Code: "MY", Name: "马来西亚"},
	{Code: "TR", Name: "土耳其"},
	{Code: "PH", Name: "菲律宾"},
	{Code: "IN", Name: "印度"},
	{Code: "BR", Name: "巴西"},
	{Code: "NL", Name: "荷兰"},
	{Code: "IT", Name: "意大利"},
	{Code: "ES", Name: "西班牙"},
	{Code: "RU", Name: "俄罗斯"},
	{Code: "AE", Name: "阿联酋"},
	{Code: "ZA", Name: "南非"},
	{Code: "MO", Name: "澳门"},
	{Code: "CN", Name: "中国大陆"},
	{Code: "KH", Name: "柬埔寨"},
	{Code: "LA", Name: "老挝"},
	{Code: "MM", Name: "缅甸"},
	{Code: "BN", Name: "文莱"},
	{Code: "MN", Name: "蒙古"},
	{Code: "NP", Name: "尼泊尔"},
	{Code: "BD", Name: "孟加拉"},
	{Code: "PK", Name: "巴基斯坦"},
	{Code: "LK", Name: "斯里兰卡"},
	{Code: "KZ", Name: "哈萨克斯坦"},
	{Code: "UZ", Name: "乌兹别克斯坦"},
	{Code: "KG", Name: "吉尔吉斯斯坦"},
	{Code: "TJ", Name: "塔吉克斯坦"},
	{Code: "SA", Name: "沙特阿拉伯"},
	{Code: "IL", Name: "以色列"},
	{Code: "QA", Name: "卡塔尔"},
	{Code: "KW", Name: "科威特"},
	{Code: "BH", Name: "巴林"},
	{Code: "OM", Name: "阿曼"},
	{Code: "JO", Name: "约旦"},
	{Code: "EG", Name: "埃及"},
	{Code: "MA", Name: "摩洛哥"},
	{Code: "KE", Name: "肯尼亚"},
	{Code: "NG", Name: "尼日利亚"},
	{Code: "GH", Name: "加纳"},
	{Code: "ET", Name: "埃塞俄比亚"},
	{Code: "MX", Name: "墨西哥"},
	{Code: "PA", Name: "巴拿马"},
	{Code: "AR", Name: "阿根廷"},
	{Code: "CL", Name: "智利"},
	{Code: "CO", Name: "哥伦比亚"},
	{Code: "PE", Name: "秘鲁"},
	{Code: "EC", Name: "厄瓜多尔"},
	{Code: "UY", Name: "乌拉圭"},
	{Code: "PY", Name: "巴拉圭"},
	{Code: "CR", Name: "哥斯达黎加"},
	{Code: "GT", Name: "危地马拉"},
	{Code: "DO", Name: "多米尼加"},
	{Code: "PR", Name: "波多黎各"},
	{Code: "SE", Name: "瑞典"},
	{Code: "NO", Name: "挪威"},
	{Code: "FI", Name: "芬兰"},
	{Code: "DK", Name: "丹麦"},
	{Code: "PL", Name: "波兰"},
	{Code: "CZ", Name: "捷克"},
	{Code: "AT", Name: "奥地利"},
	{Code: "CH", Name: "瑞士"},
	{Code: "BE", Name: "比利时"},
	{Code: "IE", Name: "爱尔兰"},
	{Code: "PT", Name: "葡萄牙"},
	{Code: "GR", Name: "希腊"},
	{Code: "LU", Name: "卢森堡"},
	{Code: "RO", Name: "罗马尼亚"},
	{Code: "BG", Name: "保加利亚"},
	{Code: "HU", Name: "匈牙利"},
	{Code: "UA", Name: "乌克兰"},
	{Code: "RS", Name: "塞尔维亚"},
	{Code: "HR", Name: "克罗地亚"},
	{Code: "SI", Name: "斯洛文尼亚"},
	{Code: "SK", Name: "斯洛伐克"},
	{Code: "LT", Name: "立陶宛"},
	{Code: "LV", Name: "拉脱维亚"},
	{Code: "EE", Name: "爱沙尼亚"},
	{Code: "IS", Name: "冰岛"},
	{Code: "MT", Name: "马耳他"},
	{Code: "CY", Name: "塞浦路斯"},
	{Code: "NZ", Name: "新西兰"},
	{Code: "FJ", Name: "斐济"},
	{Code: "PG", Name: "巴布亚新几内亚"},
	{Code: "GU", Name: "关岛"},
	{Code: "GLOBAL", Name: "全球", Flag: "🌐"},
})

func withRegionFlags(options []RegionOption) []RegionOption {
	for i := range options {
		if options[i].Flag == "" {
			options[i].Flag = isoCountryFlag(options[i].Code)
		}
	}
	return options
}

func isoCountryFlag(code string) string {
	code = strings.ToUpper(strings.TrimSpace(code))
	if len(code) != 2 {
		return ""
	}
	runes := []rune(code)
	if runes[0] < 'A' || runes[0] > 'Z' || runes[1] < 'A' || runes[1] > 'Z' {
		return ""
	}
	return string([]rune{0x1F1E6 + runes[0] - 'A', 0x1F1E6 + runes[1] - 'A'})
}

func NormalizeRegion(code, name, flag string) RegionOption {
	code = strings.ToUpper(strings.TrimSpace(code))
	if code != "" {
		for _, option := range RegionOptions {
			if option.Code == code {
				return option
			}
		}
	}
	name = strings.TrimSpace(name)
	flag = strings.TrimSpace(flag)
	if name == "" && flag == "" {
		return RegionOption{}
	}
	if code == "" {
		code = "CUSTOM"
	}
	return RegionOption{
		Code: sanitizeRegionText(code, 16),
		Name: sanitizeRegionText(name, 64),
		Flag: sanitizeRegionText(flag, 16),
	}
}

func sanitizeRegionText(value string, limit int) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	value = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, value)
	runes := []rune(value)
	if len(runes) > limit {
		value = string(runes[:limit])
	}
	return value
}
