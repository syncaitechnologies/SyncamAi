# Phase 3 configuration delivery

`T-0331` delivers zone configuration to enrolled edge devices without requiring inbound connectivity to customer sites.

- An authorized `POST /v1/zones/{id}/push` snapshots all current zones for that zone's site into an immutable `config.configuration_revisions` row. The snapshot is metadata-only and hash-addressed.
- A device receives the latest revision number as an authenticated heartbeat hint, then pulls the full newer snapshot over its existing TLS 1.3 mTLS connection using `GET /v1/edge/devices/{id}/config?after_revision=N`.
- The edge validates the SHA-256 content hash, writes and fsyncs a temporary file, then atomically activates it. An invalid or failed apply leaves the last accepted revision active and reports `failed`; an accepted revision reports `applied`.
- Device status is tenant/site-scoped and stored under RLS. The device SQL functions validate its active certificate state and site before exposing or accepting a revision.

This slice does not deliver privacy-mask zones. Those remain blocked on Super Admin authorization, dual approval, immutable security auditing, and proof that masking happens before encoding.
