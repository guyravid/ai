export const BOT_WALL_MAX_BYTES = 20 * 1024;

interface BotWallPattern {
  name: string;
  matches: (body: string) => boolean;
}

const BOT_WALL_PATTERNS: BotWallPattern[] = [
  { name: "akamai-access-denied", matches: (body) => /access denied/i.test(body) && /edgesuite|akamai/i.test(body) },
  { name: "cloudflare-attention", matches: (body) => /attention required!?\s*\|\s*cloudflare/i.test(body) },
  { name: "cloudflare-challenge", matches: (body) => /<title>\s*just a moment\.{0,3}\s*<\/title>/i.test(body) },
  { name: "incapsula-interruption", matches: (body) => /pardon our interruption/i.test(body) },
  { name: "datadome", matches: (body) => /captcha-delivery\.com|datadome/i.test(body) },
  { name: "perimeterx", matches: (body) => /perimeterx|px-captcha|px-cdn\.net/i.test(body) },
  { name: "captcha-widget", matches: (body) => /g-recaptcha|h-captcha|cf-turnstile/i.test(body) },
];

export function detectBotWall(body: string, byteLength: number): string | null {
  if (byteLength >= BOT_WALL_MAX_BYTES) return null;
  return BOT_WALL_PATTERNS.find((pattern) => pattern.matches(body))?.name ?? null;
}
