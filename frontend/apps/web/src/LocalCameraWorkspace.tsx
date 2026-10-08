import { useEffect, useRef, useState } from "react";
import { Icon } from "./icon";
import { bindCameraLifecycle, initialCameraState, LocalCameraSession } from "./local-camera-session";
import type { CameraFacing } from "./local-camera-session";
import { localPersonInferenceEndpoint, requestLocalPersonDetections } from "./local-person-inference";
import type { LocalPersonDetection } from "./local-person-inference";
import "./local-camera.css";

export function LocalCameraWorkspace({ onOpenOverview }: { onOpenOverview(): void }) {
  const videoRef = useRef<HTMLVideoElement>(null);
  const sessionRef = useRef<LocalCameraSession<MediaStream> | null>(null);
  const [state, setState] = useState(initialCameraState);
  const [authorized, setAuthorized] = useState(false);
  const [facing, setFacing] = useState<CameraFacing>("environment");
  const [personDetectionEnabled, setPersonDetectionEnabled] = useState(false);
  const [detections, setDetections] = useState<LocalPersonDetection[]>([]);
  const [inferenceMessage, setInferenceMessage] = useState("Person detection is off.");
  const [inferenceFrame, setInferenceFrame] = useState({ width: 1, height: 1 });
  const inferenceEndpoint = localPersonInferenceEndpoint(import.meta.env.VITE_LOCAL_PERSON_INFERENCE_URL);
  const busy = state.phase === "requesting" || state.phase === "starting";
  const capturing = busy || state.phase === "live";

  useEffect(() => {
    const session = new LocalCameraSession<MediaStream>({
      secure: window.isSecureContext,
      supported: typeof navigator.mediaDevices?.getUserMedia === "function",
      visible: () => document.visibilityState === "visible",
      request: (constraints) => navigator.mediaDevices.getUserMedia(constraints),
      attach: (stream) => {
        const video = videoRef.current;
        if (!video) return;
        if (!stream) video.pause();
        video.srcObject = stream;
      },
      play: async () => {
        const video = videoRef.current;
        if (!video) throw new Error("Preview unavailable");
        await video.play();
      },
      onState: setState,
    });
    sessionRef.current = session;
    const unbind = bindCameraLifecycle(session, document, window);
    return () => { unbind(); session.dispose(); sessionRef.current = null; };
  }, []);

  useEffect(() => {
    if (state.phase === "live") return;
    setPersonDetectionEnabled(false);
    setDetections([]);
    setInferenceMessage("Person detection is off.");
  }, [state.phase]);

  useEffect(() => {
    if (!personDetectionEnabled || state.phase !== "live" || !inferenceEndpoint) return;
    const canvas = document.createElement("canvas");
    const controller = new AbortController();
    let active = true;
    let nextSample: number | null = null;
    const sample = async () => {
      const video = videoRef.current;
      if (!active || !video || video.videoWidth < 1 || video.videoHeight < 1) return;
      const scale = Math.min(1, 640 / video.videoWidth, 640 / video.videoHeight);
      const width = Math.max(1, Math.floor(video.videoWidth * scale));
      const height = Math.max(1, Math.floor(video.videoHeight * scale));
      const context = canvas.getContext("2d", { willReadFrequently: true });
      if (!context) { setInferenceMessage("Person detection could not access the transient camera frame."); return; }
      canvas.width = width;
      canvas.height = height;
      context.drawImage(video, 0, 0, width, height);
      try {
        const result = await requestLocalPersonDetections(
          inferenceEndpoint,
          { width, height, rgba: context.getImageData(0, 0, width, height).data },
          controller.signal,
        );
        if (!active) return;
        setDetections(result);
        setInferenceFrame({ width, height });
        setInferenceMessage(result.length === 1 ? "1 person detected locally." : `${result.length} people detected locally.`);
      } catch (error) {
        if (!active || (error instanceof DOMException && error.name === "AbortError")) return;
        setDetections([]);
        setInferenceMessage("Person detection is unavailable. Check the trusted local development detector.");
      } finally {
        if (active) nextSample = window.setTimeout(() => { void sample(); }, 800);
      }
    };
    void sample();
    return () => {
      active = false;
      controller.abort();
      if (nextSample !== null) window.clearTimeout(nextSample);
    };
  }, [inferenceEndpoint?.href, personDetectionEnabled, state.phase]);

  return (
    <section className="local-camera-page" aria-labelledby="local-camera-title">
      <header className="local-camera-heading">
        <div>
          <span className="local-camera-eyebrow">SYNC CAM AI · REAL CAMERA INPUT</span>
          <h1 id="local-camera-title">Your phone. Your camera.</h1>
          <p>A local camera connection for the person-detection development prototype.</p>
        </div>
        <button type="button" className="local-camera-back" onClick={onOpenOverview}>
          <Icon name="arrow" size={16} /> Overview
        </button>
      </header>

      <div className="local-camera-notice">
        <Icon name="shield" />
        <p>Real video, not simulated. Person detection can send transient frames only to your explicitly configured trusted local development detector. There is no microphone, recording, face identification, weapon result, alert or stored footage.</p>
      </div>

      <div className="local-camera-layout">
        <div className="local-camera-monitor">
          <header>
            <span><Icon name="camera" size={16} /> Local device camera</span>
            <span className={state.phase === "live" ? "local-camera-status live" : "local-camera-status"}>
              {state.phase === "live" ? "Camera connected" : busy ? "Connecting" : "Camera off"}
            </span>
          </header>
          <div className="local-camera-frame">
            <video ref={videoRef} muted playsInline autoPlay aria-hidden={state.phase !== "live"} aria-label="Real local camera preview; no model inference" />
            {state.phase !== "live" && (
              <div className="local-camera-placeholder">
                <Icon name="camera" size={42} />
                <strong>{busy ? "Connecting your camera" : "Camera preview"}</strong>
                <span>{busy ? "Respond to the browser permission prompt." : "Choose a camera, confirm scene permission, then tap Start."}</span>
              </div>
            )}
            {state.phase === "live" && personDetectionEnabled && detections.map((detection, index) => (
              <span
                className="local-person-box"
                key={`${detection.left}-${detection.top}-${detection.right}-${detection.bottom}-${index}`}
                style={{
                  left: `${(detection.left / inferenceFrame.width) * 100}%`,
                  top: `${(detection.top / inferenceFrame.height) * 100}%`,
                  width: `${((detection.right - detection.left) / inferenceFrame.width) * 100}%`,
                  height: `${((detection.bottom - detection.top) / inferenceFrame.height) * 100}%`,
                }}
              >Person · {Math.round(detection.confidence * 100)}%</span>
            ))}
          </div>
          <p className="local-camera-feedback" role="status" aria-live="polite" data-phase={state.phase}>{state.message}</p>
        </div>

        <aside className="local-camera-controls" aria-labelledby="camera-controls-title">
          <span className="local-camera-eyebrow">CAMERA CONTROLS</span>
          <h2 id="camera-controls-title">Connect your camera</h2>
          <label htmlFor="local-camera-facing">Preferred camera</label>
          <select id="local-camera-facing" value={facing} disabled={busy} onChange={(event) => {
            sessionRef.current?.stop("Camera selection changed. Tap Start to connect the selected camera.");
            setFacing(event.target.value as CameraFacing);
          }}>
            <option value="environment">Rear camera · point at the scene</option>
            <option value="user">Front camera · face yourself</option>
          </select>
          <p className="local-camera-hint">The browser selects the nearest available camera. Changing this selection stops capture.</p>
          <label className="local-camera-authorization">
            <input type="checkbox" checked={authorized} onChange={(event) => {
              setAuthorized(event.target.checked);
              if (!event.target.checked) sessionRef.current?.stop("Scene permission unchecked. Camera stopped.");
            }} />
            <span>I am authorized to show this scene and the people in it.</span>
          </label>
          <p className="local-camera-hint">This acknowledgement is not biometric enrollment or face-recognition consent.</p>
          <div className="local-camera-buttons">
            <button type="button" className="local-camera-start" disabled={!authorized || capturing} onClick={() => { void sessionRef.current?.start(facing, authorized); }}>
              <Icon name="play" size={16} /> Start camera
            </button>
            <button type="button" className="local-camera-stop" disabled={!capturing} onClick={() => sessionRef.current?.stop()}>
              <Icon name="close" size={16} /> Stop
            </button>
          </div>
          <label className="local-camera-authorization local-person-toggle">
            <input
              type="checkbox"
              checked={personDetectionEnabled}
              disabled={state.phase !== "live" || !inferenceEndpoint}
              onChange={(event) => {
                setPersonDetectionEnabled(event.target.checked);
                setDetections([]);
                setInferenceMessage(event.target.checked ? "Starting local person detection." : "Person detection is off.");
              }}
            />
            <span>Enable person detection</span>
          </label>
          <p className="local-camera-hint" role="status" aria-live="polite">
            {!inferenceEndpoint
              ? "Person detection needs a trusted local HTTPS endpoint configured for this build."
              : inferenceMessage}
          </p>
          <p className="local-camera-hint">Camera access needs HTTPS and browser permission. Leaving or hiding this page stops the camera; returning does not restart it.</p>
        </aside>
      </div>

      <section className="local-camera-capabilities" aria-label="Actual model availability">
        {[
          { title: "Person detection", detail: inferenceEndpoint ? "Development-only local inference; every frame is transient and user-enabled." : "Trusted local HTTPS detector not configured for this build." },
          { title: "Firearm & knife detection", detail: "Approved detector, tool negatives and human review required." },
          { title: "Enrolled-name recognition", detail: "Biometric approval, consent, secure enrollment and liveness required." },
        ].map((capability) => (
          <article key={capability.title}>
            <span>NOT CONNECTED</span><h2>{capability.title}</h2><p>{capability.detail}</p>
          </article>
        ))}
      </section>
    </section>
  );
}
