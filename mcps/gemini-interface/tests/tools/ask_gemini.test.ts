import { describe, it, expect, vi, beforeEach } from "vitest";

const mockGenerateContent = vi.hoisted(() => vi.fn());

vi.mock("@google/genai", () => ({
  GoogleGenAI: vi.fn(function () {
    return { models: { generateContent: mockGenerateContent } };
  }),
}));

const { handleAskGemini, askGeminiTool } = await import("../../src/tools/ask_gemini.js");

describe("askGeminiTool definition", () => {
  it("has correct name and requires prompt", () => {
    expect(askGeminiTool.name).toBe("ask_gemini");
    expect(askGeminiTool.inputSchema.required).toContain("prompt");
  });
});

describe("handleAskGemini", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    process.env.GEMINI_API_KEY = "test-key";
    mockGenerateContent.mockResolvedValue({ text: "Hello from Gemini" });
  });

  it("returns model response text", async () => {
    const result = await handleAskGemini({ prompt: "Say hi" });
    expect(result.content[0].type).toBe("text");
    expect(result.content[0].text).toBe("Hello from Gemini");
  });

  it("uses gemini-2.5-flash by default", async () => {
    await handleAskGemini({ prompt: "ping" });
    expect(mockGenerateContent).toHaveBeenCalledWith(
      expect.objectContaining({ model: "gemini-2.5-flash" })
    );
  });

  it("uses provided model when specified", async () => {
    await handleAskGemini({ prompt: "ping", model: "gemini-2.5-pro" });
    expect(mockGenerateContent).toHaveBeenCalledWith(
      expect.objectContaining({ model: "gemini-2.5-pro" })
    );
  });

  it("appends history as contents before the prompt", async () => {
    await handleAskGemini({
      prompt: "Follow up",
      history: [
        { role: "user", text: "First message" },
        { role: "model", text: "First reply" },
      ],
    });

    const call = mockGenerateContent.mock.calls[0][0];
    expect(call.contents).toHaveLength(3);
    expect(call.contents[0]).toEqual({ role: "user", parts: [{ text: "First message" }] });
    expect(call.contents[1]).toEqual({ role: "model", parts: [{ text: "First reply" }] });
    expect(call.contents[2]).toEqual({ role: "user", parts: [{ text: "Follow up" }] });
  });

  it("returns fallback text when model returns nothing", async () => {
    mockGenerateContent.mockResolvedValueOnce({ text: undefined });
    const result = await handleAskGemini({ prompt: "ping" });
    expect(result.content[0].text).toBe("No response generated.");
  });

  it("throws on invalid args", async () => {
    await expect(handleAskGemini({ prompt: 42 })).rejects.toThrow("Invalid arguments");
  });

  it("wraps API errors", async () => {
    mockGenerateContent.mockRejectedValueOnce(new Error("quota exceeded"));
    await expect(handleAskGemini({ prompt: "ping" })).rejects.toThrow("Gemini query failed: quota exceeded");
  });
});
