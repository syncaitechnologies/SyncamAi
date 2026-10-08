# AI services development runtimes

## Apache-compatible person detector

The first runtime adapter is deliberately local-only. It uses Intel Open Model
Zoo `person-detection-0200` through OpenVINO, with the published Apache-2.0
manifest and SHA-384 verification encoded in
`src/syncam_ai/person_detection.py`.

Install its optional runtime dependencies in a Python 3.12 environment:

```powershell
python -m pip install -e ".[person-detector]"
```

Download the FP32 XML and BIN from the exact URLs in the adapter's source
manifest to a directory outside this repository. Do not commit, upload, or
redistribute the artifacts. `OpenVinoPersonDetector` refuses to load a missing
or checksum-mismatched pair.

This is a developer-machine integration only: it accepts already-decoded BGR
frames and returns person boxes. It has no camera capture, HTTP upload,
persistence, alert emission, customer footage, model promotion, or face
recognition. Production promotion remains blocked by ADR-001's legal and model
release gates.

