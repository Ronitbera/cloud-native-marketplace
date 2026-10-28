
package main

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"sync"
	"time"
)

type event struct {
	OrderID       string `json:"order_id"`
	CustomerEmail string `json:"customer_email"`
	SKU           string `json:"sku"`
	Quantity      int    `json:"quantity"`
	Type          string `json:"type"`
}

var mu sync.Mutex
var events []map[string]any

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func newRouter() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{
				"error": "GET required",
			})
			return
		}

		writeJSON(w, http.StatusOK, map[string]string{
			"status":  "ok",
			"service": "notifications",
		})
	})

	mux.HandleFunc("/events/order-created", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{
				"error": "POST required",
			})
			return
		}

		var e event
		if err := json.NewDecoder(r.Body).Decode(&e); err != nil || e.OrderID == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{
				"error": "valid order_id is required",
			})
			return
		}

		eventType := e.Type
		if eventType == "" {
			eventType = "order.created"
		}

		record := map[string]any{
			"type":           eventType,
			"order_id":       e.OrderID,
			"customer_email": e.CustomerEmail,
			"sku":            e.SKU,
			"quantity":       e.Quantity,
			"received_at":    time.Now().UTC().Format(time.RFC3339),
		}

		mu.Lock()
		events = append(events, record)
		mu.Unlock()

		log.Printf("notification event accepted: order=%s type=%s", e.OrderID, eventType)

		writeJSON(w, http.StatusAccepted, map[string]any{
			"accepted": true,
			"event":    record,
		})
	})

	mux.HandleFunc("/events", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{
				"error": "GET required",
			})
			return
		}

		mu.Lock()
		defer mu.Unlock()

		writeJSON(w, http.StatusOK, events)
	})

	return mux
}

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8083"
	}

	log.Printf("notifications service listening on :%s", port)
	log.Fatal(http.ListenAndServe(":"+port, newRouter()))
}
