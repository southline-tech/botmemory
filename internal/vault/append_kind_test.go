package vault

import (
	"errors"
	"testing"
)

func TestAppendKindInvalid(t *testing.T) {
	v, _ := newVault(t)
	_, err := v.Append(ctx, "pm", "vc", "newkey", "text", "assignment")
	if !errors.Is(err, ErrKindInvalid) {
		t.Fatalf("got %v", err)
	}
}

func TestAppendCreateKindUsage(t *testing.T) {
	v, _ := newVault(t)
	r, err := v.Append(ctx, "pm", "vc", "metrics", "line", "usage")
	if err != nil || r.Kind != "usage" {
		t.Fatalf("%+v %v", r, err)
	}
}
