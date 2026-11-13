# Site box setup (live MVP)

The site box is a small computer at the client's premises that runs the AI part
of this repository against the client's **existing** IP cameras or NVR. It sends
only events and one annotated snapshot per alert to the control plane; video
never leaves the site and camera passwords stay in the local config.

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
consecutive frames within a second (`weapon_review.py`). Repeat alerts from one
camera are suppressed for a minute. The current weapon model is a community
model with **no stated licence**; `fetch_site_models.ps1 -IncludeDevWeaponModel`
downloads it for development and demos only. Replace it with a licence-clean,
fine-tuned model before selling.

An intrusion zone alerts when a person **enters** it, and also when a person is
first seen **already inside** it for three consecutive frames while armed. A
loitering-near-vehicles zone never alerts for a parked vehicle, only for a
person who stays while a vehicle is present.

## Hardware

Any 64-bit Windows or Linux PC with an Intel CPU works; an Intel iGPU is used
automatically through OpenVINO (`device: AUTO`). On an i7-1165G7, YOLOX-s takes
about 18 ms per frame on the Iris Xe GPU and 80 ms on the CPU, enough for about
10 cameras at 4 fps. An Intel N100 mini-PC handles roughly 4–8 cameras.

## Install (Windows)

1. In the dashboard, register the site's cameras and create a **site key**
   (Configuration → Site boxes). Copy the key; it is shown once.
2. Copy `ai-services/site-worker.example.json` to a private folder outside Git,
   e.g. `D:\private\syncam-site.json`, and map each dashboard camera ID to its
   RTSP address. Use the camera's **sub-stream** (lower resolution) — it is
   plenty for detection and saves CPU. Put passwords in environment variables
   (`${CAM1_PASSWORD}`) rather than in the file where possible.
3. Run `pwsh scripts/install_site_box.ps1 -Config D:\private\syncam-site.json`.
   It creates a Python 3.12 environment, installs the `site-worker` extra,
   downloads and verifies the models, asks for the site key, runs a check and
   registers an auto-start task.

Common RTSP formats:

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

## Reliability and privacy

- Alerts are written to a local spool first and retried for 24 hours, so an
  internet outage or a sleeping backend does not lose them.
- The box needs only outbound HTTPS. No router ports are opened.
- Only the model files pinned in `syncam_ai/model_artifacts.py` load; a
  development-only model needs an explicit opt-in.
- Detection is not identification. Alerts are marked for human review.
