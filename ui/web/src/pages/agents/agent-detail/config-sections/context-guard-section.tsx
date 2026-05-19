import { useState, useEffect } from "react";
import { useTranslation } from "react-i18next";
import { Shield, Plus, Trash2 } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
import { Textarea } from "@/components/ui/textarea";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import type { AgentData } from "@/types/agent";

interface ContextGuardRule {
  name: string;
  description: string;
  type: "allow" | "deny";
  action: "block" | "warn";
}

interface ContextGuardConfig {
  enabled?: boolean;
  model?: string;
  scope_description?: string;
  rules?: ContextGuardRule[];
  notify_owner?: boolean;
  max_history_turns?: number;
}

interface Props {
  agent: AgentData;
  onUpdate: (updates: Record<string, unknown>) => Promise<void>;
}

function readContextGuard(agent: AgentData): ContextGuardConfig {
  const bag = (agent.other_config ?? {}) as Record<string, unknown>;
  return (bag.context_guard as ContextGuardConfig) ?? {};
}

export function ContextGuardSection({ agent, onUpdate }: Props) {
  const { t } = useTranslation("agents");
  const saved = readContextGuard(agent);

  const [enabled, setEnabled] = useState(saved.enabled ?? false);
  const [model, setModel] = useState(saved.model ?? "");
  const [scope, setScope] = useState(saved.scope_description ?? "");
  const [notifyOwner, setNotifyOwner] = useState(saved.notify_owner ?? false);
  const [maxHistory, setMaxHistory] = useState<number | undefined>(saved.max_history_turns ?? 5);
  const [rules, setRules] = useState<ContextGuardRule[]>(saved.rules ?? []);
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    const s = readContextGuard(agent);
    setEnabled(s.enabled ?? false);
    setModel(s.model ?? "");
    setScope(s.scope_description ?? "");
    setNotifyOwner(s.notify_owner ?? false);
    setMaxHistory(s.max_history_turns ?? 5);
    setRules(s.rules ?? []);
  }, [agent.other_config]);

  const dirty =
    enabled !== (saved.enabled ?? false) ||
    model !== (saved.model ?? "") ||
    scope !== (saved.scope_description ?? "") ||
    notifyOwner !== (saved.notify_owner ?? false) ||
    maxHistory !== (saved.max_history_turns ?? 5) ||
    JSON.stringify(rules) !== JSON.stringify(saved.rules ?? []);

  const addRule = () => {
    setRules([...rules, { name: "", description: "", type: "deny", action: "block" }]);
  };

  const removeRule = (idx: number) => {
    setRules(rules.filter((_, i) => i !== idx));
  };

  const changeRule = (idx: number, patch: Partial<ContextGuardRule>) => {
    setRules(rules.map((r, i) => (i === idx ? { ...r, ...patch } : r)));
  };

  const handleSave = async () => {
    setSaving(true);
    try {
      const bag = { ...((agent.other_config ?? {}) as Record<string, unknown>) };
      const cfg: ContextGuardConfig | undefined = enabled
        ? {
            enabled: true,
            model: model || undefined,
            scope_description: scope || undefined,
            notify_owner: notifyOwner,
            max_history_turns: maxHistory,
            rules: rules.length > 0 ? rules : undefined,
          }
        : undefined;
      if (cfg) {
        bag.context_guard = cfg;
      } else {
        delete bag.context_guard;
      }
      await onUpdate({ other_config: bag });
    } finally {
      setSaving(false);
    }
  };

  return (
    <section className="space-y-2.5 rounded-lg border p-3 sm:p-4">
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-2">
          <Shield className="h-4 w-4 text-emerald-500 shrink-0" />
          <h3 className="text-sm font-medium">{t("detail.contextGuard.title", "Context Guard")}</h3>
        </div>
        {dirty && (
          <Button size="xs" onClick={handleSave} disabled={saving}>
            {saving ? t("saving", "Saving...") : t("save", "Save")}
          </Button>
        )}
      </div>

      <div className="flex items-center justify-between gap-4">
        <div className="space-y-0.5">
          <Label className="text-sm font-medium">{t("detail.contextGuard.enabled", "Enable Context Guard")}</Label>
          <p className="text-xs text-muted-foreground">{t("detail.contextGuard.enabledHint", "Evaluate messages against rules using conversation context.")}</p>
        </div>
        <Switch checked={enabled} onCheckedChange={setEnabled} />
      </div>

      {enabled && (
        <div className="space-y-4 pt-2">
          <div className="space-y-2">
            <Label className="text-sm font-medium">{t("detail.contextGuard.model", "Evaluator Model")}</Label>
            <Input
              value={model}
              onChange={(e) => setModel(e.target.value)}
              placeholder="e.g. haiku"
              className="text-sm"
            />
            <p className="text-xs text-muted-foreground">{t("detail.contextGuard.modelHint", "Lightweight model for guard evaluation. Leave empty to use the agent's default model.")}</p>
          </div>

          <div className="space-y-2">
            <Label className="text-sm font-medium">{t("detail.contextGuard.scope", "Scope Description")}</Label>
            <Textarea
              value={scope}
              onChange={(e) => setScope(e.target.value)}
              placeholder="e.g. coding assistant — only help with software development"
              className="text-sm min-h-[60px]"
            />
            <p className="text-xs text-muted-foreground">{t("detail.contextGuard.scopeHint", "Describes what this agent is allowed to discuss.")}</p>
          </div>

          <div className="space-y-2">
            <Label className="text-sm font-medium">{t("detail.contextGuard.maxHistory", "History Turns")}</Label>
            <Input
              type="number"
              min={0}
              max={50}
              value={maxHistory ?? 5}
              onChange={(e) => setMaxHistory(e.target.value ? parseInt(e.target.value, 10) : undefined)}
              className="text-sm w-24"
            />
            <p className="text-xs text-muted-foreground">{t("detail.contextGuard.maxHistoryHint", "Recent messages to include in evaluation context.")}</p>
          </div>

          <div className="flex items-center justify-between gap-4">
            <div className="space-y-0.5">
              <Label className="text-sm font-medium">{t("detail.contextGuard.notifyOwner", "Notify Owner on Block")}</Label>
              <p className="text-xs text-muted-foreground">{t("detail.contextGuard.notifyOwnerHint", "Emit security event when a message is blocked.")}</p>
            </div>
            <Switch checked={notifyOwner} onCheckedChange={setNotifyOwner} />
          </div>

          <div className="space-y-2 pt-2">
            <div className="flex items-center justify-between">
              <Label className="text-sm font-medium">{t("detail.contextGuard.rules", "Rules")}</Label>
              <Button size="xs" variant="outline" onClick={addRule} className="gap-1">
                <Plus className="h-3.5 w-3.5" />
                {t("detail.contextGuard.addRule", "Add Rule")}
              </Button>
            </div>

            {rules.length === 0 && (
              <p className="text-xs text-muted-foreground italic">{t("detail.contextGuard.noRules", "No rules configured.")}</p>
            )}

            <div className="space-y-3">
              {rules.map((rule, idx) => (
                <div key={idx} className="rounded-md border p-3 space-y-2">
                  <div className="flex items-center gap-2">
                    <Input
                      value={rule.name}
                      onChange={(e) => changeRule(idx, { name: e.target.value })}
                      placeholder={t("detail.contextGuard.ruleName", "Rule name")}
                      className="text-sm flex-1"
                    />
                    <Select
                      value={rule.type}
                      onValueChange={(v) => changeRule(idx, { type: v as "allow" | "deny" })}
                    >
                      <SelectTrigger className="w-24 text-sm">
                        <SelectValue />
                      </SelectTrigger>
                      <SelectContent>
                        <SelectItem value="allow">{t("detail.contextGuard.allow", "Allow")}</SelectItem>
                        <SelectItem value="deny">{t("detail.contextGuard.deny", "Deny")}</SelectItem>
                      </SelectContent>
                    </Select>
                    <Select
                      value={rule.action}
                      onValueChange={(v) => changeRule(idx, { action: v as "block" | "warn" })}
                    >
                      <SelectTrigger className="w-24 text-sm">
                        <SelectValue />
                      </SelectTrigger>
                      <SelectContent>
                        <SelectItem value="block">{t("detail.contextGuard.block", "Block")}</SelectItem>
                        <SelectItem value="warn">{t("detail.contextGuard.warn", "Warn")}</SelectItem>
                      </SelectContent>
                    </Select>
                    <Button
                      size="icon"
                      variant="ghost"
                      className="h-8 w-8 text-destructive"
                      onClick={() => removeRule(idx)}
                    >
                      <Trash2 className="h-4 w-4" />
                    </Button>
                  </div>
                  <Textarea
                    value={rule.description}
                    onChange={(e) => changeRule(idx, { description: e.target.value })}
                    placeholder={t("detail.contextGuard.ruleDescription", "What this rule covers")}
                    className="text-sm min-h-[48px]"
                  />
                </div>
              ))}
            </div>
          </div>
        </div>
      )}
    </section>
  );
}
