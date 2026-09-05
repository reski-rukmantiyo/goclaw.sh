// Pure helpers for WhatsApp per-contact DM overrides (SRS 015 FR-04).
// Extracted from the component so the validation logic is unit-testable
// without @testing-library/react (same convention as raw-messages scope-helpers).

/** WhatsApp contact override map keys must be phone JIDs: <digits>@s.whatsapp.net */
export const WHATSAPP_PHONE_JID_PATTERN = /^\d+@s\.whatsapp\.net$/;

/** True when jid is a well-formed WhatsApp phone JID (manual-entry validation). */
export function isValidWhatsAppPhoneJid(jid: string): boolean {
  return WHATSAPP_PHONE_JID_PATTERN.test(jid.trim());
}
