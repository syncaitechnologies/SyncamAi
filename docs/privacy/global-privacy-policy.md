# Global SyncCam AI privacy policy

> Draft operational privacy policy — requires review and approval by qualified legal/privacy counsel before production use in the applicable jurisdiction.

Version: `2026-09-08.draft-2`. Not effective for production. [Implementation status](privacy-implementation-traceability.md) is part of this draft.

## Identity, scope and responsibility

SyncCam AI is the product maintained in the SyncamAi repository by SyncAi Technologies. The contracting legal entity, registered address and monitored privacy contact have not been verified: **[legal entity, address, privacy contact — required before publication]**. This draft covers the web application, Go control-plane services, edge software and proposed supporting operations. It does not establish that every component is deployed or that any customer has been approved.

For customer-directed camera use, the customer generally determines the purposes and acts as controller, Data Fiduciary or business, as applicable. SyncCam generally processes that information on documented instructions as processor/service provider. A reviewed contract must allocate notice, lawful collection, security, requests, incident cooperation and return/deletion responsibilities. Labels alone do not determine legal roles. If SyncCam independently determines purposes for its own account administration, service security or business relationships, it must assess its own responsibilities and provide appropriate notice. Responsibility cannot be transferred entirely to the customer.

## Information and purposes supported by inspected code

The following describes software capabilities, not a verified inventory of production records:

| Category | Source and information | Purpose and current limit |
|---|---|---|
| Account and access | User identity/email from the configured identity provider; tenant/site membership, roles, scopes and MFA/session information | Authenticate users and enforce access. The browser supports Supabase PKCE/session persistence. |
| Tenant/site and camera/device configuration | Organization/site names, location/timezone settings, camera identifiers and configuration, device claims/certificates, health metadata, zones and versioned configuration | Configure authorized devices and site rules; support device health and permissions. A region label is not proof of data localization. |
| Events and alerts | Tenant/site/camera/zone identifiers, time, event type, model-version label, confidence, review status, evidence references and limited vehicle classes/behavior | Present observations for human review, deduplicate, acknowledge and deliver alerts. An accepted event is not proof of an approved AI model. |
| Audit and delivery | Actor/request/resource identifiers, timestamps, action/state, hash-chain records and delivery/reconciliation metadata | Attribute changes and diagnose bounded workflow outcomes. Not a universal access log or WORM archive. |
| Video/evidence capability | Edge RTSP libraries can decode video; spool libraries can hold opaque payloads. Generic events contain references. | Current edge command is a scaffold; browser evidence/streaming and approved cloud evidence storage are not complete. No deployed recording inventory is verified. |
| Attendance and biometrics | Dedicated employee attendance, face images/templates, liveness and biometric consent services are not implemented | Proposed future categories only. Separate notice, necessity, consent, jurisdiction, model and security approval are required before collection. |

Do not submit camera passwords, tokens, footage, biometric material or identity documents through generic event fields, onboarding drafts, public issues or logs. Ordinary video of a person may itself be personal information; lack of facial identification does not make surveillance anonymous.

## Grounds, choice and human review

Each customer deployment must document a specific purpose and jurisdiction-specific ground before collection. Consent is not a substitute for necessity or a prohibited activity, and accepting general terms does not authorize biometrics. The [India](india-privacy-supplement.md), [Canada](canada-privacy-supplement.md) and [U.S.](us-privacy-supplement.md) supplements explain different legal frameworks.

Security and safety observations are probabilistic and require human review. They must not automatically cause payroll, discipline, employment or law-enforcement consequences. Vehicle activity is not a finding of theft. Model names and confidence values do not prove accuracy, fairness or authorization. Training on customer information requires a separate approved purpose and provenance/consent review; the current repository has no approved customer-training data path.

## Recipients and transfers

Authorized customer personnel receive information within their assigned tenant/site scope. A reviewed processor register must identify any hosting, identity, support or other provider receiving personal information, the categories, purposes, location and onward processors. Supabase and Vercel are evidenced technology integrations/targets; this is not a verified production subprocessor list. The audited baseline also requests Google Fonts. Government or legal-request disclosures require authorized legal review and a valid basis; no blanket voluntary sharing is approved. See [subprocessor governance](subprocessor-governance.md) and [transfers](cross-border-data-transfer-policy.md).

## Retention, deletion and safeguards

The software stores a tenant retention setting with a 30-day default and a 7–365-day range. It does **not** currently enforce that setting across all records or stores. We cannot promise a deletion deadline, complete erasure, backup expiry or biometric deletion from this implementation. Production remains blocked where a notice requires an unimplemented control. The [retention policy](data-retention-and-deletion-policy.md) specifies the schedules and evidence that must be approved and implemented.

Implemented foundations include token verification, deny-by-default role/site authorization, tenant-scoped transactions/RLS and hash-chained audit mutations. Production transport encryption, storage encryption, secret management, physical privacy masking, access logging and backup controls need deployment evidence. Local examples deliberately use unencrypted development connections. Raw entrypoint logging is a tracked gap. These statements are not a guarantee against compromise.

## Rights, withdrawal, children and contact

Depending on applicable law, people may request access, correction/updating, erasure, consent withdrawal, grievance review, nomination, portability, or applicable opt-outs/limitations. Customer-directed requests should reach the customer's designated privacy contact; SyncCam must assist under its contract and address requests for its own processing. No self-service rights or automated fulfillment API exists in this baseline. The proposed [request procedure](data-subject-request-procedure.md) requires verification, human review and a reasoned response. It must be staffed and tested before any response-time promise is published.

A future biometric program must offer a practical non-biometric alternative and withdrawal without punishment, with future processing stopped and deletion reconciled. Do not enroll anyone using this draft. The current code does not implement child/guardian eligibility or consent safeguards. Child-directed, school and other vulnerable-population deployments require separate review; incidental capture of children must be considered in each camera assessment.

**Contact/grievance mechanism: [monitored channel, responsible role, mailing address and accessible alternative — unassigned].** Do not substitute public GitHub issues for this channel. This unresolved contact blocks operational publication. Security concerns follow the private channel in [SECURITY.md](../../SECURITY.md).

## Browser storage, telemetry and sale/sharing

The browser supports persistent identity-provider sessions and keeps a short-lived API token and alert resume state in session storage. These are not biometric embeddings. Baseline styling requests external Google Fonts, which exposes ordinary network request metadata to that provider. No advertising/tracking SDK was found in application source; hosting, identity and other provider telemetry was not inspected. The inventory must be verified in the deployed browser before making a cookie or telemetry claim.

No sale/advertising-sharing feature was found. A company-wide assertion that personal information is never sold/shared requires Legal to assess contracts, vendors and applicable statutory definitions. The intended product position is no sale of biometric information and no advertising use of camera or attendance information. Any future change requires explicit review, notice and applicable choice controls.

## Incidents and changes

Suspected exposure must be handled through the proposed [incident procedure](privacy-incident-and-breach-procedure.md), with jurisdiction-specific notification triggers and clocks. The procedure is not proof of a staffed incident service. Keep the current published version, material-change history, approval references and any renewed consent requirements before changing purposes. This draft's version history is in Git; no effective date or legal approval is created by a merge.

## Draft change record

Draft 2 records the T-0406 source changes following the audit: the external font import is removed, service entrypoints emit fixed failure categories, and generic attendance intake is rejected before persistence. The baseline descriptions above remain historical audit findings, not a statement that the old font request remains in this proposed revision. Deployment of these fixes is not established by this draft. See [exposure-fix evidence](../development/privacy-exposure-fixes.md). No consent, retention or erasure workflow is added.
