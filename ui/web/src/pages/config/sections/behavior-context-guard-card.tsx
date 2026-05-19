import { Shield, Plus, Trash2 } from "lucide-react";
import { useTranslation } from "react-i18next";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
import { Textarea } from "@/components/ui/textarea";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";

export interface ContextGuardRule {
  name: string;
  description: string;
  type: "allow" | "deny";
  action: "block" | "warn";
}

export interface ContextGuardValues {
  enabled?: boolean;
  model?: string;
  scope_description?: string;
  rules?: ContextGuardRule[];
  notify_owner?: boolean;
  max_history_turns?: number;
}

interface Props {
  value: ContextGuardValues;
  onChange: (v: ContextGuardValues) => void;
}

export function BehaviorContextGuardCard({ value, onChange }: Props) {
  const { t } = useTranslation("config");
  const enabled = value.enabled ?? false;
  const rules = value.rules ?? [];

  const updateRules = (next: ContextGuardRule[]) => {
    onChange({ ...value, rules: next });
  };

  const addRule = () => {
    updateRules([
      ...rules,
      { name: "", description: "", type: "deny", action: "block" },
    ]);
  };

  const removeRule = (idx: number) => {
    updateRules(rules.filter((_, i) => i !== idx));
  };

  const changeRule = (idx: number, patch: Partial<ContextGuardRule>) => {
    updateRules(rules.map((r, i) => (i === idx ? { ...r, ...patch } : r)));
  };

  return (
    <Card>
      <CardHeader className="pb-3">
        <CardTitle className="text-base flex items-center gap-2">
          <Shield className="h-4 w-4 text-emerald-500" />
          {t("behavior.contextGuardTitle")}
        </CardTitle>
        <CardDescription>{t("behavior.contextGuardDescription")}</CardDescription>
      </CardHeader>
      <CardContent className="space-y-4">
        {/* Enable toggle */}
        <div className="flex items-center justify-between gap-4">
          <div className="space-y-0.5">
            <Label className="text-sm font-medium">{t("behavior.contextGuardEnabled")}</Label>
            <p className="text-xs text-muted-foreground">{t("behavior.contextGuardEnabledHint")}</p>
          </div>
          <Switch
            checked={enabled}
            onCheckedChange={(v) => onChange({ ...value, enabled: v })}
          />
        </div>

        {enabled && (
          <>
            {/* Model */}
            <div className="space-y-2">
              <Label className="text-sm font-medium">{t("behavior.contextGuardModel")}</Label>
              <Input
                value={value.model ?? ""}
                onChange={(e) => onChange({ ...value, model: e.target.value || undefined })}
                placeholder={t("behavior.contextGuardModelPlaceholder")}
                className="text-sm"
              />
              <p className="text-xs text-muted-foreground">{t("behavior.contextGuardModelHint")}</p>
            </div>

            {/* Scope description */}
            <div className="space-y-2">
              <Label className="text-sm font-medium">{t("behavior.contextGuardScope")}</Label>
              <Textarea
                value={value.scope_description ?? ""}
                onChange={(e) =>
                  onChange({ ...value, scope_description: e.target.value || undefined })
                }
                placeholder={t("behavior.contextGuardScopePlaceholder")}
                className="text-sm min-h-[60px]"
              />
              <p className="text-xs text-muted-foreground">{t("behavior.contextGuardScopeHint")}</p>
            </div>

            {/* Max history turns */}
            <div className="space-y-2">
              <Label className="text-sm font-medium">{t("behavior.contextGuardMaxHistory")}</Label>
              <Input
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
                className="text-sm w-24"
              />
              <p className="text-xs text-muted-foreground">{t("behavior.contextGuardMaxHistoryHint")}</p>
            </div>

            {/* Notify owner */}
            <div className="flex items-center justify-between gap-4">
              <div className="space-y-0.5">
                <Label className="text-sm font-medium">{t("behavior.contextGuardNotifyOwner")}</Label>
                <p className="text-xs text-muted-foreground">{t("behavior.contextGuardNotifyOwnerHint")}</p>
              </div>
              <Switch
                checked={value.notify_owner ?? false}
                onCheckedChange={(v) => onChange({ ...value, notify_owner: v })}
              />
            </div>

            {/* Rules */}
            <div className="space-y-2 pt-2">
              <div className="flex items-center justify-between">
                <Label className="text-sm font-medium">{t("behavior.contextGuardRules")}</Label>
                <Button size="xs" variant="outline" onClick={addRule} className="gap-1">
                  <Plus className="h-3.5 w-3.5" />
                  {t("behavior.contextGuardAddRule")}
                </Button>
              </div>

              {rules.length === 0 && (
                <p className="text-xs text-muted-foreground italic">
                  {t("behavior.contextGuardNoRules")}
                </p>
              )}

              <div className="space-y-3">
                {rules.map((rule, idx) => (
                  <div key={idx} className="rounded-md border p-3 space-y-2">
                    <div className="flex items-center gap-2">
                      <Input
                        value={rule.name}
                        onChange={(e) => changeRule(idx, { name: e.target.value })}
                        placeholder={t("behavior.contextGuardRuleName")}
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
                          <SelectItem value="allow">{t("behavior.contextGuardAllow")}</SelectItem>
                          <SelectItem value="deny">{t("behavior.contextGuardDeny")}</SelectItem>
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
                          <SelectItem value="block">{t("behavior.contextGuardBlock")}</SelectItem>
                          <SelectItem value="warn">{t("behavior.contextGuardWarn")}</SelectItem>
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
                      placeholder={t("behavior.contextGuardRuleDescription")}
                      className="text-sm min-h-[48px]"
                    />
                  </div>
                ))}
              </div>
            </div>
          </>
        )}
      </CardContent>
    </Card>
  );
}
