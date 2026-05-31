import { useState, useEffect } from "react";
import { useTranslation } from "react-i18next";
import {
  Plus,
  RefreshCw,
  FolderTree,
  Pencil,
  Trash2,
  Users,
  UserPlus,
  ShieldCheck,
  CheckCircle,
  XCircle,
  LayoutList,
} from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Badge } from "@/components/ui/badge";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
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
import {
  useGroupsAdmin,
  useGroupMembers,
  useJoinRequests,
  useGroupsTree,
} from "./hooks/use-groups-admin";
import { GroupTreeView } from "./group-tree-view";
import { UserPickerCombobox } from "@/components/shared/user-picker-combobox";
import type { Group, GroupMember, JoinRequest } from "@/types/user-mgmt";

function visibilityBadge(visibility: string) {
  if (visibility === "open") return "default" as const;
  return "secondary" as const;
}

function GroupsAdminPage() {
  const { t } = useTranslation("groups-admin");
  const { t: tc } = useTranslation("common");

  const [search, setSearch] = useState("");
  const [viewMode, setViewMode] = useState<"table" | "tree">("table");
  const {
    groups,
    loading,
    refresh,
    createGroup,
    updateGroup,
    deleteGroup,
    isCreating,
    isUpdating,
  } = useGroupsAdmin({ search });

  const { tree } = useGroupsTree();

  const spinning = useMinLoading(loading);
  const showSkeleton = useDeferredLoading(loading && groups.length === 0);

  // Create/Edit dialog state
  const [formOpen, setFormOpen] = useState(false);
  const [editTarget, setEditTarget] = useState<Group | null>(null);
  const [formName, setFormName] = useState("");
  const [formSlug, setFormSlug] = useState("");
  const [formDescription, setFormDescription] = useState("");
  const [formVisibility, setFormVisibility] = useState("open");
  const [formParentGroupId, setFormParentGroupId] = useState<string>("none");

  // Delete confirmation state
  const [deleteTarget, setDeleteTarget] = useState<Group | null>(null);
  const [deleteLoading, setDeleteLoading] = useState(false);

  // Member management dialog state
  const [memberGroup, setMemberGroup] = useState<string | null>(null);
  const [addMemberUserId, setAddMemberUserId] = useState("");
  const [addMemberRole, setAddMemberRole] = useState("member");

  // Join requests dialog state
  const [requestGroup, setRequestGroup] = useState<string | null>(null);

  // Members and requests hooks (enabled when dialog opens)
  const {
    members,
    loading: membersLoading,
    addMember,
    removeMember,
    changeMemberRole,
    isAddingMember,
  } = useGroupMembers(memberGroup);

  const {
    requests,
    loading: requestsLoading,
    reviewRequest,
    isReviewing,
  } = useJoinRequests(requestGroup);

  // Group lookup for parent names
  const groupMap = new Map(groups.map((g) => [g.id, g]));

  // Open create dialog
  const openCreate = () => {
    setEditTarget(null);
    setFormName("");
    setFormSlug("");
    setFormDescription("");
    setFormVisibility("open");
    setFormParentGroupId("none");
    setFormOpen(true);
  };

  // Open edit dialog
  const openEdit = (group: Group) => {
    setEditTarget(group);
    setFormName(group.name);
    setFormSlug(group.slug);
    setFormDescription(group.description ?? "");
    setFormVisibility(group.visibility);
    setFormParentGroupId(group.parent_group_id ?? "none");
    setFormOpen(true);
  };

  // Handle form submit (create or update)
  const handleFormSubmit = async () => {
    if (!formName.trim() || !formSlug.trim()) return;
    try {
      if (editTarget) {
        await updateGroup({
          id: editTarget.id,
          name: formName.trim(),
          description: formDescription.trim() || undefined,
          visibility: formVisibility,
          parent_group_id: formParentGroupId !== "none" ? formParentGroupId : null,
        });
      } else {
        await createGroup({
          name: formName.trim(),
          slug: formSlug.trim(),
          description: formDescription.trim() || undefined,
          visibility: formVisibility,
          parent_group_id: formParentGroupId !== "none" ? formParentGroupId : null,
        });
      }
      setFormOpen(false);
    } catch {
      // error handled by mutation
    }
  };

  // Auto-derive slug from name on create
  const handleNameChange = (v: string) => {
    setFormName(v);
    if (!editTarget) {
      setSlugFromName(v);
    }
  };

  const setSlugFromName = (v: string) => {
    setFormSlug(
      v
        .toLowerCase()
        .replace(/\s+/g, "-")
        .replace(/[^a-z0-9-]/g, ""),
    );
  };

  // Handle delete
  const handleDelete = async () => {
    if (!deleteTarget) return;
    setDeleteLoading(true);
    try {
      await deleteGroup(deleteTarget.id);
      setDeleteTarget(null);
    } catch {
      // error handled by mutation
    } finally {
      setDeleteLoading(false);
    }
  };

  // Handle add member
  const handleAddMember = async () => {
    if (!addMemberUserId.trim() || !memberGroup) return;
    try {
      await addMember({ userId: addMemberUserId.trim(), role: addMemberRole });
      setAddMemberUserId("");
      setAddMemberRole("member");
    } catch {
      // error handled by mutation
    }
  };

  // Handle review join request
  const handleReview = async (
    requestId: string,
    action: "approved" | "rejected",
  ) => {
    try {
      await reviewRequest({ requestId, action });
    } catch {
      // error handled by mutation
    }
  };

  // Reset form state on close
  useEffect(() => {
    if (!formOpen) {
      setEditTarget(null);
      setFormName("");
      setFormSlug("");
      setFormDescription("");
      setFormVisibility("open");
      setFormParentGroupId("none");
    }
  }, [formOpen]);

  // Reset add member state on dialog close
  useEffect(() => {
    if (!memberGroup) {
      setAddMemberUserId("");
      setAddMemberRole("member");
    }
  }, [memberGroup]);

  return (
    <div className="p-4 sm:p-6 pb-10">
      <PageHeader
        title={t("title")}
        description={t("description")}
        actions={
          <div className="flex gap-2">
            <div className="flex rounded-md border">
              <Button
                variant={viewMode === "table" ? "default" : "ghost"}
                size="sm"
                onClick={() => setViewMode("table")}
                className="rounded-r-none"
                title={t("viewMode.table")}
              >
                <LayoutList className="h-3.5 w-3.5" />
              </Button>
              <Button
                variant={viewMode === "tree" ? "default" : "ghost"}
                size="sm"
                onClick={() => setViewMode("tree")}
                className="rounded-l-none"
                title={t("viewMode.tree")}
              >
                <FolderTree className="h-3.5 w-3.5" />
              </Button>
            </div>
            <Button size="sm" onClick={openCreate} className="gap-1">
              <Plus className="h-3.5 w-3.5" /> {t("createGroup")}
            </Button>
            <Button
              variant="outline"
              size="sm"
              onClick={refresh}
              disabled={spinning}
              className="gap-1"
            >
              <RefreshCw
                className={
                  spinning ? "animate-spin h-3.5 w-3.5" : "h-3.5 w-3.5"
                }
              />
              {tc("refresh")}
            </Button>
          </div>
        }
      />

      <div className="mt-4">
        <SearchInput
          value={search}
          onChange={setSearch}
          placeholder={t("searchPlaceholder")}
          className="max-w-sm"
        />
      </div>

      <div className="mt-4">
        {showSkeleton ? (
          <TableSkeleton rows={5} />
        ) : viewMode === "tree" ? (
          tree.length === 0 ? (
            <EmptyState
              icon={FolderTree}
              title={t("emptyTitle")}
              description={t("emptyDescription")}
            />
          ) : (
            <GroupTreeView
              tree={tree}
              groupMap={groupMap}
              onEdit={openEdit}
              onDelete={(g) => setDeleteTarget(g)}
              onManageMembers={(id) => setMemberGroup(id)}
              onViewRequests={(id) => setRequestGroup(id)}
            />
          )
        ) : groups.length === 0 ? (
          <EmptyState
            icon={FolderTree}
            title={t("emptyTitle")}
            description={t("emptyDescription")}
          />
        ) : (
          <div className="overflow-x-auto rounded-md border">
            <table className="w-full min-w-[600px] text-base md:text-sm">
              <thead>
                <tr className="border-b bg-muted/50">
                  <th className="px-4 py-3 text-left font-medium">
                    {t("columns.name")}
                  </th>
                  <th className="px-4 py-3 text-left font-medium">
                    {t("columns.slug")}
                  </th>
                  <th className="px-4 py-3 text-left font-medium">
                    {t("columns.visibility")}
                  </th>
                  <th className="px-4 py-3 text-left font-medium">
                    {t("columns.parent")}
                  </th>
                  <th className="px-4 py-3 text-left font-medium">
                    {t("columns.status")}
                  </th>
                  <th className="px-4 py-3 text-right font-medium">
                    {t("columns.actions")}
                  </th>
                </tr>
              </thead>
              <tbody>
                {groups.map((group) => (
                  <tr
                    key={group.id}
                    className="border-b last:border-0 hover:bg-muted/30"
                  >
                    <td className="px-4 py-3 font-medium">{group.name}</td>
                    <td className="px-4 py-3">
                      <code className="rounded bg-muted px-1.5 py-0.5 text-xs">
                        {group.slug}
                      </code>
                    </td>
                    <td className="px-4 py-3">
                      <Badge variant={visibilityBadge(group.visibility)}>
                        {t(`visibility.${group.visibility}`)}
                      </Badge>
                    </td>
                    <td className="px-4 py-3 text-muted-foreground">
                      {group.parent_group_id
                        ? (groupMap.get(group.parent_group_id)?.name ??
                          group.parent_group_id)
                        : t("noParent")}
                    </td>
                    <td className="px-4 py-3">
                      <Badge
                        variant={
                          group.status === "active"
                            ? "default"
                            : "secondary"
                        }
                      >
                        {group.status}
                      </Badge>
                    </td>
                    <td className="px-4 py-3 text-right">
                      <div className="flex items-center justify-end gap-1">
                        <Button
                          variant="ghost"
                          size="sm"
                          onClick={() => setMemberGroup(group.id)}
                          title={t("actions.manageMembers")}
                        >
                          <Users className="h-3.5 w-3.5" />
                        </Button>
                        {group.visibility === "closed" && (
                          <Button
                            variant="ghost"
                            size="sm"
                            onClick={() => setRequestGroup(group.id)}
                            title={t("actions.viewRequests")}
                          >
                            <UserPlus className="h-3.5 w-3.5" />
                          </Button>
                        )}
                        <Button
                          variant="ghost"
                          size="sm"
                          onClick={() => openEdit(group)}
                          title={t("actions.edit")}
                        >
                          <Pencil className="h-3.5 w-3.5" />
                        </Button>
                        <Button
                          variant="ghost"
                          size="sm"
                          onClick={() => setDeleteTarget(group)}
                          className="text-destructive hover:text-destructive"
                          title={t("actions.delete")}
                        >
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
      </div>

      {/* Create / Edit group dialog */}
      <Dialog open={formOpen} onOpenChange={setFormOpen}>
        <DialogContent className="max-sm:inset-0 max-sm:translate-x-0 max-sm:translate-y-0 sm:max-w-lg">
          <DialogHeader>
            <DialogTitle>
              {editTarget ? t("editGroup") : t("createGroup")}
            </DialogTitle>
            <DialogDescription className="sr-only">
              {editTarget ? t("editGroup") : t("createGroup")}
            </DialogDescription>
          </DialogHeader>
          <div className="space-y-4 py-2">
            <div className="space-y-1.5">
              <Label htmlFor="group-name">{t("columns.name")}</Label>
              <Input
                id="group-name"
                value={formName}
                onChange={(e) => handleNameChange(e.target.value)}
                placeholder={t("columns.name")}
                className="text-base md:text-sm"
              />
            </div>
            <div className="space-y-1.5">
              <Label htmlFor="group-slug">{t("columns.slug")}</Label>
              <Input
                id="group-slug"
                value={formSlug}
                onChange={(e) => setFormSlug(e.target.value)}
                placeholder="my-group"
                disabled={!!editTarget}
                className="text-base md:text-sm"
              />
              {!editTarget && (
                <p className="text-xs text-muted-foreground">
                  {t("slugHelp")}
                </p>
              )}
            </div>
            <div className="space-y-1.5">
              <Label htmlFor="group-description">
                {t("form.description")}
              </Label>
              <Input
                id="group-description"
                value={formDescription}
                onChange={(e) => setFormDescription(e.target.value)}
                placeholder={t("form.descriptionPlaceholder")}
                className="text-base md:text-sm"
              />
            </div>
            <div className="space-y-1.5">
              <Label>{t("columns.visibility")}</Label>
              <Select value={formVisibility} onValueChange={setFormVisibility}>
                <SelectTrigger size="sm">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="open">
                    {t("visibility.open")}
                  </SelectItem>
                  <SelectItem value="closed">
                    {t("visibility.closed")}
                  </SelectItem>
                </SelectContent>
              </Select>
            </div>
            <div className="space-y-1.5">
              <Label>{t("form.parentGroup")}</Label>
              <Select value={formParentGroupId} onValueChange={setFormParentGroupId}>
                <SelectTrigger size="sm">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="none">
                    {t("form.parentGroupNone")}
                  </SelectItem>
                  {groups
                    .filter((g) => g.id !== editTarget?.id)
                    .map((g) => (
                      <SelectItem key={g.id} value={g.id}>
                        {g.name}
                      </SelectItem>
                    ))}
                </SelectContent>
              </Select>
            </div>
          </div>
          <DialogFooter>
            <Button
              variant="outline"
              onClick={() => setFormOpen(false)}
              disabled={isCreating || isUpdating}
            >
              {tc("cancel")}
            </Button>
            <Button
              onClick={handleFormSubmit}
              disabled={
                isCreating ||
                isUpdating ||
                !formName.trim() ||
                !formSlug.trim()
              }
            >
              {isCreating || isUpdating
                ? "..."
                : editTarget
                  ? t("saveChanges")
                  : t("createGroup")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Delete confirmation dialog */}
      <ConfirmDeleteDialog
        open={!!deleteTarget}
        onOpenChange={(v) => !v && setDeleteTarget(null)}
        title={t("delete.title")}
        description={t("delete.description", {
          name: deleteTarget?.name ?? "",
        })}
        confirmValue={deleteTarget?.name ?? ""}
        confirmLabel={t("actions.delete")}
        onConfirm={handleDelete}
        loading={deleteLoading}
      />

      {/* Member management dialog */}
      <Dialog
        open={!!memberGroup}
        onOpenChange={(v) => !v && setMemberGroup(null)}
      >
        <DialogContent className="max-sm:inset-0 max-sm:translate-x-0 max-sm:translate-y-0 sm:max-w-lg">
          <DialogHeader>
            <DialogTitle>{t("members.title")}</DialogTitle>
            <DialogDescription className="sr-only">{t("members.title")}</DialogDescription>
          </DialogHeader>
          <div className="space-y-4 py-2">
            {/* Add member form */}
            <div className="flex gap-2">
              <UserPickerCombobox
                value={addMemberUserId}
                onChange={setAddMemberUserId}
                source="tenant_user"
                valueMode="uuid"
                placeholder={t("members.addPlaceholder")}
                className="flex-1"
              />
              <Select
                value={addMemberRole}
                onValueChange={setAddMemberRole}
              >
                <SelectTrigger size="sm" className="w-[110px]">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="member">
                    {t("members.role.member")}
                  </SelectItem>
                  <SelectItem value="admin">
                    {t("members.role.admin")}
                  </SelectItem>
                </SelectContent>
              </Select>
              <Button
                size="sm"
                onClick={handleAddMember}
                disabled={isAddingMember || !addMemberUserId.trim()}
                className="gap-1"
              >
                <UserPlus className="h-3.5 w-3.5" />
              </Button>
            </div>

            {/* Members list */}
            {membersLoading ? (
              <div className="py-4 text-center text-sm text-muted-foreground">
                {tc("loading")}
              </div>
            ) : members.length === 0 ? (
              <div className="py-4 text-center text-sm text-muted-foreground">
                {t("members.empty")}
              </div>
            ) : (
              <div className="max-h-64 overflow-y-auto">
                <table className="w-full text-base md:text-sm">
                  <thead>
                    <tr className="border-b bg-muted/50">
                      <th className="px-3 py-2 text-left font-medium">
                        {t("members.columns.name")}
                      </th>
                      <th className="px-3 py-2 text-left font-medium">
                        {t("members.columns.role")}
                      </th>
                      <th className="px-3 py-2 text-right font-medium">
                        {t("columns.actions")}
                      </th>
                    </tr>
                  </thead>
                  <tbody>
                    {members.map((member: GroupMember) => (
                      <tr
                        key={member.id}
                        className="border-b last:border-0 hover:bg-muted/30"
                      >
                        <td className="px-3 py-2">
                          <div>
                            <div className="font-medium">
                              {member.display_name ?? member.user_id}
                            </div>
                            {member.email && (
                              <div className="text-xs text-muted-foreground">
                                {member.email}
                              </div>
                            )}
                          </div>
                        </td>
                        <td className="px-3 py-2">
                          <Badge
                            variant={
                              member.role === "admin" ? "default" : "secondary"
                            }
                            className="text-xs gap-1"
                          >
                            {member.role === "admin" && (
                              <ShieldCheck className="h-3 w-3" />
                            )}
                            {t(`members.role.${member.role}`)}
                          </Badge>
                        </td>
                        <td className="px-3 py-2 text-right">
                          <div className="flex items-center justify-end gap-1">
                            {member.role === "member" && (
                              <Button
                                variant="ghost"
                                size="sm"
                                onClick={() =>
                                  changeMemberRole({
                                    userId: member.user_id,
                                    role: "admin",
                                  })
                                }
                                title={t("members.promoteToAdmin")}
                              >
                                <ShieldCheck className="h-3.5 w-3.5" />
                              </Button>
                            )}
                            {member.role === "admin" && (
                              <Button
                                variant="ghost"
                                size="sm"
                                onClick={() =>
                                  changeMemberRole({
                                    userId: member.user_id,
                                    role: "member",
                                  })
                                }
                                title={t("members.demoteToMember")}
                              >
                                <Users className="h-3.5 w-3.5" />
                              </Button>
                            )}
                            <Button
                              variant="ghost"
                              size="sm"
                              onClick={() =>
                                removeMember(member.user_id)
                              }
                              className="text-destructive hover:text-destructive"
                              title={t("members.remove")}
                            >
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
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => setMemberGroup(null)}>
              {tc("close")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Join requests dialog */}
      <Dialog
        open={!!requestGroup}
        onOpenChange={(v) => !v && setRequestGroup(null)}
      >
        <DialogContent className="max-sm:inset-0 max-sm:translate-x-0 max-sm:translate-y-0 sm:max-w-lg">
          <DialogHeader>
            <DialogTitle>{t("joinRequests.title")}</DialogTitle>
            <DialogDescription className="sr-only">{t("joinRequests.title")}</DialogDescription>
          </DialogHeader>
          <div className="space-y-2 py-2">
            {requestsLoading ? (
              <div className="py-4 text-center text-sm text-muted-foreground">
                {tc("loading")}
              </div>
            ) : requests.length === 0 ? (
              <div className="py-4 text-center text-sm text-muted-foreground">
                {t("joinRequests.empty")}
              </div>
            ) : (
              <div className="max-h-64 overflow-y-auto">
                <table className="w-full text-base md:text-sm">
                  <thead>
                    <tr className="border-b bg-muted/50">
                      <th className="px-3 py-2 text-left font-medium">
                        {t("joinRequests.columns.user")}
                      </th>
                      <th className="px-3 py-2 text-left font-medium">
                        {t("joinRequests.columns.message")}
                      </th>
                      <th className="px-3 py-2 text-left font-medium">
                        {t("joinRequests.columns.status")}
                      </th>
                      <th className="px-3 py-2 text-right font-medium">
                        {t("columns.actions")}
                      </th>
                    </tr>
                  </thead>
                  <tbody>
                    {requests.map((req: JoinRequest) => (
                      <tr
                        key={req.id}
                        className="border-b last:border-0 hover:bg-muted/30"
                      >
                        <td className="px-3 py-2">
                          <div>
                            <div className="font-medium">
                              {req.display_name ?? req.user_id}
                            </div>
                            {req.email && (
                              <div className="text-xs text-muted-foreground">
                                {req.email}
                              </div>
                            )}
                          </div>
                        </td>
                        <td className="px-3 py-2 text-muted-foreground">
                          {req.message ?? "-"}
                        </td>
                        <td className="px-3 py-2">
                          <Badge
                            variant={
                              req.status === "pending"
                                ? "secondary"
                                : req.status === "approved"
                                  ? "default"
                                  : "destructive"
                            }
                            className="text-xs"
                          >
                            {req.status}
                          </Badge>
                        </td>
                        <td className="px-3 py-2 text-right">
                          {req.status === "pending" && (
                            <div className="flex items-center justify-end gap-1">
                              <Button
                                variant="ghost"
                                size="sm"
                                onClick={() =>
                                  handleReview(req.id, "approved")
                                }
                                disabled={isReviewing}
                                title={t("joinRequests.approve")}
                                className="text-green-600 hover:text-green-600"
                              >
                                <CheckCircle className="h-3.5 w-3.5" />
                              </Button>
                              <Button
                                variant="ghost"
                                size="sm"
                                onClick={() =>
                                  handleReview(req.id, "rejected")
                                }
                                disabled={isReviewing}
                                title={t("joinRequests.reject")}
                                className="text-destructive hover:text-destructive"
                              >
                                <XCircle className="h-3.5 w-3.5" />
                              </Button>
                            </div>
                          )}
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => setRequestGroup(null)}>
              {tc("close")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}

export default GroupsAdminPage;
