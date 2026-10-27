package acl

import (
	"fmt"
	"testing"
)

func doc(t *testing.T, tok string, revoked bool) *ACL {
	t.Helper()
	a, err := Parse([]byte(fmt.Sprintf(`{"bots":{"pm":{"token_sha256":%q,"revoked":%v,"roles":{"vc":"owner","*":"read"}}}}`, HashToken(tok), revoked)))
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func TestAuthenticate(t *testing.T) {
	a := doc(t, "secret", false)
	if id, err := a.Authenticate("secret"); err != nil || id != "pm" {
		t.Fatalf("got %q %v", id, err)
	}
	for _, bad := range []string{"", "nope", HashToken("secret")} {
		if _, err := a.Authenticate(bad); err != ErrUnauthorized {
			t.Errorf("token %q: want unauthorized, got %v", bad, err)
		}
	}
	if _, err := doc(t, "secret", true).Authenticate("secret"); err != ErrUnauthorized {
		t.Error("revoked token accepted")
	}
}

func TestRoles(t *testing.T) {
	a := doc(t, "s", false)
	if a.RoleFor("pm", "vc") != Owner || a.RoleFor("pm", "other") != Read || a.RoleFor("ghost", "vc") != None {
		t.Fatal("role resolution wrong")
	}
}

func TestParseRejects(t *testing.T) {
	for _, in := range []string{`{`, `{"bots":{"x":{"token_sha256":"short","roles":{}}}}`,
		fmt.Sprintf(`{"bots":{"x":{"token_sha256":%q,"roles":{"p":"admin"}}}}`, HashToken("t"))} {
		if _, err := Parse([]byte(in)); err == nil {
			t.Errorf("accepted %s", in)
		}
	}
}

func TestNewToken(t *testing.T) {
	tok, h, err := NewToken()
	if err != nil || HashToken(tok) != h || len(tok) != 67 {
		t.Fatal(tok, h, err)
	}
}
