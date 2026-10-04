#!/usr/bin/env python3
import json, os, sys, time

default_log = os.path.join(os.path.dirname(os.path.abspath(__file__)), "results", "c-mock-wire.jsonl")
log_file = os.environ.get("MOCK_LOG_FILE", default_log)
logfile = open(log_file, "a") if log_file else None

def log(direction, data):
    if logfile:
        entry = {"t": time.time(), "dir": direction, "data": data}
        logfile.write(json.dumps(entry) + "\n")
        logfile.flush()

def respond(id, result=None, error=None):
    resp = {"jsonrpc": "2.0", "id": id}
    if error is not None:
        resp["error"] = error
    else:
        resp["result"] = result
    log("out", resp)
    sys.stdout.write(json.dumps(resp) + "\n")
    sys.stdout.flush()

for line in sys.stdin:
    line = line.strip()
    if not line:
        continue
    try:
        req = json.loads(line)
    except Exception:
        continue
    log("in", req)
    method = req.get("method")
    mid = req.get("id")
    
    if method == "initialize":
        respond(mid, {
            "protocolVersion": "2024-11-05",
            "capabilities": {"tools": {}},
            "serverInfo": {"name": "wh-mock", "version": "1.0"}
        })
    elif method == "notifications/initialized":
        pass
    elif method == "tools/list":
        respond(mid, {
            "tools": [
                {
                    "name": "wh_ping",
                    "description": "returns pong response from workharbor mock MCP",
                    "inputSchema": {
                        "type": "object",
                        "properties": {
                            "msg": {"type": "string", "description": "message to echo"}
                        },
                        "required": ["msg"]
                    }
                }
            ]
        })
    elif method == "tools/call":
        args = req.get("params", {}).get("arguments", {})
        respond(mid, {
            "content": [{"type": "text", "text": f"wh-pong: {args.get('msg', 'hello')}"}]
        })
    elif mid is not None:
        respond(mid, error={"code": -32601, "message": f"unknown method {method}"})
