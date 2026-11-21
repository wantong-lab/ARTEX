# LLM 配置指南

本文覆盖 ARTEX 的 LLM 配置方式、支持的协议、失败转移/重试、用量记录与排障。  
适合场景：首次接入模型、接入聚合网关（opencode-go / OneAPI 等）、切换流式/非流式、排查 401/403/断流。



## 1. 配置入口

| 入口 | 位置 | 说明 |
|---|---|---|
| UI | 系统设置 → LLM 配置 | 推荐；支持多 profile、激活、测试连接、重试策略 |
| API | /api/llm/profiles 系列 | 见第 4 节 |
| 环境变量 | 启动进程 | 单配置场景；UI 未配置时生效 |

环境变量（agent/provider.go）：

```bash
ARTEX_LLM_PROVIDER=anthropic|openai   # 不填则按 key 推断
ARTEX_LLM_MODEL=model-id              # 不填用该格式默认模型
ARTEX_LLM_BASE_URL=https://.../v1     # 自定义端点（聚合网关/本地模型）
ARTEX_LLM_PROXY=http://127.0.0.1:7890 # LLM 出站代理（http/https/socks5）
ARTEX_LLM_STREAM=false                # false/0/off 关闭流式
ANTHROPIC_API_KEY=sk-ant-...
OPENAI_API_KEY=sk-...
```

注意：环境变量是**单配置**；一旦在 UI/DB 配置了激活 profile，运行时以 DB 为准。生产建议统一用 profile（可失败转移、可测试、可记录用量）。

## 2. 支持的协议格式

| format | 端点风格 | 典型供应商 |
|---|---|---|
| `openai` | POST /chat/completions（SSE 或非流式） | OpenAI、DeepSeek、opencode-go zen、OneAPI、Ollama、vLLM |
| `openai-responses` | POST /responses | OpenAI Responses API |
| `anthropic` | POST /v1/messages | Anthropic Claude 及兼容网关 |

**格式必须与端点匹配**。最常见错误：把 `https://api.anthropic.com` 配成 `openai`，或把 OpenAI 兼容网关配成 `anthropic`，会得到 400/404 或“测试通过但任务报错”。测试连接会按 profile 的 format 实际发一次请求，务必以其结果为准。

## 3. Profile 字段

| 字段 | JSON 名 | 说明 |
|---|---|---|
| 名称 | name | 唯一可读名 |
| 格式 | format | 见第 2 节 |
| Base URL | base_url | 聚合网关/自建端点；留空用官方地址 |
| 模型 | model | 端点支持的模型 id |
| API Key | api_key | 仅写入；读取接口只回 `api_key_hint` |
| 代理 | proxy | LLM 出站代理；空=按环境变量 |
| 流式 | streaming | 默认 true；false=真实非流式（见第 6 节） |
| 上下文窗口 | context_window_k | K tokens，用于上下文压缩阈值；0=默认 200K，上限 1000 |
| 输出上限 | max_tokens | 单次回复上限；0=不发送 |
| 输出字段名 | max_tokens_field | 仅 openai：""=max_tokens，`max_completion_tokens`=新版字段 |
| 思考开关 | thinking_type | ""=不发送，`disabled` / `enabled` |
| 思考强度 | reasoning_effort | ""=不发送；low/medium/high/xhigh/max |
| 会话头 | session_header_key | 自定义请求头名，值为当前 run 的 session id（网关缓存/粘性路由用） |
| 优先级 | priority | 失败转移链排序，越大越靠前（激活 profile 永远链首） |
| 不参与轮询 | pool_exclude | true=只允许被任务显式绑定，不作为兜底 |
| 重试覆盖 | retry | 覆盖 connect/empty/stream 三层（见第 7 节） |

## 4. 常用 API

```bash
# 列出/创建/更新（带 id 即更新）
GET  /api/llm/profiles
POST /api/llm/profiles

# 激活（全局默认）
POST /api/llm/profiles/active   {"id": 1}

# 测试连接：带上完整配置字段（仅传 profile_id 不够——provider/model/base_url 从请求体取）
POST /api/llm/test
{"provider":"openai","model":"hy4-flash-free",
 "base_url":"https://opencode.ai/zen/go/v1",
 "session_header_key":"x-opencode-session","profile_id":1,"streaming":false}

# 全局重试策略
GET  /api/llm/retry-policy
POST /api/llm/retry-policy
```

任务级链路：创建任务时 `llm_profile_ids:[1,2]`（有序）；第一项初始生效，失败时按优先级在链内切换。任务详情接口返回 `llm_failover_state` / `llm_failover_reason`，`GET /api/tasks/{id}/llm/resolution` 可查看当前解析结果。

## 5. 快速配方

### 5.1 opencode-go zen（实战验证：hy4-flash-free）

```json
{
  "name": "opencode-go-hy4-flash-free",
  "format": "openai",
  "base_url": "https://opencode.ai/zen/go/v1",
  "model": "hy4-flash-free",
  "api_key": "<你的 key>",
  "session_header_key": "x-opencode-session",
  "streaming": false,
  "context_window_k": 128,
  "max_tokens": 4096,
  "priority": 10
}
```

要点：
- 必须带 `session_header_key=x-opencode-session`，否则部分端点直接 403 “Request not allowed”；
- 该网关 SSE 在部分网络下会断流/挂起，`streaming=false` 是最稳的实战配置；
- `base_url` 不要带 `/chat/completions`，填到 `/v1` 即可。

### 5.2 OpenAI

```json
{"name":"openai-gpt","format":"openai","model":"gpt-4o-mini","api_key":"sk-...","streaming":true}
```

### 5.3 Anthropic

```json
{"name":"claude","format":"anthropic","model":"claude-sonnet-4-5","api_key":"sk-ant-...","streaming":true}
```

### 5.4 DeepSeek / OneAPI / 本地 OpenAI 兼容

```json
{"name":"deepseek","format":"openai","base_url":"https://api.deepseek.com/v1","model":"deepseek-chat","api_key":"sk-..."}
```

```json
{"name":"ollama-local","format":"openai","base_url":"http://127.0.0.1:11434/v1","model":"qwen2.5:14b","api_key":"ollama"}
```

### 5.5 需要 `max_completion_tokens` 的新版 OpenAI 推理模型

```json
{"name":"o-series","format":"openai","model":"o4-mini","api_key":"sk-...",
 "max_tokens":8192,"max_tokens_field":"max_completion_tokens"}
```

## 6. 流式 vs 非流式

- `streaming=true`（默认）：SSE，界面实时输出思考/正文，适合稳定网络与官方端点。
- `streaming=false`：单次 JSON 响应，规避网关 SSE 断流、重复包、压缩/代理缓冲问题；缺点是运行中看不到增量输出，只能等整轮结束。
- 切换只影响该 profile 之后的调用；任务运行中保存 profile 会重建该任务的 provider。
- 如果出现“请求发出后长时间无输出”“只收到一半就中断”“测试通过但任务卡住”，先把该 profile 改为非流式复测。

## 7. 失败转移、轮询与重试

### 7.1 失败转移（failover）

- 激活 profile 永远在链首；其余按 `priority` 降序，`pool_exclude=true` 的配置不进入兜底链。
- 开关 `llm_pool_enabled`（默认关）：开启后当前 provider 持续失败会切换到下一配置。
- 开关 `llm_pool_bind_fallback`：任务显式绑定的配置失败时，是否允许回落到全局轮询链（默认关）。
- 切换状态写入任务字段 `llm_failover_state` / `llm_failover_reason`；状态卡住可 `POST /api/llm/pool/reset` 重置熔断计数。
- `GET /api/llm/pool` 查看当前链、优先级与熔断状态。

### 7.2 五层重试（settings `llm_retry_policy`）

| 层 | 作用 | 默认 |
|---|---|---|
| connect | 建连/429/5xx（流开始前） | 3 次，指数退避 |
| empty | 完成但无 content block（仅 openai） | 2 次，指数退避 |
| stream | 未交付输出前的断流重放（同 provider 安全窗口） | 2 次，0.5s 起指数、封顶 4s |
| breaker | 轮询熔断：连续瞬时失败触发；硬失败（余额/密钥）立即熔断 | 3 次、冷却 1/5/30min 梯度 |
| intent | worker 以 model_error 收场后整条意图重跑 | 2 次、固定 3s |

```json
{"connect":{"attempts":3,"interval_ms":0},
 "empty":{"attempts":2,"interval_ms":0},
 "stream":{"attempts":2,"interval_ms":0},
 "breaker":{"attempts":3,"interval_ms":0},
 "intent":{"attempts":2,"interval_ms":3000}}
```

规则：`attempts`：0=默认、-1=关闭该层、>0=自定义（上限 20）；`interval_ms`：0=默认退避、>0=固定间隔（上限 1h）。profile 的 `retry` 只覆盖 connect/empty/stream，未设置字段继承全局。

## 8. 用量记录与成本

- 全局开关 `llm_record`（设置页，默认关）：开启后记录每次调用（模型、延迟、token、状态、错误）。
- `GET /api/llm/records?limit=50`、`GET /api/llm/records/{id}`、`GET /api/llm/records/by-model`；`DELETE /api/llm/records` 清理。
- 仪表盘 `GET /api/tokens/usage` 聚合全局 token；任务详情含 tokens 汇总。
- 记录正文可能包含提示词/工具输出，属于敏感数据；按需开启并纳入备份/清理策略。

## 9. 代理与网络

- profile 的 `proxy` 支持 http/https/socks5；留空时遵循 `ARTEX_LLM_PROXY` 或进程环境变量（HTTPS_PROXY 等）。
- 代理只影响 LLM 请求；目标流量代理（平台 MITM/全局出口）是另一套设置，见运维手册。
- 代理错误通常表现为“测试连接超时”，而直连正常；先用 `curl -x` 验证代理可达。

## 10. 排障速查

| 现象 | 常见原因 | 处理 |
|---|---|---|
| 401 未授权 / invalid api key | key 错、过期、带多余空格 | 重填 key；用测试连接验证 |
| 403 Request not allowed | 网关要求会话头/模型无权限/地区限制 | 设置 session_header_key；换模型；确认账号权限 |
| 400 请求体非法 | format 与端点不匹配 | openai/anthropic 互换测试 |
| 测试通过但任务失败 | 测试只发一次简单请求；任务有工具 schema/长上下文 | 看 /api/llm/records 错误；降 max_tokens、改非流式 |
| 请求长时间无响应/断流 | SSE 网关问题 | streaming=false |
| 429 rate limit | 并发/配额 | 提高间隔、开轮询、降低 workers |
| 上下文超限 | 窗口估算不准/长工具输出 | 调整 context_window_k；开启 noa 压缩需评估 |
| 任务一直“规划中” | provider 在重试或熔断 | 看 /api/llm/pool 与任务 failover_state；reset 后重试 |
| 空响应反复 | 模型不支持工具调用/返回空 content | empty 层重试；换支持 function calling 的模型 |

## 11. 安全与运维

- API key 存放在数据库 `llm_profiles.api_key`（读取接口不回传）。限制 DB 访问、加密备份，不要把数据库导出物外发。
- 当前 master key 轮换（`artex-rotate-key`）覆盖会话/凭据/代理池/C2 token，**不包含** LLM profile 的 api_key；轮换后 LLM key 不需要改，但也意味着它仍依赖 DB 访问控制。更严格场景建议轮换/删除 LLM key 时直接重新录入。
- 备份/恢复、审计、/metrics 监控见 `docs/deploy-v0.4.0.md` 与 `docs/ops-manual.md`。
