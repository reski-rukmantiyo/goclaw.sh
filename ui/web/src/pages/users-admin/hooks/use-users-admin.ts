import { useCallback } from "react";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import i18next from "i18next";
import { useHttp } from "@/hooks/use-ws";
import { queryKeys } from "@/lib/query-keys";
import { toast } from "@/stores/use-toast-store";
import type { User, PaginatedResult } from "@/types/user-mgmt";

interface UserListParams {
  search?: string;
  status?: string;
  limit?: number;
  offset?: number;
}

export function useUsersAdmin(params: UserListParams = {}) {
  const http = useHttp();
  const queryClient = useQueryClient();

  const queryParams: Record<string, string> = {};
  if (params.search) queryParams.search = params.search;
  if (params.status && params.status !== "all") queryParams.status = params.status;
  queryParams.limit = String(params.limit ?? 50);
  queryParams.offset = String(params.offset ?? 0);

  const queryKey = queryKeys.users.search(queryParams);

  const { data, isLoading: loading } = useQuery({
    queryKey,
    queryFn: () => http.get<PaginatedResult<User>>("/api/v1/users", queryParams),
    staleTime: 30_000,
  });

  const users = data?.items ?? [];
  const total = data?.total ?? 0;

  const invalidate = useCallback(
    () => queryClient.invalidateQueries({ queryKey: queryKeys.users.all }),
    [queryClient],
  );

  const changeStatus = useMutation({
    mutationFn: async ({ userId, status }: { userId: string; status: string }) => {
      await http.patch(`/api/v1/users/${userId}/status`, { status });
    },
    onSuccess: () => {
      invalidate();
      toast.success(i18next.t("users-admin:toast.statusChanged"));
    },
    onError: (err: Error) => {
      toast.error(i18next.t("users-admin:toast.failedStatusChange"), err.message);
    },
  });

  const deactivateUser = useMutation({
    mutationFn: async (userId: string) => {
      await http.delete(`/api/v1/users/${userId}`);
    },
    onSuccess: () => {
      invalidate();
      toast.success(i18next.t("users-admin:toast.deactivated"));
    },
    onError: (err: Error) => {
      toast.error(i18next.t("users-admin:toast.failedDeactivate"), err.message);
    },
  });

  const createUser = useMutation({
    mutationFn: async (input: { email: string; display_name: string; password: string }) => {
      return http.post<User>("/api/v1/users", input);
    },
    onSuccess: () => {
      invalidate();
      toast.success(i18next.t("users-admin:toast.created"));
    },
    onError: (err: Error) => {
      toast.error(i18next.t("users-admin:toast.failedCreate"), err.message);
    },
  });

  return {
    users,
    total,
    loading,
    refresh: invalidate,
    changeStatus: changeStatus.mutateAsync,
    deactivateUser: deactivateUser.mutateAsync,
    createUser: createUser.mutateAsync,
    isStatusChanging: changeStatus.isPending,
    isDeactivating: deactivateUser.isPending,
    isCreating: createUser.isPending,
  };
}
