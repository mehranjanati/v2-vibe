-- P0.6 credential hardening: D1 must hold ciphertext only.
-- Rows written before the vault store `github_tokens.access_token` as the
-- plaintext GitHub OAuth token; the Worker now writes `enc:v1:` AES-256-GCM
-- envelopes (worker/light/tokenVault.ts) and re-seals legacy rows lazily on
-- read. Purge anything still plaintext here so no unread row keeps a live
-- token — affected users simply re-authorize on the next export.
-- (`linked_identities` has no migration in this chain, so nothing to purge.)
DELETE FROM `github_tokens` WHERE substr(`access_token`, 1, 7) <> 'enc:v1:';
