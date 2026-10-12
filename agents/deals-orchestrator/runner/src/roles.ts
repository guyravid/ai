export const LIST_ROLES = ["to-buy", "escalate", "upgrades", "bought", "source-library"] as const;
export type ListRole = (typeof LIST_ROLES)[number];

export const TRACKED_ROLES: readonly ListRole[] = ["to-buy", "escalate", "upgrades"];

export const ITEM_ROLES: readonly ListRole[] = ["to-buy", "escalate", "upgrades", "bought"];
