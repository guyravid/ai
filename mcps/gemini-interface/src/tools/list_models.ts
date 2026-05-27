import { Tool } from "@modelcontextprotocol/sdk/types.js";
import { GoogleGenAI } from "@google/genai";

export const listModelsTool: Tool = {
  name: "list_models",
  description: "Get a list of the available Gemini models.",
  inputSchema: {
    type: "object",
    properties: {},
  },
};

export async function handleListModels(args: unknown) {
  const ai = new GoogleGenAI({ apiKey: process.env.GEMINI_API_KEY });

  try {
    const models = [];
    const response = await ai.models.list();
    for await (const model of response) {
      models.push(model);
    }

    return {
      content: [
        {
          type: "text",
          text: JSON.stringify(models, null, 2),
        },
      ],
    };
  } catch (error: any) {
    throw new Error(`Failed to list models: ${error.message}`);
  }
}
