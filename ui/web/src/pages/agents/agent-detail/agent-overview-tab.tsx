import { useState, useEffect } from "react";
import { useTranslation } from "react-i18next";
import type {
  AgentData, MemoryConfig, SubagentsConfig, ToolPolicyConfig,
} from "@/types/agent";
import { StickySaveBar } from "@/components/shared/sticky-save-bar";
import { PersonalitySection } from "./overview-sections/personality-section";
import { ModelBudgetSection } from "./overview-sections/model-budget-section";
import { SkillsSection } from "./overview-sections/skills-section";
import { EvolutionSection } from "./overview-sections/evolution-section";
import { PromptSettingsSection } from "./overview-sections/prompt-settings-section";
import { PinnedSkillsSection } from "./overview-sections/pinned-skills-section";
import { OrchestrationSection } from "./overview-sections/orchestration-section";
import { CapabilitiesSection } from "./overview-sections/capabilities-section";
import { ScopeGuardrailsSection, readScopeGuardrails, type ScopeGuardrailsConfig } from "./overview-sections/scope-guardrails-section";
import { ChatGPTOAuthRoutingSummarySection } from "./overview-sections/chatgpt-oauth-routing-summary-section";
import { HeartbeatCard } from "./overview-sections/heartbeat-card";
import { HooksSummaryCard } from "./overview-sections/hooks-summary-card";
import { MemorySection } from "./config-sections";
import { readPromptMode } from "./agent-display-utils";
import type { PromptMode } from "../prompt-mode-cards";
import type { UseAgentHeartbeatReturn } from "../hooks/use-agent-heartbeat";

interface AgentOverviewTabProps {
  agent: AgentData;
  onUpdate: (updates: Record<string, unknown>) => Promise<void>;
  heartbeat: UseAgentHeartbeatReturn;
  onManageCodexPool: () => void;
  onViewHooks: () => void;
  onAddHook: () => void;
}

export function AgentOverviewTab({ agent, onUpdate, heartbeat, onManageCodexPool, onViewHooks, onAddHook }: AgentOverviewTabProps) {
  const { t } = useTranslation("agents");

  // Personality
  const [emoji, setEmoji] = useState(agent.emoji ?? "");
  const [displayName, setDisplayName] = useState(agent.display_name ?? "");
  const [frontmatter, setFrontmatter] = useState(agent.frontmatter ?? "");
  const [status, setStatus] = useState(agent.status);
  const [isDefault, setIsDefault] = useState(agent.is_default);

  // Model & Budget
  const [provider, setProvider] = useState(agent.provider);
  const [model, setModel] = useState(agent.model);
  const [contextWindow, setContextWindow] = useState(agent.context_window || 200000);
  const [maxToolIterations, setMaxToolIterations] = useState(agent.max_tool_iterations || 20);
  const [budgetDollars, setBudgetDollars] = useState(
    agent.budget_monthly_cents ? String(agent.budget_monthly_cents / 100) : "",
  );
  // Evolution (predefined only)
  const [selfEvolve, setSelfEvolve] = useState(Boolean(agent.self_evolve));
  const [skillEvolve, setSkillEvolve] = useState(Boolean(agent.skill_evolve));
  const [skillNudgeInterval, setSkillNudgeInterval] = useState(
    typeof agent.skill_nudge_interval === "number" ? agent.skill_nudge_interval : 15,
  );

  // Memory (always shown — per-agent overrides, empty = use system defaults)
  const [mem, setMem] = useState<MemoryConfig>(agent.memory_config ?? {});

  // Capabilities (subagents + tool policy)
  const [subEnabled, setSubEnabled] = useState(agent.subagents_config != null);
  const [sub, setSub] = useState<SubagentsConfig>(agent.subagents_config ?? {});
  const [toolsEnabled, setToolsEnabled] = useState(agent.tools_config != null);
  const [tools, setTools] = useState<ToolPolicyConfig>(agent.tools_config ?? {});

  // Prompt settings (prompt_mode + TTS) — lifted for unified save
  const savedOtherConfig = (agent.other_config ?? {}) as Record<string, unknown>;
  const [promptMode, setPromptMode] = useState<PromptMode>(readPromptMode(agent) as PromptMode);

  // Scope Guardrails — lifted for unified save
  const savedScope = readScopeGuardrails(agent);
  const [scopeEnabled, setScopeEnabled] = useState(Boolean(savedScope.enabled));
  const [scopeEnforcement, setScopeEnforcement] = useState(savedScope.enforcement || "strict");
  const [scopeDescription, setScopeDescription] = useState(savedScope.scope_description || "");
  const [scopeAllowed, setScopeAllowed] = useState<string[]>(savedScope.allowed_topics || []);
  const [scopeDenied, setScopeDenied] = useState<string[]>(savedScope.denied_topics || []);
  const [scopeOffTopic, setScopeOffTopic] = useState(savedScope.off_topic_response || "");

  // Reset scope state when agent.other_config changes (after save)
  useEffect(() => {
    const s = readScopeGuardrails(agent);
    setScopeEnabled(Boolean(s.enabled));
    setScopeEnforcement(s.enforcement || "strict");
    setScopeDescription(s.scope_description || "");
    setScopeAllowed(s.allowed_topics || []);
    setScopeDenied(s.denied_topics || []);
    setScopeOffTopic(s.off_topic_response || "");
  }, [agent.other_config]);

  // Save state
  const [saving, setSaving] = useState(false);
  const [llmSaveBlocked, setLlmSaveBlocked] = useState(false);

  // Dirty checks for PromptSettingsSection (prompt_mode only — TTS has its own save)
  const savedPromptMode = readPromptMode(agent) as PromptMode;
  const promptDirty = promptMode !== savedPromptMode;

  // Dirty check for scope guardrails
  const scopeDirty = JSON.stringify(readScopeGuardrails(agent)) !== JSON.stringify({
    enabled: scopeEnabled,
    enforcement: scopeEnforcement,
    scope_description: scopeDescription,
    allowed_topics: scopeAllowed,
    denied_topics: scopeDenied,
    off_topic_response: scopeOffTopic,
  });

  const handleSave = async () => {
    setSaving(true);
    try {
      const budgetCents = budgetDollars ? Math.round(parseFloat(budgetDollars) * 100) : null;
      const updates: Record<string, unknown> = {
        display_name: displayName,
        frontmatter: frontmatter || null,
        provider,
        model,
        context_window: contextWindow,
        max_tool_iterations: maxToolIterations,
        status,
        is_default: isDefault,
        budget_monthly_cents: budgetCents,
        memory_config: mem,
        subagents_config: subEnabled ? sub : null,
        tools_config: toolsEnabled
          ? { profile: tools.profile, allow: tools.allow, deny: tools.deny, alsoAllow: tools.alsoAllow, byProvider: tools.byProvider }
          : {},
        // Promoted fields sent at top level (NOT NULL columns — send "" not null)
        emoji: emoji.trim(),
        self_evolve: selfEvolve,
        skill_evolve: skillEvolve,
        skill_nudge_interval: skillEvolve ? skillNudgeInterval : 15,
      };
      // When the provider changes, clear stale pool routing config so it
      // doesn't reference members from the previous provider's pool.
      if (provider !== agent.provider) {
        updates.chatgpt_oauth_routing = null;
      }

      // Merge other_config: prompt_mode + scope_guardrails
      const bag = { ...savedOtherConfig };
      // Prompt mode
      if (promptMode && promptMode !== "full") {
        bag.prompt_mode = promptMode;
      } else {
        delete bag.prompt_mode;
      }
      // Scope guardrails
      if (scopeEnabled) {
        const sg: ScopeGuardrailsConfig = { enabled: true, enforcement: scopeEnforcement };
        if (scopeDescription.trim()) sg.scope_description = scopeDescription.trim();
        if (scopeAllowed.length > 0) sg.allowed_topics = scopeAllowed;
        if (scopeDenied.length > 0) sg.denied_topics = scopeDenied;
        if (scopeOffTopic.trim()) sg.off_topic_response = scopeOffTopic.trim();
        bag.scope_guardrails = sg;
      } else {
        delete bag.scope_guardrails;
      }
      updates.other_config = bag;

      await onUpdate(updates);
    } catch {
      // toast shown by hook
    } finally {
      setSaving(false);
    }
  };

  // Unified dirty: personality, model, evolution, memory, capabilities, prompt, scope
  const personalityDirty =
    (emoji ?? "") !== (agent.emoji ?? "") ||
    displayName !== (agent.display_name ?? "") ||
    frontmatter !== (agent.frontmatter ?? "") ||
    status !== agent.status ||
    isDefault !== agent.is_default;
  const modelDirty =
    provider !== agent.provider ||
    model !== agent.model ||
    contextWindow !== (agent.context_window || 200000) ||
    maxToolIterations !== (agent.max_tool_iterations || 20) ||
    budgetDollars !== (agent.budget_monthly_cents ? String(agent.budget_monthly_cents / 100) : "");
  const evDirty =
    Boolean(agent.self_evolve) !== selfEvolve ||
    Boolean(agent.skill_evolve) !== skillEvolve ||
    (skillEvolve ? skillNudgeInterval : 15) !== (typeof agent.skill_nudge_interval === "number" ? agent.skill_nudge_interval : 15);
  const memDirty = JSON.stringify(mem) !== JSON.stringify(agent.memory_config ?? {});
  const subDirty = subEnabled !== (agent.subagents_config != null) || JSON.stringify(sub) !== JSON.stringify(agent.subagents_config ?? {});
  const toolsDirty = toolsEnabled !== (agent.tools_config != null) || JSON.stringify(tools) !== JSON.stringify(agent.tools_config ?? {});

  const hasChanges = personalityDirty || modelDirty || evDirty || memDirty || subDirty || toolsDirty || promptDirty || scopeDirty;

  return (
    <div className="space-y-4">
      <PromptSettingsSection agent={agent} onUpdate={onUpdate} promptMode={promptMode} onPromptModeChange={setPromptMode} />

      <PersonalitySection
        agentKey={agent.agent_key}
        emoji={emoji}
        onEmojiChange={setEmoji}
        displayName={displayName}
        onDisplayNameChange={setDisplayName}
        frontmatter={frontmatter}
        onFrontmatterChange={setFrontmatter}
        status={status}
        onStatusChange={setStatus}
        isDefault={isDefault}
        onIsDefaultChange={setIsDefault}
      />

      <ScopeGuardrailsSection
        agent={agent}
        frontmatter={frontmatter}
        enabled={scopeEnabled}
        onEnabledChange={setScopeEnabled}
        enforcement={scopeEnforcement}
        onEnforcementChange={setScopeEnforcement}
        scopeDescription={scopeDescription}
        onScopeDescriptionChange={setScopeDescription}
        allowedTopics={scopeAllowed}
        onAllowedTopicsChange={setScopeAllowed}
        deniedTopics={scopeDenied}
        onDeniedTopicsChange={setScopeDenied}
        offTopicResponse={scopeOffTopic}
        onOffTopicResponseChange={setScopeOffTopic}
      />

      <ModelBudgetSection
        provider={provider}
        onProviderChange={setProvider}
        model={model}
        onModelChange={setModel}
        contextWindow={contextWindow}
        onContextWindowChange={setContextWindow}
        maxToolIterations={maxToolIterations}
        onMaxToolIterationsChange={setMaxToolIterations}
        savedProvider={agent.provider}
        savedModel={agent.model}
        budgetDollars={budgetDollars}
        onBudgetDollarsChange={setBudgetDollars}
        onSaveBlockedChange={setLlmSaveBlocked}
      />

      <ChatGPTOAuthRoutingSummarySection agent={agent} onManage={onManageCodexPool} />
      {provider !== agent.provider && !!agent.chatgpt_oauth_routing && (
        <p className="text-xs text-amber-600 dark:text-amber-400 -mt-2 px-1">
          {t("chatgptOAuthRouting.providerChangedWarning")}
        </p>
      )}

      {agent.agent_type === "predefined" && (
        <EvolutionSection
          agentId={agent.id}
          selfEvolve={selfEvolve}
          onSelfEvolveChange={setSelfEvolve}
          skillEvolve={skillEvolve}
          onSkillEvolveChange={setSkillEvolve}
          skillNudgeInterval={skillNudgeInterval}
          onSkillNudgeIntervalChange={setSkillNudgeInterval}
        />
      )}

      {/* Memory — always visible, per-agent overrides */}
      <MemorySection
        value={mem}
        onChange={setMem}
      />

      <HeartbeatCard heartbeat={heartbeat} />

      <HooksSummaryCard
        agentId={agent.id}
        onViewAll={onViewHooks}
        onAddHook={onAddHook}
      />

      <SkillsSection agentId={agent.id} />
      <PinnedSkillsSection agent={agent} onUpdate={onUpdate} />

      <OrchestrationSection agentId={agent.id} />

      <CapabilitiesSection
        subEnabled={subEnabled}
        sub={sub}
        onSubToggle={setSubEnabled}
        onSubChange={setSub}
        toolsEnabled={toolsEnabled}
        tools={tools}
        onToolsToggle={setToolsEnabled}
        onToolsChange={setTools}
      />

      <StickySaveBar
        onSave={handleSave}
        saving={saving}
        disabled={llmSaveBlocked || !hasChanges}
        label={t("general.saveChanges")}
        savingLabel={t("general.saving")}
      />
    </div>
  );
}
