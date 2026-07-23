LOCAL_BIN := $(CURDIR)/bin
GOOSE := $(LOCAL_BIN)/goose
GQLGEN := $(LOCAL_BIN)/gqlgen
GOLANGCI_LINT := $(LOCAL_BIN)/golangci-lint
DATABASE_URL ?= postgres://postgres:postgres@localhost:5432/arcee?sslmode=disable
COMPONENT_TEST_DSN ?= $(DATABASE_URL)

.PHONY: run build test integration ctest lint db-up db-down generate graphql

# Запуск
run: 
	go run ./cmd/arcee

# Сборка
build: 
	go build ./...

# Запуск тестов
test: 
	go test -race ./...
	go test -coverprofile=coverage.out ./internal/domain ./internal/usecase
	@awk 'BEGIN { total=0; covered=0 } !/^mode:/ { total += $$2; covered += $$2 * $$3 } END { pct=100*covered/total; printf "domain/usecase coverage: %.1f%%\n", pct; if (pct < 80) exit 1 }' coverage.out

# Запуск интеграционных тестов без внешней БД
integration:
	go test ./tests/integration/...

# Запуск компонентных тестов
ctest: $(GOOSE)
	$(GOOSE) -dir migrations postgres "$(COMPONENT_TEST_DSN)" up
	COMPONENT_TEST_DSN="$(COMPONENT_TEST_DSN)" go test -tags=component ./tests/integration/...

# Проверка линтером
lint: $(GOLANGCI_LINT)
	$(GOLANGCI_LINT) run

# Поднять миграции
db-up: $(GOOSE)
	$(GOOSE) -dir migrations postgres "$(DATABASE_URL)" up

# Откатить миграцию
db-down: $(GOOSE)
	$(GOOSE) -dir migrations postgres "$(DATABASE_URL)" down

# Сгенерировать GraphQL
generate: graphql 

graphql: $(GQLGEN)
	$(GQLGEN) generate

$(GOOSE):
	GOBIN="$(LOCAL_BIN)" go install github.com/pressly/goose/v3/cmd/goose@v3.24.3

$(GQLGEN):
	GOBIN="$(LOCAL_BIN)" go install github.com/99designs/gqlgen@v0.17.81

$(GOLANGCI_LINT):
	GOBIN="$(LOCAL_BIN)" go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.1.6
