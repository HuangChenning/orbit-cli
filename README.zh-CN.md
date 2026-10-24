# Orbit CLI

[English](README.md) · 简体中文

**一份 YAML，把 Git 资产分发到你的开发工具。**

Go 1.20+ · 系统 Git · 声明式配置

[快速开始](#快速开始) · [命令参考](#命令参考) · [提交推送到 GitHub](#提交推送到-github) · [Axon 迁移检查](#从-axon-校验与修复)

## 简介

Orbit 面向在多个工具和项目之间复用 **skills、workflows、scripts、commands** 的开发者：`orbit vendor sync` 将上游资产下载到 `~/.orbit/repo`，`orbit link` 通过软链接分发资产，`orbit sync` 将本地资产提交并推送到个人 Git 仓库。

## 运行示例

以下会话来自隔离的本地 Git 示例，省略了本地仓库的完整路径；两个工具的 `skill-creator` 链接最终指向同一份组装后的资产。

```console
$ orbit vendor sync
synced skill-creator
assembled 1 dependencies into <local-repo>
$ orbit link
$ orbit check
{
  "issues": [],
  "status": "ok",
  "verification": "clean cache and configured symlink destinations"
}
```

## 工作原理

<img src="assets/readme/asset-flow.svg" width="640" alt="Orbit 资产流：vendor sync 经内部缓存将上游资产下载组装到本地仓库；link 将本地资产链接到工具目录；sync 将本地资产提交并推送到个人 Git 远端。">

`vendor sync` 下载并组装本地资产，不提交、不推送；`link` 按配置建立或移除工具链接；`sync` 提交并推送本地资产，不重新下载上游，也不从缓存覆盖本地内容。三个操作均不回写上游。

## 快速开始

先确保 **Git 在 PATH 中**，并安装 **Go 1.20+**。macOS / Linux 从源码构建：

```sh
git clone https://github.com/HuangChenning/orbit-cli.git
cd orbit-cli
mkdir -p "$HOME/.orbit/bin"
go build -o "$HOME/.orbit/bin/orbit" ./src
export PATH="$HOME/.orbit/bin:$PATH"
orbit init
```

将 PATH 设置加入 shell 启动文件以持久生效。`orbit init` 默认创建 `~/.orbit/orbit.yaml`，已有配置不会被覆盖。

先创建个人远端仓库，再编辑该文件，将 `YOUR_ACCOUNT` 替换为自己的账号并声明第一个依赖：

```yaml
version: "1.0"
global_cache: "~/.orbit/cache"

orbit_repo:
  remote: "git@github.com:YOUR_ACCOUNT/orbit-repo.git"
  branch: "main"
  local_path: "~/.orbit/repo"

link_targets:
  skills:
    - "~/.claude/skills"
    - "~/.codex/skills"

dependencies:
  - name: "skill-creator"
    source: "https://github.com/anthropics/skills.git"
    version: "refs/heads/main"
    subdir: "skills/skill-creator"
    mirror_dir: "skills/skill-creator"
    link: true
    labels: ["skills"]
```

然后执行：

```sh
orbit vendor sync
orbit link
orbit check
```

下载后的文件位于 `~/.orbit/repo/skills/skill-creator`，上例将此目录链接到 `~/.claude/skills/skill-creator` 和 `~/.codex/skills/skill-creator`。若目标已被真实文件、目录或父目录软链接占用，先手工处理冲突。

## 命令参考

| 命令 | 作用 | 常用选项 |
| --- | --- | --- |
| `orbit init` | 生成配置，不覆盖已有文件 | `--path <dir>` |
| `orbit vendor sync` | 下载上游并组装本地资产 | `--label <label>`、`--jobs <n>` |
| `orbit link` | 按 `link` 开关建立或移除匹配的链接 | `--label <label>`、`--force` |
| `orbit sync` | 将本地资产提交并推送到个人远端 | `--label <label>`、`--message <text>` |
| `orbit check` | 检查缓存与启用的链接 | 显式 Axon 输入下支持 `--fix` |
| `orbit clean` | 预览并确认清理选中的受管理缓存 | `--label <label>`、`--yes` |

标签区分大小写；重复 `--label` 按 **OR** 匹配，零匹配报错。Vendor 下载默认最多 4 个并发 Git 操作。Git 操作默认超时两分钟，可用 `--timeout` 调整。

Vendor 下载只获取配置指定版本的浅层快照，不拉取全部分支和完整历史。缓存属于内部实现，日常使用 `~/.orbit/repo` 中组装后的文件。

```sh
orbit vendor sync --label skills --label workflows --jobs 4
```

### 清理下载缓存

如果工具链接仍指向旧缓存路径，先运行 `orbit link` 更新链接。然后清理当前配置中所有依赖的受管理缓存：

```sh
orbit clean
```

命令会列出选中的依赖并要求确认。使用 `orbit clean --yes` 跳过确认，或用 `orbit clean --label skills` 选择分组。不需要也不支持 `--all` 参数。已从配置中移除的缓存不在清理范围内。

清理会保留 `~/.orbit/repo` 中组装后的资产及指向它们的链接。`orbit sync` 仍可在没有缓存的情况下提交并推送这些资产；下次执行 `orbit vendor sync` 时，会重新下载已清理的缓存，并更新选中的本地资产。

## 目录与配置

源码仓库中的 Go 代码位于 `src/main.go` 和 `src/internal/`，根目录的 `internal/skills/` 预留给项目技能。`go.mod` 和 `go.sum` 保留在仓库根目录；在根目录使用 `go build -o orbit ./src` 构建，或用 `go run ./src --help` 运行。

| 默认路径 | 用途 |
| --- | --- |
| `~/.orbit/bin/orbit` | 按上面的安装方式构建的程序 |
| `~/.orbit/orbit.yaml` | 全局配置 |
| `~/.orbit/cache` | 按来源和版本共享的 Git 缓存 |
| `~/.orbit/repo` | 下载后的资产，按 `mirror_dir` 组织为 skills、workflows、scripts、commands 等目录 |

配置查找优先级：`--config <file>` → 最近的项目或父目录 `orbit.yaml` → 全局配置。相对路径以配置文件所在目录为基准，支持 `~/...`。

`link_targets` 在全局按类别声明工具根目录；依赖只设置 `link` 开关。`mirror_dir` 的首段选择类别，其余路径拼接到各工具根目录。

将已有依赖的 `link` 改为 `false` 后，再执行 `orbit link`，会移除当前目标路径上仍指向该依赖本地资产目录的软链接，包括失效链接，也会清理匹配的旧缓存链接；不会删除缓存、本地资产、真实文件、目录或指向其他位置的链接。清理无需缓存存在，仍受 `--label` 限制。配置中不再包含的旧目标路径需手工处理。

| 依赖字段 | 默认或省略时的行为 |
| --- | --- |
| `name`、`source`、`version` | 必填；名称不能重复 |
| `subdir` | 默认 `.`，使用完整仓库 |
| `link` | 默认 `false`；执行 `orbit link` 时清理匹配的旧链接，仍可下载和发布 |
| `mirror_dir` | 本地 repo 中的资产路径；省略后只下载到缓存；启用链接时必须填写 |

## 提交推送到 GitHub

`orbit_repo` 指定个人远端与本地资产目录，请确保远端仓库已创建、Git 凭证可用：

```yaml
orbit_repo:
  remote: "git@github.com:YOUR_ACCOUNT/orbit-repo.git"
  branch: "main"
  local_path: "~/.orbit/repo"
```

`local_path` 可省略，默认即为 `~/.orbit/repo`。下载并检查本地资产后，执行发布：

```sh
orbit sync
```

- `vendor sync` 在替换受管理目录前准备全部选中资产。下载或复制校验失败时，保留此前组装的资产；全量刷新只清除此前管理的陈旧资产，按标签刷新保留未选中资产。刷新的受管理资产会丢弃本地修改，以恢复上游内容。
- 首次 Vendor 操作可克隆个人远端以保留历史和 README 等未管理文件，但不创建提交、不推送。后续下载不会发布待提交的本地修改。
- Vendor 组装排除各层 `.git`，并应用配置旁 `.orbitignore` 中的 Gitignore 风格规则。保留内部相对链接，拒绝绝对或越界链接；`.env` 不会被隐式排除。发布时也会拒绝本地新增的嵌套 Git 仓库。
- `sync` 发布当前受管理的本地资产，包括你的修改，不要求缓存存在，也不访问上游依赖。未管理文件的工作区修改及已有暂存修改需手工处理。仓库根目录中未跟踪的 `.DS_Store` 会保留且不发布，即使它已被 Git 忽略；已跟踪或已暂存的修改仍需手工处理。`--label` 只暂存选中资产及其 manifest 条目，其他本地修改留待后续发布。
- `.orbit-manifest.yaml` 记录组装时的上游来源和实际提交，不代表后续本地修改仍与该提交一致。内容未变时不创建空提交；推送失败保留本地提交以便重试。Git 属性或 filter 转换资产字节时拒绝发布，清除本次暂存并保留本地文件。

## 从 Axon 校验与修复

日常命令以 Orbit 配置为准。提供显式 Axon 输入后，可核对 vendor 声明及 Hub 已提交的四类资产，发现缺失依赖、工具映射、来源冲突和未覆盖文件：

```sh
orbit check --axon-config "$HOME/.axon/axon.yaml" \
  --vendors "$HOME/.axon/repo/axon.vendors.yaml"
```

确认报告后，使用 `--fix` 补齐已知来源的遗漏并去重：

```sh
orbit check --axon-config "$HOME/.axon/axon.yaml" \
  --vendors "$HOME/.axon/repo/axon.vendors.yaml" --fix
```

修复前保存原始字节备份，报告包含变更项；YAML 注释与格式会规范化。自定义依赖、标签和 `link: false` 会保留，来源冲突不自动覆盖，原 Axon 文件不改写。

在上述命令中增加 `--repo "$HOME/.orbit/repo"` 可比较本地仓库的文件清单，报告未配置的仓内资产；它不验证文件字节或版本，也不猜测未知资产的来源。

## 限制与行为边界

| 场景 | 行为 |
| --- | --- |
| 本地修改 | Vendor sync 将选中的缓存和受管理本地资产恢复至上游；通过工具链接修改会影响本地 repo，sync 会发布这些修改 |
| 版本选择 | branch 跟随上游，完整 SHA 固定提交；branch/tag 同名或已缓存 tag 变化时拒绝同步 |
| 链接冲突 | `link --force` 仅替换错误的软链接，真实文件、目录及父目录软链接须手工处理 |
| 个人仓库历史分叉 | 默认普通推送，不自动 merge/rebase；`sync --force` 需要确认，非交互时须显式使用 `--force --yes` |
| 缓存清理 | `clean --yes` 确认删除选中的受管理缓存，保留组装后的资产及链接；单独 `--yes` 不授予强推权限 |
| Git 扩展资产 | 暂不支持 Git LFS、submodule |
| 平台验证 | macOS arm64 已运行验证；macOS amd64、Linux amd64、Windows amd64 已通过交叉编译，Linux / Windows 原生行为仍待验证 |

命令成功退出 `0`，校验差异或操作失败退出非零。完整选项见 `orbit --help` 和 `orbit <command> --help`。
