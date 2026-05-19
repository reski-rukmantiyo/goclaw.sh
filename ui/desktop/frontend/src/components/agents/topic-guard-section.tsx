import { useState, useEffect, useMemo, useCallback } from 'react'
import { useTranslation } from 'react-i18next'
import { ConfigSection } from './config-section'
import { Combobox } from '../common/Combobox'
import { useProviders } from '../../hooks/use-providers'
import { getApiClient } from '../../lib/api'
import { numOrUndef } from '../../lib/format'
import type { TopicGuardConfig } from '../../types/agent'

/** Convert comma-separated string to string array, or undefined if empty. */
function tagsToArray(s: string): string[] | undefined {
  if (!s) return undefined
  return s.split(',').map((t) => t.trim()).filter(Boolean)
}
/** Convert string array to comma-separated display string. */
function arrayToTags(arr?: string[]): string {
  return arr?.join(', ') ?? ''
}

interface TopicGuardSectionProps {
  enabled: boolean
  value: TopicGuardConfig
  onToggle: (v: boolean) => void
  onChange: (v: TopicGuardConfig) => void
}

export function TopicGuardSection({ enabled, value, onToggle, onChange }: TopicGuardSectionProps) {
  const { t } = useTranslation('agents')
  const { providers } = useProviders()
  const update = (patch: Partial<TopicGuardConfig>) => onChange({ ...value, ...patch })
  const s = 'configSections.topicGuard'

  const inputCls = 'w-full bg-surface-tertiary border border-border rounded-lg px-3 py-2 text-base md:text-sm text-text-primary focus:outline-none focus:ring-1 focus:ring-accent'
  const selectCls = inputCls

  // Local string state for keyword inputs — avoids round-trip trimming that eats spaces.
  const [allowInput, setAllowInput] = useState(arrayToTags(value.allow_keywords))
  const [blockInput, setBlockInput] = useState(arrayToTags(value.block_keywords))

  // Sync local state when external value changes.
  useEffect(() => { setAllowInput(arrayToTags(value.allow_keywords)) }, [value.allow_keywords])
  useEffect(() => { setBlockInput(arrayToTags(value.block_keywords)) }, [value.block_keywords])

  // Provider/model dropdown data for LLM classification
  const enabledProviders = useMemo(
    () => providers.filter((p) => p.enabled),
    [providers],
  )
  const selectedProvider = useMemo(
    () => enabledProviders.find((p) => p.name === value.llm_provider),
    [enabledProviders, value.llm_provider],
  )
  const [models, setModels] = useState<string[]>([])
  const [modelsLoading, setModelsLoading] = useState(false)

  const loadModels = useCallback(async (providerId: string) => {
    setModelsLoading(true)
    try {
      const res = await getApiClient().get<{ models: Array<{ id: string }> }>(
        `/v1/providers/${providerId}/models`,
      )
      setModels((res.models ?? []).map((m) => m.id))
    } catch {
      setModels([])
    } finally {
      setModelsLoading(false)
    }
  }, [])

  useEffect(() => {
    if (selectedProvider?.id) loadModels(selectedProvider.id)
  }, [selectedProvider?.id, loadModels])

  const providerOptions = useMemo(
    () => [
      { value: '', label: t(`${s}.llmProviderPlaceholder`) },
      ...enabledProviders.map((p) => ({ value: p.name, label: p.display_name || p.name })),
    ],
    [enabledProviders, s, t],
  )

  const modelOptions = useMemo(
    () => models.map((m) => ({ value: m, label: m })),
    [models],
  )

  const handleProviderChange = (v: string) => {
    update({ llm_provider: v || undefined, llm_model: undefined })
  }

  return (
    <ConfigSection
      title={t(`${s}.title`)}
      description={t(`${s}.description`)}
      enabled={enabled}
      onToggle={onToggle}
    >
      {/* Mode */}
      <div className="space-y-1">
        <label className="text-[11px] font-medium text-text-secondary">{t(`${s}.mode`)}</label>
        <select value={value.mode ?? 'keyword'} onChange={(e) => update({ mode: e.target.value as TopicGuardConfig['mode'] })} className={selectCls}>
          <option value="keyword">{t(`${s}.modeKeyword`)}</option>
          <option value="keyword_and_llm">{t(`${s}.modeLLM`)}</option>
        </select>
      </div>

      {/* Intercept timing */}
      <div className="space-y-1">
        <label className="text-[11px] font-medium text-text-secondary">{t(`${s}.intercept`)}</label>
        <select value={value.intercept ?? 'before'} onChange={(e) => update({ intercept: e.target.value as TopicGuardConfig['intercept'] })} className={selectCls}>
          <option value="before">{t(`${s}.interceptBefore`)}</option>
          <option value="after">{t(`${s}.interceptAfter`)}</option>
          <option value="both">{t(`${s}.interceptBoth`)}</option>
        </select>
      </div>

      {/* Allow keywords */}
      <div className="space-y-1">
        <label className="text-[11px] font-medium text-text-secondary">{t(`${s}.allowKeywords`)}</label>
        <input
          type="text"
          placeholder={t(`${s}.keywordsPlaceholder`)}
          value={allowInput}
          onChange={(e) => setAllowInput(e.target.value)}
          onBlur={() => update({ allow_keywords: tagsToArray(allowInput) })}
          className={inputCls}
        />
      </div>

      {/* Block keywords */}
      <div className="space-y-1">
        <label className="text-[11px] font-medium text-text-secondary">{t(`${s}.blockKeywords`)}</label>
        <input
          type="text"
          placeholder={t(`${s}.keywordsPlaceholder`)}
          value={blockInput}
          onChange={(e) => setBlockInput(e.target.value)}
          onBlur={() => update({ block_keywords: tagsToArray(blockInput) })}
          className={inputCls}
        />
      </div>

      {/* Default action */}
      <div className="space-y-1">
        <label className="text-[11px] font-medium text-text-secondary">{t(`${s}.defaultAction`)}</label>
        <select value={value.default_action ?? 'allow'} onChange={(e) => update({ default_action: e.target.value as TopicGuardConfig['default_action'] })} className={selectCls}>
          <option value="allow">{t(`${s}.defaultAllow`)}</option>
          <option value="block">{t(`${s}.defaultBlock`)}</option>
        </select>
      </div>

      {/* Rejection message */}
      <div className="space-y-1">
        <label className="text-[11px] font-medium text-text-secondary">{t(`${s}.rejectionMessage`)}</label>
        <textarea
          placeholder={t(`${s}.rejectionPlaceholder`)}
          value={value.rejection_message ?? ''}
          onChange={(e) => update({ rejection_message: e.target.value || undefined })}
          rows={2}
          className={`${inputCls} resize-none`}
        />
      </div>

      {/* LLM config */}
      {value.mode === 'keyword_and_llm' && (
        <div className="space-y-3 pl-3 border-l-2 border-border">
          <p className="text-[11px] text-amber-600 dark:text-amber-400">
            {t(`${s}.llmWarning`)}
          </p>
          <div className="grid grid-cols-2 gap-3">
            <div className="space-y-1">
              <label className="text-[11px] font-medium text-text-secondary">{t(`${s}.llmProvider`)}</label>
              <Combobox
                value={value.llm_provider ?? ''}
                onChange={handleProviderChange}
                options={providerOptions}
                placeholder={t(`${s}.llmProviderPlaceholder`)}
              />
            </div>
            <div className="space-y-1">
              <label className="text-[11px] font-medium text-text-secondary">{t(`${s}.llmModel`)}</label>
              <Combobox
                value={value.llm_model ?? ''}
                onChange={(v) => update({ llm_model: v || undefined })}
                options={modelOptions}
                placeholder={modelsLoading ? '...' : t(`${s}.llmModelPlaceholder`)}
                allowCustom
              />
            </div>
          </div>
          <div className="grid grid-cols-2 gap-3">
            <div className="space-y-1">
              <label className="text-[11px] font-medium text-text-secondary">{t(`${s}.llmTimeout`)}</label>
              <input
                type="number"
                placeholder="5000"
                value={value.llm_timeout_ms ?? ''}
                onChange={(e) => update({ llm_timeout_ms: numOrUndef(e.target.value) })}
                className={inputCls}
              />
            </div>
            <div className="space-y-1">
              <label className="text-[11px] font-medium text-text-secondary">{t(`${s}.llmMaxTokens`)}</label>
              <input
                type="number"
                placeholder="10"
                value={value.llm_max_tokens ?? ''}
                onChange={(e) => update({ llm_max_tokens: numOrUndef(e.target.value) })}
                className={inputCls}
              />
            </div>
          </div>
        </div>
      )}
    </ConfigSection>
  )
}
