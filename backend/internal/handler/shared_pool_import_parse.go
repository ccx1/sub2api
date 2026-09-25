package handler

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode"

	"github.com/Wei-Shaw/sub2api/internal/handler/admin"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

func parseSharedImport(req sharedImportRequest) ([]sharedImportEntry, error) {
	if len([]rune(req.Defaults.Name)) > 100 {
		return nil, sharedImportInvalid("默认账号名称不能超过 100 个字符")
	}
	if len(req.Sources) == 0 || len(req.Sources) > sharedImportMaxSources {
		return nil, sharedImportInvalid("每次请选择 1 至 20 份导入内容")
	}
	totalBytes := 0
	for _, source := range req.Sources {
		totalBytes += len(source.Content)
		if totalBytes > sharedImportMaxContent {
			return nil, sharedImportInvalid("导入内容总大小不能超过 2 MiB")
		}
	}
	entries := []sharedImportEntry{}
	for sourceIndex, source := range req.Sources {
		values, err := sharedImportSourceValues(source.Content)
		if err != nil {
			return nil, sharedImportInvalid(fmt.Sprintf("第 %d 份内容格式无效，请检查 JSON 或账号导出格式", sourceIndex+1))
		}
		if len(entries)+len(values) > sharedImportMaxAccounts {
			return nil, sharedImportInvalid("每次最多导入 50 个账号")
		}
		sourceName := strings.TrimSpace(source.Name)
		if sourceName == "" {
			sourceName = fmt.Sprintf("导入内容 %d", sourceIndex+1)
		}
		if len([]rune(sourceName)) > 200 {
			sourceName = string([]rune(sourceName)[:200])
		}
		for _, value := range values {
			entries = append(entries, normalizeSharedImportEntry(value, req.Defaults, len(entries)+1, sourceName))
		}
	}
	if len(entries) == 0 {
		return nil, sharedImportInvalid("导入内容中没有账号")
	}
	nameSharedImportEntries(entries, req.Defaults.Name)
	return entries, nil
}

func nameSharedImportEntries(entries []sharedImportEntry, defaultName string) {
	defaultName = strings.TrimSpace(defaultName)
	for i := range entries {
		entry := &entries[i]
		if len([]rune(entry.input.Name)) > 100 {
			entry.item.Message = "账号名称不能超过 100 个字符"
			entry.input.Name = string([]rune(entry.input.Name)[:100])
		}
		if entry.input.Name == "" {
			entry.input.Name = defaultName
			if entry.input.Name == "" {
				entry.input.Name = "导入账号"
			}
			if len(entries) > 1 || defaultName == "" {
				suffix := fmt.Sprintf(" #%d", entry.item.Index)
				base := []rune(entry.input.Name)
				if len(base) > 100-len(suffix) {
					base = base[:100-len(suffix)]
				}
				entry.input.Name = string(base) + suffix
			}
		}
		entry.item.Name = entry.input.Name
	}
}

func sharedImportInvalid(message string) error {
	return infraerrors.BadRequest("INVALID_SHARED_IMPORT", message)
}

func sharedImportSourceValues(content string) ([]any, error) {
	content = strings.TrimSpace(strings.TrimPrefix(content, "\ufeff"))
	if content == "" {
		return nil, sharedImportInvalid("内容为空")
	}
	values, err := admin.ParseCodexImportValues(content)
	if err != nil {
		return nil, err
	}
	expanded := []any{}
	for _, value := range values {
		object, ok := value.(map[string]any)
		if !ok {
			expanded = append(expanded, value)
			continue
		}
		accounts, bundle := object["accounts"]
		if !bundle {
			if !strings.HasPrefix(content, "[") && !sharedImportAccountShape(object) && !sharedImportCodexShape(object) {
				return nil, sharedImportInvalid("不支持的账号格式")
			}
			expanded = append(expanded, value)
			continue
		}
		items, valid := accounts.([]any)
		if !valid || !sharedImportHeaderValid(object) {
			return nil, sharedImportInvalid("不支持的导入包格式")
		}
		expanded = append(expanded, items...)
		if len(expanded) > sharedImportMaxAccounts {
			return nil, sharedImportInvalid("账号数量超限")
		}
	}
	return expanded, nil
}

func sharedImportHeaderValid(object map[string]any) bool {
	if v, exists := object["type"]; exists && v != "" && v != "sub2api-data" && v != "sub2api-bundle" {
		return false
	}
	if v, exists := object["version"]; exists {
		n, valid := v.(json.Number)
		if !valid || (n.String() != "0" && n.String() != "1") {
			return false
		}
	}
	return true
}

func sharedImportAccountShape(object map[string]any) bool {
	for _, key := range []string{"credentials", "platform", "group_ids", "proxy_id", "owner_user_id", "priority", "rate_multiplier", "extra", "type", "status"} {
		if _, exists := object[key]; exists {
			return true
		}
	}
	return false
}

func sharedImportCodexShape(object map[string]any) bool {
	for _, key := range []string{"tokens", "access_token", "accessToken", "token", "agent_identity", "agentIdentity"} {
		if _, exists := object[key]; exists {
			return true
		}
	}
	return false
}

func normalizeSharedImportEntry(value any, defaults sharedImportDefaults, index int, source string) sharedImportEntry {
	entry := sharedImportEntry{item: sharedImportItem{Index: index, Source: source}, input: service.SharedPoolAccountInput{
		Platform: defaults.Platform, Type: defaults.Type, Concurrency: defaults.Concurrency,
		ProxyURL: defaults.ProxyURL, Enabled: defaults.Enabled, DispatchConsent: defaults.DispatchConsent, ProtectionEnabled: defaults.ProtectionEnabled,
	}}
	if defaults.DailyCooldown != nil {
		cooldown := *defaults.DailyCooldown
		entry.input.DailyCooldown = &cooldown
	}
	object, objectValue := value.(map[string]any)
	_, hasCredentials := object["credentials"]
	if objectValue && (hasCredentials || !sharedImportCodexShape(object)) {
		normalizeSharedDataAccount(&entry, object)
	} else {
		normalizeSharedCodexAccount(&entry, value)
	}
	if entry.input.Platform == service.PlatformOpenAI && entry.input.Type == service.AccountTypeOAuth {
		entry.input.CodexTicketEnabled = defaults.CodexTicketEnabled
		entry.input.ExcelBPSEnabled = defaults.ExcelBPSEnabled
	}
	for i, warning := range entry.warnings {
		entry.warnings[i] = fmt.Sprintf("第 %d 个账号：%s", index, warning)
	}
	return entry
}

func normalizeSharedDataAccount(entry *sharedImportEntry, object map[string]any) {
	for key, target := range map[string]*string{"name": &entry.input.Name, "platform": &entry.input.Platform, "type": &entry.input.Type} {
		if value, present := object[key]; present {
			text, valid := value.(string)
			if !valid {
				entry.item.Message = "账号名称、平台和认证类型必须为字符串"
				return
			}
			if text = strings.TrimSpace(text); text != "" {
				*target = text
			}
		}
	}
	credentials, valid := object["credentials"].(map[string]any)
	if !valid || len(credentials) == 0 {
		entry.item.Message = "缺少有效的账号 credentials"
		return
	}
	entry.input.Credentials = admin.EnrichAccountDataCredentials(entry.input.Platform, entry.input.Type, credentials)
}

func normalizeSharedCodexAccount(entry *sharedImportEntry, value any) {
	if object, ok := value.(map[string]any); ok && !sharedImportCodexMarkersValid(object) {
		entry.item.Message = "Codex 凭证仅支持 OpenAI OAuth，请检查账号平台和认证类型"
		return
	}
	if raw, ok := value.(string); ok {
		value = strings.TrimSpace(raw)
		if len(raw) > 32768 || strings.IndexFunc(strings.TrimSpace(raw), unicode.IsSpace) >= 0 {
			entry.item.Message = "access_token 格式无效"
			return
		}
	}
	parsed, err := admin.NormalizeCodexImportCredentials(value, entry.item.Index)
	if err != nil {
		entry.item.Message = err.Error()
		return
	}
	if parsed.AgentIdentity {
		entry.item.Message = "共享池暂不支持 Agent Identity，请导入 OAuth 凭证"
		return
	}
	entry.input.Platform = service.PlatformOpenAI
	entry.input.Type = service.AccountTypeOAuth
	entry.input.Credentials = parsed.Credentials
	entry.warnings = parsed.Warnings
	if object, ok := value.(map[string]any); ok {
		if name, ok := object["name"].(string); ok && strings.TrimSpace(name) != "" {
			entry.input.Name = strings.TrimSpace(name)
		}
		if user, ok := object["user"].(map[string]any); ok && entry.input.Name == "" {
			if name, ok := user["name"].(string); ok {
				entry.input.Name = strings.TrimSpace(name)
			}
		}
	}
}

func sharedImportCodexMarkersValid(object map[string]any) bool {
	for key, allowed := range map[string][]string{
		"platform": {"", service.PlatformOpenAI},
		"type":     {"", service.AccountTypeOAuth, "codex", "chatgpt"},
	} {
		value, exists := object[key]
		if !exists {
			continue
		}
		marker, valid := value.(string)
		if !valid {
			return false
		}
		marker = strings.ToLower(strings.TrimSpace(marker))
		matched := false
		for _, candidate := range allowed {
			matched = matched || marker == candidate
		}
		if !matched {
			return false
		}
	}
	return true
}
