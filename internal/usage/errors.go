package usage

import "errors"

var (
	ErrInvalid      = errors.New("invalid argument")
	ErrRangeInvalid = errors.New("range invalid")
	ErrCSVZone      = errors.New("csv timezone required")
	ErrNotFound     = errors.New("not found")
)
