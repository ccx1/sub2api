package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/service"
	_ "github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

func codexTicketPoolPostgresFixture(t *testing.T) (*accountRepository, *sql.DB) {
	t.Helper()
	dsn := os.Getenv("SUB2API_CODEX_TICKET_POOL_TEST_DSN")
	if dsn == "" {
		t.Skip("SUB2API_CODEX_TICKET_POOL_TEST_DSN is not set")
	}
	parsed, err := url.Parse(dsn)
	require.NoError(t, err)
	require.Equal(t, "127.0.0.1", parsed.Hostname())
	require.Equal(t, "/codex_ticket_pool_test", parsed.Path)
	db, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	_, err = db.Exec(`CREATE TEMP TABLE accounts (
 id bigint PRIMARY KEY, platform text, type text, extra jsonb, credentials jsonb,
 proxy_id bigint, status text DEFAULT 'active', schedulable boolean DEFAULT true, auto_pause_on_expired boolean DEFAULT false,
 expires_at timestamptz, updated_at timestamptz, deleted_at timestamptz)`)
	require.NoError(t, err)
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	return newAccountRepositoryWithSQL(client, db, nil), db
}

func saveCodexTicketPoolFixture(t *testing.T, db *sql.DB, account *service.Account) {
	t.Helper()
	extra, err := json.Marshal(account.Extra)
	require.NoError(t, err)
	credentials, err := json.Marshal(account.Credentials)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO accounts(id, platform, type, extra, credentials) VALUES ($1,$2,$3,$4,$5)
 ON CONFLICT (id) DO UPDATE SET extra = EXCLUDED.extra`, account.ID, account.Platform, account.Type, string(extra), string(credentials))
	require.NoError(t, err)
}

func readCodexTicketPoolFixture(t *testing.T, db *sql.DB) map[string]any {
	t.Helper()
	var raw []byte
	require.NoError(t, db.QueryRow(`SELECT extra FROM accounts WHERE id = $1`, 41).Scan(&raw))
	var result map[string]any
	require.NoError(t, json.Unmarshal(raw, &result))
	return result
}

func codexTicketPoolSlot(extra map[string]any, index int) map[string]any {
	root := extra["codex_turn_ticket:model"].(map[string]any)
	if index == 0 {
		return root
	}
	if index == 1 {
		return root["standby"].(map[string]any)
	}
	return root["reserve"].([]any)[index-2].(map[string]any)
}

func codexTicketPoolRevocation(used map[string]any, diagnostic bool) map[string]any {
	result := map[string]any{"state": used["state"], "captured_at": used["captured_at"],
		"model": used["model"], "attempt_id": used["attempt_id"], "revoked": true}
	if diagnostic {
		result["invalidation"] = map[string]any{"model": used["model"], "attempt_id": used["attempt_id"],
			"captured_at": used["captured_at"], "invalidated_at": "2026-09-21T00:00:00Z",
			"reason": "response_ticket_rejected", "source": "http", "state": "sensitive-ticket",
			"authorization": "sensitive-auth", "body": "sensitive-body"}
	}
	return result
}

func TestCodexTicketPoolPostgresRevokesOnlySentSlotAndRecordsOnce(t *testing.T) {
	for index := range 5 {
		for _, diagnostic := range []bool{false, true} {
			t.Run(fmt.Sprintf("slot-%d/diagnostic-%t", index, diagnostic), func(t *testing.T) {
				repo, db := codexTicketPoolPostgresFixture(t)
				account, tickets := codexTicketPoolFixture()
				account.Extra["unrelated"] = "preserved"
				saveCodexTicketPoolFixture(t, db, account)
				expected := readCodexTicketPoolFixture(t, db)
				used := codexTicketPoolRevocation(tickets[index], diagnostic)
				changed, err := repo.CompareAndSwapCodexTicket(context.Background(), account, "model", used)
				require.NoError(t, err)
				require.True(t, changed)
				codexTicketPoolSlot(expected, index)["revoked"] = true
				if diagnostic {
					request, err := prepareCodexTicketCAS(account, "model", used)
					require.NoError(t, err)
					var event map[string]any
					require.NoError(t, json.Unmarshal([]byte(request.args[6].(string)), &event))
					codexTicketPoolSlot(expected, index)["invalidation"] = event
					expected[service.OpenAICodexTicketInvalidationsKey] = []any{event}
				}
				got := readCodexTicketPoolFixture(t, db)
				require.Equal(t, expected, got)
				encoded, err := json.Marshal(got)
				require.NoError(t, err)
				for _, secret := range []string{"sensitive-ticket", "sensitive-auth", "sensitive-body"} {
					require.NotContains(t, string(encoded), secret)
				}
				changed, err = repo.CompareAndSwapCodexTicket(context.Background(), account, "model", used)
				require.NoError(t, err)
				require.True(t, changed)
				require.Equal(t, got, readCodexTicketPoolFixture(t, db), "重复撤票不重复追加事件")
			})
		}
	}
}

func TestCodexTicketPoolPostgresLateResponseFollowsIdentityAfterReorder(t *testing.T) {
	for _, change := range []string{"reorder", "new-state", "new-capture"} {
		t.Run(change, func(t *testing.T) {
			repo, db := codexTicketPoolPostgresFixture(t)
			account, tickets := codexTicketPoolFixture()
			used := codexTicketPoolRevocation(tickets[3], true)
			persisted, current := codexTicketPoolFixture()
			root := persisted.Extra["codex_turn_ticket:model"].(map[string]any)
			switch change {
			case "reorder":
				root["reserve"] = []any{current[4], current[2], current[3]}
			case "new-state":
				current[3]["state"] = "newly-harvested"
			case "new-capture":
				current[3]["captured_at"] = "2026-09-22T03:00:00Z"
			}
			saveCodexTicketPoolFixture(t, db, persisted)
			before := readCodexTicketPoolFixture(t, db)
			changed, err := repo.CompareAndSwapCodexTicket(context.Background(), account, "model", used)
			require.NoError(t, err)
			require.Equal(t, change == "reorder", changed)
			after := readCodexTicketPoolFixture(t, db)
			if change != "reorder" {
				require.Equal(t, before, after, "旧回调不能撤新票或追加诊断")
				return
			}
			require.Equal(t, true, codexTicketPoolSlot(after, 4)["revoked"])
			for _, index := range []int{0, 1, 2, 3} {
				require.NotContains(t, codexTicketPoolSlot(after, index), "revoked")
			}
		})
	}
}

func TestCodexTicketPoolPostgresPublicationRejectsConcurrentReserveChange(t *testing.T) {
	repo, db := codexTicketPoolPostgresFixture(t)
	snapshot, _ := codexTicketPoolFixture()
	persisted, tickets := codexTicketPoolFixture()
	tickets[4]["state"] = "concurrent-new-ticket"
	saveCodexTicketPoolFixture(t, db, persisted)
	before := readCodexTicketPoolFixture(t, db)
	changed, err := repo.CompareAndSwapCodexTicket(context.Background(), snapshot, "model", snapshot.Extra["codex_turn_ticket:model"])
	require.NoError(t, err)
	require.False(t, changed)
	require.Equal(t, before, readCodexTicketPoolFixture(t, db))
}

func TestCodexTicketPoolPostgresLegacyAndMalformedReserve(t *testing.T) {
	for _, reserve := range []any{nil, "malformed", map[string]any{}, []any{}, "missing"} {
		for _, index := range []int{0, 1, 3} {
			t.Run(fmt.Sprintf("reserve-%v/slot-%d", reserve, index), func(t *testing.T) {
				repo, db := codexTicketPoolPostgresFixture(t)
				account, tickets := codexTicketPoolFixture()
				persisted, _ := codexTicketPoolFixture()
				root := persisted.Extra["codex_turn_ticket:model"].(map[string]any)
				root["reserve"] = reserve
				if reserve == "missing" {
					delete(root, "reserve")
				}
				saveCodexTicketPoolFixture(t, db, persisted)
				expected := readCodexTicketPoolFixture(t, db)
				changed, err := repo.CompareAndSwapCodexTicket(context.Background(), account, "model", codexTicketPoolRevocation(tickets[index], true))
				require.NoError(t, err)
				require.Equal(t, index < 2, changed)
				got := readCodexTicketPoolFixture(t, db)
				if index >= 2 {
					require.Equal(t, expected, got, "已丢失备用池的旧回调不得影响主备票")
					return
				}
				require.Equal(t, true, codexTicketPoolSlot(got, index)["revoked"])
			})
		}
	}
}

func TestCodexTicketPoolPostgresSameStateRequiresMatchingCapture(t *testing.T) {
	repo, db := codexTicketPoolPostgresFixture(t)
	account, tickets := codexTicketPoolFixture()
	for _, ticket := range tickets {
		ticket["state"] = "same-state"
	}
	account.Extra["codex_turn_ticket:model"].(map[string]any)["state"] = "same-state"
	saveCodexTicketPoolFixture(t, db, account)
	changed, err := repo.CompareAndSwapCodexTicket(context.Background(), account, "model", codexTicketPoolRevocation(tickets[3], true))
	require.NoError(t, err)
	require.True(t, changed)
	got := readCodexTicketPoolFixture(t, db)
	for index := range tickets {
		if index == 3 {
			require.Equal(t, true, codexTicketPoolSlot(got, index)["revoked"])
			continue
		}
		require.NotContains(t, codexTicketPoolSlot(got, index), "revoked")
	}
}

func TestCodexTicketPoolPostgresPreservesFirstInvalidation(t *testing.T) {
	repo, db := codexTicketPoolPostgresFixture(t)
	account, tickets := codexTicketPoolFixture()
	saveCodexTicketPoolFixture(t, db, account)
	used := codexTicketPoolRevocation(tickets[4], true)
	_, err := repo.CompareAndSwapCodexTicket(context.Background(), account, "model", used)
	require.NoError(t, err)
	before := readCodexTicketPoolFixture(t, db)
	event := used["invalidation"].(map[string]any)
	event["reason"] = "response_model_mismatch"
	event["invalidated_at"] = time.Now().UTC().Format(time.RFC3339Nano)
	_, err = repo.CompareAndSwapCodexTicket(context.Background(), account, "model", used)
	require.NoError(t, err)
	require.Equal(t, before, readCodexTicketPoolFixture(t, db))
}
