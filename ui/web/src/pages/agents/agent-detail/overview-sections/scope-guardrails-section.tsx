import { useState, useEffect } from "react";
import { useTranslation } from "react-i18next";
import { ShieldCheck, X, Plus } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
import { Textarea } from "@/components/ui/textarea";
import {
  Select, SelectContent, SelectItem, SelectTrigger, SelectValue,
} from "@/components/ui/select";
import type { AgentData } from "@/types/agent";

interface Props {
  agent: AgentData;
  onUpdate: (updates: Record<string, unknown>) => Promise<void>;
}

interface ScopeGuardrailsConfig {
  enabled?: boolean;
  enforcement?: string;
  scope_description?: string;
  allowed_topics?: string[];
  denied_topics?: string[];
  off_topic_response?: string;
}

function readScopeGuardrails(agent: AgentData): ScopeGuardrailsConfig {
  const bag = (agent.other_config ?? {}) as Record<string, unknown>;
  return (bag.scope_guardrails as ScopeGuardrailsConfig) || {};
}

export function ScopeGuardrailsSection({ agent, onUpdate }: Props) {
  const { t } = useTranslation("agents");
  const saved = readScopeGuardrails(agent);

  const [enabled, setEnabled] = useState(Boolean(saved.enabled));
  const [enforcement, setEnforcement] = useState(saved.enforcement || "soft");
  const [scopeDescription, setScopeDescription] = useState(saved.scope_description || "");
  const [allowedTopics, setAllowedTopics] = useState<string[]>(saved.allowed_topics || []);
  const [deniedTopics, setDeniedTopics] = useState<string[]>(saved.denied_topics || []);
  const [offTopicResponse, setOffTopicResponse] = useState(saved.off_topic_response || "");
  const [newAllowed, setNewAllowed] = useState("");
  const [newDenied, setNewDenied] = useState("");
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    const s = readScopeGuardrails(agent);
    setEnabled(Boolean(s.enabled));
    setEnforcement(s.enforcement || "soft");
    setScopeDescription(s.scope_description || "");
    setAllowedTopics(s.allowed_topics || []);
    setDeniedTopics(s.denied_topics || []);
    setOffTopicResponse(s.off_topic_response || "");
  }, [agent.other_config]);

  const savedStr = JSON.stringify(readScopeGuardrails(agent));
  const currentStr = JSON.stringify({
    enabled, enforcement, scope_description: scopeDescription,
    allowed_topics: allowedTopics, denied_topics: deniedTopics,
    off_topic_response: offTopicResponse,
  });
  const dirty = savedStr !== currentStr;

  const addAllowed = () => {
    const v = newAllowed.trim();
    if (v && !allowedTopics.includes(v)) {
      setAllowedTopics([...allowedTopics, v]);
      setNewAllowed("");
    }
  };

  const removeAllowed = (topic: string) => {
    setAllowedTopics(allowedTopics.filter((t) => t !== topic));
  };

  const addDenied = () => {
    const v = newDenied.trim();
    if (v && !deniedTopics.includes(v)) {
      setDeniedTopics([...deniedTopics, v]);
      setNewDenied("");
    }
  };

  const removeDenied = (topic: string) => {
    setDeniedTopics(deniedTopics.filter((t) => t !== topic));
  };

  const handleSave = async () => {
    setSaving(true);
    try {
      const bag = { ...((agent.other_config ?? {}) as Record<string, unknown>) };
      if (enabled) {
        const sg: ScopeGuardrailsConfig = { enabled: true, enforcement };
        if (scopeDescription.trim()) sg.scope_description = scopeDescription.trim();
        if (allowedTopics.length > 0) sg.allowed_topics = allowedTopics;
        if (deniedTopics.length > 0) sg.denied_topics = deniedTopics;
        if (offTopicResponse.trim()) sg.off_topic_response = offTopicResponse.trim();
        bag.scope_guardrails = sg;
      } else {
        delete bag.scope_guardrails;
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
          <ShieldCheck className="h-4 w-4 text-blue-500 shrink-0" />
          <div>
            <h3 className="text-sm font-medium">{t("detail.scopeGuardrails.title", "Scope Guardrails")}</h3>
            <p className="text-xs text-muted-foreground">{t("detail.scopeGuardrails.hint", "Restrict agent to specific topics and domains")}</p>
          </div>
        </div>
        <div className="flex items-center gap-3">
          {dirty && (
            <Button size="xs" onClick={handleSave} disabled={saving}>
              {saving ? t("general.saving") : t("general.saveChanges", "Save")}
            </Button>
          )}
          <Switch checked={enabled} onCheckedChange={setEnabled} />
        </div>
      </div>

      {enabled && (
        <div className="space-y-4">
          {/* Enforcement mode */}
          <div className="space-y-1.5">
            <Label className="text-xs font-normal text-muted-foreground">
              {t("detail.scopeGuardrails.enforcementLabel", "Enforcement")}
            </Label>
            <Select value={enforcement} onValueChange={setEnforcement}>
              <SelectTrigger className="w-full sm:w-[200px] text-base md:text-sm h-8">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="soft">
                  {t("detail.scopeGuardrails.softMode", "Soft (prompt-only)")}
                </SelectItem>
                <SelectItem value="strict">
                  {t("detail.scopeGuardrails.strictMode", "Strict (prompt + output guard)")}
                </SelectItem>
              </SelectContent>
            </Select>
            <p className="text-xs text-muted-foreground">
              {enforcement === "soft"
                ? t("detail.scopeGuardrails.softHint", "Instructions are added to the system prompt. No runtime enforcement.")
                : t("detail.scopeGuardrails.strictHint", "Off-topic responses are detected and replaced at runtime.")}
            </p>
          </div>

          {/* Scope description */}
          <div className="space-y-1.5">
            <Label className="text-xs font-normal text-muted-foreground">
              {t("detail.scopeGuardrails.scopeLabel", "Scope Description")}
            </Label>
            <Textarea
              value={scopeDescription}
              onChange={(e) => setScopeDescription(e.target.value)}
              placeholder={t("detail.scopeGuardrails.scopePlaceholder", "e.g. Personal finance assistant specializing in budgeting and investing")}
              rows={2}
              className="text-base md:text-sm"
            />
          </div>

          {/* Allowed topics */}
          <div className="space-y-1.5">
            <Label className="text-xs font-normal text-muted-foreground">
              {t("detail.scopeGuardrails.allowedLabel", "Allowed Topics")}
            </Label>
            {allowedTopics.length > 0 && (
              <div className="flex flex-wrap gap-1.5">
                {allowedTopics.map((topic) => (
                  <Badge
                    key={topic} variant="secondary"
                    className="text-xs gap-1 pr-1 cursor-pointer hover:bg-green-200 dark:hover:bg-green-900"
                    onClick={() => removeAllowed(topic)}
                  >
                    {topic}
                    <X className="h-3 w-3" />
                  </Badge>
                ))}
              </div>
            )}
            <div className="flex gap-1.5">
              <Input
                value={newAllowed}
                onChange={(e) => setNewAllowed(e.target.value)}
                onKeyDown={(e) => e.key === "Enter" && (e.preventDefault(), addAllowed())}
                placeholder={t("detail.scopeGuardrails.addTopicPlaceholder", "Add topic...")}
                className="text-base md:text-sm h-8"
              />
              <Button size="sm" variant="outline" onClick={addAllowed} disabled={!newAllowed.trim()} className="shrink-0">
                <Plus className="h-3.5 w-3.5" />
              </Button>
            </div>
          </div>

          {/* Denied topics */}
          <div className="space-y-1.5">
            <Label className="text-xs font-normal text-muted-foreground">
              {t("detail.scopeGuardrails.deniedLabel", "Denied Topics")}
            </Label>
            {deniedTopics.length > 0 && (
              <div className="flex flex-wrap gap-1.5">
                {deniedTopics.map((topic) => (
                  <Badge
                    key={topic} variant="secondary"
                    className="text-xs gap-1 pr-1 cursor-pointer hover:bg-destructive/20"
                    onClick={() => removeDenied(topic)}
                  >
                    {topic}
                    <X className="h-3 w-3" />
                  </Badge>
                ))}
              </div>
            )}
            <div className="flex gap-1.5">
              <Input
                value={newDenied}
                onChange={(e) => setNewDenied(e.target.value)}
                onKeyDown={(e) => e.key === "Enter" && (e.preventDefault(), addDenied())}
                placeholder={t("detail.scopeGuardrails.addTopicPlaceholder", "Add topic...")}
                className="text-base md:text-sm h-8"
              />
              <Button size="sm" variant="outline" onClick={addDenied} disabled={!newDenied.trim()} className="shrink-0">
                <Plus className="h-3.5 w-3.5" />
              </Button>
            </div>
          </div>

          {/* Off-topic response */}
          <div className="space-y-1.5">
            <Label className="text-xs font-normal text-muted-foreground">
              {t("detail.scopeGuardrails.offTopicLabel", "Off-Topic Response")}
            </Label>
            <Textarea
              value={offTopicResponse}
              onChange={(e) => setOffTopicResponse(e.target.value)}
              placeholder={t("detail.scopeGuardrails.offTopicPlaceholder", "e.g. I'm focused on finance topics. Let me help you with budgeting or investing instead.")}
              rows={2}
              maxLength={500}
              className="text-base md:text-sm"
            />
            <p className="text-xs text-muted-foreground">
              {offTopicResponse.length}/500
            </p>
          </div>
        </div>
      )}
    </section>
  );
}
