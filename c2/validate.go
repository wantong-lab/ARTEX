package c2

// 监听地址安全约束：C2 监听器默认只允许绑定回环地址。公网/内网可达的
// 0.0.0.0、:: 或具体非回环 IP 会扩大暴露面，必须由运维显式打开
// c2_allow_non_loopback 设置后才允许（该变化会写入 CHANGELOG 与部署文档）。
// 本函数是纯函数，便于 server 层在「创建监听器」和「启动监听器」两处复用。

import (
	"fmt"
	"net"
	"strings"
)

// ValidateBindHost 校验 C2 监听地址：
//   - 空地址由调用方补默认值（127.0.0.1），这里也按回环处理；
//   - 回环（127.0.0.0/8、::1、localhost）始终允许；
//   - 非回环仅在 allowNonLoopback=true 时允许，否则返回带设置名的错误。
func ValidateBindHost(host string, allowNonLoopback bool) error {
	h := strings.TrimSpace(host)
	if h == "" || strings.EqualFold(h, "localhost") {
		return nil
	}
	if ip := net.ParseIP(h); ip != nil && ip.IsLoopback() {
		return nil
	}
	if allowNonLoopback {
		return nil
	}
	return fmt.Errorf("C2 监听地址 %q 不是回环地址：默认只允许 127.0.0.1/::1 绑定；"+
		"确需对外监听请先在设置中显式开启 c2_allow_non_loopback", h)
}
