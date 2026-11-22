# OVM-269 — асимметричная подпись access JWT

## Решение и план

Ed25519/EdDSA через существующую golang-jwt/v5. Users хранит active private seed,
kid и не больше 8 public keys с перекрытием ротации. JWKS отдаёт только публичные
OKP/Ed25519 keys. Все пути выпуска (login/register/refresh) добавляют audience.
Access TTL нового режима 15 минут по умолчанию, допустимы 1–15 минут.

Gateway получает ключи по фиксированному JWKS URL (HTTPS; HTTP только loopback
для тестов), ограничивает размер/число ключей, не следует redirect/jku/x5u.
Кэш 1 минута, unknown kid refresh не чаще 5 секунд, истёкший кэш при сбое
не используется. Durable session check Users сохраняется (cache 5 секунд,
перед защищёнными mutation — свежая проверка).

HS256 остаётся старым режимом для совместимости, но при включённом Ed25519
его проверка разрешена только с явным legacy deadline, не больше 24 часов.
После перехода JWT_SECRET не нужен gateway. Подписанный login IP контекст
использует отдельный USERS_LOGIN_CONTEXT_SECRET на Users/gateway.

1. Keys/sign/verify/JWKS Users + rotation/audience/kid tests.
2. Config и wiring Users, issuer/audience/TTL/legacy validation.
3. Gateway JWKS/cache/validation и отдельный login-context secret.
4. Проверки Users/gateway, межсервисный контракт, review, CI/merge/Jira.

Никакого deployment/смены реальных ключей. Соседние docs/infra прочитаны
из основного workspace. Следующая задача — OVM-271.

## Настройки

Users: JWT_SIGNING_SEED (base64 32 случайных байт, только Users),
JWT_SIGNING_KID (1–64 латинских буквы/цифры/`_.-`), JWT_OVERLAP_PUBLIC_KEYS
(необязательный JSON object kid → base64 public key, всего <=8 ключей).
JWT_ISSUER по умолчанию users, JWT_AUDIENCE — overmindv-gateway;
JWT_TTL — 15m. JWKS доступен по GET /.well-known/jwks.json.

Gateway: JWT_JWKS_URL и те же JWT_ISSUER/JWT_AUDIENCE. Частные ключи/seed в
gateway не передавать. На обоих сервисах USERS_LOGIN_CONTEXT_SECRET — отдельный
общий секрет 32–4096 байт без пробелов; это MAC контекста IP, не подпись JWT.
JWT_SECRET допускается в новом режиме только вместе с абсолютным RFC3339
JWT_LEGACY_HS256_UNTIL, не более 24 часов от запуска. Пустой deadline отключает
HS256 и требует убрать JWT_SECRET. Частичная/невалидная конфигурация не запускается.

## Миграция и отзыв

1. Обновить оба сервиса, оставаясь в прежнем режиме. Задать отдельный одинаковый
   USERS_LOGIN_CONTEXT_SECRET на Users и gateway, проверить обычный вход.
2. Настроить HTTPS для JWKS. Перевести gateway на JWKS URL с текущим HS256 secret
   и явным deadline (до 24h), сохранив текущие сессии на время перехода.
3. Включить signing seed/kid в Users с тем же legacy deadline. Все новые access
   токены, включая refresh, станут Ed25519 и будут жить не больше 15 минут.
4. После deadline удалить JWT_SECRET/legacy deadline из обоих сервисов.
   Не переключать обратно в старый режим ради продления старых токенов.

Для обычной ротации заранее опубликовать новый public key на всех Users-репликах
в overlap, затем поменять active seed/kid, сохранив прежний public key. Убрать
прежний key после максимального TTL старых access токенов и завершения rollout.
Один kid никогда не переиспользовать для другого ключа.

При компрометации убрать key из всех Users-реплик и JWKS, сменить активный key.
Gateway обновляет public-key cache не позднее минуты; обязательный session check
в Users отклонит подпись удалённого key раньше (до 5 секунд кэша для чтений,
свежая проверка для защищённых mutations). Refresh может перевыпустить access
под новым ключом; при компрометации аккаунта дополнительно отозвать его сессии.
Отказ Users/JWKS не даёт бессрочно использовать старый cache.

## Межсервисная проверка

После сборки обоих бинарников локальный контракт запускается так:

```sh
python3 scripts/check_signing_contract.py \
  --users-bin /tmp/ovm269-users --gateway-bin /tmp/ovm269-gateway \
  --database 'postgres://postgres:postgres@127.0.0.1:55452/ovm269_contract_test?sslmode=disable'
```

Скрипт принимает только loopback `_test` БД, применяет миграции, создаёт тестовый
аккаунт, запускает оба процесса на свободных портах с тестовыми ключами и
останавливает их. Проверяет JWKS → gateway `me` → refresh → gateway `me`
без JWT_SECRET. Это локальный auth-контур, не полный browser/stack E2E и не deployment.

## Проверки 22.11.2026

Users: make test (race, domain/usecase coverage 84.5%), integration, build,
lint 0, go vet — exit 0. Gateway: make test (race, coverage 65.5%), integration,
build, lint 0, vet — exit 0. Новые тесты проверяют rotation/overlap, audience,
unknown kid, legacy deadline, cache expiry, malformed/duplicate/private JWKS,
размер ответа, redirect и все пути выпуска сессий/refresh.

Локальный межсервисный скрипт прошёл на живых Users/Gateway с PostgreSQL 17:
JWKS, регистрация с refresh, gateway me, refresh, gateway me без JWT_SECRET.
75 OpenSpec, 11 tests документации, lint 0. Независимый review без P1/P2.
Реальные ключи/TLS ingress/browser/production rollout не проверялись;
CI и merge фиксируются в Jira.
