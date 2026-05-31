import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import i18next from "i18next";
import { useHttp } from "@/hooks/use-ws";
import { queryKeys } from "@/lib/query-keys";
import { toast } from "@/stores/use-toast-store";
import type { User } from "@/types/user-mgmt";

export function useProfile() {
  const http = useHttp();
  const queryClient = useQueryClient();

  const { data: user, isLoading } = useQuery({
    queryKey: queryKeys.userMgmt.me,
    queryFn: () => http.get<User>("/v1/users/me"),
    staleTime: 60_000,
  });

  const updateProfile = useMutation({
    mutationFn: (input: { display_name?: string; avatar_url?: string }) =>
      http.patch<User>("/v1/users/me", input),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: queryKeys.userMgmt.me });
      toast.success(i18next.t("profile:toast.saved"));
    },
    onError: (err: Error) => {
      toast.error(i18next.t("profile:toast.saveFailed"), err.message);
    },
  });

  return {
    user,
    loading: isLoading,
    updateProfile: updateProfile.mutateAsync,
    isUpdating: updateProfile.isPending,
  };
}
