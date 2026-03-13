.PHONY: build test lint migrate up down

SERVICES := gateway user tweet timeline

build:
	@for svc in $(SERVICES); do \
		echo "Building $$svc..."; \
		go build -o bin/$$svc ./cmd/$$svc; \
	done

test:
	go test ./...

lint:
	golangci-lint run ./...

up:
	docker compose up --build -d

down:
	docker compose down -v

migrate:
	@echo "Applying migrations..."
	@for f in pkg/db/migrations/*.sql; do \
		echo "Running $$f..."; \
		psql $$POSTGRES_PRIMARY_URL -f $$f; \
	done

run-gateway:
	PORT=8080 go run ./cmd/gateway

run-user:
	PORT=8081 go run ./cmd/user

run-tweet:
	PORT=8082 go run ./cmd/tweet

run-timeline:
	PORT=8083 go run ./cmd/timeline

.PHONY: tidy
tidy:
	go mod tidy
