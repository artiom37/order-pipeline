package main

import (
	"context"
	"flag"
	"log"
	"time"

	"order-pipeline/backend/internal/load"
)

func main() {
	apiBase := flag.String("api", "http://localhost:8080", "API base URL")
	rate := flag.Int("rate", 10, "orders per second")
	duration := flag.Duration("duration", 30*time.Second, "duration")
	rush := flag.Bool("rush", false, "run dinner rush pattern")
	flag.Parse()

	gen := load.NewGenerator(*apiBase)
	if *rush {
		if err := gen.RunRush(context.Background()); err != nil {
			log.Fatal(err)
		}
		for gen.Active() {
			time.Sleep(time.Second)
		}
		return
	}
	if err := gen.Start(context.Background(), *rate, *duration); err != nil {
		log.Fatal(err)
	}
	for gen.Active() {
		time.Sleep(time.Second)
	}
}
