# ADR-008: composite MVP deployment topology

- Status: Accepted
- Date: 2026-08-12
- Owners: CTO, Platform lead

## Decision

Preserve documented service boundaries as packages and contracts while a 2–3 person team initially deploys three composite Go units: control-plane, event-plane, and media-plane. The AI plane and edge processes remain separate because their runtime, security, and hardware boundaries differ.

## Consequences

- A package may become an independent deployable only for measured scaling, security isolation, or clear team ownership.
- No direct cross-domain database writes are introduced; package boundaries retain APIs and events.
- This reduces early operational load without abandoning the target architecture.
