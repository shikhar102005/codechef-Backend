-- Migration 002: Seed demonstration data

-- Seed Users
-- Password hashes use bcrypt for 'Password123!' -> $2a$10$7AcvG5rW... but for demo compatibility we also handle direct verify or standard bcrypt
INSERT INTO users (id, email, full_name, password_hash, role)
VALUES 
    ('11111111-1111-1111-1111-111111111111', 'admin@example.com', 'System Administrator', '$2a$10$w8I6tTms1bYF7mQ8.m0p3eR1o5p19iPqA0r3g6N8b5f3d7h9j1k2l', 'ADMIN'),
    ('22222222-2222-2222-2222-222222222222', 'user1@example.com', 'Alice Engineer', '$2a$10$w8I6tTms1bYF7mQ8.m0p3eR1o5p19iPqA0r3g6N8b5f3d7h9j1k2l', 'CUSTOMER'),
    ('33333333-3333-3333-3333-333333333333', 'user2@example.com', 'Bob Architect', '$2a$10$w8I6tTms1bYF7mQ8.m0p3eR1o5p19iPqA0r3g6N8b5f3d7h9j1k2l', 'CUSTOMER')
ON CONFLICT (email) DO NOTHING;

-- Seed Events
INSERT INTO events (id, title, description, venue, category, banner_url)
VALUES
    ('a0000000-0000-0000-0000-000000000001', 'Global Tech Summit 2026', 'The premier international conference on distributed computing, systems architecture, and AI engineering.', 'Grand Convention Center, San Francisco', 'Conference', 'https://images.unsplash.com/photo-1540575467063-178a50c2df87?auto=format&fit=crop&w=1200&q=80'),
    ('a0000000-0000-0000-0000-000000000002', 'Symphony Under the Stars', 'An enchanting evening of classical masterpieces performed by the National Symphony Orchestra.', 'Royal Philharmonic Hall, London', 'Concert', 'https://images.unsplash.com/photo-1514525253161-7a46d19cd819?auto=format&fit=crop&w=1200&q=80'),
    ('a0000000-0000-0000-0000-000000000003', 'Championship Esports Arena Finals', 'The world championship grand finals featuring elite teams battling in tactical strategy games.', 'Cyber Arena, Tokyo', 'Esports', 'https://images.unsplash.com/photo-1511512578047-dfb367046420?auto=format&fit=crop&w=1200&q=80')
ON CONFLICT (id) DO NOTHING;

-- Seed Sessions
INSERT INTO sessions (id, event_id, title, start_time, end_time, status)
VALUES
    ('b0000000-0000-0000-0000-000000000001', 'a0000000-0000-0000-0000-000000000001', 'Day 1 - Keynote & Systems Track', NOW() + INTERVAL '2 days', NOW() + INTERVAL '2 days 8 hours', 'ACTIVE'),
    ('b0000000-0000-0000-0000-000000000002', 'a0000000-0000-0000-0000-000000000001', 'Day 2 - AI Infrastructure Track', NOW() + INTERVAL '3 days', NOW() + INTERVAL '3 days 8 hours', 'ACTIVE'),
    ('b0000000-0000-0000-0000-000000000003', 'a0000000-0000-0000-0000-000000000002', 'Evening Gala Performance', NOW() + INTERVAL '5 days', NOW() + INTERVAL '5 days 3 hours', 'ACTIVE'),
    ('b0000000-0000-0000-0000-000000000004', 'a0000000-0000-0000-0000-000000000003', 'Grand Championship Finals', NOW() + INTERVAL '7 days', NOW() + INTERVAL '7 days 5 hours', 'ACTIVE')
ON CONFLICT (id) DO NOTHING;

-- Seed Seats for Session 1 (Day 1 Keynote)
-- Rows A (VIP: $250), B (PREMIUM: $150), C & D (STANDARD: $95), E (BALCONY: $65)
-- We use a DO block to generate seats cleanly for all sessions
DO $$
DECLARE
    sess_id UUID;
    r VARCHAR;
    s INT;
    cat VARCHAR;
    price INT;
BEGIN
    FOR sess_id IN SELECT id FROM sessions LOOP
        -- Row A (VIP, seats 1-8)
        FOR s IN 1..8 LOOP
            INSERT INTO seats (id, session_id, row_label, seat_number, seat_category, base_price_cents, status)
            VALUES (gen_random_uuid(), sess_id, 'A', s, 'VIP', 25000, 'AVAILABLE')
            ON CONFLICT (session_id, row_label, seat_number) DO NOTHING;
        END LOOP;

        -- Row B (PREMIUM, seats 1-10)
        FOR s IN 1..10 LOOP
            INSERT INTO seats (id, session_id, row_label, seat_number, seat_category, base_price_cents, status)
            VALUES (gen_random_uuid(), sess_id, 'B', s, 'PREMIUM', 15000, 'AVAILABLE')
            ON CONFLICT (session_id, row_label, seat_number) DO NOTHING;
        END LOOP;

        -- Row C (STANDARD, seats 1-10)
        FOR s IN 1..10 LOOP
            INSERT INTO seats (id, session_id, row_label, seat_number, seat_category, base_price_cents, status)
            VALUES (gen_random_uuid(), sess_id, 'C', s, 'STANDARD', 9500, 'AVAILABLE')
            ON CONFLICT (session_id, row_label, seat_number) DO NOTHING;
        END LOOP;

        -- Row D (STANDARD, seats 1-10)
        FOR s IN 1..10 LOOP
            INSERT INTO seats (id, session_id, row_label, seat_number, seat_category, base_price_cents, status)
            VALUES (gen_random_uuid(), sess_id, 'D', s, 'STANDARD', 9500, 'AVAILABLE')
            ON CONFLICT (session_id, row_label, seat_number) DO NOTHING;
        END LOOP;

        -- Row E (BALCONY, seats 1-12)
        FOR s IN 1..12 LOOP
            INSERT INTO seats (id, session_id, row_label, seat_number, seat_category, base_price_cents, status)
            VALUES (gen_random_uuid(), sess_id, 'E', s, 'BALCONY', 6500, 'AVAILABLE')
            ON CONFLICT (session_id, row_label, seat_number) DO NOTHING;
        END LOOP;
    END LOOP;
END $$;
