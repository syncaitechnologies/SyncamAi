# Phase 12 test-only privacy-rights intake boundary

T-0429 introduces a pure in-memory validation boundary for the first metadata
record of a future individual privacy request. It translates the draft
individual privacy-request procedure into a testable contract without treating
the draft policy as an approved operating workflow.

## What it validates

`privacyrights.CreateTestDraft` accepts only:

1. opaque UUIDv4 case, tenant and subject references;
2. an optional opaque UUIDv4 requester reference, so later private intake can
   support people without an application account;
3. one bounded request kind from the procedure: access, correction, erasure,
   withdrawal, grievance, nomination, portability, opt-out or limitation; and
4. caller-injected synthetic policy metadata in the exact `test_only` mode,
   including bounded jurisdiction, purpose, version and legal-clock references
   plus a bounded synthetic deadline interval.

It produces only an unpersisted `verification_pending` case draft. No caller
can supply a name, email, narrative, document, evidence reference, consent,
biometric, authentication result, fulfillment outcome or completion state.

## Deliberate boundaries

This package is not wired into HTTP, browser UI, a database, RLS, audit, outbox
or any worker. It cannot receive a real request, verify authority, assign an
owner, calculate an approved legal deadline, send a response, stop processing,
withdraw consent, delete data, apply a hold, inspect a store, create a manifest
or make a legal determination. It never introduces biometric processing.

The injected policy's IDs and deadline are synthetic test inputs, not a claim
that a jurisdictional schedule or service-level promise has been approved.

## Required before a production slice

Privacy/Legal and the product owner must approve the intake channel, requester
and authorized-agent verification, jurisdiction/purpose schedule, legal holds,
staff owners, private case/evidence store, response delivery and escalation.
After that, a later task can add a tenant-scoped transaction, audit/outbox,
RLS/migration, accessible UI and E2E tests. It must preserve the existing
non-biometric and fail-closed boundaries.
