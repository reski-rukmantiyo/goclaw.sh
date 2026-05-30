import { useState } from "react";
import { useForm } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { useTranslation } from "react-i18next";
import { AlertCircle } from "lucide-react";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Button } from "@/components/ui/button";
import {
  loginSchema,
  type LoginForm,
} from "@/schemas/user-mgmt.schema";

interface EmailLoginFormProps {
  onSuccess: (accessToken: string, userId: string) => void;
}

interface LoginResponse {
  access_token: string;
  user: {
    id: string;
    email: string;
  };
}

export function EmailLoginForm({ onSuccess }: EmailLoginFormProps) {
  const { t } = useTranslation("login");
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const {
    register,
    handleSubmit,
    formState: { errors },
  } = useForm<LoginForm>({
    resolver: zodResolver(loginSchema),
    defaultValues: { email: "", password: "" },
  });

  const onValid = async (data: LoginForm) => {
    setSubmitting(true);
    setError(null);

    try {
      const res = await fetch("/auth/login", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(data),
      });

      if (res.status === 401) {
        setError(t("email.errorInvalidCredentials"));
        return;
      }

      if (!res.ok) {
        setError(t("email.errorServer", { status: res.status }));
        return;
      }

      const body = (await res.json()) as LoginResponse;
      onSuccess(body.access_token, body.user.id);
    } catch {
      setError(t("email.errorCannotConnect"));
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <form onSubmit={handleSubmit(onValid)} className="space-y-4">
      <div className="space-y-2">
        <Label htmlFor="email" className="text-sm font-medium">
          {t("email.email")}
        </Label>
        <Input
          id="email"
          type="email"
          autoComplete="email"
          {...register("email")}
          placeholder={t("email.emailPlaceholder")}
          className="text-base md:text-sm"
          autoFocus
          disabled={submitting}
        />
        {errors.email && (
          <p className="text-xs text-destructive">{errors.email.message}</p>
        )}
      </div>

      <div className="space-y-2">
        <div className="flex items-center justify-between">
          <Label htmlFor="password" className="text-sm font-medium">
            {t("email.password")}
          </Label>
          <a
            href="#"
            onClick={(e) => e.preventDefault()}
            className="text-xs text-muted-foreground hover:text-foreground transition-colors"
          >
            {t("email.forgotPassword")}
          </a>
        </div>
        <Input
          id="password"
          type="password"
          autoComplete="current-password"
          {...register("password")}
          placeholder={t("email.passwordPlaceholder")}
          className="text-base md:text-sm"
          disabled={submitting}
        />
        {errors.password && (
          <p className="text-xs text-destructive">{errors.password.message}</p>
        )}
      </div>

      {error && (
        <div className="flex items-start gap-2 rounded-md border border-destructive/50 bg-destructive/10 px-3 py-2 text-sm text-destructive">
          <AlertCircle className="mt-0.5 h-4 w-4 shrink-0" />
          <span>{error}</span>
        </div>
      )}

      <Button
        type="submit"
        disabled={submitting}
        className="h-9 w-full"
      >
        {submitting ? t("email.signingIn") : t("email.signIn")}
      </Button>
    </form>
  );
}
