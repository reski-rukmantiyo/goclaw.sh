import { useState } from "react";
import { useTranslation } from "react-i18next";
import { RefreshCw, Download, ChevronDown, ChevronRight, ShieldCheck } from "lucide-react";
import { Button } from "@/components/ui/button";
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
import { useMinLoading } from "@/hooks/use-min-loading";
import { useDeferredLoading } from "@/hooks/use-deferred-loading";
import { useAuditLog } from "./hooks/use-audit-log";

const PAGE_SIZE = 50;

const RESOURCE_TYPES = ["all", "user", "group", "membership", "permission", "system"] as const;

function formatTime(iso: string): string {
  return new Date(iso).toLocaleString(undefined, {
    year: "numeric",
    month: "short",
    day: "numeric",
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
  });
}

export function AuditLogPage() {
  const { t } = useTranslation("audit");
  const { t: tc } = useTranslation("common");

  const [actionFilter, setActionFilter] = useState("");
  const [resourceType, setResourceType] = useState<string>("all");
  const [offset, setOffset] = useState(0);
  const [expandedId, setExpandedId] = useState<string | null>(null);

  const params = {
    limit: PAGE_SIZE,
    offset,
    ...(actionFilter ? { action: actionFilter } : {}),
    ...(resourceType !== "all" ? { resource_type: resourceType } : {}),
  };

  const { entries, total, loading, refresh } = useAuditLog(params);
  const spinning = useMinLoading(loading);
  const showSkeleton = useDeferredLoading(loading && entries.length === 0);

  const totalPages = Math.max(1, Math.ceil(total / PAGE_SIZE));
  const currentPage = Math.floor(offset / PAGE_SIZE) + 1;

  const handlePageChange = (page: number) => {
    setOffset((page - 1) * PAGE_SIZE);
  };

  const handleFilterChange = () => {
    setOffset(0);
    setExpandedId(null);
  };

  return (
    <div className="p-4 sm:p-6 pb-10">
      <PageHeader
        title={t("title")}
        description={t("description")}
        actions={
          <div className="flex gap-2">
            <Button variant="outline" size="sm" asChild className="gap-1">
              <a href="/v1/audit/export" download>
                <Download className="h-3.5 w-3.5" /> {t("export")}
              </a>
            </Button>
            <Button variant="outline" size="sm" onClick={refresh} disabled={spinning} className="gap-1">
              <RefreshCw className={spinning ? "animate-spin h-3.5 w-3.5" : "h-3.5 w-3.5"} /> {tc("refresh")}
            </Button>
          </div>
        }
      />

      {/* Filters */}
      <div className="mt-4 flex flex-wrap items-center gap-3">
        <input
          type="text"
          value={actionFilter}
          onChange={(e) => { setActionFilter(e.target.value); handleFilterChange(); }}
          placeholder={t("filterAction")}
          className="h-9 max-w-xs rounded-md border border-input bg-background px-3 text-base md:text-sm placeholder:text-muted-foreground focus:outline-none focus:ring-2 focus:ring-ring"
        />
        <Select
          value={resourceType}
          onValueChange={(v) => { setResourceType(v); handleFilterChange(); }}
        >
          <SelectTrigger className="h-9 w-[180px]">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {RESOURCE_TYPES.map((type) => (
              <SelectItem key={type} value={type}>
                {type === "all" ? t("allTypes") : t(type)}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </div>

      {/* Table */}
      <div className="mt-4">
        {showSkeleton ? (
          <TableSkeleton rows={5} />
        ) : entries.length === 0 ? (
          <EmptyState icon={ShieldCheck} title={t("noEntries")} />
        ) : (
          <>
            <div className="overflow-x-auto rounded-md border">
              <table className="w-full min-w-[600px] text-sm">
                <thead>
                  <tr className="border-b bg-muted/50">
                    <th className="px-4 py-3 text-left font-medium">{t("time")}</th>
                    <th className="px-4 py-3 text-left font-medium">{t("actor")}</th>
                    <th className="px-4 py-3 text-left font-medium">{t("action")}</th>
                    <th className="px-4 py-3 text-left font-medium">{t("resourceType")}</th>
                    <th className="px-4 py-3 text-left font-medium">{t("resourceId")}</th>
                    <th className="px-4 py-3 text-left font-medium">{t("group")}</th>
                    <th className="px-4 py-3 text-left font-medium">{t("ipAddress")}</th>
                    <th className="px-4 py-3 text-left font-medium">{t("detail")}</th>
                  </tr>
                </thead>
                <tbody>
                  {entries.map((entry) => {
                    const isExpanded = expandedId === entry.id;
                    return (
                      <tr key={entry.id} className="border-b last:border-0 hover:bg-muted/30 align-top">
                        <td className="px-4 py-3 text-muted-foreground whitespace-nowrap" title={formatTime(entry.time)}>
                          {formatTime(entry.time)}
                        </td>
                        <td className="px-4 py-3 font-mono text-xs">{entry.actor}</td>
                        <td className="px-4 py-3">
                          <span className="inline-block rounded bg-muted px-2 py-0.5 text-xs font-mono">
                            {entry.action}
                          </span>
                        </td>
                        <td className="px-4 py-3 capitalize">{entry.resource_type}</td>
                        <td className="px-4 py-3 font-mono text-xs max-w-[200px] truncate" title={entry.resource_id}>
                          {entry.resource_id}
                        </td>
                        <td className="px-4 py-3">{entry.group || "—"}</td>
                        <td className="px-4 py-3 font-mono text-xs">{entry.ip_address || "—"}</td>
                        <td className="px-4 py-3">
                          {entry.detail ? (
                            <div>
                              <Button
                                variant="ghost"
                                size="sm"
                                onClick={() => setExpandedId(isExpanded ? null : entry.id)}
                                className="gap-1 text-xs"
                              >
                                {isExpanded ? <ChevronDown className="h-3 w-3" /> : <ChevronRight className="h-3 w-3" />}
                                {t("viewDetail")}
                              </Button>
                              {isExpanded && (
                                <pre className="mt-2 max-h-60 overflow-auto rounded bg-muted p-3 text-xs font-mono whitespace-pre-wrap break-all">
                                  {JSON.stringify(entry.detail, null, 2)}
                                </pre>
                              )}
                            </div>
                          ) : (
                            "—"
                          )}
                        </td>
                      </tr>
                    );
                  })}
                </tbody>
              </table>
            </div>

            {/* Server-side pagination */}
            {total > PAGE_SIZE && (
              <div className="flex items-center justify-between py-3">
                <p className="text-sm text-muted-foreground">
                  {offset + 1}–{Math.min(offset + PAGE_SIZE, total)} of {total}
                </p>
                <div className="flex items-center gap-2">
                  <Button
                    variant="outline"
                    size="sm"
                    disabled={currentPage <= 1}
                    onClick={() => handlePageChange(currentPage - 1)}
                  >
                    {tc("prev")}
                  </Button>
                  <span className="text-sm text-muted-foreground">
                    {currentPage} / {totalPages}
                  </span>
                  <Button
                    variant="outline"
                    size="sm"
                    disabled={currentPage >= totalPages}
                    onClick={() => handlePageChange(currentPage + 1)}
                  >
                    {tc("next")}
                  </Button>
                </div>
              </div>
            )}
          </>
        )}
      </div>
    </div>
  );
}

export default AuditLogPage;
