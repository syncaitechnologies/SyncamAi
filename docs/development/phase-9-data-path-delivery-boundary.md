# Phase 9 data-path delivery boundary

T-0397 begins planning for the historical Sprint 9 data-path maturity work. It
defines the decisions and evidence that a future implementation proposal must
have; it does not implement a storage path, retention job, erasure job, load
test, synthetic fleet, backup, restore, autoscaling rule, or deployment.

## Canonical context

The roadmap's Sprint 9 calls for ClickHouse cold analytics, TimescaleDB
partitioning, tenant-scoped retention and erasure, synthetic-fleet load tests,
autoscaling and cost monitoring, and backup/restore validation. The
architecture keeps PostgreSQL as the relational source of truth and describes
derived analytics stores as projections. It also requires tenant-scoped
partitioning, lifecycle policies, and erasure across stores.

The roadmap's 1,000-camera and event-rate figures are planning targets, not
current capacity evidence or a production-service-level claim. A future test
design must select an approved environment, workload, safeguards, success
criteria, and rollback conditions before generating any test traffic.

## Scope and non-goals

This task creates no database, table, partition, retention setting, lifecycle
rule, queue or stream configuration, synthetic device, event, camera, tenant,
customer record, workload, benchmark, performance result, backup, restore,
cost record, credential, API, browser route, deployment configuration, or
production-enablement claim.

It does not choose a data store, storage tier, region, retention period,
erasure exception, legal-hold rule, recovery objective, load-test volume,
autoscaling threshold, budget, or launch date.

## Required approvals before implementation

All of the following must be explicitly recorded before a Phase 9
implementation or test task can begin:

1. Product, Platform, and Security approve the authoritative data ownership,
   tenant boundaries, permitted derived projections, and the safe failure and
   rollback behavior. A derived analytics path must not become a second
   authoritative record by implication.
2. Privacy and Legal approve retention schedules, deletion and legal-hold
   exceptions, backup/restore treatment, customer notice and contract terms,
   and the jurisdiction-specific evidence required for erasure. The existing
   architecture's retention values are design inputs, not an approved policy.
3. Security and Privacy approve the future erasure and restore reconciliation
   design, including how a restored copy cannot silently reintroduce erased
   tenant data, how immutable evidence exceptions are bounded, and which
   non-sensitive audit fields may be retained.
4. Platform, Security, and Finance approve an isolated non-production
   performance-test environment, workload guardrails, cost ceiling, monitoring,
   access controls, test-data provenance, incident contacts, and a cleanup
   procedure. Tests must not use customer footage, customer credentials,
   production tenants, or live device endpoints.
5. Platform and Security approve the backup and restore test plan, including
   access controls, encryption, tenant-isolation checks, recovery measurement,
   failure handling, and evidence retention. No recovery objective is claimed
   until a separately approved and executed test records suitable evidence.

## Future implementation constraints

- Every future data path, partition, prefix, cache key, index, queue, and
  restore operation must preserve the canonical tenant boundary. An unknown or
  cross-tenant reference must fail closed and be safely auditable.
- A future retention or erasure worker must reconcile every approved store and
  derived projection. Missing coverage, an ambiguous legal hold, a failed
  deletion, or an unverified restore condition cannot be represented as a
  completed erasure.
- A future workload must use approved synthetic or otherwise authorized data.
  It must not introduce footage, personal data, biometric templates, model
  artifacts, customer secrets, or production traffic into a benchmark.
- A future test or restore must have scoped credentials, bounded concurrency,
  a stop condition, monitored cost, and an approved cleanup plan. It must not
  change production capacity, retention, data, or deployment state.
- Performance, reliability, cost, and recovery observations are evidence only
  after their methodology and review are approved. They must not be used to
  promote a service, model, capability, or customer commitment by default.

## Relationship to the roadmap

This boundary preserves the Phase 9 order: establish authoritative ownership,
privacy and recovery decisions, and a safe test design before proposing any
data-path or scale implementation. It does not unblock the historical Sprint
9 deliverables or revise the existing Phase 5 biometric gates.

## Next safe slice: T-0398

T-0398 provides a non-authorizing [retention, erasure, and recovery review
template](../governance/phase-9-retention-erasure-recovery-review-template.md).
It structures the future lifecycle and recovery decision without recording a
customer or personal-data item, decision, approval, benchmark evidence,
configuration, or implementation claim.
