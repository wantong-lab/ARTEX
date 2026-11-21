package agent

// 工具纪律软提醒（M1-05）：不打断执行，只在 Bash 命令前钉一行 stderr 提示。
// 两类：侦察阶段别只用 curl 手敲（应用批量工具）；手写 webshell 时提醒标准模板。
// 噪音控制是关键：给出上限、用过批量工具后侦察提醒永久沉默。

import (
	"encoding/json"
	"regexp"
	"strings"
)

type NudgeConfig struct {
	ReconEnabled   bool
	ArsenalEnabled bool
	ReconFirstAt   int // 同一 run 内 curl/wget 累计达到该值首次提醒
	ReconInterval  int // 之后每再累计 N 次提醒一次
	ReconMax       int // 侦察提醒上限
	ArsenalMax     int // 军火库提醒上限
}

func DefaultNudgeConfig() NudgeConfig {
	return NudgeConfig{ReconEnabled: true, ArsenalEnabled: true, ReconFirstAt: 10, ReconInterval: 12, ReconMax: 2, ArsenalMax: 2}
}

// NudgeConfigFromSettings 读 recon_nudge / arsenal_nudge 开关；缺失或无法识别保持默认开。
func NudgeConfigFromSettings(get func(string) (string, bool, error)) NudgeConfig {
	cfg := DefaultNudgeConfig()
	if get == nil {
		return cfg
	}
	if v, ok, err := get("recon_nudge"); err == nil && ok {
		if on, known := parseDetectorSwitch(v); known {
			cfg.ReconEnabled = on
		}
	}
	if v, ok, err := get("arsenal_nudge"); err == nil && ok {
		if on, known := parseDetectorSwitch(v); known {
			cfg.ArsenalEnabled = on
		}
	}
	return cfg
}

var (
	nudgeCurlRe    = regexp.MustCompile(`\b(curl|wget)\b`)
	nudgeBatchRe   = regexp.MustCompile(`\b(httpx|ffuf|feroxbuster|dirsearch|gobuster|nuclei|subfinder|dnsx|gau|naabu|katana|waybackurls|nmap|masscan|whatweb|wafw00f|nikto|arjun|fscan)\b`)
	nudgeWsWriteRe = regexp.MustCompile(`(?i)(cat\s*>\s*|tee\s+|echo\s*.{0,80}>\s*|printf\s*.{0,80}>\s*)[^\s;|&]*\.(php|jsp|aspx|asp)\b`)
	nudgeWsBodyRe  = regexp.MustCompile(`(?i)(Runtime\.getRuntime|ProcessBuilder|eval\s*\(|assert\s*\(|@\s*system|passthru|shell_exec|\$_REQUEST|\$_POST|\$_GET|Response\.Write|CreateObject)`)
)

const (
	reconNudgeText   = "[提示] 侦察阶段别只用 curl/wget 手敲单点：成规模的探测请用 httpx/ffuf/nuclei 等批量工具，结果更全、更快、可复核。"
	arsenalNudgeText = "[提示] 正在手写 webshell：平台有标准加密马/模板可复用，手搓明文马过 WAF 率低且难维护。"
)

// NudgeState 是单条意图内的提醒状态（非并发安全，钩子串行调用）。
type NudgeState struct {
	cfg NudgeConfig

	curlCount  int
	nextAt     int
	reconFired int

	arsenalFired int
	batchSeen    bool
}

func NewNudgeState(cfg NudgeConfig) *NudgeState { return &NudgeState{cfg: cfg} }

// OnBash 观察一条 Bash 命令，返回需要前置到 stderr 的提示（可为空）。
func (n *NudgeState) OnBash(command string) string {
	if n == nil || strings.TrimSpace(command) == "" {
		return ""
	}
	if nudgeBatchRe.MatchString(command) {
		n.batchSeen = true
	}
	var out []string
	if n.cfg.ReconEnabled && !n.batchSeen {
		n.curlCount += len(nudgeCurlRe.FindAllString(command, -1))
		first := n.cfg.ReconFirstAt
		if first <= 0 {
			first = 10
		}
		if n.reconFired < n.cfg.ReconMax && n.curlCount >= first {
			if n.reconFired == 0 || n.curlCount >= n.nextAt {
				n.reconFired++
				interval := n.cfg.ReconInterval
				if interval <= 0 {
					interval = 12
				}
				n.nextAt = n.curlCount + interval
				out = append(out, reconNudgeText)
			}
		}
	}
	if n.cfg.ArsenalEnabled && n.arsenalFired < n.cfg.ArsenalMax && nudgeWsWriteRe.MatchString(command) && nudgeWsBodyRe.MatchString(command) {
		n.arsenalFired++
		out = append(out, arsenalNudgeText)
	}
	return strings.Join(out, " ")
}

// PrependBashStderr 把一行提示前置到 Bash 命令的 stderr（不改命令语义）。
// 返回修改后的工具输入与是否改动；tool 输入不是合法 JSON 或无 command 时不动。
func PrependBashStderr(input []byte, nudge string) ([]byte, bool) {
	var payload map[string]any
	if err := json.Unmarshal(input, &payload); err != nil {
		return nil, false
	}
	cmd, _ := payload["command"].(string)
	if strings.TrimSpace(cmd) == "" {
		return nil, false
	}
	// 提示文本来自代码常量，仍去掉单引号以防 shell 转义问题。
	safe := strings.ReplaceAll(nudge, "'", "")
	payload["command"] = "echo '" + safe + "' >&2\n" + cmd
	out, err := json.Marshal(payload)
	if err != nil {
		return nil, false
	}
	return out, true
}
