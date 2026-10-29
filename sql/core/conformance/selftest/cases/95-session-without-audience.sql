-- A session no application claims.
INSERT INTO sessions (public_id, user_id, refresh_hash, expires_at)
VALUES (UUID_TO_BIN(UUID(), 0), @usr, SHA2('nf-conformance-95', 256), NOW(3) + INTERVAL 1 DAY);
