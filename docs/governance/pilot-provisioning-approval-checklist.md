# Pilot-provisioning approval checklist

## Status

Preparation only. This checklist grants no approval and does not provision a
tenant, site, user, role, invitation, device, camera, stream, billing record,
or entitlement.

## Purpose

Use this checklist before a controlled pilot-provisioning procedure is
considered. The customer-facing pilot onboarding guide gathers a non-sensitive
request; it is not a provisioning authority. The browser onboarding shell also
remains unable to create any business state.

The completed review must be retained in the approved customer-success and
security systems. This repository may contain only a non-sensitive evidence
reference. Do not add customer names, tenant or site identifiers, camera
details, network information, credentials, footage, personal data, payment
details, or support records here.

## Product and customer approval

- [ ] Confirm the approved pilot purpose, authorized sites, camera areas, and
  capability scope.
- [ ] Confirm the customer’s authorized business and technical contacts, their
  escalation path, and the customer’s acceptance of the controlled setup
  process.
- [ ] Confirm a non-biometric pilot scope unless every separate Phase 5
  biometric approval and release gate has been met. A pilot cannot bypass those
  gates.
- [ ] Confirm the customer has identified privacy-sensitive areas and required
  operational limitations before any camera connection is considered.

## Privacy, Legal, and commercial approval

- [ ] Confirm the applicable privacy notice, site authorization, masking or
  exclusion requirements, retention expectations, and customer responsibilities
  through the approved Privacy and Legal review.
- [ ] Confirm the customer contract, pilot terms, support boundaries, and any
  data-processing terms through the approved Legal process.
- [ ] If the pilot has commercial terms, confirm that the Finance/Legal-approved
  price, currency, tax, invoice, payment, credit/refund, and entitlement rules
  exist. Otherwise, record that no billing or entitlement action is authorized.
- [ ] Confirm that no restricted, unapproved, or customer-specific term is
  copied into source control, public documentation, browser state, or logs.

## Security and platform approval

- [ ] Confirm the authorized provisioning procedure follows ADR-010 and the
  tenant-safe platform controls: no unprovisioned browser user, client-side
  credential, generic SQL command, or support request may create privileged
  state.
- [ ] Confirm the designated tenant/site scope, least-privilege roles, MFA
  expectations, audit/change reference, and rollback or suspension owner.
- [ ] Confirm device and camera setup use the approved certificate/claim and
  secure setup process; do not provide or record secrets, RTSP URLs, private
  network details, or full device identifiers in this checklist.
- [ ] Confirm missing, invalid, ambiguous, cross-tenant, or unsupported inputs
  fail closed and remain pending rather than creating access or activation.

## Support and operational approval

- [ ] Name the operational owner, support escalation path, maintenance window,
  incident procedure, and safe customer communication channel.
- [ ] Confirm the customer can stop or disconnect the approved setup if a
  privacy, authorization, security, or operational issue is identified.
- [ ] Confirm the pilot’s monitoring and support expectations do not claim a
  production service level, availability commitment, or feature release.
- [ ] Record only decision owners, dates, scope, and non-sensitive evidence
  references in the approved review systems.

## Required outcome

The completed review may only recommend one of the following:

- **Remain pending:** any evidence, approval, scope, identity, privacy,
  security, commercial, device, or operational item is missing or unclear.
- **Consider controlled provisioning:** every prerequisite is approved and in
  scope. A separately authorized procedure must still perform each privileged
  mutation and audit it; this checklist does not authorize those mutations.

Until that separately authorized procedure is approved and executed, no pilot
customer is provisioned and no device, camera, stream, billing record,
entitlement, or gated capability is activated.
