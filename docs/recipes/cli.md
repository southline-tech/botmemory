# BotOptimizer CLI recipes

Short copy-paste flows (loopback MCP + local vault).

## Orient

```sh
export BOTOPTIMIZER_TOKEN_FILE=/secure/bot.token
export BOTOPTIMIZER_PROJECT=demo
botoptimizer brief
botoptimizer list --if-etag "$(botoptimizer brief | jq -r .etag)"
```

## Handoff log

```sh
botoptimizer append handoff.review --text "Next: QA; blockers: none" --kind handoff
```

## Token usage (provider API)

```sh
botoptimizer usage log --bot my-agent --type tokens --in "$TOK_IN" --out "$TOK_OUT" --model "$MODEL"
```

## Weekly chart

```sh
botoptimizer usage chart --range 7d --out ./usage-week.html
```

## Import hosted shares + CSV

```sh
botoptimizer usage import --date 2026-11-01 --file fleet-shares.json
botoptimizer import-usage grok-export.csv --date 2026-11-01 --tz UTC
```
