import { useState, useEffect } from "react";
import { useTranslation } from "react-i18next";
import { User as UserIcon, Save, Mail, Shield } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Badge } from "@/components/ui/badge";
import { Card, CardContent, CardHeader, CardTitle, CardDescription } from "@/components/ui/card";
import { PageHeader } from "@/components/shared/page-header";
import { DetailSkeleton } from "@/components/shared/loading-skeleton";
import { useDeferredLoading } from "@/hooks/use-deferred-loading";
import { useProfile } from "./hooks/use-profile";
import { PasswordChangeDialog } from "@/pages/login/password-change-dialog";
import { useAuthStore } from "@/stores/use-auth-store";

function providerBadge(provider: string) {
  switch (provider) {
    case "entra_id": return "default" as const;
    case "google": return "secondary" as const;
    default: return "outline" as const;
  }
}

function providerLabel(provider: string) {
  switch (provider) {
    case "entra_id": return "Entra ID";
    case "google": return "Google";
    case "local": return "Local";
    default: return provider;
  }
}

export function ProfilePage() {
  const { t } = useTranslation("profile");
  const { user, loading, updateProfile, isUpdating } = useProfile();
  const tenantName = useAuthStore((s) => s.tenantName);
  const isTenantAdmin = false; // TODO: derive from tenant_users.role

  const [displayName, setDisplayName] = useState("");
  const [dirty, setDirty] = useState(false);

  const showSkeleton = useDeferredLoading(loading);

  useEffect(() => {
    if (user) {
      setDisplayName(user.display_name);
      setDirty(false);
    }
  }, [user]);

  const handleSave = async () => {
    await updateProfile({ display_name: displayName.trim() });
    setDirty(false);
  };

  if (showSkeleton) {
    return (
      <div className="p-4 sm:p-6 pb-10">
        <PageHeader title={t("title")} />
        <div className="mt-6"><DetailSkeleton /></div>
      </div>
    );
  }

  if (!user) return null;

  return (
    <div className="p-4 sm:p-6 pb-10">
      <PageHeader title={t("title")} description={t("description")} />

      <div className="mt-6 space-y-4 max-w-2xl">
        {/* Profile info */}
        <Card>
          <CardHeader className="pb-3">
            <CardTitle className="text-base">{t("account.title")}</CardTitle>
            <CardDescription>{t("account.description")}</CardDescription>
          </CardHeader>
          <CardContent className="space-y-4">
            <div className="flex items-center gap-4">
              <div className="flex h-16 w-16 items-center justify-center rounded-full bg-muted">
                {user.avatar_url ? (
                  <img src={user.avatar_url} alt="" className="h-16 w-16 rounded-full object-cover" />
                ) : (
                  <UserIcon className="h-8 w-8 text-muted-foreground" />
                )}
              </div>
              <div className="space-y-1">
                <div className="flex items-center gap-2">
                  <Badge variant={providerBadge(user.auth_provider)}>
                    {providerLabel(user.auth_provider)}
                  </Badge>
                  {isTenantAdmin && (
                    <Badge variant="default" className="gap-1">
                      <Shield className="h-3 w-3" /> Admin
                    </Badge>
                  )}
                </div>
                <div className="text-sm text-muted-foreground">
                  {tenantName && `${tenantName} · `}{t("account.memberSince", { date: new Date(user.created_at).toLocaleDateString() })}
                </div>
              </div>
            </div>

            <div className="grid gap-4">
              <div className="grid gap-1.5">
                <Label htmlFor="display-name">{t("account.displayName")}</Label>
                <Input
                  id="display-name"
                  value={displayName}
                  onChange={(e) => { setDisplayName(e.target.value); setDirty(true); }}
                  className="max-w-sm text-base md:text-sm"
                />
              </div>

              <div className="grid gap-1.5">
                <Label>{t("account.email")}</Label>
                <div className="flex items-center gap-2 text-sm">
                  <Mail className="h-3.5 w-3.5 text-muted-foreground" />
                  <span>{user.email}</span>
                </div>
              </div>
            </div>

            {dirty && (
              <div className="flex justify-end pt-2">
                <Button size="sm" onClick={handleSave} disabled={isUpdating} className="gap-1.5">
                  <Save className="h-3.5 w-3.5" />
                  {isUpdating ? t("account.saving") : t("account.save")}
                </Button>
              </div>
            )}
          </CardContent>
        </Card>

        {/* Password change (local auth only) */}
        {user.auth_provider === "local" && (
          <Card>
            <CardHeader className="pb-3">
              <CardTitle className="text-base">{t("password.title")}</CardTitle>
              <CardDescription>{t("password.description")}</CardDescription>
            </CardHeader>
            <CardContent>
              <PasswordChangeDialog>
                <Button variant="outline" size="sm">
                  {t("password.change")}
                </Button>
              </PasswordChangeDialog>
            </CardContent>
          </Card>
        )}
      </div>
    </div>
  );
}
