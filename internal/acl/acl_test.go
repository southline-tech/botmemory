package acl

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func doc(t *testing.T, tok string, revoked bool) *ACL {
	t.Helper()
	a, err := Parse([]byte(fmt.Sprintf(`{"bots":{"pm":{"token_sha256":%q,"revoked":%v,"roles":{"vc":"owner","org":"read"}}}}`, HashToken(tok), revoked)))
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
	if a.RoleFor("pm", "vc") != Owner || a.RoleFor("pm", "org") != Read || a.RoleFor("pm", "other") != None || a.RoleFor("ghost", "vc") != None {
		t.Fatal("role resolution wrong")
	}
}

func TestParseRejects(t *testing.T) {
	for _, in := range []string{`{`, `{"bots":{"x":{"token_sha256":"short","roles":{}}}}`,
		fmt.Sprintf(`{"bots":{"x":{"token_sha256":%q,"roles":{"p":"admin"}}}}`, HashToken("t")),
		fmt.Sprintf(`{"bots":{"x":{"token_sha256":%q,"roles":{"*":"read"}}}}`, HashToken("t"))} {
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

func TestParseRejectsDupHashAndBadIDs(t *testing.T) {
	h := HashToken("t")
	for _, in := range []string{
		fmt.Sprintf(`{"bots":{"a":{"token_sha256":%q,"roles":{}},"b":{"token_sha256":%q,"roles":{}}}}`, h, h),
		fmt.Sprintf(`{"bots":{"Bad Id":{"token_sha256":%q,"roles":{}}}}`, h),
		fmt.Sprintf(`{"bots":{"a":{"token_sha256":%q,"roles":{}}}}`, strings.ToUpper(h)),
	} {
		if _, err := Parse([]byte(in)); err == nil {
			t.Errorf("accepted %s", in)
		}
	}
}

func TestLoaderFailsClosed(t *testing.T) {
	good := []byte(fmt.Sprintf(`{"bots":{"pm":{"token_sha256":%q,"roles":{"vc":"owner"}}}}`, HashToken("tok")))
	var data []byte = good
	var ferr error
	l := &Loader{MaxFailures: 3, Fetch: func() ([]byte, error) { return data, ferr }}
	if l.Load() != nil {
		t.Fatal("initial load")
	}
	ferr = errors.New("drive down")
	for i := 1; i <= 2; i++ {
		l.Load()
		if _, err := l.Current().Authenticate("tok"); err != nil {
			t.Fatalf("failure %d: should keep previous ACL in grace period", i)
		}
	}
	l.Load() // 3rd failure
	if _, err := l.Current().Authenticate("tok"); err != ErrUnauthorized {
		t.Fatal("did not fail closed")
	}
	ferr, data = nil, []byte("{corrupt")
	l.Load()
	if _, err := l.Current().Authenticate("tok"); err != ErrUnauthorized {
		t.Fatal("corrupt ACL re-opened access")
	}
	data = good
	if l.Load() != nil {
		t.Fatal("recover")
	}
	if _, err := l.Current().Authenticate("tok"); err != nil {
		t.Fatal("did not recover")
	}
	if _, err := (&Loader{MaxFailures: 3, Fetch: func() ([]byte, error) { return nil, errors.New("x") }}).Current().Authenticate("tok"); err != ErrUnauthorized {
		t.Fatal("never-loaded loader must deny")
	}
}
