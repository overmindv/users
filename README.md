# Arcee

Сервис отвечающий за работу с пользоватлеями: вход, регистрация, обновление.

## Конфигурация

| Переменная | Стандартно | Значение |
|---|---|---|
| `PORT` | `8080` | HTTP порт |
| `DATABASE_URL` | локальный PostgreSQL DSN | PostgreSQL подключение |
| `JWT_SECRET` | - | HS256 секрет подключения |
| `JWT_ISSUER` | `arcee` | JWT издатель |
| `JWT_TTL` | `24h` | время жизни токена |
| `DB_MAX_CONNS` | `20` | максимальное кол-во подключений pgx |
| `DB_MIN_CONNS` | `2` | минимальное кол-во подключений pgx |

`JWT_SECRET` должен быть идентичным в Arcee и Laserbeak.

## Локальная разработка

```bash
export POSTGRES_PASSWORD=postgres
export JWT_SECRET=local-development-secret-change-me
docker compose up --build
```

Контейнер миграции запускает файлы goose, встроенные в образ Arcee, перед запуском приложения. Для запуска на хосте:

```bash
make migrate-up DATABASE_URL='postgres://postgres:postgres@localhost:5432/arcee?sslmode=disable'
make run
```

## Команды

```bash
make generate
make test
make ctest
make build
```
