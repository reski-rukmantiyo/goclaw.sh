import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Switch } from "@/components/ui/switch";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Plus, Trash2, ChevronDown, ChevronRight } from "lucide-react";
import type { AgentData } from "@/types/agent";

export interface WhatsAppGroupJoinRuleValues {
  name?: string;
  added_by: string;
  policy?: "open" | "pairing";
  agent_id?: string;
  listen_only?: boolean;
  listen_graph_id?: string;
  require_mention?: boolean;
}

interface Props {
  rules: WhatsAppGroupJoinRuleValues[];
  onChange: (rules: WhatsAppGroupJoinRuleValues[]) => void;
  agents: AgentData[];
}

export function WhatsAppGroupJoinRules({ rules, onChange, agents }: Props) {
  const { t } = useTranslation("channels");
  const [expanded, setExpanded] = useState<Record<number, boolean>>({});

  const addRule = () => {
    onChange([...rules, { added_by: "" }]);
    setExpanded((prev) => ({ ...prev, [rules.length]: true }));
  };

  const removeRule = (idx: number) => {
    const next = [...rules];
    next.splice(idx, 1);
    onChange(next);
  };

  const updateRule = (idx: number, rule: WhatsAppGroupJoinRuleValues) => {
    const next = [...rules];
    next[idx] = rule;
    onChange(next);
  };

  const toggle = (idx: number) => {
    setExpanded((prev) => ({ ...prev, [idx]: !prev[idx] }));
  };

  return (
    <fieldset className="rounded-md border p-3 space-y-3">
      <legend className="px-1 text-sm font-medium">
        {t("whatsappGroupJoinRules.title")}
      </legend>
      <p className="text-xs text-muted-foreground">
        {t("whatsappGroupJoinRules.hint")}
      </p>

      {rules.map((rule, idx) => (
        <div key={idx} className="rounded-md border p-3 space-y-3">
          <div className="flex items-center justify-between">
            <button
              type="button"
              className="flex items-center gap-1 text-sm font-medium hover:underline"
              onClick={() => toggle(idx)}
            >
              {expanded[idx] ? (
                <ChevronDown className="h-4 w-4" />
              ) : (
                <ChevronRight className="h-4 w-4" />
              )}
              <span className="text-sm">
                {rule.name || rule.added_by || `Rule ${idx + 1}`}
              </span>
            </button>
            <Button
              type="button"
              variant="ghost"
              size="sm"
              className="h-7 w-7 p-0 text-muted-foreground hover:text-destructive"
              onClick={() => removeRule(idx)}
            >
              <Trash2 className="h-4 w-4" />
            </Button>
          </div>

          {expanded[idx] && (
            <div className="space-y-3 pl-2">
              {/* Rule name */}
              <div className="space-y-1.5">
                <label className="text-xs font-medium text-muted-foreground">
                  {t("whatsappGroupJoinRules.ruleNameLabel")}
                </label>
                <Input
                  value={rule.name ?? ""}
                  onChange={(e) =>
                    updateRule(idx, { ...rule, name: e.target.value || undefined })
                  }
                  placeholder={t("whatsappGroupJoinRules.ruleNamePlaceholder")}
                  className="h-9 text-sm"
                />
              </div>
              {/* Added By (required) */}
              <div className="space-y-1.5">
                <label className="text-xs font-medium text-muted-foreground">
                  {t("whatsappGroupJoinRules.addedByLabel")} *
                </label>
                <Input
                  value={rule.added_by ?? ""}
                  onChange={(e) =>
                    updateRule(idx, { ...rule, added_by: e.target.value })
                  }
                  placeholder={t("whatsappGroupJoinRules.addedByPlaceholder")}
                  className="h-9 text-sm"
                  required
                />
                <p className="text-xs text-muted-foreground/70">
                  {t("whatsappGroupJoinRules.addedByHint")}
                </p>
              </div>
              {/* Policy */}
              <div className="space-y-1.5">
                <label className="text-xs font-medium text-muted-foreground">
                  {t("whatsappGroupJoinRules.policyLabel")}
                </label>
                <Select
                  value={rule.policy ?? "open"}
                  onValueChange={(val) =>
                    updateRule(idx, {
                      ...rule,
                      policy: val as "open" | "pairing",
                    })
                  }
                >
                  <SelectTrigger className="h-9">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="open">
                      {t("whatsappGroupJoinRules.policyOpen")}
                    </SelectItem>
                    <SelectItem value="pairing">
                      {t("whatsappGroupJoinRules.policyPairing")}
                    </SelectItem>
                  </SelectContent>
                </Select>
              </div>
              {/* Agent selector */}
              <div className="space-y-1.5">
                <label className="text-xs font-medium text-muted-foreground">
                  {t("whatsappGroupJoinRules.agentLabel")}
                </label>
                <Select
                  value={rule.agent_id ?? ""}
                  onValueChange={(val) =>
                    updateRule(idx, {
                      ...rule,
                      agent_id:
                        val === "__default__" || !val ? undefined : val,
                    })
                  }
                >
                  <SelectTrigger className="h-9">
                    <SelectValue
                      placeholder={t("whatsappGroupJoinRules.selectAgent")}
                    />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="__default__">
                      {t("whatsappGroupJoinRules.selectAgent")}
                    </SelectItem>
                    {agents.map((a) => (
                      <SelectItem key={a.id} value={a.agent_key}>
                        {a.display_name || a.agent_key}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>
              {/* Require @mention override */}
              <div className="space-y-1.5">
                <label className="text-xs font-medium text-muted-foreground">
                  {t("whatsappGroupJoinRules.requireMentionLabel")}
                </label>
                <Select
                  value={
                    rule.require_mention === undefined || rule.require_mention === null
                      ? "__inherit__"
                      : rule.require_mention
                        ? "true"
                        : "false"
                  }
                  onValueChange={(val) =>
                    updateRule(idx, {
                      ...rule,
                      require_mention:
                        val === "__inherit__" ? undefined : val === "true",
                    })
                  }
                >
                  <SelectTrigger className="h-9">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="__inherit__">
                      {t("whatsappGroupJoinRules.requireMentionInherit")}
                    </SelectItem>
                    <SelectItem value="true">
                      {t("whatsappGroupJoinRules.requireMentionYes")}
                    </SelectItem>
                    <SelectItem value="false">
                      {t("whatsappGroupJoinRules.requireMentionNo")}
                    </SelectItem>
                  </SelectContent>
                </Select>
              </div>
              {/* Listen-only toggle */}
              <div className="flex items-center justify-between gap-2">
                <div className="space-y-0.5">
                  <label className="text-xs font-medium text-muted-foreground">
                    {t("whatsappGroupJoinRules.listenOnlyLabel")}
                  </label>
                  <p className="text-xs text-muted-foreground/70">
                    {t("whatsappGroupJoinRules.listenOnlyHint")}
                  </p>
                </div>
                <Switch
                  checked={rule.listen_only ?? false}
                  onCheckedChange={(val) =>
                    updateRule(idx, { ...rule, listen_only: val || undefined })
                  }
                />
              </div>
              {/* Graph ID (shown when listen-only is enabled) */}
              {rule.listen_only && (
                <div className="space-y-1.5">
                  <label className="text-xs font-medium text-muted-foreground">
                    {t("whatsappGroupJoinRules.graphIdLabel")}
                  </label>
                  <Input
                    value={rule.listen_graph_id ?? ""}
                    onChange={(e) =>
                      updateRule(idx, {
                        ...rule,
                        listen_graph_id: e.target.value || undefined,
                      })
                    }
                    placeholder={t("whatsappGroupJoinRules.graphIdPlaceholder")}
                    className="h-9 text-sm"
                  />
                  <p className="text-xs text-muted-foreground/70">
                    {t("whatsappGroupJoinRules.graphIdHint")}
                  </p>
                </div>
              )}
            </div>
          )}
        </div>
      ))}

      {rules.length === 0 && (
        <p className="text-xs text-muted-foreground">
          {t("whatsappGroupJoinRules.noRules")}
        </p>
      )}

      <Button
        type="button"
        variant="outline"
        size="sm"
        className="h-8"
        onClick={addRule}
      >
        <Plus className="h-3.5 w-3.5 mr-1" />
        {t("whatsappGroupJoinRules.addRule")}
      </Button>
    </fieldset>
  );
}
