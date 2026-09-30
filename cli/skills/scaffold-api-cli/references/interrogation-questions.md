# API Interrogation Questions

Work through every section before generating anything. An unanswered question here becomes a
rewrite in Step 6.

Mark each answer **stated** (the user or the documentation said so) or **assumed**, and list every
assumption in the Step 11 report. An assumption that reaches generated code unflagged looks like a
fact until it fails against real data.

## Identity

1. What is the API called, and what is its base URL?
2. Is there an OpenAPI document? If so, where? It answers much of what follows.
3. Where is the human documentation?
4. Are there several environments (production, sandbox, regions)? Should the base URL be a setting?

## Authentication

5. What scheme: bearer token, API key header, key plus token, basic auth, OAuth client credentials?
6. Exactly which header or query parameter carries it, and in what format?
7. How many separate secrets does a request need? Trello, for example, needs a key and a token.
8. Do credentials expire or need refreshing? A refresh flow is extra scope; flag it.
9. Which permissions or scopes do the chosen commands need?

**Never ask for credential values, and never read them if offered.** This skill needs only the
names. If the user pastes a real credential, tell them to rotate it.

## Commands

10. Which three to eight operations does the agent actually need? What the agent needs, not what the
    API offers.
11. For each: method, path, required and optional parameters.
12. For each: a real success response. A real example beats a description.
13. Which operations change data? Each becomes a `write.*` command and needs explicit confirmation to
    include.
14. Do any GET requests have side effects? They would also need the `write` prefix.
15. Are any operations slow, expensive, or rate-limited more strictly than the rest?

## Shaping

16. For each list: which four to six fields does an agent need to decide what to look at next? These
    become the default `--fields`.
17. Which fields are large (descriptions, attachments, embedded objects) and belong only in `get`?
18. Do responses contain personal data or anything that should not be stored on disk by `--all`?
    Those fields need excluding from the stored projection.
19. For each list: what order does the API return, and is it guaranteed? What field makes a unique
    tiebreak?
20. Does any list have a time dimension (created, updated, occurred)? Which parameters filter by
    time, and in what format? These become `--since` and `--until`.

## Paging

21. Which operations return collections?
22. What style: opaque cursor, offset, page number, `Link` header, or before-an-id?
23. What are the parameter names, and where do they go?
24. What is the maximum page size? `--all` uses it.
25. Does the response report a total? Is it exact or an estimate?
26. Roughly how large is a typical collection, and the largest? This sets `--limit` defaults and
    dataset sizing.

## Errors

27. What does an error body look like? Field names and an example.
28. Which status codes does the API actually use? Some return 200 with an error body.
29. Is there a machine-readable error code separate from the message?
30. On rate limiting: which status, and is there `Retry-After` or an equivalent?
31. Are there API-specific failures an agent should handle distinctly (an archived resource, a plan
    limit)? These may deserve a tool-specific exit code.
32. Does the API support idempotency keys for writes?

## Configuration

33. Which environments or accounts will the operator use? Name them; they become profiles.
34. What differs between them: base URL, credentials, limits?
35. Which settings does this API need beyond the contract's table: base URL, workspace or
    organization id, region?
36. How will credentials be supplied in production: individual secret files, one `.env` file, or
    environment variables from an orchestrator? This decides the example config file.

## Diagnostics

37. What is the cheapest authenticated read, ideally one returning the caller's identity (`/me`,
    `/whoami`, an account endpoint)? `doctor` uses it.
38. Does the API send a `Date` header? `doctor` uses it to check clock skew.

## Limits

39. What are the published rate limits, and are they per key, per user, or per organization?
40. Is there a request quota the operator should know about?
