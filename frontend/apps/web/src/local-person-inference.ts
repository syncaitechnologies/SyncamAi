export type LocalPersonDetection = {
  confidence: number;
  left: number;
  top: number;
  right: number;
  bottom: number;
};

export type LocalPersonFrame = {
  width: number;
  height: number;
  rgba: Uint8ClampedArray;
};

const developmentPath = "/local-dev/v1/person-detections";
const maxFramePixels = 640 * 480;

export function localPersonInferenceEndpoint(value: string | undefined): URL | null {
  if (!value?.trim()) return null;
  try {
    const url = new URL(value);
    if (
      url.protocol !== "https:"
      || !url.host
      || url.username
      || url.password
      || url.pathname !== "/"
      || url.search
      || url.hash
    ) return null;
    return new URL(developmentPath, url);
  } catch {
    return null;
  }
}

export async function requestLocalPersonDetections(
  endpoint: URL,
  frame: LocalPersonFrame,
  signal: AbortSignal,
): Promise<LocalPersonDetection[]> {
  validateFrame(frame);
  const frameBytes = new Uint8Array(frame.rgba.byteLength);
  frameBytes.set(frame.rgba);
  const response = await fetch(`${endpoint.href}?width=${frame.width}&height=${frame.height}`, {
    method: "POST",
    headers: { "Content-Type": "application/octet-stream" },
    body: frameBytes.buffer,
    cache: "no-store",
    signal,
  });
  if (!response.ok) throw new Error("Local person detector rejected the frame.");
  return parseLocalPersonResponse(await response.json(), frame.width, frame.height);
}

export function parseLocalPersonResponse(
  payload: unknown,
  width: number,
  height: number,
): LocalPersonDetection[] {
  if (!payload || typeof payload !== "object" || !Array.isArray((payload as { detections?: unknown }).detections)) {
    throw new Error("Local person detector returned an invalid response.");
  }
  return (payload as { detections: unknown[] }).detections.map((value) => {
    if (!value || typeof value !== "object") throw new Error("Local person detector returned an invalid response.");
    const detection = value as Record<string, unknown>;
    const { confidence, left, top, right, bottom } = detection;
    if (
      !isFiniteNumber(confidence) || !isFiniteNumber(left) || !isFiniteNumber(top)
      || !isFiniteNumber(right) || !isFiniteNumber(bottom)
    ) {
      throw new Error("Local person detector returned an invalid response.");
    }
    if (
      confidence < 0 || confidence > 1 || !Number.isInteger(left) || !Number.isInteger(top)
      || !Number.isInteger(right) || !Number.isInteger(bottom) || left < 0 || top < 0
      || right > width || bottom > height || left >= right || top >= bottom
    ) throw new Error("Local person detector returned an invalid response.");
    return { confidence, left, top, right, bottom };
  });
}

function isFiniteNumber(value: unknown): value is number {
  return typeof value === "number" && Number.isFinite(value);
}

function validateFrame(frame: LocalPersonFrame): void {
  if (
    !Number.isInteger(frame.width) || !Number.isInteger(frame.height) || frame.width < 1 || frame.height < 1
    || frame.width > 640 || frame.height > 640 || frame.width * frame.height > maxFramePixels
    || frame.rgba.byteLength !== frame.width * frame.height * 4
  ) throw new Error("Camera frame exceeds the local development limit.");
}
