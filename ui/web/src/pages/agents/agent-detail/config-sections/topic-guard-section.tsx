import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Input } from "@/components/ui/input";
import { Switch } from "@/components/ui/switch";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Textarea } from "@/components/ui/textarea";
import type { TopicGuardConfig } from "@/types/agent";
import { ConfigSection, InfoLabel, tagsToArray, arrayToTags, numOrUndef } from "./config-section";

interface TopicGuardSectionProps {
  enabled: boolean;
  value: TopicGuardConfig;
  onToggle: (v: boolean) => void;
  onChange: (v: TopicGuardConfig) => void;
}

export function TopicGuardSection({ enabled, value, onToggle, onChange }: TopicGuardSectionProps) {
  const { t } = useTranslation("agents");
  const s = "configSections.topicGuard";

  return (
    <ConfigSection
      title={t(`${s}.title`, "Topic Guard")}
      description={t(
        `${s}.description`,
        "Restrict conversations to defined topics. Questions outside scope are blocked."
      )}
      enabled={enabled}
      onToggle={onToggle}
    >
      {/* Mode */}
      <div className="max-w-xs space-y-2">
        <InfoLabel tip={t(`${s}.modeTip`, "Keyword-only is fast. LLM fallback uses a lightweight model to classify ambiguous questions.")}>
          {t(`${s}.mode`, "Detection Mode")}
        </InfoLabel>
        <Select
          value={value.mode || "keyword"}
          onValueChange={(v) => onChange({ ...value, mode: v as TopicGuardConfig["mode"] })}
        >
          <SelectTrigger>
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="keyword">{t(`${s}.modeKeyword`, "Keyword Only")}</SelectItem>
            <SelectItem value="keyword_and_llm">{t(`${s}.modeLLM`, "Keyword + LLM Fallback")}</SelectItem>
          </SelectContent>
        </Select>
      </div>

      {/* Allow keywords */}
      <div className="space-y-2">
        <InfoLabel tip={t(`${s}.allowTip`, "Comma-separated keywords for in-context topics. Questions matching these are always allowed.")}>
          {t(`${s}.allowKeywords`, "Allowed Keywords")}
        </InfoLabel>
        <Input
          type="text"
          placeholder={t(`${s}.keywordsPlaceholder`, "e.g. python, api, database")}
          value={arrayToTags(value.allow_keywords)}
          onChange={(e) =>
            onChange({ ...value, allow_keywords: tagsToArray(e.target.value) })
          }
        />
      </div>

      {/* Block keywords */}
      <div className="space-y-2">
        <InfoLabel tip={t(`${s}.blockTip`, "Comma-separated keywords for out-of-context topics. Block takes priority over allow.")}>
          {t(`${s}.blockKeywords`, "Blocked Keywords")}
        </InfoLabel>
        <Input
          type="text"
          placeholder={t(`${s}.keywordsPlaceholder`, "e.g. hack, exploit")}
          value={arrayToTags(value.block_keywords)}
          onChange={(e) =>
            onChange({ ...value, block_keywords: tagsToArray(e.target.value) })
          }
        />
      </div>

      {/* Default action */}
      <div className="max-w-xs space-y-2">
        <InfoLabel tip={t(`${s}.defaultTip`, "What happens when a question matches neither allowed nor blocked keywords.")}>
          {t(`${s}.defaultAction`, "Default Action")}
        </InfoLabel>
        <Select
          value={value.default_action || "allow"}
          onValueChange={(v) => onChange({ ...value, default_action: v as TopicGuardConfig["default_action"] })}
        >
          <SelectTrigger>
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="allow">{t(`${s}.defaultAllow`, "Allow")}</SelectItem>
            <SelectItem value="block">{t(`${s}.defaultBlock`, "Block")}</SelectItem>
          </SelectContent>
        </Select>
      </div>

      {/* Rejection message */}
      <div className="space-y-2">
        <InfoLabel tip={t(`${s}.rejectionTip`, "Custom message shown when a question is blocked. Leave empty for default.")}>
          {t(`${s}.rejectionMessage`, "Rejection Message")}
        </InfoLabel>
        <Textarea
          placeholder={t(`${s}.rejectionPlaceholder`, "This question is outside the scope of this agent.")}
          value={value.rejection_message ?? ""}
          onChange={(e) =>
            onChange({ ...value, rejection_message: e.target.value || undefined })
          }
          rows={2}
        />
      </div>

      {/* LLM config (only when mode is keyword_and_llm) */}
      {value.mode === "keyword_and_llm" && (
        <div className="space-y-4 pl-4 border-l-2 border-muted">
          <p className="text-xs text-amber-600 dark:text-amber-400">
            {t(`${s}.llmWarning`, "Classification model must be small (<7B params) to minimize latency and cost.")}
          </p>
          <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
            <div className="space-y-2">
              <InfoLabel tip={t(`${s}.llmProviderTip`, "Provider for classification. Empty = use agent's provider.")}>
                {t(`${s}.llmProvider`, "LLM Provider")}
              </InfoLabel>
              <Input
                type="text"
                placeholder={t(`${s}.llmProviderPlaceholder`, "e.g. ollama, groq")}
                value={value.llm_provider ?? ""}
                onChange={(e) => onChange({ ...value, llm_provider: e.target.value || undefined })}
              />
            </div>
            <div className="space-y-2">
              <InfoLabel tip={t(`${s}.llmModelTip`, "Model for classification. Must be <7B params. Empty = smallest available.")}>
                {t(`${s}.llmModel`, "LLM Model")}
              </InfoLabel>
              <Input
                type="text"
                placeholder={t(`${s}.llmModelPlaceholder`, "e.g. llama3.2:1b, gemma2:2b")}
                value={value.llm_model ?? ""}
                onChange={(e) => onChange({ ...value, llm_model: e.target.value || undefined })}
              />
            </div>
          </div>
          <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
            <div className="space-y-2">
              <InfoLabel tip={t(`${s}.llmTimeoutTip`, "Timeout in milliseconds for classification call.")}>
                {t(`${s}.llmTimeout`, "Timeout (ms)")}
              </InfoLabel>
              <Input
                type="number"
                placeholder="5000"
                value={value.llm_timeout_ms ?? ""}
                onChange={(e) => onChange({ ...value, llm_timeout_ms: numOrUndef(e.target.value) })}
              />
            </div>
            <div className="space-y-2">
              <InfoLabel tip={t(`${s}.llmMaxTokensTip`, "Max tokens in classification response. Default 10.")}>
                {t(`${s}.llmMaxTokens`, "Max Tokens")}
              </InfoLabel>
              <Input
                type="number"
                placeholder="10"
                value={value.llm_max_tokens ?? ""}
                onChange={(e) => onChange({ ...value, llm_max_tokens: numOrUndef(e.target.value) })}
              />
            </div>
          </div>
        </div>
      )}
    </ConfigSection>
  );
}
