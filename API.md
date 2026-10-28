# Cloud-Native Marketplace API Documentation

## Overview

A Go-based microservices marketplace demonstrating order processing, inventory management, simulated payments, and event notifications. PostgreSQL stores orders, inventory, and payment records.

## Base URLs

| Service | Local URL |
|---|---|
| Orders | `http://localhost:8080` |
| Inventory | `http://localhost:8081` |
| Payments | `http://localhost:8082` |
| Notifications | `http://localhost:8083` |

## 1. Orders Service

### Health Check
`GET /health`

Checks whether the Orders service is running.

### List Orders
`GET /orders`

Returns the orders stored in PostgreSQL.

### Create Order
`POST /orders`

Example request:

```json
{
  "sku": "BOOK-001",
  "quantity": 1,
  "amount_cents": 50000,
  "customer_email": "customer@example.com"
}
```

The service reserves inventory, creates a pending order, processes a simulated payment, updates the order status, and sends a notification after successful payment.

Possible outcomes include:
- `201 Created` — order created successfully.
- `404 Not Found` — SKU does not exist.
- `409 Conflict` — insufficient stock or another conflict.
- Other error responses may occur if a dependent service or database fails.

## 2. Inventory Service

### Health Check
`GET /health`

### Check Stock
`GET /stock/{sku}`

Example: `GET /stock/BOOK-001`

### Reserve Stock
`POST /reserve`

### Release Stock
`POST /release`

Reservation and release request fields should match the implementation in `services/inventory/main.go`.

## 3. Payments Service

### Health Check
`GET /health`

### Create or Retrieve a Payment
`POST /charge`

Requires the `Idempotency-Key` request header.

Example request:

```json
{
  "order_id": "example-order-id",
  "amount_cents": 50000
}
```

The service simulates payment processing and stores payment records in PostgreSQL. Repeating a request with the same idempotency key and matching details avoids creating a duplicate payment. Reusing a key with different payment details returns a conflict.

## 4. Notifications Service

### Health Check
`GET /health`

### Submit an Event
`POST /events/order-created`

### List Received Events
`GET /events`

The service records events in memory. Events are lost when the Notifications service restarts.

## Running the Project

Start all services from the project root:

```powershell
docker compose up --build -d
```

Check service status:

```powershell
docker compose ps
```

Run tests:

```powershell
go test -count=1 ./...
```

Stop the services without deleting the database volume:

```powershell
docker compose down
```

## Important Notes

- Payments are simulated, not real financial transactions.
- Notifications are currently stored in memory rather than a persistent message broker or database.
- This project is intended as a learning and portfolio demonstration, not a production payment platform.