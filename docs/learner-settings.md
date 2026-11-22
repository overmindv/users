# OVM-272 — настройки учащегося

## Контракт и решения

Users хранит timezone (IANA, default UTC), locale (валидный BCP 47, default ru-RU),
publicName и leaderboardVisible (оба false по умолчанию). Владелец меняет их
через существующий updateUser; User DTO доступен владельцу/admin. PublicUser
не содержит email, timezone или locale. Имя/фамилия скрыты без publicName;
displayName в таком случае равен публичному username. Публичный поиск также
не использует скрытые имя/фамилию. LeaderboardVisible — opt-in контракт для
потребителя рейтинга, а не реализация самого рейтинга.

Цели/mastery остаются в recommendations. Сроки хранения и удаления из OVM-128
ведёт Леонид; этот срез их не меняет, данные принадлежат жизненному циклу аккаунта.
Регион, юридические требования и запуск очистки не определяются здесь.

## План

- [x] Домен: валидировать timezone/locale, безопасные defaults, patch настроек.
- [x] PostgreSQL: расширяющая миграция, сохранение настроек и privacy-aware поиск.
- [x] GraphQL: owner settings, public displayName и флаг рейтинга, schema generation.
- [x] Проверить owner/public/admin visibility, неверные зоны/локали, roundtrip и поиск.
- [x] Unit/race/coverage, integration, component, lint/build, независимый review.
- [ ] CI перед merge; ссылка и результат фиксируются в Jira.

Проверено 22.11.2026 на Go 1.26.11: make test (race, coverage 85.0%),
make integration, make build, make lint (0 issues), go vet ./... — exit 0.
make ctest на выделенной PostgreSQL 17 — exit 0. Повторная генерация gqlgen
не меняет generated файлы. Review обнаружил чтение чужого приватного User
обычным actor: отрицательный тест воспроизвёл ошибку, owner/admin guard её
исправил, полный набор проверок повторён успешно. Deployment, живой gateway/UI
и будущий рейтинг не проверялись. Следующая задача очереди — OVM-205.

Новые поля схемы добавляются совместимо; старые клиенты продолжают получать
прежние поля, но firstName/lastName публично пусты до явного opt-in владельца.
Gateway и UI настройки профиля могут использовать новые поля отдельным срезом;
в этом тикете реализуется контракт сервиса Users, как указано в Jira.
