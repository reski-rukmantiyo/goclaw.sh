import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Save, Loader2 } from "lucide-react";
import { Button } from "@/components/ui/button";
import type { ChannelInstanceData } from "@/types/channel";
import type { ChannelContact } from "@/types/contact";
import type { AgentData } from "@/types/agent";
import {
  WhatsAppContactOverrides,
  type WhatsAppContactConfigValues,
} from "../whatsapp-contact-overrides";

interface ChannelContactsTabProps {
  instance: ChannelInstanceData;
  onUpdate: (updates: Record<string, unknown>) => Promise<void>;
  listContacts: (search: string, channelType?: string, peerKind?: string, contactType?: string) => Promise<ChannelContact[]>;
  agents: AgentData[];
}

// ChannelContactsTab hosts the per-contact DM agent-routing editor (SRS 015 FR-04).
// WhatsApp-only — Telegram and other channels route via their own group/topic maps.
export function ChannelContactsTab({
  instance,
  onUpdate,
  listContacts,
  agents,
}: ChannelContactsTabProps) {
  const { t } = useTranslation("channels");
  const config = (instance.config ?? {}) as Record<string, unknown>;
  const [contacts, setContacts] = useState<Record<string, WhatsAppContactConfigValues>>(
    (config.contacts as Record<string, WhatsAppContactConfigValues>) ?? {},
  );
  const [saving, setSaving] = useState(false);

  const handleSave = async () => {
    // Omit an empty contacts map — mirrors the groups undefined cleanup so no
    // stale `contacts: {}` payload is written.
    const hasContacts = Object.keys(contacts).length > 0;
    const updatedConfig = {
      ...config,
      contacts: hasContacts ? contacts : undefined,
    };
    const cleanConfig = Object.fromEntries(
      Object.entries(updatedConfig).filter(([, v]) => v !== undefined),
    );

    setSaving(true);
    try {
      await onUpdate({
        config: Object.keys(cleanConfig).length > 0 ? cleanConfig : null,
      });
    } catch {
      // toast shown by hook
    } finally {
      setSaving(false);
    }
  };

  return (
    <div className="space-y-6">
      <WhatsAppContactOverrides
        contacts={contacts}
        onChange={setContacts}
        listContacts={listContacts}
        agents={agents}
      />

      <div className="flex items-center justify-end gap-2">
        <Button onClick={handleSave} disabled={saving}>
          {saving ? (
            <Loader2 className="h-4 w-4 animate-spin" />
          ) : (
            <Save className="h-4 w-4" />
          )}
          {saving
            ? t("whatsappContactOverrides.saving")
            : t("whatsappContactOverrides.saveContacts")}
        </Button>
      </div>
    </div>
  );
}
