# Запросы экспорта и удаления OVM-273

Механизм выключен по умолчанию. `USERS_PRIVACY_ENABLED=true` включает внутренние
REST endpoints и worker. `USERS_PRIVACY_OWNERS` — JSON object с ключами `tasks`,
`recommendations`, `content`, `media`, `rating`, `feed`, `sandbox`; каждое значение
содержит `url` и отдельный `token` длиной 32–4096 символов. Нужны HTTPS URL
(loopback HTTP допустим для тестов), уникальные service tokens и Kafka-конфигурация
Parker для публикации `user.deleted.v1`. Отсутствующий consumer не считается готовым.

Сначала миграция `202611220006`, затем новый Users. При включённом механизме старый
GraphQL `deleteUser` отклоняется: удаление проходит только через подтверждение.
Gateway/UI — отдельный OVM-111. Настройки production здесь не менялись.

## Владелец аккаунта

1. `POST /internal/privacy/requests`, Bearer active access JWT, JSON
   `{"kind":"export","password":"current password"}` или kind `delete`.
   Subject берётся из проверенной сессии. Не более 5 password-попыток на subject
   и 50 на доверенный IP за 15 минут; не более 5 созданных запросов в сутки.
2. Ответ содержит `request` и случайный `receipt` (32 random bytes, base64url).
   В БД только SHA-256. Receipt нельзя писать в логи или URL.
3. `POST /internal/privacy/requests/{id}/confirm`, header `X-Privacy-Receipt`,
   JSON `{"confirm":true}`. Подтвердить в течение 15 минут; исходная сессия и
   security version должны оставаться активными. Повтор подтверждения возвращает
   ту же операцию. Создание нового запроса того же вида отменяет прежний pending.
4. `GET /internal/privacy/requests/{id}` с receipt показывает state, publication
   и owners. Receipt действует 24 часа; дальнейший операционный статус доступен
   владельцу сервиса в БД. После удаления access JWT больше не нужен для receipt.
5. Готовый экспорт: `GET /internal/privacy/requests/{id}/export` с активным JWT
   того же пользователя. Receipt сам по себе не разрешает скачивание.

Все ответы `no-store`. Смена пароля/ролей или отзыв исходной сессии до подтверждения
отменяет операцию. Удаление атомарно обезличивает профиль, отзывает access/refresh,
удаляет recovery credentials и grants, отменяет экспорты и создаёт журнал/outbox.
Последнего пригодного администратора защищает существующий DB guard.

## Владельцы данных

Экспорт: Users вызывает `GET {url}/internal/privacy/export` с параметрами
`user_id`, `cursor`, `watermark`, `limit=100` и `X-Privacy-Service-Token`.
Первая страница имеет пустые cursor/watermark. Ответ:

```json
{
  "user_id": "subject UUID",
  "watermark": "stable snapshot",
  "next_cursor": "",
  "records": []
}
```

Запись: обязательные `user_id`, `id` (UUID), `kind`, `occurred_at` (RFC3339);
необязательные `resource_id` (UUID), `title` (до 1000 байт), `text` (до 64 KiB, для Tasks до 256 KiB),
`result` (до 2000 байт). Других полей нет. Допустимые kind:

| Сервис | Kind |
| --- | --- |
| tasks | submission, attempt, task_revision, task_review |
| recommendations | study_activity, plan, topic_profile |
| content | draft, contribution |
| media | file |
| rating | score |
| feed | entry |
| sandbox | execution |

Владелец возвращает только данные subject. Чужие публикации, ответы, скрытые
тесты и credentials запрещены. Users проверяет subject, kind, allowlist полей,
стабильный watermark, продвижение cursor. 100 записей/2 MiB на страницу,
1000 страниц на владельца, 10 MiB на запрос. Превышение — ошибка, не обрезанный
«успешный» экспорт. Artifact живёт максимум 24 часа от создания запроса; cleanup
работает пакетами. Поля профиля выбраны явно, password/token hashes не экспортируются.

Удаление: Kafka topic `user.deleted.v1`, key=user UUID. Конверт содержит event_id,
event_type, schema_version=1, aggregate_id, aggregate_version, occurred_at и payload
`{user_id,request_id,tombstone:true}`. Event ID совпадает с request ID и неизменен
при retry. Доставка at-least-once; consumer обязан сохранить tombstone до очистки,
блокировать поздние события и идемпотентно обработать повтор.

После обработки consumer вызывает `POST /internal/privacy/deletions/{id}/ack`
со своим `X-Privacy-Service-Token`, JSON `{"state":"completed"}` или
`{"state":"failed","error_code":"processing_failed"}` (`policy_pending` также
допустим). Owner определяется токеном; его нельзя указать в body. Completed
не регрессирует при позднем failed. Общий state completed требует всех owners.
Публикация в Kafka сама по себе не означает, что сервисы удалили данные.

`GET /internal/privacy/deletions?after=0&watermark=0&limit=100` с тем же service
token даёт постоянный журнал: `watermark`, `next_after`, `events`. Все страницы
читать с первым watermark, продолжать с next_after. Счётчик сериализован с commit,
журнал не очищается по TTL. Перед восстановлением/replay consumer применяет
актуальные tombstones, иначе старые данные могут вернуться.

## Повторы и эксплуатация

Worker: lease 1 минута, UUID fencing, 8 попыток на страницу/публикацию,
backoff 2–256 секунд; итерация 20 секунд. Ошибки upstream не сохраняются в logs.
Export с failed-владельцем не выдаётся; пользователь может запросить новый.
Для повторной публикации оператор может вернуть failed deletion outbox в pending:

```sql
UPDATE user_outbox_events
SET status='pending', attempts=0, claim_token=NULL, locked_at=NULL,
    available_at=clock_timestamp(), last_error=''
WHERE id='00000000-0000-0000-0000-000000000001'::uuid
  AND event_type='user.deleted.v1' AND status='failed';
```

Заменить UUID целевой операцией. UUID fence не даёт прежнему worker завершить
новую попытку. Журнал доступен consumer независимо от Kafka retry.

OVM-128 остаётся черновиком: правила audit, backup и физических копий согласуются
отдельно. Существующий append-only security audit и минимальные ledger/session
идентификаторы сохраняются; этот producer не объявляет их физически стёртыми.
Включение для реальных данных требует завершения политики и consumer-задач.
Локально проверены PostgreSQL, HTTP contract и mock publication; реальная Kafka,
owner endpoints и полный сквозной export/delete здесь не запускались.
