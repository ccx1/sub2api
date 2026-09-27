package service

import (
	"context"
	"strconv"
	"strings"
)

// Codex 计算节点身份来自 __oailb 的 host 声明：chat.gateway.unified-<N>.api.openai.com。
// 分配规律是同一大区内不保证同一国家（德国出口落到西班牙节点属正常），跨大区才是异常。
const (
	codexGatewayNodeHostPrefix = "chat.gateway.unified-"
	codexGatewayNodeHostSuffix = ".api.openai.com"

	CodexMacroRegionNorthAmerica = "NA"
	CodexMacroRegionSouthAmerica = "SA"
	CodexMacroRegionEurope       = "EU"
	CodexMacroRegionAsiaPacific  = "APAC"
)

type codexGatewayNode struct {
	Number int
	// Country/Region 为空表示编号合法但不在当前枚举表中（空洞或新增），不能推断大区。
	Country string
	Region  string
}

func (n codexGatewayNode) Name() string {
	return "unified-" + strconv.Itoa(n.Number)
}

func (n codexGatewayNode) MacroRegion() string {
	return codexMacroRegionForCountry(n.Country)
}

var codexGatewayFleetNodes = func() map[int]codexGatewayNode {
	nodes := make(map[int]codexGatewayNode)
	for _, group := range codexGatewayFleetRegions {
		for _, number := range group.nodes {
			nodes[number] = codexGatewayNode{Number: number, Country: group.country, Region: group.region}
		}
	}
	return nodes
}()

// 大区按 Azure 地理分组；节点表覆盖的 22 国之外，另收录常见代理出口国家，
// 未收录的国家返回空，调用方按“大区未知”处理而不是判为跨区。
var codexMacroRegionCountries = map[string]string{
	"US": CodexMacroRegionNorthAmerica, "CA": CodexMacroRegionNorthAmerica, "MX": CodexMacroRegionNorthAmerica,
	"BR": CodexMacroRegionSouthAmerica, "AR": CodexMacroRegionSouthAmerica, "CL": CodexMacroRegionSouthAmerica,
	"CO": CodexMacroRegionSouthAmerica, "PE": CodexMacroRegionSouthAmerica,
	"IE": CodexMacroRegionEurope, "GB": CodexMacroRegionEurope, "ES": CodexMacroRegionEurope, "PT": CodexMacroRegionEurope,
	"FR": CodexMacroRegionEurope, "DE": CodexMacroRegionEurope, "NL": CodexMacroRegionEurope, "BE": CodexMacroRegionEurope,
	"LU": CodexMacroRegionEurope, "CH": CodexMacroRegionEurope, "AT": CodexMacroRegionEurope, "IT": CodexMacroRegionEurope,
	"PL": CodexMacroRegionEurope, "CZ": CodexMacroRegionEurope, "DK": CodexMacroRegionEurope, "SE": CodexMacroRegionEurope,
	"NO": CodexMacroRegionEurope, "FI": CodexMacroRegionEurope,
	"JP": CodexMacroRegionAsiaPacific, "KR": CodexMacroRegionAsiaPacific, "TW": CodexMacroRegionAsiaPacific,
	"HK": CodexMacroRegionAsiaPacific, "CN": CodexMacroRegionAsiaPacific, "SG": CodexMacroRegionAsiaPacific,
	"MY": CodexMacroRegionAsiaPacific, "ID": CodexMacroRegionAsiaPacific, "TH": CodexMacroRegionAsiaPacific,
	"VN": CodexMacroRegionAsiaPacific, "PH": CodexMacroRegionAsiaPacific, "IN": CodexMacroRegionAsiaPacific,
	"AU": CodexMacroRegionAsiaPacific, "NZ": CodexMacroRegionAsiaPacific,
}

// CodexMacroRegionForCountry 返回两位国家代码所属大区，未知返回空字符串。
func CodexMacroRegionForCountry(country string) string {
	return codexMacroRegionForCountry(country)
}

func codexMacroRegionForCountry(country string) string {
	return codexMacroRegionCountries[strings.ToUpper(strings.TrimSpace(country))]
}

// parseCodexGatewayNodeHost 只接受 chat.gateway.unified-<N>.api.openai.com，N 在枚举上界内。
func parseCodexGatewayNodeHost(host string) (codexGatewayNode, bool) {
	host = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(host)), ".")
	if !strings.HasPrefix(host, codexGatewayNodeHostPrefix) || !strings.HasSuffix(host, codexGatewayNodeHostSuffix) {
		return codexGatewayNode{}, false
	}
	digits := strings.TrimSuffix(strings.TrimPrefix(host, codexGatewayNodeHostPrefix), codexGatewayNodeHostSuffix)
	if digits == "" || len(digits) > 4 || digits[0] == '0' {
		return codexGatewayNode{}, false
	}
	number, err := strconv.Atoi(digits)
	if err != nil || number <= 0 || number > codexGatewayFleetMaxNode {
		return codexGatewayNode{}, false
	}
	if node, ok := codexGatewayFleetNodes[number]; ok {
		return node, true
	}
	return codexGatewayNode{Number: number}, true
}

// codexGatewayRouteCrossRegion 仅在节点大区和出口大区都已知且不同才判跨大区。
func codexGatewayRouteCrossRegion(node codexGatewayNode, egressCountry string) bool {
	nodeRegion, egressRegion := node.MacroRegion(), codexMacroRegionForCountry(egressCountry)
	return nodeRegion != "" && egressRegion != "" && nodeRegion != egressRegion
}

type codexProxyEgressCountryResolver interface {
	CodexProxyEgressCountry(context.Context, int64) (string, error)
}

// codexTicketProxyEgressCountry 尽力读取出口国家；不可用时返回空，不阻断采集。
func (s *OpenAIGatewayService) codexTicketProxyEgressCountry(ctx context.Context, proxyID int64) string {
	if s == nil || proxyID <= 0 {
		return ""
	}
	resolver, ok := s.accountRepo.(codexProxyEgressCountryResolver)
	if !ok {
		return ""
	}
	country, err := resolver.CodexProxyEgressCountry(ctx, proxyID)
	if err != nil {
		return ""
	}
	return normalizeProxyRegionCountry(country)
}
