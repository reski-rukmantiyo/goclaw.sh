import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { useWs } from "@/hooks/use-ws";
import { queryKeys } from "@/lib/query-keys";
import { Methods } from "@/api/protocol";
import type { AuthConfigData } from "@/pages/config/sections/auth-section";

export function useTenantAuth() {
  const ws = useWs();
  const queryClient = useQueryClient();

  const { data, isLoading } = useQuery({
    queryKey: queryKeys.tenant.auth(),
    queryFn: async () => {
      const res = await ws.call<AuthConfigData>(Methods.TENANT_AUTH_GET, {});
      return res ?? {};
    },
    staleTime: 30_000,
  });

  const { mutateAsync: save, isPending: saving } = useMutation({
    mutationFn: async (auth: AuthConfigData) => {
      await ws.call(Methods.TENANT_AUTH_PATCH, auth as Record<string, unknown>);
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: queryKeys.tenant.auth() });
    },
  });

  return {
    authConfig: data,
    loading: isLoading,
    save,
    saving,
  };
}
