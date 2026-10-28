package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type reserveRequest struct {
	SKU      string `json:"sku"`
	Quantity int    `json:"quantity"`
	OrderID  string `json:"order_id"`
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
			"service": "inventory",
		})
	})

	mux.HandleFunc("/stock/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{
				"error": "GET required",
			})
			return
		}

		sku := strings.TrimPrefix(r.URL.Path, "/stock/")
		if sku == "" || strings.Contains(sku, "/") {
			writeJSON(w, http.StatusBadRequest, map[string]string{
				"error": "valid SKU is required",
			})
			return
		}

		var quantity int
		err := database.QueryRow(r.Context(),
			"SELECT quantity FROM inventory WHERE sku = $1", sku,
		).Scan(&quantity)

		if err != nil {
			if err == pgx.ErrNoRows {
				writeJSON(w, http.StatusNotFound, map[string]string{
					"error": "SKU not found",
				})
				return
			}

			log.Printf("Failed to fetch stock for SKU %s: %v", sku, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{
				"error": "failed to fetch stock",
			})
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"sku":      sku,
			"quantity": quantity,
		})
	})

	mux.HandleFunc("/reserve", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{
				"error": "POST required",
			})
			return
		}

		var req reserveRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil ||
			strings.TrimSpace(req.SKU) == "" ||
			req.Quantity <= 0 ||
			strings.TrimSpace(req.OrderID) == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{
				"error": "sku, positive quantity, and order_id are required",
			})
			return
		}

		var remaining int
		err := database.QueryRow(r.Context(), `
			UPDATE inventory
			SET quantity = quantity - $1, updated_at = NOW()
			WHERE sku = $2 AND quantity >= $1
			RETURNING quantity
		`, req.Quantity, req.SKU).Scan(&remaining)

		if err != nil {
			if err != pgx.ErrNoRows {
				log.Printf("Failed to reserve stock: %v", err)
			}

			var available int
			checkErr := database.QueryRow(r.Context(),
				"SELECT quantity FROM inventory WHERE sku = $1", req.SKU,
			).Scan(&available)

			if checkErr == pgx.ErrNoRows {
				writeJSON(w, http.StatusNotFound, map[string]string{
					"error": "SKU not found",
				})
				return
			}

			if checkErr != nil {
				writeJSON(w, http.StatusInternalServerError, map[string]string{
					"error": "failed to check stock",
				})
				return
			}

			writeJSON(w, http.StatusConflict, map[string]any{
				"reserved":  false,
				"available": available,
				"error":     "insufficient stock",
			})
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"reserved":  true,
			"sku":       req.SKU,
			"remaining": remaining,
			"order_id":  req.OrderID,
		})
	})

	mux.HandleFunc("/release", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{
				"error": "POST required",
			})
			return
		}

		var req reserveRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil ||
			strings.TrimSpace(req.SKU) == "" ||
			req.Quantity <= 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{
				"error": "sku and positive quantity are required",
			})
			return
		}

		var remaining int
		err := database.QueryRow(r.Context(), `
			UPDATE inventory
			SET quantity = quantity + $1, updated_at = NOW()
			WHERE sku = $2
			RETURNING quantity
		`, req.Quantity, req.SKU).Scan(&remaining)

		if err != nil {
			if err != pgx.ErrNoRows {
				log.Printf("Failed to release stock: %v", err)
			}
			writeJSON(w, http.StatusNotFound, map[string]string{
				"error": "SKU not found",
			})
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"released":  true,
			"sku":       req.SKU,
			"available": remaining,
		})
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
		log.Fatalf("Unable to configure database: %v", err)
	}
	defer db.Close()

	if err := db.Ping(ctx); err != nil {
		log.Fatalf("Database connection failed: %v", err)
	}

	log.Println("Connected to PostgreSQL")

	_, err = db.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS inventory (
			sku TEXT PRIMARY KEY,
			quantity INTEGER NOT NULL CHECK (quantity >= 0),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)
	`)
	if err != nil {
		log.Fatalf("ensure inventory table exists: %v", err)
	}

	_, err = db.Exec(ctx, `
		INSERT INTO inventory (sku, quantity)
		VALUES ('BOOK-001', 20), ('MUG-001', 15)
		ON CONFLICT (sku) DO NOTHING
	`)
	if err != nil {
		log.Fatalf("initialize inventory: %v", err)
	}

	port := strings.TrimSpace(os.Getenv("PORT"))
	if port == "" {
		port = "8081"
	}

	log.Printf("inventory service listening on :%s", port)
	log.Fatal(http.ListenAndServe(":"+port, newRouter(db)))
}
