import { useState, lazy, Suspense } from "react";
import { useParams, useNavigate } from "react-router";
import { useTranslation } from "react-i18next";
import { ArrowLeft, Trash2, Calendar, Hash, Shield, Pencil, Users, ShieldCheck } from "lucide-react";
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
import { PageHeader } from "@/components/shared/page-header";
import { TableSkeleton } from "@/components/shared/loading-skeleton";
import { Tabs, TabsList, TabsTrigger, TabsContent } from "@/components/ui/tabs";
import { useTenantDetail } from "./hooks/use-tenant-detail";
import { ROUTES, route } from "@/lib/constants";
import { useTenants } from "@/hooks/use-tenants";

const TenantUsersTab = lazy(() =>
  import("./tabs/tenant-users-tab").then((m) => ({ default: m.TenantUsersTab })),
);
const TenantRolesTab = lazy(() =>
  import("./tabs/tenant-roles-tab").then((m) => ({ default: m.TenantRolesTab })),
);
const TenantGroupsTab = lazy(() =>
  import("./tabs/tenant-groups-tab").then((m) => ({ default: m.TenantGroupsTab })),
);

export function TenantDetailPage() {
  const { id = "" } = useParams<{ id: string }>();
  const navigate = useNavigate();
  const { t } = useTranslation("tenants");
  const { t: tc } = useTranslation("common");

  const { isOwner, currentTenantSlug } = useTenants();

  const {
    tenant, tenantLoading,
    updateTenantName, deleteTenant,
  } = useTenantDetail(id);

  // --- Edit tenant name state ---
  const [editOpen, setEditOpen] = useState(false);
  const [editName, setEditName] = useState("");
  const [editSaving, setEditSaving] = useState(false);

  // --- Delete tenant state ---
  const [deleteOpen, setDeleteOpen] = useState(false);
  const [deleteConfirmName, setDeleteConfirmName] = useState("");
  const [deleteSaving, setDeleteSaving] = useState(false);

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
            {tenant && isOwner && (
              <Button
                variant="outline"
                size="sm"
                className="gap-1"
                onClick={() => { setEditName(tenant.name); setEditOpen(true); }}
              >
                <Pencil className="h-3.5 w-3.5" /> {t("editName")}
              </Button>
            )}
            {tenant && isOwner && (
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

      {/* Tenant UI — Users / Roles / Groups tabs */}
      <Tabs defaultValue="users">
        <TabsList>
          <TabsTrigger value="users" className="gap-1">
            <Users className="h-4 w-4" /> {t("tabs.users")}
          </TabsTrigger>
          <TabsTrigger value="roles" className="gap-1">
            <ShieldCheck className="h-4 w-4" /> {t("tabs.roles")}
          </TabsTrigger>
          {/* Groups tab hidden — functionality not yet ready for general use */}
        </TabsList>
        <TabsContent value="users">
          <Suspense fallback={<TableSkeleton rows={4} />}>
            <TenantUsersTab tenantId={id} isOwner={isOwner} />
          </Suspense>
        </TabsContent>
        <TabsContent value="roles">
          <Suspense fallback={<TableSkeleton rows={4} />}>
            <TenantRolesTab />
          </Suspense>
        </TabsContent>
        {false && (
          <TabsContent value="groups">
            <Suspense fallback={<TableSkeleton rows={4} />}>
              <TenantGroupsTab />
            </Suspense>
          </TabsContent>
        )}
      </Tabs>

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
