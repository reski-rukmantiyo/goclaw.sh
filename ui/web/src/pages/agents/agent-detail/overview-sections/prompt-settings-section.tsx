import { useState, useEffect } from "react";
import { useTranslation } from "react-i18next";
import { Button } from "@/components/ui/button";
import { useAuthStore } from "@/stores/use-auth-store";
import type { AgentData } from "@/types/agent";
import { PromptModeCards, type PromptMode } from "../../prompt-mode-cards";
import { useTtsConfig } from "@/pages/tts/hooks/use-tts-config";
import { TtsEmptyState } from "./tts-empty-state";
import { TtsOverrideBlock } from "./tts-override-block";
import { PROVIDER_MODEL_CATALOG, type TtsProviderId, type TtsModelOption } from "@/data/tts-providers";
import type { ParamValue } from "@/components/dynamic-param-form";

/**
 * Pure helper — exported for unit testing.
 * Returns true when TTS is configured globally and the TTS subsection should render.
 */
export function shouldRenderTTSSection(globalTts: { provider?: string }): boolean {
  return !!globalTts.provider;
}

/**
 * Pure helper — exported for unit testing.
 * Returns model options for a given provider id from the static catalog fallback.
 * Source of truth is GET /v1/tts/capabilities; this fallback covers non-React contexts.
 */
export function getModelOptions(providerId: string): TtsModelOption[] {
  return PROVIDER_MODEL_CATALOG[providerId as TtsProviderId] ?? [];
}

interface Props {
  agent: AgentData;
  onUpdate: (updates: Record<string, unknown>) => Promise<void>;
  promptMode: PromptMode;
  onPromptModeChange: (v: PromptMode) => void;
}

export function PromptSettingsSection({ agent, onUpdate, promptMode, onPromptModeChange }: Props) {
  const { t } = useTranslation("agents");
  const { t: tTts } = useTranslation("tts");
  const isOwner = useAuthStore((s) => s.isOwner);
  const { tts: globalTts, synthesize } = useTtsConfig();
  const globalProvider = globalTts.provider;

  const otherConfig = (agent.other_config ?? {}) as Record<string, unknown>;
  const savedVoiceId = (otherConfig.tts_voice_id as string) ?? "";
  const savedModelId = (otherConfig.tts_model_id as string) ?? "";
  const savedTtsParams = (otherConfig.tts_params as Record<string, ParamValue>) ?? {};

  const [ttsVoiceId, setTtsVoiceId] = useState<string>(savedVoiceId);
  const [ttsModelId, setTtsModelId] = useState<string>(savedModelId);
  const [ttsParams, setTtsParams] = useState<Record<string, ParamValue>>(savedTtsParams);
  const [override, setOverride] = useState<boolean>(!!(savedVoiceId || savedModelId));
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    const cfg = (agent.other_config ?? {}) as Record<string, unknown>;
    const voice = (cfg.tts_voice_id as string) ?? "";
    const model = (cfg.tts_model_id as string) ?? "";
    const params = (cfg.tts_params as Record<string, ParamValue>) ?? {};
    setTtsVoiceId(voice);
    setTtsModelId(model);
    setTtsParams(params);
    setOverride(!!(voice || model));
  }, [agent.other_config]);

  const savedOverride = !!(savedVoiceId || savedModelId);
  const ttsParamsDirty = JSON.stringify(ttsParams) !== JSON.stringify(savedTtsParams);
  const ttsDirty =
    ttsVoiceId !== savedVoiceId ||
    ttsModelId !== savedModelId ||
    override !== savedOverride ||
    ttsParamsDirty;

  const handleSave = async () => {
    setSaving(true);
    try {
      const bag = { ...otherConfig };
      if (override && ttsVoiceId) {
        bag.tts_voice_id = ttsVoiceId;
      } else {
        delete bag.tts_voice_id;
      }
      if (override && ttsModelId) {
        bag.tts_model_id = ttsModelId;
      } else {
        delete bag.tts_model_id;
      }
      if (override && Object.keys(ttsParams).length > 0) {
        bag.tts_params = ttsParams;
      } else {
        delete bag.tts_params;
      }
      await onUpdate({ other_config: bag });
    } finally {
      setSaving(false);
    }
  };

  return (
    <section className="space-y-3 rounded-lg border p-3 sm:p-4">
      <div className="flex items-center justify-between">
        <h3 className="text-sm font-medium">{t("detail.prompt.title")}</h3>
        {ttsDirty && (
          <Button size="sm" onClick={handleSave} disabled={saving}>
            {saving ? t("saving", "Saving...") : t("save", "Save")}
          </Button>
        )}
      </div>

      <PromptModeCards value={promptMode} onChange={onPromptModeChange} />

      {/* TTS subsection */}
      <div className="space-y-3 border-t pt-3">
        <h4 className="text-xs font-medium text-muted-foreground uppercase tracking-wide">
          {tTts("title")}
        </h4>
        {!globalProvider ? (
          <TtsEmptyState isOwner={isOwner} />
        ) : (
          <TtsOverrideBlock
            globalProvider={globalProvider}
            voiceId={ttsVoiceId}
            modelId={ttsModelId}
            onVoiceChange={setTtsVoiceId}
            onModelChange={setTtsModelId}
            overrideEnabled={override}
            onOverrideChange={setOverride}
            synthesize={synthesize}
            ttsParams={ttsParams}
            onTtsParamsChange={setTtsParams}
          />
        )}
      </div>
    </section>
  );
}
