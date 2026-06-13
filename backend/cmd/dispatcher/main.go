package main

import (
	"context"
	"log"
	"time"

	"order-pipeline/backend/internal/config"
	"order-pipeline/backend/internal/db"
	"order-pipeline/backend/internal/orders"
	"order-pipeline/backend/internal/queue"
)

func main() {
	ctx := context.Background()
	databaseURL := config.String("DATABASE_URL", "postgres://order:order@localhost:5432/order_pipeline?sslmode=disable")
	redisAddr := config.String("REDIS_ADDR", "localhost:6379")

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

	log.Println("dispatcher started")
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for range ticker.C {
		messages, err := repo.ClaimOutbox(ctx, 100)
		if err != nil {
			log.Printf("claim outbox: %v", err)
			continue
		}
		for _, m := range messages {
			if err := rq.Publish(ctx, m.Payload); err != nil {
				log.Printf("publish outbox id=%d: %v", m.ID, err)
				_ = repo.MarkOutboxPending(ctx, m.ID)
				continue
			}
			if err := repo.MarkOutboxPublished(ctx, m.ID); err != nil {
				log.Printf("mark outbox published id=%d: %v", m.ID, err)
			}
		}
	}
}
