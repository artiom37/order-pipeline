package load

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"sync/atomic"
	"time"
)

type Request struct {
	OrdersPerSecond int `json:"orders_per_second"`
	DurationSeconds int `json:"duration_seconds"`
}

type Generator struct {
	apiBase string
	active  atomic.Bool
}

func NewGenerator(apiBase string) *Generator {
	return &Generator{apiBase: apiBase}
}

func (g *Generator) Active() bool { return g.active.Load() }

func (g *Generator) Start(ctx context.Context, rate int, duration time.Duration) error {
	if rate <= 0 {
		rate = 1
	}
	if duration <= 0 {
		duration = 30 * time.Second
	}
	if !g.active.CompareAndSwap(false, true) {
		return fmt.Errorf("load generator already running")
	}
	go func() {
		defer g.active.Store(false)
		g.run(ctx, rate, duration)
	}()
	return nil
}

func (g *Generator) RunRush(ctx context.Context) error {
	if !g.active.CompareAndSwap(false, true) {
		return fmt.Errorf("load generator already running")
	}
	go func() {
		defer g.active.Store(false)
		g.run(ctx, 5, 10*time.Second)
		g.run(ctx, 100, 30*time.Second)
		g.run(ctx, 20, 20*time.Second)
	}()
	return nil
}

func (g *Generator) run(ctx context.Context, rate int, duration time.Duration) {
	interval := time.Second / time.Duration(rate)
	if interval < time.Millisecond {
		interval = time.Millisecond
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	deadline := time.NewTimer(duration)
	defer deadline.Stop()

	client := &http.Client{Timeout: 3 * time.Second}
	for {
		select {
		case <-ctx.Done():
			return
		case <-deadline.C:
			return
		case <-ticker.C:
			go postOne(ctx, client, g.apiBase)
		}
	}
}

func postOne(ctx context.Context, client *http.Client, apiBase string) {
	id := fmt.Sprintf("load-%d-%d", time.Now().UnixNano(), rand.Intn(100000))
	payload := map[string]string{
		"idempotency_key": id,
		"customer_name":   fmt.Sprintf("Customer %d", rand.Intn(10000)),
		"restaurant_id":   fmt.Sprintf("restaurant-%02d", rand.Intn(10)+1),
	}
	b, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiBase+"/api/orders", bytes.NewReader(b))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("loadgen request failed: %v", err)
		return
	}
	_ = resp.Body.Close()
}
