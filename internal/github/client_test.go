package github

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCreatePR(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.Header.Get("Authorization") != "Bearer token" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"number":4,"html_url":"https://github.com/o/r/pull/4"}`))
	}))
	defer server.Close()
	result, err := (Client{Token: "token", Owner: "o", Repo: "r", BaseURL: server.URL}).Create(context.Background(), "lingxi/task", "diff")
	if err != nil || result.Number != 4 {
		t.Fatalf("unexpected PR: %+v %v", result, err)
	}
}
func TestCreatePRRequiresConfig(t *testing.T) {
	if _, err := (Client{}).Create(context.Background(), "branch", "diff"); err == nil {
		t.Fatal("expected config error")
	}
}

func TestCreatePRErrors(t *testing.T) {
	for _, body := range []string{`{"message":"bad"}`, `{`} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if body == `{` {
				w.WriteHeader(http.StatusOK)
			} else {
				w.WriteHeader(http.StatusBadRequest)
			}
			_, _ = w.Write([]byte(body))
		}))
		_, err := (Client{Token: "token", Owner: "o", Repo: "r", BaseURL: server.URL}).Create(context.Background(), "branch", "diff")
		server.Close()
		if err == nil {
			t.Fatalf("expected error for response %q", body)
		}
	}
	if _, err := (Client{Token: "token", Owner: "o", Repo: "r", BaseURL: "://bad"}).Create(context.Background(), "branch", "diff"); err == nil {
		t.Fatal("expected request construction error")
	}
}
