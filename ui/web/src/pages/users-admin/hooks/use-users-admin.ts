import { useCallback } from "react";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import i18next from "i18next";
import { useHttp } from "@/hooks/use-ws";
import { queryKeys } from "@/lib/query-keys";
import { toast } from "@/stores/use-toast-store";
import { useAuthStore } from "@/stores/use-auth-store";
import type { TenantUserData } from "@/types/tenant";

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

export function useUsersAdmin() {
  const http = useHttp();
  const queryClient = useQueryClient();
  const tenantId = useAuthStore((s) => s.tenantId);

  const queryKey = queryKeys.tenants.users(tenantId);

  const { data: users = [], isLoading: loading, isFetching: refreshing } = useQuery({
    queryKey,
    queryFn: async () => {
      const res = await http.get<{ users: TenantUserData[] }>(`/v1/tenants/${tenantId}/users`);
      return res?.users ?? [];
    },
    enabled: !!tenantId,
    staleTime: 30_000,
  });

  const invalidate = useCallback(
    () => queryClient.invalidateQueries({ queryKey: queryKeys.tenants.users(tenantId) }),
    [queryClient, tenantId],
  );

  const createUser = useMutation({
    mutationFn: async (input: CreateUserInput) => {
      return http.post(`/v1/tenants/${tenantId}/users`, input);
    },
    onSuccess: () => {
      invalidate();
      toast.success(i18next.t("users-admin:toast.created"));
    },
    onError: (err: Error) => {
      toast.error(i18next.t("users-admin:toast.failedCreate"), err.message);
    },
  });

  const updateUser = useMutation({
    mutationFn: async ({ userId, input }: { userId: string; input: UpdateUserInput }) => {
      return http.put(`/v1/tenants/${tenantId}/users/${userId}`, input);
    },
    onSuccess: () => {
      invalidate();
      toast.success(i18next.t("users-admin:toast.userUpdated"));
    },
    onError: (err: Error) => {
      toast.error(i18next.t("users-admin:toast.updateUserFailed"), err.message);
    },
  });

  const removeUser = useMutation({
    mutationFn: async (userId: string) => {
      return http.delete(`/v1/tenants/${tenantId}/users/${userId}`);
    },
    onSuccess: () => {
      invalidate();
      toast.success(i18next.t("users-admin:toast.userDeleted"));
    },
    onError: (err: Error) => {
      toast.error(i18next.t("users-admin:toast.failedDeleteUser"), err.message);
    },
  });

  const changeRole = useMutation({
    mutationFn: async ({ userId, role }: { userId: string; role: string }) => {
      return http.put(`/v1/tenants/${tenantId}/users/${userId}/role`, { role });
    },
    onSuccess: () => {
      invalidate();
      toast.success(i18next.t("users-admin:toast.roleChanged"));
    },
    onError: (err: Error) => {
      toast.error(i18next.t("users-admin:toast.failedRoleChange"), err.message);
    },
  });

  const changePassword = useMutation({
    mutationFn: async ({ userId, password }: { userId: string; password: string }) => {
      return http.put(`/v1/tenants/${tenantId}/users/${userId}/password`, { password });
    },
    onSuccess: () => {
      toast.success(i18next.t("users-admin:toast.passwordChanged"));
    },
    onError: (err: Error) => {
      toast.error(i18next.t("users-admin:toast.failedPasswordChange"), err.message);
    },
  });

  return {
    users,
    total: users.length,
    loading,
    refreshing,
    refresh: invalidate,
    createUser: createUser.mutateAsync,
    updateUser: updateUser.mutateAsync,
    removeUser: removeUser.mutateAsync,
    changeRole: changeRole.mutateAsync,
    changePassword: changePassword.mutateAsync,
    isCreating: createUser.isPending,
    isUpdating: updateUser.isPending,
    isRemoving: removeUser.isPending,
    isSavingRole: changeRole.isPending,
    isChangingPassword: changePassword.isPending,
  };
}
