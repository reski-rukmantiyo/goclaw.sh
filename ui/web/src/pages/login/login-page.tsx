import { useState, useEffect } from "react";
import { useNavigate, useLocation } from "react-router";
import { useTranslation } from "react-i18next";
import { useAuthStore } from "@/stores/use-auth-store";
import { LOCAL_STORAGE_KEYS, ROUTES } from "@/lib/constants";
import { route } from "@/lib/routes";
import { LoginLayout } from "./login-layout";
import { LoginTabs, type LoginMode } from "./login-tabs";
import { TokenForm } from "./token-form";
import { EmailLoginForm } from "./email-login-form";
import { ProviderButtons } from "./provider-buttons";

export function LoginPage() {
  const { t } = useTranslation("login");
  const [mode, setMode] = useState<LoginMode>("email");

  const token = useAuthStore((s) => s.token);
  const userId = useAuthStore((s) => s.userId);
  const senderID = useAuthStore((s) => s.senderID);
  const setCredentials = useAuthStore((s) => s.setCredentials);
  const setUserProfile = useAuthStore((s) => s.setUserProfile);
  const setIsGatewayToken = useAuthStore((s) => s.setIsGatewayToken);
  const navigate = useNavigate();
  const location = useLocation();

  const from =
    (location.state as { from?: { pathname: string } })?.from?.pathname ??
    ROUTES.OVERVIEW;

  useEffect(() => {
    if ((token || senderID) && userId) {
      const slug = localStorage.getItem(LOCAL_STORAGE_KEYS.TENANT_ID) || "master";
      navigate(route(slug, ROUTES.OVERVIEW), { replace: true });
    }
  }, [token, userId, senderID, navigate]);

  function handleTokenLogin(userId: string, token: string) {
    setCredentials(token, userId);
    setIsGatewayToken(true);
    navigate(from, { replace: true });
  }

  function handleEmailLogin(accessToken: string, userId: string, tenantSlug: string, displayName: string, email: string) {
    if (tenantSlug) {
      localStorage.setItem(LOCAL_STORAGE_KEYS.TENANT_ID, tenantSlug);
    }
    setCredentials(accessToken, userId);
    setIsGatewayToken(false);
    setUserProfile(displayName, email);
    navigate(from, { replace: true });
  }

  return (
    <LoginLayout subtitle={t("subtitle")}>
      <LoginTabs mode={mode} onModeChange={setMode} />
      {mode === "token" ? (
        <TokenForm onSubmit={handleTokenLogin} />
      ) : (
        <div className="space-y-4">
          <EmailLoginForm onSuccess={handleEmailLogin} />

          <div className="relative">
            <div className="absolute inset-0 flex items-center">
              <span className="w-full border-t" />
            </div>
            <div className="relative flex justify-center text-xs uppercase">
              <span className="bg-card px-2 text-muted-foreground">
                {t("email.orContinueWith")}
              </span>
            </div>
          </div>

          <ProviderButtons className="space-y-2" />
        </div>
      )}
    </LoginLayout>
  );
}
