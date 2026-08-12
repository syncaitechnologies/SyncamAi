# ADR-003: event backbone taxonomy

- Status: Accepted
- Date: 2026-08-12
- Owners: Platform lead, SRE

## Decision

Use Kinesis for the hot event ingest path, MSK for replayable streams, SQS/SNS for alert fast-path and fan-out, and RabbitMQ for asynchronous task queues. At-least-once delivery plus idempotent consumers and `dedupe_key` is canonical.

## Consequences

- Postgres is the relational source of truth; other stores are projections.
- Every queue has a dead-letter path, metrics, and replay procedure.
- Event contracts are versioned under `shared/contracts` and compatibility-tested in CI.
