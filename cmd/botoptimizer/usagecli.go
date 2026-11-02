package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/southline-tech/botoptimizer/internal/mcp"
	"github.com/southline-tech/botoptimizer/internal/usage"
)

func runUsageCommand(args []string) error {
	if len(args) < 1 {
		return errBotUsage
	}
	sub := args[0]
	rest := args[1:]
	switch sub {
	case "log":
		return runUsageLog(rest)
	case "import":
		return runUsageImport(rest)
	case "chart":
		return runUsageChart(rest)
	case "hints":
		return runUsageHints(rest)
	default:
		return errBotUsage
	}
}

func runWatchCommand(args []string) error {
	if len(args) < 1 {
		return errBotUsage
	}
	switch args[0] {
	case "check":
		return runWatchCheck(args[1:])
	case "init":
		return runWatchInit(args[1:])
	default:
		return errBotUsage
	}
}

func runImportUsageCommand(args []string) error {
	if len(args) < 1 {
		return errBotUsage
	}
	file := args[0]
	rest := args[1:]
	var date, tz string
	var csvMap usage.CSVMap
	for i := 0; i < len(rest); i++ {
		switch rest[i] {
		case "--date":
			i++
			date = rest[i]
		case "--tz":
			i++
			tz = rest[i]
		case "--map":
			i++
			m, err := parseCSVMapFlag(rest[i])
			if err != nil {
				return err
			}
			csvMap = m
		}
	}
	if date == "" {
		return errBotUsage
	}
	b, err := os.ReadFile(file)
	if err != nil {
		return err
	}
	payload := map[string]any{
		"date": date, "csv": string(b), "tz": tz,
	}
	if csvMap.Time != "" || csvMap.Bot != "" || csvMap.Tokens != "" || csvMap.TOut != "" {
		payload["map"] = csvMap
	}
	return usageToolCall("usage_import_csv", payload)
}

func parseCSVMapFlag(s string) (usage.CSVMap, error) {
	var m usage.CSVMap
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		k, v, ok := strings.Cut(part, "=")
		if !ok || strings.TrimSpace(v) == "" {
			return m, fmt.Errorf("%w: bad --map %q (want key=column)", usage.ErrInvalid, part)
		}
		switch strings.TrimSpace(k) {
		case "time":
			m.Time = strings.TrimSpace(v)
		case "bot":
			m.Bot = strings.TrimSpace(v)
		case "tokens_in":
			m.Tokens = strings.TrimSpace(v)
		case "tokens_out":
			m.TOut = strings.TrimSpace(v)
		default:
			return m, fmt.Errorf("%w: unknown map key %q", usage.ErrInvalid, k)
		}
	}
	return m, nil
}

func runUsageLog(args []string) error {
	a := map[string]any{}
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--bot":
			i++
			a["bot"] = args[i]
		case "--type":
			i++
			a["type"] = args[i]
		case "--in":
			i++
			n, err := strconv.Atoi(args[i])
			if err != nil {
				return fmt.Errorf("%w: bad --in", usage.ErrInvalid)
			}
			a["in"] = n
		case "--out":
			i++
			n, err := strconv.Atoi(args[i])
			if err != nil {
				return fmt.Errorf("%w: bad --out", usage.ErrInvalid)
			}
			a["out"] = n
		case "--cache":
			i++
			a["cache"], _ = strconv.Atoi(args[i])
		case "--model":
			i++
			a["model"] = args[i]
		case "--cost":
			i++
			a["cost"], _ = strconv.ParseFloat(args[i], 64)
		case "--n":
			i++
			a["n"], _ = strconv.Atoi(args[i])
		case "--bytes":
			i++
			a["bytes"], _ = strconv.Atoi(args[i])
		case "--tool":
			i++
			a["tool"] = args[i]
		case "--pr":
			i++
			a["pr"] = args[i]
		case "--key":
			i++
			a["key"] = args[i]
		case "--ref":
			i++
			a["ref"] = args[i]
		case "--project":
			i++
			a["project"] = args[i]
		}
	}
	if err := usage.ValidateUsageLogMap(a); err != nil {
		fmt.Fprintln(os.Stderr, mcp.ToolErrLine(err))
		os.Exit(mcp.ExitCode(err))
	}
	return usageToolCall("usage_log", a)
}

func runUsageImport(args []string) error {
	var date, project string
	merge := false
	stdin := false
	var file string
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--date":
			i++
			date = args[i]
		case "--file":
			i++
			file = args[i]
		case "--stdin":
			stdin = true
		case "--merge":
			merge = true
		case "--project":
			i++
			project = args[i]
		}
	}
	if date == "" {
		return errBotUsage
	}
	var body []byte
	var err error
	if stdin {
		body, err = io.ReadAll(os.Stdin)
	} else if file != "" {
		body, err = os.ReadFile(file)
	} else {
		return errBotUsage
	}
	if err != nil {
		return err
	}
	m := map[string]any{"date": date, "body": string(body), "merge": merge}
	if project != "" {
		m["project"] = project
	}
	return usageToolCall("usage_import", m)
}

func runUsageChart(args []string) error {
	m := map[string]any{}
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--range":
			i++
			m["range"] = args[i]
		case "--out":
			i++
			m["out"] = args[i]
		case "--project":
			i++
			m["project"] = args[i]
		case "--explain":
			m["explain"] = true
		case "--at":
			i++
			m["at"] = args[i]
		}
	}
	return usageToolCall("usage_chart", m)
}

func runUsageHints(args []string) error {
	m := map[string]any{}
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--bot":
			i++
			m["bot"] = args[i]
		case "--since":
			i++
			m["since"] = args[i]
		case "--project":
			i++
			m["project"] = args[i]
		}
	}
	return usageToolCall("usage_hints", m)
}

func runWatchCheck(args []string) error {
	m := map[string]any{}
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--bot":
			i++
			m["bot"] = args[i]
		case "--since":
			i++
			m["since"] = args[i]
		case "--project":
			i++
			m["project"] = args[i]
		}
	}
	return usageToolCall("watch_check", m)
}

func runWatchInit(args []string) error {
	m := map[string]any{}
	for i := 0; i < len(args); i++ {
		if args[i] == "--project" && i+1 < len(args) {
			m["project"] = args[i+1]
			i++
		}
	}
	return usageToolCall("watch_init", m)
}

func usageToolCall(tool string, args map[string]any) error {
	f, _, err := parseBotFlags(nil)
	if err != nil {
		return err
	}
	tok, err := loadToken(f)
	if err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(6)
	}
	c := &mcpClient{url: f.url, token: tok, hc: &http.Client{Timeout: 10 * time.Second}}
	out, err := c.callTool(context.Background(), tool, withProject(f, args))
	if err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}
	b, err := encodeOut(out, f.human)
	if err != nil {
		return err
	}
	os.Stdout.Write(b)
	if !f.human && len(b) > 0 && b[len(b)-1] != '\n' {
		os.Stdout.Write([]byte("\n"))
	}
	return nil
}
