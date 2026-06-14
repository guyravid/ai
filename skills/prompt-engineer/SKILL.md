---
name: prompt-engineer
description: >
  Build, refine, and optimize prompts using 9 proven techniques from Anthropic's prompt engineering practices. Two trigger modes. EXPLICIT: user asks to write, improve, or craft a prompt, says 'this isn't working' about an LLM interaction, pastes a prompt for feedback, or asks 'how should I ask Claude to do X'. PROACTIVE: user sends an underspecified task request where prompt engineering would meaningfully improve the result — anything beyond simple factual questions or single-step tasks. Proactive examples: 'Write me a marketing email', 'Help me prepare for my performance review', 'Draft a project proposal', 'Analyze this data'. Do NOT trigger proactively on simple requests like 'What is the capital of France' or 'Write a haiku'. When in doubt, trigger — the skill offers help gracefully and proceeds normally if declined.
---

# Prompt Engineer

A skill for building and refining high-quality prompts using 9 techniques derived from Anthropic's prompt engineering practices.

## Deciding the workflow

This skill has three entry paths. Detect which one based on the user's message:

**Build path** — The user has a goal but no prompt yet. They say things like "help me write a prompt for X", "I need Claude to do Y", "how should I ask Claude to Z".

**Refine path** — The user has an existing prompt (or describes a failing interaction). They say things like "improve this prompt", "this isn't working", "make this better", or they paste a prompt directly.

**Proactive path** — The user sent a task request (not a prompt-building request) that would produce significantly better results with prompt engineering applied. The user isn't asking for prompt help — they're asking for the task itself. But the request is underspecified enough that the gap between "answering as-is" and "answering a well-engineered version" is substantial.

If ambiguous between Build and Refine, ask one clarifying question. The Proactive path should be unambiguous — the user clearly wants a task done, not prompt help.

## Proactive Path

When the user sends a task request that would benefit from refinement:

1. **Assess the gap.** Quickly evaluate which techniques would materially improve the output. The gap needs to be meaningful — if only one minor technique applies, just answer the request normally. Look for at least 2-3 applicable techniques before offering.

2. **Offer, don't impose.** Briefly explain that the request could produce stronger results with a bit more specificity, and offer to help sharpen it. Keep the offer to 1-2 sentences — don't lecture. Something like: "I can answer this as-is, but I think we could get you a much stronger result if we spend 30 seconds sharpening the request. Want me to suggest an improved version, or should I just go ahead?"

3. **If the user accepts:** Transition into the Refine path, treating their original message as the draft prompt. Diagnose, prioritize, and rewrite as usual.

4. **If the user declines:** Answer the original request immediately with no further mention of prompt engineering. Don't sulk, don't add a footer saying "by the way, you could have...", just do the task.

5. **Never offer twice in the same conversation** unless the user explicitly asks for prompt help later. One proactive offer per conversation is the limit — after that, just answer requests directly.

## The 9 Techniques

Load `references/techniques.json` for the full technique definitions, examples, and application criteria. The techniques are:

1. **Role + Goal + Format** — Open with identity, objective, and output shape
2. **Targeted Negative Constraints** — Exclude known failure modes, paired with positive instructions
3. **Thinking Out Loud Prefix** — Force reasoning before answering on complex tasks
4. **Staged Prompting** — Break complex tasks into sequential stages
5. **Specify the Reader** — Define the real person who will read the output
6. **Steel Man the Counter** — Ask for the strongest argument against a recommendation
7. **XML Structure** — Wrap complex multi-part inputs in XML tags
8. **Surgical Revision** — Target the single weakest element instead of generic "make it better"
9. **Show, Don't Tell** — Include concrete examples of the desired output in the prompt

## Build Path

When the user has a goal but no prompt:

1. **Understand the task.** Ask what they want Claude to do, who the output is for, and what good looks like. Keep it to 1-2 focused questions — don't over-interview. If the user already gave enough detail, skip straight to drafting.

2. **Select applicable techniques.** Read each technique's `when` field in the JSON and decide which ones apply to this task. Not every prompt needs all 9 — apply only what fits. Technique 1 (Role + Goal + Format) almost always applies. Technique 9 (Show, Don't Tell) is among the highest-leverage whenever format, tone, or style matters — but it requires example material, so ask the user for a sample of "what good looks like" if they haven't provided one. The others depend on task complexity, audience, and type.

3. **Draft the prompt.** Write a complete, ready-to-use prompt that incorporates the selected techniques. The prompt should be something the user can copy-paste directly into a new conversation.

4. **Handle Staged Prompting (Technique 4).** By default, incorporate staged thinking *within* a single prompt (e.g., "First analyze X, then draft Y, then critique your draft, then revise"). If the task is complex enough to genuinely benefit from separate prompt stages, mention this option: "This task might benefit from being split into separate prompts — want me to produce a multi-prompt chain instead?" Only produce the chain if the user opts in.

## Refine Path

When the user has an existing prompt:

1. **Diagnose.** Read the prompt and identify which techniques are missing or underused. Check each technique's `when` field — if the technique applies to this type of task but isn't present in the prompt, it's a candidate for improvement.

2. **Prioritize.** Don't apply all missing techniques at once. Focus on the 2-3 that would have the highest impact for this specific prompt. Explain *why* each change matters (not just *what* changed).

3. **Rewrite.** Produce the improved prompt in full, ready to copy-paste.

4. **If the user says "make it better" without specifics**, apply Technique 8 (Surgical Revision) to the prompt itself: identify the single weakest element, fix it, and explain why it was the weakest. Then ask if they want to continue refining.

## Output Configuration

**Default:** Output the improved prompt followed by a brief annotation section explaining which techniques were applied and why. Format the annotations as a short paragraph per technique, not a checklist.

**Prompt-only mode:** If the user says "just the prompt", "no explanation", or similar — output only the prompt with no annotations.

**The prompt itself should never reference these technique names.** The techniques are the methodology, not the content. The output prompt should read naturally — the user's recipient (Claude or another LLM) should never see "Technique 3" or "Targeted Negative Constraints" in the prompt.

## Important Principles

- **Don't over-engineer simple prompts.** If someone wants "write me a haiku about cats", they don't need Role + Goal + Format + XML tags. Match the sophistication of the techniques to the complexity of the task.

- **Explain the why, not just the what.** When annotating changes, explain *why* a technique helps for this specific case. "I added negative constraints because your task is analytical and Claude tends to hedge on analytical questions" is useful. "I applied Technique 2" is not.

- **Respect the user's voice.** When refining, preserve the user's intent and style. Don't rewrite their prompt into a completely different voice unless they ask for it.

- **Be direct about diminishing returns.** If a prompt is already good, say so. Don't add techniques for the sake of adding techniques. "This prompt is solid — the one thing I'd change is X" is a perfectly valid response.
