package usage

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/southline-tech/botmemory/internal/vault"
)

// ToolArgs holds decoded MCP usage tool arguments.
type ToolArgs struct {
	Project string  `json:"project"`
	Bot     string  `json:"bot"`
	Type    string  `json:"type"`
	In      int     `json:"in"`
	Out     int     `json:"out"`
	Cache   int     `json:"cache"`
	Model   string  `json:"model"`
	Cost    float64 `json:"cost"`
	N       int     `json:"n"`
	Bytes   int     `json:"bytes"`
	Tool    string  `json:"tool"`
	PR      string  `json:"pr"`
	Key     string  `json:"key"`
	Ref     string  `json:"ref"`
	Date    string  `json:"date"`
	Merge   bool    `json:"merge"`
	Body    string  `json:"body"`
	CSV     string  `json:"csv"`
	TZ      string  `json:"tz"`
	Since   string  `json:"since"`
	Range   string  `json:"range"`
	OutPath string  `json:"out"`
	At      string  `json:"at"`
	Explain bool    `json:"explain"`
}

func decodeArgs(raw json.RawMessage, a *ToolArgs) error {
	if len(raw) == 0 {
		return nil
	}
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()
	return dec.Decode(a)
}

// CallTool runs one usage MCP tool.
func CallTool(ctx context.Context, v *vault.Vault, authBot, name string, raw json.RawMessage, resolveProject func(string) (string, error)) (any, error) {
	var a ToolArgs
	if err := decodeArgs(raw, &a); err != nil {
		return nil, fmt.Errorf("%w: %v", vault.ErrInvalid, err)
	}
	project, err := resolveProject(a.Project)
	if err != nil {
		return nil, err
	}
	svc := &Service{V: v}
	switch name {
	case "usage_log":
		return svc.Log(ctx, authBot, project, LogInput{
			Bot: a.Bot, Type: a.Type, In: a.In, Out: a.Out, Cache: a.Cache,
			Model: a.Model, Cost: a.Cost, N: a.N, Bytes: a.Bytes, Tool: a.Tool, PR: a.PR, Key: a.Key, Ref: a.Ref,
		})
	case "usage_import":
		if a.Date == "" {
			return nil, fmt.Errorf("%w: date required", ErrInvalid)
		}
		var doc ShareDoc
		src := strings.TrimSpace(a.Body)
		if src == "" {
			return nil, fmt.Errorf("%w: body required", ErrInvalid)
		}
		if err := json.Unmarshal([]byte(src), &doc); err != nil {
			return nil, fmt.Errorf("%w: invalid share json", ErrInvalid)
		}
		return svc.ImportShare(ctx, authBot, project, a.Date, doc, a.Merge)
	case "usage_import_csv":
		if a.Date == "" {
			return nil, fmt.Errorf("%w: date required", ErrInvalid)
		}
		r := strings.NewReader(a.CSV)
		if a.CSV == "" && a.Body != "" {
			r = strings.NewReader(a.Body)
		}
		return svc.ImportCSV(ctx, authBot, project, a.Date, a.TZ, r, CSVMap{})
	case "watch_check":
		since := time.Time{}
		if strings.TrimSpace(a.Since) != "" {
			since, err = vault.ParseSince(a.Since)
			if err != nil {
				return nil, err
			}
		}
		return svc.WatchCheck(ctx, authBot, project, a.Bot, since)
	case "watch_init":
		return svc.InitWatchRules(ctx, authBot, project)
	case "usage_chart":
		if a.Explain && a.At != "" {
			return svc.ExplainAt(ctx, authBot, project, a.At)
		}
		html, meta, err := svc.ChartHTML(ctx, authBot, project, a.Range)
		if err != nil {
			return nil, err
		}
		if a.OutPath != "" {
			if err := os.WriteFile(a.OutPath, []byte(html), 0644); err != nil {
				return nil, err
			}
			meta["ok"] = true
			meta["out"] = a.OutPath
			return meta, nil
		}
		meta["html"] = html
		return meta, nil
	case "usage_hints":
		since := time.Time{}
		if strings.TrimSpace(a.Since) != "" {
			since, err = vault.ParseSince(a.Since)
			if err != nil {
				return nil, err
			}
		}
		return svc.Hints(ctx, authBot, project, a.Bot, since)
	default:
		return nil, fmt.Errorf("unknown usage tool %q", name)
	}
}

// ImportShareFromReader reads JSON share doc from r.
func ImportShareFromReader(r io.Reader) (ShareDoc, error) {
	var doc ShareDoc
	if err := json.NewDecoder(r).Decode(&doc); err != nil {
		return doc, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	return doc, nil
}
