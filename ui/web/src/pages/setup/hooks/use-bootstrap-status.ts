import { useMemo } from "react";
import { useProviders } from "@/pages/providers/hooks/use-providers";
import { useChatGPTOAuthProviderStatuses } from "@/pages/providers/hooks/use-chatgpt-oauth-provider-statuses";
import { useAgents } from "@/pages/agents/hooks/use-agents";
import { useAuthStore } from "@/stores/use-auth-store";
import { isSetupSkipped } from "@/lib/setup-skip";

export type SetupStep = 1 | 2 | 3 | 4 | "complete";

export function useBootstrapStatus() {
  const connected = useAuthStore((s) => s.connected);
  const userId = useAuthStore((s) => s.userId);
  const tenantId = useAuthStore((s) => s.tenantId);
  const tenantSlug = useAuthStore((s) => s.tenantSlug);
  const { providers, loading: providersLoading } = useProviders();
  const { statuses: oauthStatuses, isPending: oauthStatusesPending } = useChatGPTOAuthProviderStatuses(providers);
  const { agents, loading: agentsLoading } = useAgents();

  // React Query v5: disabled queries stay in isPending. Only count loading when
  // there are actually ChatGPT OAuth providers to check.
  const hasChatGPTOAuth = providers.some((p) => p.provider_type === "chatgpt_oauth");
  const oauthStatusesLoading = hasChatGPTOAuth && oauthStatusesPending;

  // Wait for WS to connect before considering agents loaded
  const loading = providersLoading || agentsLoading || oauthStatusesLoading || !connected;

  const { needsSetup, currentStep } = useMemo(() => {
    // While loading we don't know the real step; return 1 as a safe neutral value.
    // Returning "complete" here creates a race where SetupPage can redirect before
    // fresh tenant data arrives.
    if (loading) return { needsSetup: false, currentStep: 1 as SetupStep };

    const readyOAuthProviders = new Set(
      oauthStatuses
        .filter((status) => status.availability === "ready")
        .map((status) => status.provider.name),
    );
    const hasProvider = providers.some((provider) => provider.enabled && (
      provider.api_key === "***"
      || provider.api_key?.length > 0
      || provider.provider_type === "claude_cli"
      || provider.provider_type === "ollama"
      || (provider.provider_type === "chatgpt_oauth" && readyOAuthProviders.has(provider.name))
    ));
    const hasAgent = agents.length > 0;

    // Allow skipping setup entirely via localStorage
    const skipped = isSetupSkipped({ userId, tenantId, tenantSlug });
    if (skipped) return { needsSetup: false, currentStep: "complete" as SetupStep };

    if (!hasProvider) return { needsSetup: true, currentStep: 1 as SetupStep };
    if (!hasAgent) return { needsSetup: true, currentStep: 2 as SetupStep };
    return { needsSetup: false, currentStep: "complete" as SetupStep };
  }, [agents, loading, oauthStatuses, providers, tenantId, tenantSlug, userId]);

  return { needsSetup, currentStep, loading, providers, agents };
}
