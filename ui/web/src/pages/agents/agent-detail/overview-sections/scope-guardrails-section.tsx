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

export interface ScopeGuardrailsConfig {
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

interface Props {
  agent?: AgentData;
  frontmatter: string;
  enabled: boolean;
  onEnabledChange: (v: boolean) => void;
  enforcement: string;
  onEnforcementChange: (v: string) => void;
  scopeDescription: string;
  onScopeDescriptionChange: (v: string) => void;
  allowedTopics: string[];
  onAllowedTopicsChange: (v: string[]) => void;
  deniedTopics: string[];
  onDeniedTopicsChange: (v: string[]) => void;
  offTopicResponse: string;
  onOffTopicResponseChange: (v: string) => void;
}

export { readScopeGuardrails };

export function ScopeGuardrailsSection({
  frontmatter,
  enabled,
  onEnabledChange,
  enforcement,
  onEnforcementChange,
  scopeDescription,
  onScopeDescriptionChange,
  allowedTopics,
  onAllowedTopicsChange,
  deniedTopics,
  onDeniedTopicsChange,
  offTopicResponse,
  onOffTopicResponseChange,
}: Props) {
  const { t } = useTranslation("agents");
  const [newAllowed, setNewAllowed] = useState("");
  const [newDenied, setNewDenied] = useState("");

  // Auto-fill scope description from frontmatter (Expertise Summary) when empty
  useEffect(() => {
    if (enabled && !scopeDescription && frontmatter) {
      onScopeDescriptionChange(frontmatter);
    }
  }, [enabled, frontmatter]); // eslint-disable-line react-hooks/exhaustive-deps

  const addAllowed = () => {
    const v = newAllowed.trim();
    if (v && !allowedTopics.includes(v)) {
      onAllowedTopicsChange([...allowedTopics, v]);
      setNewAllowed("");
    }
  };

  const removeAllowed = (topic: string) => {
    onAllowedTopicsChange(allowedTopics.filter((t) => t !== topic));
  };

  const addDenied = () => {
    const v = newDenied.trim();
    if (v && !deniedTopics.includes(v)) {
      onDeniedTopicsChange([...deniedTopics, v]);
      setNewDenied("");
    }
  };

  const removeDenied = (topic: string) => {
    onDeniedTopicsChange(deniedTopics.filter((t) => t !== topic));
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
        <Switch checked={enabled} onCheckedChange={onEnabledChange} />
      </div>

      {enabled && (
        <div className="space-y-4">
          {/* Enforcement mode */}
          <div className="space-y-1.5">
            <Label className="text-xs font-normal text-muted-foreground">
              {t("detail.scopeGuardrails.enforcementLabel", "Enforcement")}
            </Label>
            <Select value={enforcement} onValueChange={onEnforcementChange}>
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
              onChange={(e) => onScopeDescriptionChange(e.target.value)}
              placeholder={frontmatter || t("detail.scopeGuardrails.scopePlaceholder", "e.g. Personal finance assistant specializing in budgeting and investing")}
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
              onChange={(e) => onOffTopicResponseChange(e.target.value)}
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
