import { useCallback } from "react";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import i18next from "i18next";
import { useHttp } from "@/hooks/use-ws";
import { queryKeys } from "@/lib/query-keys";
import { toast } from "@/stores/use-toast-store";
import type { Role } from "@/types/user-mgmt";

interface RoleListParams {
  // 005: the explicitly-viewed tenant (UUID or slug). When provided the hook uses
  // the tenant-scoped endpoint so the result follows the viewed tenant, not the
  // caller's ambient active tenant.
  tenantId?: string;
  search?: string;
  limit?: number;
  offset?: number;
}

// GET /v1/roles returns this shape (see internal/http/roles.go handleList):
// { roles: [...], total, offset, limit }. Note: NOT PaginatedResult (which uses `items`).
interface RoleListResponse {
  roles: Role[];
  total: number;
  offset: number;
  limit: number;
}

export function useRoles(params: RoleListParams = {}) {
  const http = useHttp();
  const queryClient = useQueryClient();

  const queryParams: Record<string, string> = {};
  if (params.search) queryParams.search = params.search;
  queryParams.limit = String(params.limit ?? 50);
  queryParams.offset = String(params.offset ?? 0);

  // 005: prefer the tenant-scoped endpoint when a viewed tenant is supplied.
  const endpoint = params.tenantId
    ? `/v1/tenants/${params.tenantId}/roles`
    : "/v1/roles";
  // Include tenantId in the key so different tenants cache independently.
  const queryKey = queryKeys.roles.list({ ...queryParams, tenantId: params.tenantId ?? "" });

  const { data, isLoading: loading } = useQuery({
    queryKey,
    queryFn: () => http.get<RoleListResponse>(endpoint, queryParams),
    staleTime: 30_000,
  });

  // Backend returns { roles: [...], total, offset, limit } (see internal/http/roles.go handleList).
  const roles = data?.roles ?? [];
  const total = data?.total ?? 0;

  const invalidate = useCallback(
    () => queryClient.invalidateQueries({ queryKey: queryKeys.roles.all }),
    [queryClient],
  );

  const createRole = useMutation({
    mutationFn: async (input: { name: string; description?: string }) => {
      return http.post<Role>("/v1/roles", input);
    },
    onSuccess: () => {
      invalidate();
      toast.success(i18next.t("role-management:toast.created"));
    },
    onError: (err: Error) => {
      toast.error(i18next.t("role-management:toast.failedCreate"), err.message);
    },
  });

  const updateRole = useMutation({
    mutationFn: async ({ id, ...input }: { id: string; name?: string; description?: string }) => {
      return http.patch<Role>(`/v1/roles/${id}`, input);
    },
    onSuccess: () => {
      invalidate();
      toast.success(i18next.t("role-management:toast.updated"));
    },
    onError: (err: Error) => {
      toast.error(i18next.t("role-management:toast.failedUpdate"), err.message);
    },
  });

  const deleteRole = useMutation({
    mutationFn: async (id: string) => {
      await http.delete(`/v1/roles/${id}`);
    },
    onSuccess: () => {
      invalidate();
      toast.success(i18next.t("role-management:toast.deleted"));
    },
    onError: (err: Error) => {
      toast.error(i18next.t("role-management:toast.failedDelete"), err.message);
    },
  });

  const setRolePermissions = useMutation({
    mutationFn: async ({ id, permissions }: { id: string; permissions: string[] }) => {
      await http.put(`/v1/roles/${id}/permissions`, { permissions });
    },
    onSuccess: (_data, vars) => {
      queryClient.invalidateQueries({ queryKey: queryKeys.roles.permissions(vars.id) });
      invalidate();
      toast.success(i18next.t("role-management:toast.permissionsUpdated"));
    },
    onError: (err: Error) => {
      toast.error(i18next.t("role-management:toast.failedPermissionsUpdate"), err.message);
    },
  });

  return {
    roles,
    total,
    loading,
    refresh: invalidate,
    createRole: createRole.mutateAsync,
    updateRole: updateRole.mutateAsync,
    deleteRole: deleteRole.mutateAsync,
    setRolePermissions: setRolePermissions.mutateAsync,
    isCreating: createRole.isPending,
    isUpdating: updateRole.isPending,
    isDeleting: deleteRole.isPending,
    isSettingPermissions: setRolePermissions.isPending,
  };
}

export function useRolePermissions(roleId: string | null) {
  const http = useHttp();

  const queryKey = roleId ? queryKeys.roles.permissions(roleId) : ["roles", "permissions", "none"];

  const { data, isLoading: loading } = useQuery({
    queryKey,
    queryFn: () => http.get<{ permissions: string[] }>(`/v1/roles/${roleId}/permissions`),
    enabled: !!roleId,
    staleTime: 30_000,
  });

  return {
    permissions: data?.permissions ?? [],
    loading,
  };
}

export function useAllPermissions() {
  const http = useHttp();

  const { data, isLoading: loading } = useQuery({
    queryKey: ["permissions", "all"],
    queryFn: () => http.get<{ permissions: string[] }>("/v1/users/me/permissions"),
    staleTime: 60_000,
  });

  return {
    permissions: data?.permissions ?? [],
    loading,
  };
}
