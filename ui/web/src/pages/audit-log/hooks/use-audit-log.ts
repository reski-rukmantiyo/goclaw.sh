import { useCallback } from "react";
import { useQuery } from "@tanstack/react-query";
import { useHttp } from "@/hooks/use-ws";
import { queryKeys } from "@/lib/query-keys";

export interface AuditEntry {
  id: string;
  time: string;
  actor: string;
  action: string;
  resource_type: string;
  resource_id: string;
  group: string;
  ip_address: string;
  detail: Record<string, unknown> | null;
}

export interface AuditListResponse {
  entries: AuditEntry[];
  total: number;
}

export interface AuditListParams {
  limit?: number;
  offset?: number;
  action?: string;
  resource_type?: string;
}

export function useAuditLog(params: AuditListParams = {}) {
  const http = useHttp();

  const { data, isLoading: loading, refetch } = useQuery({
    queryKey: queryKeys.auditLog.list(params as Record<string, unknown>),
    queryFn: async (): Promise<AuditListResponse> => {
      const searchParams = new URLSearchParams();
      if (params.limit) searchParams.set("limit", String(params.limit));
      if (params.offset) searchParams.set("offset", String(params.offset));
      if (params.action) searchParams.set("action", params.action);
      if (params.resource_type) searchParams.set("resource_type", params.resource_type);
      const qs = searchParams.toString();
      return http.get<AuditListResponse>(`/v1/audit${qs ? `?${qs}` : ""}`);
    },
    staleTime: 30_000,
  });

  const refresh = useCallback(() => refetch(), [refetch]);

  return {
    entries: data?.entries ?? [],
    total: data?.total ?? 0,
    loading,
    refresh,
  };
}
