import { useState, useEffect, useCallback } from "react";
import { useWs } from "@/hooks/use-ws";
import { useAuthStore } from "@/stores/use-auth-store";
import { Methods } from "@/api/protocol";

export interface CommandGroup {
  id: string;
  name: string;
  description: string;
  patterns: string[];
  isBuiltin: boolean;
  createdAt: string;
  updatedAt: string;
  createdBy: string;
}

export interface GroupLink {
  id: string;
  workstationId: string;
  groupId: string;
  enabled: boolean;
  createdAt: string;
}

export function useCommandGroups() {
  const ws = useWs();
  const connected = useAuthStore((s) => s.connected);
  const [groups, setGroups] = useState<CommandGroup[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const load = useCallback(async () => {
    if (!connected) return;
    setLoading(true);
    setError(null);
    try {
      const res = await ws.call<{ groups: CommandGroup[] }>(Methods.WORKSTATIONS_COMMAND_GROUPS_LIST);
      setGroups(res.groups ?? []);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to load command groups");
    } finally {
      setLoading(false);
    }
  }, [ws, connected]);

  useEffect(() => {
    load();
  }, [load]);

  const getGroup = useCallback(
    async (id: string): Promise<CommandGroup> => {
      const res = await ws.call<{ group: CommandGroup }>(Methods.WORKSTATIONS_COMMAND_GROUPS_GET, { id });
      return res.group;
    },
    [ws],
  );

  const createGroup = useCallback(
    async (params: { name: string; description: string; patterns: string[] }): Promise<CommandGroup> => {
      const res = await ws.call<{ group: CommandGroup }>(Methods.WORKSTATIONS_COMMAND_GROUPS_CREATE, params);
      await load();
      return res.group;
    },
    [ws, load],
  );

  const updateGroup = useCallback(
    async (id: string, params: { name?: string; description?: string; patterns?: string[] }): Promise<void> => {
      await ws.call(Methods.WORKSTATIONS_COMMAND_GROUPS_UPDATE, { id, updates: params });
      await load();
    },
    [ws, load],
  );

  const deleteGroup = useCallback(
    async (id: string): Promise<void> => {
      await ws.call(Methods.WORKSTATIONS_COMMAND_GROUPS_DELETE, { id });
      await load();
    },
    [ws, load],
  );

  const applyGroup = useCallback(
    async (workstationId: string, groupId: string): Promise<void> => {
      await ws.call(Methods.WORKSTATIONS_COMMAND_GROUPS_APPLY, { workstationId, groupId });
    },
    [ws],
  );

  const removeGroup = useCallback(
    async (workstationId: string, groupId: string): Promise<void> => {
      await ws.call(Methods.WORKSTATIONS_COMMAND_GROUPS_REMOVE, { workstationId, groupId });
    },
    [ws],
  );

  const toggleGroupLink = useCallback(
    async (workstationId: string, groupId: string, enabled: boolean): Promise<void> => {
      await ws.call(Methods.WORKSTATIONS_COMMAND_GROUPS_TOGGLE, { workstationId, groupId, enabled });
    },
    [ws],
  );

  const listGroupLinks = useCallback(
    async (workstationId: string): Promise<GroupLink[]> => {
      const res = await ws.call<{ links: GroupLink[] }>(
        Methods.WORKSTATIONS_COMMAND_GROUPS_LIST_FOR_WORKSTATION,
        { workstationId },
      );
      return res.links ?? [];
    },
    [ws],
  );

  return {
    groups,
    loading,
    error,
    refresh: load,
    getGroup,
    createGroup,
    updateGroup,
    deleteGroup,
    applyGroup,
    removeGroup,
    toggleGroupLink,
    listGroupLinks,
  };
}
