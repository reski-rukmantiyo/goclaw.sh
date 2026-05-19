import { useState, useEffect, useMemo, useCallback } from 'react'
import { useTranslation } from 'react-i18next'
import { ConfigSection } from './config-section'
import { Combobox } from '../common/Combobox'
import { useProviders } from '../../hooks/use-providers'
import { getApiClient } from '../../lib/api'
import { numOrUndef } from '../../lib/format'
import type { TopicGuardConfig } from '../../types/agent'

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

  const tagsToArray = (s: string): string[] | undefined => {
    const trimmed = s.trim()
    if (!trimmed) return undefined
    return trimmed.split(',').map((t) => t.trim()).filter(Boolean)
  }
  const arrayToTags = (arr?: string[]): string => arr?.join(', ') ?? ''

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

      {/* Allow keywords */}
      <div className="space-y-1">
        <label className="text-[11px] font-medium text-text-secondary">{t(`${s}.allowKeywords`)}</label>
        <input
          type="text"
          placeholder={t(`${s}.keywordsPlaceholder`)}
          value={arrayToTags(value.allow_keywords)}
          onChange={(e) => update({ allow_keywords: tagsToArray(e.target.value) })}
          className={inputCls}
        />
      </div>

      {/* Block keywords */}
      <div className="space-y-1">
        <label className="text-[11px] font-medium text-text-secondary">{t(`${s}.blockKeywords`)}</label>
        <input
          type="text"
          placeholder={t(`${s}.keywordsPlaceholder`)}
          value={arrayToTags(value.block_keywords)}
          onChange={(e) => update({ block_keywords: tagsToArray(e.target.value) })}
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
