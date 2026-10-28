package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHealthEndpoint(t *testing.T) {
	router := newRouter("", "", "")
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", recorder.Code)
	}

	var response map[string]string
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatalf("invalid JSON response: %v", err)
	}

	if response["status"] != "ok" {
		t.Errorf("expected status ok, got %q", response["status"])
	}

	if response["service"] != "orders" {
		t.Errorf("expected service orders, got %q", response["service"])
	}
}

func TestHealthEndpointRejectsPost(t *testing.T) {
	router := newRouter("", "", "")
	req := httptest.NewRequest(http.MethodPost, "/health", nil)
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected status 405, got %d", recorder.Code)
	}
}

func TestOrdersRejectsUnsupportedMethod(t *testing.T) {
	router := newRouter("", "", "")
	req := httptest.NewRequest(http.MethodDelete, "/orders", nil)
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected status 405, got %d", recorder.Code)
	}
}

func TestOrdersRejectsInvalidJSON(t *testing.T) {
	router := newRouter("", "", "")
	req := httptest.NewRequest(
		http.MethodPost,
		"/orders",
		strings.NewReader("{invalid"),
	)
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", recorder.Code)
	}
}

func TestOrdersRejectsInvalidQuantity(t *testing.T) {
	router := newRouter("", "", "")
	body := `{"sku":"BOOK-001","quantity":0,"amount_cents":50000}`

	req := httptest.NewRequest(
		http.MethodPost,
		"/orders",
		strings.NewReader(body),
	)
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", recorder.Code)
	}
}
