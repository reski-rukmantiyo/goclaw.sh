import { useState, useEffect, useMemo } from "react";
import { useTranslation } from "react-i18next";
import { Plus, RefreshCw, Users, Pencil, Trash2, KeyRound, ArrowRightLeft } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
  DialogFooter,
} from "@/components/ui/dialog";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { ConfirmDeleteDialog } from "@/components/shared/confirm-delete-dialog";
import { EmptyState } from "@/components/shared/empty-state";
import { TableSkeleton } from "@/components/shared/loading-skeleton";
import { useDeferredLoading } from "@/hooks/use-deferred-loading";
import { useMinLoading } from "@/hooks/use-min-loading";
import { useTenantDetail } from "../hooks/use-tenant-detail";
import { formatLastLogin } from "@/lib/format";
import { formatAuthProvider, formatUserStatus } from "@/lib/format-user";
import { useAuthStore } from "@/stores/use-auth-store";
import { useRole } from "@/hooks/use-role";

const ALL_ROLES = ["admin", "member", "viewer"] as const;
const ADMIN_CREATE_ROLES = ["member", "viewer"] as const;
const ADMIN_ROLE_CHANGE_ROLES = ["member", "viewer"] as const;

const ROLE_LABELS: Record<string, string> = {
  owner: "Owner",
  admin: "Admin",
  member: "Member",
  viewer: "Viewer",
};

interface TenantUsersTabProps {
  tenantId: string;
  isOwner: boolean;
}

export function TenantUsersTab({ tenantId, isOwner }: TenantUsersTabProps) {
  const { t } = useTranslation("tenants");

  const currentUserId = useAuthStore((s) => s.userId);
  const { isGatewayToken } = useRole();
  // isOwner prop is from Tenant Detail context; also treat Gateway Token as owner
  const callerIsOwner = isOwner || isGatewayToken;
  const callerIsAdmin = !callerIsOwner; // if not owner/gateway, must be admin (route guard ensures this)

  const createRoles = callerIsOwner ? ALL_ROLES : ADMIN_CREATE_ROLES;
  const roleChangeRoles = callerIsOwner ? ALL_ROLES : ADMIN_ROLE_CHANGE_ROLES;

  const {
    users, usersLoading, usersRefreshing, refreshUsers,
    createUser, updateUser, removeUser, updateUserRole, changePassword,
    isCreating, isUpdating, isRemoving, isSavingRole, isChangingPassword,
  } = useTenantDetail(tenantId);

  const spinning = useMinLoading(usersRefreshing);
  const showSkeleton = useDeferredLoading(usersLoading && users.length === 0);

  // --- Create dialog state ---
  const [addOpen, setAddOpen] = useState(false);
  const [email, setEmail] = useState("");
  const [displayName, setDisplayName] = useState("");
  const [phone, setPhone] = useState("");
  const [password, setPassword] = useState("");
  const [createRole, setCreateRole] = useState("member");

  useEffect(() => {
    if (!addOpen) {
      setEmail(""); setDisplayName(""); setPhone(""); setPassword(""); setCreateRole("member");
    }
  }, [addOpen]);

  // --- Edit dialog state ---
  const [editTarget, setEditTarget] = useState<{ user_id: string; display_name?: string | null; phone?: string | null } | null>(null);
  const [editDisplayName, setEditDisplayName] = useState("");
  const [editPhone, setEditPhone] = useState("");

  useEffect(() => {
    if (editTarget) {
      setEditDisplayName(editTarget.display_name ?? "");
      setEditPhone(editTarget.phone ?? "");
    }
  }, [editTarget]);

  // --- Change Role dialog state ---
  const [roleTarget, setRoleTarget] = useState<{ user_id: string; role: string; display_name?: string | null } | null>(null);
  const [newRole, setNewRole] = useState("");

  useEffect(() => {
    if (roleTarget) setNewRole(roleTarget.role);
  }, [roleTarget]);

  // --- Change Password dialog state ---
  const [passwordTarget, setPasswordTarget] = useState<{ user_id: string; display_name?: string | null } | null>(null);
  const [newPassword, setNewPassword] = useState("");

  useEffect(() => {
    if (!passwordTarget) setNewPassword("");
  }, [passwordTarget]);

  // --- Delete dialog state ---
  const [deleteTarget, setDeleteTarget] = useState<{ user_id: string; display_name?: string | null; role: string; is_owner: boolean } | null>(null);

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
      setAddOpen(false);
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
      await updateUserRole({ userId: roleTarget.user_id, role: newRole });
      setRoleTarget(null);
    } catch { /* handled */ }
  };

  const handleDelete = async () => {
    if (!deleteTarget) return;
    try { await removeUser(deleteTarget.user_id); setDeleteTarget(null); } catch { /* handled */ }
  };

  // --- Action visibility per SRS v2.0 §6.8.5/6.8.6 ---
  const canEdit = (u: { role: string; is_owner: boolean }) => {
    if (callerIsOwner) return true;
    if (callerIsAdmin && (u.role === "owner" || u.role === "admin" || u.is_owner)) return false;
    return true;
  };

  const canChangeRole = (u: { role: string; is_owner: boolean }) => {
    if (u.is_owner || u.role === "owner") return false;
    if (callerIsOwner) return true;
    if (callerIsAdmin && u.role === "admin") return false;
    return callerIsAdmin;
  };

  const canChangePassword = (u: { role: string; is_owner: boolean }) => {
    if (callerIsOwner) return true;
    if (callerIsAdmin && (u.role === "owner" || u.role === "admin" || u.is_owner)) return false;
    return callerIsAdmin;
  };

  const canDelete = (u: { user_id: string; role: string; is_owner: boolean }) => {
    if (u.user_id === currentUserId) return false;
    if (callerIsOwner) return true;
    if (callerIsAdmin && (u.role === "owner" || u.role === "admin" || u.is_owner)) return false;
    return callerIsAdmin;
  };

  const ownerCount = useMemo(() => users.filter((u) => u.is_owner || u.role === "owner").length, [users]);
  const isLastOwner = (u: { role: string; is_owner: boolean }) => (u.is_owner || u.role === "owner") && ownerCount <= 1;

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <h3 className="text-sm font-semibold flex items-center gap-2">
        </h3>
        <div className="flex gap-2">
          <Button size="sm" onClick={() => setAddOpen(true)} className="gap-1">
            <Plus className="h-3.5 w-3.5" /> {t("createUser")}
          </Button>
          <Button variant="outline" size="sm" onClick={refreshUsers} disabled={spinning} className="gap-1">
            <RefreshCw className={spinning ? "animate-spin h-3.5 w-3.5" : "h-3.5 w-3.5"} />
          </Button>
        </div>
      </div>

      {showSkeleton ? (
        <TableSkeleton rows={4} />
      ) : users.length === 0 ? (
        <EmptyState icon={Users} title={t("noUsers")} description="" />
      ) : (
        <div className="overflow-x-auto rounded-md border">
          <table className="w-full min-w-[600px] text-base md:text-sm">
            <thead>
              <tr className="border-b bg-muted/50">
                <th className="px-4 py-3 text-left font-medium">{t("displayName")}</th>
                <th className="px-4 py-3 text-left font-medium">{t("email")}</th>
                <th className="px-4 py-3 text-left font-medium">{t("role")}</th>
                <th className="px-4 py-3 text-left font-medium">{t("provider", { defaultValue: "Provider" })}</th>
                <th className="px-4 py-3 text-left font-medium">{t("status")}</th>
                <th className="px-4 py-3 text-left font-medium">{t("lastLogin", { defaultValue: "Last Login" })}</th>
                <th className="px-4 py-3 text-right font-medium">{t("actions", { defaultValue: "Actions" })}</th>
              </tr>
            </thead>
            <tbody>
              {users.map((u) => (
                <tr key={u.user_id} className="border-b last:border-0 hover:bg-muted/30">
                  <td className="px-4 py-3 font-medium">
                    {u.display_name || u.email || u.user_id}
                  </td>
                  <td className="px-4 py-3 text-muted-foreground">
                    {u.email || u.user_id}
                  </td>
                  <td className="px-4 py-3">
                    <span className="text-sm">{ROLE_LABELS[u.role] ?? u.role}</span>
                  </td>
                  <td className="px-4 py-3 text-muted-foreground">
                    {formatAuthProvider(u.auth_provider)}
                  </td>
                  <td className="px-4 py-3">
                    {formatUserStatus(u.status)}
                  </td>
                  <td className="px-4 py-3 text-muted-foreground">
                    {formatLastLogin(u.last_login_at) || t("never", { defaultValue: "Never" })}
                  </td>
                  <td className="px-4 py-3 text-right">
                    <div className="flex items-center justify-end gap-1">
                      {canEdit(u) && (
                        <Button variant="ghost" size="sm" onClick={() => setEditTarget(u)} title={t("editName")}>
                          <Pencil className="h-3.5 w-3.5" />
                        </Button>
                      )}
                      {canChangeRole(u) && (
                        <Button variant="ghost" size="sm" onClick={() => setRoleTarget(u)} title={t("updateRole")}>
                          <ArrowRightLeft className="h-3.5 w-3.5" />
                        </Button>
                      )}
                      {canChangePassword(u) && (
                        <Button variant="ghost" size="sm" onClick={() => setPasswordTarget(u)} title={t("changePassword", { defaultValue: "Change Password" })}>
                          <KeyRound className="h-3.5 w-3.5" />
                        </Button>
                      )}
                      {canDelete(u) && (
                        <Button
                          variant="ghost"
                          size="sm"
                          onClick={() => !isLastOwner(u) && setDeleteTarget(u)}
                          disabled={isLastOwner(u)}
                          title={isLastOwner(u) ? t("lastOwnerBlocked") : t("removeUser")}
                          className={isLastOwner(u) ? "" : "text-destructive hover:text-destructive"}
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

      {/* Create User Dialog */}
      <Dialog open={addOpen} onOpenChange={setAddOpen}>
        <DialogContent className="max-sm:inset-0 max-sm:translate-x-0 max-sm:translate-y-0 sm:max-w-md">
          <DialogHeader>
            <DialogTitle>{t("createUserTitle")}</DialogTitle>
            <DialogDescription className="sr-only">{t("createUserTitle")}</DialogDescription>
          </DialogHeader>
          <div className="space-y-4 py-2">
            <div className="space-y-1.5">
              <Label htmlFor="tenant-user-email">{t("email")}</Label>
              <Input id="tenant-user-email" type="email" value={email} onChange={(e) => setEmail(e.target.value)} placeholder="user@example.com" className="text-base md:text-sm" />
            </div>
            <div className="space-y-1.5">
              <Label htmlFor="tenant-user-name">{t("displayName")}</Label>
              <Input id="tenant-user-name" value={displayName} onChange={(e) => setDisplayName(e.target.value)} placeholder={t("displayName")} className="text-base md:text-sm" />
            </div>
            <div className="space-y-1.5">
              <Label htmlFor="tenant-user-phone">{t("phone")}</Label>
              <Input id="tenant-user-phone" type="tel" value={phone} onChange={(e) => setPhone(e.target.value)} placeholder="+1 234 567 890" className="text-base md:text-sm" />
            </div>
            <div className="space-y-1.5">
              <Label htmlFor="tenant-user-password">{t("password")}</Label>
              <Input id="tenant-user-password" type="password" value={password} onChange={(e) => setPassword(e.target.value)} placeholder={t("password")} className="text-base md:text-sm" />
              <p className="text-xs text-muted-foreground mt-1">{t("passwordHint")}</p>
            </div>
            <div className="space-y-1.5">
              <Label>{t("selectRole")}</Label>
              <Select value={createRole} onValueChange={setCreateRole}>
                <SelectTrigger className="text-base md:text-sm"><SelectValue /></SelectTrigger>
                <SelectContent>
                  {createRoles.map((r) => (
                    <SelectItem key={r} value={r}>{t(ROLE_KEYS[r] ?? r)}</SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
          </div>
          <DialogFooter>
            <Button onClick={handleCreate} disabled={isCreating || !email.trim() || !displayName.trim() || !password.trim()}>
              {isCreating ? "..." : t("createUser")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Edit User Dialog */}
      <Dialog open={!!editTarget} onOpenChange={(v) => !v && setEditTarget(null)}>
        <DialogContent className="max-sm:inset-0 max-sm:translate-x-0 max-sm:translate-y-0 sm:max-w-md">
          <DialogHeader>
            <DialogTitle>{t("editName")}</DialogTitle>
            <DialogDescription className="sr-only">{t("editName")}</DialogDescription>
          </DialogHeader>
          <div className="space-y-4 py-2">
            <div className="space-y-1.5">
              <Label htmlFor="tu-edit-display-name">{t("displayName")}</Label>
              <Input id="tu-edit-display-name" value={editDisplayName} onChange={(e) => setEditDisplayName(e.target.value)} className="text-base md:text-sm" />
            </div>
            <div className="space-y-1.5">
              <Label htmlFor="tu-edit-phone">{t("phone")}</Label>
              <Input id="tu-edit-phone" type="tel" value={editPhone} onChange={(e) => setEditPhone(e.target.value)} placeholder="+1 234 567 890" className="text-base md:text-sm" />
            </div>
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => setEditTarget(null)} disabled={isUpdating}>{t("cancel", { defaultValue: "Cancel" })}</Button>
            <Button onClick={handleEditSave} disabled={isUpdating}>{isUpdating ? "..." : t("save", { defaultValue: "Save" })}</Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Change Role Dialog */}
      <Dialog open={!!roleTarget} onOpenChange={(v) => !v && setRoleTarget(null)}>
        <DialogContent className="max-sm:inset-0 max-sm:translate-x-0 max-sm:translate-y-0 sm:max-w-sm">
          <DialogHeader>
            <DialogTitle>{t("updateRole")}</DialogTitle>
            <DialogDescription>{t("changeRoleDescription", { name: roleTarget?.display_name ?? "" })}</DialogDescription>
          </DialogHeader>
          <div className="space-y-4 py-2">
            <div className="space-y-1.5">
              <Label>{t("selectRole")}</Label>
              <Select value={newRole} onValueChange={setNewRole}>
                <SelectTrigger className="text-base md:text-sm"><SelectValue /></SelectTrigger>
                <SelectContent>
                  {roleChangeRoles.map((r) => (
                    <SelectItem key={r} value={r}>{t(ROLE_KEYS[r] ?? r)}</SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => setRoleTarget(null)} disabled={isSavingRole}>{t("cancel", { defaultValue: "Cancel" })}</Button>
            <Button onClick={handleRoleChange} disabled={isSavingRole || !newRole || newRole === roleTarget?.role}>
              {isSavingRole ? "..." : t("save", { defaultValue: "Save" })}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Change Password Dialog */}
      <Dialog open={!!passwordTarget} onOpenChange={(v) => !v && setPasswordTarget(null)}>
        <DialogContent className="max-sm:inset-0 max-sm:translate-x-0 max-sm:translate-y-0 sm:max-w-sm">
          <DialogHeader>
            <DialogTitle>{t("changePassword", { defaultValue: "Change Password" })}</DialogTitle>
            <DialogDescription>{t("changePasswordDescription", { name: passwordTarget?.display_name ?? "" })}</DialogDescription>
          </DialogHeader>
          <div className="space-y-4 py-2">
            <div className="space-y-1.5">
              <Label htmlFor="tu-new-password">{t("password")}</Label>
              <Input id="tu-new-password" type="password" value={newPassword} onChange={(e) => setNewPassword(e.target.value)} className="text-base md:text-sm" />
              <p className="text-xs text-muted-foreground mt-1">{t("passwordHint")}</p>
            </div>
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => setPasswordTarget(null)} disabled={false}>{t("cancel", { defaultValue: "Cancel" })}</Button>
            <Button onClick={async () => {
              if (!passwordTarget || !newPassword.trim()) return;
              try {
                await changePassword({ userId: passwordTarget.user_id, password: newPassword.trim() });
                setPasswordTarget(null);
              } catch { /* handled by mutation */ }
            }} disabled={isChangingPassword || !newPassword.trim()}>
              {t("save", { defaultValue: "Save" })}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Remove User Confirm */}
      <ConfirmDeleteDialog
        open={!!deleteTarget}
        onOpenChange={(o) => { if (!o) setDeleteTarget(null); }}
        title={t("removeUser")}
        description={t("confirmRemoveUser")}
        confirmValue={deleteTarget?.display_name ?? ""}
        confirmLabel={t("removeUser")}
        onConfirm={handleDelete}
        loading={isRemoving}
      />
    </div>
  );
}

const ROLE_KEYS: Record<string, string> = {
  owner: "roleOwner", admin: "roleAdmin",
  member: "roleMember", viewer: "roleViewer",
};
