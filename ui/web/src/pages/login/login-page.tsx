import { useState } from "react";
import { useNavigate, useLocation } from "react-router";
import { useTranslation } from "react-i18next";
import { useAuthStore } from "@/stores/use-auth-store";
import { ROUTES } from "@/lib/constants";
import { LoginLayout } from "./login-layout";
import { LoginTabs, type LoginMode } from "./login-tabs";
import { TokenForm } from "./token-form";
import { PairingForm } from "./pairing-form";
import { EmailLoginForm } from "./email-login-form";
import { ProviderButtons } from "./provider-buttons";

export function LoginPage() {
  const { t } = useTranslation("login");
  const [mode, setMode] = useState<LoginMode>("token");

  const setCredentials = useAuthStore((s) => s.setCredentials);
  const setPairing = useAuthStore((s) => s.setPairing);
  const navigate = useNavigate();
  const location = useLocation();

  const from =
    (location.state as { from?: { pathname: string } })?.from?.pathname ??
    ROUTES.OVERVIEW;

  function handleTokenLogin(userId: string, token: string) {
    setCredentials(token, userId);
    navigate(from, { replace: true });
  }

  function handlePairingApproved(senderID: string, userId: string) {
    setPairing(senderID, userId);
    setTimeout(() => navigate(from, { replace: true }), 500);
  }

  function handleEmailLogin(accessToken: string, userId: string) {
    setCredentials(accessToken, userId);
    navigate(from, { replace: true });
  }

  return (
    <LoginLayout subtitle={t("subtitle")}>
      <LoginTabs mode={mode} onModeChange={setMode} />
      {mode === "token" ? (
        <TokenForm onSubmit={handleTokenLogin} />
      ) : mode === "pairing" ? (
        <PairingForm onApproved={handlePairingApproved} />
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
