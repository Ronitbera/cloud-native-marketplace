package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHealthEndpoint(t *testing.T) {
	handler := newRouter()

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}

	body := rec.Body.String()
	if !strings.Contains(body, `"service":"notifications"`) {
		t.Errorf("expected notifications service in response, got %s", body)
	}
}

func TestHealthRejectsPost(t *testing.T) {
	handler := newRouter()

	req := httptest.NewRequest(http.MethodPost, "/health", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected status 405, got %d", rec.Code)
	}
}

func TestOrderCreatedRejectsGet(t *testing.T) {
	handler := newRouter()

	req := httptest.NewRequest(http.MethodGet, "/events/order-created", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected status 405, got %d", rec.Code)
	}
}

func TestOrderCreatedRejectsInvalidJSON(t *testing.T) {
	handler := newRouter()

	req := httptest.NewRequest(
		http.MethodPost,
		"/events/order-created",
		strings.NewReader(`{"order_id":`),
	)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", rec.Code)
	}
}

func TestOrderCreatedAcceptsValidEvent(t *testing.T) {
	handler := newRouter()

	payload := `{
		"order_id": "test-order-001",
		"customer_email": "test@example.com",
		"sku": "BOOK-001",
		"quantity": 1,
		"type": "order.paid"
	}`

	req := httptest.NewRequest(
		http.MethodPost,
		"/events/order-created",
		strings.NewReader(payload),
	)
	req.Header.Set("Content-Type", "application/json")

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected status 202, got %d: %s", rec.Code, rec.Body.String())
	}

	var response struct {
		Accepted bool           `json:"accepted"`
		Event    map[string]any `json:"event"`
	}

	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("invalid response JSON: %v", err)
	}

	if !response.Accepted {
		t.Error("expected accepted to be true")
	}

	if response.Event["order_id"] != "test-order-001" {
		t.Errorf("unexpected order ID: %v", response.Event["order_id"])
	}
}

func TestEventsEndpoint(t *testing.T) {
	handler := newRouter()

	req := httptest.NewRequest(http.MethodGet, "/events", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}

	if !json.Valid(rec.Body.Bytes()) {
		t.Errorf("expected valid JSON, got %s", rec.Body.String())
	}
}
