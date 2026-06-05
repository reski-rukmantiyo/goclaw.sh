import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Plus, Pencil, Trash2, FolderTree } from "lucide-react";
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
import { useGroupsAdmin } from "@/pages/groups-admin/hooks/use-groups-admin";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";

export function TenantGroupsTab() {
  const { t } = useTranslation("groupsAdmin");

  const [search, setSearch] = useState("");
  const {
    groups, loading,
    createGroup, updateGroup, deleteGroup,
    isCreating, isUpdating, isDeleting,
  } = useGroupsAdmin({ search: search || undefined });

  const showSkeleton = useDeferredLoading(loading && groups.length === 0);

  // --- Create/Edit dialog ---
  const [dialogOpen, setDialogOpen] = useState(false);
  const [editTarget, setEditTarget] = useState<{ id: string; name: string; slug: string; description: string; visibility: string } | null>(null);
  const [formName, setFormName] = useState("");
  const [formSlug, setFormSlug] = useState("");
  const [formDesc, setFormDesc] = useState("");
  const [formVisibility, setFormVisibility] = useState("open");

  const openCreate = () => {
    setEditTarget(null);
    setFormName("");
    setFormSlug("");
    setFormDesc("");
    setFormVisibility("open");
    setDialogOpen(true);
  };

  const openEdit = (group: { id: string; name: string; slug: string; description?: string | null; visibility?: string | null }) => {
    setEditTarget({ id: group.id, name: group.name, slug: group.slug, description: group.description ?? "", visibility: group.visibility ?? "open" });
    setFormName(group.name);
    setFormSlug(group.slug);
    setFormDesc(group.description ?? "");
    setFormVisibility(group.visibility ?? "open");
    setDialogOpen(true);
  };

  // Auto-derive slug from name on create
  const handleNameChange = (value: string) => {
    setFormName(value);
    if (!editTarget) {
      setFormSlug(value.toLowerCase().replace(/[^a-z0-9]+/g, "-").replace(/^-|-$/g, ""));
    }
  };

  const handleSave = async () => {
    if (!formName.trim()) return;
    if (editTarget) {
      await updateGroup({
        id: editTarget.id,
        name: formName.trim(),
        description: formDesc.trim() || undefined,
        visibility: formVisibility,
      });
    } else {
      await createGroup({
        name: formName.trim(),
        slug: formSlug.trim(),
        description: formDesc.trim() || undefined,
        visibility: formVisibility,
      });
    }
    setDialogOpen(false);
  };

  // --- Delete ---
  const [deleteTarget, setDeleteTarget] = useState<string | null>(null);
  const handleDelete = async () => {
    if (!deleteTarget) return;
    await deleteGroup(deleteTarget);
    setDeleteTarget(null);
  };

  const visibilityBadge = (vis?: string | null) => {
    const variant = vis === "open" ? "default" : "secondary";
    return <Badge variant={variant}>{vis === "open" ? t("open") : t("closed")}</Badge>;
  };

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <SearchInput value={search} onChange={setSearch} placeholder={t("searchPlaceholder")} className="max-w-xs" />
        <Button size="sm" onClick={openCreate} className="gap-1">
          <Plus className="h-3.5 w-3.5" /> {t("addGroup")}
        </Button>
      </div>

      {showSkeleton ? (
        <TableSkeleton rows={4} />
      ) : groups.length === 0 ? (
        <EmptyState icon={FolderTree} title={t("emptyTitle")} description={t("emptyDescription")} />
      ) : (
        <div className="overflow-x-auto">
          <table className="w-full text-sm min-w-[500px]">
            <thead>
              <tr className="border-b text-left text-xs text-muted-foreground">
                <th className="pb-2 font-medium">{t("columns.name")}</th>
                <th className="pb-2 font-medium">{t("columns.slug")}</th>
                <th className="pb-2 font-medium">{t("columns.visibility")}</th>
                <th className="pb-2 font-medium text-right">{t("columns.actions")}</th>
              </tr>
            </thead>
            <tbody>
              {groups.map((group) => (
                <tr key={group.id} className="border-b last:border-0 hover:bg-muted/30 transition-colors">
                  <td className="py-2.5 font-medium">{group.name}</td>
                  <td className="py-2.5">
                    <code className="rounded bg-muted px-1.5 py-0.5 text-xs">{group.slug}</code>
                  </td>
                  <td className="py-2.5">{visibilityBadge(group.visibility)}</td>
                  <td className="py-2.5 text-right">
                    <div className="flex items-center justify-end gap-1">
                      <Button variant="ghost" size="sm" className="h-7 w-7 p-0" onClick={() => openEdit(group)} title={t("edit")}>
                        <Pencil className="h-3.5 w-3.5" />
                      </Button>
                      <Button variant="ghost" size="sm" className="h-7 w-7 p-0 text-muted-foreground hover:text-destructive" onClick={() => setDeleteTarget(group.id)} title={t("delete")}>
                        <Trash2 className="h-3.5 w-3.5" />
                      </Button>
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
            <DialogTitle>{editTarget ? t("editGroup") : t("addGroup")}</DialogTitle>
          </DialogHeader>
          <div className="space-y-4 py-2">
            <div className="space-y-1.5">
              <Label>{t("columns.name")}</Label>
              <Input value={formName} onChange={(e) => handleNameChange(e.target.value)} className="text-base md:text-sm" />
            </div>
            <div className="space-y-1.5">
              <Label>{t("columns.slug")}</Label>
              <Input value={formSlug} onChange={(e) => setFormSlug(e.target.value)} disabled={!!editTarget} className="text-base md:text-sm font-mono" />
            </div>
            <div className="space-y-1.5">
              <Label>{t("columns.description")}</Label>
              <Input value={formDesc} onChange={(e) => setFormDesc(e.target.value)} className="text-base md:text-sm" />
            </div>
            <div className="space-y-1.5">
              <Label>{t("columns.visibility")}</Label>
              <Select value={formVisibility} onValueChange={setFormVisibility}>
                <SelectTrigger className="text-base md:text-sm"><SelectValue /></SelectTrigger>
                <SelectContent>
                  <SelectItem value="open">{t("open")}</SelectItem>
                  <SelectItem value="closed">{t("closed")}</SelectItem>
                </SelectContent>
              </Select>
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
        title={t("deleteGroup")}
        description={t("deleteConfirm")}
        confirmLabel={t("delete")}
        variant="destructive"
        onConfirm={handleDelete}
        loading={isDeleting}
      />
    </div>
  );
}
