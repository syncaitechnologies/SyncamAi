# Phase 3 privacy-mask governance

Privacy masks remain disabled. The `privacy_masks:approve` capability is
reserved for an MFA-authenticated Super Admin only. The metadata-only request
workflow requires two distinct Super Admin approvals and rejects approval by
the requester. A completed approval record is still not sufficient to deliver
or activate a mask.

The next persistence slice must make every request and approval an immutable
audited record. Pre-encode edge-side masking verification and hardware-in-loop
release evidence are still required before a privacy-mask configuration can
reach a camera. No frames, pixels, stream credentials, model weights, or
masking execution are added by this workflow slice.
