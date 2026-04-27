.PHONY: dev run test tidy

dev:
	go run github.com/air-verse/air@latest -c .air.toml

run:
	go run ./cmd/api

test:
	go test ./...

test-frontend:
	node --test web/admin/app.test.js

tidy:
	go mod tidy
