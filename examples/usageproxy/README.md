# usageproxy (optional)

Thin helpers to map provider `usage` blocks to `botmemory usage log --type tokens`.
Deferred in v0.4 P1 — wire OpenAI/Anthropic SDK callbacks here when ready.

Example (manual):

```sh
botmemory usage log --bot my-agent --type tokens --in 1200 --out 400 --model gpt-4.1 --ref run-id
```

Never fabricate token counts when the provider omits usage; use proxy etypes instead.
