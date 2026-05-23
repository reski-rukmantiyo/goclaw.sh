import { useState, useEffect, useCallback } from "react";
import { useTranslation } from "react-i18next";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Switch } from "@/components/ui/switch";
import { Shield, Layers, X } from "lucide-react";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { useCommandGroups } from "./hooks/use-command-groups";

interface WorkstationPermissionsTabProps {
  workstationId: string;
}

export function WorkstationPermissionsTab({ workstationId }: WorkstationPermissionsTabProps) {
  const { t } = useTranslation("workstations");
  const { groups, applyGroup, removeGroup, toggleGroupLink, listGroupLinks } = useCommandGroups();

  const [groupLinks, setGroupLinks] = useState<{ id: string; groupId: string; enabled: boolean }[]>([]);
  const [loading, setLoading] = useState(false);
  const [selectedGroupId, setSelectedGroupId] = useState<string>("");
  const [applying, setApplying] = useState(false);

  const groupById = useCallback(
    (id: string) => groups.find((g) => g.id === id),
    [groups],
  );

  const load = useCallback(async () => {
    setLoading(true);
    try {
      const links = await listGroupLinks(workstationId);
      setGroupLinks(links);
    } catch {
      setGroupLinks([]);
    } finally {
      setLoading(false);
    }
  }, [workstationId, listGroupLinks]);

  useEffect(() => {
    load();
  }, [load]);

  const handleApplyGroup = async () => {
    if (!selectedGroupId) return;
    setApplying(true);
    try {
      await applyGroup(workstationId, selectedGroupId);
      setSelectedGroupId("");
      await load();
    } finally {
      setApplying(false);
    }
  };

  const handleRemoveGroup = async (groupId: string) => {
    await removeGroup(workstationId, groupId);
    await load();
  };

  const handleToggleGroup = async (groupId: string, enabled: boolean) => {
    await toggleGroupLink(workstationId, groupId, enabled);
    await load();
  };

  return (
    <div className="space-y-4">
      {/* Apply from Group */}
      {groups.length > 0 && (
        <div className="flex items-end gap-2 rounded-md border bg-muted/20 px-3 py-3">
          <div className="flex-1">
            <label className="text-sm font-medium mb-1.5 block">
              {t("permissions.applyFromGroup", "Apply from Group")}
            </label>
            <Select value={selectedGroupId} onValueChange={setSelectedGroupId}>
              <SelectTrigger className="text-base md:text-sm">
                <SelectValue placeholder={t("commandGroups.title")} />
              </SelectTrigger>
              <SelectContent>
                {groups.map((g) => (
                  <SelectItem key={g.id} value={g.id}>
                    {g.name} ({g.patterns?.length || 0} commands)
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
          <Button
            size="sm"
            onClick={handleApplyGroup}
            disabled={!selectedGroupId || applying}
            className="gap-1"
          >
            <Layers className="h-3.5 w-3.5" />
            {t("commandGroups.apply.label", "Apply")}
          </Button>
        </div>
      )}

      {/* Applied Groups */}
      {groupLinks.length > 0 && (
        <div className="space-y-2">
          <p className="text-sm font-medium">{t("commandGroups.title")}</p>
          {groupLinks.map((link) => {
            const group = groupById(link.groupId);
            if (!group) return null;
            return (
              <div
                key={link.id}
                className="rounded-md border px-3 py-2"
              >
                <div className="flex items-center justify-between">
                  <div className="flex items-center gap-2">
                    <span className="text-sm font-medium">{group.name}</span>
                    <Badge variant={link.enabled ? "default" : "secondary"} className="text-xs">
                      {link.enabled
                        ? t("permissions.enabled", "Enabled")
                        : t("permissions.disabled", "Disabled")}
                    </Badge>
                  </div>
                  <div className="flex items-center gap-2">
                    <Switch
                      checked={link.enabled}
                      onCheckedChange={(v) => handleToggleGroup(link.groupId, v)}
                      disabled={loading}
                    />
                    <Button
                      variant="ghost"
                      size="sm"
                      onClick={() => handleRemoveGroup(link.groupId)}
                      disabled={loading}
                      className="h-7 w-7 p-0 text-destructive"
                    >
                      <X className="h-3.5 w-3.5" />
                    </Button>
                  </div>
                </div>
                <div className="flex flex-wrap gap-1 mt-1.5">
                  {group.patterns?.map((p) => (
                    <code key={p} className="text-xs font-mono bg-muted px-1.5 py-0.5 rounded">
                      {p}
                    </code>
                  ))}
                </div>
              </div>
            );
          })}
        </div>
      )}

      {groupLinks.length === 0 && (
        <div className="flex items-center gap-2 rounded-md border border-dashed px-4 py-6 text-sm text-muted-foreground">
          <Shield className="h-4 w-4" />
          {t("permissions.empty", "No permissions configured. All exec commands will be denied.")}
        </div>
      )}
    </div>
  );
}
