# Pilot onboarding guide

This guide helps a pilot customer prepare a site for a controlled SyncCam AI
onboarding review. It does not create a tenant, activate a device, start a
stream, enable a feature, set retention, or establish billing. Keep all
credentials, camera URLs, customer footage, employee data, payment details,
and private network information out of tickets, email threads, and source
control.

## Before requesting onboarding

Name one customer contact who is authorized to confirm the pilot site and one
technical contact who can coordinate the review. The customer should confirm
that use of each camera and monitored area is authorized under its applicable
privacy, employment, and site policies.

Do not send camera passwords, private keys, tokens, Wi-Fi passwords, full RTSP
URLs, floor plans, or footage to the onboarding team. Use the approved secure
channel only after the controlled-provisioning team provides one.

## Site readiness checklist

- Identify the pilot site's operating name, city/region, time zone, and a
  customer contact authorized to approve the site request.
- Identify the intended operational purpose and the camera areas in scope.
  Exclude areas that require a privacy review or are not authorized for the
  stated purpose.
- Confirm local notice, masking, retention, and access requirements with the
  customer's privacy and security owners before any camera is connected.
- Identify who may receive the first administrator invitation. The person must
  use an existing work account and follow the customer’s MFA policy.
- Agree the escalation path for a camera outage, privacy concern, accidental
  activation, or access issue.

## Camera readiness checklist

For each proposed camera, prepare non-sensitive readiness information for the
controlled review: a customer-owned camera label, the site/area it serves,
camera vendor/model, expected video codec, and the local contact responsible
for it. Do not include a serial number, IP address, MAC address, RTSP URL, or
credential in this guide or an unapproved ticket.

- Confirm the camera is customer-authorized, physically secured, and assigned
  to the approved site and purpose.
- Confirm the customer can provide a supported H.264 or H.265 video stream
  through the approved secure setup process. SyncCam’s edge transport uses
  TCP by default; network and firewall details are reviewed separately.
- Confirm the camera view has been reviewed for privacy-sensitive areas and
  that any required mask or exclusion-zone work is complete before activation.
- Confirm a safe maintenance window and local contact are available for the
  controlled activation review.
- Plan a rollback: the customer can disconnect the approved integration if the
  review identifies an authorization, privacy, or operational problem.

## Controlled-provisioning handoff

The browser onboarding shell is a request-preparation tool only. It does not
create an organization, site, user, role, invitation, tenant, device claim, or
camera integration. Submit the completed non-sensitive request through the
approved customer-success process.

The authorized provisioning team verifies the request, performs any approved
administrative procedure, and records the appropriate audit/change reference.
If any identity, tenant, site, device, consent, privacy, security, or billing
prerequisite is missing or unclear, the request remains pending rather than
being provisioned or activated.

## Pilot FAQ

### Can the pilot use attendance or face recognition?

No. Those capabilities remain blocked until the required Product,
Privacy/Legal, Security, AI-release, evaluation, and human-oversight approvals
are complete. A pilot plan or onboarding request cannot bypass these gates.

### Can we send a camera password or stream URL to support?

No. Use only the approved secure setup channel provided by the authorized
provisioning team. Never put secrets in email, chat, ticket comments, browser
forms, logs, or source control.

### Does submitting the onboarding request start billing?

No. Billing, invoices, payment collection, and entitlement enforcement remain
separate, approved Phase 7 work. The Phase 7 billing boundary does not activate
commercial terms.

### What happens if a required approval is missing?

The request remains pending. SyncCam must not activate a device, start a stream,
collect payment, or enable a gated capability as a workaround.
