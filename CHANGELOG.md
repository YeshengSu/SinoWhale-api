# 更新日志 (Changelog)

本文件记录 NewApi（SinoWhale-api）的所有重要变更。

格式基于 [Keep a Changelog](https://keepachangelog.com/zh-CN/1.1.0/)，
版本号遵循 [SemVer](https://semver.org/lang/zh-CN/)。

## [Unreleased]

### Added
- **注册强制手机短信验证**：注册页现在必须提交手机号 + 短信验证码（验证码一次性烧毁、手机号查重、同码重放拒绝）；管理员/超管在后台创建的用户不经注册页，不受影响。`GET /api/status` 新增 `phone_verification: true` 驱动前端自动启用手机/验证码字段。
- **登录支持验证码方式（全用户通用）**：`POST /api/user/login` 支持 `{phone, sms_code}`（与账号密码方式并存），含按手机号 5 次/15 分钟防爆破锁；登录页新增「密码登录 / 验证码登录」切换。

### Fixed
- 注册页手机验证为 AGENT_MODE 退役时误删的强制门禁，本次恢复；`status_test` 同步更新为断言 `phone_verification: true`。

### Tests
- 新增 `controller/user_register_sms_test.go`：覆盖注册路径下手机号 + 短信验证码的强制门禁（缺验证码 / 错验证码 / 已占用手机号 → 拒绝；正确验证码 → 注册成功且 `users.phone` 入库、验证码被烧毁防重放），与 SMS 登录互不干扰。

### Added
- **订阅快照补齐**：首次购买（余额支付 / 支付订单 / 管理员绑定）现在从套餐写入 `Tag`、`PlanLevel`、5 小时/周/月 额度快照与模型白名单快照。此前仅升级路径写入这些字段，导致首购订阅不记录窗口用量、不校验模型白名单。
- **三档窗口额度拦截**：新增 `CheckUserPlanWindowLimits`，5 小时/周/月 任一窗口当期用量达到订阅快照上限即拒绝请求（`0` = 不限量）。拦截挂在订阅预扣费成功之后，拒绝即回滚本次预扣，返回 403「当前套餐{5 小时/周/月}额度已用完」。
- **令牌鉴权诊断日志**：`TokenAuth` 拒绝凭证时记录提交密钥的长度与前 10 位前缀（不记录完整密钥），用于排查上游 401 "Invalid token" 类问题。标记为临时诊断，可按需移除。
- **测试**：新增 `model/subscription_snapshot_test.go`，覆盖快照写入与窗口拦截语义（超量命中、0 不限、非 agent 不拦截）。

### Changed
- **套餐管理表单精简**：管理端新建/编辑套餐只需配置 名称、价格、等级（lite/plus/max）、可用模型、三档额度、支付方式。有效期固定 1 个月、额度池不限量（`total_amount=0`）、限购默认 1、到期降级组默认 `default`、tag 默认 `agent`，提交时自动填充。
- **可用模型改为多选下拉**：数据源为已启用渠道的模型列表（`GET /api/channel/models_enabled`），保留手输创建未收录模型；编辑已有套餐时保留原模型倍率，新模型倍率默认 1。
- **i18n**：套餐表单新文案同步至全部语言。

### Fixed
- SQLite 建表 DDL 补齐 agent-plan 相关列（`tag` / `plan_level` / `five_hour_limit` / `weekly_limit` / `monthly_limit` / `allowed_models`）。

## [v0.2.0] - 2026-08-31

### Changed
- **AGENT_MODE 退役**：登录/注册不再区分 agent 专用实例；统一登录三种方式（密码/短信/OAuth），钱包与套餐接口对所有登录用户开放。环境变量 `AGENT_MODE` 已废弃 —— 设置它仅会打印弃用日志，请从部署环境变量中移除。
- 登录签发的令牌统一携带 tag，模型白名单计费校验下沉为通用能力。

### Deployment Notes（v0.2.0 起部署必读）
- 从 v0.1.x 升级后，请从环境变量中移除 `AGENT_MODE`。
- 前端资源随本次变更更新，部署时需重新构建（`make build-web` 或对应 Docker 流程）。
- 套餐的 `allowed_models` 建议从渠道模型中选取；令牌 key 请使用自动生成的 48 位密钥（中继鉴权会按 `-` 截断提交的密钥，含 `-` 的自定义 key 无法通过认证）。
