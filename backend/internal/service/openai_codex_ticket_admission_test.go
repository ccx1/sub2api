package service

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestCodexTicketAdmissionModesHonorFailClosed(t *testing.T) {
	for _, mode := range []string{"state", "cookie", "cookie_state"} {
		for _, override := range []bool{false, true} {
			for _, closed := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/override=%t/closed=%t", mode, override, closed), func(t *testing.T) {
					cfg := config.OpenAICodexTicketConfig{Enabled: true, CredentialMode: mode, FailClosed: closed}
					account := ticketTestAccount(41)
					if override {
						cfg.CredentialMode = "state"
						account.Extra = map[string]any{CodexTicketCredentialPolicyExtraKey: map[string]any{"mode": mode}}
					}
					s := ticketTestService(t, cfg, nil)
					require.Equal(t, closed, s.openAICodexTicketBlocksAccount(account, "gpt-6-astra"))
					require.False(t, s.openAICodexTicketBlocksAccount(account, "gpt-5.5"))
					status := OpenAICodexTicketStatuses(account, cfg, time.Now())[0]
					require.False(t, status.Ready)
					require.Equal(t, closed, status.Blocked)
					req, err := http.NewRequest(http.MethodPost, chatgptCodexURL, nil)
					require.NoError(t, err)
					err = s.applyOpenAICodexTicketRequest(account, "gpt-6-astra", req)
					if closed {
						require.ErrorIs(t, err, ErrOpenAICodexTicketUnavailable)
					} else {
						require.NoError(t, err)
					}
				})
			}
		}
	}
}

func TestCodexTicketAdmissionHTTPBuildersRejectMissingCredentials(t *testing.T) {
	for _, mode := range []string{"state", "cookie", "cookie_state"} {
		t.Run(mode, func(t *testing.T) {
			s := ticketTestService(t, config.OpenAICodexTicketConfig{Enabled: true, CredentialMode: mode, FailClosed: true}, nil)
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
			account, body := ticketTestAccount(41), []byte(`{"model":"gpt-6-astra","input":"hello"}`)
			req, err := s.buildUpstreamRequest(context.Background(), c, account, body, "test-token", true, "", true)
			require.ErrorIs(t, err, ErrOpenAICodexTicketUnavailable)
			require.Nil(t, req)
			req, err = s.buildUpstreamRequestOpenAIPassthrough(context.Background(), c, account, body, "test-token")
			require.ErrorIs(t, err, ErrOpenAICodexTicketUnavailable)
			require.Nil(t, req)
		})
	}
}

func TestCodexTicketAdmissionCookieLifecycle(t *testing.T) {
	for _, mode := range []string{"cookie", "cookie_state"} {
		for _, invalid := range []string{"expired", "revoked"} {
			t.Run(mode+"/"+invalid, func(t *testing.T) {
				s, account, first := codexCookiePendingEntryFixture(t, mode)
				receipt := first.Context().Value(openAICodexTicketReceiptKey{}).(*openAICodexTicketReceipt)
				require.False(t, s.openAICodexTicketBlocksAccount(account, "gpt-6-astra"))
				if invalid == "revoked" {
					s.invalidateOpenAICodexTicket(context.Background(), account, &receipt.ticket)
				} else {
					ticket := receipt.ticket
					ticket.ExpiresAt = time.Now().Add(-time.Second)
					s.openaiCodexTickets.Store(openAICodexTicketKey(account.ID, ticket.Model), &ticket)
				}
				require.True(t, s.openAICodexTicketBlocksAccount(account, "gpt-6-astra"))
				next, err := http.NewRequest(http.MethodPost, chatgptCodexURL, nil)
				require.NoError(t, err)
				require.ErrorIs(t, s.applyOpenAICodexTicketRequest(account, "gpt-6-astra", next), ErrOpenAICodexTicketUnavailable)
				require.Empty(t, next.Header.Get("Cookie"))
			})
		}
	}
}

func TestCodexTicketAdmissionRejectsInvalidatedHTTPBeforeSend(t *testing.T) {
	for _, mode := range []string{"cookie", "cookie_state"} {
		for _, invalid := range []string{"expired", "revoked", "valid"} {
			t.Run(mode+"/"+invalid, func(t *testing.T) {
				s, account, req := codexCookiePendingEntryFixture(t, mode)
				receipt := req.Context().Value(openAICodexTicketReceiptKey{}).(*openAICodexTicketReceipt)
				if invalid == "expired" {
					receipt.ticket.ExpiresAt = time.Now().Add(-time.Second)
				} else if invalid == "revoked" {
					s.invalidateOpenAICodexTicket(context.Background(), account, &receipt.ticket)
				}
				calls := 0
				s.httpUpstream = &codexTicketFuncUpstream{do: func(request *http.Request) (*http.Response, error) {
					calls++
					return nil, io.EOF
				}}
				_, err := s.doOpenAIUpstream(req, "", account)
				if invalid == "valid" {
					require.ErrorIs(t, err, io.EOF)
					require.Equal(t, 1, calls)
				} else {
					require.ErrorIs(t, err, ErrOpenAICodexTicketUnavailable)
					require.Zero(t, calls, "失效票据不得发送到上游")
				}
				require.NoError(t, s.checkCodexTicketBusinessIdle(context.Background(), account))
			})
		}
	}
}
