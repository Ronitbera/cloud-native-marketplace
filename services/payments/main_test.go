package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHealthEndpoint(t *testing.T) {
	handler := newRouter(nil)

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, rec.Code)
	}

	body := rec.Body.String()
	if !strings.Contains(body, `"status":"ok"`) {
		t.Errorf("expected health status ok, got %s", body)
	}
	if !strings.Contains(body, `"service":"payments"`) {
		t.Errorf("expected payments service, got %s", body)
	}
}

func TestHealthRejectsPost(t *testing.T) {
	handler := newRouter(nil)

	req := httptest.NewRequest(http.MethodPost, "/health", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected status 405, got %d", rec.Code)
	}
}

func TestChargeRejectsGet(t *testing.T) {
	handler := newRouter(nil)

	req := httptest.NewRequest(http.MethodGet, "/charge", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected status 405, got %d", rec.Code)
	}
}

func TestChargeRejectsMissingIdempotencyKey(t *testing.T) {
	handler := newRouter(nil)

	body := `{"order_id":"test-order","amount_cents":1000}`
	req := httptest.NewRequest(
		http.MethodPost,
		"/charge",
		strings.NewReader(body),
	)
	req.Header.Set("Content-Type", "application/json")

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", rec.Code)
	}
}

func TestChargeRejectsInvalidPaymentFields(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{
			name: "missing order ID",
			body: `{"amount_cents":1000}`,
		},
		{
			name: "zero amount",
			body: `{"order_id":"test-order","amount_cents":0}`,
		},
		{
			name: "negative amount",
			body: `{"order_id":"test-order","amount_cents":-100}`,
		},
		{
			name: "invalid JSON",
			body: `{"order_id":`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := newRouter(nil)

			req := httptest.NewRequest(
				http.MethodPost,
				"/charge",
				strings.NewReader(tt.body),
			)
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Idempotency-Key", "test-key")

			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("expected status 400, got %d; response: %s",
					rec.Code, rec.Body.String())
			}
		})
	}
}
