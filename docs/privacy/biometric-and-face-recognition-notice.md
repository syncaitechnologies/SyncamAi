# Separate biometric and face-recognition notice

> Draft operational privacy policy — requires review and approval by qualified legal/privacy counsel before production use in the applicable jurisdiction.

Version: `2026-09-08.draft-1`. **Proposed wording only — do not present for acceptance or collect a face, template, signature or consent record using this draft.** Implementation and activation remain blocked by the [approval-evidence checklist](../governance/biometric-attendance-approval-evidence-checklist.md).

## Required notice before any future opt-in

The customer and reviewers must replace every bracketed field and validate each described behavior. No application enrollment flow exists yet.

“[Employer/controller legal name] proposes optional face-based attendance at [specific sites] for [specific attendance purpose]. [SyncCam legal entity] would process the approved information for us. The information would include [facial images captured and whether retained], [face template/embedding], [liveness signals and retention], [employee identifier], [attendance event fields] and [versioned consent evidence]. We would not use this permission for unrelated identification, advertising, model training or cross-camera tracking.

“The authorized recipients would be [roles, service providers, processing countries and purpose for each]. Information would be retained for [approved category-specific periods and start triggers], then handled by [tested deletion process, backup lifecycle and any lawful exceptions]. These fields must describe enforced behavior, not merely planned behavior.

“Face systems can produce false matches or missed matches, perform differently across conditions and groups, and expose sensitive identifying information if compromised. A liveness check is an anti-spoofing measure, not proof of identity or a basis for punishment. A person reviews disputed outcomes through [review/correction/appeal method]. No automatic payroll, disciplinary or law-enforcement consequence is authorized.

“You can decline and use [working badge/PIN/manual alternative] through [instructions] without punishment for declining. You can withdraw through [accessible channel] as easily as you opted in. Withdrawal stops future consent-based face processing through [tested revocation propagation] and starts [deletion/reconciliation process]. [Specific legally retained attendance or consent records] may be retained only for [basis and limited period]. Contact [monitored privacy contact] for questions, access, correction or a complaint.”

## Proposed choice design

Provide separate, initially unselected choices for the precise necessary capture/use described above and any optional photo retention. No broad “I agree to biometric processing” checkbox. Provide equally visible “Use non-biometric attendance” and “Ask a question” actions. Do not label acknowledgment of reading as consent. Record the exact notice/purpose version and affirmative action only after the approved identity process; withdrawal creates a new audited state, never rewrites history as though permission remained active.

The proposed record schema is described in [traceability](privacy-implementation-traceability.md); it is not implemented. Never place templates/embeddings in browser storage, Redis, logs, ordinary events or source control. Require dedicated encryption/key ownership, tenant isolation, human oversight, model/data license and provenance, fairness evaluation and erasure evidence before any enrollment. Jurisdiction-specific statutory forms, languages, notice periods or prohibitions can require changes to this wording; this generic draft cannot satisfy them by itself.
