"""The SyncCam site box: existing cameras in, verified alerts out.

The site worker runs on a small computer at the client's premises. It reads
the client's existing IP cameras over RTSP, runs licence-approved detectors,
applies the zones and schedules configured in the dashboard, and sends only
events plus one annotated snapshot per alert to the SyncCam control plane.
Video never leaves the site, and camera passwords stay in the local config.
"""
