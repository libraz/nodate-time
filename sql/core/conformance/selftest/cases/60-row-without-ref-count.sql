-- A row referencing an object whose count was never raised.
INSERT INTO storage_objects (public_id, owner_user_id, sha256, byte_size, content_type, storage_key, ref_count)
VALUES (UUID_TO_BIN(UUID(), 0), @usr, UNHEX(SHA2('nf-conformance-60b', 256)), 1, 'image/png', 'nf-conformance/60b', 0);
UPDATE users SET avatar_storage_object_id = LAST_INSERT_ID() WHERE id = @usr;
