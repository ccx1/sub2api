package service

import (
	"context"
	"fmt"
	"html"
	"io"
	"net"
	"net/http"
	"regexp"
	"strings"
	"time"
)

const (
	proxyQualityIPFMTarget       = "ipfm_location"
	proxyQualityIPFMMaxBodyBytes = int64(64 * 1024)
)

// proxyQualityIPFMBaseURL 是 ip.fm 的 IP 查询页，末尾追加出口 IP 即返回包含 Location 的 HTML。
var proxyQualityIPFMBaseURL = "https://ip.fm/ip/"

var (
	proxyQualityIPFMLocationPattern = regexp.MustCompile(`(?s)Location:\s*</span>(.*?)</p>`)
	proxyQualityHTMLTagPattern      = regexp.MustCompile(`<[^>]*>`)
)

// runProxyQualityIPFMLocation 通过代理向 ip.fm 查询出口 IP 的归属地，作为 ip-api 之外的第二来源。
// 该检测只用于交叉核对位置，查询失败或未识别位置仅记为告警，不判定代理不可用。
func runProxyQualityIPFMLocation(ctx context.Context, client *http.Client, exitIP string) ProxyQualityCheckItem {
	item := ProxyQualityCheckItem{
		Target: proxyQualityIPFMTarget,
	}

	ip := net.ParseIP(strings.TrimSpace(exitIP))
	if ip == nil {
		item.Status = "warn"
		item.Message = "出口 IP 为空或无效，跳过 ip.fm 位置检测"
		return item
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, proxyQualityIPFMBaseURL+ip.String(), nil)
	if err != nil {
		item.Status = "warn"
		item.Message = fmt.Sprintf("构建请求失败: %v", err)
		return item
	}
	req.Header.Set("Accept", "text/html,*/*")
	req.Header.Set("User-Agent", proxyQualityClientUserAgent)

	start := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		item.Status = "warn"
		item.LatencyMs = time.Since(start).Milliseconds()
		item.Message = fmt.Sprintf("ip.fm 请求失败: %v", err)
		return item
	}
	defer func() { _ = resp.Body.Close() }()
	item.LatencyMs = time.Since(start).Milliseconds()
	item.HTTPStatus = resp.StatusCode

	if resp.StatusCode != http.StatusOK {
		item.Status = "warn"
		item.Message = fmt.Sprintf("ip.fm 返回非预期状态码: %d", resp.StatusCode)
		return item
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, proxyQualityIPFMMaxBodyBytes))
	if err != nil {
		item.Status = "warn"
		item.Message = fmt.Sprintf("读取响应失败: %v", err)
		return item
	}

	location, ok := parseIPFMLocation(body)
	if !ok {
		item.Status = "warn"
		item.Message = "ip.fm 响应中未找到 Location 字段"
		return item
	}
	item.Status = "pass"
	if location == "" {
		item.Message = fmt.Sprintf("ip.fm 未收录 %s 的位置", ip.String())
		return item
	}
	item.Message = location
	return item
}

// parseIPFMLocation 从 ip.fm 查询页中提取 “Location:” 字段；ok 表示页面包含该字段。
// 未收录位置的 IP 在页面上渲染为 null 标签，此时返回空位置。
func parseIPFMLocation(body []byte) (location string, ok bool) {
	match := proxyQualityIPFMLocationPattern.FindSubmatch(body)
	if match == nil {
		return "", false
	}
	location = proxyQualityHTMLTagPattern.ReplaceAllString(string(match[1]), "")
	location = strings.TrimSpace(html.UnescapeString(location))
	if strings.EqualFold(location, "null") {
		location = ""
	}
	return location, true
}
