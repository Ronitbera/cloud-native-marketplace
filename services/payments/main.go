package main

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type chargeRequest struct {
	OrderID     string `json:"order_id"`
	AmountCents int64  `json:"amount_cents"`
}

type payment struct {
	PaymentID   string `json:"payment_id"`
	OrderID     string `json:"order_id"`
	AmountCents int64  `json:"amount_cents"`
	Status      string `json:"status"`
}

var db *pgxpool.Pool

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func newRouter(database *pgxpool.Pool) http.Handler {
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
			"service": "payments",
		})
	})

	mux.HandleFunc("/charge", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{
				"error": "POST required",
			})
			return
		}

		key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))

		var req chargeRequest
		if key == "" ||
			json.NewDecoder(r.Body).Decode(&req) != nil ||
			strings.TrimSpace(req.OrderID) == "" ||
			req.AmountCents <= 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{
				"error": "Idempotency-Key, order_id, and positive amount_cents are required",
			})
			return
		}

		var existing payment
		err := database.QueryRow(r.Context(), `
			SELECT payment_id, order_id, amount_cents, status
			FROM payments
			WHERE idempotency_key = $1
		`, key).Scan(
			&existing.PaymentID,
			&existing.OrderID,
			&existing.AmountCents,
			&existing.Status,
		)

		if err == nil {
			if existing.OrderID != req.OrderID ||
				existing.AmountCents != req.AmountCents {
				writeJSON(w, http.StatusConflict, map[string]string{
					"error": "idempotency key was already used for a different payment request",
				})
				return
			}

			writeJSON(w, http.StatusOK, existing)
			return
		}

		if !errors.Is(err, pgx.ErrNoRows) {
			log.Printf("lookup payment: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{
				"error": "payment lookup failed",
			})
			return
		}

		p := payment{
			PaymentID:   "pay_" + req.OrderID,
			OrderID:     req.OrderID,
			AmountCents: req.AmountCents,
			Status:      "succeeded_simulated",
		}

		err = database.QueryRow(r.Context(), `
			INSERT INTO payments (
				payment_id,
				order_id,
				amount_cents,
				status,
				idempotency_key
			)
			VALUES ($1, $2, $3, $4, $5)
			ON CONFLICT DO NOTHING
			RETURNING payment_id, order_id, amount_cents, status
		`,
			p.PaymentID,
			p.OrderID,
			p.AmountCents,
			p.Status,
			key,
		).Scan(
			&p.PaymentID,
			&p.OrderID,
			&p.AmountCents,
			&p.Status,
		)

		if errors.Is(err, pgx.ErrNoRows) {
			// A concurrent request may have inserted the payment first.
			err = database.QueryRow(r.Context(), `
				SELECT payment_id, order_id, amount_cents, status
				FROM payments
				WHERE idempotency_key = $1
			`, key).Scan(
				&p.PaymentID,
				&p.OrderID,
				&p.AmountCents,
				&p.Status,
			)

			if err != nil {
				writeJSON(w, http.StatusConflict, map[string]string{
					"error": "payment conflicts with an existing payment record",
				})
				return
			}

			if p.OrderID != req.OrderID ||
				p.AmountCents != req.AmountCents {
				writeJSON(w, http.StatusConflict, map[string]string{
					"error": "idempotency key was already used for a different payment request",
				})
				return
			}

			writeJSON(w, http.StatusOK, p)
			return
		}

		if err != nil {
			log.Printf("save payment: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{
				"error": "failed to save payment",
			})
			return
		}

		writeJSON(w, http.StatusCreated, p)
	})

	return mux
}

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	databaseURL := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	if databaseURL == "" {
		log.Fatal("DATABASE_URL is not set")
	}

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

	_, err = db.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS payments (
			payment_id TEXT PRIMARY KEY,
			order_id TEXT NOT NULL UNIQUE,
			amount_cents BIGINT NOT NULL CHECK (amount_cents > 0),
			status TEXT NOT NULL,
			idempotency_key TEXT NOT NULL UNIQUE,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)
	`)
	if err != nil {
		log.Fatalf("ensure payments table exists: %v", err)
	}

	port := strings.TrimSpace(os.Getenv("PORT"))
	if port == "" {
		port = "8082"
	}

	mux := newRouter(db)

	log.Printf("payments service listening on :%s", port)
	log.Fatal(http.ListenAndServe(":"+port, mux))
}
