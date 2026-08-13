export type ApiCamera = {
  id: string;
  tenant_id: string;
  site_id: string;
  serial_number: string;
  name: string;
  group_name?: string;
  tags: string[];
  lifecycle_status: "pending" | "active" | "offline" | "retired";
  config_version: number;
  created_at: string;
  updated_at: string;
};

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function isString(value: unknown): value is string {
  return typeof value === "string" && value.length > 0;
}

export function isApiCamera(value: unknown): value is ApiCamera {
  if (!isRecord(value)) return false;
  return (
    isString(value.id) &&
    isString(value.tenant_id) &&
    isString(value.site_id) &&
    isString(value.serial_number) &&
    isString(value.name) &&
    (value.group_name === undefined || typeof value.group_name === "string") &&
    Array.isArray(value.tags) &&
    value.tags.every(isString) &&
    typeof value.lifecycle_status === "string" &&
    ["pending", "active", "offline", "retired"].includes(
      value.lifecycle_status,
    ) &&
    Number.isInteger(value.config_version) &&
    Number(value.config_version) > 0 &&
    isString(value.created_at) &&
    isString(value.updated_at)
  );
}

export function parseCameraListEnvelope(value: unknown): ApiCamera[] {
  if (
    !isRecord(value) ||
    !Array.isArray(value.data) ||
    !value.data.every(isApiCamera)
  ) {
    throw new Error("Camera list response did not match the v1 contract.");
  }
  return value.data;
}
