import { useState, useEffect } from "react";
import { useTranslation } from "react-i18next";
import { Input } from "@/components/ui/input";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Textarea } from "@/components/ui/textarea";
import { ProviderModelSelect } from "@/components/shared/provider-model-select";
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

  // Local string state for keyword inputs — avoids round-trip trimming that eats spaces.
  const [allowInput, setAllowInput] = useState(arrayToTags(value.allow_keywords));
  const [blockInput, setBlockInput] = useState(arrayToTags(value.block_keywords));

  // Sync local state when external value changes (e.g. after save/reload).
  useEffect(() => { setAllowInput(arrayToTags(value.allow_keywords)); }, [value.allow_keywords]);
  useEffect(() => { setBlockInput(arrayToTags(value.block_keywords)); }, [value.block_keywords]);

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

      {/* Intercept timing */}
      <div className="max-w-xs space-y-2">
        <InfoLabel tip={t(`${s}.interceptTip`, "Before = check question before LLM (saves tokens). After = check response after LLM. Both = maximum protection.")}>
          {t(`${s}.intercept`, "Intercept Timing")}
        </InfoLabel>
        <Select
          value={value.intercept || "before"}
          onValueChange={(v) => onChange({ ...value, intercept: v as TopicGuardConfig["intercept"] })}
        >
          <SelectTrigger>
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="before">{t(`${s}.interceptBefore`, "Before (save tokens)")}</SelectItem>
            <SelectItem value="after">{t(`${s}.interceptAfter`, "After (check response)")}</SelectItem>
            <SelectItem value="both">{t(`${s}.interceptBoth`, "Both (maximum protection)")}</SelectItem>
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
          value={allowInput}
          onChange={(e) => setAllowInput(e.target.value)}
          onBlur={() =>
            onChange({ ...value, allow_keywords: tagsToArray(allowInput) })
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
          value={blockInput}
          onChange={(e) => setBlockInput(e.target.value)}
          onBlur={() =>
            onChange({ ...value, block_keywords: tagsToArray(blockInput) })
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
          <ProviderModelSelect
            provider={value.llm_provider ?? ""}
            onProviderChange={(v) => onChange({ ...value, llm_provider: v || undefined, llm_model: undefined })}
            model={value.llm_model ?? ""}
            onModelChange={(v) => onChange({ ...value, llm_model: v || undefined })}
            allowEmpty
            providerLabel={t(`${s}.llmProvider`, "LLM Provider")}
            modelLabel={t(`${s}.llmModel`, "LLM Model")}
            providerTip={t(`${s}.llmProviderTip`, "Provider for classification. Empty = use agent's provider.")}
            modelTip={t(`${s}.llmModelTip`, "Model for classification. Must be <7B params.")}
            providerPlaceholder={t(`${s}.llmProviderPlaceholder`, "(use agent provider)")}
            modelPlaceholder={t(`${s}.llmModelPlaceholder`, "e.g. llama3.2:1b, gemma2:2b")}
            showVerify={false}
          />
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
