package main

import (
	"encoding/json"
	"log"
	"math/rand"
	"net/http"
	"sync"
	"time"

	"order-pipeline/backend/internal/config"
)

type ChaosConfig struct {
	FailureRate float64 `json:"failure_rate"`
	MinDelayMS  int     `json:"min_delay_ms"`
	MaxDelayMS  int     `json:"max_delay_ms"`
}

type SimState struct {
	mu         sync.RWMutex
	restaurant ChaosConfig
	courier    ChaosConfig
}

func main() {
	addr := config.String("HTTP_ADDR", ":8090")
	state := &SimState{
		restaurant: ChaosConfig{FailureRate: 0.05, MinDelayMS: 100, MaxDelayMS: 700},
		courier:    ChaosConfig{FailureRate: 0.08, MinDelayMS: 100, MaxDelayMS: 900},
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("/restaurant/confirm", state.handleRestaurant("confirm"))
	mux.HandleFunc("/restaurant/prepare", state.handleRestaurant("prepare"))
	mux.HandleFunc("/restaurant/ready", state.handleRestaurant("ready"))
	mux.HandleFunc("/courier/dispatch", state.handleCourier("dispatch"))
	mux.HandleFunc("/courier/deliver", state.handleCourier("deliver"))
	mux.HandleFunc("/chaos/restaurant", state.updateRestaurant)
	mux.HandleFunc("/chaos/courier", state.updateCourier)
	mux.HandleFunc("/chaos", func(w http.ResponseWriter, r *http.Request) {
		state.mu.RLock()
		defer state.mu.RUnlock()
		writeJSON(w, http.StatusOK, map[string]ChaosConfig{"restaurant": state.restaurant, "courier": state.courier})
	})

	log.Printf("simulator listening on %s", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatal(err)
	}
}

func (s *SimState) handleRestaurant(operation string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		s.mu.RLock()
		cfg := s.restaurant
		s.mu.RUnlock()
		handleFlaky(w, r, "restaurant_"+operation, cfg)
	}
}

func (s *SimState) handleCourier(operation string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		s.mu.RLock()
		cfg := s.courier
		s.mu.RUnlock()
		handleFlaky(w, r, "courier_"+operation, cfg)
	}
}

func handleFlaky(w http.ResponseWriter, r *http.Request, operation string, cfg ChaosConfig) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if cfg.MaxDelayMS < cfg.MinDelayMS {
		cfg.MaxDelayMS = cfg.MinDelayMS
	}
	delay := cfg.MinDelayMS
	if cfg.MaxDelayMS > cfg.MinDelayMS {
		delay = cfg.MinDelayMS + rand.Intn(cfg.MaxDelayMS-cfg.MinDelayMS+1)
	}
	time.Sleep(time.Duration(delay) * time.Millisecond)
	if rand.Float64() < cfg.FailureRate {
		status := http.StatusInternalServerError
		if rand.Intn(4) == 0 {
			status = http.StatusTooManyRequests
		}
		writeJSON(w, status, map[string]any{"operation": operation, "ok": false, "delay_ms": delay})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"operation": operation, "ok": true, "delay_ms": delay})
}

func (s *SimState) updateRestaurant(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var cfg ChaosConfig
	if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	normalize(&cfg)
	s.mu.Lock()
	s.restaurant = cfg
	s.mu.Unlock()
	writeJSON(w, http.StatusOK, cfg)
}

func (s *SimState) updateCourier(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var cfg ChaosConfig
	if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	normalize(&cfg)
	s.mu.Lock()
	s.courier = cfg
	s.mu.Unlock()
	writeJSON(w, http.StatusOK, cfg)
}

func normalize(cfg *ChaosConfig) {
	if cfg.FailureRate < 0 {
		cfg.FailureRate = 0
	}
	if cfg.FailureRate > 1 {
		cfg.FailureRate = 1
	}
	if cfg.MinDelayMS < 0 {
		cfg.MinDelayMS = 0
	}
	if cfg.MaxDelayMS < cfg.MinDelayMS {
		cfg.MaxDelayMS = cfg.MinDelayMS
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
