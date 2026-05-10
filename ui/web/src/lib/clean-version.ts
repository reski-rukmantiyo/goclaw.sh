// Strip git commit count + hash from version string.
// "v2.5.1-3-g4fd653c1" → "v2.5.1-3-g4fd653c1", "v3.11.3C-rclaw" → "v3.11.3C-rclaw", "dev" → "dev"
export function cleanVersion(v: string): string {
  const match = v.match(/^(v?\d+\.\d+\.\d+[a-zA-Z0-9._-]*)/);
  return match?.[1] ?? v;
}
