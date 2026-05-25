import { Tool } from "@modelcontextprotocol/sdk/types.js";
import { z } from "zod";
import { GoogleGenAI } from "@google/genai";

export const askGeminiTool: Tool = {
  name: "ask_gemini",
  description: "Query Gemini with a text prompt and optionally follow up on a previous conversation.",
  inputSchema: {
    type: "object",
    properties: {
      prompt: {
        type: "string",
        description: "Your question or prompt for Gemini.",
      },
      history: {
        type: "array",
        description: "Optional conversation history to provide context for follow-ups.",
        items: {
          type: "object",
          properties: {
            role: { type: "string", enum: ["user", "model"] },
            text: { type: "string" }
          },
          required: ["role", "text"]
        }
      },
      model: {
        type: "string",
        description: "Optional model to use, defaults to gemini-2.5-flash",
      }
    },
    required: ["prompt"],
  },
};

const inputSchema = z.object({
  prompt: z.string(),
  history: z.array(z.object({
    role: z.enum(["user", "model"]),
    text: z.string()
  })).optional(),
  model: z.string().optional(),
});

export async function handleAskGemini(args: unknown) {
  const parsed = inputSchema.safeParse(args);
  if (!parsed.success) {
    throw new Error(`Invalid arguments: ${parsed.error.message}`);
  }

  const { prompt, history, model } = parsed.data;
  const ai = new GoogleGenAI({ apiKey: process.env.GEMINI_API_KEY });
  const targetModel = model || "gemini-2.5-flash";

  try {
    const contents: any[] = [];
    
    // Append history if provided
    if (history && history.length > 0) {
      for (const msg of history) {
        contents.push({
          role: msg.role,
          parts: [{ text: msg.text }]
        });
      }
    }
    
    // Append current prompt
    contents.push({
      role: "user",
      parts: [{ text: prompt }]
    });

    const response = await ai.models.generateContent({
      model: targetModel,
      contents,
    });

    return {
      content: [
        {
          type: "text",
          text: response.text || "No response generated.",
        },
      ],
    };
  } catch (error: any) {
    throw new Error(`Gemini query failed: ${error.message}`);
  }
}
