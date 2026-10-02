package handler

import (
	"encoding/json"
	"fmt"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

// 仅从账号导出的 extra 中读取 BPS 配置，不传入其它管理员字段。
func applySharedImportFileBPS(input *service.SharedPoolAccountInput, object map[string]any) error {
	extra, ok := object["extra"].(map[string]any)
	if !ok {
		return nil
	}
	fields := []struct{ source, option string }{
		{"openai_excel_bps_models", "models"},
		{"openai_excel_bps_auto_disable_on_403", "auto_disable_on_403"},
		{"openai_excel_bps_cache_creation_as_input", "cache_creation_as_input"},
		{service.ExcelBPSIgnoreImagesKey, "ignore_images"},
		{service.ExcelBPSIgnoreEncryptedContentKey, "ignore_encrypted_content"},
		{service.ExcelBPSOmitUnsupportedToolsKey, "omit_unsupported_tools"},
		{service.ExcelBPSAutoMoveOn403Key, "auto_move_on_403"},
		{service.ExcelBPS403TargetGroupIDKey, "target_group_id"},
	}
	values := make(map[string]any)
	for _, field := range fields {
		if value, exists := extra[field.source]; exists {
			if value == nil {
				return fmt.Errorf("%s is null", field.source)
			}
			values[field.option] = value
		}
	}
	rawEnabled, hasEnabled := extra["openai_excel_bps"]
	if !hasEnabled && len(values) == 0 {
		return nil
	}
	enabled := false
	if hasEnabled {
		var valid bool
		enabled, valid = rawEnabled.(bool)
		if !valid {
			return fmt.Errorf("openai_excel_bps must be a boolean")
		}
	}
	var options service.ExcelBPSOptions
	encoded, err := json.Marshal(values)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(encoded, &options); err != nil {
		return err
	}
	if options.AutoMoveOn403 || options.TargetGroupID != nil {
		return fmt.Errorf("shared pool BPS group action is admin only")
	}
	input.ExcelBPSEnabled = &enabled
	input.ExcelBPSOptions = nil
	if enabled && len(values) > 0 {
		input.ExcelBPSOptions = &options
	}
	return nil
}
