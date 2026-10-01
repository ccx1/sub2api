package service

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

type guardConfigSettings struct {
	SettingRepository
	value string
	err   error
}

func (r *guardConfigSettings) GetValue(context.Context, string) (string, error) {
	return r.value, nil
}

func (r *guardConfigSettings) Set(_ context.Context, _, value string) error {
	if r.err != nil {
		return r.err
	}
	r.value = value
	return nil
}

func newGuardModelProbeServer(t *testing.T, models chan<- string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			Model string `json:"model"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Error(err)
		}
		select {
		case models <- payload.Model:
		default:
		}
		_, _ = io.WriteString(w, `{"status":"active"}`)
	}))
}

func TestAccountTokenGuardUsesLastSuccessfullySavedConfiguration(t *testing.T) {
	models := make(chan string, 1)
	server := newGuardModelProbeServer(t, models)
	defer server.Close()
	svc := newGuardTestService(&guardMemoryRepo{}, &guardMemoryAccounts{items: []Account{guardTestAccount(1)}}, server.URL)
	defer svc.Stop()
	settings := &guardConfigSettings{}
	svc.settings = settings
	cfg := svc.currentConfig()
	cfg.ProbeModel = "saved-model"
	if _, err := svc.SaveConfig(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
	cfg.ProbeModel, settings.err = "unsaved-model", errors.New("test settings write failure")
	if _, err := svc.SaveConfig(context.Background(), cfg); err == nil {
		t.Fatal("failed settings write reported success")
	}
	if svc.currentConfig().ProbeModel != "saved-model" {
		t.Fatal("failed save changed runtime configuration")
	}
	job, err := svc.StartRun(true)
	if err != nil {
		t.Fatal(err)
	}
	if finished := awaitGuardJob(t, svc, job.ID); finished.Status != AccountTokenGuardJobSucceeded {
		t.Fatalf("saved configuration job failed: %+v", finished)
	}
	select {
	case model := <-models:
		if model != "saved-model" {
			t.Fatalf("job used unsaved model: %q", model)
		}
	default:
		t.Fatal("job completed without using saved probe endpoint")
	}
}
