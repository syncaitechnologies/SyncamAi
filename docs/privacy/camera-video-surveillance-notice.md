# Camera and video-surveillance notice

> Draft operational privacy policy — requires review and approval by qualified legal/privacy counsel before production use in the applicable jurisdiction.

Version: `2026-09-08.draft-1`. Proposed signage and layered notice only. Current code provides camera metadata and edge transport libraries; recording, cloud evidence storage and physical masking are not established deployment facts.

## Proposed short notice

“[Customer legal name] operates cameras in [clearly identified area] for [specific safety/security purpose]. Images and [enabled event metadata] may identify people. [State accurately whether recording occurs, whether audio is captured, and whether any approved analytics operates.] Contact [monitored channel or staffed alternative] for the full notice, questions or privacy requests. More information: [accessible notice location].”

Do not use “By entering you consent” as a substitute for a lawful ground, separate biometric consent, or required local signage. Do not assert that masking or deletion operates until verified. If recognition is prohibited, signage cannot make it lawful.

## Required full notice and deployment review

Identify controller/processor roles; each category collected (images, clips, timestamps, location/camera identifiers, observations, confidence and review records); purpose and ground; exact recipients; processor countries; retention period/start trigger; access/correction/request options; incident contact; and any statutory exceptions. Describe camera coverage, operating times, recording vs live viewing and enabled analytics in plain language. State that observations need human review and do not prove theft, a weapon, fire or misconduct.

Review placement and less intrusive alternatives before collection. Avoid private/sensitive areas; assess bystanders and children. Approve privacy zones and actual pre-encode masking, and verify that masked pixels cannot pass into downstream encoding, inference, storage or export. Metadata approvals and an injected executor are not physical-HIL evidence. Keep raw-video access separately restricted and auditable when implemented.

Audio, recognition, face search, cross-camera tracking and unrelated secondary uses require separate scope and legal review; none is authorized by this draft. A customer-owned CCTV system may process data independently of SyncCam and needs its own accurate notice. Determine sector-specific and local rules, including NYC or Portland where relevant, through the [jurisdiction matrix](privacy-jurisdiction-matrix.md).
