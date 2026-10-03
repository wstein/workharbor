#!/usr/bin/env python3
"""Spike #174 driver: an SDK-type MCP server answered over Claude Code's stdio
control channel (stream-json). Throwaway spike code.

The driver plays the supervisor: it declares one SDK MCP server ("wh") in the
control `initialize` request, answers the CLI's `mcp_message` control requests
with JSON-RPC results, and logs every line in both directions with a
millisecond offset. Message shapes below come from strings in the Claude Code
2.1.288 binary and are UNVERIFIED until a run shows them.

Usage: mcpdriver.py <case> [--results DIR]
Cases: a1 a2 a3 a4 a5 a6 a7 (see README.md). Environment:
  AGENT_CMD       command that starts the agent with stdin/stdout on the channel
                  (default: container exec -i ... claude ..., see agent_cmd())
  AGENT_CONTAINER container name (default whtmp-mcp-ag1)
  TOKEN_FILE      0600 env file for the signed-in session (cases a2..a7 only)
  MODEL           claude model alias (default haiku)
  ASK_SECONDS     override a3's delays, comma list (default 10,300,never)
  NEVER_CAP       seconds to wait for the "never" answer (default 900)
"""
import json, os, queue, shlex, signal, subprocess, sys, threading, time, uuid

CONTAINER = os.environ.get("AGENT_CONTAINER", "whtmp-mcp-ag1")
TOOLS = os.environ.get("TOOLS_DIR", "/tools")
TOKEN_FILE = os.environ.get("TOKEN_FILE")
MODEL = os.environ.get("MODEL", "haiku")
SERVER = "wh"
RESULTS = "results"


def agent_cmd(extra=()):
    if os.environ.get("AGENT_CMD"):
        return shlex.split(os.environ["AGENT_CMD"]) + list(extra)
    c = ["container", "exec", "-i"]
    if TOKEN_FILE:
        c += ["--env-file", TOKEN_FILE]
    c += ["-w", "/work", CONTAINER, f"{TOOLS}/whr-shim", "run", "-pidfile", "/work/agent.pid", "--",
          f"{TOOLS}/claude", "-p", "--verbose", "--output-format", "stream-json", "--input-format", "stream-json",
          "--strict-mcp-config", "--permission-mode", "manual", "--permission-prompt-tool", "stdio",
          "--model", MODEL]
    return c + list(extra)


def log(msg):
    print(f"[{time.strftime('%H:%M:%S')}] {msg}", flush=True)


class Session:
    """One agent process on the control channel, with a full wire log."""

    def __init__(self, name, tools, extra_args=(), approve=True, identity=None):
        self.name, self.tools, self.approve = name, tools, approve
        self.identity = identity or f"run-{uuid.uuid4().hex[:8]}"
        self.t0 = time.time()
        self.wire = open(os.path.join(RESULTS, f"{name}-wire.jsonl"), "w")
        self.events = []          # (t_ms, direction, obj)
        self.q = queue.Queue()
        self.pending = {}         # request_id -> Queue for our control requests
        self.calls = []           # per tools/call: dict with timings
        self.cancelled = []
        self.listed = False
        self.hold = threading.Event()   # set to release a blocked ask
        self.p = subprocess.Popen(agent_cmd(extra_args), stdin=subprocess.PIPE, stdout=subprocess.PIPE,
                                  stderr=subprocess.PIPE, text=True, bufsize=1, start_new_session=True)
        threading.Thread(target=self._read, daemon=True).start()
        threading.Thread(target=self._err, daemon=True).start()
        self.stderr = []

    def ms(self):
        return int((time.time() - self.t0) * 1000)

    def _rec(self, d, obj):
        t = self.ms()
        self.events.append((t, d, obj))
        self.wire.write(json.dumps({"t_ms": t, "dir": d, "msg": obj}) + "\n")
        self.wire.flush()

    def _err(self):
        for line in self.p.stderr:
            self.stderr.append(line)
            self._rec("stderr", line.rstrip())

    def send(self, obj):
        self._rec("out", obj)
        try:
            self.p.stdin.write(json.dumps(obj) + "\n")
            self.p.stdin.flush()
        except (BrokenPipeError, ValueError):
            self._rec("note", "stdin closed")

    def _read(self):
        for line in self.p.stdout:
            try:
                obj = json.loads(line)
            except ValueError:
                self._rec("in-raw", line.rstrip())
                continue
            self._rec("in", obj)
            if obj.get("type") == "control_request":
                threading.Thread(target=self._serve, args=(obj,), daemon=True).start()
            elif obj.get("type") == "control_response":
                rid = obj.get("response", {}).get("request_id")
                if rid in self.pending:
                    self.pending[rid].put(obj)
            else:
                self.q.put(obj)
        self.q.put({"type": "_eof"})

    # -- supervisor side ---------------------------------------------------
    def _reply(self, rid, response, error=None):
        body = {"subtype": "success", "request_id": rid, "response": response}
        if error:
            body = {"subtype": "error", "request_id": rid, "error": error}
        self.send({"type": "control_response", "response": body})

    def _serve(self, obj):
        rid, req = obj["request_id"], obj["request"]
        st = req.get("subtype")
        if st == "mcp_message" and req.get("server_name") == SERVER:
            out = self._mcp(req["message"])
            self._reply(rid, {"mcp_response": out} if out is not None else {})
        elif st == "can_use_tool":
            self._reply(rid, {"behavior": "allow", "updatedInput": req.get("input", {})} if self.approve
                        else {"behavior": "deny", "message": "spike deny"})
        else:
            self._reply(rid, None, error=f"unsupported {st}")

    def _mcp(self, m):
        method, mid = m.get("method"), m.get("id")
        if mid is None:
            if method == "notifications/cancelled":
                self.cancelled.append((self.ms(), m))
            return None
        ok = lambda r: {"jsonrpc": "2.0", "id": mid, "result": r}
        if method == "initialize":
            version = m["params"].get("protocolVersion", "2025-06-18")
            info = {"name": SERVER, "version": "0"}
            return ok({"protocolVersion": version, "capabilities": {"tools": {}}, "serverInfo": info})
        if method == "tools/list":
            self.listed = True
            return ok({"tools": self.tools})
        if method == "tools/call":
            return self._call(m, ok)
        return {"jsonrpc": "2.0", "id": mid, "error": {"code": -32601, "message": f"no {method}"}}

    def _call(self, m, ok):
        name, args = m["params"]["name"], m["params"].get("arguments") or {}
        c = {"tool": name, "args": args, "recv_ms": self.ms()}
        self.calls.append(c)
        if name == "echo":
            text = "x" * int(args.get("bytes", 1024))
        elif name == "whoami":
            text = self.identity
        elif name == "ask":
            d = args.get("delay_s", 0)
            if d < 0:
                self.hold.wait()      # never answered (until the case releases it)
            else:
                self.hold.wait(d)
            text = f"answered after {d}s"
        else:
            return {"jsonrpc": "2.0", "id": m["id"], "error": {"code": -32602, "message": "unknown tool"}}
        c["resp_ms"] = self.ms()
        return ok({"content": [{"type": "text", "text": text}]})

    # -- client side -------------------------------------------------------
    def control(self, request, timeout=30):
        rid = str(uuid.uuid4())
        self.pending[rid] = queue.Queue()
        self.send({"type": "control_request", "request_id": rid, "request": request})
        try:
            return self.pending[rid].get(timeout=timeout)
        except queue.Empty:
            return None

    def initialize(self, wait_list=30):
        """Send initialize, then wait for the CLI's tools/list (it may only connect on the first turn)."""
        r = self.control({"subtype": "initialize", "sdkMcpServers": [SERVER]})
        end = time.time() + wait_list
        while time.time() < end and not self.listed:
            time.sleep(0.1)
        return r

    def prompt(self, text):
        self.send({"type": "user", "message": {"role": "user", "content": [{"type": "text", "text": text}]}})

    def until_result(self, timeout):
        end, out = time.time() + timeout, []
        while time.time() < end:
            try:
                o = self.q.get(timeout=1)
            except queue.Empty:
                continue
            out.append(o)
            if o.get("type") in ("result", "_eof"):
                return out
        return out

    def close(self, kill=False):
        try:
            if kill:
                os.killpg(self.p.pid, signal.SIGKILL)
            else:
                self.p.stdin.close()
        except Exception:
            pass
        try:
            self.p.wait(timeout=20)
        except subprocess.TimeoutExpired:
            os.killpg(self.p.pid, signal.SIGKILL)
        self.wire.close()


def tool_results(stream):
    """(tool_use name, is_error, content length) for each tool_result in the stream."""
    out = []
    for o in stream:
        if o.get("type") != "user":
            continue
        for b in (o.get("message", {}).get("content") or []):
            if isinstance(b, dict) and b.get("type") == "tool_result":
                c = b.get("content")
                n = len(c) if isinstance(c, str) else len(json.dumps(c))
                out.append({"is_error": b.get("is_error"), "len": n, "head": (c if isinstance(c, str) else json.dumps(c))[:120]})
    return out


def save(name, data):
    with open(os.path.join(RESULTS, f"{name}.json"), "w") as f:
        json.dump(data, f, indent=2, default=str)
    log(f"{name}: {json.dumps(data, default=str)[:600]}")


TOOLS_ALL = [
    {"name": "echo", "description": "Return N bytes of text.", "inputSchema": {
        "type": "object", "properties": {"bytes": {"type": "integer"}}, "required": ["bytes"]}},
    {"name": "ask", "description": "Block until the human answers (delay_s seconds; -1 never).", "inputSchema": {
        "type": "object", "properties": {"delay_s": {"type": "number"}}, "required": ["delay_s"]}},
    {"name": "whoami", "description": "Return this run's identity.", "inputSchema": {"type": "object", "properties": {}}},
]
FULL = lambda n: f"mcp__{SERVER}__{n}"


def case_a1():
    """A.1: init + tools/list, no model turn, so no credential is needed if the CLI connects at initialize."""
    s = Session("a1", TOOLS_ALL)
    init = s.initialize()
    time.sleep(float(os.environ.get("A1_WAIT", "15")))
    status = s.control({"subtype": "mcp_status"})
    listed = [e for e in s.events if e[1] == "in" and e[2].get("type") == "control_request"
              and e[2]["request"].get("subtype") == "mcp_message"]
    methods = [e[2]["request"]["message"].get("method") for e in listed]
    s.close()
    save("a1", {"initialize_response": init, "mcp_status": status, "mcp_methods_seen": methods,
                "declared_tools": [t["name"] for t in TOOLS_ALL], "stderr": s.stderr[:5]})


def run_turn(s, text, timeout=240):
    s.prompt(text)
    return s.until_result(timeout)


def case_a2():
    s = Session("a2", TOOLS_ALL)
    s.initialize()
    res = {}
    for kb, n in (("1KB", 1024), ("64KB", 65536), ("1MB", 1048576), ("10MB", 10485760)):
        s.calls.clear()
        out = run_turn(s, f"Call the tool {FULL('echo')} exactly once with bytes={n} and then reply DONE. Do not repeat the output.")
        ms = [c.get("resp_ms", 0) - c["recv_ms"] for c in s.calls]
        res[kb] = {"server_ms": ms, "calls": len(s.calls), "model_saw": tool_results(out)}
    s.close()
    save("a2", res)


def case_a3():
    delays = os.environ.get("ASK_SECONDS", "10,300,never").split(",")
    res = {}
    for d in delays:
        s = Session(f"a3-{d}", TOOLS_ALL)
        s.initialize()
        secs = -1 if d == "never" else float(d)
        cap = float(os.environ.get("NEVER_CAP", "900")) if d == "never" else secs + 120
        t = time.time()
        s.prompt(f"Call the tool {FULL('ask')} exactly once with delay_s={secs} and then reply DONE.")
        out = s.until_result(cap)
        res[d] = {"turn_s": round(time.time() - t, 1), "call": s.calls[-1] if s.calls else None,
                  "model_saw": tool_results(out), "cancel_notifications": s.cancelled,
                  "ended_with": out[-1].get("type") if out else None}
        s.hold.set()
        s.close(kill=(d == "never"))
    save("a3", res)


def case_a4():
    """A.4: an approval request while an ask is blocked (D26 / spike #7)."""
    s = Session("a4", TOOLS_ALL)
    s.initialize()
    s.prompt(f"First call {FULL('ask')} with delay_s=20. Then run the Bash command `echo approved-while-blocked`. Then reply DONE.")
    out = s.until_result(240)
    ev = [(t, e[2]["request"].get("subtype"), e[2]["request"].get("tool_name") or e[2]["request"].get("message", {}).get("method"))
          for t, e in ((x[0], x) for x in s.events) if e[1] == "in" and e[2].get("type") == "control_request"]
    s.close()
    save("a4", {"order": ev, "calls": s.calls, "model_saw": tool_results(out)})


def case_a5():
    """A.5: planted .mcp.json + user settings refused; name collision. Setup is done by run-nocred.sh/README."""
    s = Session("a5", TOOLS_ALL)
    s.initialize()
    time.sleep(10)
    status = s.control({"subtype": "mcp_status"})
    out = run_turn(s, f"List every MCP tool you have. Call {FULL('whoami')} once. Reply DONE.")
    s.close()
    save("a5", {"mcp_status": status, "model_saw": tool_results(out), "calls": s.calls,
                "marker_note": "check /work/PLANTED_RAN and /root/PLANTED_RAN in the guest: absent means refused"})


def case_a6():
    ids = {}
    for n in ("run1", "run2"):
        s = Session(f"a6-{n}", TOOLS_ALL, identity=f"identity-{n}")
        s.initialize()
        out = run_turn(s, f"Call {FULL('whoami')} once with no arguments and reply with exactly what it returns.")
        ids[n] = {"model_saw": tool_results(out), "calls": s.calls}
        s.close()
    schema_args = {t["name"]: list(t["inputSchema"].get("properties", {})) for t in TOOLS_ALL}
    save("a6", {"runs": ids, "declared_args": schema_args,
                "note": "whoami has no argument; identity comes from the Session, i.e. the channel"})


def case_a7():
    """A.7: exec killed mid-call (kill) and stdin closed mid-call (eof); guest process check is done by run-nocred.sh/README."""
    res = {}
    for mode in ("kill", "eof"):
        s = Session(f"a7-{mode}", TOOLS_ALL)
        s.initialize()
        s.prompt(f"Call {FULL('ask')} with delay_s=-1 and then reply DONE.")
        for _ in range(120):
            if s.calls:
                break
            time.sleep(1)
        t = time.time()
        s.close(kill=(mode == "kill"))
        seen = tool_results([e[2] for e in s.events if e[1] == "in"])
        res[mode] = {"call_seen": bool(s.calls), "exit_code": s.p.returncode}
        res[mode]["closed_after_s"] = round(time.time() - t, 1)
        res[mode]["tool_result_after_close"] = seen
    save("a7", res)


if __name__ == "__main__":
    case = sys.argv[1]
    if "--results" in sys.argv:
        RESULTS = sys.argv[sys.argv.index("--results") + 1]
    os.makedirs(RESULTS, exist_ok=True)
    globals()["case_" + case]()
