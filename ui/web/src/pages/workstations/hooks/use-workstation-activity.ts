import { useState, useCallback } from "react";
import { useWs } from "@/hooks/use-ws";
import { Methods } from "@/api/protocol";

export interface WorkstationActivity {
  id: string;
  tenantId: string;
  workstationId: string;
  agentId: string;
  action: "exec" | "deny";
  cmdHash: string;
  cmdPreview: string;
  exitCode: number | null;
  durationMs: number | null;
  denyReason: string;
  createdAt: string;
}

export interface ActivityFilters {
  workstationId?: string;
  agentId?: string;
}

interface UseWorkstationActivityResult {
  rows: WorkstationActivity[];
  loading: boolean;
  error: string | null;
  hasMore: boolean;
  load: (filters: ActivityFilters) => Promise<void>;
  loadMore: () => Promise<void>;
}

export function useWorkstationActivity(): UseWorkstationActivityResult {
  const ws = useWs();
  const [rows, setRows] = useState<WorkstationActivity[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [cursor, setCursor] = useState<string | undefined>(undefined);
  const [hasMore, setHasMore] = useState(false);
  const [currentFilters, setCurrentFilters] = useState<ActivityFilters>({});

  const load = useCallback(
    async (filters: ActivityFilters) => {
      setLoading(true);
      setError(null);
      setCurrentFilters(filters);
      setCursor(undefined);
      try {
        const params: Record<string, unknown> = {
          limit: 50,
        };
        if (filters.workstationId) {
          params.workstationId = filters.workstationId;
        }
        if (filters.agentId) {
          params.agentId = filters.agentId;
        }
        const res = await ws.call<{
          activity: WorkstationActivity[];
          nextCursor?: string;
        }>(Methods.WORKSTATIONS_LIST_ACTIVITY, params);
        setRows(res.activity ?? []);
        setCursor(res.nextCursor);
        setHasMore(!!res.nextCursor);
      } catch (err) {
        setError(err instanceof Error ? err.message : "Failed to load activity");
      } finally {
        setLoading(false);
      }
    },
    [ws],
  );

  const loadMore = useCallback(async () => {
    if (!cursor || loading) return;
    setLoading(true);
    try {
      const params: Record<string, unknown> = {
        limit: 50,
        cursor,
      };
      if (currentFilters.workstationId) {
        params.workstationId = currentFilters.workstationId;
      }
      if (currentFilters.agentId) {
        params.agentId = currentFilters.agentId;
      }
      const res = await ws.call<{
        activity: WorkstationActivity[];
        nextCursor?: string;
      }>(Methods.WORKSTATIONS_LIST_ACTIVITY, params);
      setRows((prev) => [...prev, ...(res.activity ?? [])]);
      setCursor(res.nextCursor);
      setHasMore(!!res.nextCursor);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to load more activity");
    } finally {
      setLoading(false);
    }
  }, [ws, currentFilters, cursor, loading]);

  return { rows, loading, error, hasMore, load, loadMore };
}
