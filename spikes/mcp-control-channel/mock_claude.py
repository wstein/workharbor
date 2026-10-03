#!/usr/bin/env python3
"""A stand-in for Claude Code that speaks the assumed control protocol, so the
driver's own logic can be checked on a Mac without a container or a login.
It proves NOTHING about real Claude Code: the message shapes here are the
driver's assumptions. Handles a1, a2 and a6 only."""
import json, re, sys, threading, uuid

lock, waiting = threading.Lock(), {}

def out(o):
    with lock:
        print(json.dumps(o), flush=True)

def ask_driver(server, msg, wait=True):
    rid = str(uuid.uuid4())
    ev = threading.Event(); waiting[rid] = [ev, None]
    out({"type": "control_request", "request_id": rid, "request": {"subtype": "mcp_message", "server_name": server, "message": msg}})
    if wait:
        ev.wait(30)
        return waiting[rid][1]

def handle_init(rid, req):
    out({"type": "control_response", "response": {"subtype": "success", "request_id": rid, "response": {}}})
    for s in req.get("sdkMcpServers", []):
        ask_driver(s, {"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": {"protocolVersion": "2025-06-18"}})
        ask_driver(s, {"jsonrpc": "2.0", "method": "notifications/initialized"}, wait=False)
        state["tools"] = ask_driver(s, {"jsonrpc": "2.0", "id": 2, "method": "tools/list"})
        state["server"] = s

state = {}
for line in sys.stdin:
    o = json.loads(line)
    if o["type"] == "control_response":
        r = o["response"]; w = waiting.get(r["request_id"])
        if w:
            w[1] = (r.get("response") or {}).get("mcp_response"); w[0].set()
    elif o["type"] == "control_request":
        req = o["request"]
        if req["subtype"] == "initialize":
            threading.Thread(target=handle_init, args=(o["request_id"], req), daemon=True).start()
        elif req["subtype"] == "mcp_status":
            names = [t["name"] for t in state["tools"]["result"]["tools"]]
            srv = {"name": state["server"], "status": "connected", "tools": names}
            out({"type": "control_response", "response": {"subtype": "success", "request_id": o["request_id"],
                "response": {"mcpServers": [srv]}}})
    elif o["type"] == "user":
        text = o["message"]["content"][0]["text"]
        m = re.search(r"mcp__wh__(\w+)", text)
        n = re.search(r"bytes=(\d+)", text)
        args = {"bytes": int(n.group(1))} if n else {}
        def turn():
            r = ask_driver(state["server"], {"jsonrpc": "2.0", "id": 9, "method": "tools/call", "params": {"name": m.group(1), "arguments": args}})
            c = r["result"]["content"][0]["text"]
            out({"type": "user", "message": {"role": "user", "content": [{"type": "tool_result", "content": c}]}})
            out({"type": "result", "subtype": "success"})
        threading.Thread(target=turn, daemon=True).start()
