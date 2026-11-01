package mcp

import (
	"errors"
	"fmt"

	"github.com/southline-tech/botmemory/internal/usage"
	"github.com/southline-tech/botmemory/internal/vault"
)

func errCode(err error) string {
	switch {
	case errors.Is(err, usage.ErrRangeInvalid):
		return "RANGE_INVALID"
	case errors.Is(err, usage.ErrCSVZone):
		return "CSV_TZ_REQUIRED"
	case errors.Is(err, vault.ErrConflict):
		return "CONFLICT"
	case errors.Is(err, vault.ErrForbidden):
		return "FORBIDDEN"
	case errors.Is(err, vault.ErrNotFound):
		return "NOT_FOUND"
	case errors.Is(err, vault.ErrInvalid), errors.Is(err, usage.ErrInvalid):
		return "INVALID"
	default:
		return "INTERNAL"
	}
}

func errHint(code string) string {
	switch code {
	case "CONFLICT":
		return "get then put with if_version=current"
	case "FORBIDDEN":
		return "ask owner to acl grant project access"
	case "NOT_FOUND":
		return "use put or append (append creates missing key)"
	case "INVALID":
		return "check args; project may need BOTMEMORY_PROJECT"
	case "RANGE_INVALID":
		return "use --range between 24h and 90d"
	case "CSV_TZ_REQUIRED":
		return "pass --tz for zoneless CSV timestamps"
	default:
		return "retry once or escalate to operator"
	}
}

func formatToolErr(err error) string {
	code := errCode(err)
	return fmt.Sprintf("%s: %v; hint: %s", code, err, errHint(code))
}
