package main

import (
	"strings"
	"testing"
)

func TestCheckAddrLoopbackOnly(t *testing.T) {
	for addr, ok := range map[string]bool{
		"127.0.0.1:8787": true, "localhost:8787": false, "[::1]:8787": true, "127.0.0.2:1": true,
		"0.0.0.0:8787": false, ":8787": false, "[::]:8787": false, "10.0.0.5:8787": false,
		"example.com:80": false, "nonsense": false,
	} {
		if err := checkAddr(addr); (err == nil) != ok {
			t.Errorf("%q: err=%v want ok=%v", addr, err, ok)
		}
	}
}

func TestNormalizeLocalhost(t *testing.T) {
	for in, want := range map[string]string{"localhost:8787": "127.0.0.1:8787", "LOCALHOST:1": "127.0.0.1:1", "127.0.0.1:2": "127.0.0.1:2"} {
		if got := normalizeAddr(in); got != want {
			t.Errorf("%s -> %s want %s", in, got, want)
		}
		if checkAddr(normalizeAddr(in)) != nil {
			t.Errorf("%s rejected after normalize", in)
		}
	}
}

func TestLockPathFixedPerRoot(t *testing.T) {
	t.Setenv("BOTMEMORY_LOCK_FILE", "")
	a := lockPath("root-A")
	t.Setenv("TMPDIR", "/elsewhere")
	t.Setenv("XDG_RUNTIME_DIR", "/run/user/999")
	if lockPath("root-A") != a {
		t.Fatal("lock path varies with environment")
	}
	if !strings.HasPrefix(a, "/var/tmp/botmemory-") || !strings.HasSuffix(a, ".lock") || len(a) != len("/var/tmp/botmemory-")+12+5 {
		t.Fatalf("unexpected path %s", a)
	}
	if lockPath("root-B") == a {
		t.Fatal("different roots share a lock")
	}
	t.Setenv("BOTMEMORY_LOCK_FILE", "/custom.lock")
	if lockPath("root-A") != "/custom.lock" {
		t.Fatal("override ignored")
	}
}

func TestBootstrapTestFolderEnv(t *testing.T) {
	t.Setenv("BOTMEMORY_TEST_FOLDER", "")
	if bootstrapTestFolder(nil) {
		t.Fatal("empty env should be false")
	}
	for _, v := range []string{"1", "true", "YES", " True "} {
		t.Setenv("BOTMEMORY_TEST_FOLDER", v)
		if !bootstrapTestFolder(nil) {
			t.Fatalf("truthy %q", v)
		}
	}
	if !bootstrapTestFolder([]string{"--test-folder"}) {
		t.Fatal("--test-folder flag should enable test-folder mode")
	}
	t.Setenv("BOTMEMORY_TEST_FOLDER", "0")
	if !bootstrapTestFolder([]string{"--test-folder"}) {
		t.Fatal("--test-folder should win over env")
	}
}
