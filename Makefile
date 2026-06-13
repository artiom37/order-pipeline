.PHONY: up down logs test fmt rush load-small degrade-restaurant recover-restaurant degrade-courier recover-courier

up:
	docker compose up --build

down:
	docker compose down -v

logs:
	docker compose logs -f api worker dispatcher simulator

test:
	cd backend && go test ./...

fmt:
	cd backend && gofmt -w $$(find . -name '*.go')

load-small:
	curl -X POST http://localhost:8080/api/load/start -H "Content-Type: application/json" -d '{"orders_per_second":5,"duration_seconds":30}'

rush:
	curl -X POST http://localhost:8080/api/load/rush

degrade-restaurant:
	curl -X POST http://localhost:8080/api/chaos/restaurant -H "Content-Type: application/json" -d '{"failure_rate":0.75,"min_delay_ms":500,"max_delay_ms":2500}'

recover-restaurant:
	curl -X POST http://localhost:8080/api/chaos/restaurant -H "Content-Type: application/json" -d '{"failure_rate":0,"min_delay_ms":100,"max_delay_ms":700}'

degrade-courier:
	curl -X POST http://localhost:8080/api/chaos/courier -H "Content-Type: application/json" -d '{"failure_rate":0.65,"min_delay_ms":500,"max_delay_ms":3000}'

recover-courier:
	curl -X POST http://localhost:8080/api/chaos/courier -H "Content-Type: application/json" -d '{"failure_rate":0,"min_delay_ms":100,"max_delay_ms":800}'
