package autoresearch

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestNewHTTPEngramClient_Defaults(t *testing.T) {
	c := NewHTTPEngramClient(EngramClientConfig{})
	if c.baseURL != defaultEngramURL {
		t.Errorf("baseURL = %q, want %q", c.baseURL, defaultEngramURL)
	}
	if c.appID != defaultAppID {
		t.Errorf("appID = %q, want %q", c.appID, defaultAppID)
	}
	if c.userID != defaultUserID {
		t.Errorf("userID = %q, want %q", c.userID, defaultUserID)
	}
}

func TestNewHTTPEngramClient_Custom(t *testing.T) {
	c := NewHTTPEngramClient(EngramClientConfig{
		BaseURL: "http://custom:9999",
		AppID:   "test-app",
		UserID:  "test-user",
		Timeout: 5 * time.Second,
	})
	if c.baseURL != "http://custom:9999" {
		t.Errorf("baseURL = %q, want http://custom:9999", c.baseURL)
	}
	if c.appID != "test-app" {
		t.Errorf("appID = %q, want test-app", c.appID)
	}
	if c.userID != "test-user" {
		t.Errorf("userID = %q, want test-user", c.userID)
	}
}

func TestAddExperimentResult(t *testing.T) {
	var received map[string]interface{}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/v1/memories/" {
			t.Errorf("path = %q, want /v1/memories/", r.URL.Path)
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("content-type = %q, want application/json", ct)
		}

		if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
			t.Fatalf("decode body: %v", err)
		}

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id": "mem-123"}`))
	}))
	defer srv.Close()

	client := NewHTTPEngramClient(EngramClientConfig{BaseURL: srv.URL})
	result := ExperimentResult{
		ID:         "exp-001",
		Name:       "lr-sweep",
		Hypothesis: "lower lr improves convergence",
		Status:     StatusCompleted,
		Phase:      PhaseEvaluation,
		Timestamp:  time.Now(),
	}

	err := client.AddExperimentResult(context.Background(), result)
	if err != nil {
		t.Fatalf("AddExperimentResult: %v", err)
	}

	if received["user_id"] != "nfsarch33" {
		t.Errorf("user_id = %v, want nfsarch33", received["user_id"])
	}

	meta, ok := received["metadata"].(map[string]interface{})
	if !ok {
		t.Fatal("metadata not a map")
	}
	if meta["experiment_id"] != "exp-001" {
		t.Errorf("experiment_id = %v, want exp-001", meta["experiment_id"])
	}
	if meta["app_id"] != "autoresearch" {
		t.Errorf("app_id = %v, want autoresearch", meta["app_id"])
	}
}

func TestAddExperimentResult_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error": "db down"}`))
	}))
	defer srv.Close()

	client := NewHTTPEngramClient(EngramClientConfig{BaseURL: srv.URL})
	err := client.AddExperimentResult(context.Background(), ExperimentResult{ID: "fail"})
	if err == nil {
		t.Fatal("expected error on 500 response")
	}
}

func TestSearchRelatedExperiments(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/memories/search/" {
			t.Errorf("path = %q, want /v1/memories/search/", r.URL.Path)
		}

		var body map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if body["query"] != "learning rate" {
			t.Errorf("query = %v, want 'learning rate'", body["query"])
		}
		if body["limit"] != float64(5) {
			t.Errorf("limit = %v, want 5", body["limit"])
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"results": [{"id": "m1", "memory": "lr sweep result"}, {"id": "m2", "memory": "lr comparison"}]}`))
	}))
	defer srv.Close()

	client := NewHTTPEngramClient(EngramClientConfig{BaseURL: srv.URL})
	results, err := client.SearchRelatedExperiments(context.Background(), "learning rate", 5)
	if err != nil {
		t.Fatalf("SearchRelatedExperiments: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("len(results) = %d, want 2", len(results))
	}
	if results[0].ID != "m1" {
		t.Errorf("results[0].ID = %q, want m1", results[0].ID)
	}
}

func TestSearchRelatedExperiments_DefaultLimit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]interface{}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["limit"] != float64(10) {
			t.Errorf("default limit = %v, want 10", body["limit"])
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"results": []}`))
	}))
	defer srv.Close()

	client := NewHTTPEngramClient(EngramClientConfig{BaseURL: srv.URL})
	_, err := client.SearchRelatedExperiments(context.Background(), "test", 0)
	if err != nil {
		t.Fatalf("SearchRelatedExperiments: %v", err)
	}
}

func TestGetExperimentHistory(t *testing.T) {
	var receivedQuery string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]interface{}
		_ = json.NewDecoder(r.Body).Decode(&body)
		receivedQuery, _ = body["query"].(string)

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"results": [{"id": "h1", "memory": "phase 1"}, {"id": "h2", "memory": "phase 2"}]}`))
	}))
	defer srv.Close()

	client := NewHTTPEngramClient(EngramClientConfig{BaseURL: srv.URL})
	results, err := client.GetExperimentHistory(context.Background(), "exp-abc")
	if err != nil {
		t.Fatalf("GetExperimentHistory: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("len(results) = %d, want 2", len(results))
	}
	if receivedQuery != "experiment_id:exp-abc" {
		t.Errorf("query = %q, want experiment_id:exp-abc", receivedQuery)
	}
}

func TestEngramClientInterface(t *testing.T) {
	var _ EngramClient = (*HTTPEngramClient)(nil)
}
