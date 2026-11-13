// The signed-in user's tenant and sites, taken from verified token claims by
// the MFA gate. UI routing only: the Go API re-verifies every request.
export type Workspace = { tenantId: string; siteIds: string[]; roles: string[] };

const workspaceKey = "syncam.workspace";

export function storeWorkspace(
  workspace: Workspace | null,
  storage: Pick<Storage, "setItem" | "removeItem"> = sessionStorage,
) {
  if (workspace) storage.setItem(workspaceKey, JSON.stringify(workspace));
  else storage.removeItem(workspaceKey);
}

export function readWorkspace(
  storage: Pick<Storage, "getItem"> = sessionStorage,
): Workspace | null {
  try {
    const value = JSON.parse(storage.getItem(workspaceKey) ?? "null") as unknown;
    if (
      typeof value === "object" && value !== null &&
      typeof (value as Workspace).tenantId === "string" &&
      Array.isArray((value as Workspace).siteIds) &&
      Array.isArray((value as Workspace).roles)
    ) {
      return value as Workspace;
    }
  } catch {
    // Corrupt storage is treated as no workspace.
  }
  return null;
}

export function canManage(workspace: Workspace | null) {
  return Boolean(workspace?.roles.some((role) => role === "super_admin" || role === "site_admin"));
}
