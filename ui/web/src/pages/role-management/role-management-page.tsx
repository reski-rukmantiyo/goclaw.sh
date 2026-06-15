import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Plus, RefreshCw, ShieldCheck, Pencil, Trash2 } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Badge } from "@/components/ui/badge";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
  DialogFooter,
} from "@/components/ui/dialog";
import { PageHeader } from "@/components/shared/page-header";
import { EmptyState } from "@/components/shared/empty-state";
import { TableSkeleton } from "@/components/shared/loading-skeleton";
import { SearchInput } from "@/components/shared/search-input";
import { ConfirmDeleteDialog } from "@/components/shared/confirm-delete-dialog";
import { useDeferredLoading } from "@/hooks/use-deferred-loading";
import { useMinLoading } from "@/hooks/use-min-loading";
import { useRoles } from "./hooks/use-roles";
import { RolePermissionEditor } from "./role-permission-editor";
import type { Role } from "@/types/user-mgmt";

export default function RoleManagementPage() {
  const { t } = useTranslation("role-management");
  const { t: tc } = useTranslation("common");

  const [search, setSearch] = useState("");
  const { roles, total, loading, refresh, createRole, updateRole, deleteRole, setRolePermissions, isCreating, isUpdating, isDeleting, isSettingPermissions } = useRoles({ search });

  const spinning = useMinLoading(loading);
  const showSkeleton = useDeferredLoading(loading && roles.length === 0);

  const [createOpen, setCreateOpen] = useState(false);
  const [editRole, setEditRole] = useState<Role | null>(null);
  const [deleteTarget, setDeleteTarget] = useState<Role | null>(null);
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");

  const openCreate = () => {
    setName("");
    setDescription("");
    setCreateOpen(true);
  };

  const openEdit = (role: Role) => {
    setEditRole(role);
    setName(role.name);
    setDescription(role.description ?? "");
  };

  const handleCreate = async () => {
    if (!name.trim()) return;
    await createRole({ name: name.trim(), description: description.trim() || undefined });
    setCreateOpen(false);
    setName("");
    setDescription("");
  };

  const handleUpdate = async () => {
    if (!editRole || !name.trim()) return;
    await updateRole({ id: editRole.id, name: name.trim(), description: description.trim() || undefined });
    setEditRole(null);
  };

  const handleDelete = async () => {
    if (!deleteTarget) return;
    await deleteRole(deleteTarget.id);
    setDeleteTarget(null);
  };

  return (
    <div className="space-y-6 p-4 md:p-6">
      <PageHeader
        title={t("title")}
        description={t("description")}
        actions={
          <div className="flex gap-2">
            <Button variant="outline" size="sm" onClick={() => refresh()} disabled={spinning}>
              <RefreshCw className={`mr-2 h-4 w-4 ${spinning ? "animate-spin" : ""}`} />
              {tc("refresh")}
            </Button>
            <Button size="sm" onClick={openCreate}>
              <Plus className="mr-2 h-4 w-4" />
              {t("createRole")}
            </Button>
          </div>
        }
      />

      <div className="flex items-center gap-4">
        <SearchInput value={search} onChange={setSearch} placeholder={t("searchPlaceholder")} className="max-w-sm" />
        <span className="text-muted-foreground text-sm">{tc("total", { count: total })}</span>
      </div>

      {showSkeleton ? (
        <TableSkeleton rows={5} />
      ) : roles.length === 0 ? (
        <EmptyState icon={ShieldCheck} title={t("emptyTitle")} description={t("emptyDescription")} />
      ) : (
        <div className="border rounded-lg overflow-hidden">
          <table className="w-full text-sm">
            <thead className="bg-muted">
              <tr>
                <th className="px-4 py-3 text-left font-medium">{t("name")}</th>
                <th className="px-4 py-3 text-left font-medium">{t("description")}</th>
                <th className="px-4 py-3 text-left font-medium">{t("permissions")}</th>
                <th className="px-4 py-3 text-left font-medium">{t("type")}</th>
                <th className="px-4 py-3 text-right font-medium">{tc("actions")}</th>
              </tr>
            </thead>
            <tbody className="divide-y">
              {roles.map((role) => (
                <tr key={role.id} className="hover:bg-muted/50">
                  <td className="px-4 py-3 font-medium">{role.name}</td>
                  <td className="px-4 py-3 text-muted-foreground">{role.description || "—"}</td>
                  <td className="px-4 py-3">
                    <Badge variant="secondary">{role.permissions.length}</Badge>
                  </td>
                  <td className="px-4 py-3">
                    {role.is_system ? (
                      <Badge variant="outline">{t("system")}</Badge>
                    ) : (
                      <Badge variant="default">{t("custom")}</Badge>
                    )}
                  </td>
                  <td className="px-4 py-3 text-right">
                    <div className="flex justify-end gap-2">
                      <Button variant="ghost" size="sm" onClick={() => openEdit(role)}>
                        <Pencil className="h-4 w-4" />
                      </Button>
                      {!role.is_system && (
                        <Button variant="ghost" size="sm" className="text-destructive" onClick={() => setDeleteTarget(role)}>
                          <Trash2 className="h-4 w-4" />
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

      {/* Create Dialog */}
      <Dialog open={createOpen} onOpenChange={setCreateOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{t("createRole")}</DialogTitle>
            <DialogDescription>{t("createRoleDescription")}</DialogDescription>
          </DialogHeader>
          <div className="space-y-4 py-4">
            <div className="space-y-2">
              <Label htmlFor="role-name">{t("name")}</Label>
              <Input id="role-name" value={name} onChange={(e) => setName(e.target.value)} placeholder={t("namePlaceholder")} />
            </div>
            <div className="space-y-2">
              <Label htmlFor="role-desc">{t("description")}</Label>
              <Input id="role-desc" value={description} onChange={(e) => setDescription(e.target.value)} placeholder={t("descriptionPlaceholder")} />
            </div>
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => setCreateOpen(false)}>{tc("cancel")}</Button>
            <Button onClick={handleCreate} disabled={!name.trim() || isCreating}>
              {isCreating ? tc("saving") : tc("create")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Edit Dialog */}
      <Dialog open={!!editRole} onOpenChange={(open) => !open && setEditRole(null)}>
        <DialogContent className="max-w-2xl max-h-[90vh] overflow-y-auto">
          <DialogHeader>
            <DialogTitle>{t("editRole")}</DialogTitle>
            <DialogDescription>{t("editRoleDescription")}</DialogDescription>
          </DialogHeader>
          <div className="space-y-6 py-4">
            <div className="space-y-2">
              <Label htmlFor="edit-role-name">{t("name")}</Label>
              <Input id="edit-role-name" value={name} onChange={(e) => setName(e.target.value)} disabled={editRole?.is_system} />
            </div>
            <div className="space-y-2">
              <Label htmlFor="edit-role-desc">{t("description")}</Label>
              <Input id="edit-role-desc" value={description} onChange={(e) => setDescription(e.target.value)} disabled={editRole?.is_system} />
            </div>
            {editRole && (
              <RolePermissionEditor
                roleId={editRole.id}
                onSave={async (perms) => {
                  await setRolePermissions({ id: editRole.id, permissions: perms });
                }}
                isSaving={isSettingPermissions}
                readOnly={editRole.is_system}
              />
            )}
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => setEditRole(null)}>{tc("cancel")}</Button>
            <Button onClick={handleUpdate} disabled={!name.trim() || isUpdating || !!editRole?.is_system}>
              {isUpdating ? tc("saving") : tc("save")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Delete Dialog */}
      <ConfirmDeleteDialog
        open={!!deleteTarget}
        onOpenChange={(open) => !open && setDeleteTarget(null)}
        title={t("deleteRoleTitle")}
        description={t("deleteRoleDescription", { name: deleteTarget?.name })}
        confirmValue={deleteTarget?.name ?? ""}
        onConfirm={handleDelete}
        loading={isDeleting}
      />
    </div>
  );
}
