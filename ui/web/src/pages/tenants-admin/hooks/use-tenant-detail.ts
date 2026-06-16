import { useCallback } from "react";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import i18next from "i18next";
import { useWs, useHttp } from "@/hooks/use-ws";
import { queryKeys } from "@/lib/query-keys";
import { toast } from "@/stores/use-toast-store";
import { Methods } from "@/api/protocol";
import type { TenantData, TenantUserData } from "@/types/tenant";

interface CreateUserInput {
  email: string;
  display_name: string;
  phone?: string;
  password: string;
  role: string;
}

interface UpdateUserInput {
  display_name?: string;
  phone?: string;
}

export function useTenantDetail(tenantId: string) {
  const ws = useWs();
  const http = useHttp();
  const queryClient = useQueryClient();

  const { data: tenant, isLoading: tenantLoading } = useQuery({
    queryKey: queryKeys.tenants.detail(tenantId),
    queryFn: async () => {
      const res = await ws.call<TenantData>(Methods.TENANTS_GET, { id: tenantId });
      return res ?? null;
    },
    enabled: !!tenantId,
    staleTime: 60_000,
  });

  const invalidateTenant = useCallback(
    () => queryClient.invalidateQueries({ queryKey: queryKeys.tenants.detail(tenantId) }),
    [queryClient, tenantId],
  );

  const { data: users = [], isLoading: usersLoading, isFetching: usersRefreshing } = useQuery({
    queryKey: queryKeys.tenants.users(tenantId),
    queryFn: async () => {
      const res = await ws.call<{ users: TenantUserData[] }>(Methods.TENANTS_USERS_LIST, { tenant_id: tenantId });
      return res?.users ?? [];
    },
    enabled: !!tenantId,
    staleTime: 60_000,
  });

  const invalidateUsers = useCallback(
    () => queryClient.invalidateQueries({ queryKey: queryKeys.tenants.users(tenantId) }),
    [queryClient, tenantId],
  );

  // --- HTTP mutations ---

  const createUserMutation = useMutation({
    mutationFn: async (input: CreateUserInput) => {
      return http.post(`/v1/tenants/${tenantId}/users`, input);
    },
    onSuccess: () => {
      invalidateUsers();
      toast.success(i18next.t("tenants:userCreated"));
    },
    onError: (err: Error) => {
      toast.error(i18next.t("tenants:createUserFailed"), err.message);
    },
  });

  const updateUserMutation = useMutation({
    mutationFn: async ({ userId, input }: { userId: string; input: UpdateUserInput }) => {
      return http.put(`/v1/tenants/${tenantId}/users/${userId}`, input);
    },
    onSuccess: () => {
      invalidateUsers();
      toast.success(i18next.t("tenants:userUpdated"));
    },
    onError: (err: Error) => {
      toast.error(i18next.t("tenants:updateUserFailed"), err.message);
    },
  });

  const removeUserMutation = useMutation({
    mutationFn: async (userId: string) => {
      return http.delete(`/v1/tenants/${tenantId}/users/${userId}`);
    },
    onSuccess: () => {
      invalidateUsers();
      toast.success(i18next.t("tenants:userDeleted"));
    },
    onError: (err: Error) => {
      toast.error(i18next.t("tenants:deleteUserFailed"), err.message);
    },
  });

  const updateUserRoleMutation = useMutation({
    mutationFn: async ({ userId, role }: { userId: string; role: string }) => {
      return http.put(`/v1/tenants/${tenantId}/users/${userId}/role`, { role });
    },
    onSuccess: () => {
      invalidateUsers();
      toast.success(i18next.t("tenants:roleUpdated"));
    },
    onError: (err: Error) => {
      toast.error(i18next.t("tenants:roleUpdateFailed"), err.message);
    },
  });

  const changePasswordMutation = useMutation({
    mutationFn: async ({ userId, password }: { userId: string; password: string }) => {
      return http.put(`/v1/tenants/${tenantId}/users/${userId}/password`, { password });
    },
    onSuccess: () => {
      toast.success(i18next.t("tenants:passwordChanged", { defaultValue: "Password changed" }));
    },
    onError: (err: Error) => {
      toast.error(i18next.t("tenants:passwordChangeFailed", { defaultValue: "Failed to change password" }), err.message);
    },
  });

  // --- WS-based tenant ops ---

  const updateTenantName = useCallback(
    async (name: string) => {
      try {
        await ws.call(Methods.TENANTS_UPDATE, { id: tenantId, name });
        await invalidateTenant();
        toast.success(i18next.t("tenants:editName"));
      } catch (err) {
        toast.error(i18next.t("tenants:editName"), err instanceof Error ? err.message : "");
        throw err;
      }
    },
    [ws, tenantId, invalidateTenant],
  );

  const deleteTenant = useCallback(
    async () => {
      try {
        await ws.call(Methods.TENANTS_DELETE, { tenant_id: tenantId });
        queryClient.invalidateQueries({ queryKey: queryKeys.tenants.list() });
        toast.success(i18next.t("tenants:deleteTenant"));
      } catch (err) {
        toast.error(i18next.t("tenants:deleteTenant"), err instanceof Error ? err.message : "");
        throw err;
      }
    },
    [ws, tenantId, queryClient],
  );

  return {
    tenant,
    tenantLoading,
    users,
    usersLoading,
    usersRefreshing,
    refreshUsers: invalidateUsers,
    // HTTP mutations
    createUser: createUserMutation.mutateAsync,
    updateUser: updateUserMutation.mutateAsync,
    removeUser: removeUserMutation.mutateAsync,
    updateUserRole: updateUserRoleMutation.mutateAsync,
    changePassword: changePasswordMutation.mutateAsync,
    // Pending flags
    isCreating: createUserMutation.isPending,
    isUpdating: updateUserMutation.isPending,
    isRemoving: removeUserMutation.isPending,
    isSavingRole: updateUserRoleMutation.isPending,
    isChangingPassword: changePasswordMutation.isPending,
    // WS mutations
    updateTenantName,
    deleteTenant,
  };
}
