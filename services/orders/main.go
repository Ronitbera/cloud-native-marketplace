package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type orderRequest struct {
	SKU           string `json:"sku"`
	Quantity      int    `json:"quantity"`
	AmountCents   int    `json:"amount_cents"`
	CustomerEmail string `json:"customer_email"`
}

type order struct {
	ID            string    `json:"id"`
	SKU           string    `json:"sku"`
	Quantity      int       `json:"quantity"`
	AmountCents   int       `json:"amount_cents"`
	Status        string    `json:"status"`
	PaymentID     string    `json:"payment_id,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
	CustomerEmail string    `json:"customer_email"`
}

var db *pgxpool.Pool

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func newRouter(inventoryURL, paymentsURL, notificationsURL string) http.Handler {
	mux := http.NewServeMux()

	client := &http.Client{Timeout: 5 * time.Second}

	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{
				"error": "GET required",
			})
			return
		}

		writeJSON(w, http.StatusOK, map[string]string{
			"status":  "ok",
			"service": "orders",
		})
	})

	mux.HandleFunc("/orders", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			if db == nil {
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "database unavailable"})
				return
			}

			rows, err := db.Query(r.Context(), `
			SELECT id, sku, quantity, amount_cents, status,
			       COALESCE(payment_id, ''), created_at, customer_email
			FROM orders ORDER BY created_at DESC
		`)
			if err != nil {
				log.Printf("list orders: %v", err)
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to retrieve orders"})
				return
			}
			defer rows.Close()

			orders := make([]order, 0)

			for rows.Next() {
				var item order

				if err := rows.Scan(
					&item.ID,
					&item.SKU,
					&item.Quantity,
					&item.AmountCents,
					&item.Status,
					&item.PaymentID,
					&item.CreatedAt,
					&item.CustomerEmail,
				); err != nil {
					log.Printf("read order: %v", err)
					writeJSON(w, http.StatusInternalServerError, map[string]string{
						"error": "failed to read orders",
					})
					return
				}

				orders = append(orders, item)
			}
			if err := rows.Err(); err != nil {
				log.Printf("iterate orders: %v", err)
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to read orders"})
				return
			}
			writeJSON(w, http.StatusOK, orders)

		case http.MethodPost:
			var req orderRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil ||
				strings.TrimSpace(req.SKU) == "" || req.Quantity <= 0 || req.AmountCents <= 0 {
				writeJSON(w, http.StatusBadRequest, map[string]string{
					"error": "sku, positive quantity, and positive amount_cents are required",
				})
				return
			}
			if db == nil {
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "database unavailable"})
				return
			}
			if strings.TrimSpace(inventoryURL) == "" || strings.TrimSpace(paymentsURL) == "" {
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "inventory or payments URL is not configured"})
				return
			}

			orderID := fmt.Sprintf("ord_%d", time.Now().UnixNano())
			baseCtx := r.Context()

			// 1. Reserve stock.
			reserveBody, _ := json.Marshal(map[string]any{
				"sku": req.SKU, "quantity": req.Quantity, "order_id": orderID,
			})
			reserveReq, err := http.NewRequestWithContext(baseCtx, http.MethodPost,
				strings.TrimRight(inventoryURL, "/")+"/reserve", bytes.NewReader(reserveBody))
			if err != nil {
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "could not create inventory request"})
				return
			}
			reserveReq.Header.Set("Content-Type", "application/json")

			reserveResp, err := client.Do(reserveReq)
			if err != nil {
				writeJSON(w, http.StatusBadGateway, map[string]string{"error": "inventory service unavailable"})
				return
			}
			reserveData, _ := io.ReadAll(reserveResp.Body)
			reserveResp.Body.Close()
			if reserveResp.StatusCode < 200 || reserveResp.StatusCode >= 300 {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(reserveResp.StatusCode)
				_, _ = w.Write(reserveData)
				return
			}

			// Best-effort helper for returning reserved stock.
			releaseStock := func() {
				body, _ := json.Marshal(map[string]any{
					"sku": req.SKU, "quantity": req.Quantity, "order_id": orderID,
				})
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				releaseReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
					strings.TrimRight(inventoryURL, "/")+"/release", bytes.NewReader(body))
				if err != nil {
					log.Printf("create stock release request: %v", err)
					return
				}
				releaseReq.Header.Set("Content-Type", "application/json")
				resp, err := client.Do(releaseReq)
				if err != nil {
					log.Printf("release reserved stock: %v", err)
					return
				}
				resp.Body.Close()
				if resp.StatusCode < 200 || resp.StatusCode >= 300 {
					log.Printf("stock release returned HTTP %d", resp.StatusCode)
				}
			}

			// 2. Persist a pending order before attempting payment.
			var saved order
			err = db.QueryRow(baseCtx, `

        INSERT INTO orders (
         id, sku, quantity, amount_cents, status,
         payment_id, created_at, customer_email
          )
          VALUES ($1, $2, $3, $4, 'pending', NULL, NOW(), $5)
          RETURNING id, sku, quantity, amount_cents, status,
          COALESCE(payment_id, ''), created_at, customer_email
          `, orderID, req.SKU, req.Quantity, req.AmountCents, req.CustomerEmail).Scan(
				&saved.ID, &saved.SKU, &saved.Quantity, &saved.AmountCents,
				&saved.Status, &saved.PaymentID, &saved.CreatedAt, &saved.CustomerEmail,
			)

			if err != nil {
				log.Printf("save pending order: %v", err)
				releaseStock()
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to save order"})
				return
			}

			// 3. Call the payments service using its documented JSON contract.
			paymentPayload, _ := json.Marshal(map[string]any{
				"order_id": orderID, "amount_cents": req.AmountCents,
			})
			paymentReq, err := http.NewRequestWithContext(baseCtx, http.MethodPost,
				strings.TrimRight(paymentsURL, "/")+"/charge", bytes.NewReader(paymentPayload))
			if err != nil {
				log.Printf("create payment request: %v", err)
				releaseStock()
				_, _ = db.Exec(context.Background(),
					`UPDATE orders SET status = 'payment_failed' WHERE id = $1`, orderID)
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "could not create payment request"})
				return
			}
			paymentReq.Header.Set("Content-Type", "application/json")
			paymentReq.Header.Set("Idempotency-Key", orderID)

			paymentResp, err := client.Do(paymentReq)
			if err != nil {
				log.Printf("payment service unavailable: %v", err)
				releaseStock()
				_, _ = db.Exec(context.Background(),
					`UPDATE orders SET status = 'payment_failed' WHERE id = $1`, orderID)
				writeJSON(w, http.StatusBadGateway, map[string]string{
					"error":    "payment outcome could not be confirmed; check payment service before retrying",
					"order_id": orderID,
				})
				return
			}
			paymentData, readErr := io.ReadAll(paymentResp.Body)
			paymentResp.Body.Close()
			if readErr != nil {
				log.Printf("read payment response: %v", readErr)
			}

			var paymentResult struct {
				PaymentID   string `json:"payment_id"`
				OrderID     string `json:"order_id"`
				AmountCents int64  `json:"amount_cents"`
				Status      string `json:"status"`
			}

			if paymentResp.StatusCode < 200 || paymentResp.StatusCode >= 300 ||
				json.Unmarshal(paymentData, &paymentResult) != nil ||
				paymentResult.PaymentID == "" ||
				paymentResult.OrderID != orderID ||
				paymentResult.AmountCents != int64(req.AmountCents) ||
				paymentResult.Status != "succeeded_simulated" {
				log.Printf("payment not confirmed for order %s: HTTP %d, body %s",
					orderID, paymentResp.StatusCode, strings.TrimSpace(string(paymentData)))
				releaseStock()
				_, _ = db.Exec(context.Background(),
					`UPDATE orders SET status = 'payment_failed' WHERE id = $1`, orderID)
				writeJSON(w, http.StatusBadGateway, map[string]string{
					"error": "payment was not confirmed", "order_id": orderID,
				})
				return
			}

			// 4. Persist the confirmed payment ID and order status.
			err = db.QueryRow(baseCtx, `
			UPDATE orders
			SET payment_id = $2, status = 'paid'
			WHERE id = $1
			RETURNING id, sku, quantity, amount_cents, status,
			          COALESCE(payment_id, ''), created_at
		`, orderID, paymentResult.PaymentID).Scan(
				&saved.ID, &saved.SKU, &saved.Quantity, &saved.AmountCents,
				&saved.Status, &saved.PaymentID, &saved.CreatedAt,
			)
			if err != nil {
				log.Printf("save payment result for order %s: %v", orderID, err)
				// Do not release stock here: payment may already have succeeded.
				writeJSON(w, http.StatusInternalServerError, map[string]string{
					"error":    "payment succeeded but saving its result failed; investigate before retrying",
					"order_id": orderID,
				})
				return
			}

			// 5. Notify only after the paid order has been persisted.
			if notificationsURL != "" {
				go func(item order) {
					payload, _ := json.Marshal(map[string]any{
						"order_id":       item.ID,
						"sku":            item.SKU,
						"quantity":       item.Quantity,
						"type":           "order.paid",
						"customer_email": req.CustomerEmail,
					})
					ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
					defer cancel()
					notifyReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
						strings.TrimRight(notificationsURL, "/")+"/events/order-created", bytes.NewReader(payload))
					if err != nil {
						log.Printf("create notification request: %v", err)
						return
					}
					notifyReq.Header.Set("Content-Type", "application/json")
					resp, err := client.Do(notifyReq)
					if err != nil {
						log.Printf("send order notification: %v", err)
						return
					}
					resp.Body.Close()
					if resp.StatusCode < 200 || resp.StatusCode >= 300 {
						log.Printf("notification service returned HTTP %d", resp.StatusCode)
					}
				}(saved)
			}

			writeJSON(w, http.StatusCreated, saved)

		default:
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "GET or POST required"})
		}
	})
	return mux
}

func main() {
	databaseURL := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	if databaseURL == "" {
		log.Fatal("DATABASE_URL is not set")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var err error
	db, err = pgxpool.New(ctx, databaseURL)
	if err != nil {
		log.Fatalf("configure database: %v", err)
	}
	defer db.Close()

	if err := db.Ping(ctx); err != nil {
		log.Fatalf("connect to database: %v", err)
	}
	log.Println("Connected to PostgreSQL")

	// Create the table if it does not already exist.
	_, err = db.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS orders (
			id TEXT PRIMARY KEY,
			sku TEXT NOT NULL,
			quantity INTEGER NOT NULL CHECK (quantity > 0),
			amount_cents INTEGER NOT NULL CHECK (amount_cents > 0),
			status TEXT NOT NULL DEFAULT 'pending',
			payment_id TEXT,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)
	`)

	_, err = db.Exec(ctx, `
		ALTER TABLE orders
		ADD COLUMN IF NOT EXISTS customer_email TEXT NOT NULL DEFAULT ''
	`)
	if err != nil {
		log.Fatalf("Unable to add customer_email column: %v", err)
	}
	if err != nil {
		log.Fatalf("ensure orders table exists: %v", err)
	}

	_ = errors.Is // keep the pgx dependency available for later database error handling
	_ = pgx.ErrNoRows

	inventoryURL := strings.TrimSpace(os.Getenv("INVENTORY_URL"))
	paymentsURL := strings.TrimSpace(os.Getenv("PAYMENTS_URL"))
	notificationsURL := strings.TrimSpace(os.Getenv("NOTIFICATIONS_URL"))

	if inventoryURL == "" {
		inventoryURL = "http://inventory:8081"
	}
	if paymentsURL == "" {
		paymentsURL = "http://payments:8082"
	}
	if notificationsURL == "" {
		notificationsURL = "http://notifications:8083"
	}

	port := strings.TrimSpace(os.Getenv("PORT"))
	if port == "" {
		port = "8080"
	}

	log.Printf("orders service listening on :%s", port)
	log.Fatal(http.ListenAndServe(":"+port, newRouter(
		inventoryURL, paymentsURL, notificationsURL,
	)))
}
