import { useTranslation } from "react-i18next";
import { Badge } from "@/components/ui/badge";
import { Radio, Users } from "lucide-react";
import { useChannelInstances } from "@/pages/channels/hooks/use-channel-instances";
import type { ChannelInstanceData } from "@/types/channel";

interface WhatsAppGroupConfig {
  name?: string;
  agent_id?: string;
  enabled?: boolean;
}

interface MatchedGroup {
  jid: string;
  name?: string;
  inherited?: boolean;
}

interface BoundEntry {
  instance: ChannelInstanceData;
  groups: MatchedGroup[];
}

function getWhatsAppGroups(
  config: Record<string, unknown> | null,
): Record<string, WhatsAppGroupConfig> {
  if (!config?.groups) return {};
  return (config.groups as Record<string, WhatsAppGroupConfig>) ?? {};
}

interface BoundChannelsSectionProps {
  agentId: string;
  agentKey: string;
}

export function BoundChannelsSection({ agentId, agentKey }: BoundChannelsSectionProps) {
  const { t } = useTranslation("agents");
  const s = "configSections.boundChannels";
  const { instances } = useChannelInstances();

  const bound: BoundEntry[] = [];

  for (const inst of instances) {
    const config = inst.config as Record<string, unknown> | null;

    if (inst.agent_id === agentId) {
      // Agent is the channel's default agent (UUID match)
      const groups: MatchedGroup[] = [];
      if (inst.channel_type === "whatsapp" && config) {
        const waGroups = getWhatsAppGroups(config);
        for (const [jid, cfg] of Object.entries(waGroups)) {
          if (cfg.agent_id && cfg.agent_id !== agentKey) continue;
          groups.push({ jid, name: cfg.name });
        }
      }
      bound.push({ instance: inst, groups });
    } else if (inst.channel_type === "whatsapp" && config) {
      // Check per-group overrides for agent_key match
      const waGroups = getWhatsAppGroups(config);
      const matched: MatchedGroup[] = [];
      for (const [jid, cfg] of Object.entries(waGroups)) {
        if (cfg.agent_id === agentKey) {
          matched.push({ jid, name: cfg.name });
        }
      }
      if (matched.length > 0) {
        bound.push({ instance: inst, groups: matched });
      }
    }
  }

  return (
    <section className="space-y-3">
      <div className="flex items-center gap-2">
        <div className="flex h-8 w-8 items-center justify-center rounded-lg bg-blue-100 dark:bg-blue-900/30">
          <Radio className="h-4 w-4 text-blue-600 dark:text-blue-400" />
        </div>
        <div>
          <h3 className="text-sm font-semibold">{t(`${s}.title`)}</h3>
          <p className="text-xs text-muted-foreground">{t(`${s}.description`)}</p>
        </div>
      </div>

      {bound.length > 0 ? (
        <div className="flex flex-wrap gap-1.5">
          {bound.map((entry) => {
            const inst = entry.instance;
            const hasGroups = entry.groups.length > 0;

            return (
              <div key={inst.id}>
                <Badge
                  variant={inst.enabled ? "default" : "secondary"}
                  className="gap-1"
                >
                  {inst.display_name || inst.name}
                  <span className="text-xs opacity-70">{inst.channel_type}</span>
                  {!inst.enabled && (
                    <span className="text-xs opacity-50">({t(`${s}.disabled`)})</span>
                  )}
                </Badge>

                {hasGroups && (
                  <div className="mt-1 ml-4 flex flex-wrap gap-1">
                    {entry.groups.map((g) => (
                      <Badge
                        key={g.jid}
                        variant="secondary"
                        className="gap-1 text-[11px]"
                      >
                        <Users className="h-3 w-3" />
                        {g.name || g.jid}
                        {g.inherited && (
                          <span className="text-[10px] opacity-50">({t(`${s}.inheritedGroup`)})</span>
                        )}
                      </Badge>
                    ))}
                  </div>
                )}
              </div>
            );
          })}
        </div>
      ) : (
        <p className="text-xs text-muted-foreground italic">{t(`${s}.empty`)}</p>
      )}
    </section>
  );
}
