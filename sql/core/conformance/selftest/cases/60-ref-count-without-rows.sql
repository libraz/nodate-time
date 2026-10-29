-- An object claiming a reference no row holds.
INSERT INTO storage_objects (public_id, workspace_id, sha256, byte_size, content_type, storage_key, ref_count)
VALUES (UUID_TO_BIN(UUID(), 0), @ws, UNHEX(SHA2('nf-conformance-60a', 256)), 1, 'text/plain', 'nf-conformance/60a', 1);
