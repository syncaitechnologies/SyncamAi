# ADR-009: Use Supabase + Cloudflare as the temporary MVP backend

## Status

Accepted on 2026-08-30.

## Decision

For the development MVP, Supabase provides Auth, Postgres, row-level security,
read-only tenant/site context and future realtime UI primitives. Go remains the
only business-mutation boundary: it verifies tokens, sets transaction-local
tenant context, applies domain rules, and atomically persists product state,
the transactional outbox, and hash-chained audit records.

Cloudflare is reserved for a later queue-worker, retry, DLQ and webhook adapter.
The existing provider-neutral `outbox.Publisher` remains that adapter boundary;
this decision creates no Worker, Queue or R2 dependency. Vercel remains the web
deployment target and its existing routing behavior is unchanged.

AWS remains a later migration target. Database, identity and outbox integrations
must stay behind portable Go interfaces so that migration does not change REST
envelopes or domain contracts.

## Consequences

- Supabase CLI migrations in `backend/supabase/migrations` are the authoritative
  schema history. The historical Go embedded migrator is retained only for old
  local test compatibility and is not a deployment mechanism.
- Browser clients can read only their active membership and assigned site context;
  they cannot mutate business tables or access audit/outbox data directly.
- Supabase authorization claims are copied only into `app_metadata.syncam`; the
  Go verifier never authorizes from `user_metadata`.
- Datasets/DVC, R2, AI models, video streaming, footage and production evidence
  storage are explicitly deferred. No remote Supabase, Cloudflare or Vercel
  resource is created by this decision.
