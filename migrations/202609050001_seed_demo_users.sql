-- +goose Up
-- Демо-данные: набор пользователей, чтобы платформа была заполнена.
-- Пароли хранятся в открытом виде вида "<username>123" (см. security.PlainTextHasher — прототип).
INSERT INTO users (id, email, password_hash, username, first_name, last_name, birth_date, phone, roles, is_superuser, created_at, updated_at)
VALUES
    ('00000000-0000-0000-0000-000000000001', 'ivan.smirnov@example.com',  'ivan123',   'ivan.smirnov',   'Ivan',    'Smirnov',   '1995-03-14', '+79001112233', ARRAY[]::text[],            false, NOW() - INTERVAL '10 days', NOW() - INTERVAL '10 days'),
    ('00000000-0000-0000-0000-000000000002', 'anna.rodina@example.com',  'anna123',   'anna.rodina',   'Anna',    'Rodina',   '2007-03-01', '+79260001122', ARRAY[]::text[],            false, NOW() - INTERVAL '9 days',  NOW() - INTERVAL '9 days'),
    ('00000000-0000-0000-0000-000000000003', 'petr.petrov@example.com',   'petr123',   'petr.petrov',   'Petr',    'Petrov',    '1993-11-05', '+79164567890', ARRAY[]::text[],            false, NOW() - INTERVAL '9 days',  NOW() - INTERVAL '9 days'),
    ('00000000-0000-0000-0000-000000000004', 'maria.sokolova@example.com','maria123',  'maria.sokolova','Maria',   'Sokolova',  '2000-01-30', '+79997775544', ARRAY[]::text[],            false, NOW() - INTERVAL '8 days',  NOW() - INTERVAL '8 days'),
    ('00000000-0000-0000-0000-000000000005', 'yaroslav.voronenko@example.com','yaroslav123','yaroslav.voronenko','Yaroslav','Voronenko', '1990-05-18', '+79851234567', ARRAY[]::text[],           false, NOW() - INTERVAL '8 days',  NOW() - INTERVAL '8 days'),
    ('00000000-0000-0000-0000-000000000006', 'elena.popova@example.com',  'elena123',  'elena.popova',   'Elena',   'Popova',    '1996-09-09', '+79639998877', ARRAY[]::text[],            false, NOW() - INTERVAL '7 days',  NOW() - INTERVAL '7 days'),
    ('00000000-0000-0000-0000-000000000007', 'dmitri.volkov@example.com', 'dmitri123', 'dmitri.volkov', 'Dmitri',  'Volkov',    '1988-12-01', '+79035432100', ARRAY[]::text[],           false, NOW() - INTERVAL '6 days',  NOW() - INTERVAL '6 days'),
    ('00000000-0000-0000-0000-000000000008', 'olga.fedorova@example.com', 'olga123',   'olga.fedorova',  'Olga',    'Fedorova',  '1999-04-27', '+79115556677', ARRAY[]::text[],           false, NOW() - INTERVAL '5 days',  NOW() - INTERVAL '5 days'),
    ('00000000-0000-0000-0000-000000000009', 'alexei.morozov@example.com','alexei123', 'alexei.morozov','Alexei',  'Morozov',   '1985-02-11', '+79262223344', ARRAY['admin']::text[],    false, NOW() - INTERVAL '4 days',  NOW() - INTERVAL '4 days'),
    ('00000000-0000-0000-0000-000000000010', 'natalia.evsyukova@example.com','natalia123','natalia.evsyukova','Natalia','Evsyukova',  '2007-07-08', '+79648880011', ARRAY[]::text[],          false, NOW() - INTERVAL '3 days',  NOW() - INTERVAL '3 days');

-- +goose Down
DELETE FROM users WHERE id IN (
    '00000000-0000-0000-0000-000000000001',
    '00000000-0000-0000-0000-000000000002',
    '00000000-0000-0000-0000-000000000003',
    '00000000-0000-0000-0000-000000000004',
    '00000000-0000-0000-0000-000000000005',
    '00000000-0000-0000-0000-000000000006',
    '00000000-0000-0000-0000-000000000007',
    '00000000-0000-0000-0000-000000000008',
    '00000000-0000-0000-0000-000000000009',
    '00000000-0000-0000-0000-000000000010'
);