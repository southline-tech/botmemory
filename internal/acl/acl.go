// Package acl maps per-bot bearer tokens to bot ids and per-project roles.
// The ACL document (_acl.json at the vault root) stores only SHA-256 hashes
// of tokens, never plaintext, so it is safe to keep in Drive.
package acl

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
)

type Role int

const (
	None Role = iota
	Read
	Write
	Owner
)

func ParseRole(s string) (Role, error) {
	switch s {
	case "read":
		return Read, nil
	case "write":
		return Write, nil
	case "owner":
		return Owner, nil
	}
	return None, fmt.Errorf("acl: unknown role %q", s)
}

func (r Role) String() string {
	return [...]string{"none", "read", "write", "owner"}[r]
}

// Bot is one ACL entry. Roles maps project -> role. Deny by default: a bot
// has no access to any project not listed explicitly (no wildcard).
type Bot struct {
	TokenSHA256 string            `json:"token_sha256"`
	Revoked     bool              `json:"revoked,omitempty"`
	Roles       map[string]string `json:"roles"`
}

// Doc is the on-disk _acl.json shape.
type Doc struct {
	Bots map[string]Bot `json:"bots"`
}

var ErrUnauthorized = errors.New("acl: invalid or revoked token")

// ACL is an immutable, validated view of a Doc.
type ACL struct {
	bots  map[string]Bot
	roles map[string]map[string]Role
}

func Parse(data []byte) (*ACL, error) {
	var d Doc
	if err := json.Unmarshal(data, &d); err != nil {
		return nil, fmt.Errorf("acl: %w", err)
	}
	a := &ACL{bots: d.Bots, roles: map[string]map[string]Role{}}
	for id, b := range d.Bots {
		if len(b.TokenSHA256) != 64 {
			return nil, fmt.Errorf("acl: bot %q: token_sha256 must be 64 hex chars", id)
		}
		m := map[string]Role{}
		for p, s := range b.Roles {
			if p == "*" || p == "" {
				return nil, fmt.Errorf("acl: bot %q: wildcard/empty project not allowed (deny by default; grant projects explicitly)", id)
			}
			r, err := ParseRole(s)
			if err != nil {
				return nil, fmt.Errorf("acl: bot %q: %w", id, err)
			}
			m[p] = r
		}
		a.roles[id] = m
	}
	return a, nil
}

// Authenticate resolves a bearer token to a bot id (constant-time compare).
func (a *ACL) Authenticate(token string) (string, error) {
	if token == "" {
		return "", ErrUnauthorized
	}
	h := HashToken(token)
	found := ""
	for id, b := range a.bots {
		if subtle.ConstantTimeCompare([]byte(h), []byte(b.TokenSHA256)) == 1 && !b.Revoked {
			found = id
		}
	}
	if found == "" {
		return "", ErrUnauthorized
	}
	return found, nil
}

// RoleFor returns the bot's explicit role on project, or None.
func (a *ACL) RoleFor(bot, project string) Role {
	return a.roles[bot][project]
}

func HashToken(t string) string {
	s := sha256.Sum256([]byte(t))
	return hex.EncodeToString(s[:])
}

// NewToken returns a random 32-byte token (hex) and its hash.
func NewToken() (token, hash string, err error) {
	b := make([]byte, 32)
	if _, err = rand.Read(b); err != nil {
		return "", "", err
	}
	token = "bv_" + hex.EncodeToString(b)
	return token, HashToken(token), nil
}
