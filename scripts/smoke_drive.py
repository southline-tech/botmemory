#!/usr/bin/env python3
"""Real-Drive smoke test for a running botVault server (RAD-280).

Exercises every MCP tool against a live server and prints one PASS/FAIL line
per check. Reads everything from the environment; never prints the token.

  BOTVAULT_SMOKE_TOKEN    bearer token with owner on BOTVAULT_SMOKE_PROJECT (required)
  BOTVAULT_SMOKE_PROJECT  project to use (default: smoke); use a throwaway one
  BOTVAULT_URL            default http://127.0.0.1:8787/mcp
  BOTVAULT_SMOKE_M1=1     only the M1 tools (no search/history/reindex)

Exit status is non-zero if any check fails.
"""
import json, os, sys, time, urllib.error, urllib.request

URL = os.environ.get("BOTVAULT_URL", "http://127.0.0.1:8787/mcp")
TOKEN = os.environ.get("BOTVAULT_SMOKE_TOKEN", "")
PROJECT = os.environ.get("BOTVAULT_SMOKE_PROJECT", "smoke")
M1 = os.environ.get("BOTVAULT_SMOKE_M1") == "1"
if not TOKEN:
    sys.exit("BOTVAULT_SMOKE_TOKEN is not set")


def call(name, args, tok=None):
    """Returns (ok, result-or-error-text). HTTP errors return (False, 'HTTP n')."""
    body = json.dumps({"jsonrpc": "2.0", "id": 1, "method": "tools/call",
                       "params": {"name": name, "arguments": args}}).encode()
    req = urllib.request.Request(URL, body, {"Authorization": "Bearer " + (tok or TOKEN),
                                             "Content-Type": "application/json"})
    try:
        out = json.loads(urllib.request.urlopen(req, timeout=90).read())["result"]
    except urllib.error.HTTPError as e:
        return False, f"HTTP {e.code}"
    if out.get("isError"):
        return False, out["content"][0]["text"]
    return True, out.get("structuredContent", {}).get("result")


fails = 0


def check(label, cond):
    global fails
    fails += not cond
    print(("PASS " if cond else "FAIL ") + label, flush=True)


key = "smoke-" + time.strftime("%Y%m%d%H%M%S")
A = {"project": PROJECT, "key": key}
ok, r = call("put", {**A, "title": "Smoke one", "body": "hello drive", "kind": "note", "tags": ["smoke"]})
check("put create -> v1", ok and r["version"] == 1)
ok, r = call("put", {**A, "title": "again", "body": "x"})
check("put without if_version on existing key -> CONFLICT", not ok and str(r).startswith("CONFLICT"))
ok, r = call("put", {**A, "title": "Smoke two", "body": "hello v2", "if_version": 1})
check("put if_version=1 -> v2, kind kept", ok and r["version"] == 2 and r["kind"] == "note")
ok, r = call("get", A)
check("get -> v2", ok and r["version"] == 2 and r["title"] == "Smoke two")
ok, r = call("get", {**A, "version": 1})
check("get version=1", ok and r["version"] == 1 and r["title"] == "Smoke one")
if not M1:
    ok, r = call("history", A)
    check("history v2,v1", ok and [x["version"] for x in r] == [2, 1])
ok, r = call("list", {"project": PROJECT})
check("list", ok and any(x["key"] == key for x in r))
ok, r = call("list_projects", {})
check("list_projects", ok and PROJECT in r)
ok, r = call("append", {**A, "text": "appended line"})
check("append -> v3", ok and r["version"] == 3 and "appended line" in r["body"])
if not M1:
    ok, r = call("search", {"project": PROJECT, "query": "smoke two"})
    check("search", ok and any(x["key"] == key for x in r))
    ok, r = call("reindex", {"project": PROJECT})
    check("reindex", ok and r["entries"] >= 1)
ok, r = call("delete", {**A, "if_version": 3})
check("delete -> archived v4", ok and r["status"] == "archived" and r["version"] == 4)
ok, r = call("list", {"project": PROJECT, "status": "archived"})
check("list status=archived", ok and any(x["key"] == key for x in r))
if not M1:
    ok, r = call("search", {"project": PROJECT, "query": "smoke two"})
    check("archived hidden from search", ok and not any(x["key"] == key for x in r))
bogus = "bv_smoke_not_in_acl"
check("unknown token denied (list_projects)", call("list_projects", {}, bogus) == (False, "HTTP 401"))
check("unknown token denied (get)", call("get", A, bogus) == (False, "HTTP 401"))
print(f"{'ALL PASS' if not fails else str(fails) + ' FAILED'} (project {PROJECT}, key {key})")
sys.exit(1 if fails else 0)
