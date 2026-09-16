# Phase 3 test-mode privacy-release runtime composition

T-0428 composes the already bounded privacy-release components into an
optional, cancellable `EdgeRuntime` loop for synthetic verification. It does
not change the default `edge-agent` command path.

## Local composition boundary

The caller must explicitly supply all five components:

1. a `PrivacyMaskReleaseBundleSource` that returns raw artifact bytes;
2. the T-0427 `PrivacyMaskReleaseLoader` with a configured scope and public
   verification key;
3. the existing controlled release gate;
4. a worker and bounded retry supervisor; and
5. `NewEdgeRuntimeWithPrivacyMaskRelease`.

The source receives the version of the gate's last accepted release. A `nil`
source result means no newer artifact and has no activation effect. Every
non-empty artifact must pass the strict loader before the controlled gate can
call its injected applier. Source, parsing, verification, application and
cancellation failures cannot create a bypass or replace an already accepted
release.

The runtime exposes only the safe lifecycle event
`privacy_mask_release:started`. It never reports release geometry, signatures,
hardware controls, source paths, credentials, frames or pixels.

## Deliberate test-mode limits

This slice introduces no default startup configuration, file reader, trust
store, certificate, API route, transport migration, hardware executor, frame
processor, activation, model, media handling or deployment configuration.

The current dedicated edge transport returns a decoded release manifest. T-0427
correctly requires the original signed bundle bytes so it can reject duplicate,
unknown and trailing JSON before parsing. T-0428 therefore keeps transport
integration deliberately unresolved rather than weakening the loader or
silently changing the existing wire contract.

Production composition still requires an approved artifact-provisioning
contract, an OS-backed trust source, a real allowlisted pre-encode hardware
executor, physical signed-mask HIL evidence, verified mTLS ingress and explicit
human review.
