import { useAuthStore } from "@/stores/use-auth-store";
import { useTenants } from "@/hooks/use-tenants";

const ROLE_LEVELS: Record<string, number> = {
  owner: 4,
  admin: 3,
  member: 2,
  viewer: 1,
};

export type UserRole = "owner" | "admin" | "member" | "viewer" | "";

export interface RoleInfo {
  role: UserRole;
  isOwner: boolean;
  isAdmin: boolean;
  isMember: boolean;
  isViewer: boolean;
  hasMinRole: (min: UserRole | string) => boolean;
}

/**
 * Shared hook for role-based access control.
 * Derives isOwner/isAdmin/isMember/isViewer from the auth store role.
 * Also exposes hasMinRole for fine-grained checks.
 *
 * Note: isOwner here is the role-level check. For tenant ownership
 * (from useTenants), use useTenants() directly.
 */
export function useRole(): RoleInfo {
  const role = useAuthStore((s) => s.role) as UserRole;
  const { isOwner: isTenantOwner } = useTenants();

  return {
    role,
    isOwner: isTenantOwner || role === "owner",
    isAdmin: role === "admin" || role === "owner",
    isMember: role === "member" || role === "admin" || role === "owner",
    isViewer: role === "viewer",
    hasMinRole: (min) => (ROLE_LEVELS[role] ?? 0) >= (ROLE_LEVELS[min] ?? 0),
  };
}
