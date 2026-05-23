import { useState, useEffect, useCallback } from "react";
import { useTranslation } from "react-i18next";
import { RefreshCw, CheckCircle, XCircle, ShieldOff } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Skeleton } from "@/components/ui/skeleton";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { formatDate } from "@/lib/format";
import { useAgents } from "@/pages/agents/hooks/use-agents";
import {
  useWorkstationActivity,
  type WorkstationActivity,
} from "./hooks/use-workstation-activity";
import { useWorkstations } from "./hooks/use-workstations";

function ActionBadge({ action }: { action: WorkstationActivity["action"] }) {
  const { t } = useTranslation("workstations");
  if (action === "deny") {
    return (
      <Badge variant="destructive" className="gap-1 text-xs">
        <ShieldOff className="h-3 w-3" />
        {t("activity.actions.deny")}
      </Badge>
    );
  }
  return (
    <Badge variant="secondary" className="gap-1 text-xs">
      {t("activity.actions.exec")}
    </Badge>
  );
}

function ExitCodeCell({ exitCode }: { exitCode: number | null }) {
  if (exitCode === null) return <span className="text-muted-foreground">—</span>;
  const ok = exitCode === 0;
  return (
    <span className="flex items-center gap-1">
      {ok ? (
        <CheckCircle className="h-3.5 w-3.5 text-green-600 dark:text-green-400" />
      ) : (
        <XCircle className="h-3.5 w-3.5 text-red-600 dark:text-red-400" />
      )}
      <span className={ok ? "text-green-700 dark:text-green-400" : "text-red-700 dark:text-red-400"}>
        {exitCode}
      </span>
    </span>
  );
}

function formatDuration(ms: number | null): string {
  if (ms === null) return "—";
  if (ms < 1000) return `${ms}ms`;
  return `${(ms / 1000).toFixed(1)}s`;
}

export function WorkstationGlobalActivityTab() {
  const { t } = useTranslation("workstations");
  const { rows, loading, error, hasMore, load, loadMore } = useWorkstationActivity();
  const { workstations } = useWorkstations();
  const { agents } = useAgents();

  const [workstationFilter, setWorkstationFilter] = useState<string>("__all__");
  const [agentFilter, setAgentFilter] = useState<string>("__all__");

  const applyFilters = useCallback(() => {
    const filters: { workstationId?: string; agentId?: string } = {};
    if (workstationFilter !== "__all__") {
      filters.workstationId = workstationFilter;
    }
    if (agentFilter !== "__all__") {
      filters.agentId = agentFilter;
    }
    load(filters);
  }, [workstationFilter, agentFilter, load]);

  useEffect(() => {
    applyFilters();
  }, [applyFilters]);

  const resolveWorkstationName = (wsId: string) => {
    const ws = workstations.find((w) => w.id === wsId);
    return ws?.name || ws?.workstationKey || wsId.slice(0, 8);
  };

  if (loading && rows.length === 0) {
    return (
      <div className="space-y-2 p-4">
        {Array.from({ length: 5 }).map((_, i) => (
          <Skeleton key={i} className="h-10 w-full" />
        ))}
      </div>
    );
  }

  if (error) {
    return (
      <div className="flex flex-col items-center gap-2 p-8 text-center">
        <p className="text-sm text-destructive">{error}</p>
        <Button variant="outline" size="sm" onClick={applyFilters}>
          {t("common:retry", "Retry")}
        </Button>
      </div>
    );
  }

  return (
    <div className="space-y-4 p-4">
      {/* Filters */}
      <div className="flex flex-wrap items-end gap-3">
        <div className="flex flex-col gap-1.5">
          <label className="text-xs font-medium text-muted-foreground">
            {t("activity.filters.workstation", "Workstation")}
          </label>
          <Select value={workstationFilter} onValueChange={setWorkstationFilter}>
            <SelectTrigger className="h-9 w-52 text-xs">
              <SelectValue placeholder={t("activity.filters.allWorkstations", "All Workstations")} />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="__all__">{t("activity.filters.allWorkstations", "All Workstations")}</SelectItem>
              {workstations.map((ws) => (
                <SelectItem key={ws.id} value={ws.id}>
                  {ws.name || ws.workstationKey}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>

        <div className="flex flex-col gap-1.5">
          <label className="text-xs font-medium text-muted-foreground">
            {t("activity.filters.agent", "Agent")}
          </label>
          <Select value={agentFilter} onValueChange={setAgentFilter}>
            <SelectTrigger className="h-9 w-52 text-xs">
              <SelectValue placeholder={t("activity.filters.allAgents", "All Agents")} />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="__all__">{t("activity.filters.allAgents", "All Agents")}</SelectItem>
              {agents.map((a) => (
                <SelectItem key={a.id} value={a.id}>
                  {a.display_name || a.agent_key || a.id.slice(0, 8)}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>

        <Button
          variant="outline"
          size="sm"
          className="h-9 gap-1 text-xs"
          onClick={applyFilters}
          disabled={loading}
        >
          <RefreshCw className={"h-3 w-3" + (loading ? " animate-spin" : "")} />
          {t("common:refresh", "Refresh")}
        </Button>
      </div>

      {rows.length === 0 ? (
        <div className="flex flex-col items-center gap-2 p-12 text-center">
          <p className="font-medium text-muted-foreground">{t("activity.emptyTitle")}</p>
          <p className="text-sm text-muted-foreground">{t("activity.emptyDescription")}</p>
        </div>
      ) : (
        <>
          <div className="overflow-x-auto rounded-md border">
            <table className="min-w-[700px] w-full text-sm">
              <thead className="border-b bg-muted/50">
                <tr>
                  <th className="px-3 py-2 text-left font-medium text-muted-foreground">
                    {t("activity.columns.action")}
                  </th>
                  <th className="px-3 py-2 text-left font-medium text-muted-foreground">
                    {t("activity.columns.workstation", "Workstation")}
                  </th>
                  <th className="px-3 py-2 text-left font-medium text-muted-foreground">
                    {t("activity.columns.agent", "Agent")}
                  </th>
                  <th className="px-3 py-2 text-left font-medium text-muted-foreground">
                    {t("activity.columns.cmdPreview")}
                  </th>
                  <th className="px-3 py-2 text-left font-medium text-muted-foreground">
                    {t("activity.columns.exitCode")}
                  </th>
                  <th className="px-3 py-2 text-left font-medium text-muted-foreground">
                    {t("activity.columns.duration")}
                  </th>
                  <th className="px-3 py-2 text-left font-medium text-muted-foreground">
                    {t("activity.columns.timestamp")}
                  </th>
                </tr>
              </thead>
              <tbody className="divide-y">
                {rows.map((row) => (
                  <tr key={row.id} className="hover:bg-muted/30 transition-colors">
                    <td className="px-3 py-2">
                      <ActionBadge action={row.action} />
                    </td>
                    <td className="px-3 py-2 text-xs text-muted-foreground">
                      {resolveWorkstationName(row.workstationId)}
                    </td>
                    <td className="px-3 py-2 text-xs text-muted-foreground">
                      {row.agentId || <span className="italic">—</span>}
                    </td>
                    <td className="px-3 py-2 font-mono text-xs text-muted-foreground max-w-[240px] truncate">
                      {row.cmdPreview || <span className="italic">—</span>}
                    </td>
                    <td className="px-3 py-2">
                      <ExitCodeCell exitCode={row.exitCode} />
                    </td>
                    <td className="px-3 py-2 text-muted-foreground">
                      {formatDuration(row.durationMs)}
                    </td>
                    <td className="px-3 py-2 text-muted-foreground whitespace-nowrap">
                      {formatDate(row.createdAt)}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>

          {hasMore && (
            <div className="flex justify-center pt-2">
              <Button variant="outline" size="sm" onClick={loadMore} disabled={loading}>
                {loading ? (
                  <RefreshCw className="h-3.5 w-3.5 animate-spin" />
                ) : (
                  t("activity.loadMore")
                )}
              </Button>
            </div>
          )}
        </>
      )}
    </div>
  );
}
