package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"testing"
	"time"
)

func TestIntegrationRejectsUnknownSKU(t *testing.T) {
	client := &http.Client{Timeout: 5 * time.Second}

	payload := map[string]any{
		"sku":            "TEST-NONEXISTENT-999",
		"quantity":       1,
		"amount_cents":   12500,
		"customer_email": "integration-test@example.com",
	}

	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}

	req, err := http.NewRequest(
		http.MethodPost,
		"http://localhost:8080/orders",
		bytes.NewReader(body),
	)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("could not reach orders service: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected HTTP 404 for unknown SKU, got %d", resp.StatusCode)
	}
}

func TestIntegrationRejectsInsufficientStock(t *testing.T) {
	client := &http.Client{Timeout: 5 * time.Second}

	payload := map[string]any{
		"sku":            "BOOK-001",
		"quantity":       999999,
		"amount_cents":   12500,
		"customer_email": "integration-test@example.com",
	}

	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}

	req, err := http.NewRequest(
		http.MethodPost,
		"http://localhost:8080/orders",
		bytes.NewReader(body),
	)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("could not reach orders service: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("expected HTTP 409 for insufficient stock, got %d", resp.StatusCode)
	}
}

func TestIntegrationCreatesSuccessfulOrder(t *testing.T) {
	client := &http.Client{Timeout: 10 * time.Second}

	payload := map[string]any{
		"sku":            "BOOK-001",
		"quantity":       1,
		"amount_cents":   50000,
		"customer_email": "integration-test@example.com",
	}

	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}

	req, err := http.NewRequest(
		http.MethodPost,
		"http://localhost:8080/orders",
		bytes.NewReader(body),
	)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("could not reach orders service: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected HTTP 201 for successful order, got %d", resp.StatusCode)
	}

	var result map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}

	if result["status"] != "paid" {
		t.Fatalf("expected order status 'paid', got %v", result["status"])
	}

	if result["payment_id"] == nil || result["payment_id"] == "" {
		t.Fatal("expected a payment ID in the response")
	}
}

func TestIntegrationNotificationReceived(t *testing.T) {
	client := &http.Client{Timeout: 5 * time.Second}

	resp, err := client.Get("http://localhost:8083/events")
	if err != nil {
		t.Fatalf("could not reach notifications service: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected HTTP 200 from notifications, got %d", resp.StatusCode)
	}

	var events []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&events); err != nil {
		t.Fatalf("could not decode notification events: %v", err)
	}

	found := false
	for _, event := range events {
		if event["customer_email"] == "integration-test@example.com" &&
			event["type"] == "order.paid" {
			found = true
			break
		}
	}

	if !found {
		t.Fatal("expected an order.paid notification for integration-test@example.com")
	}
}
