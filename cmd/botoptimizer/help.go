package main

import (
	"fmt"
	"os"
	"strings"
)

const helpRoot = `BotOptimizer — fleet memory, usage, and watches (CLI + MCP).

Usage: botoptimizer <command> [flags]
Env: BOTOPTIMIZER_TOKEN_FILE, BOTOPTIMIZER_PROJECT, BOTOPTIMIZER_URL (default http://127.0.0.1:8787/mcp)

Commands:
  serve, stdio, migrate, bootstrap, mint-token, acl
  projects, list, brief, changed, get, put, append, search, history, stats, delete
  usage (log|import|chart|hints), watch (check|init), import-usage
  version, ping

Run: botoptimizer <command> --help
`

var helpTopics = map[string]string{
	"serve": `usage: botoptimizer serve [--storage local|drive|memory] [--data-dir PATH] [--warm-interval D]

Start MCP HTTP server on BOTOPTIMIZER_ADDR (default 127.0.0.1:8787).

Example:
  botoptimizer serve --storage local
`,
	"stdio": `usage: botoptimizer stdio

MCP over stdin/stdout (one process per bot token).

Example:
  export BOTOPTIMIZER_TOKEN_FILE=/secure/bot.token
  botoptimizer stdio
`,
	"migrate": `usage: botoptimizer migrate [--dry-run] [--from local|drive] [--to local|drive]

Copy vault data between backends.

Example:
  botoptimizer migrate --dry-run
`,
	"bootstrap": `usage: botoptimizer bootstrap [--test-folder]

Create botOptimizer root folder in Google Drive.

Example:
  botoptimizer bootstrap
`,
	"mint-token": `usage: botoptimizer mint-token BOT_ID

Print a new bearer token and SHA-256 hash for _acl.json (never commit the token).

Example:
  botoptimizer mint-token pm
`,
	"acl": `usage: botoptimizer acl grant|revoke|list ...

Edit _acl.json (see botoptimizer acl --help on subcommand).

Example:
  botoptimizer acl list
`,
	"projects": `usage: botoptimizer projects [--token-file PATH] [--project NAME]

List granted projects (MCP list_projects).

Example:
  botoptimizer projects
`,
	"list": `usage: botoptimizer list [PROJECT] [--if-etag ETAG] [--project NAME]

Index summaries for a project (call before get).

Example:
  botoptimizer list --if-etag abc123
`,
	"brief": `usage: botoptimizer brief [PROJECT] [--limit N] [--if-etag ETAG] [--kind KIND] [--tag TAG]

Fast orient (~2k tokens).

Example:
  botoptimizer brief --limit 20
`,
	"changed": `usage: botoptimizer changed [PROJECT] --since RFC3339|YYYY-MM-DD [--kind KIND] [--tag TAG]

Keys updated after since.

Example:
  botoptimizer changed --since 2026-11-01
`,
	"get": `usage: botoptimizer get KEY [--version N] [--head N] [--tail N] [--since T] [--max-bytes N] [--summary]

Example:
  botoptimizer get plan --head 50
`,
	"put": `usage: botoptimizer put KEY --title T [--body B|--file PATH] [--kind KIND] [--tags a,b] [--if-version N]

Example:
  botoptimizer put plan --title Plan --kind spec --body "..."
`,
	"append": `usage: botoptimizer append KEY (--text S|--file PATH) [--kind KIND]

Append text; creates missing key. --kind: handoff|spec|decision|note|usage (default note).

Example:
  botoptimizer append log --text "- shipped" --kind handoff
`,
	"search": `usage: botoptimizer search QUERY [--project P|*] [--limit N]

Example:
  botoptimizer search "handoff rollout" --project demo
`,
	"history": `usage: botoptimizer history KEY [--project NAME]

Example:
  botoptimizer history plan
`,
	"stats": `usage: botoptimizer stats

Process call counters (no note bodies).

Example:
  botoptimizer stats
`,
	"delete": `usage: botoptimizer delete KEY [--if-version N] [--project NAME]

Owner-only soft archive.

Example:
  botoptimizer delete old-plan
`,
	"version": `usage: botoptimizer version | botoptimizer --version

Print version string.

Example:
  botoptimizer version
`,
	"ping": `usage: botoptimizer ping

Health check against BOTOPTIMIZER_URL.

Example:
  botoptimizer ping
`,
	"usage": `usage: botoptimizer usage log|import|chart|hints ...

Optimizer usage telemetry (see subcommand --help).

Example:
  botoptimizer usage log --help
`,
	"usage log": `usage: botoptimizer usage log --bot SLUG --type TYPE [flags]

Append one line to log.usage.<slug>. Real tokens: --type tokens --in N --out N [--cache N] [--model M].
Proxies: wake|bytes_r|tool|recheck|msg|image|retry|read|other with --n/--bytes/--tool/--key/--ref.

Example:
  botoptimizer usage log --bot my-agent --type tokens --in 1200 --out 400 --model claude-sonnet
`,
	"usage import": `usage: botoptimizer usage import --date YYYY-MM-DD (--file PATH|--stdin) [--merge] [--project NAME]

Write hosted fleet share JSON to usage.share.<date>.

Example:
  botoptimizer usage import --date 2026-11-01 --file shares.json
`,
	"usage chart": `usage: botoptimizer usage chart [--range 24h|7d|90d] [--out FILE] [--project NAME] [--explain --at RFC3339]

Offline HTML+SVG chart (range 24h–90d).

Example:
  botoptimizer usage chart --range 7d --out /tmp/usage.html
`,
	"usage hints": `usage: botoptimizer usage hints [--bot SLUG] [--since T] [--project NAME]

Waste hints (recheck_loop, token_spike, …).

Example:
  botoptimizer usage hints --since 2026-11-01
`,
	"watch": `usage: botoptimizer watch check|init ...

Example:
  botoptimizer watch check --help
`,
	"watch check": `usage: botoptimizer watch check [--bot SLUG] [--since T] [--project NAME]

Evaluate watch.rules (token budgets before proxy rules).

Example:
  botoptimizer watch check
`,
	"watch init": `usage: botoptimizer watch init [--project NAME]

Create watch.rules with defaults if missing.

Example:
  botoptimizer watch init
`,
	"import-usage": `usage: botoptimizer import-usage FILE.csv --date YYYY-MM-DD [--tz TZ] [--map time=...,bot=...]

Import dashboard CSV to usage.csv.<date> (zoneless timestamps need --tz).

Example:
  botoptimizer import-usage export.csv --date 2026-11-01 --tz America/Chicago
`,
}

func isHelpFlag(s string) bool {
	return s == "--help" || s == "-h"
}

func argsHaveHelp(args []string) bool {
	for _, a := range args {
		if isHelpFlag(a) {
			return true
		}
	}
	return false
}

func maybePrintHelp(argv []string) bool {
	if len(argv) < 2 {
		return false
	}
	cmd := argv[1]
	rest := argv[2:]
	if isHelpFlag(cmd) {
		fmt.Print(helpRoot)
		return true
	}
	if !argsHaveHelp(rest) {
		return false
	}
	topic := cmd
	if cmd == "usage" && len(rest) > 0 && !isHelpFlag(rest[0]) {
		topic = "usage " + rest[0]
	}
	if cmd == "watch" && len(rest) > 0 && !isHelpFlag(rest[0]) {
		topic = "watch " + rest[0]
	}
	if cmd == "acl" && len(rest) > 0 && !isHelpFlag(rest[0]) {
		topic = "acl " + rest[0]
	}
	if text, ok := helpTopics[topic]; ok {
		fmt.Print(text)
		return true
	}
	if text, ok := helpTopics[cmd]; ok {
		fmt.Print(text)
		return true
	}
	fmt.Fprintf(os.Stderr, "no detailed help for %q; try botoptimizer --help\n", topic)
	return true
}

func helpTopicForBotCommand(cmd string, args []string) (string, bool) {
	if !argsHaveHelp(args) {
		return "", false
	}
	return cmd, true
}

func usageSubHelp(sub string) bool {
	if !strings.Contains(sub, "--help") && sub != "--help" && sub != "-h" {
		for _, a := range strings.Fields(sub) {
			if isHelpFlag(a) {
				sub = strings.TrimSpace(strings.ReplaceAll(sub, a, ""))
				break
			}
		}
	}
	key := "usage"
	if sub != "" && sub != "--help" && sub != "-h" {
		parts := strings.Fields(sub)
		if len(parts) > 0 {
			key = "usage " + parts[0]
		}
	}
	if text, ok := helpTopics[key]; ok {
		fmt.Print(text)
		return true
	}
	return false
}
