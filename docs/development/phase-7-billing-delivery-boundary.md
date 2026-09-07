# Phase 7 billing delivery boundary

T-0393 begins Phase 7 planning for the billing and pilot-operations work. It
defines prerequisites only; it does not implement a billing service, plan,
meter, invoice, tax calculation, payment, entitlement, or quota.

## Canonical commercial context

The business strategy selects a hybrid commercial architecture: a per-site
platform fee, per-managed-camera module fee, and optional add-ons. It also
lists five plan concepts. The prices, currency conversions, annual-prepay
discount, storage charge, and Plan E premium in that document are explicitly
marked as assumptions to be re-validated at GA; they are not executable prices
or an approved billing catalog.

Plan A includes a future attendance capability. That capability stays blocked
by the Phase 5 consent, Privacy/Legal, Security, AI-release, evaluation, and
human-oversight gates. A billing plan must not be used to activate attendance,
face recognition, enrollment, matching, or liveness.

## Scope and non-goals

This task creates no product plan, tenant entitlement, price, meter, usage
record, invoice, tax treatment, payment intent, provider customer, provider
account, secret, webhook, refund, dunning action, API, browser route,
activation, deployment configuration, or production claim.

It does not choose Razorpay, Stripe, a merchant-of-record model, a launch
country, a tax jurisdiction, an invoice template, a currency, a billing
schedule, a discount, a refund policy, or an entitlement rule.

## Required approvals before implementation

All of the following must be recorded and approved before a billing
implementation task can begin:

1. Product and founders approve the launch regions, customer segments, plan
   availability, billing cadence, trial policy, entitlement behavior, upgrade
   and downgrade treatment, and which roadmap capabilities may be sold. Any
   plan that references a gated capability must keep it unavailable until its
   own release approvals are complete.
2. Finance and Legal approve final price books, currencies, tax registration
   and collection responsibilities, GST/invoice requirements, credit/refund
   and dunning policies, record retention, and customer contract terms for
   every launch jurisdiction.
3. Legal, Finance, and Security approve the payment-provider selection,
   merchant onboarding, applicable data-processing terms, payout and dispute
   process, provider webhook verification design, and incident contacts.
4. Platform, Security, and Product approve the future tenant-scoped usage
   definition and reconciliation method against authoritative operational
   aggregates. A usage disagreement, missing source, or ambiguous tenant/site
   scope must fail closed rather than producing a charge or changing an
   entitlement automatically.
5. Security approves least privilege, tenant isolation, append-only audit
   fields, provider-secret handling, and incident/reconciliation paths. A
   browser, source tree, logs, URLs, generic events, and ordinary analytics
   must not contain provider secrets or full payment data.

## Future implementation constraints

- A future billing service must keep commercial state tenant-scoped and must
  not trust browser-supplied price, plan, usage, or payment status.
- Provider credentials belong only in a separately scoped server runtime and
  approved secret manager. They must not be committed, sent to the browser, or
  included in a command line, log, audit payload, or support export.
- A future payment-provider webhook must be independently authenticated,
  idempotent, replay-protected, tenant-scoped, and audited with safe result
  codes only.
- A future usage meter may report a bounded, documented operational measure;
  it must not create a charge, invoice, or entitlement change until the
  approved reconciliation and business rules exist.
- A missing approval, unverifiable provider event, unknown price/version,
  cross-tenant reference, or reconciliation mismatch must fail closed. It may
  enter a documented manual-review state, but cannot silently collect payment
  or activate a capability.

## Relationship to the Phase 7 roadmap

The historical Sprint 7 roadmap calls for billing plans, metering, invoices,
Razorpay/Stripe, usage dashboards, onboarding, pilot provisioning, and model
registry work. This boundary does not make any of those items live. It is the
first dependency-safe step before a separate, approved billing implementation
task can be proposed.

## Next safe Phase 7 slice: T-0394

T-0394 adds a customer-safe [pilot onboarding guide](../customer/pilot-onboarding-guide.md).
It helps a pilot prepare a non-sensitive controlled-provisioning request but
does not create a tenant, device, camera integration, stream, billing record,
or entitlement.
