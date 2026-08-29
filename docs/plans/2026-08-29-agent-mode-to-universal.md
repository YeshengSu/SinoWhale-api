# PRD：AGENT_MODE 迁移至通用模式改造方案

| 项 | 内容 |
| --- | --- |
| 文档版本 | v1.1（评审决议：仅保留 tag 参数、新增 per-phone 防爆破限流） |
| 日期 | 2026-08-29 |
| 状态 | 评审中 |
| 影响范围 | 后端（controller / service / model / common）、API 契约、部署配置 |
| 关联文档 | `PACKAGE_FLOW.md`、`.env.production.example`、`docs/plans/` |

---

## 1. 背景与问题

### 1.1 现状

v0.1.1 引入的 `AGENT_MODE` 是**实例级**功能开关（`common/init.go:L118` 读取环境变量），将整个 API 实例切换为「Agent 专用用户实例」。当前生产实例（api.sinxwhalex.com）保持 `AGENT_MODE=false`。

开关在代码中共有 **15 处门控**，覆盖：登录/注册流程（4 处）、`/api/status` 能力声明（2 处）、Token 标签强制（2 处）、计费与订阅（2 处）、管理员套餐视图（1 处）、常量定义与读取（4 处）。

### 1.2 核心问题

| # | 问题 | 证据 |
| --- | --- | --- |
| P1 | **架构错位**：SWAPI 在系统中的真实角色是 SWX 的服务账号网关（`AI_API_BASE_URL` + `AI_API_TOKEN`），无人类用户登录链路依赖手机号体系 | SWX `docker-compose.deploy.yml:L134`；全仓无 agent/plan 接口调用方 |
| P2 | **白名单空洞（资损风险）**：订阅套餐 `allowed_models` 的强制执行被 `AGENT_MODE` 门控（`billing_session.go:L400`），mode=false 时套餐可购买、额度会扣减、但模型白名单完全不生效 | `IsModelAllowedBySubscription` 仅在 agent 分支被调用 |
| P3 | **启用条件为零**：生产环境 SMS 凭据 0 条、管理员手机绑定 0/2，启用即全员锁死 | 2026-08-29 服务器核查 |
| P4 | **产品语义混淆**：同一实例承载「SWX 服务网关」与「C 端 Agent 订阅平台」双人格，登录形态、计费规则、前端入口互相耦合 | `misc.go:L58-59`、`token.go:L226` 等 |

### 1.3 迁移目标

将 `AGENT_MODE` 承载的能力**从实例级开关下沉为按实体/按请求的通用能力**，实现：

1. 一个登录接口同时支持三种认证方式（用户名+密码 / 手机号+密码 / 手机号+验证码）
2. Token 的 agent 标识由创建方按需声明，而非实例强制
3. 计费规则全场景统一，白名单按订阅配置生效，不再依赖实例开关
4. 管理后台与前端入口不再被实例开关隐藏/过滤

---

## 2. 目标与非目标

### 2.1 目标

- G1 统一登录：`POST /api/user/login` 单接口承载三种认证方式，方法由请求体字段自动判定
- G2 向后兼容：现有用户名+密码用户、以及按 agent 三要素（phone+sms_code+password）调用的存量客户端**零改动**
- G3 错误码体系：登录类错误返回机器可读的 `code` 字段，区分不同认证方式的失败原因
- G4 Token 参数化：`AddToken`/`UpdateToken` 支持可选 `tag` 参数声明 agent 标识
- G5 计费统一：移除计费路径上全部 `AGENT_MODE` 分支，白名单按订阅快照生效
- G6 管理与状态：管理员套餐列表不过滤 tag；`/api/status` 不再驱动前端隐藏登录/注册页
- G7 `AGENT_MODE` 环境变量退役：读取到时仅打警告日志，行为一律按通用模式
- G8 防爆破：手机号+密码登录增加「按手机号」失败锁定机制（双层限流，见 4.5）

### 2.2 非目标

- 不实现「手机号注册」（通用模式下手机号仍通过 `POST /api/user/self/phone/bind` 在登录后按需绑定，短信验证码链路保留）
- 不修改前端仓库代码（`/api/status` 字段保留保证旧前端不隐藏页面；前端适配另行排期）
- 不修改 SWX 集成（`X-SWX-*` Header、`SWX_HEADER_*` 独立开关不受影响）
- 不做数据库 Schema 变更（无表结构改动，无破坏性数据迁移）

---

## 3. 总体设计

### 3.1 迁移策略：开关退役、能力下沉

| 原 AGENT_MODE 行为 | 通用模式归宿 |
| --- | --- |
| 登录强制三要素（手机+短信+密码） | 变为登录方式之一：`phone + sms_code + password` 组合可选 |
| 注册强制手机验证、不自动登录 | 回归用户名+密码注册（自动登录）；手机号走绑定期接口 |
| Token 全部强制 `tag=agent` | `tag` 成为 Token 的可选字段，由创建方声明 |
| 计费白名单仅 agent 模式执行 | **所有**活跃订阅的 `allowed_models` 快照均强制执行 |
| 订阅匹配按 tag 隔离 | 统一匹配任意 tag 的活跃订阅 |
| 管理员套餐列表只看 agent 档位 | 不过滤，展示全部 |
| 前端隐藏登录/注册页 | `agent_mode`/`phone_verification` 固定返回 `false` |

### 3.2 `AGENT_MODE` 环境变量退役策略

```go
// common/init.go（改造后）
if GetEnvOrDefaultBool("AGENT_MODE", false) {
    SysLog("[DEPRECATED] AGENT_MODE 已于 v0.2.0 退役，请从环境配置中移除；当前按通用模式运行")
}
AgentModeEnabled = false // 常量保留一个过渡版本，全部引用点删除
```

- 过渡期（1 个版本）：保留 `AgentModeEnabled` 常量定义但恒为 `false`，所有业务分支删除；下下个版本删除常量本体
- 服务器 `.env.production` 当前未配置该变量，无需服务器变更

---

## 4. 模块一：统一登录认证

### 4.1 接口契约

`POST /api/user/login`（路由不变：`api-router.go:L71`，含 `CriticalRateLimit` + `TurnstileCheck`）

请求体（LoginRequest 扩展，字段全部向后兼容）：

```json
{
  "username": "string, 可选；用户名登录方式使用",
  "password": "string, 必填；三种方式均需要",
  "phone":    "string, 可选；手机号登录方式使用",
  "sms_code": "string, 可选；手机号+验证码方式使用"
}
```

**认证方式判定优先级**（在 `controller.Login` 内实现，替代原 `AgentModeEnabled` 分支）：

| 优先级 | 请求体组合 | 认证方式 | 流程 |
| --- | --- | --- | --- |
| 1 | `phone` + `sms_code` + `password` | 手机号+验证码 | 规范化手机号 → 格式校验 → 校验短信验证码（login purpose）→ 按手机号查用户 → 验证密码 → 销毁验证码 → 下发会话 |
| 2 | `phone` + `password`（无 `sms_code`） | 手机号+密码（**新增**） | 规范化手机号 → 格式校验 → 按手机号查用户 → 验证密码 → 下发会话 |
| 3 | `username` + `password` | 用户名+密码（现状默认） | 现有 `ValidateAndFill` 流程，逐字不动 |

判定规则（伪代码）：

```go
phone := model.NormalizePhone(req.Phone)
switch {
case phone != "" && req.SmsCode != "":
    return loginByPhoneSms(c, phone, req.SmsCode, req.Password)   // 优先级 1
case phone != "":
    return loginByPhonePassword(c, phone, req.Password)           // 优先级 2（新增）
case req.Username != "" && req.Password != "":
    return loginByUsernamePassword(c, req)                        // 优先级 3（现状）
default:
    common.ApiErrorI18nCode(c, i18n.MsgInvalidParams, "invalid_params")
}
// 边界：sms_code 非空但 phone 为空 → invalid_params（不按 username 字段猜测手机号）
```

### 4.2 关键设计约束

- **防枚举**：手机号+密码方式的失败统一返回 `invalid_credentials`（`MsgUserUsernameOrPasswordError`），不区分「手机号未注册」与「密码错误」；手机号+验证码方式保留 `phone_not_found`（发码阶段已隐式确认号码存在，无新增枚举面）
- **会话一致性**：三种方式成功后均走现有 session 下发逻辑（原 agent 模式「登录即签发强制 sk-key、不写会话」的行为废除；agent 客户端改用登录后调 Token API）
- **兼容原 agent 三要素客户端**：优先级 1 的流程与原 agent 分支语义一致（唯一差异：成功后下发标准会话而非 agent 专属响应体），存量客户端需按 G2 评估——见 4.4

### 4.3 错误码体系

**实现方式**：新增 `common.ApiErrorI18nCode(c, key, code)`，在现有错误 JSON 上**追加**机器可读 `code` 字段：

```json
{ "success": false, "message": "验证码错误或已过期", "code": "verification_code_error" }
```

旧前端只读 `success`/`message`，多出的 `code` 字段为增量字段，向后兼容。

| code | i18n key | 触发条件 | 前端建议处理 |
| --- | --- | --- | --- |
| `invalid_params` | `common.invalid_params` | 字段组合不合法（password 缺失 / sms_code 无 phone 等） | 提示参数不完整 |
| `phone_invalid` | `user.phone_invalid` | 手机号格式非法（非 CN 11 位） | 提示手机号格式 |
| `verification_code_error` | `user.verification_code_error` | 短信验证码错误/过期 | 引导重新获取验证码 |
| `phone_not_found` | `user.phone_not_found` | 验证码方式下手机号未注册 | 引导注册 |
| `invalid_credentials` | `user.username_or_password_error` | 密码错误 / 账号禁用 / 手机号+密码方式下号码未注册（防枚举统一） | 提示「账号或密码错误」 |
| `account_temporarily_locked` | `user.account_temporarily_locked`（**新增 key**） | 手机号密码连续错误 ≥5 次，锁定期内（见 4.5） | 提示「密码错误次数过多，请 15 分钟后重试或使用短信验证码登录」 |
| `password_login_disabled` | 现有 key | 全局密码登录被关闭 | 提示联系管理员 |

复用现有 i18n key（`i18n/keys.go:L88-96`）；**新增 1 个文案 key**：`user.account_temporarily_locked`（中英文同步补齐）。废弃不再使用：`user.login_sms_required`（通用模式下 sms_code 是可选凭据而非强制项）。

### 4.4 向后兼容性分析

| 调用方 | 现状 | 改造后 | 兼容 |
| --- | --- | --- | --- |
| 普通用户（username+password） | 唯一路径 | 优先级 3，逐字不动 | ✅ |
| agent 三要素客户端（phone+sms_code+password） | agent 模式专属分支 | 优先级 1，校验链一致；响应体从「agent 专属（含强制 api_key）」变为「标准会话」 | ⚠️ 响应体变化；当前生产无此类客户端（mode=false 从未启用），无实际影响面 |
| 手机号+密码 | 不存在 | 新增优先级 2 | ✅ 纯增量 |

### 4.5 手机号+密码登录防爆破限流（双层）

按 IP 的 CriticalRateLimit 对「分布式定向爆破」与「CGNAT 误伤」两个盲区无防御，故新增按手机号的失败锁定层：

| 层 | 阈值 | 实现 | 覆盖攻击面 |
| --- | --- | --- | --- |
| 第 1 层（已有） | 20 次 / 20 分钟 / IP（CT 桶，登录类接口共享） | `CriticalRateLimit`，零改动 | 单 IP 高频、脚本扫描 |
| 第 2 层（新增） | 同一手机号密码**连续错 5 次 → 该手机号的密码登录锁定 15 分钟**（自动解封） | `loginByPhonePassword` 内实现：key `login_fail:phone:<规范化手机号>`，失败 INCR + TTL 900s，≥5 次拒绝并返回 `account_temporarily_locked`；**成功登录清零** | 分布式定向爆破（换 IP 无效，计数在手机号维度） |

设计要点：

- **只锁「密码方式」，不锁账号**：被恶意锁号（攻击者故意输错以锁住受害者）的用户仍可用「手机号+验证码」登录——6 位一次性验证码不可爆破（错 1 次即失效、发码有独立限流），锁定的实质是强制降级到更安全的通道，而非拒绝服务
- 计数器复用现有 `common.InMemoryRateLimiter` / Redis 基础设施（与限流中间件同栈，Redis 部署时分布式生效）
- 锁定期内**优先级 1（验证码方式）与优先级 3（用户名方式）不受影响**

---

## 5. 模块二：Token 体系优化

### 5.1 接口契约

`AddToken` / `UpdateToken`（`controller/token.go`）请求体新增可选字段：

```json
{
  "name": "...",
  "tag": "agent"           // 新增：可选，缺省 ""；仅允许白名单值（见 5.2）。评审决议：不提供 is_agent 布尔参数
}
```

### 5.2 校验规则

```go
// constant/token.go 新增
var TokenTagWhitelist = []string{"", "agent"}   // 后续新增标签在此扩展

// controller/token.go AddToken / UpdateToken 统一处理
cleanToken.Tag = normalizeTokenTag(token.Tag)
// normalizeTokenTag：tag 不在白名单 → 400 invalid_params；空串合法（缺省）
```

替换现有两处强制赋值（`token.go:L226-230` 新建、`L308-313` 更新）。

### 5.3 鉴权兼容性

- `TokenAuth` 中间件（`middleware/auth.go`）**不读取 Tag**——Tag 不参与鉴权判定，本次零改动，验证逻辑不受影响
- Tag 的消费方仅两处：计费订阅匹配（第 6 节统一后不再依赖）、管理端 Token 列表展示（字段已随 `model.Token` 序列化，`token.go:L31`）
- 既有 Token 数据（`tag=""`）行为完全不变；`IssueAgentToken` 辅助函数（`token.go:L527`，name=`SinoWhale Agent`+tag=agent 幂等签发）保留，供 demo/seed 场景使用

---

## 6. 模块三：计费与订阅系统统一

### 6.1 改动点 1：白名单执行与实例开关解耦

`service/billing_session.go:L400`：

```go
// 改造前
if common.AgentModeEnabled && relayInfo.OriginModelName != "" {
// 改造后
if relayInfo.OriginModelName != "" {
```

**安全性论证**：`IsModelAllowedBySubscription`（`model/subscription.go:L2140`）对空快照/损坏 JSON/空列表一律返回 `true`（不限制），因此通用化后，未配置 `allowed_models` 的存量订阅行为不变；只有显式配置了白名单的套餐才会触发强制——这同时修复 P2 资损空洞。

### 6.2 改动点 2：订阅匹配统一

`service/billing_session.go:L438-443`：

```go
// 改造前：按模式二选一
if common.AgentModeEnabled {
    return model.HasActiveAgentSubscription(relayInfo.UserId)
}
return model.HasActiveUserSubscription(relayInfo.UserId)
// 改造后：统一
return model.HasActiveUserSubscription(relayInfo.UserId)
```

### 6.3 改动前后语义对照

| 场景 | 改造前（mode=false，现状） | 改造前（mode=true） | 改造后（通用） |
| --- | --- | --- | --- |
| 无订阅用户 | 钱包扣费 | 钱包扣费 | 钱包扣费（不变） |
| 有任意 tag 活跃订阅 | 订阅优先扣费 | 仅 agent tag 订阅参与 | 订阅优先扣费（含 agent tag） |
| 订阅套餐配置了 allowed_models | **不执行**（空洞） | 白名单外模型 403+回滚 | 白名单外模型 403+回滚 |
| 订阅套餐未配置 allowed_models | 不执行 | 允许全部模型 | 允许全部模型（不变） |
| 订阅额度不足 | 按 `allow_overflow` 回退钱包 | 同左 | 同左（不变） |

**结论**：对当前生产（mode=false、无订阅数据）行为零变化；对持有「配置了白名单的订阅」的用户从「不受限」收紧为「受限」——该收紧是修复漏洞而非行为回归，上线说明中需明示。

---

## 7. 模块四：管理后台与状态接口

### 7.1 管理员套餐列表去过滤

`controller/subscription.go:L124-131`：删除 `if common.AgentModeEnabled { queryBuilder = queryBuilder.Where("tag = ?", ...) }` 分支，所有 tag 套餐统一展示；`credits` 字段（v0.1.1）保留。

### 7.2 `/api/status` 固定通用值

`controller/misc.go:L58-59`：

```go
"phone_verification": false, // 通用模式：注册不强制手机验证（保留字段供旧前端读取）
"agent_mode":         false, // 已退役的实例开关，恒为 false；前端不再隐藏登录/注册页
```

两个字段**保留不删除**（兼容已部署前端），值固定为 `false`。

### 7.3 注册流程回归通用

`controller/user.go` 注册函数删除 4 处 agent 分支（`L363-384` 手机强制、`L429-431`、`L455-457`、`L488-494` agent 注册响应），回归：用户名+密码注册、自动登录、可选邮箱验证；手机号绑定沿用 `POST /api/user/self/phone/bind` / `phone/unbind`（短信验证链路不动）。

---

## 8. 数据迁移策略

### 8.1 生产数据库（v0.1.0 在运行）

| 对象 | 现状 | 迁移动作 |
| --- | --- | --- |
| `subscription_plans` / `user_subscriptions` 表 | **不存在**（agent-mode 随 v0.1.1 代码引入，v0.1.1 未部署） | 无需迁移；新版本启动时 `AutoMigrate` 建表 + `SeedAgentPlans` 幂等 seed |
| `users.phone` | 管理员均为 NULL | 不动；通用模式手机号可选 |
| `tokens.tag` | 全部 `''` | 不动；`tag=""` 在白名单内，行为不变 |
| `options` 表 SMS 配置 | 无 | 不动 |

**结论：零数据迁移，零停机风险。**

### 8.2 开发/测试库

可能存在 agent 数据（agent tag 订阅/Token）：Schema 无变化，通用逻辑接受任意 tag，无需清理。

### 8.3 配置与环境变量（五处同步检查清单）

| 检查项 | 动作 | 状态 |
| --- | --- | --- |
| 代码引用 | 删除 15 处 `AgentModeEnabled` 业务分支；`init.go` 保留弃用警告读取 | 本 PRD Task 1-5 |
| 模板声明 | `.env.production.example` 移除 `AGENT_MODE`，`SMS_*` 段保留并注明「发码/绑定手机用」 | Task 5 |
| Compose 透传 | `docker-compose.deploy.yml` 移除 `AGENT_MODE` 行；`SMS_*` 保留 | Task 5 |
| 服务器实值 | 当前未配置，无需变更 | 无动作 |
| 文档说明 | `PACKAGE_FLOW.md` 可选字段段改写（移除 AGENT_MODE 描述） | Task 5 |

---

## 9. 实施步骤

> 全程 TDD：每个 Task 先写失败测试再实现；每 Task 独立可提交、可回滚。

### Task 1：计费统一（最高优先，修复资损空洞）

1. 写失败测试：`service/billing_session_test.go` —— 有订阅+白名单快照时，mode 无关地拒绝白名单外模型（403+回滚）；空快照放行；任意 tag 订阅参与 subscription_first 匹配
2. 实现 6.1 / 6.2 两处改动
3. 验证：`go test ./service/... ./model/...` 全绿

### Task 2：Token tag 参数化

1. 写失败测试：`controller/token_test.go` —— `tag="agent"` 持久化；非法 tag 400；缺省 `""`；`UpdateToken` 同规则（评审决议：不提供 is_agent 布尔参数，仅保留 tag 单一入口）
2. 新增 `constant.TokenTagWhitelist` + `normalizeTokenTag`；改造 `AddToken`/`UpdateToken`
3. 验证：单测 + `go build ./...`

### Task 3：统一登录

1. 写失败测试：`controller/login_test.go` —— 三种方式判定矩阵（4.1 表全组合）、防枚举断言（手机号+密码下未注册号码返回 `invalid_credentials`）、验证码销毁、错误 code 字段存在性、per-phone 锁定（连错 5 次触发 `account_temporarily_locked`、锁定期内验证码方式可登录、成功登录清零、15 分钟自动解封）
2. 新增 `common.ApiErrorI18nCode`；新增 i18n key `user.account_temporarily_locked`；重构 `controller.Login` 为三函数（`loginByPhoneSms` / `loginByPhonePassword` / `loginByUsernamePassword`）
3. `loginByPhonePassword` 实现 per-phone 失败计数与锁定（4.5 第 2 层，复用 InMemoryRateLimiter / Redis）
4. 删除 `issueAgentTokenAndRespond` 调用路径与 `respondAgentRegister`；注册回归通用（7.3）
5. 验证：单测 + 手工 curl 三种方式联调（含锁定/解封路径）

### Task 4：管理与状态接口

1. 写失败测试：`controller/status_test.go`（agent_mode/phone_verification 恒 false）、`controller/subscription_test.go`（列表含非 agent tag 套餐）
2. 实现 7.1 / 7.2
3. 验证：单测

### Task 5：AGENT_MODE 退役与配置同步

1. `init.go` 弃用警告 + `AgentModeEnabled` 恒 false；全局 grep 确认无残余业务分支
2. `.env.production.example` / `docker-compose.deploy.yml` / `PACKAGE_FLOW.md` / `docs/DATABASE_SCHEMA.md`（如有订阅表描述）按 8.3 同步
3. 验证：`go build ./... && go vet ./...` + 全量单测

### Task 6：发布

按 `PACKAGE_FLOW.md` 第四章热更新流程：VERSION → tag `v0.2.0` → `build-swapi-images.ps1` → 分块上传 → 服务器复用 `.env.production`+`data`+`logs` → `install.sh` → 健康验证。

### Task 7：功能回归测试清单（发布门禁）

见第 10 节。**P0 全过方可发布。**

---

## 10. 测试计划

### 10.1 单元/集成测试（随 Task 1-5 交付）

| 模块 | 测试文件 | 覆盖点 |
| --- | --- | --- |
| 计费 | `service/billing_session_test.go` | 白名单 mode 无关执行、空快照放行、tag 无关匹配、回退钱包 |
| Token | `controller/token_test.go` | tag 参数化、白名单校验、兼容缺省 |
| 登录 | `controller/login_test.go` | 判定矩阵、防枚举、错误 code、会话一致性、per-phone 锁定/解封/清零 |
| 状态 | `controller/status_test.go` | 字段固定值 |
| 订阅 | `controller/subscription_test.go` | 列表不过滤 |

### 10.2 功能回归测试清单

> 维度：数据模型层 / 核心业务逻辑 / API 路由层 / 代理中间层 / 前端状态管理 / 前端 UI 组件 / 跨服务端到端 / 安全与权限 / 现有功能无回归
> 前端两项为跨仓库协作事项（前端仓库另行排期），本清单记录预期行为作为验收依据。

| # | 测试场景 | 前置条件 | 预期结果 | 优先级 |
| --- | --- | --- | --- | --- |
| 7.1.1 | 新版本启动 AutoMigrate 建订阅表 | 空 v0.1.0 库升级 | `subscription_plans`/`user_subscriptions` 创建，lite/plus/max 三档 seed | P0 |
| 7.1.2 | 存量 Token（tag=""）调用模型 | 现有数据 | 鉴权与计费与升级前一致 | P0 |
| 7.2.1 | 用户名+密码登录 | 现有账号 | 登录成功，会话正常 | P0 |
| 7.2.2 | 手机号+密码登录（新增） | 用户已绑手机 | 登录成功 | P0 |
| 7.2.3 | 手机号+密码登录、密码错误 | 已绑手机 | `success=false, code=invalid_credentials`，不泄露号码是否存在 | P0 |
| 7.2.4 | 手机号+验证码登录（原三要素） | 已绑手机+已收码 | 登录成功，验证码一次性销毁 | P0 |
| 7.2.5 | 手机号+验证码登录、验证码错误 | — | `code=verification_code_error` | P1 |
| 7.2.6 | sms_code 有值但 phone 为空 | — | `code=invalid_params` | P1 |
| 7.2.7 | 手机号格式非法（非 CN） | — | `code=phone_invalid` | P1 |
| 7.2.8 | 三种方式并发/连续登录 | 同一账号 | 会话互不影响，均有效 | P2 |
| 7.2.9 | 手机号+密码连续错误 5 次 | 已绑手机账号 | 第 6 次返回 `code=account_temporarily_locked` | P0 |
| 7.2.10 | 锁定期内改用验证码方式登录 | 同上处于锁定期 | 登录成功（锁定只作用于密码方式） | P0 |
| 7.2.11 | 锁定期内正确密码登录 | 同上 | 仍被拒（15 分钟内），返回同一错误码 | P1 |
| 7.2.12 | 锁定自动解封 | 锁定满 15 分钟 | 密码方式恢复，正确密码登录成功 | P1 |
| 7.2.13 | 成功登录清零失败计数 | 错 3 次后成功登录 | 计数归零，后续再错 5 次才触发锁定（不累计） | P1 |
| 7.3.1 | 订阅配置白名单后调用白名单外模型 | 用户持活跃订阅 | 403 + 预扣回滚 + 计费无残留 | P0 |
| 7.3.2 | 订阅未配置白名单 | 活跃订阅无 allowed_models | 全模型可调 | P0 |
| 7.3.3 | agent tag 订阅参与扣费 | 用户持 agent tag 订阅 | subscription_first 正常命中 | P0 |
| 7.3.4 | 非agent tag 订阅参与扣费 | 用户持其他 tag 订阅 | 同样命中（与改造前 mode=false 行为一致） | P0 |
| 7.3.5 | 订阅额度不足回退钱包 | 套餐允许 overflow | 回退成功，无双重扣费 | P1 |
| 7.4.1 | AddToken 携带 tag=agent | 登录态 | Token 落库 tag=agent | P0 |
| 7.4.2 | AddToken 携带非法 tag | 登录态 | 400 拒绝 | P1 |
| 7.4.3 | AddToken 缺省 tag | 登录态 | tag=""，与旧行为一致 | P0 |
| 7.4.4 | UpdateToken 修改 tag | 登录态 | 按白名单更新成功 | P2 |
| 7.5.1 | 管理员套餐列表 | 存在多 tag 套餐 | 全部返回，不过滤 | P0 |
| 7.5.2 | /api/status | 服务运行 | `agent_mode=false, phone_verification=false`，其余字段不变 | P0 |
| 7.6.1 | 旧版前端登录/注册页可见性 | 部署后访问 Web | 页面正常展示（不被隐藏） | P0 |
| 7.6.2 | 旧版前端登录（username+password） | — | 正常登录 | P0 |
| 7.7.1 | SWX 经服务账号 Token 调模型 | SWX 服务在线 | 计费路径行为与升级前一致 | P0 |
| 7.7.2 | SWX admin 连通性检查 | — | /api/status 正常，检查通过 | P0 |
| 7.8.1 | 越权：未登录调用 AddToken | — | 401 | P0 |
| 7.8.2 | JWT/会话篡改后调用登录后接口 | — | 拒绝 | P1 |
| 7.8.3 | 短信验证码跨 purpose 复用（login 码用于 bind） | — | 校验失败 | P1 |
| 7.8.4 | 登录接口限流 | 连续错误尝试 | CriticalRateLimit 生效 | P1 |
| 7.9.1 | 渠道管理/模型倍率 | — | 无变化 | P0 |
| 7.9.2 | Credits 计价显示 | 定价页 | 与 v0.1.1 一致 | P1 |
| 7.9.3 | 手机绑定/解绑（登录后） | 已配置 SMS 或 mock | 链路正常 | P1 |
| 7.9.4 | `AGENT_MODE=true` 环境变量注入启动 | 测试环境 | 仅打弃用警告，行为=通用模式 | P2 |

---

## 11. 上线与发布方案

沿用 `PACKAGE_FLOW.md` 标准离线部署流程（本地构建 → bundle → 分块上传 → 服务器热更新）：

1. **发布窗口**：低峰期（建议工作日晚间）
2. **前置备份**：`docker exec swapi-postgres pg_dump` + 数据卷 tar（按 3.4 备份惯例）
3. **部署顺序**：单容器应用（swapi-new-api），postgres/redis 不动
4. **部署后验证**：`/api/status` healthy 且 `agent_mode:false` → 三种登录方式手工验证（P0 项 7.2.1/7.2.2/7.2.4）→ SWX 侧服务账号调用抽测（7.7.1）
5. **观察期**：24 小时（监控项见第 12 节）

---

## 12. 上线后监控方案

| 指标/观测点 | 采集方式 | 正常基线 | 告警阈值 |
| --- | --- | --- | --- |
| 容器健康 | `docker ps` + healthcheck | healthy | unhealthy 持续 2 分钟 |
| `/api/status` | `curl http://localhost:3088/api/status` | `success:true` | 失败连续 3 次 |
| 登录失败率（按 code 分布） | 应用日志 grep `code=invalid_credentials|verification_code_error` | < 5% 登录请求 | > 15% 持续 10 分钟（疑似撞库/故障） |
| `phone_not_found` 出现量 | 同上 | 基线内 | 突增 10x（枚举探测信号） |
| `account_temporarily_locked` 出现量 | 同上 | 正常应 ≈ 0 | 非零突增 = 针对性爆破信号，人工核查来源 IP 分布 |
| 白名单拒绝（`当前套餐不支持模型`） | `docker logs` grep | 上线初期允许出现 | 持续高位需人工确认白名单配置 |
| SWX 服务账号调用成功率 | SWX ai-service 日志 + SWAPI 日志交叉 | ≥ 99% | < 95% 持续 5 分钟 |
| 短信发送失败 | `user.sms_send_failed` 日志 | 仅绑定期出现 | 突增告警 |
| Token tag 分布 | 管理端列表 / DB 查询 | 无异常批量 agent tag | 人工巡检 |
| DB 迁移日志 | 启动日志 `database migrated` | 无 error | 出现 migration error 立即回滚 |

日志规范：登录失败日志统一输出 `login method=<sms|phone_password|username> code=<business_code> result=<ok|fail>`，便于 grep 聚合。

---

## 13. 回滚方案

- **版本回滚**：服务器保留旧版本 bundle 目录，`cd /opt/swapi-deploy-bundle-vX.Y.Z && bash install.sh`（既有机制，秒级）
- **数据回滚**：本次无 Schema 变更、无破坏性迁移，无需数据回滚；如需彻底还原，恢复 11.2 的 pg_dump 备份
- **配置回滚**：`AGENT_MODE` 重注入环境变量**无效**（已退役，仅警告）——回滚必须走版本回退，不可用配置开关替代

---

## 14. 风险与开放问题

| # | 风险 | 等级 | 缓解 |
| --- | --- | --- | --- |
| R1 | 白名单收紧影响意外持有「已配置白名单订阅」的用户 | 低（生产无订阅数据） | 上线公告明示；监控 12.3 拒绝率 |
| R2 | 三方式判定歧义（同时携带 username+phone） | 低 | 判定优先级固定并写入接口文档；sms_code 无 phone 一律 invalid_params |
| R3 | 旧前端读取 `agent_mode` 做其他假设 | 低 | 字段保留、值恒 false，语义与旧默认一致 |
| R4 | SWX 前端/订单系统未来想消费 agent 套餐 | 开放问题 | tag 体系已参数化，随时可按 tag 扩展展示层，无需再动计费 |

**评审决议（2026-08-29，已回写正文）**：
1. ~~`is_agent` 布尔便捷参数是否保留~~ → **已决：不提供**，仅保留 `tag` 单一入口，减少契约面（5.1 / 5.2 / Task 2 已同步）
2. ~~手机号+密码登录是否需要独立限流阈值~~ → **已决：双层限流**，IP 层沿用 CriticalRateLimit（20 次 / 20 分钟），新增按手机号「连错 5 次锁 15 分钟」防爆破层（4.5，Task 3 / 回归清单 7.2.9-7.2.13 / 监控方案已同步）
