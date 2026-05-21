import { useState, useEffect, useCallback } from "react";
import { useTranslation } from "react-i18next";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Badge } from "@/components/ui/badge";
import { Switch } from "@/components/ui/switch";
import { Shield, Trash2, Plus } from "lucide-react";
import { useWorkstations } from "./hooks/use-workstations";

interface WorkstationPermissionsTabProps {
  workstationId: string;
}

export function WorkstationPermissionsTab({ workstationId }: WorkstationPermissionsTabProps) {
  const { t } = useTranslation("workstations");
  const { listPermissions, addPermission, removePermission, togglePermission } = useWorkstations();

  const [permissions, setPermissions] = useState<{ id: string; pattern: string; enabled: boolean }[]>([]);
  const [loading, setLoading] = useState(false);
  const [newPattern, setNewPattern] = useState("");

  const load = useCallback(async () => {
    setLoading(true);
    try {
      const res = await listPermissions(workstationId);
      setPermissions(res);
    } catch {
      setPermissions([]);
    } finally {
      setLoading(false);
    }
  }, [workstationId, listPermissions]);

  useEffect(() => {
    load();
  }, [load]);

  const handleAdd = async () => {
    if (!newPattern.trim()) return;
    await addPermission(workstationId, newPattern.trim());
    setNewPattern("");
    await load();
  };

  const handleRemove = async (id: string) => {
    await removePermission(workstationId, id);
    await load();
  };

  const handleToggle = async (id: string, enabled: boolean) => {
    await togglePermission(id, enabled);
    await load();
  };

  return (
    <div className="space-y-4">
      <div className="flex items-end gap-2">
        <div className="flex-1">
          <label className="text-sm font-medium mb-1.5 block">
            {t("permissions.addLabel", "Allow Command")}
          </label>
          <Input
            value={newPattern}
            onChange={(e) => setNewPattern(e.target.value)}
            placeholder={t("permissions.patternPlaceholder", "e.g. ls, cat, docker*")}
            className="text-base md:text-sm"
            onKeyDown={(e) => { if (e.key === "Enter") handleAdd(); }}
          />
        </div>
        <Button size="sm" onClick={handleAdd} disabled={!newPattern.trim() || loading} className="gap-1">
          <Plus className="h-3.5 w-3.5" />
          {t("permissions.add", "Add")}
        </Button>
      </div>

      <p className="text-xs text-muted-foreground">
        {t("permissions.hint", "Exact match (ls) or prefix glob (docker*). Default-deny: no match → exec rejected.")}
      </p>

      {permissions.length === 0 ? (
        <div className="flex items-center gap-2 rounded-md border border-dashed px-4 py-6 text-sm text-muted-foreground">
          <Shield className="h-4 w-4" />
          {t("permissions.empty", "No permissions configured. All exec commands will be denied.")}
        </div>
      ) : (
        <div className="space-y-2">
          {permissions.map((perm) => (
            <div
              key={perm.id}
              className="flex items-center justify-between rounded-md border px-3 py-2"
            >
              <div className="flex items-center gap-3">
                <code className="text-sm font-mono bg-muted px-1.5 py-0.5 rounded">{perm.pattern}</code>
                <Badge variant={perm.enabled ? "default" : "secondary"} className="text-xs">
                  {perm.enabled ? t("permissions.enabled", "Enabled") : t("permissions.disabled", "Disabled")}
                </Badge>
              </div>
              <div className="flex items-center gap-2">
                <Switch
                  checked={perm.enabled}
                  onCheckedChange={(v) => handleToggle(perm.id, v)}
                  disabled={loading}
                />
                <Button
                  variant="ghost"
                  size="sm"
                  onClick={() => handleRemove(perm.id)}
                  disabled={loading}
                  className="h-7 w-7 p-0 text-destructive"
                >
                  <Trash2 className="h-3.5 w-3.5" />
                </Button>
              </div>
            </div>
          ))}
        </div>
      )}
    </div>
  );
}
