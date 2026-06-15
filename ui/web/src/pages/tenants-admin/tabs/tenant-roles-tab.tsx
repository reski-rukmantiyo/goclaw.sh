import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Plus, Pencil, Trash2, ShieldCheck } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Badge } from "@/components/ui/badge";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogFooter,
} from "@/components/ui/dialog";
import { ConfirmDialog } from "@/components/shared/confirm-dialog";
import { EmptyState } from "@/components/shared/empty-state";
import { TableSkeleton } from "@/components/shared/loading-skeleton";
import { SearchInput } from "@/components/shared/search-input";
import { useDeferredLoading } from "@/hooks/use-deferred-loading";
import { useRoles } from "@/pages/role-management/hooks/use-roles";
import { RolePermissionEditor } from "@/pages/role-management/role-permission-editor";

interface TenantRolesTabProps {
  tenantId: string;
}

export function TenantRolesTab({ tenantId }: TenantRolesTabProps) {
  const { t } = useTranslation("roleManagement");

  const [search, setSearch] = useState("");
  const {
    roles, loading,
    createRole, updateRole, deleteRole, setRolePermissions,
    isCreating, isUpdating, isDeleting,
  } = useRoles({ tenantId, search: search || undefined });

  const showSkeleton = useDeferredLoading(loading && roles.length === 0);

  // --- Create/Edit dialog ---
  const [dialogOpen, setDialogOpen] = useState(false);
  const [editTarget, setEditTarget] = useState<{ id: string; name: string; description: string } | null>(null);
  const [formName, setFormName] = useState("");
  const [formDesc, setFormDesc] = useState("");

  const openCreate = () => {
    setEditTarget(null);
    setFormName("");
    setFormDesc("");
    setDialogOpen(true);
  };

  const openEdit = (role: { id: string; name: string; description?: string | null }) => {
    setEditTarget({ id: role.id, name: role.name, description: role.description ?? "" });
    setFormName(role.name);
    setFormDesc(role.description ?? "");
    setDialogOpen(true);
  };

  const handleSave = async () => {
    if (!formName.trim()) return;
    if (editTarget) {
      await updateRole({ id: editTarget.id, name: formName.trim(), description: formDesc.trim() || undefined });
    } else {
      await createRole({ name: formName.trim(), description: formDesc.trim() || undefined });
    }
    setDialogOpen(false);
  };

  // --- Delete ---
  const [deleteTarget, setDeleteTarget] = useState<string | null>(null);
  const handleDelete = async () => {
    if (!deleteTarget) return;
    await deleteRole(deleteTarget);
    setDeleteTarget(null);
  };

  // --- Permissions dialog ---
  const [permsTarget, setPermsTarget] = useState<string | null>(null);
  const [isSavingPerms, setIsSavingPerms] = useState(false);

  const handleSavePerms = async (newPerms: string[]) => {
    if (!permsTarget) return;
    setIsSavingPerms(true);
    try {
      await setRolePermissions({ id: permsTarget, permissions: newPerms });
    } finally {
      setIsSavingPerms(false);
    }
  };

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <SearchInput value={search} onChange={setSearch} placeholder={t("searchPlaceholder")} className="max-w-xs" />
        <Button size="sm" onClick={openCreate} className="gap-1">
          <Plus className="h-3.5 w-3.5" /> {t("addRole")}
        </Button>
      </div>

      {showSkeleton ? (
        <TableSkeleton rows={4} />
      ) : roles.length === 0 ? (
        <EmptyState icon={ShieldCheck} title={t("emptyTitle")} description={t("emptyDescription")} />
      ) : (
        <div className="overflow-x-auto">
          <table className="w-full text-sm min-w-[500px]">
            <thead>
              <tr className="border-b text-left text-xs text-muted-foreground">
                <th className="pb-2 font-medium">{t("columns.name")}</th>
                <th className="pb-2 font-medium">{t("columns.description")}</th>
                <th className="pb-2 font-medium">{t("columns.permissions")}</th>
                <th className="pb-2 font-medium">{t("columns.type")}</th>
                <th className="pb-2 font-medium text-right">{t("columns.actions")}</th>
              </tr>
            </thead>
            <tbody>
              {roles.map((role) => (
                <tr key={role.id} className="border-b last:border-0 hover:bg-muted/30 transition-colors">
                  <td className="py-2.5 font-medium">{role.name}</td>
                  <td className="py-2.5 text-muted-foreground max-w-[200px] truncate">{role.description || "—"}</td>
                  <td className="py-2.5">
                    <Badge
                      variant="secondary"
                      className="cursor-pointer"
                      onClick={() => setPermsTarget(role.id)}
                    >
                      {role.permissions?.length ?? 0}
                    </Badge>
                  </td>
                  <td className="py-2.5">
                    <Badge variant={role.is_system ? "default" : "secondary"}>
                      {role.is_system ? t("systemRole") : t("customRole")}
                    </Badge>
                  </td>
                  <td className="py-2.5 text-right">
                    <div className="flex items-center justify-end gap-1">
                      <Button variant="ghost" size="sm" className="h-7 w-7 p-0" onClick={() => openEdit(role)} title={t("edit")}>
                        <Pencil className="h-3.5 w-3.5" />
                      </Button>
                      {!role.is_system && (
                        <Button variant="ghost" size="sm" className="h-7 w-7 p-0 text-muted-foreground hover:text-destructive" onClick={() => setDeleteTarget(role.id)} title={t("delete")}>
                          <Trash2 className="h-3.5 w-3.5" />
                        </Button>
                      )}
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      {/* Create/Edit Dialog */}
      <Dialog open={dialogOpen} onOpenChange={setDialogOpen}>
        <DialogContent className="max-sm:inset-0 max-sm:translate-x-0 max-sm:translate-y-0 sm:max-w-md">
          <DialogHeader>
            <DialogTitle>{editTarget ? t("editRole") : t("addRole")}</DialogTitle>
          </DialogHeader>
          <div className="space-y-4 py-2">
            <div className="space-y-1.5">
              <Label>{t("columns.name")}</Label>
              <Input value={formName} onChange={(e) => setFormName(e.target.value)} className="text-base md:text-sm" />
            </div>
            <div className="space-y-1.5">
              <Label>{t("columns.description")}</Label>
              <Input value={formDesc} onChange={(e) => setFormDesc(e.target.value)} className="text-base md:text-sm" />
            </div>
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => setDialogOpen(false)}>{t("cancel")}</Button>
            <Button onClick={handleSave} disabled={isCreating || isUpdating || !formName.trim()}>
              {isCreating || isUpdating ? "..." : t("save")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Delete Confirm */}
      <ConfirmDialog
        open={!!deleteTarget}
        onOpenChange={(o) => { if (!o) setDeleteTarget(null); }}
        title={t("deleteRole")}
        description={t("deleteConfirm")}
        confirmLabel={t("delete")}
        variant="destructive"
        onConfirm={handleDelete}
        loading={isDeleting}
      />

      {/* Permissions Dialog */}
      <Dialog open={!!permsTarget} onOpenChange={(o) => { if (!o) setPermsTarget(null); }}>
        <DialogContent className="max-sm:inset-0 max-sm:translate-x-0 max-sm:translate-y-0 sm:max-w-2xl max-h-[80vh] overflow-y-auto">
          <DialogHeader>
            <DialogTitle>{t("editPermissions")}</DialogTitle>
          </DialogHeader>
          {permsTarget && (
            <RolePermissionEditor
              roleId={permsTarget}
              onSave={handleSavePerms}
              isSaving={isSavingPerms}
            />
          )}
        </DialogContent>
      </Dialog>
    </div>
  );
}
