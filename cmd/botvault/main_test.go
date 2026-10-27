package main

import "testing"

func TestCheckAddrLoopbackOnly(t *testing.T) {
	for addr, ok := range map[string]bool{
		"127.0.0.1:8787": true, "localhost:8787": true, "[::1]:8787": true, "127.0.0.2:1": true,
		"0.0.0.0:8787": false, ":8787": false, "[::]:8787": false, "10.0.0.5:8787": false,
		"example.com:80": false, "nonsense": false,
	} {
		if err := checkAddr(addr); (err == nil) != ok {
			t.Errorf("%q: err=%v want ok=%v", addr, err, ok)
		}
	}
}
