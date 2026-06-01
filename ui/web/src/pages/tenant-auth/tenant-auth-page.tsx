import { useTranslation } from "react-i18next";
import { PageHeader } from "@/components/shared/page-header";
import { AuthSection, type AuthConfigData } from "@/pages/config/sections/auth-section";
import { useTenantAuth } from "./hooks/use-tenant-auth";
import { toast } from "@/stores/use-toast-store";

export function TenantAuthPage() {
  const { t } = useTranslation("tenants");
  const { authConfig, loading, save, saving } = useTenantAuth();

  const handleSave = async (value: AuthConfigData) => {
    try {
      await save(value);
      toast.success(t("authSaved"));
    } catch (err) {
      toast.error(t("authSaveFailed"), err instanceof Error ? err.message : "");
      throw err;
    }
  };

  return (
    <div className="p-4 sm:p-6 space-y-6">
      <PageHeader
        title={t("authentication")}
        description={t("authenticationDescription")}
      />
      {loading ? (
        <div className="animate-pulse space-y-4">
          <div className="h-32 rounded-lg bg-muted" />
          <div className="h-32 rounded-lg bg-muted" />
        </div>
      ) : (
        <AuthSection data={authConfig} onSave={handleSave} saving={saving} />
      )}
    </div>
  );
}
