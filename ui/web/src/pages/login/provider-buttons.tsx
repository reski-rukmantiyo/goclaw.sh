import { useState, useEffect } from "react";
import { useTranslation } from "react-i18next";
import { Button } from "@/components/ui/button";

interface Provider {
  id: string;
  name: string;
  authorize_url: string;
  enabled: boolean;
}

interface ProviderButtonsProps {
  className?: string;
}

export function ProviderButtons({ className }: ProviderButtonsProps) {
  const { t } = useTranslation("login");
  const [providers, setProviders] = useState<Provider[]>([]);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    let cancelled = false;

    async function fetchProviders() {
      try {
        const res = await fetch("/auth/providers");
        if (!res.ok) return;
        const body = (await res.json()) as { providers: Provider[] };
        if (!cancelled) {
          setProviders(body.providers?.filter((p) => p.enabled) ?? []);
        }
      } catch {
        // Silently ignore — OIDC is optional
      } finally {
        if (!cancelled) setLoading(false);
      }
    }

    fetchProviders();
    return () => {
      cancelled = true;
    };
  }, []);

  if (loading || providers.length === 0) return null;

  return (
    <div className={className}>
      {providers.map((provider) => (
        <Button
          key={provider.id}
          variant="outline"
          className="h-9 w-full"
          onClick={() => {
            window.location.href = provider.authorize_url;
          }}
        >
          {t("email.signInWith", { provider: provider.name })}
        </Button>
      ))}
    </div>
  );
}
