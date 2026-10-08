# Local person-inference bridge

Task: T-0434. This guide is for an authorized developer demonstration, not a
production deployment or a customer-footage workflow.

## What it does

The web app's real camera workspace samples at most one 640-pixel-wide RGBA
frame roughly every 800 ms only after the person-detection checkbox is enabled.
It sends that in-memory frame to the configured HTTPS development endpoint and
draws returned person boxes over the local preview. The Python process converts
the frame to BGR, runs the checksum-verified ADR-001 person model, and returns
only `confidence`, `left`, `top`, `right`, and `bottom`.

There is no recording, frame persistence, request logging, model download,
identity, face analysis, weapon result, alert, evidence object, tenant API, or
background capture. Stop, loss of visibility, navigation, and disposal cancel
the browser request and clear displayed boxes.

## Required local setup

1. Use only an authorized developer test scene with consenting adults. Do not
   use customer footage or treat this as a security decision system.
2. Install the ignored local Python runtime extras in a Python 3.12 environment
   and place the verified XML/BIN model pair outside Git as ADR-001 requires.
3. Create a trusted development TLS certificate and private key outside the
   repository. For an iPhone, the phone must trust the issuing local CA; a
   self-signed certificate that Safari does not trust will not work. Never add
   the certificate, key, CA, model, or footage to the repository.
4. Start the detector on the developer machine. Bind to a private network
   address only when necessary for the phone; the default loopback binding is
   safer.

```powershell
$env:PYTHONPATH = (Resolve-Path "ai-services\src").Path
python -m syncam_ai.local_person_server `
  --allowed-origin https://YOUR-HTTPS-WEB-ORIGIN `
  --certificate D:\private\syncam-dev-cert.pem `
  --private-key D:\private\syncam-dev-key.pem `
  --model-xml D:\private\models\person-detection-0200.xml `
  --model-bin D:\private\models\person-detection-0200.bin
```

5. Build or run the web app with one public, non-secret environment value:

```powershell
$env:VITE_LOCAL_PERSON_INFERENCE_URL = "https://YOUR-DEVELOPER-MACHINE:8443/"
pnpm --dir frontend/apps/web dev --host 0.0.0.0
```

The URL must be HTTPS with no path, credentials, query, or fragment. It is
ignored when absent. The endpoint independently permits only the configured
origin, so the browser configuration alone cannot open access.

## Demonstration limits

The result establishes only that the selected local model can draw person boxes
for that controlled scene. It does not establish detection accuracy, weapon
detection, face recognition, biometric consent, production readiness, or a
safe automatic response. A human remains responsible for every interpretation.
