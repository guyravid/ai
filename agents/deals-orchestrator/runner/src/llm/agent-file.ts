export interface AgentDefinition {
  name: string;
  description: string;
  tools: string[];
  prompt: string;
}

export function parseAgentFile(markdown: string): AgentDefinition {
  const match = /^---\r?\n([\s\S]*?)\r?\n---\r?\n?([\s\S]*)$/.exec(markdown);
  if (match === null) throw new Error("agent file has no frontmatter");
  const fields = new Map<string, string>();
  for (const line of (match[1] ?? "").split(/\r?\n/)) {
    const separator = line.indexOf(":");
    if (separator > 0) fields.set(line.slice(0, separator).trim(), line.slice(separator + 1).trim());
  }
  const name = fields.get("name");
  const description = fields.get("description");
  if (name === undefined || description === undefined) throw new Error("frontmatter needs name and description");
  const tools = (fields.get("tools") ?? "")
    .replace(/^\[|\]$/g, "")
    .split(",")
    .map((tool) => tool.trim())
    .filter((tool) => tool !== "");
  return { name, description, tools, prompt: (match[2] ?? "").trim() };
}
