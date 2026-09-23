package repository

import (
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
)

func codexTicketRevocationHasCredentials(ticket codexTicketRevocationIdentity) bool {
	if ticket.State != "" {
		return true
	}
	if ticket.CredentialMode != config.CodexTicketCredentialCookie {
		return false
	}
	for _, cookie := range ticket.Cookies {
		if cookie != nil && cookie.Name != "" && cookie.Value != "" {
			return true
		}
	}
	return false
}

func matchCodexTicketRevocationIdentity(used, inventory codexTicketRevocationIdentity) (codexTicketRevocationIdentity, bool) {
	captured, err := time.Parse(time.RFC3339Nano, used.CapturedAt)
	if err != nil || captured.IsZero() || !codexTicketRevocationHasCredentials(used) {
		return codexTicketRevocationIdentity{}, false
	}
	candidates := append([]*codexTicketRevocationIdentity{&inventory, inventory.Standby}, inventory.Reserve...)
	for _, candidate := range candidates {
		if candidate == nil || candidate.State != used.State || !codexTicketRevocationHasCredentials(*candidate) {
			continue
		}
		at, err := time.Parse(time.RFC3339Nano, candidate.CapturedAt)
		if err == nil && captured.Equal(at) {
			return *candidate, true
		}
	}
	return codexTicketRevocationIdentity{}, false
}

// 按实际发送版本定位槽，迟到的主票反馈不能覆盖同时采得的备用票。
const codexTicketPrimaryMatchesSQL = `(extra -> $1 ->> 'state') = $5
 AND (extra -> $1 ->> 'captured_at') = $6`

const codexTicketStandbyMatchesSQL = `(extra -> $1 -> 'standby' ->> 'state') = $5
 AND (extra -> $1 -> 'standby' ->> 'captured_at') = $6`

const codexTicketReserveArraySQL = `(CASE WHEN jsonb_typeof(extra -> $1 -> 'reserve') = 'array'
 THEN extra -> $1 -> 'reserve' ELSE '[]'::jsonb END)`

const codexTicketReserveItemMatchesSQL = `(ticket ->> 'state') = $5 AND (ticket ->> 'captured_at') = $6`

const codexTicketReserveMatchesSQL = `EXISTS (
 SELECT 1 FROM jsonb_array_elements(` + codexTicketReserveArraySQL + `) AS reserve(ticket)
 WHERE ` + codexTicketReserveItemMatchesSQL + `)`

const codexTicketSentSlotSQL = `(CASE WHEN ` + codexTicketPrimaryMatchesSQL + `
 THEN extra -> $1 WHEN ` + codexTicketStandbyMatchesSQL + ` THEN extra -> $1 -> 'standby'
 ELSE (SELECT ticket FROM jsonb_array_elements(` + codexTicketReserveArraySQL + `) AS reserve(ticket)
  WHERE ` + codexTicketReserveItemMatchesSQL + ` LIMIT 1) END)`

// 始终基于 UPDATE 当时的库存定位票据，不能用发送时的数组下标覆盖新票。
func codexTicketUpdatedInventorySQL(patch string) string {
	return `CASE WHEN ` + codexTicketPrimaryMatchesSQL + `
 THEN (extra -> $1) || ` + patch + `
 WHEN ` + codexTicketStandbyMatchesSQL + `
 THEN jsonb_set(extra -> $1, '{standby}', (extra -> $1 -> 'standby') || ` + patch + `)
 ELSE jsonb_set(extra -> $1, '{reserve}', (
  SELECT jsonb_agg(CASE WHEN ` + codexTicketReserveItemMatchesSQL + ` THEN ticket || ` + patch + `
   ELSE ticket END ORDER BY position)
  FROM jsonb_array_elements(` + codexTicketReserveArraySQL + `) WITH ORDINALITY AS reserve(ticket, position)
 )) END`
}
