# ADR-013: Mobile walkthrough for the first three AI workflows

## Status

Engineering decision, 2026-09-29, following the owner's request to open the
web app on a phone and show it to a client. Related task: T-0432.

## Decision

Add a mobile-first, explicitly synthetic walkthrough for person detection,
weapon review and consent-based face attendance. Open it directly with
`?demo=vision`, or from the application navigation. Use deterministic local
illustrations and scripted observations. Store walkthrough state only in
component memory and keep review actions inside the walkthrough.

The face screen illustrates prerequisite checks with fictional labels. It
does not enroll, identify or match a person, collect consent, process faces,
create attendance, or grant a biometric implementation approval. All three
screens identify their observations as simulated rather than measured results.

In live data mode, simulation controls are unavailable. Neither the URL nor
the navigation changes authentication, tenant context, registry status or
server authorization. Existing API domains and contracts remain unchanged.

## Consequences

- A Vercel preview can demonstrate the three workflows without AWS, a GPU,
  camera permissions, uploads, customer data, model weights or runtime secrets.
- Model selection/promotion remains subject to ADR-001. Biometric processing
  remains subject to the existing Phase 5 approval boundary.
- This delivery is a product walkthrough. Real inference still needs licensed
  approved artifacts, an inference runtime and camera-to-alert composition;
  face attendance additionally needs its dedicated consent, liveness,
  enrollment, lifecycle and human-review services.
- The client-facing surface must remain clear about simulation. A deployment
  record or frontend preview cannot be counted as live AI verification.
