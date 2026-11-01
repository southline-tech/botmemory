package mcp

import (
	"errors"
	"strings"

	"github.com/southline-tech/botmemory/internal/usage"
	"github.com/southline-tech/botmemory/internal/vault"
)

// ExitCode maps vault errors to bot CLI exit codes.
func ExitCode(err error) int {
	if err == nil {
		return 0
	}
	switch {
	case errors.Is(err, vault.ErrNotFound):
		return 3
	case errors.Is(err, vault.ErrForbidden):
		return 4
	case errors.Is(err, vault.ErrConflict):
		return 5
	case errors.Is(err, vault.ErrInvalid), errors.Is(err, usage.ErrInvalid):
		return 6
	default:
		return 1
	}
}

// ExitCodeFromToolText parses CODE: from MCP tool error text.
func ExitCodeFromToolText(text string) int {
	code, _, _ := strings.Cut(text, ":")
	switch strings.TrimSpace(code) {
	case "NOT_FOUND":
		return 3
	case "FORBIDDEN":
		return 4
	case "CONFLICT":
		return 5
	case "INVALID":
		return 6
	default:
		return 1
	}
}

// ToolErrLine formats stderr for CLI (CODE: message; hint: …).
func ToolErrLine(err error) string {
	return formatToolErr(err)
}
