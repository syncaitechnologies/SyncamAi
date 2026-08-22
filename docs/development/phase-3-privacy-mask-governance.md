# Phase 3 privacy-mask governance

Privacy masks remain disabled. This first security prerequisite reserves the
`privacy_masks:approve` capability for an MFA-authenticated Super Admin only.
It is not sufficient to create, approve, deliver, or activate a mask.

The later dedicated workflow must enforce two distinct Super Admin approvals,
immutable audit records, a pre-encode edge-side masking verification result,
and hardware-in-loop release evidence before a privacy-mask configuration can
reach a camera. No frames, pixels, stream credentials, model weights, or
masking execution are added by this authorization-only slice.
