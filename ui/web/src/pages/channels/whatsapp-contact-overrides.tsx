import { useState, useEffect, useCallback } from "react";
import { useTranslation } from "react-i18next";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Switch } from "@/components/ui/switch";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Plus, Trash2 } from "lucide-react";
import type { ChannelContact } from "@/types/contact";
import type { AgentData } from "@/types/agent";
import { isValidWhatsAppPhoneJid } from "./contact-jid";

export interface WhatsAppContactConfigValues {
  name?: string;
  agent_id?: string;
  enabled?: boolean;
}

interface Props {
  contacts: Record<string, WhatsAppContactConfigValues>;
  onChange: (contacts: Record<string, WhatsAppContactConfigValues>) => void;
  listContacts: (search: string, channelType?: string, peerKind?: string, contactType?: string) => Promise<ChannelContact[]>;
  agents: AgentData[];
}

// WhatsAppContactOverrides edits the per-contact DM agent-routing map (SRS 015),
// mirroring WhatsAppGroupOverrides. Lookup keys are phone JIDs
// (<number>@s.whatsapp.net) — discovered contacts insert them verbatim; manual
// entry is validated against that pattern so local-format numbers never match.
export function WhatsAppContactOverrides({ contacts, onChange, listContacts, agents }: Props) {
  const { t } = useTranslation("channels");
  const [newContactJid, setNewContactJid] = useState("");
  const [jidError, setJidError] = useState(false);
  const [knownContacts, setKnownContacts] = useState<ChannelContact[]>([]);

  const contactJids = Object.keys(contacts);

  const addContact = (jid: string, name?: string) => {
    const key = jid.trim();
    if (!key || contacts[key]) return;
    onChange({ ...contacts, [key]: name ? { name } : {} });
  };

  const removeContact = (jid: string) => {
    const next = { ...contacts };
    delete next[jid];
    onChange(next);
  };

  const updateContact = (jid: string, config: WhatsAppContactConfigValues) => {
    onChange({ ...contacts, [jid]: config });
  };

  const handleManualAdd = () => {
    const jid = newContactJid.trim();
    if (!jid) return;
    if (!isValidWhatsAppPhoneJid(jid)) {
      setJidError(true);
      return;
    }
    setJidError(false);
    addContact(jid);
    setNewContactJid("");
  };

  // Load discovered WhatsApp DM contacts (peer_kind=direct, contact_type=user).
  const loadKnownContacts = useCallback(async () => {
    try {
      const list = await listContacts("", "whatsapp", "direct", "user");
      setKnownContacts(list);
    } catch {
      /* handled by http hook */
    }
  }, [listContacts]);

  useEffect(() => {
    loadKnownContacts();
  }, [loadKnownContacts]);

  // Known contacts not yet added as overrides
  const availableContacts = knownContacts.filter((ct) => !contacts[ct.sender_id]);

  return (
    <fieldset className="rounded-md border p-3 space-y-3">
      <legend className="px-1 text-sm font-medium">
        {t("whatsappContactOverrides.title")}
      </legend>
      <p className="text-xs text-muted-foreground">
        {t("whatsappContactOverrides.hint")}
      </p>

      {contactJids.map((jid) => {
        const contact = contacts[jid] ?? {};
        return (
          <div key={jid} className="rounded-md border p-3 space-y-3">
            <div className="flex items-center justify-between gap-2">
              <div className="min-w-0">
                <p className="text-sm font-medium truncate">{contact.name || jid}</p>
                <p className="font-mono text-xs text-muted-foreground truncate">{jid}</p>
              </div>
              <Button
                type="button"
                variant="ghost"
                size="sm"
                className="h-7 w-7 p-0 text-muted-foreground hover:text-destructive"
                onClick={() => removeContact(jid)}
              >
                <Trash2 className="h-4 w-4" />
              </Button>
            </div>

            <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
              {/* Display name */}
              <div className="space-y-1.5">
                <label className="text-xs font-medium text-muted-foreground">
                  {t("whatsappContactOverrides.nameLabel")}
                </label>
                <Input
                  value={contact.name ?? ""}
                  onChange={(e) =>
                    updateContact(jid, { ...contact, name: e.target.value || undefined })
                  }
                  className="h-9 text-base md:text-sm"
                />
              </div>
              {/* Agent selector */}
              <div className="space-y-1.5">
                <label className="text-xs font-medium text-muted-foreground">
                  {t("whatsappContactOverrides.agent")}
                </label>
                <Select
                  value={contact.agent_id ?? "__default__"}
                  onValueChange={(val) =>
                    updateContact(jid, {
                      ...contact,
                      agent_id:
                        val === "__default__" || !val ? undefined : val,
                    })
                  }
                >
                  <SelectTrigger className="h-9 text-base md:text-sm">
                    <SelectValue placeholder={t("whatsappContactOverrides.defaultAgent")} />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="__default__">
                      {t("whatsappContactOverrides.defaultAgent")}
                    </SelectItem>
                    {agents.map((a) => (
                      <SelectItem key={a.id} value={a.agent_key}>
                        {a.display_name || a.agent_key}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>
            </div>

            {/* Enabled toggle */}
            <div className="flex items-center justify-between gap-2">
              <div className="space-y-0.5">
                <label className="text-xs font-medium text-muted-foreground">
                  {t("whatsappContactOverrides.enabled")}
                </label>
                <p className="text-xs text-muted-foreground/70">
                  {t("whatsappContactOverrides.enabledHint")}
                </p>
              </div>
              <Switch
                checked={contact.enabled ?? true}
                onCheckedChange={(val) =>
                  updateContact(jid, { ...contact, enabled: val ? undefined : false })
                }
              />
            </div>
          </div>
        );
      })}

      {/* Discovered contacts quick-add */}
      {availableContacts.length > 0 && (
        <div className="space-y-1.5">
          <p className="text-xs font-medium text-muted-foreground">
            {t("whatsappContactOverrides.knownContacts")}
          </p>
          <div className="flex flex-wrap gap-1.5">
            {availableContacts.map((ct) => (
              <button
                key={ct.id}
                type="button"
                onClick={() => addContact(ct.sender_id, ct.display_name || undefined)}
                className="inline-flex items-center gap-1 rounded-md border px-2 py-1 text-xs hover:bg-muted/50 transition-colors"
              >
                <Plus className="h-3 w-3" />
                <span>{ct.display_name || ct.sender_id}</span>
              </button>
            ))}
          </div>
        </div>
      )}

      {/* Manual JID entry */}
      <div className="space-y-1.5">
        <div className="flex items-center gap-2">
          <Input
            value={newContactJid}
            onChange={(e) => {
              setNewContactJid(e.target.value);
              if (jidError) setJidError(false);
            }}
            placeholder={t("whatsappContactOverrides.contactJid")}
            className="h-8 flex-1 text-base md:text-sm"
            onKeyDown={(e) => {
              if (e.key === "Enter") {
                e.preventDefault();
                handleManualAdd();
              }
            }}
          />
          <Button
            type="button"
            variant="outline"
            size="sm"
            className="h-8"
            onClick={handleManualAdd}
            disabled={!newContactJid.trim()}
          >
            <Plus className="h-3.5 w-3.5 mr-1" />
            {t("whatsappContactOverrides.addContact")}
          </Button>
        </div>
        {jidError && (
          <p className="text-xs text-destructive">
            {t("whatsappContactOverrides.invalidJid")}
          </p>
        )}
      </div>
    </fieldset>
  );
}
