import type { Spec } from "../spec.ts";
import type { RunConfig, TrackedRole } from "./run-config.ts";

export type Evidence = "page" | "search";
export type AlertLabel = "target" | "best-so-far";

export interface PriceObservation {
  price: number;
  currency: string;
  evidence: Evidence;
  asOf?: string | undefined;
}

export type AlertDecision =
  | { kind: "alert"; label: AlertLabel }
  | { kind: "note-only"; reason: string }
  | { kind: "ignore"; reason: string };

export function listingAlertKey(listingId: string): string {
  return listingId;
}

export function productAlertKey(url: string, price: number): string {
  return `${url}@${price}`;
}

function targetThreshold(role: TrackedRole, target: number, config: RunConfig): number {
  return role === "upgrades" ? target * config.alert.upgradesTargetRatio : target;
}

export function decideAlert(options: {
  role: TrackedRole;
  spec: Spec;
  observation: PriceObservation;
  bestPrice: number | undefined;
  alreadyAlerted: boolean;
  config: RunConfig;
}): AlertDecision {
  const { role, spec, observation, bestPrice, alreadyAlerted, config } = options;
  const { targetPrice, maxPrice } = spec;
  if (targetPrice === null || maxPrice === null) return { kind: "ignore", reason: "spec has no prices" };
  if (observation.currency !== targetPrice.currency || observation.currency !== maxPrice.currency) {
    return { kind: "note-only", reason: `currency ${observation.currency} differs from spec` };
  }
  if (observation.price > maxPrice.amount) return { kind: "ignore", reason: "above max price" };
  if (alreadyAlerted) return { kind: "ignore", reason: "already alerted" };

  if (observation.price <= targetThreshold(role, targetPrice.amount, config)) return { kind: "alert", label: "target" };
  if (role === "escalate" && bestPrice !== undefined && observation.price < bestPrice) {
    return { kind: "alert", label: "best-so-far" };
  }
  return { kind: "note-only", reason: "between target and max" };
}

export function nextBestPrice(current: number | undefined, observation: PriceObservation, spec: Spec): number | undefined {
  const { maxPrice } = spec;
  const valid = maxPrice !== null && observation.currency === maxPrice.currency && observation.price <= maxPrice.amount;
  if (!valid) return current;
  return current === undefined ? observation.price : Math.min(current, observation.price);
}

export interface AlertMessageInput {
  itemName: string;
  title: string;
  url: string;
  cardUrl: string;
  observation: PriceObservation;
  label: AlertLabel;
  targetPrice: { amount: number; currency: string };
}

export function formatAlertMessage(input: AlertMessageInput): string {
  const { observation, targetPrice } = input;
  const lines = [
    `Deal: ${input.itemName}${input.label === "best-so-far" ? " (best price so far)" : ""}`,
    `${observation.price} ${observation.currency} (target ${targetPrice.amount} ${targetPrice.currency})`,
    input.title,
    input.url,
  ];
  if (observation.evidence === "search") {
    lines.push(`Seen via search, as of ${observation.asOf ?? "unknown date"}. Verify on site before buying.`);
  } else {
    lines.push("Price read from the page.");
  }
  if (input.cardUrl !== "") lines.push(`Card: ${input.cardUrl}`);
  return lines.join("\n");
}
