package repository

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

type codexTicketRevocationIdentity struct {
	CredentialMode string                           `json:"credential_mode,omitempty"`
	Cookies        []*http.Cookie                   `json:"cookies,omitempty"`
	State          string                           `json:"state"`
	CapturedAt     string                           `json:"captured_at"`
	Model          json.RawMessage                  `json:"model"`
	AttemptID      json.RawMessage                  `json:"attempt_id"`
	Invalidation   json.RawMessage                  `json:"invalidation"`
	Standby        *codexTicketRevocationIdentity   `json:"standby,omitempty"`
	Reserve        []*codexTicketRevocationIdentity `json:"reserve,omitempty"`
}

func prepareCodexTicketInvalidation(key string, used, expected codexTicketRevocationIdentity) ([]byte, error) {
	var event service.CodexTicketInvalidation
	if err := json.Unmarshal(used.Invalidation, &event); err != nil {
		return nil, err
	}
	model := strings.TrimPrefix(key, codexTicketCASKeyPrefix)
	captured, err := time.Parse(time.RFC3339Nano, expected.CapturedAt)
	fields, fieldsErr := decodeCodexTicketInvalidationIdentity(used, expected)
	if err != nil || fieldsErr != nil || event.Model != model || (fields[0] != "" && fields[0] != model) ||
		(fields[1] != "" && fields[1] != model) || event.AttemptID != fields[3] ||
		fields[2] != fields[3] || len(event.AttemptID) > 128 ||
		!event.CapturedAt.Equal(captured) || event.InvalidatedAt.IsZero() {
		return nil, errors.New("codex ticket invalidation does not match the sent ticket")
	}
	switch event.Reason {
	case "response_model_mismatch", "response_ticket_rejected", "cookie_changed":
	default:
		event.Reason = "unknown"
	}
	switch event.Source {
	case "http", "websocket", "websocket_handshake", "websocket_prewarm":
	default:
		event.Source = "unknown"
	}
	event.ReportedModels = sanitizeCodexTicketInvalidationModels(event.ReportedModels)
	if event.ReturnedTicketLength != nil && *event.ReturnedTicketLength < 0 {
		event.ReturnedTicketLength = nil
	}
	// 仅序列化诊断类型，replacement 中的 state、认证字段和正文不能进入历史。
	return json.Marshal(event)
}

func decodeCodexTicketInvalidationIdentity(used, expected codexTicketRevocationIdentity) ([4]string, error) {
	var result [4]string
	for index, raw := range []json.RawMessage{used.Model, expected.Model, used.AttemptID, expected.AttemptID} {
		if len(raw) == 0 {
			continue
		}
		if err := json.Unmarshal(raw, &result[index]); err != nil {
			return result, err
		}
	}
	return result, nil
}

func sanitizeCodexTicketInvalidationModels(models []string) []string {
	var result []string
	seen := make(map[string]bool)
	for _, model := range models {
		model = strings.TrimSpace(model)
		if len(model) > 256 {
			model = model[:256]
			for !utf8.ValidString(model) {
				model = model[:len(model)-1]
			}
		}
		if model == "" || seen[model] {
			continue
		}
		seen[model] = true
		result = append(result, model)
		if len(result) == 8 {
			break
		}
	}
	return result
}

// 同一行 UPDATE 同时撤票并追加诊断；重试保留首次原因，旧票回调不能影响新票。
// 独立账本不依赖成功记录先写入，并且在模型快照被新票替换后仍保留。
var codexTicketRevocationWithInvalidationSQL = `UPDATE accounts
SET extra = CASE WHEN (` + codexTicketSentSlotSQL + ` ->> 'revoked') = 'true' THEN extra
 ELSE COALESCE(extra, '{}'::jsonb) || jsonb_build_object(
  $1::text, ` + codexTicketUpdatedInventorySQL(`jsonb_build_object('revoked', true, 'invalidation', $7::jsonb)`) + `,
  $8::text, (
   SELECT jsonb_agg(item ORDER BY position) FROM (
    SELECT item, position FROM jsonb_array_elements(
     jsonb_build_array($7::jsonb) || CASE WHEN jsonb_typeof(extra -> $8) = 'array'
      THEN extra -> $8 ELSE '[]'::jsonb END
    ) WITH ORDINALITY AS events(item, position)
    ORDER BY position LIMIT $9
   ) AS retained
  )
 ) END, updated_at = NOW()
WHERE id = $2 AND platform = $3 AND type = $4
 AND (` + codexTicketPrimaryMatchesSQL + ` OR ` + codexTicketStandbyMatchesSQL + ` OR ` + codexTicketReserveMatchesSQL + `)
 AND deleted_at IS NULL`
