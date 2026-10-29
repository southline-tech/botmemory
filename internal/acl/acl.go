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
	"log"
	"regexp"
	"sync"
	"time"
)

var (
	botIDRE = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)
	hashRE  = regexp.MustCompile(`^[0-9a-f]{64}$`)
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
	seen := map[string]string{}
	for id, b := range d.Bots {
		if !botIDRE.MatchString(id) {
			return nil, fmt.Errorf("acl: bot id %q must match %s", id, botIDRE)
		}
		if !hashRE.MatchString(b.TokenSHA256) {
			return nil, fmt.Errorf("acl: bot %q: token_sha256 must be 64 lowercase hex chars", id)
		}
		if other, dup := seen[b.TokenSHA256]; dup {
			return nil, fmt.Errorf("acl: bots %q and %q share a token hash", other, id)
		}
		seen[b.TokenSHA256] = id
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

// DenyAll is an ACL that authenticates nobody (fail-closed state).
func DenyAll() *ACL { return &ACL{bots: map[string]Bot{}, roles: map[string]map[string]Role{}} }

// Loader holds the current ACL and reloads it. After MaxFailures consecutive
// reload failures it fails closed (DenyAll) so a broken or unreadable
// _acl.json can never keep a revoked token alive indefinitely.
type Loader struct {
	Fetch       func() ([]byte, error)
	MaxFailures int

	mu       sync.RWMutex
	cur      *ACL
	failures int
}

// Load fetches and parses; on failure keeps the previous ACL until the
// failure budget is spent, then switches to DenyAll.
func (l *Loader) Load() error {
	data, err := l.Fetch()
	var a *ACL
	if err == nil {
		a, err = Parse(data)
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if err != nil {
		l.failures++
		if l.cur == nil || l.failures >= l.MaxFailures {
			if l.cur != nil {
				log.Printf("!!! botmemory ACL FAIL-CLOSED: %d consecutive reload failures, denying ALL tokens until _acl.json is fixed: %v", l.failures, err)
			}
			l.cur = DenyAll()
		} else {
			log.Printf("!!! botmemory ACL reload failed (%d/%d, keeping previous): %v", l.failures, l.MaxFailures, err)
		}
		return err
	}
	if l.failures >= l.MaxFailures {
		log.Printf("botmemory ACL recovered after %d failures", l.failures)
	}
	l.failures = 0
	l.cur = a
	return nil
}

func (l *Loader) Current() *ACL {
	l.mu.RLock()
	defer l.mu.RUnlock()
	if l.cur == nil {
		return DenyAll()
	}
	return l.cur
}

// Run reloads every interval until stop is closed.
func (l *Loader) Run(interval time.Duration, stop <-chan struct{}) {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-t.C:
			_ = l.Load()
		case <-stop:
			return
		}
	}
}
