# SinoWhale-api 上游同步基线锁文件方案（upstream-baseline-lock）

- **日期**：2026-09-01
- **状态**：设计已确认，待实施
- **参考**：[2026-08-31-upstream-tag-separation.md](../../../SineWhaleAgent/docs/plans/2026-08-31-upstream-tag-separation.md)（SineWhaleAgent 仓库已验证落地的同构方案）

## 1. 背景与问题

SinoWhale-api fork 自 `QuantumNous/new-api`，两者各自管理 tag。上游已有 677 个 v-tag 且命名混乱（`v0.8.8.5.1`、`v0.9.19`、`v1.0.0-rc.30` 混杂），若上游 tag 与自有 tag 混入同一命名空间：

- 无法一眼判断"当前基于哪个上游版本"
- `git tag --sort=-v:refname` 等版本检测会被污染
- 定期同步上游时缺少锚点，靠人工翻历史极易出错

本方案将"上游同步基线"显式固化到一个锁文件，并以脚本半自动化维护，同时清理 CI 中对 fork 无效的上游残留发布流程。

## 2. 已验证的事实基线（2026-09-01）

| 事实 | 值 | 验证方式 |
|------|-----|---------|
| fork 基线（merge-base） | `8f31b305`（2026-07-07） | `git merge-base main upstream/main` |
| 基线包含的上游最新 release | **v1.0.0-rc.12**（`55b00fcf`，2026-06-18） | 祖先分析 + `git cat-file -t` |
| 上游最新 release | v1.0.0-rc.30 | `git ls-remote --tags upstream` |
| 上游领先提交数 | 202 | `git rev-list --count main..upstream/main` |
| 本地命名空间 | 仅 4 个自有 tag：v0.1.0 ~ v0.2.1，无上游遗留 tag | `git tag -l` |
| upstream remote | 已配置 → `https://github.com/QuantumNous/new-api` | `git remote -v` |

## 3. 方案设计

### 3.1 `upstream.lock.json`（核心交付物）

仓库根目录新增锁文件，记录同步基线。`tag` 给人看，`commit` 是机器锚点（merge-base 校验用）：

```json
{
  "schemaVersion": 1,
  "upstream": {
    "repo": "https://github.com/QuantumNous/new-api",
    "branch": "main",
    "tag": "v1.0.0-rc.12",
    "commit": "55b00fcf0956f3f1aa3906dd3c0f236b90e4f062"
  },
  "syncedAt": "2026-09-01"
}
```

- 初值 = 上表已验证基线（v1.0.0-rc.12 / `55b00fcf`）
- 自有 tag 线维持现有 `v0.x` 命名，不引入前缀；与上游的区分**完全由锁文件 + 命名空间纪律**承担

### 3.2 `scripts/sync-upstream.ps1`

PowerShell 5.1 兼容，与参考方案 `sync-upstream.ps1` 同构。

**参数**：

| 参数 | 行为 |
|------|------|
| `-Check` | 只读检查：报告当前基线与上游最新的差距（落后 N 个 release、merge-base 校验），不改任何文件 |
| `-To <tag>` | 更新基线到指定上游 tag：校验存在 → 交互确认 → 更新锁文件 → 打印人工 merge 指引。**不自动 merge** |
| （无参数） | 默认等价 `-Check` |

**流程（严格顺序）**：

1. 幂等确保 `upstream` remote 存在（缺失则自动添加）
2. `git fetch upstream main --no-tags` —— **铁律：上游 tag 永不进入本地命名空间**
3. `git ls-remote --tags upstream` 只读获取上游 tag 列表（不污染本地 refs）
4. SemVer 比较选出上游最新 release：
   - 解析 `^v(\d+)\.(\d+)\.(\d+)(?:-([0-9A-Za-z.-]+))?$`
   - prerelease 按点分段比较：数字段数值比较（解决 `v1.0.0-rc.9 < v1.0.0-rc.30`），字母段字典序
   - 无法解析的 tag（如 `v0.8.8.5.1` 五段式）跳过并 `WARN`
5. `-Check`：输出当前基线 tag/commit → 上游最新 tag、落后 release 数、`lock.commit` 是否等于上游 `<tag>` 的真实指向（ls-remote 解析 peeled commit 做配对校验，校验锁记录真实性；不一致或 tag 不存在则警告基线漂移）
6. `-To <tag>`：交互确认后更新锁文件（tag + commit + syncedAt），打印：
   ```
   人工 merge 指引：
     git merge <new-commit>      # 解决冲突后提交
     git push origin main
   ```

**幂等性**：`lock.tag` 已等于目标版本时直接退出，无任何写入。

### 3.3 CI 守卫（打包流程优化）

检查结论：5 个 workflow 中 3 个是上游残留（产物目标为上游官方命名空间 `calciumion/new-api` 或 fork 未使用的桌面壳），fork 实际发布链路是本地 `scripts/build-swapi-images.ps1` → `sinowhalex/swapi` → 部署包 → 自有服务器，完全绕过 CI docker 流程。

| Workflow | 处置 | 理由 |
|----------|------|------|
| release.yml | **保留不动** | 发布三平台二进制到 fork 自己的 GitHub Release，有价值 |
| docker-build.yml | **加守卫** | 推送目标 `calciumion/new-api`，fork 无 secrets/无权限，白跑 |
| docker-image-branch.yml | **加守卫** | 同上 |
| electron-build.yml | **加守卫** | fork 不使用 electron/ 桌面壳发布，白耗 CI 时长 |
| pr-check.yml | 不动 | 与 tag/发布无关 |

**守卫实现**（不删除文件、不改动任何 `new-api`/`QuantumNous`/`calciumion` 品牌引用，遵守 AGENTS.md 保护规则；上游同步 merge 时同守卫行零冲突）：

```yaml
# GitHub Actions 仅支持 job/step 级 if，需加到每个 job：
jobs:
  build_single_arch:
    if: github.repository == 'QuantumNous/new-api'
    # 已有条件的 job 合并书写，如 electron-build.yml 的 release job：
    # if: github.repository == 'QuantumNous/new-api' && startsWith(github.ref, 'refs/tags/')
```

`scripts/build-swapi-images.ps1` 本身不做改动——其版本检测 `git tag --sort=-v:refname` 在"本地命名空间只有自有 tag"的不变量下是纯净的，方案正是维护该不变量。

### 3.4 设计不变量（方案核心承诺）

1. **命名空间纯净**：本地/origin 永远只有自有 `v*` tag；`--no-tags` 是脚本 fetch 的强制参数
2. **上游版本单一事实源**：只存在于锁文件 + `ls-remote` 只读查询
3. **撞号免疫**：未来 fork 自有版本线到达 `v1.0.0` 与上游 `v1.0.0` 撞号无冲突——`git tag` 检测只见自有 tag，锁文件 `upstream.tag` 语义独立
4. **守卫即文档**：CI 中的 `github.repository == 'QuantumNous/new-api'` 条件显式声明了"此流程属于上游"的归属
5. **脚本不自动 merge**：更新锁文件与执行 merge 是两个动作，人工确认防止自动污染

## 4. 实施任务

| Task | 内容 | 验证 |
|------|------|------|
| 1 | 创建 `upstream.lock.json`（初值 v1.0.0-rc.12 / `55b00fcf`） | `git cat-file -t 55b00fcf` = commit |
| 2 | 编写 `scripts/sync-upstream.ps1`（`-Check` / `-To`，SemVer 比较含 prerelease 数字段） | 见回归清单 #1~#4 |
| 3 | 三个 workflow 加 `if: github.repository == 'QuantumNous/new-api'` 守卫 | 见回归清单 #5~#7 |
| 4 | README 增补「上游同步」章节：锁文件说明 + `scripts/sync-upstream.ps1 -Check` 用法 | 文档链接可达 |
| 5 | 回归验证（执行下方清单） | 全部 P0 通过 |

## 5. 未决问题

- **上游 v1.0.0 正式版发布后**：rc → stable 跟随策略无变化（`-To v1.0.0` 即可），锁文件结构无需演进
- **fork 自有版本线未来到达 v1.x 时**：与上游正式版撞号由不变量 #3 兜底；若届时出现实际混淆，再评估前缀 tag 迁移（本次已决策不做）

## 6. 功能回归测试清单

| # | 测试场景 | 前置条件 | 预期结果 | 优先级 |
|---|---------|---------|---------|--------|
| 1 | `sync-upstream.ps1`（默认/-Check）只读幂等 | 已配置 upstream remote | 本地 tag 列表、锁文件、工作区均无变化；输出基线差距报告 | P0 |
| 2 | fetch 不污染本地命名空间 | 执行过一次脚本 | `git tag -l` 仅含 v0.1.0~v0.2.1，无任何上游 tag；`git ls-remote --tags upstream` 可见上游 tag | P0 |
| 3 | SemVer 排序含 prerelease 数字比较 | 脚本内置排序逻辑 | `v1.0.0-rc.9 < v1.0.0-rc.30 < v1.0.0`；`v0.9.19 < v1.0.0-alpha.1`；`v0.8.8.5.1` 被跳过且输出 WARN | P0 |
| 4 | 锁文件初值正确 | Task 1 完成 | `tag=v1.0.0-rc.12`、`commit=55b00fcf...`；`git cat-file -t` = commit；`git merge-base --is-ancestor 55b00fcf main` 通过 | P0 |
| 5 | 守卫后 fork 不触发残留 workflow | Task 3 完成，push 自有 tag v0.2.2（测试） | docker-build.yml / docker-image-branch.yml / electron-build.yml 在 Actions 列表中跳过（skipped） | P0 |
| 6 | release.yml 行为不受影响 | push 自有 tag | 三平台构建照常触发，产物上传 fork GitHub Release | P0 |
| 7 | 打包脚本版本检测纯净 | 命名空间不变量成立 | `build-swapi-images.ps1` 检测到的 latest tag 为自有最新 tag，不受上游影响 | P0 |
| 8 | `-To` 幂等 | lock.tag 已是目标 | 二次执行 `-To <同 tag>` 直接退出，锁文件 mtime/内容不变 | P1 |
| 9 | `-To` 拒绝不存在的 tag | 传入 `v9.9.9` | 报错退出（非零码），锁文件不变 | P1 |
| 10 | 基线漂移告警 | 人工篡改 lock.commit 为非祖先 commit | `-Check` 输出基线漂移警告 | P1 |
| 11 | 守卫对上游无副作用 | 审查 YAML | 守卫条件与上游仓库名精确匹配，不改变 job 内任何品牌引用；上游 merge 时该行可无冲突合并 | P1 |
| 12 | merge 指引可用性 | `-To v1.0.0-rc.30` 后 | 打印的 `git merge <commit>` 命令可复制直接执行 | P2 |
| 13 | 锁文件 schemaVersion 前向兼容 | — | 未来加字段时旧脚本读到未知字段不崩溃（未知字段忽略） | P2 |
