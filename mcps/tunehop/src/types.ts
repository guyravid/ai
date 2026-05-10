export type MatchConfidence = "exact" | "closest";

export interface OdesliPlatformLink {
  url: string;
  nativeAppUriMobile?: string;
  nativeAppUriDesktop?: string;
  entityUniqueId: string;
}

export interface OdesliEntity {
  id: string;
  type: "song" | "album";
  title?: string;
  artistName?: string;
  albumName?: string;
  thumbnailUrl?: string;
  apiProvider: string;
  platforms: string[];
}

export interface OdesliResponse {
  entityUniqueId: string;
  userCountry: string;
  pageUrl: string;
  entitiesByUniqueId: Record<string, OdesliEntity>;
  linksByPlatform: Record<string, OdesliPlatformLink>;
}

export interface ConvertMusicLinkOutput {
  title: string | null;
  artistName: string | null;
  albumName: string | null;
  type: "song" | "album";
  links: Record<string, string>;
  universalLink: string;
  matchConfidence: MatchConfidence;
}
