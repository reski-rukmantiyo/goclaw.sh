import { useCallback } from "react";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import i18next from "i18next";
import { useHttp } from "@/hooks/use-ws";
import { queryKeys } from "@/lib/query-keys";
import { toast } from "@/stores/use-toast-store";
import type {
  Group,
  GroupMember,
  GroupTreeNode,
  JoinRequest,
  PaginatedResult,
} from "@/types/user-mgmt";

interface GroupListParams {
  search?: string;
  limit?: number;
  offset?: number;
}

export function useGroupsAdmin(params: GroupListParams = {}) {
  const http = useHttp();
  const queryClient = useQueryClient();

  const queryParams: Record<string, string> = {};
  if (params.search) queryParams.search = params.search;
  queryParams.limit = String(params.limit ?? 50);
  queryParams.offset = String(params.offset ?? 0);

  const queryKey = queryKeys.groups.list(queryParams);

  const { data, isLoading: loading } = useQuery({
    queryKey,
    queryFn: () =>
      http.get<PaginatedResult<Group>>("/v1/groups", queryParams),
    staleTime: 30_000,
  });

  const groups = data?.items ?? [];
  const total = data?.total ?? 0;

  const invalidate = useCallback(
    () =>
      queryClient.invalidateQueries({ queryKey: queryKeys.groups.all }),
    [queryClient],
  );

  const createGroup = useMutation({
    mutationFn: async (input: {
      name: string;
      slug: string;
      description?: string;
      visibility?: string;
      parent_group_id?: string | null;
    }) => {
      return http.post<Group>("/v1/groups", input);
    },
    onSuccess: () => {
      invalidate();
      toast.success(i18next.t("groups-admin:toast.created"));
    },
    onError: (err: Error) => {
      toast.error(i18next.t("groups-admin:toast.failedCreate"), err.message);
    },
  });

  const updateGroup = useMutation({
    mutationFn: async ({
      id,
      ...input
    }: {
      id: string;
      name?: string;
      description?: string;
      visibility?: string;
      parent_group_id?: string | null;
    }) => {
      return http.patch<Group>(`/v1/groups/${id}`, input);
    },
    onSuccess: () => {
      invalidate();
      toast.success(i18next.t("groups-admin:toast.updated"));
    },
    onError: (err: Error) => {
      toast.error(i18next.t("groups-admin:toast.failedUpdate"), err.message);
    },
  });

  const deleteGroup = useMutation({
    mutationFn: async (id: string) => {
      await http.delete(`/v1/groups/${id}`);
    },
    onSuccess: () => {
      invalidate();
      toast.success(i18next.t("groups-admin:toast.deleted"));
    },
    onError: (err: Error) => {
      toast.error(i18next.t("groups-admin:toast.failedDelete"), err.message);
    },
  });

  return {
    groups,
    total,
    loading,
    refresh: invalidate,
    createGroup: createGroup.mutateAsync,
    updateGroup: updateGroup.mutateAsync,
    deleteGroup: deleteGroup.mutateAsync,
    isCreating: createGroup.isPending,
    isUpdating: updateGroup.isPending,
    isDeleting: deleteGroup.isPending,
  };
}

export function useGroupMembers(groupId: string | null) {
  const http = useHttp();
  const queryClient = useQueryClient();

  const queryKey = groupId
    ? queryKeys.groups.members(groupId)
    : ["groups", "members", "none"];

  const { data, isLoading: loading } = useQuery({
    queryKey,
    queryFn: () =>
      http.get<GroupMember[]>(`/v1/groups/${groupId}/members`),
    enabled: !!groupId,
    staleTime: 30_000,
  });

  const members = data ?? [];

  const invalidate = useCallback(() => {
    if (groupId) {
      queryClient.invalidateQueries({
        queryKey: queryKeys.groups.members(groupId),
      });
    }
  }, [queryClient, groupId]);

  const addMember = useMutation({
    mutationFn: async ({
      userId,
      role,
    }: {
      userId: string;
      role: string;
    }) => {
      await http.post(`/v1/groups/${groupId}/members`, {
        user_id: userId,
        role,
      });
    },
    onSuccess: () => {
      invalidate();
      toast.success(i18next.t("groups-admin:toast.memberAdded"));
    },
    onError: (err: Error) => {
      toast.error(
        i18next.t("groups-admin:toast.failedAddMember"),
        err.message,
      );
    },
  });

  const removeMember = useMutation({
    mutationFn: async (userId: string) => {
      await http.delete(`/v1/groups/${groupId}/members/${userId}`);
    },
    onSuccess: () => {
      invalidate();
      toast.success(i18next.t("groups-admin:toast.memberRemoved"));
    },
    onError: (err: Error) => {
      toast.error(
        i18next.t("groups-admin:toast.failedRemoveMember"),
        err.message,
      );
    },
  });

  const changeMemberRole = useMutation({
    mutationFn: async ({
      userId,
      role,
    }: {
      userId: string;
      role: string;
    }) => {
      await http.patch(`/v1/groups/${groupId}/members/${userId}`, {
        role,
      });
    },
    onSuccess: () => {
      invalidate();
      toast.success(i18next.t("groups-admin:toast.roleChanged"));
    },
    onError: (err: Error) => {
      toast.error(
        i18next.t("groups-admin:toast.failedRoleChange"),
        err.message,
      );
    },
  });

  return {
    members,
    loading,
    refresh: invalidate,
    addMember: addMember.mutateAsync,
    removeMember: removeMember.mutateAsync,
    changeMemberRole: changeMemberRole.mutateAsync,
    isAddingMember: addMember.isPending,
    isRemovingMember: removeMember.isPending,
  };
}

export function useJoinRequests(groupId: string | null) {
  const http = useHttp();
  const queryClient = useQueryClient();

  const queryKey = groupId
    ? queryKeys.groups.joinRequests(groupId)
    : ["groups", "join-requests", "none"];

  const { data, isLoading: loading } = useQuery({
    queryKey,
    queryFn: () =>
      http.get<JoinRequest[]>(`/v1/groups/${groupId}/join-requests`),
    enabled: !!groupId,
    staleTime: 30_000,
  });

  const requests = data ?? [];

  const invalidate = useCallback(() => {
    if (groupId) {
      queryClient.invalidateQueries({
        queryKey: queryKeys.groups.joinRequests(groupId),
      });
    }
  }, [queryClient, groupId]);

  const reviewRequest = useMutation({
    mutationFn: async ({
      requestId,
      action,
    }: {
      requestId: string;
      action: "approved" | "rejected";
    }) => {
      await http.patch(
        `/v1/groups/${groupId}/join-requests/${requestId}`,
        { status: action },
      );
    },
    onSuccess: () => {
      invalidate();
      toast.success(i18next.t("groups-admin:toast.requestReviewed"));
    },
    onError: (err: Error) => {
      toast.error(
        i18next.t("groups-admin:toast.failedReviewRequest"),
        err.message,
      );
    },
  });

  return {
    requests,
    loading,
    refresh: invalidate,
    reviewRequest: reviewRequest.mutateAsync,
    isReviewing: reviewRequest.isPending,
  };
}

export function useGroupsTree() {
  const http = useHttp();
  const { data, isLoading } = useQuery({
    queryKey: queryKeys.groups.tree,
    queryFn: () => http.get<GroupTreeNode[]>("/v1/groups/tree"),
    staleTime: 30_000,
  });
  return { tree: data ?? [], loading: isLoading };
}
