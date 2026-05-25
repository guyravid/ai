import { describe, it, expect, vi, beforeEach } from "vitest";

const mockModels = [
  { name: "models/gemini-2.5-flash", displayName: "Gemini 2.5 Flash" },
  { name: "models/gemini-2.5-pro", displayName: "Gemini 2.5 Pro" },
];

const mockList = vi.hoisted(() => vi.fn());

vi.mock("@google/genai", () => ({
  GoogleGenAI: vi.fn(function () {
    return { models: { list: mockList } };
  }),
}));

// Import after mock is registered
const { handleListModels, listModelsTool } = await import("../../src/tools/list_models.js");

describe("listModelsTool definition", () => {
  it("has correct name and no required inputs", () => {
    expect(listModelsTool.name).toBe("list_models");
    expect(listModelsTool.inputSchema.properties).toEqual({});
  });
});

describe("handleListModels", () => {
  beforeEach(() => {
    process.env.GEMINI_API_KEY = "test-key";
    mockList.mockReturnValue({
      [Symbol.asyncIterator]: async function* () {
        for (const m of mockModels) yield m;
      },
    });
  });

  it("returns JSON list of models", async () => {
    const result = await handleListModels({});
    expect(result.content).toHaveLength(1);
    expect(result.content[0].type).toBe("text");
    const parsed = JSON.parse(result.content[0].text as string);
    expect(parsed).toEqual(mockModels);
  });

  it("propagates API errors", async () => {
    mockList.mockReturnValue({
      [Symbol.asyncIterator]: async function* () {
        throw new Error("API unavailable");
      },
    });

    await expect(handleListModels({})).rejects.toThrow("Failed to list models: API unavailable");
  });
});
