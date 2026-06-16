import { useState } from "react";
import { useForm } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { useTranslation } from "react-i18next";
import { AlertCircle } from "lucide-react";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Button } from "@/components/ui/button";
import { registerSchema, type RegisterFormData } from "@/schemas/login.schema";

interface RegisterFormProps {
  onSuccess: (accessToken: string, userId: string, tenantSlug: string) => void;
}

interface RegisterResponse {
  access_token: string;
  tenant_slug: string;
  user: {
    id: string;
    email: string;
  };
}

export function RegisterForm({ onSuccess }: RegisterFormProps) {
  const { t } = useTranslation("login");
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const {
    register,
    handleSubmit,
    formState: { errors },
  } = useForm<RegisterFormData>({
    resolver: zodResolver(registerSchema),
    defaultValues: { email: "", password: "", displayName: "" },
  });

  const onValid = async (data: RegisterFormData) => {
    setSubmitting(true);
    setError(null);

    try {
      const res = await fetch("/auth/register", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(data),
      });

      if (res.status === 409) {
        setError(t("register.errorEmailExists"));
        return;
      }

      if (!res.ok) {
        setError(t("register.errorServer", { status: res.status }));
        return;
      }

      const body = (await res.json()) as RegisterResponse;
      onSuccess(body.access_token, body.user.id, body.tenant_slug);
    } catch {
      setError(t("register.errorCannotConnect"));
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <form onSubmit={handleSubmit(onValid)} className="space-y-4">
      <div className="space-y-2">
        <Label htmlFor="reg-email" className="text-sm font-medium">
          {t("email.email")}
        </Label>
        <Input
          id="reg-email"
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
        <Label htmlFor="reg-display-name" className="text-sm font-medium">
          {t("register.displayName")}
        </Label>
        <Input
          id="reg-display-name"
          type="text"
          {...register("displayName")}
          placeholder={t("register.displayNamePlaceholder")}
          className="text-base md:text-sm"
          disabled={submitting}
        />
        {errors.displayName && (
          <p className="text-xs text-destructive">{errors.displayName.message}</p>
        )}
      </div>

      <div className="space-y-2">
        <Label htmlFor="reg-password" className="text-sm font-medium">
          {t("email.password")}
        </Label>
        <Input
          id="reg-password"
          type="password"
          autoComplete="new-password"
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

      <Button type="submit" disabled={submitting} className="h-9 w-full">
        {submitting ? t("register.submitting") : t("register.submit")}
      </Button>
    </form>
  );
}
