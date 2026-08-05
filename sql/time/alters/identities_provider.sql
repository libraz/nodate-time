-- ====================================
-- identities.provider — product-layer widening
--
-- The shared contract enumerates the providers every product implementing it
-- must accept. This product also offers LINE sign-in, which the contract does
-- not name, so the value is added here rather than in core/: widening an ENUM
-- in the product layer keeps the vendored contract byte-identical to upstream
-- while still letting a LINE identity be stored.
--
-- Without this the provider is rejected by the column's own constraint, the
-- sign-in transaction rolls back, and the failure surfaces as a raw error in
-- the middle of a browser redirect.
--
-- oauth_provider_configs.provider already lists 'line' in its own definition
-- because that table belongs to this layer.
-- ====================================
ALTER TABLE identities
  MODIFY COLUMN provider
    ENUM('local','google','github','microsoft','generic_oidc','line')
    NOT NULL COMMENT 'Identity provider kind';
