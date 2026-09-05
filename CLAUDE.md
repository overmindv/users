# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Service overview

`users` — identity-сервис платформы Overmindv. Владеет пользователями, паролями, профилями, ролями и выпуском JWT. Является единственным источником истины по ролям; api-gateway читает роли из JWT и проксирует управление пользователями обратно в Users.

Ключевой момент архитектуры: **один image, два процесса**. `users` (API, GraphQL) и `users-worker` (фоновый доставщик avatar outbox) запускаются из одного бинарника `cmd/users`. API обслуживает GraphQL, worker периодически опрашивает transactional outbox и переключает avatar binding в Media. Workers запускается только если заданы `MEDIA_URL`, `MEDIA_USERS_TOKEN`, `USERS_WORKER_HTTP_ADDR`.

## Common Commands

```bash
make run                       # запуск сервиса (пarker-каркас: HTTP, health, metrics, миграции)
make build                     # go build ./...
make generate                  # генерация GraphQL через gqlgen (из api/graphql/schema.graphqls)
make test                      # unit-тесты с -race + coverage для domain/usecase с порогом 80%
make integration               # интеграционные тесты без внешней БД (tests/integration)
make ctest                     # component-тесты, требуют PostgreSQL (DSN через COMPONENT_TEST_DSN)
make lint                      # golangci-lint run
make db-up / db-down           # накатить/откатить миграции (goose через бинарник users)

# Одиночный unit-тест (без Docker)
go test -v ./internal/usecase/ -run TestName

# Component-тесты с отдельной БД
make ctest COMPONENT_TEST_DSN='postgres://postgres:postgres@localhost:5432/users?sslmode=disable'
```

## Architecture

Слои следуют модели user_service → repository, все зависимости проходят через `internal/app/container.go` (`Build` выполняет wiring на каркас parker).

```
internal/
├── app/container.go        # wiring: БД, JWT, media-client, usecase, bootstrap, worker, GraphQL
├── auth/                   # JWT manager + middleware (NewManager, OptionalHTTP)
├── config/                 # бизнес-конфиг из env (JWT/Media/Bootstrap); инфраструктуру владеет parker
├── delivery/graphql/       # GraphQL-транспорт: resolver, mapper, schema.resolvers, generated/
├── domain/                 # сущность User + value objects, доменные ошибки
├── security/               # PasswordHasher (PlainTextHasher — только для dev)
├── media/                  # клиент Media (проверка avatar, готовность)
├── repository/postgres/    # UserRepository + AvatarOutbox (transactional outbox)
├── usecase/                # бизнес-логика: UserService, BootstrapSuperuser, DTO, contracts
└── worker/avatar.go        # фоновый доставщик avatar outbox в Media
```

- **Транспорт** — GraphQL (gqlgen). Схема в `api/graphql/schema.graphqls`; сгенерированный код в `internal/delivery/graphql/generated`. JWT применяется к `/query` и `/graphql`, но не к `/playground` (auth.OptionalHTTP).
- **Каркас** — `github.com/overmindv/parker`: владеет инфраструктурным конфигом (HTTP, PostgreSQL, логирование, метрики, health-чеки), миграциями и graceful shutdown. Users добавляет только бизнес-настройки и регистрирует health-чек `media`.
- **Bootstrap суперпользователя** — `EnsureBootstrapSuperuser` выполняется в `Build` до старта HTTP, чтобы первый админ был доступен сразу.
- **Transactional outbox аватаров** — перед изменением avatar сервис проверяет готовый публичный файл через Media, затем атомарно сохраняет `avatar_file_id` и outbox-событие в одной транзакции; `users-worker` идемпотентно переключает binding в Media. Временная недоступность Media не теряет изменение.
- **Миграции** — goose, файлы в `migrations/`, прокатываются через сам бинарник (`go run ./cmd/users migrate ...`).

## Config (env)

Обязательные: `JWT_SECRET`, `MEDIA_URL`, `MEDIA_USERS_TOKEN`. Опциональные: `JWT_ISSUER`, `JWT_TTL`, `MEDIA_TIMEOUT`, `USERS_WORKER_POLL_INTERVAL`, `BOOTSTRAP_SUPERUSER_*`. Инфраструктурные переменные (HTTP, PostgreSQL, логирование) читает parker.

## Notes

- Пароли шифруются через `security.PasswordHasher`; `PlainTextHasher` используется только для локальной разработки — не использовать в проде.
- Изменение схемы GraphQL требует `make generate` (gqlgen), измение мапперов в `delivery/graphql/mapper.go`.
