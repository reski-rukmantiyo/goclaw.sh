import { useState, useEffect, useMemo } from "react";
import { useTranslation } from "react-i18next";
import {
  Plus,
  RefreshCw,
  Users,
  Pencil,
  Trash2,
  KeyRound,
  ArrowRightLeft,
} from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
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
import { ConfirmDeleteDialog } from "@/components/shared/confirm-delete-dialog";
import { useDeferredLoading } from "@/hooks/use-deferred-loading";
import { useMinLoading } from "@/hooks/use-min-loading";
import { useUsersAdmin } from "./hooks/use-users-admin";
import { formatLastLogin } from "@/lib/format";
import { formatAuthProvider, formatUserStatus } from "@/lib/format-user";
import { useAuthStore } from "@/stores/use-auth-store";
import { useRole } from "@/hooks/use-role";
import type { TenantUserData } from "@/types/tenant";

const ALL_ROLES = ["admin", "member", "viewer"] as const;
const ADMIN_CREATE_ROLES = ["member", "viewer"] as const;
const ADMIN_ROLE_CHANGE_ROLES = ["member", "viewer"] as const;

const ROLE_LABELS: Record<string, string> = {
  owner: "Owner",
  admin: "Admin",
  member: "Member",
  viewer: "Viewer",
};

function UsersAdminPage() {
  const { t } = useTranslation("users-admin");
  const { t: tc } = useTranslation("common");

  const currentUserId = useAuthStore((s) => s.userId);
  const { isAdmin: callerIsAdmin, isOwner: callerIsOwner } = useRole();

  const {
    users,
    loading,
    refreshing,
    refresh,
    createUser,
    updateUser,
    removeUser,
    changeRole,
    changePassword,
    isCreating,
    isUpdating,
    isRemoving,
    isSavingRole,
    isChangingPassword,
  } = useUsersAdmin();

  const spinning = useMinLoading(refreshing);
  const showSkeleton = useDeferredLoading(loading && users.length === 0);

  // --- Create dialog state ---
  const [createOpen, setCreateOpen] = useState(false);
  const [email, setEmail] = useState("");
  const [displayName, setDisplayName] = useState("");
  const [phone, setPhone] = useState("");
  const [password, setPassword] = useState("");
  const [createRole, setCreateRole] = useState("member");

  useEffect(() => {
    if (!createOpen) {
      setEmail(""); setDisplayName(""); setPhone(""); setPassword(""); setCreateRole("member");
    }
  }, [createOpen]);

  // --- Edit dialog state ---
  const [editTarget, setEditTarget] = useState<TenantUserData | null>(null);
  const [editDisplayName, setEditDisplayName] = useState("");
  const [editPhone, setEditPhone] = useState("");

  useEffect(() => {
    if (editTarget) {
      setEditDisplayName(editTarget.display_name ?? "");
      setEditPhone(editTarget.phone ?? "");
    }
  }, [editTarget]);

  // --- Change Role dialog state ---
  const [roleTarget, setRoleTarget] = useState<TenantUserData | null>(null);
  const [newRole, setNewRole] = useState("");

  useEffect(() => {
    if (roleTarget) {
      setNewRole(roleTarget.role);
    }
  }, [roleTarget]);

  // --- Change Password dialog state ---
  const [passwordTarget, setPasswordTarget] = useState<TenantUserData | null>(null);
  const [newPassword, setNewPassword] = useState("");

  useEffect(() => {
    if (!passwordTarget) setNewPassword("");
  }, [passwordTarget]);

  // --- Delete dialog state ---
  const [deleteTarget, setDeleteTarget] = useState<TenantUserData | null>(null);

  // Determine which roles the caller can create/change
  const createRoles = callerIsOwner ? ALL_ROLES : ADMIN_CREATE_ROLES;
  const roleChangeRoles = callerIsOwner ? ALL_ROLES : ADMIN_ROLE_CHANGE_ROLES;

  // --- Handlers ---
  const handleCreate = async () => {
    if (!email.trim() || !displayName.trim() || !password.trim()) return;
    try {
      await createUser({
        email: email.trim(),
        display_name: displayName.trim(),
        phone: phone.trim() || undefined,
        password: password.trim(),
        role: createRole,
      });
      setCreateOpen(false);
    } catch { /* handled by mutation */ }
  };

  const handleEditSave = async () => {
    if (!editTarget) return;
    try {
      await updateUser({
        userId: editTarget.user_id,
        input: {
          display_name: editDisplayName.trim() || undefined,
          phone: editPhone.trim() || undefined,
        },
      });
      setEditTarget(null);
    } catch { /* handled */ }
  };

  const handleRoleChange = async () => {
    if (!roleTarget || !newRole) return;
    try {
      await changeRole({ userId: roleTarget.user_id, role: newRole });
      setRoleTarget(null);
    } catch { /* handled */ }
  };

  const handlePasswordChange = async () => {
    if (!passwordTarget || !newPassword.trim()) return;
    try {
      await changePassword({ userId: passwordTarget.user_id, password: newPassword.trim() });
      setPasswordTarget(null);
    } catch { /* handled */ }
  };

  const handleDelete = async () => {
    if (!deleteTarget) return;
    try {
      await removeUser(deleteTarget.user_id);
      setDeleteTarget(null);
    } catch { /* handled */ }
  };

  // --- Action visibility per SRS v2.0 §6.8.5/6.8.6 ---
  const canEdit = (u: TenantUserData) => {
    if (callerIsOwner) return true;
    // Admin cannot edit Owner/Admin targets
    if (callerIsAdmin && (u.role === "owner" || u.role === "admin" || u.is_owner)) return false;
    return true;
  };

  const canChangeRole = (u: TenantUserData) => {
    if (u.is_owner || u.role === "owner") return false; // Owner role change = delete+recreate
    if (callerIsOwner) return true;
    if (callerIsAdmin && (u.role === "admin")) return false;
    return callerIsAdmin;
  };

  const canChangePassword = (u: TenantUserData) => {
    if (callerIsOwner) return true;
    if (callerIsAdmin && (u.role === "owner" || u.role === "admin" || u.is_owner)) return false;
    return callerIsAdmin;
  };

  const canDelete = (u: TenantUserData) => {
    if (u.user_id === currentUserId) return false; // Self-deletion blocked (REQ-6.8.8)
    if (callerIsOwner) return true;
    if (callerIsAdmin && (u.role === "owner" || u.role === "admin" || u.is_owner)) return false;
    return callerIsAdmin;
  };

  // Last owner check — count owners in list
  const ownerCount = useMemo(() => users.filter((u) => u.is_owner || u.role === "owner").length, [users]);
  const isLastOwner = (u: TenantUserData) => (u.is_owner || u.role === "owner") && ownerCount <= 1;

  return (
    <div className="p-4 sm:p-6 pb-10">
      <PageHeader
        title={t("title")}
        description={t("description")}
        actions={
          <div className="flex gap-2">
            <Button size="sm" onClick={() => setCreateOpen(true)} className="gap-1">
              <Plus className="h-3.5 w-3.5" /> {t("addUser")}
            </Button>
            <Button
              variant="outline"
              size="sm"
              onClick={refresh}
              disabled={spinning}
              className="gap-1"
            >
              <RefreshCw className={spinning ? "animate-spin h-3.5 w-3.5" : "h-3.5 w-3.5"} />
              {tc("refresh")}
            </Button>
          </div>
        }
      />

      <div className="mt-4">
        {showSkeleton ? (
          <TableSkeleton rows={5} />
        ) : users.length === 0 ? (
          <EmptyState
            icon={Users}
            title={t("emptyTitle")}
            description={t("emptyDescription")}
          />
        ) : (
          <div className="overflow-x-auto rounded-md border">
            <table className="w-full min-w-[600px] text-base md:text-sm">
              <thead>
                <tr className="border-b bg-muted/50">
                  <th className="px-4 py-3 text-left font-medium">{t("columns.displayName")}</th>
                  <th className="px-4 py-3 text-left font-medium">{t("columns.email")}</th>
                  <th className="px-4 py-3 text-left font-medium">{t("columns.role")}</th>
                  <th className="px-4 py-3 text-left font-medium">{t("columns.provider")}</th>
                  <th className="px-4 py-3 text-left font-medium">{t("columns.status")}</th>
                  <th className="px-4 py-3 text-left font-medium">{t("columns.lastLogin")}</th>
                  <th className="px-4 py-3 text-right font-medium">{t("columns.actions")}</th>
                </tr>
              </thead>
              <tbody>
                {users.map((user) => (
                  <tr key={user.user_id} className="border-b last:border-0 hover:bg-muted/30">
                    <td className="px-4 py-3 font-medium">
                      {user.display_name || user.email || user.user_id}
                    </td>
                    <td className="px-4 py-3 text-muted-foreground">
                      {user.email || user.user_id}
                    </td>
                    <td className="px-4 py-3">
                      <span className="text-sm">{ROLE_LABELS[user.role] ?? user.role}</span>
                    </td>
                    <td className="px-4 py-3 text-muted-foreground">
                      {formatAuthProvider(user.auth_provider)}
                    </td>
                    <td className="px-4 py-3">
                      {formatUserStatus(user.status)}
                    </td>
                    <td className="px-4 py-3 text-muted-foreground">
                      {formatLastLogin(user.last_login_at) || t("never")}
                    </td>
                    <td className="px-4 py-3 text-right">
                      <div className="flex items-center justify-end gap-1">
                        {canEdit(user) && (
                          <Button variant="ghost" size="sm" onClick={() => setEditTarget(user)} title={t("actions.edit")}>
                            <Pencil className="h-3.5 w-3.5" />
                          </Button>
                        )}
                        {canChangeRole(user) && (
                          <Button variant="ghost" size="sm" onClick={() => setRoleTarget(user)} title={t("actions.changeRole")}>
                            <ArrowRightLeft className="h-3.5 w-3.5" />
                          </Button>
                        )}
                        {canChangePassword(user) && (
                          <Button variant="ghost" size="sm" onClick={() => setPasswordTarget(user)} title={t("actions.changePassword")}>
                            <KeyRound className="h-3.5 w-3.5" />
                          </Button>
                        )}
                        {canDelete(user) && (
                          <Button
                            variant="ghost"
                            size="sm"
                            onClick={() => !isLastOwner(user) && setDeleteTarget(user)}
                            disabled={isLastOwner(user)}
                            title={isLastOwner(user) ? t("lastOwnerTooltip") : t("actions.delete")}
                            className={isLastOwner(user) ? "" : "text-destructive hover:text-destructive"}
                          >
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
      </div>

      {/* Create User Dialog */}
      <Dialog open={createOpen} onOpenChange={setCreateOpen}>
        <DialogContent className="max-sm:inset-0 max-sm:translate-x-0 max-sm:translate-y-0 sm:max-w-md">
          <DialogHeader>
            <DialogTitle>{t("addUser")}</DialogTitle>
            <DialogDescription className="sr-only">{t("addUser")}</DialogDescription>
          </DialogHeader>
          <div className="space-y-4 py-2">
            <div className="space-y-1.5">
              <Label htmlFor="user-email">{t("columns.email")}</Label>
              <Input id="user-email" type="email" value={email} onChange={(e) => setEmail(e.target.value)} placeholder="user@example.com" className="text-base md:text-sm" />
            </div>
            <div className="space-y-1.5">
              <Label htmlFor="user-display-name">{t("columns.displayName")}</Label>
              <Input id="user-display-name" value={displayName} onChange={(e) => setDisplayName(e.target.value)} placeholder={t("columns.displayName")} className="text-base md:text-sm" />
            </div>
            <div className="space-y-1.5">
              <Label htmlFor="user-phone">{t("columns.phone")}</Label>
              <Input id="user-phone" type="tel" value={phone} onChange={(e) => setPhone(e.target.value)} placeholder="+1 234 567 890" className="text-base md:text-sm" />
            </div>
            <div className="space-y-1.5">
              <Label htmlFor="user-password">{t("password")}</Label>
              <Input id="user-password" type="password" value={password} onChange={(e) => setPassword(e.target.value)} placeholder={t("passwordPlaceholder")} className="text-base md:text-sm" />
              <p className="text-xs text-muted-foreground mt-1">{t("passwordHint")}</p>
            </div>
            <div className="space-y-1.5">
              <Label>{t("selectRole")}</Label>
              <Select value={createRole} onValueChange={setCreateRole}>
                <SelectTrigger className="text-base md:text-sm"><SelectValue /></SelectTrigger>
                <SelectContent>
                  {createRoles.map((r) => (
                    <SelectItem key={r} value={r}>{t(`role.${r}`)}</SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => setCreateOpen(false)} disabled={isCreating}>{tc("cancel")}</Button>
            <Button onClick={handleCreate} disabled={isCreating || !email.trim() || !displayName.trim() || !password.trim()}>
              {isCreating ? "..." : t("addUser")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Edit User Dialog */}
      <Dialog open={!!editTarget} onOpenChange={(v) => !v && setEditTarget(null)}>
        <DialogContent className="max-sm:inset-0 max-sm:translate-x-0 max-sm:translate-y-0 sm:max-w-md">
          <DialogHeader>
            <DialogTitle>{t("editUser.title")}</DialogTitle>
            <DialogDescription className="sr-only">{t("editUser.title")}</DialogDescription>
          </DialogHeader>
          <div className="space-y-4 py-2">
            <div className="space-y-1.5">
              <Label htmlFor="edit-display-name">{t("columns.displayName")}</Label>
              <Input id="edit-display-name" value={editDisplayName} onChange={(e) => setEditDisplayName(e.target.value)} className="text-base md:text-sm" />
            </div>
            <div className="space-y-1.5">
              <Label htmlFor="edit-phone">{t("columns.phone")}</Label>
              <Input id="edit-phone" type="tel" value={editPhone} onChange={(e) => setEditPhone(e.target.value)} placeholder="+1 234 567 890" className="text-base md:text-sm" />
            </div>
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => setEditTarget(null)} disabled={isUpdating}>{tc("cancel")}</Button>
            <Button onClick={handleEditSave} disabled={isUpdating}>{isUpdating ? "..." : tc("confirm")}</Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Change Role Dialog */}
      <Dialog open={!!roleTarget} onOpenChange={(v) => !v && setRoleTarget(null)}>
        <DialogContent className="max-sm:inset-0 max-sm:translate-x-0 max-sm:translate-y-0 sm:max-w-sm">
          <DialogHeader>
            <DialogTitle>{t("changeRole.title")}</DialogTitle>
            <DialogDescription>{t("changeRole.description", { name: roleTarget?.display_name ?? "" })}</DialogDescription>
          </DialogHeader>
          <div className="space-y-4 py-2">
            <div className="space-y-1.5">
              <Label>{t("selectRole")}</Label>
              <Select value={newRole} onValueChange={setNewRole}>
                <SelectTrigger className="text-base md:text-sm"><SelectValue /></SelectTrigger>
                <SelectContent>
                  {roleChangeRoles.map((r) => (
                    <SelectItem key={r} value={r}>{t(`role.${r}`)}</SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => setRoleTarget(null)} disabled={isSavingRole}>{tc("cancel")}</Button>
            <Button onClick={handleRoleChange} disabled={isSavingRole || !newRole || newRole === roleTarget?.role}>
              {isSavingRole ? "..." : tc("confirm")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Change Password Dialog */}
      <Dialog open={!!passwordTarget} onOpenChange={(v) => !v && setPasswordTarget(null)}>
        <DialogContent className="max-sm:inset-0 max-sm:translate-x-0 max-sm:translate-y-0 sm:max-w-sm">
          <DialogHeader>
            <DialogTitle>{t("changePassword.title")}</DialogTitle>
            <DialogDescription>{t("changePassword.description", { name: passwordTarget?.display_name ?? "" })}</DialogDescription>
          </DialogHeader>
          <div className="space-y-4 py-2">
            <div className="space-y-1.5">
              <Label htmlFor="new-password">{t("changePassword.newPassword")}</Label>
              <Input id="new-password" type="password" value={newPassword} onChange={(e) => setNewPassword(e.target.value)} className="text-base md:text-sm" />
              <p className="text-xs text-muted-foreground mt-1">{t("passwordHint")}</p>
            </div>
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => setPasswordTarget(null)} disabled={isChangingPassword}>{tc("cancel")}</Button>
            <Button onClick={handlePasswordChange} disabled={isChangingPassword || !newPassword.trim()}>
              {isChangingPassword ? "..." : tc("confirm")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Delete Confirmation Dialog */}
      <ConfirmDeleteDialog
        open={!!deleteTarget}
        onOpenChange={(v) => !v && setDeleteTarget(null)}
        title={t("delete.title")}
        description={t("delete.description", { name: deleteTarget?.display_name ?? "" })}
        confirmValue={deleteTarget?.display_name ?? ""}
        confirmLabel={t("actions.delete")}
        onConfirm={handleDelete}
        loading={isRemoving}
      />
    </div>
  );
}

export default UsersAdminPage;
