package handler

import "encoding/json"

// 限制实际 JSON 字节数，避免控制字符/HTML 转义放大批次结果而超过幂等记录的 64 KiB 上限。
func boundSharedImportResult(result *sharedImportResult) {
	for i := range result.Items {
		item := &result.Items[i]
		item.Name = sharedImportDisplayText(item.Name, 100, 400)
		item.Source = sharedImportDisplayText(item.Source, 200, 256)
		item.Message = sharedImportDisplayText(item.Message, 100, 256)
	}
	warnings := make([]string, 0, len(result.Warnings))
	warningBytes := 2
	for _, warning := range result.Warnings {
		warning = sharedImportDisplayText(warning, 100, 256)
		encoded, _ := json.Marshal(warning)
		if warningBytes+len(encoded)+1 > 4<<10 {
			break
		}
		warnings = append(warnings, warning)
		warningBytes += len(encoded) + 1
	}
	result.Warnings = warnings
}

func sharedImportDisplayText(value string, maxRunes, maxJSONBytes int) string {
	runes := []rune(value)
	if len(runes) > maxRunes {
		runes = runes[:maxRunes]
	}
	for len(runes) > 0 {
		value = string(runes)
		encoded, _ := json.Marshal(value)
		if len(encoded) <= maxJSONBytes {
			return value
		}
		runes = runes[:len(runes)-1]
	}
	return ""
}
