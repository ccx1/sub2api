package admin

// CodexImportCredentials 是导入解析结果，不包含账号查询、更新或管理员配置。
type CodexImportCredentials struct {
	Name          string
	Credentials   map[string]any
	Warnings      []string
	AgentIdentity bool
}

// ParseCodexImportValues 复用管理员的文本、多行、JSON 流和数组解析规则。
func ParseCodexImportValues(content string) ([]any, error) {
	return parseCodexSessionImportContent(content)
}

// NormalizeCodexImportCredentials 只解析凭证，不访问任何已存在账号。
func NormalizeCodexImportCredentials(value any, index int) (*CodexImportCredentials, error) {
	item, err := normalizeCodexImportEntry(codexImportEntry{Index: index, Value: value})
	if err != nil {
		return nil, err
	}
	return &CodexImportCredentials{
		Name: item.Name, Credentials: item.Credentials,
		Warnings: item.WarningTexts, AgentIdentity: item.IsAgentIdentity,
	}, nil
}

// EnrichAccountDataCredentials 与管理员备份导入一致，只补齐缺少的 ID token 信息。
func EnrichAccountDataCredentials(platform, kind string, credentials map[string]any) map[string]any {
	cloned := make(map[string]any, len(credentials))
	for key, value := range credentials {
		cloned[key] = value
	}
	item := DataAccount{Platform: platform, Type: kind, Credentials: cloned}
	enrichCredentialsFromIDToken(&item)
	return item.Credentials
}
