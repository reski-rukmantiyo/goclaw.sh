import { useState, useEffect } from "react";
import { useTranslation } from "react-i18next";
import { Plus, Loader2, Pencil } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import type { CreateWorkstationParams, UpdateWorkstationParams } from "./hooks/use-workstations";
import type { Workstation } from "./hooks/use-workstations";

interface WorkstationCreateDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onCreate: (params: CreateWorkstationParams) => Promise<void>;
  onUpdate?: (id: string, params: UpdateWorkstationParams) => Promise<void>;
  getWorkstation?: (id: string) => Promise<Workstation & { metadataSummary?: Record<string, unknown> }>;
  editWorkstation?: Workstation | null;
}

type BackendType = "ssh" | "docker";

export function WorkstationCreateDialog({
  open,
  onOpenChange,
  onCreate,
  onUpdate,
  getWorkstation,
  editWorkstation,
}: WorkstationCreateDialogProps) {
  const { t } = useTranslation("workstations");
  const isEdit = Boolean(editWorkstation);

  const [name, setName] = useState("");
  const [key, setKey] = useState("");
  const [backend, setBackend] = useState<BackendType>("ssh");
  // SSH fields
  const [host, setHost] = useState("");
  const [port, setPort] = useState("22");
  const [user, setUser] = useState("");
  const [identityFile, setIdentityFile] = useState("");
  const [password, setPassword] = useState("");
  const [hasExistingKey, setHasExistingKey] = useState(false);
  // Docker fields
  const [container, setContainer] = useState("");
  const [dockerHost, setDockerHost] = useState("");

  const [submitting, setSubmitting] = useState(false);
  const [fieldError, setFieldError] = useState<string | null>(null);
  const [loadingDetail, setLoadingDetail] = useState(false);

  function resetForm() {
    setName("");
    setKey("");
    setBackend("ssh");
    setHost("");
    setPort("22");
    setUser("");
    setIdentityFile("");
    setPassword("");
    setHasExistingKey(false);
    setContainer("");
    setDockerHost("");
    setFieldError(null);
  }

  // Populate form when editing
  useEffect(() => {
    if (!isEdit || !editWorkstation) {
      resetForm();
      return;
    }

    setName(editWorkstation.name);
    setKey(editWorkstation.workstationKey);
    setBackend(editWorkstation.backendType);

    const meta = editWorkstation.metadataSummary;
    if (meta) {
      if (typeof meta.host === "string") setHost(meta.host);
      if (typeof meta.port === "number") setPort(String(meta.port));
      if (typeof meta.user === "string") setUser(meta.user);
      if (typeof meta.hasKey === "boolean") setHasExistingKey(meta.hasKey);
    }

    // For SSH auth fields, we never get them from API (security).
    // Leave identityFile and password empty; user re-enters to change.
    setIdentityFile("");
    setPassword("");

    // Docker fields — attempt to load from detail if available
    if (editWorkstation.backendType === "docker" && getWorkstation) {
      setLoadingDetail(true);
      getWorkstation(editWorkstation.id)
        .then((ws) => {
          const dMeta = ws.metadataSummary;
          if (dMeta) {
            if (typeof dMeta.containerName === "string") setContainer(dMeta.containerName);
            if (typeof dMeta.image === "string") setContainer(dMeta.image);
          }
        })
        .catch(() => {
          // ignore — fields stay empty
        })
        .finally(() => setLoadingDetail(false));
    }

    setFieldError(null);
  }, [editWorkstation, isEdit, getWorkstation, open]);

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    if (!name.trim()) return;
    if (!isEdit && !key.trim()) return;

    // Build backend metadata
    let metadata: Record<string, unknown>;
    if (backend === "ssh") {
      if (!host.trim() || !user.trim()) {
        setFieldError("Host and SSH user are required for SSH backend.");
        return;
      }
      if (!isEdit && !identityFile.trim() && !password.trim()) {
        setFieldError("SSH authentication required: provide identity file or password.");
        return;
      }
      metadata = {
        host: host.trim(),
        port: parseInt(port, 10) || 22,
        user: user.trim(),
        ...(identityFile.trim() ? { identityFile: identityFile.trim() } : {}),
        ...(password.trim() ? { password: password.trim() } : {}),
      };
    } else {
      if (!container.trim()) {
        setFieldError("Container name is required for Docker backend.");
        return;
      }
      metadata = {
        container: container.trim(),
        ...(dockerHost.trim() ? { docker_host: dockerHost.trim() } : {}),
      };
    }

    setFieldError(null);
    setSubmitting(true);
    try {
      if (isEdit && editWorkstation && onUpdate) {
        await onUpdate(editWorkstation.id, { name: name.trim(), metadata });
      } else {
        await onCreate({ workstationKey: key.trim(), name: name.trim(), backendType: backend, metadata });
      }
      resetForm();
      onOpenChange(false);
    } catch (err) {
      setFieldError(err instanceof Error ? err.message : "Failed to save workstation.");
    } finally {
      setSubmitting(false);
    }
  }

  const dialogKey = isEdit ? `edit-${editWorkstation?.id}` : "create";

  return (
    <Dialog key={dialogKey} open={open} onOpenChange={(v) => { if (!submitting) { resetForm(); onOpenChange(v); } }}>
      <DialogContent className="sm:max-w-lg">
        <form onSubmit={handleSubmit}>
          <DialogHeader>
            <DialogTitle>{isEdit ? t("editDialog.title") : t("createDialog.title")}</DialogTitle>
            <DialogDescription>{isEdit ? t("editDialog.description") : t("createDialog.description")}</DialogDescription>
          </DialogHeader>

          <div className="mt-4 space-y-4">
            <div className="space-y-1.5">
              <Label htmlFor="ws-name">{t("createDialog.nameLabel")}</Label>
              <Input
                id="ws-name"
                value={name}
                onChange={(e) => setName(e.target.value)}
                placeholder={t("createDialog.namePlaceholder")}
                required
                className="text-base md:text-sm"
              />
            </div>

            <div className="space-y-1.5">
              <Label htmlFor="ws-key">{t("createDialog.keyLabel")}</Label>
              <Input
                id="ws-key"
                value={key}
                onChange={(e) => setKey(e.target.value.toLowerCase().replace(/[^a-z0-9-]/g, ""))}
                placeholder={t("createDialog.keyPlaceholder")}
                required={!isEdit}
                disabled={isEdit}
                className="text-base md:text-sm"
              />
              {!isEdit && (
                <p className="text-xs text-muted-foreground">{t("createDialog.keyHint")}</p>
              )}
            </div>

            <div className="space-y-1.5">
              <Label>{t("createDialog.backendLabel")}</Label>
              <Select value={backend} onValueChange={(v) => setBackend(v as BackendType)} disabled={isEdit}>
                <SelectTrigger className="text-base md:text-sm">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="ssh">{t("createDialog.sshOption")}</SelectItem>
                  <SelectItem value="docker">{t("createDialog.dockerOption")}</SelectItem>
                </SelectContent>
              </Select>
            </div>

            {backend === "ssh" && (
              <>
                <div className="grid grid-cols-3 gap-3">
                  <div className="col-span-2 space-y-1.5">
                    <Label htmlFor="ws-host">{t("createDialog.hostLabel")}</Label>
                    <Input
                      id="ws-host"
                      value={host}
                      onChange={(e) => setHost(e.target.value)}
                      placeholder={t("createDialog.hostPlaceholder")}
                      className="text-base md:text-sm"
                    />
                  </div>
                  <div className="space-y-1.5">
                    <Label htmlFor="ws-port">{t("createDialog.portLabel")}</Label>
                    <Input
                      id="ws-port"
                      type="number"
                      min={1}
                      max={65535}
                      value={port}
                      onChange={(e) => setPort(e.target.value)}
                      className="text-base md:text-sm"
                    />
                  </div>
                </div>
                <div className="space-y-1.5">
                  <Label htmlFor="ws-user">{t("createDialog.userLabel")}</Label>
                  <Input
                    id="ws-user"
                    value={user}
                    onChange={(e) => setUser(e.target.value)}
                    placeholder={t("createDialog.userPlaceholder")}
                    className="text-base md:text-sm"
                  />
                </div>
                <div className="space-y-1.5">
                  <Label htmlFor="ws-identity">{t("createDialog.identityFileLabel")}</Label>
                  <Input
                    id="ws-identity"
                    value={identityFile}
                    onChange={(e) => setIdentityFile(e.target.value)}
                    placeholder={t("createDialog.identityFilePlaceholder")}
                    className="text-base md:text-sm"
                  />
                  {isEdit && hasExistingKey && !identityFile && (
                    <p className="text-xs text-amber-600">Key configured — leave empty to keep existing.</p>
                  )}
                  {isEdit && (
                    <p className="text-xs text-muted-foreground">{t("editDialog.authHint")}</p>
                  )}
                </div>
                <div className="space-y-1.5">
                  <Label htmlFor="ws-password">{t("createDialog.passwordLabel")}</Label>
                  <Input
                    id="ws-password"
                    type="password"
                    value={password}
                    onChange={(e) => setPassword(e.target.value)}
                    placeholder={t("createDialog.passwordPlaceholder")}
                    className="text-base md:text-sm"
                  />
                </div>
              </>
            )}

            {backend === "docker" && (
              <>
                <div className="space-y-1.5">
                  <Label htmlFor="ws-container">{t("createDialog.containerLabel")}</Label>
                  <Input
                    id="ws-container"
                    value={container}
                    onChange={(e) => setContainer(e.target.value)}
                    placeholder={t("createDialog.containerPlaceholder")}
                    className="text-base md:text-sm"
                  />
                </div>
                <div className="space-y-1.5">
                  <Label htmlFor="ws-docker-host">{t("createDialog.dockerHostLabel")}</Label>
                  <Input
                    id="ws-docker-host"
                    value={dockerHost}
                    onChange={(e) => setDockerHost(e.target.value)}
                    placeholder={t("createDialog.dockerHostPlaceholder")}
                    className="text-base md:text-sm"
                  />
                </div>
              </>
            )}

            {fieldError && (
              <p className="text-sm text-destructive">{fieldError}</p>
            )}
          </div>

          <DialogFooter className="mt-6">
            <Button type="button" variant="outline" onClick={() => { resetForm(); onOpenChange(false); }} disabled={submitting || loadingDetail}>
              {t("createDialog.cancel")}
            </Button>
            <Button type="submit" disabled={submitting || loadingDetail || !name.trim() || (!isEdit && !key.trim())} className="gap-1">
              {submitting || loadingDetail ? (
                <Loader2 className="h-3.5 w-3.5 animate-spin" />
              ) : isEdit ? (
                <Pencil className="h-3.5 w-3.5" />
              ) : (
                <Plus className="h-3.5 w-3.5" />
              )}
              {isEdit ? t("editDialog.save") : t("createDialog.create")}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
