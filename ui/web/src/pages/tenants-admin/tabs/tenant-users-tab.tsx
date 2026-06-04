import { useState, useMemo, useEffect } from "react";
import { useTranslation } from "react-i18next";
import { Plus, RefreshCw, Users, Trash2, Pencil, X, Check } from "lucide-react";
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
import { ConfirmDialog } from "@/components/shared/confirm-dialog";
import { EmptyState } from "@/components/shared/empty-state";
import { TableSkeleton } from "@/components/shared/loading-skeleton";
import { useContactResolver } from "@/hooks/use-contact-resolver";
import { formatUserLabel } from "@/lib/format-user-label";
import { useDeferredLoading } from "@/hooks/use-deferred-loading";
import { useMinLoading } from "@/hooks/use-min-loading";
import { useTenantDetail } from "../hooks/use-tenant-detail";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";

const ALL_ROLES = ["owner", "admin", "member", "viewer"] as const;
const ADMIN_CREATE_ROLES = ["member", "viewer"] as const;
const ADMIN_ROLE_CHANGE_ROLES = ["member", "viewer"] as const;

const ROLE_KEYS: Record<string, string> = {
  owner: "roleOwner", admin: "roleAdmin",
  member: "roleMember", viewer: "roleViewer",
};

const ROLE_COLORS: Record<string, string> = {
  owner: "bg-amber-100 text-amber-800 dark:bg-amber-900/30 dark:text-amber-300",
  admin: "bg-orange-100 text-orange-800 dark:bg-orange-900/30 dark:text-orange-300",
  member: "bg-muted text-muted-foreground",
  viewer: "bg-muted text-muted-foreground",
};

interface TenantUsersTabProps {
  tenantId: string;
  isOwner: boolean;
}

export function TenantUsersTab({ tenantId, isOwner }: TenantUsersTabProps) {
  const { t } = useTranslation("tenants");

  const createRoles = isOwner ? ALL_ROLES : ADMIN_CREATE_ROLES;
  const roleChangeRoles = isOwner ? ALL_ROLES : ADMIN_ROLE_CHANGE_ROLES;

  const {
    users, usersLoading, usersRefreshing, refreshUsers,
    createUser, updateUser, removeUser, updateUserRole,
    isCreating, isUpdating, isRemoving, isSavingRole,
  } = useTenantDetail(tenantId);

  const spinning = useMinLoading(usersRefreshing);
  const showSkeleton = useDeferredLoading(usersLoading && users.length === 0);

  const userIds = useMemo(() => users.map((u) => u.user_id), [users]);
  const { resolve } = useContactResolver(userIds);

  // --- Create user dialog state ---
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

  // --- Remove user state ---
  const [removeTarget, setRemoveTarget] = useState<string | null>(null);

  // --- Inline edit state ---
  const [editingUserId, setEditingUserId] = useState<string | null>(null);
  const [editDisplayName, setEditDisplayName] = useState("");
  const [editPhone, setEditPhone] = useState("");

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

  const handleRemove = async () => {
    if (!removeTarget) return;
    try { await removeUser(removeTarget); setRemoveTarget(null); } catch { /* handled */ }
  };

  const handleRoleChange = async (userId: string, newRole: string) => {
    try { await updateUserRole({ userId, role: newRole }); } catch { /* handled */ }
  };

  const startEditing = (u: { user_id: string; display_name?: string | null; phone?: string | null }) => {
    setEditingUserId(u.user_id);
    setEditDisplayName(u.display_name ?? "");
    setEditPhone(u.phone ?? "");
  };

  const handleEditSave = async () => {
    if (!editingUserId) return;
    try {
      await updateUser({
        userId: editingUserId,
        input: {
          display_name: editDisplayName.trim() || undefined,
          phone: editPhone.trim() || undefined,
        },
      });
      setEditingUserId(null);
    } catch { /* handled */ }
  };

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <h3 className="text-sm font-semibold flex items-center gap-2">
          {users.length > 0 && (
            <span className="text-xs font-normal text-muted-foreground">({users.length})</span>
          )}
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
        <div className="grid gap-2">
          {users.map((u) => (
            <div key={u.user_id} className="flex items-center justify-between rounded-lg border px-4 py-3 hover:bg-muted/30 transition-colors">
              {editingUserId === u.user_id ? (
                <div className="flex-1 flex flex-col gap-2">
                  <div className="flex items-center gap-2">
                    <Label className="text-xs shrink-0 w-20">{t("displayName")}</Label>
                    <Input value={editDisplayName} onChange={(e) => setEditDisplayName(e.target.value)} className="h-7 text-sm" />
                  </div>
                  <div className="flex items-center gap-2">
                    <Label className="text-xs shrink-0 w-20">{t("phone")}</Label>
                    <Input value={editPhone} onChange={(e) => setEditPhone(e.target.value)} className="h-7 text-sm" placeholder="+1 234 567 890" />
                  </div>
                  <div className="flex items-center gap-1 ml-20">
                    <Button size="sm" variant="ghost" className="h-6 w-6 p-0" onClick={handleEditSave} disabled={isUpdating}>
                      <Check className="h-3.5 w-3.5 text-green-600" />
                    </Button>
                    <Button size="sm" variant="ghost" className="h-6 w-6 p-0" onClick={() => setEditingUserId(null)} disabled={isUpdating}>
                      <X className="h-3.5 w-3.5 text-muted-foreground" />
                    </Button>
                  </div>
                </div>
              ) : (
                <div className="flex items-center gap-3 min-w-0">
                  <div className="flex h-8 w-8 shrink-0 items-center justify-center rounded-full bg-muted text-xs font-medium uppercase">
                    {(u.display_name || u.email || u.user_id).charAt(0)}
                  </div>
                  <div className="min-w-0">
                    <p className="text-sm font-medium truncate">{u.display_name || u.email || formatUserLabel(u.user_id, resolve)}</p>
                    <p className="text-xs text-muted-foreground truncate">
                      {u.phone ? `${u.phone} · ` : ""}{u.email || u.user_id}
                    </p>
                  </div>
                </div>
              )}
              {!editingUserId || editingUserId !== u.user_id ? (
                <div className="flex items-center gap-2 shrink-0">
                  <Button variant="ghost" size="sm" className="h-7 w-7 p-0 text-muted-foreground hover:text-foreground" onClick={() => startEditing(u)} title={t("editName")}>
                    <Pencil className="h-3 w-3" />
                  </Button>
                  {u.is_owner || u.role === "owner" ? (
                    <span className={`inline-flex items-center rounded-full px-2 py-0.5 text-xs font-medium ${ROLE_COLORS.owner}`}>
                      {t("roleOwner")}
                    </span>
                  ) : (
                    <Select value={u.role} onValueChange={(r) => handleRoleChange(u.user_id, r)} disabled={isSavingRole}>
                      <SelectTrigger className="h-7 w-[100px] text-xs"><SelectValue /></SelectTrigger>
                      <SelectContent>
                        {roleChangeRoles.map((r) => (
                          <SelectItem key={r} value={r}>{t(ROLE_KEYS[r] ?? r)}</SelectItem>
                        ))}
                      </SelectContent>
                    </Select>
                  )}
                  <Button variant="ghost" size="sm" className="h-8 w-8 p-0 text-muted-foreground hover:text-destructive" onClick={() => setRemoveTarget(u.user_id)} title={t("removeUser")}>
                    <Trash2 className="h-3.5 w-3.5" />
                  </Button>
                </div>
              ) : null}
            </div>
          ))}
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

      {/* Remove User Confirm */}
      <ConfirmDialog
        open={!!removeTarget}
        onOpenChange={(o) => { if (!o) setRemoveTarget(null); }}
        title={t("removeUser")}
        description={t("confirmRemoveUser")}
        confirmLabel={t("removeUser")}
        variant="destructive"
        onConfirm={handleRemove}
        loading={isRemoving}
      />
    </div>
  );
}
