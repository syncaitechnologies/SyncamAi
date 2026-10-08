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

## Physical iPhone test on the same private Wi-Fi

`pnpm dev --host 0.0.0.0` alone serves HTTP, which iPhone Chrome must not use
for camera access. For the physical test, give both the web app and the local
inference server the same trusted certificate whose subject alternative name
contains the laptop's private Wi-Fi IPv4 address.

1. Find the laptop's private Wi-Fi IPv4 address with `ipconfig`; call it
   `192.168.1.25` below. Keep the phone and laptop on that same private Wi-Fi.
   Do not use a public address or expose either port to the internet.
2. Generate a development certificate outside Git with a local CA trusted by
   the iPhone. For example, `mkcert` may generate a certificate with the IP
   subject alternative name:

```powershell
mkcert -install
mkcert -cert-file D:\private\syncam-demo-cert.pem `
  -key-file D:\private\syncam-demo-key.pem 192.168.1.25
```

   Transfer that local CA's public root certificate to the test iPhone, install
   it as a profile, then explicitly enable full trust in iOS Certificate Trust
   Settings. Keep the CA private, use it only for this test device, and remove
   the profile after the demonstration if it is no longer needed.
3. Download the exact checksum-verified FP32 model pair outside Git:

```powershell
New-Item -ItemType Directory -Force D:\private\models | Out-Null
Invoke-WebRequest `
  https://storage.openvinotoolkit.org/repositories/open_model_zoo/2023.0/models_bin/1/person-detection-0200/FP32/person-detection-0200.xml `
  -OutFile D:\private\models\person-detection-0200.xml
Invoke-WebRequest `
  https://storage.openvinotoolkit.org/repositories/open_model_zoo/2023.0/models_bin/1/person-detection-0200/FP32/person-detection-0200.bin `
  -OutFile D:\private\models\person-detection-0200.bin
```

4. In the first PowerShell window, start the local detector. Use Python 3.12;
   the command fails closed if the artifact checksum, certificate, or model
   runtime is wrong.

```powershell
py -3.12 -m venv D:\private\syncam-person-venv
& D:\private\syncam-person-venv\Scripts\Activate.ps1
python -m pip install --upgrade pip
python -m pip install -e ".\ai-services[person-detector]"
$env:PYTHONPATH = (Resolve-Path "ai-services\src").Path
python -m syncam_ai.local_person_server `
  --bind 0.0.0.0 `
  --allowed-origin https://192.168.1.25:5173 `
  --certificate D:\private\syncam-demo-cert.pem `
  --private-key D:\private\syncam-demo-key.pem `
  --model-xml D:\private\models\person-detection-0200.xml `
  --model-bin D:\private\models\person-detection-0200.bin
```

5. In a second PowerShell window, serve the web app over that same trusted
   HTTPS certificate. Windows may ask to allow ports 5173 and 8443 on private
   networks; do not allow public-network access.

```powershell
$env:VITE_LOCAL_PERSON_INFERENCE_URL = "https://192.168.1.25:8443/"
$env:SYNCAM_DEV_TLS_CERTIFICATE = "D:\private\syncam-demo-cert.pem"
$env:SYNCAM_DEV_TLS_PRIVATE_KEY = "D:\private\syncam-demo-key.pem"
pnpm --dir frontend/apps/web dev --host 0.0.0.0
```

6. On the iPhone, open `https://192.168.1.25:5173/?camera=local`, grant camera
   permission, choose the rear camera, press **Start camera**, and explicitly
   enable **person detection**. Test one consenting adult, an empty scene,
   Stop, background/foreground, and restart. A person box is a development
   observation only, never an identity or security decision.

## Demonstration limits

The result establishes only that the selected local model can draw person boxes
for that controlled scene. It does not establish detection accuracy, weapon
detection, face recognition, biometric consent, production readiness, or a
safe automatic response. A human remains responsible for every interpretation.
