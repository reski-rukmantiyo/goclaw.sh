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
import { ProviderModelSelect } from "@/components/shared/provider-model-select";

export interface ContextGuardRule {
  name: string;
  description: string;
  type: "allow" | "deny";
  action: "block" | "warn";
}

export interface ContextGuardConfig {
  enabled?: boolean;
  provider?: string;
  model?: string;
  scope_description?: string;
  refusal_message?: string;
  rules?: ContextGuardRule[];
  notify_owner?: boolean;
  max_history_turns?: number;
  default_action?: "allow" | "block";
}

interface Props {
  value: ContextGuardConfig;
  onChange: (v: ContextGuardConfig) => void;
}

export function ContextGuardSection({ value, onChange }: Props) {
  const { t } = useTranslation("agents");
  const enabled = value.enabled ?? false;
  const rules = value.rules ?? [];

  const updateRules = (next: ContextGuardRule[]) => {
    onChange({ ...value, rules: next });
  };

  const addRule = () => {
    updateRules([...rules, { name: "", description: "", type: "deny", action: "block" }]);
  };

  const removeRule = (idx: number) => {
    updateRules(rules.filter((_, i) => i !== idx));
  };

  const changeRule = (idx: number, patch: Partial<ContextGuardRule>) => {
    updateRules(rules.map((r, i) => (i === idx ? { ...r, ...patch } : r)));
  };

  return (
    <section className="space-y-2.5 rounded-lg border p-3 sm:p-4">
      <div className="flex items-center gap-2">
        <Shield className="h-4 w-4 text-emerald-500 shrink-0" />
        <h3 className="text-sm font-medium">{t("detail.contextGuard.title")}</h3>
      </div>

      <div className="flex items-center justify-between gap-4">
        <div className="space-y-0.5">
          <Label className="text-sm font-medium">{t("detail.contextGuard.enabled")}</Label>
          <p className="text-xs text-muted-foreground">{t("detail.contextGuard.enabledHint")}</p>
        </div>
        <Switch
          checked={enabled}
          onCheckedChange={(v) => onChange({ ...value, enabled: v })}
        />
      </div>

      {enabled && (
        <div className="space-y-4 pt-2">
          {/* Provider + Model */}
          <ProviderModelSelect
            provider={value.provider ?? ""}
            onProviderChange={(v) => onChange({ ...value, provider: v || undefined })}
            model={value.model ?? ""}
            onModelChange={(v) => onChange({ ...value, model: v || undefined })}
            allowEmpty
            providerTip="LLM provider for guard evaluation. Leave empty to use the agent's provider."
            modelTip="Model for guard evaluation. Leave empty to use the agent's model."
          />

          {/* Scope description */}
          <div className="space-y-2">
            <Label className="text-sm font-medium">{t("detail.contextGuard.scope")}</Label>
            <Textarea
              value={value.scope_description ?? ""}
              onChange={(e) =>
                onChange({ ...value, scope_description: e.target.value || undefined })
              }
              placeholder={t("detail.contextGuard.scopePlaceholder", "e.g. coding assistant — only help with software development")}
              className="text-sm min-h-[60px]"
            />
            <p className="text-xs text-muted-foreground">{t("detail.contextGuard.scopeHint")}</p>
          </div>

          {/* Refusal message */}
          <div className="space-y-2">
            <Label className="text-sm font-medium">{t("detail.contextGuard.refusal")}</Label>
            <Textarea
              value={value.refusal_message ?? ""}
              onChange={(e) =>
                onChange({ ...value, refusal_message: e.target.value || undefined })
              }
              placeholder={t("detail.contextGuard.refusalPlaceholder", "I'm designed to help with %s. I can't assist with requests outside that scope.")}
              className="text-sm min-h-[60px]"
            />
            <p className="text-xs text-muted-foreground">{t("detail.contextGuard.refusalHint")}</p>
          </div>

          {/* Max history turns */}
          <div className="space-y-2">
            <Label className="text-sm font-medium">{t("detail.contextGuard.maxHistory")}</Label>
            <input
              type="number"
              min={0}
              max={50}
              value={value.max_history_turns ?? 5}
              onChange={(e) =>
                onChange({
                  ...value,
                  max_history_turns: e.target.value ? parseInt(e.target.value, 10) : undefined,
                })
              }
              className="text-sm w-24 h-9 rounded-md border border-input bg-transparent px-3 py-1 shadow-sm transition-colors"
            />
            <p className="text-xs text-muted-foreground">{t("detail.contextGuard.maxHistoryHint")}</p>
          </div>

          {/* Default action */}
          <div className="space-y-2">
            <Label className="text-sm font-medium">{t("detail.contextGuard.defaultAction")}</Label>
            <Select
              value={value.default_action ?? "allow"}
              onValueChange={(v) => onChange({ ...value, default_action: v as "allow" | "block" })}
            >
              <SelectTrigger className="w-32 text-sm">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="allow">{t("detail.contextGuard.allow")}</SelectItem>
                <SelectItem value="block">{t("detail.contextGuard.block")}</SelectItem>
              </SelectContent>
            </Select>
            <p className="text-xs text-muted-foreground">{t("detail.contextGuard.defaultActionHint")}</p>
          </div>

          {/* Notify owner */}
          <div className="flex items-center justify-between gap-4">
            <div className="space-y-0.5">
              <Label className="text-sm font-medium">{t("detail.contextGuard.notifyOwner")}</Label>
              <p className="text-xs text-muted-foreground">{t("detail.contextGuard.notifyOwnerHint")}</p>
            </div>
            <Switch
              checked={value.notify_owner ?? false}
              onCheckedChange={(v) => onChange({ ...value, notify_owner: v })}
            />
          </div>

          {/* Rules */}
          <div className="space-y-2 pt-2">
            <div className="flex items-center justify-between">
              <Label className="text-sm font-medium">{t("detail.contextGuard.rules")}</Label>
              <Button size="xs" variant="outline" onClick={addRule} className="gap-1">
                <Plus className="h-3.5 w-3.5" />
                {t("detail.contextGuard.addRule")}
              </Button>
            </div>

            {rules.length === 0 && (
              <p className="text-xs text-muted-foreground italic">
                {t("detail.contextGuard.noRules")}
              </p>
            )}

            <div className="space-y-3">
              {rules.map((rule, idx) => (
                <div key={idx} className="rounded-md border p-3 space-y-2">
                  <div className="flex items-center gap-2">
                    <Input
                      value={rule.name}
                      onChange={(e) => changeRule(idx, { name: e.target.value })}
                      placeholder={t("detail.contextGuard.ruleName")}
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
                        <SelectItem value="allow">{t("detail.contextGuard.allow")}</SelectItem>
                        <SelectItem value="deny">{t("detail.contextGuard.deny")}</SelectItem>
                      </SelectContent>
                    </Select>
                    {rule.type === "deny" && (
                      <Select
                        value={rule.action}
                        onValueChange={(v) => changeRule(idx, { action: v as "block" | "warn" })}
                      >
                        <SelectTrigger className="w-24 text-sm">
                          <SelectValue />
                        </SelectTrigger>
                        <SelectContent>
                          <SelectItem value="block">{t("detail.contextGuard.block")}</SelectItem>
                          <SelectItem value="warn">{t("detail.contextGuard.warn")}</SelectItem>
                        </SelectContent>
                      </Select>
                    )}
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
                    placeholder={t("detail.contextGuard.ruleDescription", "Describe what topics this rule covers")}
                    className="text-sm min-h-[48px]"
                  />
                  <p className="text-xs text-muted-foreground">
                    {rule.type === "allow"
                      ? t("detail.contextGuard.allowHint", "Messages matching this rule are permitted. Messages NOT matching any allow rule are blocked.")
                      : t("detail.contextGuard.denyHint", "Messages matching this rule are rejected with the selected action.")}
                  </p>
                </div>
              ))}
            </div>
          </div>
        </div>
      )}
    </section>
  );
}
