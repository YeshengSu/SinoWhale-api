# 开发约定

**ATTENTION AI:** 以下为本项目所有 Prisma 相关服务的开发约定，你必须遵守。

---

## 1. Prisma 服务自动建表

### 规则

所有使用 Prisma 的微服务，其 `dev` 脚本必须在启动服务前自动执行 `prisma db push --skip-generate`。

### 标准模板

每个服务必须包含：

**`scripts/prisma-push.ts`：**

```typescript
import { config } from 'dotenv';
import { resolve } from 'path';
import { execSync } from 'child_process';

config({ path: resolve(__dirname, '../../../.env') });

// ⚠️ 必须显式覆盖，因为所有 Prisma 服务共用 POSTGRESQL_URI 变量名但指向不同数据库
process.env.POSTGRESQL_URI = 'postgresql://admin:dev_password@localhost:5432/<service-db-name>';

execSync('npx prisma db push --skip-generate', {
  cwd: resolve(__dirname, '..'),
  stdio: 'inherit',
});
```

**`package.json` 的 dev 脚本：**

```json
"dev": "tsx scripts/prisma-push.ts && tsx watch src/app.ts"
```

### 原因

1. 根 `.env` 中的 `POSTGRESQL_URI` 是 Prisma CLI 建表所需，但 Prisma CLI 默认只查找 `services/<name>/.env`（不存在），不会向上回退，所以必须通过脚本显式加载根 `.env`
2. `prisma db push --skip-generate` 只在 Schema 有变动时同步表结构，无变动时秒过，零副作用
3. 避免新成员克隆项目后启动时遇到 `table does not exist` 错误

### 现有服务

| 服务            | 状态      |
| --------------- | --------- |
| content-service | ✅ 已配置 |
| media-service   | ✅ 已配置 |

### ⚠️ 每个服务的 `init-env.ts` 也必须覆盖 `POSTGRESQL_URI`

因为根 `.env` 的 `POSTGRESQL_URI` 默认指向 `sinowhalex-content`，加载后会**覆盖** `env.ts` 的 `default()` 值，导致其他服务运行时连错数据库。

**`src/init-env.ts` 模板：**

```typescript
import { config } from 'dotenv';
import { resolve } from 'path';

config({ path: resolve(__dirname, '../../../.env') });

// ⚠️ 必须覆盖为当前服务的数据库
process.env.POSTGRESQL_URI = 'postgresql://admin:dev_password@localhost:5432/<service-db-name>';
```

### 新增服务检查清单

新建 Prisma 微服务时，必须完成：

- [ ] 创建 `scripts/prisma-push.ts`
- [ ] `package.json` 的 `dev` 脚本包含 `tsx scripts/prisma-push.ts && ...`
- [ ] 根 `package.json` 的 `prisma:generate` 覆盖该服务
- [ ] 根 `package.json` 的 `prisma:migrate` 覆盖该服务（如有需要）

---

## 2. 开发计划必须集成功能回归测试清单

### 规则

所有 `docs/plans/` 下的开发计划文档，**必须**在最后一个 Task 中包含一份 **功能回归测试清单（Task N: 功能回归测试清单）**。

### 清单结构要求

回归测试清单必须按模块拆分，覆盖以下维度：

| 维度 | 说明 |
|------|------|
| **数据模型层** | Schema 字段校验、默认值、兼容性 |
| **核心业务逻辑** | 正常路径、边界条件、并发、事务、异常 |
| **API 路由层** | 认证鉴权、参数校验、分页、错误码 |
| **代理/中间层** | 透传正确性、错误传播、密码验证 |
| **前端状态管理** | AuthContext 同步、刷新持久化、Token 过期 |
| **前端 UI 组件** | 渲染正确性、交互流、空状态、loading、错误处理、响应式 |
| **跨服务端到端** | 完整调用链路、降级行为 |
| **安全与权限** | 越权访问、JWT 篡改、参数注入 |
| **现有功能无回归** | 已有功能不受影响、数据库迁移不丢数据 |

### 每条测试用例格式

| # | 测试场景 | 前置条件 | 预期结果 | 优先级 |
|---|---------|---------|---------|--------|
| N.M.K | 场景描述 | 前置数据/状态 | 明确可验证的预期 | P0/P1/P2 |

- **P0**：核心路径，必须全部通过才能发布
- **P1**：重要边界/异常路径，应全部通过
- **P2**：锦上添花，可延后但需记录

### 原因

1. 开发计划是 AI 和开发者执行的唯一依据，缺失回归清单会导致边界条件、权限、端到端链路等验证被遗漏
2. 结构化 checklist 可在实现完成后逐条勾验，避免"感觉没问题"的假阳性
3. 回归清单作为计划文档的一部分，Code Review 时可以对照检查覆盖率
