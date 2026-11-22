# OVM-268 — восстановление доступа и подтверждение email

## Решение

Users владеет одноразовыми случайными 256-bit reset/verification tokens.
В таблице токенов хранится только SHA-256 digest, назначение, email/version
аккаунта и TTL (reset 30 минут, verification 24 часа). Запрос выдаёт одинаковый
ответ для существующего, отсутствующего и ограниченного по частоте email.
Токен возвращается только в письме, никогда в GraphQL ответе или логах.

Письмо пишется в transactional outbox вместе с токеном. Payload зашифрован
AES-256-GCM отдельным ключом, поскольку worker должен восстановить токен для
отправки. SMTP требует TLS; доставка имеет lease/fencing и ограниченные retries.
Недоступный SMTP не откатывает запрос, повтор письма не делает токен многоразовым.

Reset атомарно потребляет токен, меняет пароль и отзывает access/refresh сессии.
Проверка email не меняет текущую политику разрешения входа. Старый security
version, удалённый аккаунт, чужое назначение, истёкший/использованный токен
отклоняются одинаково. Пароль 8–1024 байта; лимиты перед дорогим Argon2id.

Функция выключена без SMTP и ключа outbox. Реальные письма и deployment не
выполняются при разработке; component tests используют собственные аккаунты.

Для пилота выбран Brevo SMTP: опубликованный Free тариф — 300 писем/сутки,
transactional продукт заявляет размещение во Франции и Германии.
Источник проверен 22.11.2026: [тариф](https://help.brevo.com/hc/en-us/articles/208580669-FAQs-What-are-the-limits-of-the-Free-plan),
[SMTP и размещение](https://www.brevo.com/products/transactional-email/).
Это выбор адаптера для пилота, не смена региона платформы. Аккаунт провайдера,
домен отправителя и секреты не создавались; код не привязан к его API.
При исчерпании бесплатной квоты провайдер может задержать письмо дольше TTL:
токен всё равно истечёт, пользователь сможет запросить новый.

## Контракт и включение

GraphQL Users: requestAccountRecovery(email, purpose: "reset" | "verify")
возвращает boolean без токена; resetAccountPassword(token,password) и
verifyAccountEmail(token) потребляют соответствующий токен. Лимиты запросов:
5/email, 100/IP за 15 минут; consume — 5/token, 20/IP за 15 минут. IP берётся
из существующего проверенного gateway-контекста или TCP peer.

Задать все USERS_RECOVERY_SMTP_ADDRESS (host:587 с STARTTLS),
USERS_RECOVERY_SMTP_USERNAME, USERS_RECOVERY_SMTP_PASSWORD,
USERS_RECOVERY_FROM и USERS_RECOVERY_MAIL_KEY (base64 случайных 32 байт).
Частичная конфигурация останавливает запуск. Письмо содержит код, не ссылку
с секретом в URL. Gateway и frontend формы восстановления — отдельный срез
потребителей Users API; текущий тикет ограничен схемой/логикой Users.

Каждый новый запрос того же назначения отменяет предыдущее письмо. Worker
пытается доставить не больше 8 раз, backoff 10–900 секунд, lease 1 минута.
Повтор SMTP после потери подтверждения допустим: токен одноразовый. После
успешной отправки ciphertext очищается; использованные/истёкшие токены и
outbox удаляются пакетами по 1000. Смена ключа outbox требует предварительно
дождаться завершения/истечения старой очереди (максимум 24 часа); ключи не
должны попадать в git или логи. Политика хранения других данных не меняется.

## План по слоям

1. Usecase: request/reset/verify, validation, throttling, regression tests.
2. PostgreSQL: tokens + encrypted outbox, atomic consume/session revocation;
   отдельные component сценарии replay/expiry/конкурентности.
3. SMTP adapter/worker: TLS, bounded retry, claim fencing; outage tests.
4. GraphQL/config/wiring и generation; owner/public границы.
5. Проверки make test/integration/ctest/lint/build, review, CI; merge/Jira.

Соседний docs читается из основного workspace; в изолированном worktree его нет.
Следующий пункт очереди — OVM-269 (асимметричные JWT).

## Проверки 22.11.2026

Go 1.26.11: make test (race, domain/usecase coverage 84.5%), integration,
build, lint (0 issues), go vet ./... — exit 0. Полный make ctest на свежей
выделенной PostgreSQL 17 — exit 0. Старые fixture TRUNCATE расширены новыми
FK-таблицами; проверки аудита/последнего администратора не ослаблены.
Повторная генерация gqlgen не меняет generated файлы. OpenSpec 74/74,
tests документации 11/11 и lint — успешно. Независимый review без P1/P2.

Проверены expired/used/wrong-purpose/version, отзыв access/refresh, generic
GraphQL request, шифрование и tamper, SMTP outage, отказ plaintext SMTP,
fencing повторного claim, bounded retry и cleanup. Успешная живая SMTP/TLS
доставка, конкурентный reset/login/refresh, gateway/frontend и deployment
не проверялись. CI и merge фиксируются в Jira.
