import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Dialog, DialogContent, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Copy, Check, RotateCcw, Pencil } from "lucide-react";
import { formatDate } from "@/lib/format";
import { useClipboard } from "@/hooks/use-clipboard";
import type { RawMessage } from "./hooks/use-raw-messages";
import { scopeEditCanSave } from "./scope-helpers";

export interface ScopeAgentOption {
  id: string;
  display_name?: string;
  agent_key?: string;
}

interface RawMessageDetailDialogProps {
  message: RawMessage;
  agents: ScopeAgentOption[];
  onClose: () => void;
  onReset?: () => void;
  /** Persist a scope change. Only changed fields are non-undefined. Resolves/closes on success. */
  onSaveScope?: (agentId: string | undefined, graphId: string | undefined) => Promise<void> | void;
}

export function RawMessageDetailDialog({ message, agents, onClose, onReset, onSaveScope }: RawMessageDetailDialogProps) {
  const { t } = useTranslation("raw-messages");
  const { copied, copy } = useClipboard();

  const [editing, setEditing] = useState(false);
  const [editAgent, setEditAgent] = useState(message.agent_id);
  const [editGraph, setEditGraph] = useState(message.graph_id);
  const [saving, setSaving] = useState(false);

  const graphTrim = editGraph.trim();
  const agentChanged = editAgent !== message.agent_id;
  const graphChanged = graphTrim !== message.graph_id;
  const canSave = scopeEditCanSave({
    hasCallback: !!onSaveScope,
    currentAgent: message.agent_id,
    currentGraph: message.graph_id,
    editAgent,
    editGraph,
    saving,
  });

  const handleSave = async () => {
    if (!onSaveScope || !canSave) return;
    setSaving(true);
    try {
      await onSaveScope(agentChanged ? editAgent : undefined, graphChanged ? graphTrim : undefined);
    } finally {
      setSaving(false);
    }
  };

  const handleCancel = () => {
    setEditAgent(message.agent_id);
    setEditGraph(message.graph_id);
    setEditing(false);
  };

  const fields: { label: string; value: React.ReactNode }[] = [
    { label: t("detail.agent"), value: <>{message.agent_name && <span className="font-medium">{message.agent_name}</span>}{message.agent_name ? " " : ""}<code className="text-xs text-muted-foreground">{message.agent_id}</code></> },
    { label: t("detail.group"), value: <>{message.chat_name && <span className="font-medium">{message.chat_name}</span>}{message.chat_name ? " " : ""}<code className="text-xs text-muted-foreground">{message.chat_id}</code></> },
    { label: t("detail.sender"), value: message.sender },
    { label: t("detail.senderId"), value: <code className="text-xs">{message.sender_id}</code> },
    { label: t("detail.graphId"), value: <code className="text-xs">{message.graph_id}</code> },
    { label: t("detail.channel"), value: message.channel_name },
    {
      label: t("detail.status"),
      value: message.processed_at
        ? <Badge variant="success" className="text-xs">{t("status.processed")}</Badge>
        : <Badge variant="secondary" className="text-xs">{t("status.pending")}</Badge>,
    },
    { label: t("detail.messageTime"), value: formatDate(message.msg_timestamp) },
    { label: t("detail.createdAt"), value: formatDate(message.created_at) },
    {
      label: t("detail.processedAt"),
      value: message.processed_at ? formatDate(message.processed_at) : t("detail.na"),
    },
  ];

  return (
    <Dialog open onOpenChange={() => onClose()}>
      <DialogContent className="max-h-[85vh] flex flex-col sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle>{t("detail.title")}</DialogTitle>
        </DialogHeader>

        <div className="overflow-y-auto min-h-0 -mx-4 px-4 sm:-mx-6 sm:px-6">
          {/* Metadata grid */}
          <div className="grid grid-cols-1 sm:grid-cols-2 gap-x-6 gap-y-3 text-sm">
            {fields.map((f) => (
              <div key={f.label} className="min-w-0">
                <div className="text-xs text-muted-foreground">{f.label}</div>
                <div className="mt-0.5 truncate">{f.value}</div>
              </div>
            ))}
          </div>

          {/* Edit scope (agent + graph) */}
          {onSaveScope && (
            <div className="mt-4 border-t pt-4">
              <div className="mb-2 flex items-center justify-between">
                <span className="text-xs font-medium text-muted-foreground">
                  {t("actions.scopePromptTitle")}
                </span>
                {!editing && (
                  <Button
                    variant="ghost"
                    size="sm"
                    className="h-7 gap-1 text-xs"
                    onClick={() => setEditing(true)}
                  >
                    <Pencil className="h-3 w-3" />
                    {t("detail.editScope")}
                  </Button>
                )}
              </div>
              {editing ? (
                <div className="space-y-3">
                  <p className="text-xs text-muted-foreground">{t("detail.scopeHint")}</p>
                  <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
                    <div className="min-w-0">
                      <label className="mb-1 block text-xs text-muted-foreground">
                        {t("actions.scopeAgentLabel")}
                      </label>
                      <Select value={editAgent} onValueChange={setEditAgent}>
                        <SelectTrigger className="h-8 text-sm">
                          <SelectValue placeholder={t("actions.scopeAgentLabel")} />
                        </SelectTrigger>
                        <SelectContent>
                          {agents.map((a) => (
                            <SelectItem key={a.id} value={a.id}>
                              {a.display_name || a.agent_key || a.id.slice(0, 8)}
                            </SelectItem>
                          ))}
                        </SelectContent>
                      </Select>
                    </div>
                    <div className="min-w-0">
                      <label className="mb-1 block text-xs text-muted-foreground">
                        {t("actions.scopeGraphLabel")}
                      </label>
                      <input
                        type="text"
                        value={editGraph}
                        onChange={(e) => setEditGraph(e.target.value)}
                        className="h-8 w-full rounded-md border bg-background px-2 text-sm text-base md:text-sm placeholder:text-muted-foreground"
                      />
                    </div>
                  </div>
                  <div className="flex items-center gap-2">
                    <Button size="sm" className="h-7 text-xs" onClick={handleSave} disabled={!canSave}>
                      {t("detail.saveScope")}
                    </Button>
                    <Button size="sm" variant="ghost" className="h-7 text-xs" onClick={handleCancel} disabled={saving}>
                      {t("detail.cancel")}
                    </Button>
                  </div>
                </div>
              ) : null}
            </div>
          )}

          {/* Full message body */}
          <div className="mt-4 border-t pt-4">
            <div className="mb-2 flex items-center justify-between">
              <span className="text-xs font-medium text-muted-foreground">{t("detail.messageBody")}</span>
              <div className="flex items-center gap-1">
                {onReset && (
                  <Button
                    variant="outline"
                    size="sm"
                    className="h-7 gap-1 text-xs"
                    onClick={onReset}
                  >
                    <RotateCcw className="h-3 w-3" />
                    {t("detail.resetToPending")}
                  </Button>
                )}
                <Button
                  variant="ghost"
                  size="sm"
                  className="h-7 gap-1 text-xs"
                  onClick={() => copy(message.body)}
                >
                  {copied ? <Check className="h-3 w-3" /> : <Copy className="h-3 w-3" />}
                  {copied ? t("detail.copied") : t("detail.copy")}
                </Button>
              </div>
            </div>
            <div className="rounded-md bg-muted/50 p-3 text-sm whitespace-pre-wrap break-words max-h-[40vh] overflow-y-auto">
              {message.body || t("detail.na")}
            </div>
          </div>
        </div>
      </DialogContent>
    </Dialog>
  );
}
