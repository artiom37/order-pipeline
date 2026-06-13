package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/google/uuid"

	"order-pipeline/backend/internal/config"
	"order-pipeline/backend/internal/db"
	"order-pipeline/backend/internal/orders"
	"order-pipeline/backend/internal/queue"
	"order-pipeline/backend/internal/simulator"
)

const maxAttempts = 5

type WorkMessage struct {
	OrderID string `json:"order_id"`
}

func main() {
	ctx := context.Background()
	databaseURL := config.String("DATABASE_URL", "postgres://order:order@localhost:5432/order_pipeline?sslmode=disable")
	redisAddr := config.String("REDIS_ADDR", "localhost:6379")
	simulatorURL := config.String("SIMULATOR_URL", "http://localhost:8090")
	consumer := config.String("WORKER_NAME", fmt.Sprintf("worker-%d", time.Now().UnixNano()))

	pool, err := db.Connect(ctx, databaseURL)
	if err != nil {
		log.Fatal(err)
	}
	defer pool.Close()
	if err := db.Migrate(ctx, pool); err != nil {
		log.Fatal(err)
	}

	repo := orders.NewRepository(pool)
	rq := queue.NewRedisQueue(redisAddr)
	defer rq.Close()
	if err := rq.EnsureGroup(ctx); err != nil {
		log.Fatalf("ensure redis stream group: %v", err)
	}
	sim := simulator.NewClient(simulatorURL)

	log.Printf("worker started consumer=%s", consumer)
	for {
		msgs, err := rq.Read(ctx, consumer, 10, 2*time.Second)
		if err != nil {
			log.Printf("read stream: %v", err)
			time.Sleep(time.Second)
			continue
		}
		for _, msg := range msgs {
			if err := handle(ctx, repo, sim, msg.Payload); err != nil {
				log.Printf("handle message id=%s: %v", msg.ID, err)
			}
			if err := rq.Ack(ctx, msg.ID); err != nil {
				log.Printf("ack message id=%s: %v", msg.ID, err)
			}
		}
	}
}

func handle(ctx context.Context, repo *orders.Repository, sim *simulator.Client, payload []byte) error {
	var wm WorkMessage
	if err := json.Unmarshal(payload, &wm); err != nil {
		return err
	}
	orderID, err := uuid.Parse(wm.OrderID)
	if err != nil {
		return err
	}
	order, err := repo.GetOrder(ctx, orderID)
	if err != nil {
		return err
	}
	if orders.IsTerminal(order.Status) {
		return nil
	}
	next, ok := orders.NextStatus(order.Status)
	if !ok {
		return nil
	}
	path, operation := downstreamPath(order.Status, next)
	callCtx, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()
	if err := sim.Call(callCtx, path, order.ID.String()); err != nil {
		delay := retryDelay(order.AttemptCount + 1)
		result, scheduleErr := repo.ScheduleRetry(ctx, order.ID, fmt.Sprintf("%s failed: %v", operation, err), delay, maxAttempts)
		if scheduleErr != nil {
			return scheduleErr
		}
		log.Printf("order=%s status=%s downstream=%s result=%s delay=%s err=%v", order.ID, order.Status, operation, result, delay, err)
		return nil
	}

	changed, err := repo.Transition(ctx, order.ID, order.Status, next, operation+" succeeded")
	if err != nil {
		return err
	}
	if changed {
		log.Printf("order=%s transitioned %s -> %s", order.ID, order.Status, next)
	} else {
		log.Printf("order=%s skipped transition %s -> %s; state already changed", order.ID, order.Status, next)
	}
	return nil
}

func downstreamPath(from, to string) (string, string) {
	switch from {
	case orders.StatusPlaced:
		return "/restaurant/confirm", "restaurant_confirm"
	case orders.StatusConfirmed:
		return "/restaurant/prepare", "restaurant_prepare"
	case orders.StatusPreparing:
		return "/restaurant/ready", "restaurant_ready"
	case orders.StatusReady:
		return "/courier/dispatch", "courier_dispatch"
	case orders.StatusOutForDelivery:
		return "/courier/deliver", "courier_deliver"
	default:
		return "/health", "noop"
	}
}

func retryDelay(attempt int) time.Duration {
	switch attempt {
	case 1:
		return time.Second
	case 2:
		return 2 * time.Second
	case 3:
		return 5 * time.Second
	case 4:
		return 10 * time.Second
	default:
		return 20 * time.Second
	}
}
