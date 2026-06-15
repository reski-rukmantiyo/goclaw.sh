import { useState, useEffect } from "react";
import { useTranslation } from "react-i18next";
import { ShieldCheck } from "lucide-react";
import { Button } from "@/components/ui/button";
import { useRolePermissions } from "./hooks/use-roles";

interface RolePermissionEditorProps {
  roleId: string;
  onSave: (permissions: string[]) => Promise<void>;
  isSaving: boolean;
  /** When true the permission set is read-only (system roles). Checkboxes are disabled and Save is hidden. */
  readOnly?: boolean;
}

const PERMISSION_CATEGORIES: Record<string, string[]> = {
  user: [
    "user.list",
    "user.get",
    "user.create",
    "user.update",
    "user.delete",
    "user.enroll",
    "user.unenroll",
    "user.assign_role",
    "user.pre_provision",
    "user.suspend",
    "user.reactivate",
  ],
  group: [
    "group.list",
    "group.get",
    "group.create",
    "group.update",
    "group.delete",
    "group.manage_members",
    "group.assign_role",
  ],
  role: [
    "role.list",
    "role.get",
    "role.create",
    "role.update",
    "role.delete",
  ],
  system: [
    "system.manage_settings",
    "system.manage_auth",
    "system.view_health",
  ],
  audit: [
    "audit.list",
    "audit.get",
  ],
};

const CATEGORY_LABELS: Record<string, string> = {
  user: "User Management",
  group: "Group Management",
  role: "Role Management",
  system: "System",
  audit: "Audit Log",
};

export function RolePermissionEditor({ roleId, onSave, isSaving, readOnly = false }: RolePermissionEditorProps) {
  const { t } = useTranslation("role-management");
  const { permissions: currentPerms, loading } = useRolePermissions(roleId);
  const [selected, setSelected] = useState<Set<string>>(new Set());

  useEffect(() => {
    setSelected(new Set(currentPerms));
  }, [currentPerms]);

  const toggle = (perm: string) => {
    setSelected((prev) => {
      const next = new Set(prev);
      if (next.has(perm)) {
        next.delete(perm);
      } else {
        next.add(perm);
      }
      return next;
    });
  };

  const toggleAll = (perms: string[], checked: boolean) => {
    setSelected((prev) => {
      const next = new Set(prev);
      perms.forEach((p) => {
        if (checked) next.add(p);
        else next.delete(p);
      });
      return next;
    });
  };

  const handleSave = async () => {
    await onSave(Array.from(selected));
  };

  if (loading) {
    return (
      <div className="flex items-center justify-center py-8">
        <ShieldCheck className="h-6 w-6 animate-pulse text-muted-foreground" />
      </div>
    );
  }

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <h3 className="text-sm font-medium">{t("permissions")}</h3>
        {readOnly ? (
          <span className="text-xs text-muted-foreground">{t("systemRoleReadonly")}</span>
        ) : (
          <Button size="sm" variant="outline" onClick={handleSave} disabled={isSaving}>
            {isSaving ? t("savingPermissions") : t("savePermissions")}
          </Button>
        )}
      </div>

      <div className="space-y-6">
        {Object.entries(PERMISSION_CATEGORIES).map(([category, perms]) => {
          const allChecked = perms.every((p) => selected.has(p));
          const someChecked = perms.some((p) => selected.has(p)) && !allChecked;

          return (
            <div key={category} className="space-y-3">
              <div className="flex items-center gap-2">
                <input
                  type="checkbox"
                  id={`cat-${category}`}
                  checked={allChecked}
                  ref={(el) => {
                    if (el) el.indeterminate = someChecked;
                  }}
                  onChange={(e) => toggleAll(perms, e.target.checked)}
                  disabled={readOnly}
                  className="h-4 w-4 rounded border-gray-300"
                />
                <label htmlFor={`cat-${category}`} className="text-sm font-semibold">
                  {CATEGORY_LABELS[category] || category}
                </label>
              </div>
              <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-2 pl-6">
                {perms.map((perm) => (
                  <div key={perm} className="flex items-center gap-2">
                    <input
                      type="checkbox"
                      id={`perm-${perm}`}
                      checked={selected.has(perm)}
                      onChange={() => toggle(perm)}
                      disabled={readOnly}
                      className="h-4 w-4 rounded border-gray-300"
                    />
                    <label htmlFor={`perm-${perm}`} className="text-sm text-muted-foreground">
                      {perm}
                    </label>
                  </div>
                ))}
              </div>
            </div>
          );
        })}
      </div>
    </div>
  );
}
