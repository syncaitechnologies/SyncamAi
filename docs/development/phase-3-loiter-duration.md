# Phase 3 loitering dwell configuration

Loitering is deterministic camera-local dwell logic, not a detector model. A
loitering zone may carry `loiter_seconds` from 30 seconds through 600 seconds;
the API and storage default it to 30 seconds when omitted. The value is
rejected for intrusion, restricted-zone, abandoned-object, and tripwire zones.

The field is a versioned Zone API mutation: create is idempotent and audited;
an update requires the zone's current configuration version and produces a new
version only when its value changes. It is stored under the existing tenant RLS
policy and is present in an immutable published configuration snapshot. The
local rule runtime validates the same range before it activates a candidate.

This does not add raw frames, model weights, tracking, event transport, or an
autonomous response. Privacy-mask zones remain excluded pending their dedicated
Super Admin, dual-approval, immutable-audit, pre-encode verification, and
hardware-in-loop security slice.
