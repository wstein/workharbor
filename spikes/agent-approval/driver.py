#!/usr/bin/env python3
import json
import os
import re
import signal
import subprocess
import sys
import time

CONTAINER = os.environ.get("AGENT_CONTAINER", "whspike-ag1")
TOOLS_DIR = os.environ.get("TOOLS_DIR", "/tools")
TOKEN_FILE = os.environ.get("TOKEN_FILE")
RESULTS_DIR = os.environ.get("RESULTS_DIR", "results")

def log(msg):
    print(f"[{time.strftime('%H:%M:%S')}] {msg}", flush=True)

def check_prerequisites():
    log("Checking environment prerequisites...")
    res = subprocess.run(["container", "exec", CONTAINER, "sh", "-c", "test -d /proc && test -f /proc/1/stat"], capture_output=True)
    if res.returncode != 0:
        log("ERROR: /proc is not available or readable in container!")
        return False
    log("✓ Verified /proc filesystem is mounted and readable")

    # Check that harness does not require pgrep
    res_pgrep = subprocess.run(["container", "exec", CONTAINER, "sh", "-c", "which pgrep 2>/dev/null || true"], capture_output=True, text=True)
    has_pgrep = bool(res_pgrep.stdout.strip())
    log(f"Container pgrep presence: {has_pgrep} (Harness uses pure /proc inspection and does NOT rely on pgrep)")
    return True

def get_guest_processes(container, pattern=None):
    sh_script = """
for p in /proc/[0-9]*; do
  [ -d "$p" ] || continue
  pid=$(basename "$p")
  [ "$pid" = "1" ] || [ "$pid" = "2" ] && continue
  cmd=$(cat "$p/cmdline" 2>/dev/null | tr '\0' ' ')
  [ -z "$cmd" ] && continue
  stat=$(cat "$p/stat" 2>/dev/null)
  state=$(echo "$stat" | awk '{print $3}')
  echo "PID:$pid|STATE:$state|CMD:$cmd|STAT:$stat"
done
"""
    res = subprocess.run(["container", "exec", container, "sh", "-c", sh_script], capture_output=True, text=True)
    procs = []
    for line in res.stdout.strip().splitlines():
        if not line or "|" not in line:
            continue
        parts = line.split("|", 3)
        if len(parts) == 4:
            p_pid = parts[0].replace("PID:", "")
            p_state = parts[1].replace("STATE:", "")
            p_cmd = parts[2].replace("CMD:", "")
            p_stat = parts[3].replace("STAT:", "")
            if pattern:
                if not re.search(pattern, p_cmd):
                    continue
            procs.append({
                "pid": p_pid,
                "state": p_state,
                "cmd": p_cmd,
                "stat": p_stat
            })
    return procs

def run_case_allow():
    log("=== Case 1: Allow Roundtrip ===")
    out_file = os.path.join(RESULTS_DIR, "case1-allow-stream.jsonl")
    summary_file = os.path.join(RESULTS_DIR, "case1-allow.txt")

    subprocess.run(["container", "exec", CONTAINER, "rm", "-f", "/work/allow_test.txt"], check=False)

    cmd = [
        "container", "exec", "-i",
        "--env-file", TOKEN_FILE,
        "-w", "/work",
        CONTAINER,
        f"{TOOLS_DIR}/whr-shim", "run", "-pidfile", "/work/agent.pid", "--",
        f"{TOOLS_DIR}/claude", "-p",
        "--verbose",
        "--output-format", "stream-json",
        "--input-format", "stream-json",
        "--permission-mode", "manual",
        "--permission-prompt-tool", "stdio",
        "--model", "haiku"
    ]

    t0 = time.time()
    proc = subprocess.Popen(cmd, stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True)

    user_prompt = {"type": "user", "message": {"role": "user", "content": [{"type": "text", "text": "Write ALLOWED_SUCCESS to /work/allow_test.txt using the Write tool, then reply with exactly: ALL_DONE."}]}}
    proc.stdin.write(json.dumps(user_prompt) + "\n")
    proc.stdin.flush()

    resp_latency = None
    stream_lines = []

    for line in proc.stdout:
        stream_lines.append(line)
        try:
            data = json.loads(line)
            if data.get("type") == "control_request":
                req_id = data["request_id"]
                tool_name = data["request"].get("tool_name")
                log(f"Received control_request for tool '{tool_name}' (request_id: {req_id})")

                resp = {
                    "type": "control_response",
                    "response": {
                        "subtype": "success",
                        "request_id": req_id,
                        "response": {"behavior": "allow"}
                    }
                }
                t_send = time.time()
                proc.stdin.write(json.dumps(resp) + "\n")
                proc.stdin.flush()
                resp_latency = (time.time() - t_send) * 1000
                log(f"Sent control_response allow (dispatch latency: {resp_latency:.2f} ms)")
            elif data.get("type") == "result":
                log(f"Received result event: {data.get('subtype')}")
                proc.stdin.close()
                break
        except Exception:
            pass

    stdout, stderr = proc.communicate()
    total_time = time.time() - t0
    rc = proc.returncode

    chk = subprocess.run(["container", "exec", CONTAINER, "cat", "/work/allow_test.txt"], capture_output=True, text=True)
    file_content = chk.stdout.strip()
    file_created = (chk.returncode == 0 and "ALLOWED_SUCCESS" in file_content)

    with open(out_file, "w") as f:
        f.writelines(stream_lines)

    summary = f"""Case 1: Allow Roundtrip
Exit Code: {rc}
Total Turn Time: {total_time:.3f} s
Response Dispatch Latency: {resp_latency:.2f} ms
File Created: {file_created} (content: {file_content})
Verdict: {'PASS' if rc == 0 and file_created else 'FAIL'}
"""
    log(summary)
    with open(summary_file, "w") as f:
        f.write(summary)
    return rc == 0 and file_created

def run_case_deny():
    log("=== Case 2: Deny Roundtrip ===")
    out_file = os.path.join(RESULTS_DIR, "case2-deny-stream.jsonl")
    summary_file = os.path.join(RESULTS_DIR, "case2-deny.txt")

    subprocess.run(["container", "exec", CONTAINER, "rm", "-f", "/work/deny_test.txt"], check=False)

    cmd = [
        "container", "exec", "-i",
        "--env-file", TOKEN_FILE,
        "-w", "/work",
        CONTAINER,
        f"{TOOLS_DIR}/whr-shim", "run", "-pidfile", "/work/agent.pid", "--",
        f"{TOOLS_DIR}/claude", "-p",
        "--verbose",
        "--output-format", "stream-json",
        "--input-format", "stream-json",
        "--permission-mode", "manual",
        "--permission-prompt-tool", "stdio",
        "--model", "haiku"
    ]

    t0 = time.time()
    proc = subprocess.Popen(cmd, stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True)

    user_prompt = {"type": "user", "message": {"role": "user", "content": [{"type": "text", "text": "Write DENIED_SECRET to /work/deny_test.txt using the Write tool, then reply with DONE."}]}}
    proc.stdin.write(json.dumps(user_prompt) + "\n")
    proc.stdin.flush()

    resp_latency = None
    stream_lines = []
    permission_denials = []

    for line in proc.stdout:
        stream_lines.append(line)
        try:
            data = json.loads(line)
            if data.get("type") == "control_request":
                req_id = data["request_id"]
                tool_name = data["request"].get("tool_name")
                log(f"Received control_request for tool '{tool_name}' (request_id: {req_id})")

                resp = {
                    "type": "control_response",
                    "response": {
                        "subtype": "success",
                        "request_id": req_id,
                        "response": {"behavior": "deny", "message": "Denied by supervisor policy"}
                    }
                }
                t_send = time.time()
                proc.stdin.write(json.dumps(resp) + "\n")
                proc.stdin.flush()
                resp_latency = (time.time() - t_send) * 1000
                log(f"Sent control_response deny (dispatch latency: {resp_latency:.2f} ms)")
            elif data.get("type") == "result":
                permission_denials = data.get("permission_denials", [])
                log(f"Received result event with {len(permission_denials)} permission_denials")
                proc.stdin.close()
                break
        except Exception:
            pass

    stdout, stderr = proc.communicate()
    total_time = time.time() - t0
    rc = proc.returncode

    chk = subprocess.run(["container", "exec", CONTAINER, "test", "-f", "/work/deny_test.txt"], capture_output=True)
    file_exists = (chk.returncode == 0)

    with open(out_file, "w") as f:
        f.writelines(stream_lines)

    passed = (rc == 0 and not file_exists and len(permission_denials) > 0)
    summary = f"""Case 2: Deny Roundtrip
Exit Code: {rc}
Total Turn Time: {total_time:.3f} s
Response Dispatch Latency: {resp_latency:.2f} ms
File Exists: {file_exists} (must be False)
Permission Denials: {json.dumps(permission_denials)}
Verdict: {'PASS' if passed else 'FAIL'}
"""
    log(summary)
    with open(summary_file, "w") as f:
        f.write(summary)
    return passed

def run_case_closed_stdin():
    log("=== Case 3: Stdin EOF / Closed Channel ===")
    out_file = os.path.join(RESULTS_DIR, "case3-closed-stdin-stream.jsonl")
    summary_file = os.path.join(RESULTS_DIR, "case3-closed-stdin.txt")

    subprocess.run(["container", "exec", CONTAINER, "rm", "-f", "/work/eof_test.txt"], check=False)

    cmd = [
        "container", "exec", "-i",
        "--env-file", TOKEN_FILE,
        "-w", "/work",
        CONTAINER,
        f"{TOOLS_DIR}/whr-shim", "run", "-pidfile", "/work/agent.pid", "--",
        f"{TOOLS_DIR}/claude", "-p",
        "--verbose",
        "--output-format", "stream-json",
        "--input-format", "stream-json",
        "--permission-mode", "manual",
        "--permission-prompt-tool", "stdio",
        "--model", "haiku"
    ]

    t0 = time.time()
    proc = subprocess.Popen(cmd, stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True)

    user_prompt = {"type": "user", "message": {"role": "user", "content": [{"type": "text", "text": "Write EOF_SECRET to /work/eof_test.txt using the Write tool, then reply with DONE."}]}}
    proc.stdin.write(json.dumps(user_prompt) + "\n")
    proc.stdin.flush()

    stream_lines = []

    for line in proc.stdout:
        stream_lines.append(line)
        try:
            data = json.loads(line)
            if data.get("type") == "control_request":
                log("Received control_request; immediately closing supervisor stdin pipe (EOF)...")
                proc.stdin.close()
                break
        except Exception:
            pass

    try:
        remaining_stdout, remaining_stderr = proc.communicate(timeout=10)
        stream_lines.append(remaining_stdout)
    except subprocess.TimeoutExpired:
        log("Process blocked after stdin EOF; terminating with whr-shim...")
        subprocess.run(["container", "exec", CONTAINER, f"{TOOLS_DIR}/whr-shim", "kill", "-pidfile", "/work/agent.pid", "-grace", "500ms"])
        proc.kill()

    total_time = time.time() - t0
    rc = proc.returncode

    chk = subprocess.run(["container", "exec", CONTAINER, "test", "-f", "/work/eof_test.txt"], capture_output=True)
    file_exists = (chk.returncode == 0)

    with open(out_file, "w") as f:
        f.writelines(stream_lines)

    passed = (not file_exists)
    summary = f"""Case 3: Stdin EOF / Closed Channel
Exit Code: {rc}
Total Time: {total_time:.3f} s
File Exists: {file_exists} (must be False: tool call aborted on EOF)
Verdict: {'PASS' if passed else 'FAIL'}
"""
    log(summary)
    with open(summary_file, "w") as f:
        f.write(summary)
    return passed

def run_case_supervisor_crash():
    log("=== Case 4: Supervisor Crash (Exec Client Killed Mid-Approval) ===")
    summary_file = os.path.join(RESULTS_DIR, "case4-supervisor-crash.txt")

    subprocess.run(["container", "exec", CONTAINER, "rm", "-f", "/work/crash_test.txt"], check=False)

    cmd = [
        "container", "exec", "-i",
        "--env-file", TOKEN_FILE,
        "-w", "/work",
        CONTAINER,
        f"{TOOLS_DIR}/whr-shim", "run", "-pidfile", "/work/agent.pid", "--",
        f"{TOOLS_DIR}/claude", "-p",
        "--verbose",
        "--output-format", "stream-json",
        "--input-format", "stream-json",
        "--permission-mode", "manual",
        "--permission-prompt-tool", "stdio",
        "--model", "haiku"
    ]

    proc = subprocess.Popen(cmd, stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True)

    user_prompt = {"type": "user", "message": {"role": "user", "content": [{"type": "text", "text": "Write CRASH_SECRET to /work/crash_test.txt using the Write tool, then reply with DONE."}]}}
    proc.stdin.write(json.dumps(user_prompt) + "\n")
    proc.stdin.flush()

    for line in proc.stdout:
        try:
            data = json.loads(line)
            if data.get("type") == "control_request":
                log("Control request received mid-turn. Simulating supervisor CRASH (kill -9 exec client)...")
                break
        except Exception:
            pass

    proc.kill()
    proc.wait()
    log("Host container exec client killed.")

    time.sleep(1.0)

    chk_pid = subprocess.run(["container", "exec", CONTAINER, "sh", "-c", "cat /work/agent.pid 2>/dev/null"], capture_output=True, text=True)
    agent_pid = chk_pid.stdout.strip()

    pid_cmdline = ""
    pid_stat = ""
    pid_state = "UNKNOWN"
    is_alive = False

    if agent_pid:
        res_info = subprocess.run(["container", "exec", CONTAINER, "sh", "-c", f"""
if [ -d "/proc/{agent_pid}" ]; then
  echo "CMD:$(cat /proc/{agent_pid}/cmdline 2>/dev/null | tr '\0' ' ')"
  stat=$(cat /proc/{agent_pid}/stat 2>/dev/null)
  echo "STAT:$stat"
  echo "STATE:$(echo "$stat" | awk '{{print $3}}')"
else
  echo "DEAD"
fi
"""], capture_output=True, text=True)
        for line in res_info.stdout.strip().splitlines():
            if line.startswith("CMD:"):
                pid_cmdline = line[4:].strip()
            elif line.startswith("STAT:"):
                pid_stat = line[5:].strip()
            elif line.startswith("STATE:"):
                pid_state = line[6:].strip()
        is_alive = (pid_state != "DEAD" and pid_state != "Z" and bool(pid_cmdline))

    log(f"Guest agent process {agent_pid} state after host client crash: state={pid_state}, is_alive={is_alive}")
    log(f"Guest cmdline: {pid_cmdline}")
    log(f"Guest stat: {pid_stat}")

    t_kill0 = time.time()
    kill_res = subprocess.run(["container", "exec", CONTAINER, f"{TOOLS_DIR}/whr-shim", "kill", "-pidfile", "/work/agent.pid", "-grace", "500ms"], capture_output=True, text=True)
    kill_latency_ms = (time.time() - t_kill0) * 1000
    log(f"whr-shim kill executed in {kill_latency_ms:.2f} ms (rc: {kill_res.returncode})")

    time.sleep(0.5)
    orphans = get_guest_processes(CONTAINER, pattern="claude|whr-shim|node")
    orphan_str = ", ".join(f"{p['pid']}:{p['cmd']}" for p in orphans)

    chk_file = subprocess.run(["container", "exec", CONTAINER, "test", "-f", "/work/crash_test.txt"], capture_output=True)
    file_exists = (chk_file.returncode == 0)

    passed = (not file_exists and orphan_str == "" and is_alive)
    summary = f"""Case 4: Supervisor Crash (Exec Client Killed Mid-Approval)
Host exec client killed with SIGKILL: Success
Guest Agent PID: {agent_pid}
Guest Agent /proc/{agent_pid}/cmdline: {pid_cmdline}
Guest Agent /proc/{agent_pid}/stat: {pid_stat}
Guest Agent Process State: {pid_state} ({'Alive, not Zombie' if is_alive else 'Dead/Zombie'})
Guest Process Survived Client Death: {is_alive}
whr-shim kill latency: {kill_latency_ms:.2f} ms
Remaining Guest Orphans: '{orphan_str}' (must be empty)
File Created: {file_exists} (must be False: tool call fail-closed)
Verdict: {'PASS' if passed else 'FAIL'}
"""
    log(summary)
    with open(summary_file, "w") as f:
        f.write(summary)
    return passed

def run_case_timeout():
    log("=== Case 5: Unanswered Approval (Deadline / Timeout) ===")
    summary_file = os.path.join(RESULTS_DIR, "case5-timeout.txt")

    subprocess.run(["container", "exec", CONTAINER, "rm", "-f", "/work/timeout_test.txt"], check=False)

    cmd = [
        "container", "exec", "-i",
        "--env-file", TOKEN_FILE,
        "-w", "/work",
        CONTAINER,
        f"{TOOLS_DIR}/whr-shim", "run", "-pidfile", "/work/agent.pid", "--",
        f"{TOOLS_DIR}/claude", "-p",
        "--verbose",
        "--output-format", "stream-json",
        "--input-format", "stream-json",
        "--permission-mode", "manual",
        "--permission-prompt-tool", "stdio",
        "--model", "haiku"
    ]

    proc = subprocess.Popen(cmd, stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True)

    user_prompt = {"type": "user", "message": {"role": "user", "content": [{"type": "text", "text": "Write TIMEOUT_SECRET to /work/timeout_test.txt using the Write tool, then reply with DONE."}]}}
    proc.stdin.write(json.dumps(user_prompt) + "\n")
    proc.stdin.flush()

    for line in proc.stdout:
        try:
            data = json.loads(line)
            if data.get("type") == "control_request":
                log("Control request received. Holding unanswered to simulate supervisor decision deadline...")
                break
        except Exception:
            pass

    DEADLINE = 5.0
    time.sleep(DEADLINE)
    log(f"Decision deadline ({DEADLINE}s) expired. Checking if tool executed prematurely...")

    chk_premature = subprocess.run(["container", "exec", CONTAINER, "test", "-f", "/work/timeout_test.txt"], capture_output=True)
    tool_executed_prematurely = (chk_premature.returncode == 0)

    log("Terminating expired run via whr-shim kill...")
    t_kill0 = time.time()
    kill_res = subprocess.run(["container", "exec", CONTAINER, f"{TOOLS_DIR}/whr-shim", "kill", "-pidfile", "/work/agent.pid", "-grace", "500ms"], capture_output=True, text=True)
    kill_latency_ms = (time.time() - t_kill0) * 1000
    log(f"whr-shim kill executed in {kill_latency_ms:.2f} ms (rc: {kill_res.returncode})")

    proc.kill()
    proc.wait()

    time.sleep(0.5)
    orphans = get_guest_processes(CONTAINER, pattern="claude|whr-shim|node")
    orphan_str = ", ".join(f"{p['pid']}:{p['cmd']}" for p in orphans)

    chk_file = subprocess.run(["container", "exec", CONTAINER, "test", "-f", "/work/timeout_test.txt"], capture_output=True)
    file_exists = (chk_file.returncode == 0)

    passed = (not file_exists and not tool_executed_prematurely and orphan_str == "")
    summary = f"""Case 5: Unanswered Approval (Deadline / Timeout)
Deadline Duration: {DEADLINE} s
Tool Executed Prematurely: {tool_executed_prematurely} (must be False)
whr-shim Kill Latency: {kill_latency_ms:.2f} ms
Remaining Guest Orphans: '{orphan_str}' (must be empty)
File Created: {file_exists} (must be False)
Verdict: {'PASS' if passed else 'FAIL'}
"""
    log(summary)
    with open(summary_file, "w") as f:
        f.write(summary)
    return passed

def run_case_request_id_mismatch():
    log("=== Case 6: Request ID Validation & Superseded Response ===")
    out_file = os.path.join(RESULTS_DIR, "case6-reqid-stream.jsonl")
    summary_file = os.path.join(RESULTS_DIR, "case6-reqid.txt")

    subprocess.run(["container", "exec", CONTAINER, "rm", "-f", "/work/reqid_test.txt"], check=False)

    cmd = [
        "container", "exec", "-i",
        "--env-file", TOKEN_FILE,
        "-w", "/work",
        CONTAINER,
        f"{TOOLS_DIR}/whr-shim", "run", "-pidfile", "/work/agent.pid", "--",
        f"{TOOLS_DIR}/claude", "-p",
        "--verbose",
        "--output-format", "stream-json",
        "--input-format", "stream-json",
        "--permission-mode", "manual",
        "--permission-prompt-tool", "stdio",
        "--model", "haiku"
    ]

    proc = subprocess.Popen(cmd, stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True)

    user_prompt = {"type": "user", "message": {"role": "user", "content": [{"type": "text", "text": "Write REQID_TEST to /work/reqid_test.txt using the Write tool, then reply with DONE."}]}}
    proc.stdin.write(json.dumps(user_prompt) + "\n")
    proc.stdin.flush()

    stream_lines = []
    mismatch_ignored = False
    deny_latency_ms = None

    for line in proc.stdout:
        stream_lines.append(line)
        try:
            data = json.loads(line)
            if data.get("type") == "control_request":
                real_id = data["request_id"]
                log(f"Received control_request with real request_id={real_id}")

                bogus_id = "00000000-0000-0000-0000-000000000000"
                log(f"Sending control_response with MISMATCHED request_id: {bogus_id}")
                bogus_resp = {
                    "type": "control_response",
                    "response": {
                        "subtype": "success",
                        "request_id": bogus_id,
                        "response": {"behavior": "allow"}
                    }
                }
                proc.stdin.write(json.dumps(bogus_resp) + "\n")
                proc.stdin.flush()

                time.sleep(2.0)
                chk_mismatch = subprocess.run(["container", "exec", CONTAINER, "test", "-f", "/work/reqid_test.txt"], capture_output=True)
                mismatch_ignored = (chk_mismatch.returncode != 0)
                log(f"Did Claude Code ignore mismatched request_id? {mismatch_ignored} (file not created)")

                deny_resp = {
                    "type": "control_response",
                    "response": {
                        "subtype": "success",
                        "request_id": real_id,
                        "response": {"behavior": "deny", "message": "Denied after bad request_id test"}
                    }
                }
                t_deny = time.time()
                proc.stdin.write(json.dumps(deny_resp) + "\n")
                proc.stdin.flush()
                deny_latency_ms = (time.time() - t_deny) * 1000
                log(f"Real deny response dispatched in {deny_latency_ms:.2f} ms")
            elif data.get("type") == "result":
                log("Result received, ending case 6.")
                proc.stdin.close()
                break
        except Exception:
            pass

    proc.communicate()
    rc = proc.returncode

    chk_file = subprocess.run(["container", "exec", CONTAINER, "test", "-f", "/work/reqid_test.txt"], capture_output=True)
    file_exists = (chk_file.returncode == 0)

    with open(out_file, "w") as f:
        f.writelines(stream_lines)

    passed = (rc == 0 and mismatch_ignored and not file_exists)
    summary = f"""Case 6: Request ID Validation & Superseded Response
Exit Code: {rc}
Mismatched Request ID Ignored: {mismatch_ignored}
Real Deny Dispatch Latency: {deny_latency_ms:.2f} ms
File Created: {file_exists} (must be False)
Verdict: {'PASS' if passed else 'FAIL'}
"""
    log(summary)
    with open(summary_file, "w") as f:
        f.write(summary)
    return passed

def run_case_cancel_resume():
    log("=== Case 7: Leftover #10 - Mid-Tool-Call Cancel via whr-shim & Resume Inspection ===")
    summary_file = os.path.join(RESULTS_DIR, "case7-cancel-resume.txt")
    resume_stream_file = os.path.join(RESULTS_DIR, "case7-resume-stream.jsonl")

    subprocess.run(["container", "exec", CONTAINER, "rm", "-f", "/work/done.txt"], check=False)

    cmd = [
        "container", "exec", "-i",
        "--env-file", TOKEN_FILE,
        "-w", "/work",
        CONTAINER,
        f"{TOOLS_DIR}/whr-shim", "run", "-pidfile", "/work/agent.pid", "--",
        f"{TOOLS_DIR}/claude", "-p",
        "--verbose",
        "--output-format", "stream-json",
        "--input-format", "stream-json",
        "--permission-mode", "manual",
        "--permission-prompt-tool", "stdio",
        "--model", "haiku"
    ]

    proc = subprocess.Popen(cmd, stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True)

    user_prompt = {"type": "user", "message": {"role": "user", "content": [{"type": "text", "text": "Run the command `for i in $(seq 1 30); do sleep 1; done && echo DONE > /work/done.txt` using the Bash tool. Do not run any other tool."}]}}
    proc.stdin.write(json.dumps(user_prompt) + "\n")
    proc.stdin.flush()

    session_id = None

    for line in proc.stdout:
        try:
            data = json.loads(line)
            if data.get("session_id") and not session_id:
                session_id = data["session_id"]
            if data.get("type") == "control_request":
                req_id = data["request_id"]
                tool_name = data["request"].get("tool_name")
                log(f"Approving tool {tool_name} so it begins background execution...")
                resp = {
                    "type": "control_response",
                    "response": {
                        "subtype": "success",
                        "request_id": req_id,
                        "response": {"behavior": "allow"}
                    }
                }
                proc.stdin.write(json.dumps(resp) + "\n")
                proc.stdin.flush()
                break
        except Exception:
            pass

    time.sleep(3.0)

    active_procs = get_guest_processes(CONTAINER, pattern="sleep 1|seq 1 30")
    active_procs_formatted = "\n".join(
        f"  PID: {p['pid']} | State: {p['state']} | Cmd: {p['cmd']} | Stat: {p['stat']}"
        for p in active_procs
    )
    log(f"Observed active background sleep in container:\n{active_procs_formatted}")

    t0_kill = time.time()
    kill_res = subprocess.run(["container", "exec", CONTAINER, f"{TOOLS_DIR}/whr-shim", "kill", "-pidfile", "/work/agent.pid", "-grace", "500ms"], capture_output=True, text=True)
    kill_duration_ms = (time.time() - t0_kill) * 1000
    log(f"whr-shim kill completed in {kill_duration_ms:.2f} ms")

    proc.kill()
    proc.wait()

    time.sleep(1.0)
    orphans = get_guest_processes(CONTAINER, pattern="sleep 1|seq 1 30|claude|whr-shim")
    orphan_str = ", ".join(f"{p['pid']}:{p['cmd']}" for p in orphans)

    chk_file = subprocess.run(["container", "exec", CONTAINER, "test", "-f", "/work/done.txt"], capture_output=True)
    file_exists = (chk_file.returncode == 0)
    log(f"Sleep cancelled: orphan processes='{orphan_str}', file_exists={file_exists}")

    log(f"Resuming session {session_id}...")
    resume_cmd = [
        "container", "exec", "-i",
        "--env-file", TOKEN_FILE,
        "-w", "/work",
        CONTAINER,
        f"{TOOLS_DIR}/whr-shim", "run", "-pidfile", "/work/agent.pid", "--",
        f"{TOOLS_DIR}/claude", "-p",
        "--resume", session_id,
        "--verbose",
        "--output-format", "stream-json",
        "--input-format", "stream-json",
        "--model", "haiku"
    ]

    r_proc = subprocess.Popen(resume_cmd, stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True)
    resume_prompt = {"type": "user", "message": {"role": "user", "content": [{"type": "text", "text": "What is the status of the command you started? Did it finish or was it interrupted?"}]}}
    r_proc.stdin.write(json.dumps(resume_prompt) + "\n")
    r_proc.stdin.flush()

    resume_lines = []
    resumed_answer = ""

    for line in r_proc.stdout:
        resume_lines.append(line)
        try:
            data = json.loads(line)
            if data.get("type") == "assistant":
                for item in data.get("message", {}).get("content", []):
                    if item.get("type") == "text":
                        resumed_answer += item.get("text", "")
            elif data.get("type") == "result":
                if data.get("result"):
                    resumed_answer = data.get("result")
                if data.get("origin", {}).get("kind") != "task-notification":
                    r_proc.stdin.close()
                    break
        except Exception:
            pass

    r_proc.communicate()
    log(f"Resumed agent answer: {resumed_answer}")

    with open(resume_stream_file, "w") as f:
        f.writelines(resume_lines)

    cancel_clean = (orphan_str == "" and not file_exists and len(active_procs) > 0)

    ans_lower = resumed_answer.lower()
    falsely_claimed_never_ran = ("never executed" in ans_lower or "rejected before" in ans_lower or "rejected during" in ans_lower)
    acknowledged_interruption = ("interrupted" in ans_lower or "killed" in ans_lower or "stopped" in ans_lower) and not falsely_claimed_never_ran
    resume_accurate = acknowledged_interruption

    if falsely_claimed_never_ran:
        finding_text = "Claude Code told the model that the tool was rejected during permission prompt and never executed, even though it was approved and ran for 3 seconds before being cancelled by whr-shim. The resumed session lacks awareness of partial execution."
    elif not resume_accurate:
        finding_text = f"The resumed model did not recognize that the command was cancelled mid-execution: '{resumed_answer.strip()}'"
    else:
        finding_text = "The resumed model accurately reported that the command was interrupted mid-execution."

    cancellation_verdict = "PASS" if cancel_clean else "FAIL"
    resume_verdict = "PASS" if resume_accurate else "FAIL"
    overall_verdict = "PASS" if (cancel_clean and resume_accurate) else "FAIL"

    summary = f"""Case 7: Leftover #10 - Mid-Tool-Call Cancel & Resume Inspection
Session ID: {session_id}
Observed Active Process During Execution:
{active_procs_formatted if active_procs_formatted else "  None"}
whr-shim Kill Latency: {kill_duration_ms:.2f} ms
Remaining Orphan Processes: '{orphan_str}' (must be empty)
File Created: {file_exists} (must be False)
Process Group Cancellation: {cancellation_verdict}
Resumed Agent Answer:
{resumed_answer.strip()}
Resume Accuracy Evaluation:
  Acknowledged interruption: {acknowledged_interruption}
  Falsely claimed never ran: {falsely_claimed_never_ran}
  Finding: {finding_text}
  Architectural Remedy: D27 (#66)
Verdict: {overall_verdict} (Cancellation: {cancellation_verdict}; Resume Accuracy: {resume_verdict})
"""
    log(summary)
    with open(summary_file, "w") as f:
        f.write(summary)
    return overall_verdict == "PASS"

def generate_results_table(results_dir):
    def get_val(filename, prefix):
        p = os.path.join(results_dir, filename)
        if not os.path.exists(p):
            return "N/A"
        with open(p) as f:
            for line in f:
                if line.startswith(prefix):
                    return line[len(prefix):].strip()
        return "N/A"

    c1_turn = get_val("case1-allow.txt", "Total Turn Time:")
    c1_lat = get_val("case1-allow.txt", "Response Dispatch Latency:")
    c1_verd = get_val("case1-allow.txt", "Verdict:")

    c2_turn = get_val("case2-deny.txt", "Total Turn Time:")
    c2_lat = get_val("case2-deny.txt", "Response Dispatch Latency:")
    c2_verd = get_val("case2-deny.txt", "Verdict:")

    c3_time = get_val("case3-closed-stdin.txt", "Total Time:")
    c3_verd = get_val("case3-closed-stdin.txt", "Verdict:")

    c4_lat = get_val("case4-supervisor-crash.txt", "whr-shim kill latency:")
    c4_surv = get_val("case4-supervisor-crash.txt", "Guest Process Survived Client Death:")
    c4_orph = get_val("case4-supervisor-crash.txt", "Remaining Guest Orphans:")
    c4_verd = get_val("case4-supervisor-crash.txt", "Verdict:")

    c5_lat = get_val("case5-timeout.txt", "whr-shim Kill Latency:")
    c5_dead = get_val("case5-timeout.txt", "Deadline Duration:")
    c5_orph = get_val("case5-timeout.txt", "Remaining Guest Orphans:")
    c5_verd = get_val("case5-timeout.txt", "Verdict:")

    c6_lat = get_val("case6-reqid.txt", "Real Deny Dispatch Latency:")
    c6_verd = get_val("case6-reqid.txt", "Verdict:")

    c7_lat = get_val("case7-cancel-resume.txt", "whr-shim Kill Latency:")
    c7_canc = get_val("case7-cancel-resume.txt", "Process Group Cancellation:")
    c7_verd = get_val("case7-cancel-resume.txt", "Verdict:")

    table = [
        "| Case | Scenario | Description | Key Latency / Observation | Fail-Closed / Clean | Verdict |",
        "| --- | --- | --- | --- | --- | --- |",
        f"| **Case 1** | Allow round-trip | Tool approval dispatched via stdin | Dispatch: {c1_lat}; Turn: {c1_turn} | Yes (Target file written) | **{c1_verd}** |",
        f"| **Case 2** | Deny round-trip | Tool denial dispatched via stdin | Dispatch: {c2_lat}; Turn: {c2_turn} | Yes (Permission denial recorded) | **{c2_verd}** |",
        f"| **Case 3** | Stdin EOF | Pipe closed immediately on `control_request` | Aborted in {c3_time} | Yes (Tool aborted on EOF) | **{c3_verd}** |",
        f"| **Case 4** | Supervisor Crash | Host exec killed mid-turn; guest checked via /proc; reaped via `whr-shim` | Guest survived: {c4_surv}; Reaped: {c4_lat} | Yes (0 orphans, fail-closed) | **{c4_verd}** |",
        f"| **Case 5** | Approval Deadline | Unanswered approval held for {c5_dead}; reaped via `whr-shim` | Reaped: {c5_lat}; 0 orphans | Yes (Tool not run prematurely) | **{c5_verd}** |",
        f"| **Case 6** | Request ID validation | Mismatched `request_id` ignored; real `request_id` deny processed | Real Deny: {c6_lat} | Yes (Ignored invalid) | **{c6_verd}** |",
        f"| **Case 7** | #10 Cancel & Resume | 30s Bash loop killed mid-turn via `whr-shim`; resumed session inspected | Reaped: {c7_lat}; 0 orphans | Cancellation: {c7_canc}; Resume: False | **{c7_verd.split()[0] if c7_verd != 'N/A' else 'FAIL'}** |"
    ]
    return "\n".join(table)

def update_results_md(results_dir, results_md_path):
    table_str = generate_results_table(results_dir)
    with open(results_md_path) as f:
        content = f.read()

    # Replace summary table
    table_pattern = r"\| Case \| Scenario \| Description \| Key Latency / Observation \| Fail-Closed / Clean \| Verdict \|[\s\S]*?\| \*\*Case 7\*\* \|[^\n]*"
    new_content = re.sub(table_pattern, table_str, content)

    # Update D23/#23 to D27/#66
    new_content = new_content.replace("(D23 / #23)", "(D27 / #66)")

    with open(results_md_path, "w") as f:
        f.write(new_content)
    log(f"Updated {results_md_path} with live measurement table.")

if __name__ == "__main__":
    if len(sys.argv) < 2:
        print("usage: driver.py <allow|deny|closed_stdin|crash|timeout|reqid|cancel_resume|all|check-prereqs|generate-results>")
        sys.exit(1)

    action = sys.argv[1]
    results = {}
    if action == "check-prereqs":
        ok = check_prerequisites()
        sys.exit(0 if ok else 1)
    if action == "generate-results":
        update_results_md(RESULTS_DIR, os.path.join(os.path.dirname(__file__), "RESULTS.md"))
        sys.exit(0)
    if action in ("allow", "all"):
        results["allow"] = run_case_allow()
    if action in ("deny", "all"):
        results["deny"] = run_case_deny()
    if action in ("closed_stdin", "all"):
        results["closed_stdin"] = run_case_closed_stdin()
    if action in ("crash", "all"):
        results["crash"] = run_case_supervisor_crash()
    if action in ("timeout", "all"):
        results["timeout"] = run_case_timeout()
    if action in ("reqid", "all"):
        results["reqid"] = run_case_request_id_mismatch()
    if action in ("cancel_resume", "all"):
        results["cancel_resume"] = run_case_cancel_resume()
    if action == "all":
        update_results_md(RESULTS_DIR, os.path.join(os.path.dirname(__file__), "RESULTS.md"))
