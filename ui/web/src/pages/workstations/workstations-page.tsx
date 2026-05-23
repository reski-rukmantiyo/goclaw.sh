import { useState, Fragment } from "react";
import { MonitorCog, Plus, RefreshCw, Trash2, Power, PowerOff, ChevronDown, ChevronRight, Pencil } from "lucide-react";
import { useTranslation } from "react-i18next";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { PageHeader } from "@/components/shared/page-header";
import { EmptyState } from "@/components/shared/empty-state";
import { TableSkeleton } from "@/components/shared/loading-skeleton";
import { ConfirmDialog } from "@/components/shared/confirm-dialog";
import { useMinLoading } from "@/hooks/use-min-loading";
import { useDeferredLoading } from "@/hooks/use-deferred-loading";
import { formatDate } from "@/lib/format";
import { useWorkstations, type Workstation } from "./hooks/use-workstations";
import { WorkstationCreateDialog } from "./workstation-create-dialog";
import { WorkstationActivityTab } from "./workstation-activity-tab";
import { WorkstationAgentsTab } from "./workstation-agents-tab";
import { WorkstationPermissionsTab } from "./workstation-permissions-tab";
import { WorkstationGlobalActivityTab } from "./workstation-global-activity-tab";
import { CommandGroupsTab } from "./command-groups-tab";
import { CommandGroupDialog } from "./command-group-dialog";
import { useCommandGroups, type CommandGroup } from "./hooks/use-command-groups";

export function WorkstationsPage() {
  const { t } = useTranslation("workstations");
  const { workstations, loading, refresh, getWorkstation, createWorkstation, updateWorkstation, deleteWorkstation, toggleWorkstation } = useWorkstations();
  const { groups, loading: cgLoading, createGroup, updateGroup, deleteGroup } = useCommandGroups();

  const spinning = useMinLoading(loading);
  const isEmpty = workstations.length === 0;
  const showSkeleton = useDeferredLoading(loading && isEmpty);

  const [activeTab, setActiveTab] = useState("workstations");
  const [createOpen, setCreateOpen] = useState(false);
  const [editTarget, setEditTarget] = useState<Workstation | null>(null);
  const [deleteTarget, setDeleteTarget] = useState<Workstation | null>(null);
  const [expandedId, setExpandedId] = useState<string | null>(null);

  // Command group dialog state (lifted to page level for tab-row button)
  const [cgDialogOpen, setCgDialogOpen] = useState(false);
  const [cgEditTarget, setCgEditTarget] = useState<CommandGroup | null>(null);

  function toggleExpand(id: string) {
    setExpandedId((prev) => (prev === id ? null : id));
  }

  return (
    <div className="p-4 sm:p-6 pb-10">
      <PageHeader
        title={t("title")}
        description={t("description")}
      />

      <Tabs value={activeTab} onValueChange={setActiveTab} className="mt-4">
        <div className="flex items-center justify-between mb-4">
          <TabsList>
            <TabsTrigger value="workstations">{t("tabs.workstations")}</TabsTrigger>
            <TabsTrigger value="commandGroups">{t("tabs.commandGroups")}</TabsTrigger>
            <TabsTrigger value="activity">{t("tabs.activity")}</TabsTrigger>
          </TabsList>

          {activeTab === "workstations" && (
            <div className="flex gap-2">
              <Button variant="outline" size="sm" onClick={refresh} disabled={spinning} className="gap-1">
                <RefreshCw className={"h-3.5 w-3.5" + (spinning ? " animate-spin" : "")} />
                {t("common:refresh", "Refresh")}
              </Button>
              <Button size="sm" onClick={() => setCreateOpen(true)} className="gap-1">
                <Plus className="h-3.5 w-3.5" />
                {t("addWorkstation")}
              </Button>
            </div>
          )}
          {activeTab === "commandGroups" && (
            <Button size="sm" onClick={() => { setCgEditTarget(null); setCgDialogOpen(true); }} className="gap-1">
              <Plus className="h-3.5 w-3.5" />
              {t("commandGroups.addGroup")}
            </Button>
          )}
        </div>

        <TabsContent value="workstations" className="space-y-4">
          {showSkeleton ? (
            <TableSkeleton rows={4} />
          ) : isEmpty ? (
            <EmptyState
              icon={MonitorCog}
              title={t("emptyTitle")}
              description={t("emptyDescription")}
            />
          ) : (
            <div className="rounded-md border overflow-x-auto">
              <table className="w-full min-w-[600px] text-sm">
                <thead>
                  <tr className="border-b bg-muted/50">
                    <th className="px-4 py-3 text-left font-medium w-8"></th>
                    <th className="px-4 py-3 text-left font-medium">{t("columns.name")}</th>
                    <th className="px-4 py-3 text-left font-medium">{t("columns.key")}</th>
                    <th className="px-4 py-3 text-left font-medium">{t("columns.backend")}</th>
                    <th className="px-4 py-3 text-left font-medium">{t("columns.status")}</th>
                    <th className="px-4 py-3 text-left font-medium">{t("columns.created")}</th>
                    <th className="px-4 py-3 text-right font-medium">{t("columns.actions")}</th>
                  </tr>
                </thead>
                <tbody>
                  {workstations.map((ws) => {
                    const isExpanded = expandedId === ws.id;
                    return (
                      <Fragment key={ws.id}>
                        <tr
                          className="border-b last:border-0 hover:bg-muted/30 cursor-pointer"
                          onClick={() => toggleExpand(ws.id)}
                        >
                          <td className="px-4 py-3 text-muted-foreground">
                            {isExpanded ? (
                              <ChevronDown className="h-4 w-4" />
                            ) : (
                              <ChevronRight className="h-4 w-4" />
                            )}
                          </td>
                          <td className="px-4 py-3 font-medium">{ws.name}</td>
                          <td className="px-4 py-3 font-mono text-xs text-muted-foreground">{ws.workstationKey}</td>
                          <td className="px-4 py-3">
                            <Badge variant="outline">{t(`backend.${ws.backendType}`)}</Badge>
                          </td>
                          <td className="px-4 py-3">
                            <Badge variant={ws.active ? "default" : "secondary"}>
                              {ws.active ? t("status.active") : t("status.inactive")}
                            </Badge>
                          </td>
                          <td className="px-4 py-3 text-muted-foreground">
                            {formatDate(new Date(ws.createdAt))}
                          </td>
                          <td className="px-4 py-3 text-right" onClick={(e) => e.stopPropagation()}>
                            <Button
                              variant="ghost"
                              size="sm"
                              onClick={() => toggleWorkstation(ws.id, !ws.active)}
                              className="gap-1"
                            >
                              {ws.active ? (
                                <>
                                  <PowerOff className="h-3.5 w-3.5" />
                                  {t("actions.deactivate")}
                                </>
                              ) : (
                                <>
                                  <Power className="h-3.5 w-3.5" />
                                  {t("actions.activate")}
                                </>
                              )}
                            </Button>
                            <Button
                              variant="ghost"
                              size="sm"
                              onClick={() => setEditTarget(ws)}
                              className="gap-1"
                            >
                              <Pencil className="h-3.5 w-3.5" />
                              {t("actions.edit")}
                            </Button>
                            <Button
                              variant="ghost"
                              size="sm"
                              onClick={() => setDeleteTarget(ws)}
                              className="gap-1"
                            >
                              <Trash2 className="h-3.5 w-3.5" />
                              {t("actions.delete")}
                            </Button>
                          </td>
                        </tr>
                        {isExpanded && (
                          <tr key={`${ws.id}-detail`} className="bg-muted/10">
                            <td colSpan={7} className="px-4 py-4">
                              <Tabs defaultValue="activity">
                                <TabsList className="mb-3">
                                  <TabsTrigger value="activity">{t("activity.title")}</TabsTrigger>
                                  <TabsTrigger value="agents">{t("agents.title", "Linked Agents")}</TabsTrigger>
                                  <TabsTrigger value="permissions">{t("permissions.title", "Permissions")}</TabsTrigger>
                                </TabsList>
                                <TabsContent value="activity">
                                  <WorkstationActivityTab workstationId={ws.id} />
                                </TabsContent>
                                <TabsContent value="agents">
                                  <WorkstationAgentsTab workstationId={ws.id} />
                                </TabsContent>
                                <TabsContent value="permissions">
                                  <WorkstationPermissionsTab workstationId={ws.id} />
                                </TabsContent>
                              </Tabs>
                            </td>
                          </tr>
                        )}
                      </Fragment>
                    );
                  })}
                </tbody>
              </table>
            </div>
          )}

          <WorkstationCreateDialog
            open={createOpen || !!editTarget}
            onOpenChange={(v) => {
              if (!v) {
                setCreateOpen(false);
                setEditTarget(null);
              } else {
                setCreateOpen(v);
              }
            }}
            onCreate={async (params) => {
              await createWorkstation(params);
            }}
            onUpdate={async (id, params) => {
              await updateWorkstation(id, params);
            }}
            getWorkstation={getWorkstation}
            editWorkstation={editTarget}
          />

          {deleteTarget && (
            <ConfirmDialog
              open
              onOpenChange={() => setDeleteTarget(null)}
              title={t("deleteDialog.title")}
              description={t("deleteDialog.description", { name: deleteTarget.name })}
              confirmLabel={t("deleteDialog.confirmLabel")}
              variant="destructive"
              onConfirm={async () => {
                await deleteWorkstation(deleteTarget.id);
                setDeleteTarget(null);
              }}
            />
          )}
        </TabsContent>

        <TabsContent value="commandGroups">
          <CommandGroupsTab
            groups={groups}
            loading={cgLoading}
            deleteGroup={deleteGroup}
            onDialogOpenChange={setCgDialogOpen}
            onEditTargetChange={setCgEditTarget}
          />
        </TabsContent>

        <TabsContent value="activity">
          <WorkstationGlobalActivityTab />
        </TabsContent>
      </Tabs>

      <CommandGroupDialog
        open={cgDialogOpen}
        onOpenChange={(v) => { if (!v) setCgEditTarget(null); setCgDialogOpen(v); }}
        onSubmit={async (params) => {
          if (cgEditTarget) {
            await updateGroup(cgEditTarget.id, params);
          } else {
            await createGroup(params);
          }
        }}
        group={cgEditTarget}
      />
    </div>
  );
}
