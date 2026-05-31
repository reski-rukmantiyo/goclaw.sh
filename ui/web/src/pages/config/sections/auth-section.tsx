import { useState, useEffect } from "react";
import { Save, KeyRound } from "lucide-react";
import { useTranslation } from "react-i18next";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Switch } from "@/components/ui/switch";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { InfoLabel } from "@/components/shared/info-label";

interface LocalAuthConfig {
  enabled: boolean;
}

interface EntraIDAuthConfig {
  enabled: boolean;
  client_id?: string;
  tenant_id?: string;
  redirect_uri?: string;
}

interface GoogleAuthConfig {
  enabled: boolean;
  client_id?: string;
  redirect_uri?: string;
}

interface AuthSessionConfig {
  timeout_minutes?: number;
  refresh_enabled?: boolean;
}

export interface AuthConfigData {
  providers?: {
    local?: LocalAuthConfig;
    entra_id?: EntraIDAuthConfig;
    google?: GoogleAuthConfig;
  };
  session?: AuthSessionConfig;
}

const DEFAULT: AuthConfigData = {};

interface Props {
  data: AuthConfigData | undefined;
  onSave: (value: AuthConfigData) => Promise<void>;
  saving: boolean;
}

export function AuthSection({ data, onSave, saving }: Props) {
  const { t } = useTranslation("config");
  const [draft, setDraft] = useState<AuthConfigData>(data ?? DEFAULT);
  const [dirty, setDirty] = useState(false);

  useEffect(() => {
    setDraft(data ?? DEFAULT);
    setDirty(false);
  }, [data]);

  const updateProviders = (provider: "local" | "entra_id" | "google", patch: Record<string, unknown>) => {
    setDraft((prev) => ({
      ...prev,
      providers: {
        ...prev.providers,
        [provider]: { ...(prev.providers?.[provider] as Record<string, unknown> | undefined ?? {}), ...patch },
      },
    }));
    setDirty(true);
  };

  const updateSession = (patch: Partial<AuthSessionConfig>) => {
    setDraft((prev) => ({
      ...prev,
      session: { ...prev.session, ...patch },
    }));
    setDirty(true);
  };

  const handleSave = () => {
    onSave(draft);
  };

  const local = draft.providers?.local;
  const entra = draft.providers?.entra_id;
  const google = draft.providers?.google;
  const session = draft.session;

  return (
    <div className="space-y-4">
      {/* Local Auth */}
      <Card>
        <CardHeader className="pb-3">
          <CardTitle className="text-base">{t("auth.local.title")}</CardTitle>
          <CardDescription>{t("auth.local.description")}</CardDescription>
        </CardHeader>
        <CardContent className="space-y-4">
          <div className="flex items-center justify-between">
            <InfoLabel tip={t("auth.local.enabledTip")}>{t("auth.local.enabled")}</InfoLabel>
            <Switch
              checked={local?.enabled ?? false}
              onCheckedChange={(v) => updateProviders("local", { enabled: v })}
            />
          </div>
        </CardContent>
      </Card>

      {/* Entra ID */}
      <Card>
        <CardHeader className="pb-3">
          <CardTitle className="text-base">{t("auth.entra.title")}</CardTitle>
          <CardDescription>{t("auth.entra.description")}</CardDescription>
        </CardHeader>
        <CardContent className="space-y-4">
          <div className="flex items-center justify-between">
            <InfoLabel tip={t("auth.entra.enabledTip")}>{t("auth.entra.enabled")}</InfoLabel>
            <Switch
              checked={entra?.enabled ?? false}
              onCheckedChange={(v) => updateProviders("entra_id", { enabled: v })}
            />
          </div>

          <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
            <div className="grid gap-1.5">
              <InfoLabel tip={t("auth.entra.clientIdTip")}>{t("auth.entra.clientId")}</InfoLabel>
              <Input
                value={entra?.client_id ?? ""}
                onChange={(e) => updateProviders("entra_id", { client_id: e.target.value })}
                placeholder="xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx"
              />
            </div>
            <div className="grid gap-1.5">
              <InfoLabel tip={t("auth.entra.tenantIdTip")}>{t("auth.entra.tenantId")}</InfoLabel>
              <Input
                value={entra?.tenant_id ?? ""}
                onChange={(e) => updateProviders("entra_id", { tenant_id: e.target.value })}
                placeholder="xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx"
              />
            </div>
          </div>

          <div className="grid gap-1.5">
            <InfoLabel tip={t("auth.entra.redirectUriTip")}>{t("auth.entra.redirectUri")}</InfoLabel>
            <Input
              value={entra?.redirect_uri ?? ""}
              onChange={(e) => updateProviders("entra_id", { redirect_uri: e.target.value })}
              placeholder="https://your-domain.com/auth/entra/callback"
            />
          </div>

          <div className="flex items-center gap-2 text-xs text-muted-foreground">
            <KeyRound className="h-3.5 w-3.5" />
            <span>{t("auth.entra.secretManaged")}</span>
          </div>
        </CardContent>
      </Card>

      {/* Google OAuth2 */}
      <Card>
        <CardHeader className="pb-3">
          <CardTitle className="text-base">{t("auth.google.title")}</CardTitle>
          <CardDescription>{t("auth.google.description")}</CardDescription>
        </CardHeader>
        <CardContent className="space-y-4">
          <div className="flex items-center justify-between">
            <InfoLabel tip={t("auth.google.enabledTip")}>{t("auth.google.enabled")}</InfoLabel>
            <Switch
              checked={google?.enabled ?? false}
              onCheckedChange={(v) => updateProviders("google", { enabled: v })}
            />
          </div>

          <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
            <div className="grid gap-1.5">
              <InfoLabel tip={t("auth.google.clientIdTip")}>{t("auth.google.clientId")}</InfoLabel>
              <Input
                value={google?.client_id ?? ""}
                onChange={(e) => updateProviders("google", { client_id: e.target.value })}
                placeholder="xxxx.apps.googleusercontent.com"
              />
            </div>
            <div className="grid gap-1.5">
              <InfoLabel tip={t("auth.google.redirectUriTip")}>{t("auth.google.redirectUri")}</InfoLabel>
              <Input
                value={google?.redirect_uri ?? ""}
                onChange={(e) => updateProviders("google", { redirect_uri: e.target.value })}
                placeholder="https://your-domain.com/auth/google/callback"
              />
            </div>
          </div>

          <div className="flex items-center gap-2 text-xs text-muted-foreground">
            <KeyRound className="h-3.5 w-3.5" />
            <span>{t("auth.google.secretManaged")}</span>
          </div>
        </CardContent>
      </Card>

      {/* Session */}
      <Card>
        <CardHeader className="pb-3">
          <CardTitle className="text-base">{t("auth.session.title")}</CardTitle>
          <CardDescription>{t("auth.session.description")}</CardDescription>
        </CardHeader>
        <CardContent className="space-y-4">
          <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
            <div className="grid gap-1.5">
              <InfoLabel tip={t("auth.session.timeoutTip")}>{t("auth.session.timeoutMinutes")}</InfoLabel>
              <Input
                type="number"
                min={1}
                value={session?.timeout_minutes ?? 480}
                onChange={(e) => updateSession({ timeout_minutes: parseInt(e.target.value, 10) || 480 })}
              />
            </div>
            <div className="flex items-center justify-between">
              <InfoLabel tip={t("auth.session.refreshTip")}>{t("auth.session.refreshEnabled")}</InfoLabel>
              <Switch
                checked={session?.refresh_enabled ?? true}
                onCheckedChange={(v) => updateSession({ refresh_enabled: v })}
              />
            </div>
          </div>
        </CardContent>
      </Card>

      {dirty && (
        <div className="flex justify-end pt-2">
          <Button size="sm" onClick={handleSave} disabled={saving} className="gap-1.5">
            <Save className="h-3.5 w-3.5" /> {saving ? t("saving") : t("save")}
          </Button>
        </div>
      )}
    </div>
  );
}
