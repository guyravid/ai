const ODESLI_BASE = "https://api.song.link/v1-alpha.1/links";

export async function fetchOdesli(
  url: string,
  userAgent: string,
  fetcher: typeof fetch = fetch
): Promise<Response> {
  const endpoint = `${ODESLI_BASE}?url=${encodeURIComponent(url)}&userCountry=CA`;
  return fetcher(endpoint, {
    headers: { "User-Agent": userAgent },
  });
}
