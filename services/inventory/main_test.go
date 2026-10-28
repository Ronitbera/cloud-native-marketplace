
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
		t.Fatalf("expected status 200, got %d", rec.Code)
	}

	body := rec.Body.String()
	if !strings.Contains(body, `"service":"inventory"`) {
		t.Errorf("expected inventory service in response, got %s", body)
	}
}

func TestHealthRejectsPost(t *testing.T) {
	handler := newRouter(nil)

	req := httptest.NewRequest(http.MethodPost, "/health", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected status 405, got %d", rec.Code)
	}
}

func TestStockRejectsPost(t *testing.T) {
	handler := newRouter(nil)

	req := httptest.NewRequest(http.MethodPost, "/stock/BOOK-001", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected status 405, got %d", rec.Code)
	}
}

func TestStockRejectsMalformedSKUPath(t *testing.T) {
	handler := newRouter(nil)

	req := httptest.NewRequest(http.MethodGet, "/stock/BOOK-001/extra", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", rec.Code)
	}
}

func TestReserveRejectsGet(t *testing.T) {
	handler := newRouter(nil)

	req := httptest.NewRequest(http.MethodGet, "/reserve", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected status 405, got %d", rec.Code)
	}
}

func TestReserveRejectsInvalidJSON(t *testing.T) {
	handler := newRouter(nil)

	req := httptest.NewRequest(
		http.MethodPost,
		"/reserve",
		strings.NewReader(`{"sku":`),
	)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", rec.Code)
	}
}

func TestReleaseRejectsGet(t *testing.T) {
	handler := newRouter(nil)

	req := httptest.NewRequest(http.MethodGet, "/release", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected status 405, got %d", rec.Code)
	}
}

func TestReleaseRejectsInvalidJSON(t *testing.T) {
	handler := newRouter(nil)

	req := httptest.NewRequest(
		http.MethodPost,
		"/release",
		strings.NewReader(`{"sku":`),
	)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", rec.Code)
	}
}
