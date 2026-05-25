import { describe, it, expect } from "vitest";
import { MIME_TYPES } from "../src/mime_types.js";

describe("MIME_TYPES", () => {
  it("covers common audio formats", () => {
    expect(MIME_TYPES[".m4a"]).toBe("audio/mp4");
    expect(MIME_TYPES[".m4b"]).toBe("audio/mp4");
    expect(MIME_TYPES[".mp3"]).toBe("audio/mpeg");
    expect(MIME_TYPES[".wav"]).toBe("audio/wav");
    expect(MIME_TYPES[".wave"]).toBe("audio/wav");
    expect(MIME_TYPES[".flac"]).toBe("audio/flac");
    expect(MIME_TYPES[".aac"]).toBe("audio/aac");
    expect(MIME_TYPES[".ogg"]).toBe("audio/ogg");
    expect(MIME_TYPES[".oga"]).toBe("audio/ogg");
    expect(MIME_TYPES[".opus"]).toBe("audio/opus");
    expect(MIME_TYPES[".aiff"]).toBe("audio/aiff");
    expect(MIME_TYPES[".aif"]).toBe("audio/aiff");
    expect(MIME_TYPES[".amr"]).toBe("audio/amr");
    expect(MIME_TYPES[".weba"]).toBe("audio/webm");
    expect(MIME_TYPES[".wma"]).toBe("audio/x-ms-wma");
  });

  it("covers common video formats", () => {
    expect(MIME_TYPES[".mp4"]).toBe("video/mp4");
    expect(MIME_TYPES[".m4v"]).toBe("video/mp4");
    expect(MIME_TYPES[".f4v"]).toBe("video/mp4");
    expect(MIME_TYPES[".mov"]).toBe("video/quicktime");
    expect(MIME_TYPES[".qt"]).toBe("video/quicktime");
    expect(MIME_TYPES[".avi"]).toBe("video/x-msvideo");
    expect(MIME_TYPES[".divx"]).toBe("video/x-msvideo");
    expect(MIME_TYPES[".mkv"]).toBe("video/x-matroska");
    expect(MIME_TYPES[".webm"]).toBe("video/webm");
    expect(MIME_TYPES[".wmv"]).toBe("video/x-ms-wmv");
    expect(MIME_TYPES[".3gp"]).toBe("video/3gpp");
    expect(MIME_TYPES[".3g2"]).toBe("video/3gpp2");
    expect(MIME_TYPES[".flv"]).toBe("video/x-flv");
    expect(MIME_TYPES[".ogv"]).toBe("video/ogg");
    expect(MIME_TYPES[".mpg"]).toBe("video/mpeg");
    expect(MIME_TYPES[".mpeg"]).toBe("video/mpeg");
    expect(MIME_TYPES[".mpe"]).toBe("video/mpeg");
    expect(MIME_TYPES[".m2v"]).toBe("video/mpeg");
    expect(MIME_TYPES[".ts"]).toBe("video/mp2t");
    expect(MIME_TYPES[".mts"]).toBe("video/mp2t");
    expect(MIME_TYPES[".m2ts"]).toBe("video/mp2t");
  });

  it("returns undefined for non-media extensions", () => {
    expect(MIME_TYPES[".xyz"]).toBeUndefined();
    expect(MIME_TYPES[".txt"]).toBeUndefined();
    expect(MIME_TYPES[".pdf"]).toBeUndefined();
    expect(MIME_TYPES[".ts"]).not.toBeUndefined(); // .ts is video/mp2t, not TypeScript
  });

  it("uses lowercase keys only — callers must normalise before lookup", () => {
    expect(MIME_TYPES[".M4A"]).toBeUndefined();
    expect(MIME_TYPES[".MP4"]).toBeUndefined();
    expect(MIME_TYPES[".MOV"]).toBeUndefined();
  });
});
