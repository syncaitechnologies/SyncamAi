export type CameraFacing = "environment" | "user";
export type CameraPhase = "idle" | "requesting" | "starting" | "live" | "stopped" | "error";
export type CameraState = { phase: CameraPhase; message: string };
type CameraTrack = Pick<MediaStreamTrack, "readyState" | "stop" | "addEventListener" | "removeEventListener">;
export type CameraStream = { getTracks(): CameraTrack[]; getVideoTracks(): CameraTrack[] };
type CameraHost<S extends CameraStream> = {
  secure: boolean;
  supported: boolean;
  visible(): boolean;
  request(constraints: MediaStreamConstraints): Promise<S>;
  attach(stream: S | null): void;
  play(): Promise<void>;
  onState(state: CameraState): void;
};

export function initialCameraState(): CameraState {
  return { phase: "idle", message: "Camera off. Models are not connected." };
}

export function initialLocalCameraView(search: string, mode: "demo" | "live"): "local-camera" | null {
  return mode === "demo" && new URLSearchParams(search).get("camera") === "local"
    ? "local-camera" : null;
}

export function cameraErrorMessage(error: unknown): string {
  const name = typeof error === "object" && error !== null && "name" in error ? error.name : "";
  switch (name) {
    case "NotAllowedError":
    case "SecurityError": return "Camera access was blocked. Allow camera access in the browser and iPhone settings, then tap Start again.";
    case "NotFoundError": return "No camera is available on this device.";
    case "NotReadableError": return "The camera is busy or unavailable. Close other camera apps and try again.";
    case "OverconstrainedError": return "This camera cannot use the requested settings. Try the other camera.";
    default: return "The camera could not start. Check browser permissions and try again.";
  }
}

export class LocalCameraSession<S extends CameraStream> {
  private epoch = 0;
  private stream: S | null = null;
  private trackCleanup: (() => void) | null = null;
  private disposed = false;
  private readonly host: CameraHost<S>;

  constructor(host: CameraHost<S>) { this.host = host; }

  private release(): void {
    this.trackCleanup?.();
    this.trackCleanup = null;
    const previous = this.stream;
    this.stream = null;
    this.host.attach(null);
    previous?.getTracks().forEach((track) => track.stop());
  }

  async start(facing: CameraFacing, authorized: boolean): Promise<void> {
    if (this.disposed) return;
    const epoch = ++this.epoch;
    this.release();
    const fail = (message: string) => this.host.onState({ phase: "error", message });
    if (!authorized) { fail("Confirm that you are authorized to show this scene before starting."); return; }
    if (!this.host.secure) { fail("Open the app over HTTPS to use the phone camera."); return; }
    if (!this.host.supported) { fail("Camera capture is not supported in this browser. Try an updated iPhone browser."); return; }
    if (!this.host.visible()) { fail("Keep this page visible, then tap Start camera again."); return; }
    this.host.onState({ phase: "requesting", message: "Waiting for browser camera permission. You can cancel with Stop." });
    try {
      const stream = await this.host.request({
        audio: false,
        video: { facingMode: { ideal: facing }, width: { ideal: 1280 }, height: { ideal: 720 }, frameRate: { ideal: 15, max: 30 } },
      });
      if (epoch !== this.epoch || this.disposed || !this.host.visible()) {
        stream.getTracks().forEach((track) => track.stop());
        if (epoch === this.epoch && !this.disposed) this.stop("Camera stopped because this page is no longer visible.");
        return;
      }
      this.stream = stream;
      if (!stream.getVideoTracks().some((track) => track.readyState === "live")) {
        this.release(); fail("The camera returned no active video. Tap Start to try again."); return;
      }
      const onEnded = () => {
        if (epoch === this.epoch) this.stop("Camera disconnected. Tap Start to reconnect.");
      };
      stream.getTracks().forEach((track) => track.addEventListener("ended", onEnded));
      this.trackCleanup = () => stream.getTracks().forEach((track) => track.removeEventListener("ended", onEnded));
      this.host.attach(stream);
      this.host.onState({ phase: "starting", message: "Starting local camera preview. No video is uploaded." });
      await this.host.play();
      if (epoch !== this.epoch || this.disposed) return;
      if (!this.host.visible()) { this.stop("Camera stopped because this page is no longer visible."); return; }
      this.host.onState({ phase: "live", message: "Real camera connected. Person, weapon and face models are not connected yet." });
    } catch (error) {
      if (epoch !== this.epoch || this.disposed) return;
      this.release();
      fail(cameraErrorMessage(error));
    }
  }

  stop(message = "Camera stopped. Tap Start to reconnect."): void {
    if (this.disposed) return;
    ++this.epoch;
    this.release();
    this.host.onState({ phase: "stopped", message });
  }

  dispose(): void {
    if (this.disposed) return;
    this.disposed = true;
    ++this.epoch;
    this.release();
  }
}

export function bindCameraLifecycle(
  session: { stop(message: string): void },
  documentTarget: Pick<Document, "visibilityState" | "addEventListener" | "removeEventListener">,
  windowTarget: Pick<Window, "addEventListener" | "removeEventListener">,
): () => void {
  const onVisibility = () => {
    if (documentTarget.visibilityState !== "visible") session.stop("Camera stopped while the page was hidden. Tap Start to reconnect.");
  };
  const onPageHide = () => session.stop("Camera stopped because you left the page. Tap Start to reconnect.");
  documentTarget.addEventListener("visibilitychange", onVisibility);
  windowTarget.addEventListener("pagehide", onPageHide);
  return () => {
    documentTarget.removeEventListener("visibilitychange", onVisibility);
    windowTarget.removeEventListener("pagehide", onPageHide);
  };
}
