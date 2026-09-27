package service

import (
	"strings"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

// proxyCountryAliases 收录常见的三位代码、英文全称与中文名，比较地区前统一折算为两位国家代码，
// 避免 US / USA / United States / 美国 这类写法差异导致有代理却匹配不上。
var proxyCountryAliases = map[string]string{
	"USA": "US", "UNITED STATES": "US", "UNITED STATES OF AMERICA": "US", "AMERICA": "US", "美国": "US",
	"UK": "GB", "GBR": "GB", "UNITED KINGDOM": "GB", "GREAT BRITAIN": "GB", "BRITAIN": "GB", "ENGLAND": "GB", "英国": "GB",
	"JPN": "JP", "JAPAN": "JP", "日本": "JP",
	"KOR": "KR", "KOREA": "KR", "SOUTH KOREA": "KR", "REPUBLIC OF KOREA": "KR", "韩国": "KR",
	"CHN": "CN", "CHINA": "CN", "中国": "CN",
	"HKG": "HK", "HONG KONG": "HK", "香港": "HK", "中国香港": "HK",
	"TWN": "TW", "TAIWAN": "TW", "台湾": "TW", "中国台湾": "TW",
	"SGP": "SG", "SINGAPORE": "SG", "新加坡": "SG",
	"DEU": "DE", "GERMANY": "DE", "德国": "DE",
	"FRA": "FR", "FRANCE": "FR", "法国": "FR",
	"CAN": "CA", "CANADA": "CA", "加拿大": "CA",
	"AUS": "AU", "AUSTRALIA": "AU", "澳大利亚": "AU",
	"IND": "IN", "INDIA": "IN", "印度": "IN",
	"PHL": "PH", "PHILIPPINES": "PH", "菲律宾": "PH",
	"THA": "TH", "THAILAND": "TH", "泰国": "TH",
	"VNM": "VN", "VIETNAM": "VN", "VIET NAM": "VN", "越南": "VN",
	"IDN": "ID", "INDONESIA": "ID", "印度尼西亚": "ID", "印尼": "ID",
	"MYS": "MY", "MALAYSIA": "MY", "马来西亚": "MY",
}

var proxyCountrySeparators = strings.NewReplacer(".", "", "_", " ", "-", " ")

// CanonicalProxyCountry 去空格、转大写并处理常见别名，返回两位国家代码；无法识别时返回空字符串。
func CanonicalProxyCountry(raw string) string {
	value := strings.ToUpper(strings.Join(strings.Fields(proxyCountrySeparators.Replace(raw)), " "))
	if code, ok := proxyCountryAliases[value]; ok {
		return code
	}
	if len(value) != 2 || value[0] < 'A' || value[0] > 'Z' || value[1] < 'A' || value[1] > 'Z' {
		return ""
	}
	return value
}

// NormalizeProxyCountryCode accepts an optional ISO 3166-1 alpha-2 code or a
// common alias of one. Empty values mean that the proxy country is unknown and
// should be inferred from the latest egress probe when one is available.
func NormalizeProxyCountryCode(raw string) (string, error) {
	if strings.TrimSpace(raw) == "" {
		return "", nil
	}
	value := CanonicalProxyCountry(raw)
	if value == "" {
		return "", infraerrors.BadRequest("INVALID_PROXY_COUNTRY_CODE", "代理国家必须是两位国家代码")
	}
	return value, nil
}
