import { create } from "zustand";
import { persist } from "zustand/middleware";
import { LOCAL_STORAGE_KEYS } from "@/lib/constants";
import { clearSetupSkippedState } from "@/lib/setup-skip";
import type { TenantMembership } from "@/types/tenant";

type UserRole = "owner" | "admin" | "member" | "viewer" | "";
export type Edition = "standard" | "lite";

interface AuthState {
  token: string;
  userId: string;
  senderID: string; // browser pairing: persistent device identity
  connected: boolean;
  role: UserRole; // server-assigned role from connect response
  serverInfo: { name?: string; version?: string } | null;
  tenantId: string;
  tenantName: string;
  tenantSlug: string;
  isOwner: boolean;
  isMasterScope: boolean; // server-derived: owner OR on master tenant — advisory UI hint only
  edition: Edition; // server edition — UI feature gating
  permissions: string[]; // effective permissions from /v1/users/me/permissions
  availableTenants: TenantMembership[];
  tenantSelected: boolean; // true after user picks a tenant (or auto-selected)
  isGatewayToken: boolean; // true when logged in via Gateway Auth Token

  setCredentials: (token: string, userId: string) => void;
  setPairing: (senderID: string, userId: string) => void;
  setConnected: (connected: boolean, serverInfo?: { name?: string; version?: string }) => void;
  setRole: (role: UserRole) => void;
  setTenant: (id: string, name: string, slug: string, isOwner: boolean) => void;
  setConnectInfo: (info: { isMasterScope: boolean; edition: Edition }) => void;
  setPermissions: (permissions: string[]) => void;
  setAvailableTenants: (tenants: TenantMembership[]) => void;
  setTenantSelected: (selected: boolean) => void;
  setIsGatewayToken: (value: boolean) => void;
  logout: () => void;
}

function getPersistedAuth(): { token: string; userId: string; senderID: string } | null {
  try {
    const raw = localStorage.getItem("goclaw:auth");
    if (!raw) return null;
    const parsed = JSON.parse(raw);
    return {
      token: parsed.state?.token ?? "",
      userId: parsed.state?.userId ?? "",
      senderID: parsed.state?.senderID ?? "",
    };
  } catch {
    return null;
  }
}

const persisted = getPersistedAuth();

export const useAuthStore = create<AuthState>()(
  persist(
    (set) => ({
      token: persisted?.token ?? "",
      userId: persisted?.userId ?? "",
      senderID: persisted?.senderID ?? "",
      connected: false,
      role: "" as UserRole,
      serverInfo: null,
      tenantId: "",
      tenantName: "",
      tenantSlug: "",
      isOwner: false,
      isMasterScope: false,
      edition: "standard" as Edition,
      permissions: [],
      availableTenants: [],
      tenantSelected: !!localStorage.getItem(LOCAL_STORAGE_KEYS.TENANT_ID),
      isGatewayToken: false,

      setCredentials: (token, userId) => {
        set({ token, userId });
      },

      setPairing: (senderID, userId) => {
        set({ senderID, userId });
      },

      setConnected: (connected, serverInfo) => {
        set({ connected, serverInfo: serverInfo ?? null });
      },

      setRole: (role) => {
        set({ role });
      },

      setTenant: (id, name, slug, isOwner) => {
        set({ tenantId: id, tenantName: name, tenantSlug: slug, isOwner });
      },

      setConnectInfo: ({ isMasterScope, edition }) => {
        set({ isMasterScope, edition });
      },

      setPermissions: (permissions) => {
        set({ permissions });
      },

      setAvailableTenants: (tenants) => {
        set({ availableTenants: tenants });
      },

      setTenantSelected: (selected) => {
        set({ tenantSelected: selected });
      },

      setIsGatewayToken: (value) => {
        set({ isGatewayToken: value });
      },

      logout: () => {
        // Remove tenant scope keys that are still managed outside persist
        localStorage.removeItem("goclaw:tenant_id");
        localStorage.removeItem("goclaw:tenant_hint");
        clearSetupSkippedState();
        set({
          token: "", userId: "", senderID: "", connected: false, role: "", serverInfo: null,
          tenantId: "", tenantName: "", tenantSlug: "", isOwner: false,
          isMasterScope: false, edition: "standard", permissions: [],
          availableTenants: [], tenantSelected: false, isGatewayToken: false,
        });
      },
    }),
    {
      name: "goclaw:auth", // localStorage key
      partialize: (state) => ({
        // Only persist credentials — not transient runtime state
        token: state.token,
        userId: state.userId,
        senderID: state.senderID,
        isGatewayToken: state.isGatewayToken,
      }),
    }
  )
);
