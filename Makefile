PKGS_TEST := ./cmd/... ./graph ./internal/...

.PHONY: test test-all fmt

test:
	go test $(PKGS_TEST)

test-all:
	go test ./...

fmt:
	gofmt -w cmd/server/main.go cmd/server/lidarr_cleanup.go internal/agent/executor.go internal/db/postgres.go internal/db/sync.go
