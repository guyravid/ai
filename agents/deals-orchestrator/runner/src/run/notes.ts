export const NOTE_MAX_CHARS = 1500;

export interface RunNoteInput {
  date: string;
  lines: string[];
}

export function buildRunNote(input: RunNoteInput): string | null {
  if (input.lines.length === 0) return null;
  const note = [`Run ${input.date}`, ...input.lines.map((line) => `- ${line}`)].join("\n");
  return note.length <= NOTE_MAX_CHARS ? note : `${note.slice(0, NOTE_MAX_CHARS - 1)}…`;
}

export function dateOf(now: Date): string {
  return now.toISOString().slice(0, 10);
}
