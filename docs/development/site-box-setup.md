# Site box setup (live MVP)

The site box is a small computer at the client's premises that runs the AI part
of this repository against the client's **existing** IP cameras or NVR. It sends
only events and one annotated snapshot per alert to the control plane, plus
near-live frames while someone is watching a camera in the dashboard (see
[Live view](#live-view-in-the-dashboard)). Video is never recorded in the cloud
and camera passwords stay in the local config.

```
NVR / IP cameras (RTSP, private LAN)
  -> site box: python -m syncam_ai.site_worker run
       YOLOX (Apache-2.0) people + vehicles -> CameraLocalDetectionTracker
       -> ZoneRuntime rules + schedules + presence + loitering near vehicles
  -> HTTPS (site key) -> control plane on Render -> alert
  -> Telegram photo + live Alert Center (Vercel)
```

## What it detects

| Client need | How to configure it in the dashboard | Event |
|---|---|---|
| Unauthorized person (after hours) | Intrusion or restricted zone with a schedule, e.g. every day 22:00–06:00 | `intrusion` / `restricted_zone` |
| Zone protection | Intrusion, restricted zone or tripwire on a camera | `intrusion` / `restricted_zone` |
| Car-theft risk | Loitering zone over the parking area with subject classes person + vehicles, 60–120 s | `loitering` ("Person loitering near vehicles") |
| Camera offline / tampered | Automatic for every configured camera after 60 s | `camera_health` |
| Possible weapon | Set `models.weapon` and `allow_dev_models: true` (development model only) | `weapon_review` |

Weapon review runs only on frames where a person is visible, also inside
enlarged crops around the largest people, and needs the same object in three
consecutive frames within a second (`weapon_review.py`). A weapon alerts once
per appearance; after it is out of view for about three seconds, showing it
again alerts again, at most once per 15 s per weapon type and camera
(`weapon_repeat_seconds` in the site config). Telegram applies its own 15 s
window per zone and alert type (`SYNCAM_TELEGRAM_COOLDOWN_SECONDS` on the
API). The current weapon model is a community
model with **no stated licence**; `fetch_site_models.ps1 -IncludeDevWeaponModel`
downloads it for development and demos only. Replace it with a licence-clean,
fine-tuned model before selling.

An intrusion zone alerts when a person **enters** it, and also when a person is
first seen **already inside** it for three consecutive frames while armed. A
loitering-near-vehicles zone never alerts for a parked vehicle, only for a
person who stays while a vehicle is present.

## Staff face attendance

Attendance uses Intel's Apache-2.0 OpenVINO face models (detection, landmarks
and re-identification), downloaded by `fetch_site_models.ps1 -IncludeFaceModels`.

1. In the dashboard (**Attendance**), add the staff member and tick the box
   confirming their **written consent**. The dashboard records who confirmed it
   and when, and shows the staff ID.
2. On the site box, create the gallery key once and keep it only on that box:
   `python -m syncam_ai.site_worker face-key`, then store the printed value in
   the `SYNCAM_FACE_KEY` user environment variable.
3. Put 3–5 clear photos of the person's face in a folder and run
   `python -m syncam_ai.site_worker enroll --config <file> --staff-id <id> --photos <folder>`.
   The photos are not stored; delete them afterwards.
4. In the site config set
   `"models": {"faces": "D:/private/models", "liveness": "D:/private/models/minifasnet_v2.onnx"}`
   and list the entrance cameras in `"entrance_cameras": ["<camera id>"]`.
5. Check liveness at the entrance camera:
   `python -m syncam_ai.site_worker liveness --config <file>` prints a score
   every second. Look at the camera (real faces should score above
   `face_liveness_threshold`, default 0.8), then hold up a printed photo or a
   phone showing a face at several distances and angles. If real faces score
   low or photos score high, move the camera or add light; see the limits
   below.

### Liveness (anti-spoofing)

Every face is checked by MiniFASNet-V2 from minivision-ai's
Silent-Face-Anti-Spoofing (Apache-2.0, also downloaded by
`-IncludeFaceModels`) before it can be matched. The model looks at the face
plus the area around it (2.7× the face box). A face that fails is never
matched; if it looks like an enrolled staff member, the site box logs
`possible photo or screen spoof` (at most once a minute per person) and does
not mark attendance. Without `models.liveness` attendance still runs, with a
warning at start-up.

> **Attendance is a convenience record, not anti-fraud.** On-site tests on a
> laptop webcam in evening light (November 2026) showed that a phone photo of
> an enrolled person is *usually* rejected but **can mark attendance** when held
> close or tilted, and that real faces further from the camera are often
> scored as spoofs. Two stronger passive checks were built and measured, and
> neither was reliable enough to ship:
>
> | Check | Result |
> |---|---|
> | MiniFASNet over a 4-second window | Scores followed face size and light more than real-vs-photo: the photo reached a 0.70 four-second mean, real faces dropped to 0.4 |
> | Phone/tablet/monitor detection (YOLOX COCO classes) | Found the phone around the face in only 2 of 183 frames |
> | Blink check with Intel open-closed-eye-0001 (15 fps, colour and greyscale, 1,296 rule variants) | No variant rejected the photo while catching real blinks; the photo's eyes read "closed" on 10–23% of frames |
>
> Sell attendance as "who arrived, when" with spot checks, not as proof of
> presence. For fraud-resistant attendance, use a door terminal with infrared
> or 3D (depth) liveness, or a kiosk that asks the person to blink or turn on
> cue.

Other limits: this is a passive check from a single ordinary camera, not a
certified presentation-attack-detection system. Lighting, camera height and
distance change the scores; mount entrance cameras at face height with
light on the face, and keep staff aware that attendance is reviewed.

The box marks a person present after two matches within ten seconds, then
ignores them at that camera for ten minutes. Face signatures stay in an
encrypted file on the box; the cloud stores only names, consent records and
sighting times. Removing a person in the dashboard stops matching immediately;
also run `python -m syncam_ai.site_worker forget --config <file> --staff-id <id>`
on the box to delete their face data. Unknown faces are ignored and never stored.

**Before selling attendance:** the consent wording and retention period need
legal review for each country.

## Hardware

Any 64-bit Windows or Linux PC with an Intel CPU works; an Intel iGPU is used
automatically through OpenVINO (`device: AUTO`). On an i7-1165G7, YOLOX-s takes
about 18 ms per frame on the Iris Xe GPU and 80 ms on the CPU, enough for about
10 cameras at 4 fps. An Intel N100 mini-PC handles roughly 4–8 cameras.

## Install (Windows, recommended): SyncCamSetup.exe

Pick a Windows 10/11 64-bit PC at the site that stays on and is on the same
network as the cameras or recorder.

1. In the dashboard, **Configuration → Site box → Pair a site box**. It shows
   an 8-character code such as `K7Q4-XM92` (works once, for 30 minutes) and a
   link to download **SyncCamSetup.exe**.
2. Run SyncCamSetup.exe on the PC. The installer is not code-signed yet, so
   Windows SmartScreen may warn: choose **More info → Run anyway**. It needs
   administrator rights.
3. Type the pairing code when the installer asks. It then
   - installs SyncCam Box under `C:\Program Files\SyncCam Box`,
   - creates `C:\ProgramData\SyncCam` (config, encrypted vault, models, alert
     spool, logs), readable only by SYSTEM and Administrators,
   - downloads the detection model and checks its SHA-384 pin,
   - pairs the box, and
   - registers the **SyncCam Box** task, which runs as SYSTEM at computer
     start-up (nobody has to sign in) and restarts within a minute if it stops.
4. In the dashboard, **Configuration → Cameras → Find cameras** adds the
   cameras (see [below](#find-cameras-no-rtsp-addresses-to-type)). Nothing
   else is configured on the PC.

IT teams can install without the wizard:
`SyncCamSetup.exe /VERYSILENT /CODE=K7Q4XM92`.

**Status page.** On the box itself, the Start menu entry **SyncCam Box status**
opens <http://127.0.0.1:8790>: whether the box is paired and connected, each
camera's last frame, the last alert, alerts waiting to be delivered and the
update state. It is served on the loopback address only, shows no addresses,
passwords or keys, and refuses requests for any other host name.

**Updates.** Once a day the box reads the latest release's
`syncam-box-manifest.json`. It installs a newer version only when the manifest's
Ed25519 signature matches a key built into the program
(`site_worker/release_keys.py`), the installer is a release asset of this
repository and its SHA-256 matches the signed manifest. Older or equal
versions are ignored. The installer runs as a separate one-off SYSTEM task and
keeps the pairing, cameras and models.

**Uninstall** from Windows Settings → Apps. The `C:\ProgramData\SyncCam` folder
(pairing, recorder passwords, models) is kept so a reinstall continues where it
left off; delete it, and revoke the box's key in the dashboard, to remove
everything.

Logs: `C:\ProgramData\SyncCam\logs\syncam-box.log` (rotated at 5 MB) and
`setup.log` from the installer. The program also takes the commands below,
e.g. `"C:\Program Files\SyncCam Box\syncam-box.exe" pair --code <code>` (as
Administrator) to pair again after a failed attempt.

### Publishing a release (maintainers)

1. Set `VERSION` in `ai-services/src/syncam_ai/site_worker/version.py` and
   merge it.
2. Push a tag `box-v<VERSION>`. The `site-box-installer` workflow builds the
   installer on Windows (PyInstaller 6.11.1 + Inno Setup 6), signs the update
   manifest with the `SYNCAM_RELEASE_SIGNING_KEY` repository secret and
   publishes the GitHub release. Every pull request that touches the site box
   also builds the installer as a 7-day workflow artifact.
3. The release key was created with
   `python installer/release_manifest.py generate-key --out <private file>`.
   Its private half lives only in that secret and the maintainer's private
   folder; never commit it. Rotation is described in `release_keys.py`.

Build-time tools: PyInstaller (GPL-2.0 with the bootloader exception, which
allows distributing the bundled program) and Inno Setup (free for commercial
use). Neither is a runtime dependency. The installed folder keeps the licence
files of the bundled libraries, including OpenCV's LGPL FFmpeg.

## Install from source (developers)

1. In the dashboard, register the site's cameras, then under
   **Configuration → Site box** press **Pair a site box**. It shows an
   8-character code such as `K7Q4-XM92` that works once for 30 minutes.
2. Copy `ai-services/site-worker.example.json` to a private folder outside Git,
   e.g. `D:\private\syncam-site.json`, and map each dashboard camera ID to its
   RTSP address. Use the camera's **sub-stream** (lower resolution) — it is
   plenty for detection and saves CPU. Put passwords in environment variables
   (`${CAM1_PASSWORD}`) rather than in the file where possible.
3. Run `pwsh scripts/install_site_box.ps1 -Config D:\private\syncam-site.json -PairingCode K7Q4-XM92`.
   It creates a Python 3.12 environment, installs the `site-worker` extra,
   downloads and verifies the models, **pairs the box**, runs a check and
   registers an auto-start task.

Pairing: the box creates its own key pair, sends the code and its public key,
and receives a site key that only it holds. The key is stored in
`syncam-box.vault` next to the config, encrypted with Windows DPAPI (machine
scope: unreadable on any other computer; a Fernet key file is used on Linux).
Nothing has to be copied from the dashboard. To move a box to another site,
revoke its key in the dashboard, delete the vault file and pair again. You can
also pair by hand: `python -m syncam_ai.site_worker pair --config <file> --code <code>`.
The older way, a site key from **Advanced: create a site key instead** in the
`SYNCAM_SITE_KEY` environment variable, still works and takes precedence.

### Find cameras (no RTSP addresses to type)

Once the box is paired and running, **Configuration → Cameras → Find cameras**:

1. **Scan the network.** The box sends an ONVIF WS-Discovery probe and scans
   its own /24 subnet for RTSP (554) and brand ports (37777 Dahua/Lorex,
   8000 Hikvision, 9000 Reolink). Each answer is labelled with its brand.
   If nothing is found, type the recorder's local address.
2. **Connect** to the recorder with a view-only account. The password is
   encrypted in the browser for this box only (ECDH P-256 → HKDF → AES-GCM);
   the API relays ciphertext it cannot read and keeps it in memory only until
   the box picks it up. The box keeps it in its encrypted vault.
3. The box finds the RTSP format that works (Dahua/Lorex/Amcrest/CP Plus,
   Hikvision/HiLook/Annke, Uniview or Reolink) and checks channels 1–32,
   stopping at once on a wrong password so the recorder does not lock the
   account. It shows a picture of each channel.
4. **Tick and name** the channels, choose their **protection** and press
   **Add**. The cameras are created in the dashboard and start on the box
   within seconds; no restart. They survive restarts because the box stores
   them in its vault. Protection defaults to **Car park watch** where the box
   saw parked vehicles in the channel's picture (marked "Vehicles seen") and to
   **Night intrusion** elsewhere; choose "None" to draw zones later.

### Protection templates

One click protects a whole camera view, in Find cameras or under **Zones &
rules → Protect this camera**:

| Template | Rule | Armed (site timezone) |
|---|---|---|
| Night intrusion | Person anywhere in view (intrusion) | Every day 22:00–06:00 |
| Car park watch | Person staying 90 s near vehicles (loitering near vehicles) | Every day 20:00–07:00 |

Each template is an ordinary zone over the whole picture, so it can be paused
or replaced by drawn zones later. A camera shows "Added" for a template it
already has.

"Near vehicles" means a vehicle's centre is within 0.3 of the picture's size
of the person's centre (`NEAR_VEHICLE_DISTANCE` in `site_worker/rules.py`).
A car on the far side of a whole-view zone does not count, and this applies
to drawn loitering-near-vehicles zones too.

### Telegram alerts

**Configuration → Telegram alerts → Link a Telegram chat** shows a QR code:
scan it with a phone camera, Telegram opens the SyncCam bot, press **Start**.
"Add the bot to a group" opens Telegram's group picker and links the group the
same way. The code works once, for 15 minutes.

The box only ever probes private (LAN) addresses. Setup tasks and their
results (which include the channel pictures) are kept in the API's memory for
at most an hour and are visible only to administrators of that site.

Common RTSP formats (used automatically by Find cameras):

| Brand | Sub-stream URL |
|---|---|
| Hikvision | `rtsp://user:pass@IP:554/Streaming/Channels/102` (camera 1; `202` for camera 2 on an NVR) |
| Dahua / CP Plus | `rtsp://user:pass@IP:554/cam/realmonitor?channel=1&subtype=1` |
| Generic ONVIF | see the camera's web page under Network → RTSP |

## Commands

```powershell
python -m syncam_ai.site_worker check --config D:\private\syncam-site.json
python -m syncam_ai.site_worker snapshot --config D:\private\syncam-site.json --camera <id> --out frame.jpg
python -m syncam_ai.site_worker run --config D:\private\syncam-site.json
```

A camera entry may also be a video file path (looped) or a webcam index such as
`"0"`, which is useful for demos and testing.

## Live view in the dashboard

The Camera Wall shows a near-live view of any camera on request.

- Video stays on the site box until someone with live-view rights (Operator,
  Site admin or Super admin with the `raw_video` data class) presses
  **Watch live** on a camera tile or in its spotlight.
- That starts a viewing session, which the API writes to the tenant's
  hash-chained audit log (`camera.live_view.started`, with the viewer's
  email) **before** any frame is shown. If the record cannot be written, live
  view does not start.
- The site box checks every 2 seconds which of its cameras are being watched
  and sends about 2 small JPEG frames a second (at most 640 px wide) for those
  only. It stops within about 10 seconds of the viewer closing the tile,
  pressing Stop or hiding the browser tab.
- Frames pass through the API's memory and are never written to storage. The
  view shows a **LIVE · viewed by … · logged** chip and the age of the frame.
- Expect a choppy picture with 1–2 seconds of delay; it is meant for checking
  a scene, not for smooth video.
- To keep a site's video entirely on the premises, set `"live_view": false`
  in its site config.

### Live-view minutes, pause and the still wall (ADR-017)

Live view uses Render bandwidth, so it is metered per workspace:

- Each workspace has a monthly allowance of viewing minutes
  (`identity.tenants.live_view_minutes`): **10 minutes** by default, `NULL` for
  unlimited (the owner's own workspace), `0` to switch live view off. Time is
  counted per viewer per camera and resets on the 1st of each month (UTC).
  When the minutes are used up, live view stops with "minutes for this month
  are used up"; alerts keep working.
- One session pauses after **5 minutes** and asks "Still watching?";
  continuing starts a new, audited session.
- The Camera Wall shows each camera's **still picture** (the reference
  picture the box uploads every 5 minutes, also used to draw zones). Live video
  plays only for the one camera opened in the spotlight.
- The usage table (`platform.live_view_usage`) holds only a count of seconds
  per workspace per month: no images and no viewer identity. Usage is saved
  every 30 seconds, so it is accurate to about that.

## Reliability and privacy

- Alerts are written to a local spool first and retried for 24 hours, so an
  internet outage or a sleeping backend does not lose them.
- The box needs only outbound HTTPS. No router ports are opened.
- Only the model files pinned in `syncam_ai/model_artifacts.py` load; a
  development-only model needs an explicit opt-in.
- Detection is not identification. Alerts are marked for human review.
