import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";

const mockUpload = vi.hoisted(() => vi.fn());
const mockFilesGet = vi.hoisted(() => vi.fn());
const mockGenerateContent = vi.hoisted(() => vi.fn());
const mockExistsSync = vi.hoisted(() => vi.fn());

vi.mock("@google/genai", () => ({
  GoogleGenAI: vi.fn(function () {
    return {
      files: { upload: mockUpload, get: mockFilesGet },
      models: { generateContent: mockGenerateContent },
    };
  }),
}));

vi.mock("fs", () => ({
  existsSync: mockExistsSync,
}));

const { handleTranscribeMedia, transcribeMediaTool } = await import(
  "../../src/tools/transcribe_media.js"
);

describe("transcribeMediaTool definition", () => {
  it("has correct name and requires filePath", () => {
    expect(transcribeMediaTool.name).toBe("transcribe_media");
    expect(transcribeMediaTool.inputSchema.required).toContain("filePath");
  });
});

describe("handleTranscribeMedia", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    process.env.GEMINI_API_KEY = "test-key";
    mockExistsSync.mockReturnValue(true);
    mockUpload.mockResolvedValue({ uri: "gs://bucket/file", name: "files/abc123", state: "ACTIVE" });
    mockGenerateContent.mockResolvedValue({ text: "Transcript text" });
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it("returns transcript for an ACTIVE file", async () => {
    const result = await handleTranscribeMedia({ filePath: "/tmp/audio.mp3" });
    expect(result.content[0].type).toBe("text");
    expect(result.content[0].text).toBe("Transcript text");
  });

  it("polls until file becomes ACTIVE", async () => {
    vi.useFakeTimers();
    mockUpload.mockResolvedValueOnce({ uri: "gs://bucket/file", name: "files/abc", state: "PROCESSING" });
    mockFilesGet.mockResolvedValueOnce({ state: "ACTIVE" });

    const promise = handleTranscribeMedia({ filePath: "/tmp/audio.mp3" });
    await vi.runAllTimersAsync();
    const result = await promise;

    expect(mockFilesGet).toHaveBeenCalledOnce();
    expect(result.content[0].text).toBe("Transcript text");
  });

  it("throws when file processing fails", async () => {
    vi.useFakeTimers();
    mockUpload.mockResolvedValueOnce({ uri: "gs://bucket/file", name: "files/abc", state: "PROCESSING" });
    mockFilesGet.mockResolvedValueOnce({ state: "FAILED" });

    const promise = handleTranscribeMedia({ filePath: "/tmp/audio.mp3" });
    // Attach rejection handler before advancing timers to avoid unhandled rejection
    const assertion = expect(promise).rejects.toThrow("Transcription failed: Gemini file processing failed.");
    await vi.runAllTimersAsync();
    await assertion;
  });

  it("throws when file does not exist", async () => {
    mockExistsSync.mockReturnValueOnce(false);

    await expect(handleTranscribeMedia({ filePath: "/tmp/missing.mp3" })).rejects.toThrow(
      "File not found at path: /tmp/missing.mp3"
    );
  });

  it("uses default prompt when none provided", async () => {
    await handleTranscribeMedia({ filePath: "/tmp/audio.mp3" });
    const parts = mockGenerateContent.mock.calls[0][0].contents[0].parts;
    expect(parts[1]).toEqual({ text: "Provide a detailed transcript of this media file." });
  });

  it("uses custom prompt when provided", async () => {
    await handleTranscribeMedia({ filePath: "/tmp/audio.mp3", prompt: "Translate to Spanish" });
    const parts = mockGenerateContent.mock.calls[0][0].contents[0].parts;
    expect(parts[1]).toEqual({ text: "Translate to Spanish" });
  });

  it("references the uploaded file via fileData in generateContent", async () => {
    mockUpload.mockResolvedValueOnce({
      uri: "gs://bucket/file",
      name: "files/abc123",
      state: "ACTIVE",
      mimeType: "audio/mpeg",
    });
    await handleTranscribeMedia({ filePath: "/tmp/audio.mp3" });
    const parts = mockGenerateContent.mock.calls[0][0].contents[0].parts;
    expect(parts[0]).toEqual({ fileData: { mimeType: "audio/mpeg", fileUri: "gs://bucket/file" } });
  });

  it("returns fallback text when model returns nothing", async () => {
    mockGenerateContent.mockResolvedValueOnce({ text: undefined });
    const result = await handleTranscribeMedia({ filePath: "/tmp/audio.mp3" });
    expect(result.content[0].text).toBe("No transcript generated.");
  });

  it("throws on invalid args", async () => {
    await expect(handleTranscribeMedia({ filePath: 123 })).rejects.toThrow("Invalid arguments");
  });

  it("wraps upload API errors", async () => {
    mockUpload.mockRejectedValueOnce(new Error("upload failed"));
    await expect(handleTranscribeMedia({ filePath: "/tmp/audio.mp3" })).rejects.toThrow(
      "Transcription failed: upload failed"
    );
  });

  it("passes MIME type for known audio extension (.m4a)", async () => {
    await handleTranscribeMedia({ filePath: "/tmp/recording.m4a" });
    expect(mockUpload).toHaveBeenCalledWith(
      expect.objectContaining({ config: { mimeType: "audio/mp4" } })
    );
  });

  it("passes MIME type for known video extension (.mov)", async () => {
    await handleTranscribeMedia({ filePath: "/tmp/recording.mov" });
    expect(mockUpload).toHaveBeenCalledWith(
      expect.objectContaining({ config: { mimeType: "video/quicktime" } })
    );
  });

  it("omits config for unknown extension, leaving MIME detection to the SDK", async () => {
    await handleTranscribeMedia({ filePath: "/tmp/recording.xyz" });
    expect(mockUpload).toHaveBeenCalledWith({ file: "/tmp/recording.xyz" });
  });

  it("handles uppercase file extensions", async () => {
    await handleTranscribeMedia({ filePath: "/tmp/recording.M4A" });
    expect(mockUpload).toHaveBeenCalledWith(
      expect.objectContaining({ config: { mimeType: "audio/mp4" } })
    );
  });
});
