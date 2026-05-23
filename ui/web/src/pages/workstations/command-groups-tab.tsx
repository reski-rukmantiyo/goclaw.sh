import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Pencil, Trash2, Terminal } from "lucide-react";
import { EmptyState } from "@/components/shared/empty-state";
import { TableSkeleton } from "@/components/shared/loading-skeleton";
import { ConfirmDialog } from "@/components/shared/confirm-dialog";
import { useCommandGroups, type CommandGroup } from "./hooks/use-command-groups";

interface CommandGroupsTabProps {
  dialogOpen: boolean;
  onDialogOpenChange: (open: boolean) => void;
  editTarget: CommandGroup | null;
  onEditTargetChange: (group: CommandGroup | null) => void;
}

export function CommandGroupsTab({
  onDialogOpenChange,
  onEditTargetChange,
}: CommandGroupsTabProps) {
  const { t } = useTranslation("workstations");
  const { groups, loading, deleteGroup } = useCommandGroups();

  const [deleteTarget, setDeleteTarget] = useState<CommandGroup | null>(null);

  const isEmpty = groups.length === 0;

  return (
    <div className="space-y-4">
      {loading && isEmpty ? (
        <TableSkeleton rows={3} />
      ) : isEmpty ? (
        <EmptyState
          icon={Terminal}
          title={t("commandGroups.emptyTitle")}
          description={t("commandGroups.emptyDescription")}
        />
      ) : (
        <div className="rounded-md border overflow-x-auto">
          <table className="w-full min-w-[600px] text-sm">
            <thead>
              <tr className="border-b bg-muted/50">
                <th className="px-4 py-3 text-left font-medium">{t("commandGroups.columns.name")}</th>
                <th className="px-4 py-3 text-left font-medium">{t("commandGroups.columns.patterns")}</th>
                <th className="px-4 py-3 text-left font-medium">{t("commandGroups.columns.scope")}</th>
                <th className="px-4 py-3 text-right font-medium">{t("columns.actions")}</th>
              </tr>
            </thead>
            <tbody>
              {groups.map((g) => (
                <tr key={g.id} className="border-b last:border-0 hover:bg-muted/30">
                  <td className="px-4 py-3">
                    <div className="font-medium">{g.name}</div>
                    {g.description && (
                      <div className="text-xs text-muted-foreground">{g.description}</div>
                    )}
                  </td>
                  <td className="px-4 py-3">
                    <div className="flex flex-wrap gap-1">
                      {g.patterns?.slice(0, 5).map((p) => (
                        <code key={p} className="text-xs font-mono bg-muted px-1.5 py-0.5 rounded">{p}</code>
                      ))}
                      {(g.patterns?.length || 0) > 5 && (
                        <span className="text-xs text-muted-foreground">+{g.patterns.length - 5}</span>
                      )}
                    </div>
                  </td>
                  <td className="px-4 py-3">
                    <Badge variant={g.isBuiltin ? "secondary" : "outline"}>
                      {g.isBuiltin ? t("commandGroups.scope.builtin") : t("commandGroups.scope.custom")}
                    </Badge>
                  </td>
                  <td className="px-4 py-3 text-right">
                    {!g.isBuiltin && (
                      <>
                        <Button
                          variant="ghost"
                          size="sm"
                          onClick={() => { onEditTargetChange(g); onDialogOpenChange(true); }}
                          className="gap-1"
                        >
                          <Pencil className="h-3.5 w-3.5" />
                          {t("commandGroups.actions.edit")}
                        </Button>
                        <Button
                          variant="ghost"
                          size="sm"
                          onClick={() => setDeleteTarget(g)}
                          className="gap-1 text-destructive"
                        >
                          <Trash2 className="h-3.5 w-3.5" />
                          {t("commandGroups.actions.delete")}
                        </Button>
                      </>
                    )}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      {deleteTarget && (
        <ConfirmDialog
          open
          onOpenChange={() => setDeleteTarget(null)}
          title={t("commandGroups.deleteDialog.title")}
          description={t("commandGroups.deleteDialog.description", { name: deleteTarget.name })}
          confirmLabel={t("commandGroups.deleteDialog.confirmLabel")}
          variant="destructive"
          onConfirm={async () => {
            await deleteGroup(deleteTarget.id);
            setDeleteTarget(null);
          }}
        />
      )}
    </div>
  );
}
