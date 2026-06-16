import { useState } from "react";
import { useForm } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { useTranslation } from "react-i18next";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "@/components/ui/dialog";
import {
  passwordChangeSchema,
  type PasswordChangeForm,
} from "@/schemas/user-mgmt.schema";
import { toast } from "@/stores/use-toast-store";

interface PasswordChangeDialogProps {
  children: React.ReactNode;
}

export function PasswordChangeDialog({ children }: PasswordChangeDialogProps) {
  const { t } = useTranslation("login");
  const [open, setOpen] = useState(false);
  const [submitting, setSubmitting] = useState(false);

  const {
    register,
    handleSubmit,
    reset,
    formState: { errors },
  } = useForm<PasswordChangeForm>({
    resolver: zodResolver(passwordChangeSchema),
    defaultValues: {
      current_password: "",
      new_password: "",
      confirm_password: "",
    },
  });

  const onValid = async (data: PasswordChangeForm) => {
    setSubmitting(true);
    try {
      const res = await fetch("/auth/password/change", {
        method: "POST",
        headers: {
          "Content-Type": "application/json",
          Authorization: `Bearer ${localStorage.getItem("goclaw:token") ?? ""}`,
        },
        body: JSON.stringify({
          current_password: data.current_password,
          new_password: data.new_password,
        }),
      });

      if (!res.ok) {
        const err = await res.json().catch(() => ({ error: "Unknown error" }));
        const message =
          typeof err.error === "string"
            ? err.error
            : err.error?.message ?? res.statusText;
        toast.error(t("passwordChange.errorTitle"), message);
        return;
      }

      toast.success(t("passwordChange.successTitle"), t("passwordChange.successMessage"));
      reset();
      setOpen(false);
    } catch {
      toast.error(t("passwordChange.errorTitle"), t("passwordChange.errorNetwork"));
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <Dialog open={open} onOpenChange={(next) => {
      setOpen(next);
      if (!next) reset();
    }}>
      <DialogTrigger asChild>{children}</DialogTrigger>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{t("passwordChange.title")}</DialogTitle>
          <DialogDescription>
            {t("passwordChange.description")}
          </DialogDescription>
        </DialogHeader>

        <form onSubmit={handleSubmit(onValid)} className="space-y-4">
          <div className="space-y-2">
            <Label htmlFor="current_password" className="text-sm font-medium">
              {t("passwordChange.currentPassword")}
            </Label>
            <Input
              id="current_password"
              type="password"
              autoComplete="current-password"
              {...register("current_password")}
              className="text-base md:text-sm"
              disabled={submitting}
            />
            {errors.current_password && (
              <p className="text-xs text-destructive">
                {errors.current_password.message}
              </p>
            )}
          </div>

          <div className="space-y-2">
            <Label htmlFor="new_password" className="text-sm font-medium">
              {t("passwordChange.newPassword")}
            </Label>
            <Input
              id="new_password"
              type="password"
              autoComplete="new-password"
              {...register("new_password")}
              className="text-base md:text-sm"
              disabled={submitting}
            />
            {errors.new_password && (
              <p className="text-xs text-destructive">
                {errors.new_password.message}
              </p>
            )}
          </div>

          <div className="space-y-2">
            <Label htmlFor="confirm_password" className="text-sm font-medium">
              {t("passwordChange.confirmPassword")}
            </Label>
            <Input
              id="confirm_password"
              type="password"
              autoComplete="new-password"
              {...register("confirm_password")}
              className="text-base md:text-sm"
              disabled={submitting}
            />
            {errors.confirm_password && (
              <p className="text-xs text-destructive">
                {errors.confirm_password.message}
              </p>
            )}
          </div>

          <DialogFooter>
            <Button
              type="button"
              variant="outline"
              onClick={() => setOpen(false)}
              disabled={submitting}
            >
              {t("passwordChange.cancel")}
            </Button>
            <Button type="submit" disabled={submitting}>
              {submitting
                ? t("passwordChange.changing")
                : t("passwordChange.change")}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
