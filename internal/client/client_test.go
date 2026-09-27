package client

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func testClient(url string) *Client {
	c := New(url, "tdk_test_abc", "terraform-provider-transcdr/test")
	c.RetryBase = time.Millisecond
	c.RetryMax = 5 * time.Millisecond
	return c
}

func TestHeadersAndDecode(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer tdk_test_abc" {
			t.Errorf("Authorization = %q", got)
		}
		if got := r.Header.Get("User-Agent"); got != "terraform-provider-transcdr/test" {
			t.Errorf("User-Agent = %q", got)
		}
		if r.Method == http.MethodPost {
			if got := r.Header.Get("Idempotency-Key"); !strings.HasPrefix(got, "tf-") || len(got) != 35 {
				t.Errorf("Idempotency-Key = %q", got)
			}
			if got := r.Header.Get("Content-Type"); got != "application/json" {
				t.Errorf("Content-Type = %q", got)
			}
			body, _ := io.ReadAll(r.Body)
			if string(body) != `{"name":"x"}` {
				t.Errorf("body = %s", body)
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"con_1","name":"x"}`))
	}))
	defer srv.Close()

	var out struct{ ID, Name string }
	if err := testClient(srv.URL).Create(context.Background(), "/v1/connections", map[string]string{"name": "x"}, &out); err != nil {
		t.Fatal(err)
	}
	if out.ID != "con_1" {
		t.Fatalf("decoded %+v", out)
	}
	if err := testClient(srv.URL).Get(context.Background(), "/v1/connections/con_1", nil, &out); err != nil {
		t.Fatal(err)
	}
}

func TestRetriesGetOn5xxAnd429(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch calls.Add(1) {
		case 1:
			w.WriteHeader(http.StatusServiceUnavailable)
		case 2:
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
		default:
			_, _ = w.Write([]byte(`{"ok":true}`))
		}
	}))
	defer srv.Close()

	var out map[string]bool
	if err := testClient(srv.URL).Get(context.Background(), "/v1/organization", nil, &out); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 3 || !out["ok"] {
		t.Fatalf("calls = %d, out = %v", calls.Load(), out)
	}
}

func TestCreateIsNotRetriedOn5xx(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer srv.Close()

	err := testClient(srv.URL).Create(context.Background(), "/v1/presets", map[string]any{}, nil)
	var apiErr *Error
	if !errors.As(err, &apiErr) || apiErr.Status != http.StatusBadGateway || apiErr.Type != "api_error" {
		t.Fatalf("err = %v", err)
	}
	if calls.Load() != 1 {
		t.Fatalf("a create that may have been processed was sent %d times", calls.Load())
	}
}

func TestCreateIsRetriedOn429WithTheSameKey(t *testing.T) {
	var keys []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		keys = append(keys, r.Header.Get("Idempotency-Key"))
		if len(keys) == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	if err := testClient(srv.URL).Create(context.Background(), "/v1/presets", map[string]any{}, nil); err != nil {
		t.Fatal(err)
	}
	if len(keys) != 2 || keys[0] != keys[1] || keys[0] == "" {
		t.Fatalf("keys = %v", keys)
	}
}

func TestGivesUpAfterMaxRetries(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := testClient(srv.URL)
	c.MaxRetries = 2
	if err := c.Get(context.Background(), "/v1/x", nil, nil); err == nil {
		t.Fatal("expected an error")
	}
	if calls.Load() != 3 {
		t.Fatalf("calls = %d, want 3", calls.Load())
	}
}

func TestErrorEnvelope(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Request-Id", "header-id")
		w.WriteHeader(http.StatusUnprocessableEntity)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{
			"type": "invalid_request_error", "code": "validation_failed",
			"message": "The output.renditions field is required.", "param": "output.renditions",
			"details":    map[string][]string{"output.renditions": {"The output.renditions field is required."}, "name": {"Too long."}},
			"request_id": "3f2a-1c",
		}})
	}))
	defer srv.Close()

	err := testClient(srv.URL).Patch(context.Background(), "/v1/presets/pre_1", map[string]any{}, nil)
	var apiErr *Error
	if !errors.As(err, &apiErr) {
		t.Fatalf("err = %v", err)
	}
	if apiErr.Status != 422 || apiErr.Code != "validation_failed" || apiErr.Param != "output.renditions" || apiErr.RequestID != "3f2a-1c" {
		t.Fatalf("parsed %+v", apiErr)
	}
	detail := apiErr.Detail()
	for _, want := range []string{
		"The output.renditions field is required.",
		"  - name: Too long.\n  - output.renditions: The output.renditions field is required.",
		"PATCH /v1/presets/pre_1 returned HTTP 422 (invalid_request_error: validation_failed).",
		"Request id: 3f2a-1c.",
	} {
		if !strings.Contains(detail, want) {
			t.Errorf("Detail() lacks %q:\n%s", want, detail)
		}
	}
}

func TestErrorWithoutEnvelope(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Request-Id", "rid-9")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte("not here"))
	}))
	defer srv.Close()

	err := testClient(srv.URL).Get(context.Background(), "/v1/automations/aut_x", nil, nil)
	if !IsNotFound(err) {
		t.Fatalf("err = %v", err)
	}
	var apiErr *Error
	errors.As(err, &apiErr)
	if apiErr.Message != "not here" || apiErr.RequestID != "rid-9" || apiErr.Type != "invalid_request_error" {
		t.Fatalf("parsed %+v", apiErr)
	}
}

func TestDeleteTreats404AsGone(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()
	if err := testClient(srv.URL).Delete(context.Background(), "/v1/webhooks/whk_x"); err != nil {
		t.Fatal(err)
	}
}

func TestIsConflict(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"error":{"type":"invalid_request_error","code":"connection_in_use","message":"An automation uses this connection."}}`))
	}))
	defer srv.Close()
	err := testClient(srv.URL).Delete(context.Background(), "/v1/connections/con_x")
	if !IsConflict(err) {
		t.Fatalf("err = %v", err)
	}
}

func TestBackoff(t *testing.T) {
	c := New("http://x", "", "")
	if d := c.Backoff(0, "2"); d != 2*time.Second {
		t.Errorf("Retry-After 2 → %v", d)
	}
	if d := c.Backoff(0, "3600"); d != 30*time.Second {
		t.Errorf("Retry-After is capped: %v", d)
	}
	for attempt := 0; attempt < 10; attempt++ {
		d := c.Backoff(attempt, "")
		limit := min(500*time.Millisecond<<attempt, 30*time.Second)
		if d < limit/2 || d > limit {
			t.Errorf("attempt %d: %v outside [%v, %v]", attempt, d, limit/2, limit)
		}
	}
}

func TestListAllFollowsCursors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("limit") != "100" {
			t.Errorf("limit = %q", r.URL.Query().Get("limit"))
		}
		switch r.URL.Query().Get("cursor") {
		case "":
			_, _ = w.Write([]byte(`{"object":"list","data":[{"id":"a"},{"id":"b"}],"has_more":true,"next_cursor":"b"}`))
		case "b":
			_, _ = w.Write([]byte(`{"object":"list","data":[{"id":"c"}],"has_more":false,"next_cursor":null}`))
		}
	}))
	defer srv.Close()

	type item struct{ ID string }
	all, err := ListAll[item](context.Background(), testClient(srv.URL), "/v1/api-keys", nil, nil)
	if err != nil || len(all) != 3 || all[2].ID != "c" {
		t.Fatalf("all = %v, err = %v", all, err)
	}
	found, err := ListAll(context.Background(), testClient(srv.URL), "/v1/api-keys", nil, func(i item) bool { return i.ID == "a" })
	if err != nil || len(found) != 1 {
		t.Fatalf("stop: %v, %v", found, err)
	}
}
