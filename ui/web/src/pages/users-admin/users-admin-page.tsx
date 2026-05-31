import { useState, useEffect } from "react";
import { useTranslation } from "react-i18next";
import {
  Plus,
  RefreshCw,
  Users,
  ShieldCheck,
  ShieldOff,
  UserX,
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
import { useUsersAdmin } from "./hooks/use-users-admin";
import { formatRelativeTime } from "@/lib/format";
import type { User } from "@/types/user-mgmt";

function statusVariant(
  status: string,
): "default" | "secondary" | "destructive" {
  if (status === "active") return "default";
  if (status === "suspended") return "destructive";
  return "secondary";
}

function UsersAdminPage() {
  const { t } = useTranslation("users-admin");
  const { t: tc } = useTranslation("common");

  const [search, setSearch] = useState("");
  const [statusFilter, setStatusFilter] = useState("all");

  const {
    users,
    total,
    loading,
    refresh,
    changeStatus,
    deactivateUser,
    createUser,
    isCreating,
    toggleAdmin,
    isTogglingAdmin,
  } = useUsersAdmin({ search, status: statusFilter });

  const spinning = useMinLoading(loading);
  const showSkeleton = useDeferredLoading(loading && users.length === 0);

  const [createOpen, setCreateOpen] = useState(false);
  const [email, setEmail] = useState("");
  const [displayName, setDisplayName] = useState("");
  const [password, setPassword] = useState("");
  const [isAdmin, setIsAdmin] = useState(false);

  const [deactivateTarget, setDeactivateTarget] = useState<User | null>(null);
  const [deactivateLoading, setDeactivateLoading] = useState(false);

  const [actionLoading, setActionLoading] = useState<string | null>(null);

  const handleCreate = async () => {
    if (!email.trim() || !displayName.trim() || !password.trim()) return;
    try {
      await createUser({
        email: email.trim(),
        display_name: displayName.trim(),
        password: password.trim(),
        role: isAdmin ? "admin" : "member",
      });
      setCreateOpen(false);
      setEmail("");
      setDisplayName("");
      setPassword("");
      setIsAdmin(false);
    } catch {
      // error handled by mutation
    }
  };

  const handleStatusChange = async (user: User, newStatus: string) => {
    setActionLoading(user.id);
    try {
      await changeStatus({ userId: user.id, status: newStatus });
    } catch {
      // error handled by mutation
    } finally {
      setActionLoading(null);
    }
  };

  const handleDeactivate = async () => {
    if (!deactivateTarget) return;
    setDeactivateLoading(true);
    try {
      await deactivateUser(deactivateTarget.id);
      setDeactivateTarget(null);
    } catch {
      // error handled by mutation
    } finally {
      setDeactivateLoading(false);
    }
  };

  // Reset create dialog state when closed
  useEffect(() => {
    if (!createOpen) {
      setEmail("");
      setDisplayName("");
      setPassword("");
      setIsAdmin(false);
    }
  }, [createOpen]);

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
              <RefreshCw
                className={
                  spinning
                    ? "animate-spin h-3.5 w-3.5"
                    : "h-3.5 w-3.5"
                }
              />
              {tc("refresh")}
            </Button>
          </div>
        }
      />

      <div className="mt-4 flex flex-col gap-3 sm:flex-row sm:items-center">
        <SearchInput
          value={search}
          onChange={setSearch}
          placeholder={t("searchPlaceholder")}
          className="max-w-sm"
        />
        <Select value={statusFilter} onValueChange={setStatusFilter}>
          <SelectTrigger size="sm" className="w-[150px]">
            <SelectValue placeholder={t("filterStatus")} />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="all">{t("status.all")}</SelectItem>
            <SelectItem value="active">{t("status.active")}</SelectItem>
            <SelectItem value="suspended">{t("status.suspended")}</SelectItem>
          </SelectContent>
        </Select>
        {total > 0 && (
          <span className="text-sm text-muted-foreground">
            {tc("totalItems", { count: total })}
          </span>
        )}
      </div>

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
                  <th className="px-4 py-3 text-left font-medium">
                    {t("columns.displayName")}
                  </th>
                  <th className="px-4 py-3 text-left font-medium">
                    {t("columns.email")}
                  </th>
                  <th className="px-4 py-3 text-left font-medium">
                    {t("columns.provider")}
                  </th>
                  <th className="px-4 py-3 text-left font-medium">
                    {t("columns.status")}
                  </th>
                  <th className="px-4 py-3 text-left font-medium">
                    {t("columns.role")}
                  </th>
                  <th className="px-4 py-3 text-left font-medium">
                    {t("columns.lastLogin")}
                  </th>
                  <th className="px-4 py-3 text-right font-medium">
                    {t("columns.actions")}
                  </th>
                </tr>
              </thead>
              <tbody>
                {users.map((user) => (
                  <tr
                    key={user.id}
                    className="border-b last:border-0 hover:bg-muted/30"
                  >
                    <td className="px-4 py-3 font-medium">
                      {user.display_name}
                    </td>
                    <td className="px-4 py-3 text-muted-foreground">
                      {user.email}
                    </td>
                    <td className="px-4 py-3">
                      <Badge variant="outline" className="text-xs">
                        {user.auth_provider}
                      </Badge>
                    </td>
                    <td className="px-4 py-3">
                      <Badge variant={statusVariant(user.status)}>
                        {t(`status.${user.status}`)}
                      </Badge>
                    </td>
                    <td className="px-4 py-3">
                      <Badge variant="secondary" className="text-xs">
                        {t("role.member")}
                      </Badge>
                    </td>
                    <td className="px-4 py-3 text-muted-foreground">
                      {user.last_login_at
                        ? formatRelativeTime(user.last_login_at)
                        : t("never")}
                    </td>
                    <td className="px-4 py-3 text-right">
                      <div className="flex items-center justify-end gap-1">
                        {user.status === "active" && (
                          <Button
                            variant="ghost"
                            size="sm"
                            onClick={() =>
                              handleStatusChange(user, "suspended")
                            }
                            disabled={actionLoading === user.id}
                            title={t("actions.suspend")}
                          >
                            <ShieldOff className="h-3.5 w-3.5" />
                          </Button>
                        )}
                        {user.status === "suspended" && (
                          <Button
                            variant="ghost"
                            size="sm"
                            onClick={() =>
                              handleStatusChange(user, "active")
                            }
                            disabled={actionLoading === user.id}
                            title={t("actions.reactivate")}
                          >
                            <ShieldCheck className="h-3.5 w-3.5" />
                          </Button>
                        )}
                        <Button
                          variant="ghost"
                          size="sm"
                          onClick={() => toggleAdmin(user.id)}
                          disabled={isTogglingAdmin}
                          title={t("actions.toggleRole")}
                        >
                          <ShieldCheck className="h-3.5 w-3.5" />
                        </Button>
                        <Button
                          variant="ghost"
                          size="sm"
                          onClick={() => setDeactivateTarget(user)}
                          className="text-destructive hover:text-destructive"
                          title={t("actions.deactivate")}
                        >
                          <UserX className="h-3.5 w-3.5" />
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

      {/* Create user dialog */}
      <Dialog open={createOpen} onOpenChange={setCreateOpen}>
        <DialogContent className="max-sm:inset-0 max-sm:translate-x-0 max-sm:translate-y-0 sm:max-w-md">
          <DialogHeader>
            <DialogTitle>{t("addUser")}</DialogTitle>
            <DialogDescription className="sr-only">{t("addUser")}</DialogDescription>
          </DialogHeader>
          <div className="space-y-4 py-2">
            <div className="space-y-1.5">
              <Label htmlFor="user-email">{t("columns.email")}</Label>
              <Input
                id="user-email"
                type="email"
                value={email}
                onChange={(e) => setEmail(e.target.value)}
                placeholder="user@example.com"
                className="text-base md:text-sm"
              />
            </div>
            <div className="space-y-1.5">
              <Label htmlFor="user-display-name">
                {t("columns.displayName")}
              </Label>
              <Input
                id="user-display-name"
                value={displayName}
                onChange={(e) => setDisplayName(e.target.value)}
                placeholder={t("columns.displayName")}
                className="text-base md:text-sm"
              />
            </div>
            <div className="space-y-1.5">
              <Label htmlFor="user-password">{t("password")}</Label>
              <Input
                id="user-password"
                type="password"
                value={password}
                onChange={(e) => setPassword(e.target.value)}
                placeholder={t("passwordPlaceholder")}
                className="text-base md:text-sm"
              />
              <p className="text-xs text-muted-foreground mt-1">{t("passwordHint")}</p>
            </div>
            <div className="flex items-center gap-2">
              <input
                id="user-is-admin"
                type="checkbox"
                checked={isAdmin}
                onChange={(e) => setIsAdmin(e.target.checked)}
                className="h-4 w-4 rounded border-input"
              />
              <Label htmlFor="user-is-admin" className="text-sm font-normal cursor-pointer">
                {t("isAdmin")}
              </Label>
            </div>
          </div>
          <DialogFooter>
            <Button
              variant="outline"
              onClick={() => setCreateOpen(false)}
              disabled={isCreating}
            >
              {tc("cancel")}
            </Button>
            <Button
              onClick={handleCreate}
              disabled={
                isCreating ||
                !email.trim() ||
                !displayName.trim() ||
                !password.trim()
              }
            >
              {isCreating ? "..." : t("addUser")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Deactivate confirmation dialog */}
      <ConfirmDeleteDialog
        open={!!deactivateTarget}
        onOpenChange={(v) => !v && setDeactivateTarget(null)}
        title={t("deactivate.title")}
        description={t("deactivate.description", {
          name: deactivateTarget?.display_name ?? "",
        })}
        confirmValue={deactivateTarget?.display_name ?? ""}
        confirmLabel={t("actions.deactivate")}
        onConfirm={handleDeactivate}
        loading={deactivateLoading}
      />
    </div>
  );
}

export default UsersAdminPage;
