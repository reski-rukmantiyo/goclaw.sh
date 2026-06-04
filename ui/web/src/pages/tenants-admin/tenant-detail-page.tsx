import { useState, useMemo, useEffect } from "react";
import { useParams, useNavigate } from "react-router";
import { useTranslation } from "react-i18next";
import { ArrowLeft, Plus, RefreshCw, Users, Trash2, Calendar, Hash, Shield, Pencil, X, Check } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Badge } from "@/components/ui/badge";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
  DialogFooter,
} from "@/components/ui/dialog";
import { ConfirmDialog } from "@/components/shared/confirm-dialog";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { PageHeader } from "@/components/shared/page-header";
import { EmptyState } from "@/components/shared/empty-state";
import { TableSkeleton } from "@/components/shared/loading-skeleton";
import { UserPickerCombobox } from "@/components/shared/user-picker-combobox";
import { useContactResolver } from "@/hooks/use-contact-resolver";
import { formatUserLabel } from "@/lib/format-user-label";
import { useDeferredLoading } from "@/hooks/use-deferred-loading";
import { useMinLoading } from "@/hooks/use-min-loading";
import { useTenantDetail } from "./hooks/use-tenant-detail";
import { ROUTES, route } from "@/lib/constants";
import { useTenants } from "@/hooks/use-tenants";

const TENANT_ROLES = ["owner", "admin", "member", "viewer"] as const;
const NON_OWNER_ROLES = ["admin", "member", "viewer"] as const;

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

export function TenantDetailPage() {
  const { id = "" } = useParams<{ id: string }>();
  const navigate = useNavigate();
  const { t } = useTranslation("tenants");
  const { t: tc } = useTranslation("common");

  const { isOwner, currentTenantSlug } = useTenants();
  const availableRoles = isOwner ? TENANT_ROLES : NON_OWNER_ROLES;

  const {
    tenant, tenantLoading, users, usersLoading, usersRefreshing, refreshUsers,
    createUser, enrollUser, updateUser, removeUser, updateUserRole,
    isCreating, isEnrolling, isUpdating, isRemoving, isSavingRole,
    updateTenantName, deleteTenant,
  } = useTenantDetail(id);

  const spinning = useMinLoading(usersRefreshing);
  const showSkeleton = useDeferredLoading(usersLoading && users.length === 0);

  // Resolve user IDs to display names via contacts
  const userIds = useMemo(() => users.map((u) => u.user_id), [users]);
  const { resolve } = useContactResolver(userIds);

  // --- Create/Enroll dialog state ---
  const [addOpen, setAddOpen] = useState(false);
  const [addMode, setAddMode] = useState<"create" | "enroll">("create");
  // Create fields
  const [email, setEmail] = useState("");
  const [displayName, setDisplayName] = useState("");
  const [phone, setPhone] = useState("");
  const [password, setPassword] = useState("");
  const [createRole, setCreateRole] = useState("member");
  // Enroll fields
  const [enrollUserId, setEnrollUserId] = useState("");
  const [enrollRole, setEnrollRole] = useState("member");

  // Reset dialog state on close
  useEffect(() => {
    if (!addOpen) {
      setAddMode("create");
      setEmail("");
      setDisplayName("");
      setPhone("");
      setPassword("");
      setCreateRole("member");
      setEnrollUserId("");
      setEnrollRole("member");
    }
  }, [addOpen]);

  // --- Remove user state ---
  const [removeTarget, setRemoveTarget] = useState<string | null>(null);

  // --- Inline edit state ---
  const [editingUserId, setEditingUserId] = useState<string | null>(null);
  const [editDisplayName, setEditDisplayName] = useState("");
  const [editPhone, setEditPhone] = useState("");

  // --- Edit tenant name state ---
  const [editOpen, setEditOpen] = useState(false);
  const [editName, setEditName] = useState("");
  const [editSaving, setEditSaving] = useState(false);

  // --- Delete tenant state ---
  const [deleteOpen, setDeleteOpen] = useState(false);
  const [deleteConfirmName, setDeleteConfirmName] = useState("");
  const [deleteSaving, setDeleteSaving] = useState(false);

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
    } catch {
      // error handled by mutation
    }
  };

  const handleEnroll = async () => {
    if (!enrollUserId.trim()) return;
    try {
      await enrollUser({ userId: enrollUserId.trim(), role: enrollRole });
      setAddOpen(false);
    } catch {
      // error handled by mutation
    }
  };

  const handleRemove = async () => {
    if (!removeTarget) return;
    try {
      await removeUser(removeTarget);
      setRemoveTarget(null);
    } catch {
      // error handled by mutation
    }
  };

  const handleRoleChange = async (userId: string, newRole: string) => {
    try {
      await updateUserRole({ userId, role: newRole });
    } catch {
      // error handled by mutation
    }
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
    } catch {
      // error handled by mutation
    }
  };

  const handleEditCancel = () => {
    setEditingUserId(null);
  };

  const handleTenantNameSave = async () => {
    if (!editName.trim() || editName.trim() === tenant?.name) {
      setEditOpen(false);
      return;
    }
    setEditSaving(true);
    try {
      await updateTenantName(editName.trim());
      setEditOpen(false);
    } finally {
      setEditSaving(false);
    }
  };

  const handleDeleteTenant = async () => {
    if (deleteConfirmName.trim() !== tenant?.name) return;
    setDeleteSaving(true);
    try {
      await deleteTenant();
      setDeleteOpen(false);
      setDeleteConfirmName("");
      navigate(route(currentTenantSlug, ROUTES.TENANTS));
    } finally {
      setDeleteSaving(false);
    }
  };

  if (tenantLoading) {
    return <div className="p-4 sm:p-6 pb-10"><TableSkeleton rows={3} /></div>;
  }

  return (
    <div className="p-4 sm:p-6 space-y-6">
      <PageHeader
        title={tenant?.name ?? t("detail")}
        description=""
        actions={
          <div className="flex items-center gap-2">
            {tenant && (
              <Button
                variant="outline"
                size="sm"
                className="gap-1"
                onClick={() => { setEditName(tenant.name); setEditOpen(true); }}
              >
                <Pencil className="h-3.5 w-3.5" /> {t("editName")}
              </Button>
            )}
            {tenant && (
              <Button
                variant="destructive"
                size="sm"
                className="gap-1"
                onClick={() => { setDeleteOpen(true); setDeleteConfirmName(""); }}
              >
                <Trash2 className="h-3.5 w-3.5" /> {t("deleteTenant")}
              </Button>
            )}
            <Button variant="outline" size="sm" onClick={() => navigate(route(currentTenantSlug, ROUTES.TENANTS))} className="gap-1">
              <ArrowLeft className="h-3.5 w-3.5" /> {t("back")}
            </Button>
          </div>
        }
      />

      {/* Tenant Info Card */}
      {tenant && (
        <div className="grid grid-cols-1 sm:grid-cols-3 gap-3">
          <InfoCard icon={Hash} label={t("slug")} value={tenant.slug} mono />
          <InfoCard icon={Shield} label={t("status")}>
            <Badge variant={tenant.status === "active" ? "default" : tenant.status === "suspended" ? "destructive" : "secondary"}>
              {t(tenant.status) || tenant.status}
            </Badge>
          </InfoCard>
          <InfoCard icon={Calendar} label={t("created")} value={new Date(tenant.created_at).toLocaleDateString()} />
        </div>
      )}

      {/* User Management */}
      <div>
        <div className="flex items-center justify-between mb-3">
          <h2 className="text-base font-semibold flex items-center gap-2">
            <Users className="h-4 w-4 text-muted-foreground" />
            {t("userManagement")}
            {users.length > 0 && (
              <span className="text-xs font-normal text-muted-foreground">({users.length})</span>
            )}
          </h2>
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
                  /* Inline edit mode */
                  <div className="flex-1 flex flex-col gap-2">
                    <div className="flex items-center gap-2">
                      <Label className="text-xs shrink-0 w-20">{t("displayName")}</Label>
                      <Input
                        value={editDisplayName}
                        onChange={(e) => setEditDisplayName(e.target.value)}
                        className="h-7 text-sm"
                      />
                    </div>
                    <div className="flex items-center gap-2">
                      <Label className="text-xs shrink-0 w-20">{t("phone")}</Label>
                      <Input
                        value={editPhone}
                        onChange={(e) => setEditPhone(e.target.value)}
                        className="h-7 text-sm"
                        placeholder="+1 234 567 890"
                      />
                    </div>
                    <div className="flex items-center gap-1 ml-20">
                      <Button size="sm" variant="ghost" className="h-6 w-6 p-0" onClick={handleEditSave} disabled={isUpdating}>
                        <Check className="h-3.5 w-3.5 text-green-600" />
                      </Button>
                      <Button size="sm" variant="ghost" className="h-6 w-6 p-0" onClick={handleEditCancel} disabled={isUpdating}>
                        <X className="h-3.5 w-3.5 text-muted-foreground" />
                      </Button>
                    </div>
                  </div>
                ) : (
                  /* Display mode */
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
                    <Button
                      variant="ghost"
                      size="sm"
                      className="h-7 w-7 p-0 text-muted-foreground hover:text-foreground"
                      onClick={() => startEditing(u)}
                      title={t("editName")}
                    >
                      <Pencil className="h-3 w-3" />
                    </Button>
                    {u.is_owner || u.role === "owner" ? (
                      <span className={`inline-flex items-center rounded-full px-2 py-0.5 text-xs font-medium ${ROLE_COLORS.owner}`}>
                        {t("roleOwner")}
                      </span>
                    ) : (
                      <Select value={u.role} onValueChange={(r) => handleRoleChange(u.user_id, r)} disabled={isSavingRole}>
                        <SelectTrigger className="h-7 w-[100px] text-xs">
                          <SelectValue />
                        </SelectTrigger>
                        <SelectContent>
                          {availableRoles.map((r) => (
                            <SelectItem key={r} value={r}>{t(ROLE_KEYS[r] ?? r)}</SelectItem>
                          ))}
                        </SelectContent>
                      </Select>
                    )}
                    <Button
                      variant="ghost"
                      size="sm"
                      className="h-8 w-8 p-0 text-muted-foreground hover:text-destructive"
                      onClick={() => setRemoveTarget(u.user_id)}
                      title={t("removeUser")}
                    >
                      <Trash2 className="h-3.5 w-3.5" />
                    </Button>
                  </div>
                ) : null}
              </div>
            ))}
          </div>
        )}
      </div>

      {/* Create / Enroll User Dialog */}
      <Dialog open={addOpen} onOpenChange={setAddOpen}>
        <DialogContent className="max-sm:inset-0 max-sm:translate-x-0 max-sm:translate-y-0 sm:max-w-md">
          <DialogHeader>
            <DialogTitle>{addMode === "create" ? t("createUserTitle") : t("addUserTitle")}</DialogTitle>
            <DialogDescription className="sr-only">{addMode === "create" ? t("createUserTitle") : t("addUserTitle")}</DialogDescription>
          </DialogHeader>

          {addMode === "create" ? (
            <>
              <div className="space-y-4 py-2">
                <div className="space-y-1.5">
                  <Label htmlFor="tenant-user-email">{t("email")}</Label>
                  <Input
                    id="tenant-user-email"
                    type="email"
                    value={email}
                    onChange={(e) => setEmail(e.target.value)}
                    placeholder="user@example.com"
                    className="text-base md:text-sm"
                  />
                </div>
                <div className="space-y-1.5">
                  <Label htmlFor="tenant-user-name">{t("displayName")}</Label>
                  <Input
                    id="tenant-user-name"
                    value={displayName}
                    onChange={(e) => setDisplayName(e.target.value)}
                    placeholder={t("displayName")}
                    className="text-base md:text-sm"
                  />
                </div>
                <div className="space-y-1.5">
                  <Label htmlFor="tenant-user-phone">{t("phone")}</Label>
                  <Input
                    id="tenant-user-phone"
                    type="tel"
                    value={phone}
                    onChange={(e) => setPhone(e.target.value)}
                    placeholder="+1 234 567 890"
                    className="text-base md:text-sm"
                  />
                </div>
                <div className="space-y-1.5">
                  <Label htmlFor="tenant-user-password">{t("password")}</Label>
                  <Input
                    id="tenant-user-password"
                    type="password"
                    value={password}
                    onChange={(e) => setPassword(e.target.value)}
                    placeholder={t("password")}
                    className="text-base md:text-sm"
                  />
                  <p className="text-xs text-muted-foreground mt-1">{t("passwordHint")}</p>
                </div>
                <div className="space-y-1.5">
                  <Label>{t("selectRole")}</Label>
                  <Select value={createRole} onValueChange={setCreateRole}>
                    <SelectTrigger className="text-base md:text-sm"><SelectValue /></SelectTrigger>
                    <SelectContent>
                      {availableRoles.map((r) => (
                        <SelectItem key={r} value={r}>{t(ROLE_KEYS[r] ?? r)}</SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                </div>
              </div>
              <DialogFooter className="flex-col gap-2 sm:flex-row">
                <Button onClick={handleCreate} disabled={isCreating || !email.trim() || !displayName.trim() || !password.trim()}>
                  {isCreating ? "..." : t("createUser")}
                </Button>
                <button
                  type="button"
                  className="text-xs text-muted-foreground hover:text-foreground underline"
                  onClick={() => setAddMode("enroll")}
                >
                  {t("orEnrollExisting")}
                </button>
              </DialogFooter>
            </>
          ) : (
            <>
              <div className="space-y-4 py-2">
                <div className="space-y-1.5">
                  <Label>{t("userId")}</Label>
                  <UserPickerCombobox
                    value={enrollUserId}
                    onChange={setEnrollUserId}
                    placeholder="user-id"
                    source="tenant_user"
                    allowCustom={true}
                  />
                </div>
                <div className="space-y-1.5">
                  <Label>{t("selectRole")}</Label>
                  <Select value={enrollRole} onValueChange={setEnrollRole}>
                    <SelectTrigger className="text-base md:text-sm"><SelectValue /></SelectTrigger>
                    <SelectContent>
                      {availableRoles.map((r) => (
                        <SelectItem key={r} value={r}>{t(ROLE_KEYS[r] ?? r)}</SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                </div>
              </div>
              <DialogFooter className="flex-col gap-2 sm:flex-row">
                <Button onClick={handleEnroll} disabled={isEnrolling || !enrollUserId.trim()}>
                  {isEnrolling ? "..." : t("addUser")}
                </Button>
                <button
                  type="button"
                  className="text-xs text-muted-foreground hover:text-foreground underline"
                  onClick={() => setAddMode("create")}
                >
                  {t("createUser")}
                </button>
              </DialogFooter>
            </>
          )}
        </DialogContent>
      </Dialog>

      {/* Edit Name Dialog */}
      <Dialog open={editOpen} onOpenChange={setEditOpen}>
        <DialogContent className="max-sm:inset-0 max-sm:translate-x-0 max-sm:translate-y-0 sm:max-w-md">
          <DialogHeader>
            <DialogTitle>{t("editName")}</DialogTitle>
          </DialogHeader>
          <div className="space-y-4 py-2">
            <div className="space-y-1.5">
              <Label>{t("name")}</Label>
              <Input
                value={editName}
                onChange={(e) => setEditName(e.target.value)}
                placeholder={t("name")}
                className="text-base md:text-sm"
                onKeyDown={(e) => { if (e.key === "Enter") handleTenantNameSave(); }}
              />
            </div>
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => setEditOpen(false)} disabled={editSaving}>{tc("cancel")}</Button>
            <Button onClick={handleTenantNameSave} disabled={editSaving || !editName.trim()}>{tc("save")}</Button>
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

      {/* Delete Tenant Dialog */}
      <Dialog open={deleteOpen} onOpenChange={(o) => { if (!o) { setDeleteOpen(false); setDeleteConfirmName(""); } }}>
        <DialogContent className="max-sm:inset-0 max-sm:translate-x-0 max-sm:translate-y-0 sm:max-w-md">
          <DialogHeader>
            <DialogTitle>{t("deleteTenant")}</DialogTitle>
            <DialogDescription>{t("deleteConfirm")}</DialogDescription>
          </DialogHeader>
          <div className="space-y-4 py-2">
            <p className="text-sm text-muted-foreground">
              {t("typeToConfirm", { name: tenant?.name ?? "" })}
            </p>
            <Input
              value={deleteConfirmName}
              onChange={(e) => setDeleteConfirmName(e.target.value)}
              placeholder={tenant?.name ?? ""}
              className="text-base md:text-sm"
              autoFocus
            />
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => { setDeleteOpen(false); setDeleteConfirmName(""); }} disabled={deleteSaving}>
              {tc("cancel")}
            </Button>
            <Button
              variant="destructive"
              onClick={handleDeleteTenant}
              disabled={deleteSaving || deleteConfirmName.trim() !== tenant?.name}
            >
              {t("deleteTenant")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}

function InfoCard({ icon: Icon, label, value, mono, children }: {
  icon: React.ComponentType<{ className?: string }>;
  label: string;
  value?: string;
  mono?: boolean;
  children?: React.ReactNode;
}) {
  return (
    <div className="rounded-lg border p-3 flex items-start gap-3">
      <div className="flex h-8 w-8 shrink-0 items-center justify-center rounded-md bg-muted">
        <Icon className="h-4 w-4 text-muted-foreground" />
      </div>
      <div className="min-w-0">
        <p className="text-xs text-muted-foreground">{label}</p>
        {children ?? (
          <p className={`text-sm font-medium truncate ${mono ? "font-mono" : ""}`}>{value}</p>
        )}
      </div>
    </div>
  );
}
