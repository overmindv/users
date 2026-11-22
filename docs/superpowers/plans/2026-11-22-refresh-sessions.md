# OVM-267 — одноразовые refresh-сессии Users

Пользователь поручил самостоятельно выбирать и выполнять задачи ядра tasks/recommendations/users. Исполнитель Alexander Vasyukov; отдельная ветка codex/ovm-267-refresh-sessions. Task-hunter больше не расширяем.

## Решение
Расширить существующие durable sessions: случайный refresh (32 байта crypto/rand, base64url), в БД только SHA-256. Family имеет абсолютный срок 30 дней; rotation не продлевает этот срок. Каждый refresh используется один раз. Повтор старого использованного token отзывает family и все её access sessions. Два одновременных refresh: один может успеть получить ответ, второй отзывает family, поэтому выданный первым JWT уже недействителен. Отмена/ошибка БД до commit откатывает rotation. Потеря ответа после commit требует нового входа. Изменение password/roles/deletion запрещает дальнейшее продление через security_version.

Существующие login/register остаются совместимыми. Новый необязательный флаг refreshSession включает выдачу пары; AuthPayload получает nullable refreshToken/refreshExpiresAt. Mutation refreshSession работает по bearer-secret без access JWT, revokeRefreshSession идемпотентно отзывает family. Logout по access JWT также отзывает его family. Отдельные входы других устройств не затрагиваются.

Users — внутренний сервис. Gateway/frontend пока не запрашивают эти поля и не меняются: для браузерного автопродления потребуется отдельное подключение защищённой httpOnly cookie в gateway, запрет передачи refresh в JS и сериализация refresh. Нельзя объявлять автопродление в UI реализованным этим срезом.

Альтернативы: бессрочный refresh без rotation не выполняет критерий Jira; замена всей авторизации расширяет область. Выбрано расширение существующего хранилища с opt-in.

## По-коммитный план
1. Schema/domain/storage: новая append-only migration, domain session types, postgres refresh store, sessions logout, component-тесты. Проверки real PostgreSQL: rotation/reuse/concurrent/expiry/security change/rollback. Существующие очистки тестовой БД механически дополняются новыми FK-таблицами (объяснённое превышение 7 файлов при необходимости).
2. Auth (4 файла): выпуск/валидация opaque token, подпись durable access; unit-тесты.
3. Usecase (4 файла): login/register opt-in, refresh/revoke; unit-тесты. Проверки make test и integration, coverage >=80%.
4. GraphQL/wiring (7 файлов): schema, resolver/mapper, generated код, app wiring, transport tests. Generated файлы — неделимая механическая группа. make generate, lint, build, полный ctest, независимый review.
5. Документация: CLAUDE и этот handoff (2 файла).

## Ограничения и порядок внедрения
Миграция до нового API. Старые access-сессии без family сохраняют прежнюю семантику. Не логировать/включать токены в ошибки или отчёты. Отдельная тестовая БД, общий стек не трогаем. Локальные sibling docs отсутствуют; прочитаны исходные спецификации session-revocation и gateway-session-check в /Users/alevasyukovv/Desktop/Overmindv/docs/openspec/specs.


## Handoff — 2026-11-22

Реализовано локально; commit/push и развёртывание — разные этапы, публикации нет.
Storage и API-тесты разделены по commit-группам. Дополнены только test fixtures с FK.
OpenSpec находится в отдельном docs worktree ovm-267-refresh-spec, change
`rotate-users-refresh-sessions`; исходный docs/main не изменён.

Проверки (exit 0): `make generate`, `make build`, `make test` (race, coverage 84.8%),
`make ctest` на отдельном PostgreSQL 16; TestRefresh* дополнительно с `-race`.
`golangci-lint` 2.11.4: 0 issues. Генератор/линтер/build/full tests выполнены Go 1.26.11
(Go 1.27 несовместим с закреплёнными инструментами). Docs: npm ci, check (68/68),
11 Python tests, markdown lint — 0 issues. `git diff --check` чистый.

Независимый review выявил fallible GetByID после commit ротации: исправлено переносом
profile snapshot в ту же транзакцию. Повторный review: actionable замечаний нет.
Есть regression на отсутствие отдельного чтения профиля после ротации.

Не проверено: production, CI/full-stack e2e и браузерное автопродление.
Следующий шаг: review локальных commits и миграции, затем отдельный BFF/browser срез
(httpOnly cookie, CSRF, coalescing). Задачи Лёни и Task Hunter не изменялись этим срезом.
