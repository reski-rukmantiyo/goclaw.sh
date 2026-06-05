/**
 * User display formatting utilities per SRS v2.0 §6.8.
 */

const PROVIDER_DISPLAY: Record<string, string> = {
  local: "local",
  google: "Google",
  entra_id: "Entra",
  oauth2: "OAuth2",
};

/**
 * Map auth_provider to SRS v2.0 display value.
 * Returns "—" for unknown/missing providers.
 */
export function formatAuthProvider(provider: string | undefined): string {
  if (!provider) return "—";
  return PROVIDER_DISPLAY[provider] ?? "—";
}

/**
 * Map user status to SRS v2.0 two-value display: Active / Inactive.
 */
export function formatUserStatus(status: string | undefined): string {
  if (status === "active") return "Active";
  return "Inactive";
}
