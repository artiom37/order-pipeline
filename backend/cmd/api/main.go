package main

import (
	"bytes"
	"context"
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"order-pipeline/backend/internal/config"
	"order-pipeline/backend/internal/db"
	"order-pipeline/backend/internal/load"
	"order-pipeline/backend/internal/orders"
	"order-pipeline/backend/internal/queue"
	"order-pipeline/backend/internal/simulator"
	"order-pipeline/backend/internal/sse"
)

func main() {
	ctx := context.Background()
	databaseURL := config.String("DATABASE_URL", "postgres://order:order@localhost:5432/order_pipeline?sslmode=disable")
	redisAddr := config.String("REDIS_ADDR", "localhost:6379")
	simulatorURL := config.String("SIMULATOR_URL", "http://localhost:8090")
	addr := config.String("HTTP_ADDR", ":8080")

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
	broker := sse.NewBroker()
	generator := load.NewGenerator("http://localhost" + addr)
	simClient := simulator.NewClient(simulatorURL)

	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("/ready", func(w http.ResponseWriter, r *http.Request) {
		ready := map[string]string{"postgres": "ok", "redis": "ok", "simulator": "ok"}
		status := http.StatusOK
		if err := pool.Ping(r.Context()); err != nil {
			ready["postgres"] = err.Error()
			status = http.StatusServiceUnavailable
		}
		if err := rq.Ping(r.Context()); err != nil {
			ready["redis"] = err.Error()
			status = http.StatusServiceUnavailable
		}
		if err := simClient.Health(r.Context()); err != nil {
			ready["simulator"] = err.Error()
			status = http.StatusServiceUnavailable
		}
		writeJSON(w, status, ready)
	})
	mux.HandleFunc("/metrics", func(w http.ResponseWriter, r *http.Request) {
		d, err := repo.Dashboard(r.Context())
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, d.Totals)
	})
	mux.Handle("/api/events", broker)
	mux.HandleFunc("/api/orders", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			var in orders.CreateOrderInput
			if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			order, created, err := repo.CreateOrder(r.Context(), in)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			broker.Broadcast("order_event", map[string]any{"order_id": order.ID, "to_status": order.Status, "created": created})
			status := http.StatusCreated
			if !created {
				status = http.StatusOK
			}
			writeJSON(w, status, order)
		case http.MethodGet:
			list, err := repo.ListOrders(r.Context(), 100)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			writeJSON(w, http.StatusOK, list)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	})
	mux.HandleFunc("/api/orders/", func(w http.ResponseWriter, r *http.Request) {
		idStr := strings.TrimPrefix(r.URL.Path, "/api/orders/")
		id, err := uuid.Parse(idStr)
		if err != nil {
			http.Error(w, "invalid order id", http.StatusBadRequest)
			return
		}
		order, err := repo.GetOrder(r.Context(), id)
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		writeJSON(w, http.StatusOK, order)
	})
	mux.HandleFunc("/api/dashboard", func(w http.ResponseWriter, r *http.Request) {
		d, err := repo.Dashboard(r.Context())
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, d)
	})
	mux.HandleFunc("/api/load/start", func(w http.ResponseWriter, r *http.Request) {
		var req load.Request
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if err := generator.Start(context.Background(), req.OrdersPerSecond, time.Duration(req.DurationSeconds)*time.Second); err != nil {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		writeJSON(w, http.StatusAccepted, map[string]any{"status": "started", "active": generator.Active()})
	})
	mux.HandleFunc("/api/load/rush", func(w http.ResponseWriter, r *http.Request) {
		if err := generator.RunRush(context.Background()); err != nil {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		writeJSON(w, http.StatusAccepted, map[string]any{"status": "rush_started", "active": generator.Active()})
	})
	mux.HandleFunc("/api/chaos/restaurant", proxyChaos(simulatorURL+"/chaos/restaurant"))
	mux.HandleFunc("/api/chaos/courier", proxyChaos(simulatorURL+"/chaos/courier"))

	go func() {
		ticker := time.NewTicker(1 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			d, err := repo.Dashboard(context.Background())
			if err == nil {
				broker.Broadcast("dashboard", d)
			}
		}
	}()

	log.Printf("api listening on %s", addr)
	if err := http.ListenAndServe(addr, cors(mux)); err != nil {
		log.Fatal(err)
	}
}

func proxyChaos(target string) http.HandlerFunc {
	client := &http.Client{Timeout: 5 * time.Second}
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		var reqBody bytes.Buffer
		_, _ = reqBody.ReadFrom(r.Body)
		req, err := http.NewRequestWithContext(r.Context(), http.MethodPost, target, bytes.NewReader(reqBody.Bytes()))
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := client.Do(req)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()
		var respBody bytes.Buffer
		_, _ = respBody.ReadFrom(resp.Body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(resp.StatusCode)
		_, _ = w.Write(respBody.Bytes())
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		w.Header().Set("Access-Control-Allow-Methods", "GET,POST,OPTIONS")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

