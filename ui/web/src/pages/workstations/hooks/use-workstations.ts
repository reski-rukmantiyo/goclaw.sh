import { useState, useEffect, useCallback } from "react";
import { useWs } from "@/hooks/use-ws";
import { useAuthStore } from "@/stores/use-auth-store";
import { Methods } from "@/api/protocol";

export interface Workstation {
  id: string;
  workstationKey: string;
  name: string;
  backendType: "ssh" | "docker";
  active: boolean;
  createdAt: string;
  updatedAt: string;
  metadataSummary?: Record<string, unknown>;
}

export interface CreateWorkstationParams {
  workstationKey: string;
  name: string;
  backendType: "ssh" | "docker";
  metadata?: Record<string, unknown>;
}

export interface UpdateWorkstationParams {
  name?: string;
  active?: boolean;
  metadata?: Record<string, unknown>;
}

export function useWorkstations() {
  const ws = useWs();
  const connected = useAuthStore((s) => s.connected);
  const [workstations, setWorkstations] = useState<Workstation[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const load = useCallback(async () => {
    if (!connected) return;
    setLoading(true);
    setError(null);
    try {
      const res = await ws.call<{ workstations: Workstation[] }>(Methods.WORKSTATIONS_LIST);
      setWorkstations(res.workstations ?? []);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to load workstations");
    } finally {
      setLoading(false);
    }
  }, [ws, connected]);

  useEffect(() => {
    load();
  }, [load]);

  const getWorkstation = useCallback(
    async (id: string): Promise<Workstation & { metadataSummary?: Record<string, unknown> }> => {
      const res = await ws.call<{ workstation: Workstation & { metadataSummary?: Record<string, unknown> } }>(
        Methods.WORKSTATIONS_GET,
        { id },
      );
      return res.workstation;
    },
    [ws],
  );

  const createWorkstation = useCallback(
    async (params: CreateWorkstationParams): Promise<Workstation> => {
      const res = await ws.call<{ workstation: Workstation }>(Methods.WORKSTATIONS_CREATE, params as unknown as Record<string, unknown>);
      await load();
      return res.workstation;
    },
    [ws, load],
  );

  const updateWorkstation = useCallback(
    async (id: string, params: UpdateWorkstationParams): Promise<void> => {
      await ws.call(Methods.WORKSTATIONS_UPDATE, { id, ...params });
      await load();
    },
    [ws, load],
  );

  const deleteWorkstation = useCallback(
    async (id: string): Promise<void> => {
      await ws.call(Methods.WORKSTATIONS_DELETE, { id });
      await load();
    },
    [ws, load],
  );

  const toggleWorkstation = useCallback(
    async (id: string, active: boolean): Promise<void> => {
      await ws.call(Methods.WORKSTATIONS_TOGGLE, { id, active });
      await load();
    },
    [ws, load],
  );

  const listLinkedAgents = useCallback(
    async (workstationId: string): Promise<{ agentId: string; isDefault: boolean }[]> => {
      const res = await ws.call<{ links: { agentId: string; isDefault: boolean }[] }>(
        Methods.WORKSTATIONS_LIST_LINKED_AGENTS,
        { workstationId }
      );
      return res.links ?? [];
    },
    [ws],
  );

  const linkAgent = useCallback(
    async (workstationId: string, agentId: string, isDefault = false): Promise<void> => {
      await ws.call(Methods.WORKSTATIONS_LINK_AGENT, { workstationId, agentId, isDefault });
      await load();
    },
    [ws, load],
  );

  const unlinkAgent = useCallback(
    async (workstationId: string, agentId: string): Promise<void> => {
      await ws.call(Methods.WORKSTATIONS_UNLINK_AGENT, { workstationId, agentId });
      await load();
    },
    [ws, load],
  );

  const listPermissions = useCallback(
    async (workstationId: string): Promise<{ id: string; pattern: string; enabled: boolean }[]> => {
      const res = await ws.call<{ permissions: { id: string; pattern: string; enabled: boolean }[] }>(
        Methods.WORKSTATIONS_PERM_LIST,
        { workstationId }
      );
      return res.permissions ?? [];
    },
    [ws],
  );

  const addPermission = useCallback(
    async (workstationId: string, pattern: string): Promise<void> => {
      await ws.call(Methods.WORKSTATIONS_PERM_ADD, { workstationId, pattern, enabled: true });
    },
    [ws],
  );

  const removePermission = useCallback(
    async (_workstationId: string, id: string): Promise<void> => {
      await ws.call(Methods.WORKSTATIONS_PERM_REMOVE, { id });
    },
    [ws],
  );

  const togglePermission = useCallback(
    async (id: string, enabled: boolean): Promise<void> => {
      await ws.call(Methods.WORKSTATIONS_PERM_TOGGLE, { id, enabled });
    },
    [ws],
  );

  return {
    workstations,
    loading,
    error,
    refresh: load,
    getWorkstation,
    createWorkstation,
    updateWorkstation,
    deleteWorkstation,
    toggleWorkstation,
    listLinkedAgents,
    linkAgent,
    unlinkAgent,
    listPermissions,
    addPermission,
    removePermission,
    togglePermission,
  };
}
