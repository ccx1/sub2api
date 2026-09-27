package service

// 按「codex 计算节点（chat.gateway.unified-N）全表」2026-09-25 的 DoH 枚举整理：
// 共 215 个编号、201 个存活、38 个地区、22 个国家。地名是 Azure IP 归属地，是推论而非实测；
// 编号可能被回收，重扫后需同步更新本表（codex_gateway_fleet_test.go 校验计数）。
const codexGatewayFleetMaxNode = 215

var codexGatewayFleetRegions = []struct {
	country string
	region  string
	nodes   []int
}{
	{"US", "Texas", []int{1, 2, 8, 9, 30, 31, 41, 42, 43, 46, 47, 48, 49, 50, 51, 52, 76, 77, 78}},
	{"US", "Iowa", []int{40, 44, 45, 53, 54, 63, 64, 79, 81, 130, 131, 132, 133, 148, 167, 168}},
	{"US", "Virginia", []int{4, 5, 15, 16, 17, 18, 61, 62, 65, 66, 67, 68, 69, 82, 187}},
	{"BR", "Sao Paulo", []int{83, 84, 92, 93, 101, 102, 115, 136, 137, 160, 202, 213, 214, 215}},
	{"US", "Arizona", []int{118, 119, 120, 121, 122, 123, 124, 125, 126, 127, 147, 149, 153}},
	{"US", "California", []int{6, 13, 19, 20, 21, 22, 29, 35, 36, 166}},
	{"JP", "Tokyo", []int{75, 99, 114, 139, 152, 185, 195, 196}},
	{"PL", "Mazovia", []int{24, 113, 116, 134, 135, 141, 142, 143}},
	{"AU", "New South Wales", []int{23, 25, 74, 86, 95, 110, 111}},
	{"KR", "Seoul", []int{55, 88, 98, 107, 108, 109, 197}},
	{"US", "Wyoming", []int{128, 129, 146, 154, 155, 169, 182}},
	{"IN", "Maharashtra", []int{58, 72, 91, 97, 117, 144}},
	{"BE", "Brussels Capital", []int{157, 161, 173, 188, 201}},
	{"DE", "Bremen", []int{162, 163, 174, 175, 193}},
	{"JP", "Osaka", []int{56, 87, 96, 100, 205}},
	{"DK", "Capital Region", []int{180, 189, 191, 192}},
	{"ES", "Madrid", []int{32, 39, 60, 186}},
	{"SG", "Central Singapore", []int{57, 85, 94, 112}},
	{"US", "Georgia", []int{207, 209, 210, 211}},
	{"AU", "Victoria", []int{156, 203, 208}},
	{"GB", "England", []int{80, 89, 140}},
	{"ID", "Jakarta", []int{164, 176, 194}},
	{"IT", "Lombardy", []int{26, 27, 37}},
	{"MY", "Kuala Lumpur", []int{158, 177, 198}},
	{"NZ", "Auckland", []int{73, 159, 199}},
	{"CA", "Ontario", []int{59, 151}},
	{"CH", "Geneva", []int{165, 179}},
	{"CH", "Zurich", []int{150, 178}},
	{"DE", "Hesse", []int{138, 145}},
	{"GB", "Wales", []int{103, 206}},
	{"IN", "Tamil Nadu", []int{181, 200}},
	{"IN", "Telangana", []int{204, 212}},
	{"NL", "North Holland", []int{183, 184}},
	{"US", "Illinois", []int{11, 12}},
	{"AU", "Australian Capital Territory", []int{71}},
	{"IE", "Leinster", []int{3}},
	{"SE", "Gavleborg County", []int{190}},
	{"US", "Washington", []int{7}},
}
