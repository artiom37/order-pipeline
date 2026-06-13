package orders

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

type Order struct {
	ID             uuid.UUID  `json:"id"`
	IdempotencyKey string     `json:"idempotency_key"`
	CustomerName   string     `json:"customer_name"`
	RestaurantID   string     `json:"restaurant_id"`
	Status         string     `json:"status"`
	FailureReason  *string    `json:"failure_reason,omitempty"`
	AttemptCount   int        `json:"attempt_count"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
	DeliveredAt    *time.Time `json:"delivered_at,omitempty"`
}

type CreateOrderInput struct {
	IdempotencyKey string `json:"idempotency_key"`
	CustomerName   string `json:"customer_name"`
	RestaurantID   string `json:"restaurant_id"`
}

type Event struct {
	ID         int64      `json:"id"`
	OrderID    uuid.UUID  `json:"order_id"`
	FromStatus *string    `json:"from_status,omitempty"`
	ToStatus   string     `json:"to_status"`
	Reason     *string    `json:"reason,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
}

type Dashboard struct {
	StatusCounts map[string]int64 `json:"status_counts"`
	RecentEvents []Event          `json:"recent_events"`
	Totals       Totals           `json:"totals"`
}

type Totals struct {
	OrdersTotal        int64 `json:"orders_total"`
	InFlight           int64 `json:"in_flight"`
	Delivered          int64 `json:"delivered"`
	Failed             int64 `json:"failed"`
	PendingOutbox      int64 `json:"pending_outbox"`
	MaxAttemptCount    int64 `json:"max_attempt_count"`
	OldestPendingSecs  int64 `json:"oldest_pending_seconds"`
	RecentEventsWindow int64 `json:"recent_events_window"`
}

func (r *Repository) CreateOrder(ctx context.Context, in CreateOrderInput) (Order, bool, error) {
	if in.IdempotencyKey == "" || in.CustomerName == "" || in.RestaurantID == "" {
		return Order{}, false, errors.New("idempotency_key, customer_name, and restaurant_id are required")
	}

	id := uuid.New()
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Order{}, false, err
	}
	defer tx.Rollback(ctx)

	var order Order
	err = tx.QueryRow(ctx, `
		INSERT INTO orders (id, idempotency_key, customer_name, restaurant_id, status)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (idempotency_key) DO NOTHING
		RETURNING id, idempotency_key, customer_name, restaurant_id, status, failure_reason, attempt_count, created_at, updated_at, delivered_at
	`, id, in.IdempotencyKey, in.CustomerName, in.RestaurantID, StatusPlaced).Scan(
		&order.ID, &order.IdempotencyKey, &order.CustomerName, &order.RestaurantID, &order.Status,
		&order.FailureReason, &order.AttemptCount, &order.CreatedAt, &order.UpdatedAt, &order.DeliveredAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// Idempotency conflict means the client retried a previously accepted order.
			err = tx.QueryRow(ctx, `
				SELECT id, idempotency_key, customer_name, restaurant_id, status, failure_reason, attempt_count, created_at, updated_at, delivered_at
				FROM orders WHERE idempotency_key = $1
			`, in.IdempotencyKey).Scan(
				&order.ID, &order.IdempotencyKey, &order.CustomerName, &order.RestaurantID, &order.Status,
				&order.FailureReason, &order.AttemptCount, &order.CreatedAt, &order.UpdatedAt, &order.DeliveredAt,
			)
			if err != nil {
				return Order{}, false, err
			}
			if err := tx.Commit(ctx); err != nil {
				return Order{}, false, err
			}
			return order, false, nil
		}
		return Order{}, false, err
	}

	if err := insertEvent(ctx, tx, order.ID, nil, StatusPlaced, "order placed"); err != nil {
		return Order{}, false, err
	}
	if err := insertOutbox(ctx, tx, "orders.advance", map[string]any{"order_id": order.ID.String()}, time.Now()); err != nil {
		return Order{}, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Order{}, false, err
	}
	return order, true, nil
}

func (r *Repository) ListOrders(ctx context.Context, limit int) ([]Order, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := r.pool.Query(ctx, `
		SELECT id, idempotency_key, customer_name, restaurant_id, status, failure_reason, attempt_count, created_at, updated_at, delivered_at
		FROM orders
		ORDER BY created_at DESC
		LIMIT $1
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Order
	for rows.Next() {
		var o Order
		if err := rows.Scan(&o.ID, &o.IdempotencyKey, &o.CustomerName, &o.RestaurantID, &o.Status, &o.FailureReason, &o.AttemptCount, &o.CreatedAt, &o.UpdatedAt, &o.DeliveredAt); err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

func (r *Repository) GetOrder(ctx context.Context, id uuid.UUID) (Order, error) {
	var o Order
	err := r.pool.QueryRow(ctx, `
		SELECT id, idempotency_key, customer_name, restaurant_id, status, failure_reason, attempt_count, created_at, updated_at, delivered_at
		FROM orders WHERE id=$1
	`, id).Scan(&o.ID, &o.IdempotencyKey, &o.CustomerName, &o.RestaurantID, &o.Status, &o.FailureReason, &o.AttemptCount, &o.CreatedAt, &o.UpdatedAt, &o.DeliveredAt)
	return o, err
}

func (r *Repository) Transition(ctx context.Context, orderID uuid.UUID, from, to, reason string) (bool, error) {
	if !CanTransition(from, to) {
		return false, fmt.Errorf("invalid transition %s -> %s", from, to)
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)

	cmd, err := tx.Exec(ctx, `
		UPDATE orders
		SET status=$1,
		    updated_at=now(),
		    delivered_at=CASE WHEN $1 = 'delivered' THEN now() ELSE delivered_at END,
		    failure_reason=NULL,
		    attempt_count=0
		WHERE id=$2 AND status=$3
	`, to, orderID, from)
	if err != nil {
		return false, err
	}
	if cmd.RowsAffected() == 0 {
		if err := tx.Commit(ctx); err != nil {
			return false, err
		}
		return false, nil
	}
	if err := insertEvent(ctx, tx, orderID, &from, to, reason); err != nil {
		return false, err
	}
	if !IsTerminal(to) {
		if err := insertOutbox(ctx, tx, "orders.advance", map[string]any{"order_id": orderID.String()}, time.Now()); err != nil {
			return false, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return false, err
	}
	return true, nil
}

func (r *Repository) ScheduleRetry(ctx context.Context, orderID uuid.UUID, failureReason string, delay time.Duration, maxAttempts int) (string, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)

	var attempts int
	var currentStatus string
	err = tx.QueryRow(ctx, `
		UPDATE orders
		SET attempt_count = attempt_count + 1,
		    failure_reason = $1,
		    updated_at = now()
		WHERE id=$2 AND status NOT IN ($3, $4, $5)
		RETURNING attempt_count, status
	`, failureReason, orderID, StatusDelivered, StatusCancelled, StatusFailed).Scan(&attempts, &currentStatus)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "ignored", tx.Commit(ctx)
		}
		return "", err
	}

	if attempts >= maxAttempts {
		from := currentStatus
		cmd, err := tx.Exec(ctx, `
			UPDATE orders
			SET status=$1, failure_reason=$2, updated_at=now()
			WHERE id=$3 AND status=$4
		`, StatusFailed, failureReason, orderID, currentStatus)
		if err != nil {
			return "", err
		}
		if cmd.RowsAffected() > 0 {
			if err := insertEvent(ctx, tx, orderID, &from, StatusFailed, failureReason); err != nil {
				return "", err
			}
		}
		if err := tx.Commit(ctx); err != nil {
			return "", err
		}
		return "failed", nil
	}

	availableAt := time.Now().Add(delay)
	if err := insertOutbox(ctx, tx, "orders.advance", map[string]any{"order_id": orderID.String()}, availableAt); err != nil {
		return "", err
	}
	if err := tx.Commit(ctx); err != nil {
		return "", err
	}
	return "retry_scheduled", nil
}

func (r *Repository) ClaimOutbox(ctx context.Context, limit int) ([]OutboxMessage, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	rows, err := tx.Query(ctx, `
		SELECT id, topic, payload
		FROM outbox_messages
		WHERE status='pending' AND available_at <= now()
		ORDER BY id
		LIMIT $1
		FOR UPDATE SKIP LOCKED
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var messages []OutboxMessage
	for rows.Next() {
		var m OutboxMessage
		if err := rows.Scan(&m.ID, &m.Topic, &m.Payload); err != nil {
			return nil, err
		}
		messages = append(messages, m)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return messages, nil
}

func (r *Repository) MarkOutboxPublished(ctx context.Context, id int64) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE outbox_messages
		SET status='published', published_at=now()
		WHERE id=$1
	`, id)
	return err
}

func (r *Repository) MarkOutboxPending(ctx context.Context, id int64) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE outbox_messages
		SET status='pending'
		WHERE id=$1
	`, id)
	return err
}

func (r *Repository) Dashboard(ctx context.Context) (Dashboard, error) {
	statusCounts := map[string]int64{}
	rows, err := r.pool.Query(ctx, `SELECT status, count(*) FROM orders GROUP BY status`)
	if err != nil {
		return Dashboard{}, err
	}
	for rows.Next() {
		var status string
		var count int64
		if err := rows.Scan(&status, &count); err != nil {
			rows.Close()
			return Dashboard{}, err
		}
		statusCounts[status] = count
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return Dashboard{}, err
	}

	events, err := r.RecentEvents(ctx, 30)
	if err != nil {
		return Dashboard{}, err
	}

	var t Totals
	err = r.pool.QueryRow(ctx, `
		SELECT
		  count(*) AS orders_total,
		  count(*) FILTER (WHERE status NOT IN ('delivered','cancelled','failed')) AS in_flight,
		  count(*) FILTER (WHERE status='delivered') AS delivered,
		  count(*) FILTER (WHERE status='failed') AS failed,
		  COALESCE((SELECT count(*) FROM outbox_messages WHERE status IN ('pending','publishing')), 0) AS pending_outbox,
		  COALESCE(max(attempt_count), 0) AS max_attempt_count,
		  COALESCE(EXTRACT(EPOCH FROM now() - (SELECT min(available_at) FROM outbox_messages WHERE status='pending')), 0)::bigint AS oldest_pending_seconds,
		  (SELECT count(*) FROM order_events WHERE created_at >= now() - interval '60 seconds') AS recent_events_window
		FROM orders
	`).Scan(&t.OrdersTotal, &t.InFlight, &t.Delivered, &t.Failed, &t.PendingOutbox, &t.MaxAttemptCount, &t.OldestPendingSecs, &t.RecentEventsWindow)
	if err != nil {
		return Dashboard{}, err
	}
	return Dashboard{StatusCounts: statusCounts, RecentEvents: events, Totals: t}, nil
}

func (r *Repository) RecentEvents(ctx context.Context, limit int) ([]Event, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, order_id, from_status, to_status, reason, created_at
		FROM order_events
		ORDER BY id DESC
		LIMIT $1
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var events []Event
	for rows.Next() {
		var e Event
		if err := rows.Scan(&e.ID, &e.OrderID, &e.FromStatus, &e.ToStatus, &e.Reason, &e.CreatedAt); err != nil {
			return nil, err
		}
		events = append(events, e)
	}
	return events, rows.Err()
}

type OutboxMessage struct {
	ID      int64           `json:"id"`
	Topic   string          `json:"topic"`
	Payload json.RawMessage `json:"payload"`
}

func insertEvent(ctx context.Context, tx pgx.Tx, orderID uuid.UUID, from *string, to, reason string) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO order_events (order_id, from_status, to_status, reason)
		VALUES ($1, $2, $3, $4)
	`, orderID, from, to, reason)
	return err
}

func insertOutbox(ctx context.Context, tx pgx.Tx, topic string, payload map[string]any, availableAt time.Time) error {
	b, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO outbox_messages (topic, payload, available_at)
		VALUES ($1, $2, $3)
	`, topic, b, availableAt)
	return err
}
