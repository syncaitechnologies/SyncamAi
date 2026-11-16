import type { FoundDevice, ProbeResult } from "./syncam-api";

export type ChannelChoice = { channel: number; name: string; selected: boolean; thumbnail?: string };

/** Only addresses on a private network can be checked by the site box. */
export function isPrivateIPv4(value: string) {
  const parts = value.trim().split(".");
  if (parts.length !== 4 || parts.some((part) => !/^\d{1,3}$/.test(part) || Number(part) > 255)) return false;
  const [a, b] = parts.map(Number) as [number, number, number, number];
  return a === 10 || (a === 172 && b >= 16 && b <= 31) || (a === 192 && b === 168);
}

/** A device typed in by hand when the scan did not find it. */
export function manualDevice(ip: string, port: number): FoundDevice {
  return {
    device_id: `${ip.trim()}:${port}`,
    ip: ip.trim(),
    rtsp_port: port,
    ports: [port],
    family: null,
    brand: "Unknown brand (we will try the common ones)",
    name: "",
    model: "",
    onvif: false,
  };
}

export function deviceTitle(device: FoundDevice) {
  const label = [device.name, device.model].filter(Boolean).join(" ");
  return label ? `${label} · ${device.ip}` : `${device.brand} · ${device.ip}`;
}

/** Channels with a picture start ticked; empty ones (no camera wired) do not. */
export function channelChoices(result: ProbeResult): ChannelChoice[] {
  return result.channels.map((channel) => ({
    channel: channel.channel,
    name: `Camera ${channel.channel}`,
    selected: Boolean(channel.thumbnail),
    thumbnail: channel.thumbnail,
  }));
}

export function validChoices(choices: ChannelChoice[]) {
  return choices.filter((choice) => choice.selected && choice.name.trim().length > 0 && choice.name.trim().length <= 120);
}

export function adoptPayload(deviceId: string, created: { camera_id: string; channel: number }[]) {
  return { device_id: deviceId, cameras: created.map(({ camera_id, channel }) => ({ camera_id, channel })) };
}
