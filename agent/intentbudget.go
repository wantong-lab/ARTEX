package agent

// 意图预算分级（M5-01）：编排层按意图类别给 worker 五档预算，不依赖 planner 声明。
// 分类结果同时决定注入 worker system 的纪律提示，把「本阶段该快收」变成编排层
// 下发的明确预期，避免模型在死面上烧穿预算。

import (
	"encoding/json"
	"regexp"
	"strings"
)

const (
	IntentBudgetRecon   = 900
	IntentBudgetEnum    = 1800
	IntentBudgetBrute   = 2400
	IntentBudgetExploit = 2400
	IntentBudgetGeneral = 1800
)

// IntentBudget 是一次意图预算分类结果。
type IntentBudget struct {
	Class   string
	Seconds int
	Note    string
}

var (
	reBudgetBrute   = regexp.MustCompile("(?i)爆破|口令|弱口令|字典|撞库|brute|crack|hydra")
	reBudgetExploit = regexp.MustCompile("(?i)利用|漏洞验证|getshell|提权|绕过|rce|sql注入|命令执行|反序列化|文件上传|ssrf|xxe|exploit")
	reBudgetEnum    = regexp.MustCompile("(?i)枚举|扫描|目录|子域|端口|指纹|fuzz|scan|enum|ffuf|dirsearch|nmap")
	reBudgetRecon   = regexp.MustCompile("(?i)侦察|探测|存活|可达|信息收集|recon|probe|whoami")
)

// ClassifyIntentBudget 从意图 payload 分类（chain_tags 优先，summary/text 关键词回退）。
func ClassifyIntentBudget(payload []byte) IntentBudget {
	var p struct {
		Summary   string   `json:"summary"`
		Text      string   `json:"text"`
		ChainTags []string `json:"chain_tags"`
	}
	_ = json.Unmarshal(payload, &p)
	for _, tag := range p.ChainTags {
		switch strings.ToLower(strings.TrimSpace(tag)) {
		case "brute", "exploit", "enum", "recon":
			return budgetOf(strings.ToLower(strings.TrimSpace(tag)))
		}
	}
	s := strings.TrimSpace(p.Summary)
	if s == "" {
		s = strings.TrimSpace(p.Text)
	}
	switch {
	case s == "":
		return budgetOf("general")
	case reBudgetBrute.MatchString(s):
		return budgetOf("brute")
	case reBudgetExploit.MatchString(s):
		return budgetOf("exploit")
	case reBudgetEnum.MatchString(s):
		return budgetOf("enum")
	case reBudgetRecon.MatchString(s):
		return budgetOf("recon")
	}
	return budgetOf("general")
}

func budgetOf(class string) IntentBudget {
	switch class {
	case "recon":
		return IntentBudget{Class: class, Seconds: IntentBudgetRecon, Note: budgetNoteRecon}
	case "enum":
		return IntentBudget{Class: class, Seconds: IntentBudgetEnum, Note: budgetNoteEnum}
	case "brute":
		return IntentBudget{Class: class, Seconds: IntentBudgetBrute, Note: budgetNoteBrute}
	case "exploit":
		return IntentBudget{Class: class, Seconds: IntentBudgetExploit, Note: budgetNoteExploit}
	default:
		return IntentBudget{Class: "general", Seconds: IntentBudgetGeneral}
	}
}

const (
	budgetNoteRecon   = "【编排预算·侦察类】本意图被归类为侦察类，预算较短（约 15 分钟）——拿到存活/指纹/可达性结论立刻写回返回，不要顺手扩展成枚举或利用（那是下一条意图的事）。烧满预算通常意味着在死面空转。"
	budgetNoteEnum    = "【编排预算·枚举类】本意图被归类为枚举类（约 30 分钟）——输出量大，结果先压缩要点再写回；预算耗尽前把已覆盖范围（端口段/字典偏移/路径进度）写进 fact 供续派。"
	budgetNoteBrute   = "【编排预算·口令类】本意图被归类为爆破/口令类（约 40 分钟）——先用小字典确认认证 oracle 有效、无锁定无禁封，再放大规模；进度随时写进 fact（字典偏移/已试范围），断点靠 fact 续传。"
	budgetNoteExploit = "【编排预算·利用类】本意图被归类为利用/突破类（约 40 分钟）——聚焦拿到可复现证据：利用链打通后立即 report_finding / register_session 写回，不要顺手做完整提权或横向闭环。"
)
