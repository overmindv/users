-- Для просмотра списка пользователей в локальной БД. Вход запрещен.
INSERT INTO users (id, email, password_hash, username, first_name, last_name, created_at, updated_at)
SELECT gen_random_uuid(), format('demo-%s@example.invalid', n), '$argon2id$locked',
       format('demo_%s', n), 'Demo', n::text, NOW(), NOW()
FROM generate_series(1, 10) AS n
ON CONFLICT DO NOTHING;
