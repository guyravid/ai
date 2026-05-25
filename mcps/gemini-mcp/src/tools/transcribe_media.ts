import { Tool } from "@modelcontextprotocol/sdk/types.js";
import { z } from "zod";
import { GoogleGenAI } from "@google/genai";
import * as fs from "fs";
import * as path from "path";
import { MIME_TYPES } from "../mime_types.js";

export const transcribeMediaTool: Tool = {
  name: "transcribe_media",
  description: "Uploads a local audio or video file to Gemini and returns a transcript.",
  inputSchema: {
    type: "object",
    properties: {
      filePath: {
        type: "string",
        description: "The absolute local file path to the audio or video file (e.g., /Users/guy/audio.mp3)",
      },
      prompt: {
        type: "string",
        description: "Optional specific instructions, like 'Translate this to Spanish' or 'Summarize the meeting'. Defaults to 'Provide a detailed transcript.'",
      },
      model: {
        type: "string",
        description: "Optional model to use, defaults to gemini-2.5-flash",
      }
    },
    required: ["filePath"],
  },
};

const inputSchema = z.object({
  filePath: z.string(),
  prompt: z.string().optional(),
  model: z.string().optional(),
});

export async function handleTranscribeMedia(args: unknown) {
  const parsed = inputSchema.safeParse(args);
  if (!parsed.success) {
    throw new Error(`Invalid arguments: ${parsed.error.message}`);
  }

  const { filePath, prompt, model } = parsed.data;

  if (!fs.existsSync(filePath)) {
    throw new Error(`File not found at path: ${filePath}`);
  }

  const ai = new GoogleGenAI({ apiKey: process.env.GEMINI_API_KEY });
  const targetModel = model || "gemini-2.5-flash";
  const instructions = prompt || "Provide a detailed transcript of this media file.";

  try {
    const mimeType = MIME_TYPES[path.extname(filePath).toLowerCase()];
    console.error(`Uploading file ${filePath} to Gemini...`);
    const uploadResult = await ai.files.upload({
      file: filePath,
      ...(mimeType && { config: { mimeType } }),
    });
    console.error(`Upload complete. File URI: ${uploadResult.uri}`);

    if (!uploadResult.name) {
      throw new Error("Upload result missing name, cannot poll status.");
    }

    // Poll until the file is active (required for video processing)
    let fileState = uploadResult.state;
    while (fileState === "PROCESSING") {
      console.error("File is processing, waiting 5 seconds...");
      await new Promise((resolve) => setTimeout(resolve, 5000));
      const fileStatus = await ai.files.get({ name: uploadResult.name });
      fileState = fileStatus.state;
      if (fileState === "FAILED") {
        throw new Error("Gemini file processing failed.");
      }
    }

    console.error(`File is ACTIVE. Generating transcript with ${targetModel}...`);
    
    const response = await ai.models.generateContent({
      model: targetModel,
      contents: [
        {
          role: "user",
          parts: [
            { fileData: { mimeType: uploadResult.mimeType, fileUri: uploadResult.uri! } },
            { text: instructions },
          ],
        },
      ],
    });

    return {
      content: [
        {
          type: "text",
          text: response.text || "No transcript generated.",
        },
      ],
    };
  } catch (error: any) {
    throw new Error(`Transcription failed: ${error.message}`);
  }
}
