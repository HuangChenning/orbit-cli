# Orbit CLI

[English](README.md) · 简体中文

**一份 YAML，把 Git 资产分发到你的开发工具。**

Go 1.26 / 1.27 · 系统 Git · 声明式配置

[快速开始](#快速开始) · [命令参考](#命令参考) · [配置参数](#配置参数参考) · [提交推送到 GitHub](#提交推送到-github) · [Axon 迁移检查](#从-axon-校验与修复)

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

在 macOS（Apple Silicon / Intel）或 Linux amd64 上使用 [Homebrew](https://brew.sh) 安装：

Ubuntu 如果还没有 `brew` 命令，请[先安装 Homebrew](#ubuntu提示找不到-brew-命令)。

```sh
brew install HuangChenning/tap/orbit
orbit --version
orbit init
```

[个人 tap](https://github.com/HuangChenning/homebrew-tap) 使用已发布的二进制包并校验 SHA256，无需 Go 或 Python。macOS 要求 13 或更新版本。配置与资产仍默认存放在 `~/.orbit`，程序由 Homebrew 管理。升级时先执行 `brew update`，再执行 `brew upgrade HuangChenning/tap/orbit`，使用这组命令替代 `orbit update`。

如果之前手动安装过 Orbit，请用 `command -v orbit` 查看当前使用的程序，PATH 中靠前的旧安装可能优先被执行。可以直接用 `"$(brew --prefix orbit)/bin/orbit" --version` 检查 Brew 版本，再调整 PATH 选择要使用的安装。

也可以从源码构建：先确保 **Git 在 PATH 中**，并安装 Go 1.26 或 1.27，CI 固定验证 1.26.10 和 1.27.3。macOS / Linux 执行：

```sh
git clone https://github.com/HuangChenning/orbit-cli.git
cd orbit-cli
mkdir -p "$HOME/.orbit/bin"
go build -o "$HOME/.orbit/bin/orbit" ./src
export PATH="$HOME/.orbit/bin:$PATH"
orbit init
```

源码安装时，将 PATH 设置加入 shell 启动文件以持久生效。`orbit init` 默认创建 `~/.orbit/orbit.yaml`，已有配置不会被覆盖。

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
    ref: "refs/heads/main"
    subdir: "skills/skill-creator"
    mirror_dir: "skills/skill-creator"
    vendor: true
    link: true
    labels:
      - skills
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
| `orbit update check` | 检查 GitHub 最新稳定版 | 无需资产配置 |
| `orbit update` | 下载、校验 SHA256 后替换当前程序 | 无需资产配置 |
| `orbit init` | 生成配置，不覆盖已有文件 | `--path <dir>` |
| `orbit vendor sync` | 下载上游并组装本地资产 | `--name <name>` / `--label <label>`、`--jobs <n>` |
| `orbit link` | 按 `link` 开关建立或移除匹配的链接 | `--name <name>` / `--label <label>`、`--force` |
| `orbit sync` | 将本地资产提交并推送到个人远端 | `--name <name>` / `--label <label>`、`--message <text>` |
| `orbit check` | 检查缓存与启用的链接，或检查重复声明和技能 | `--duplicates`；显式 Axon 输入下支持 `--fix` |
| `orbit clean` | 预览并确认清理选中的受管理缓存 | `--name <name>` / `--label <label>`、`--yes` |

使用 `--name` 按资产名称精确选择，或用 `--label` 按可选标签分组选择，二者互斥。两个参数都支持英文逗号列表和重复传参，会去除各项首尾空白、自动去重，并按 **OR** 取并集。名称和标签区分大小写；未知名称、标签零匹配、空项和中文逗号 `，` 均报错。筛选也适用于 `link`、`sync`、`check`、`clean`；显式 Axon 覆盖校验及修复始终比较完整配置。Vendor 下载默认最多 4 个并发 Git 操作。Git 操作默认超时两分钟，可用 `--timeout` 调整。

`link` 显示所用配置、每个资产的操作计数及最终汇总，区分创建、替换、未变化、移除、保留、原本不存在和失败。`sync` 显示资产校验、新提交 ID 或无本地变化提示、推送进度及成功结果。快照未变化时仍会重试推送已有提交；失败返回非零退出码。

Vendor 下载只获取配置指定 Git ref 的浅层快照，不拉取全部分支和完整历史。缓存属于内部实现，日常使用 `~/.orbit/repo` 中组装后的文件。

```sh
# 将示例资产名替换为配置中声明的名称。
orbit vendor sync --name core-skill,go-review
orbit vendor sync --name core-skill --name go-review
orbit vendor sync --label skills,workflows --jobs 4
```

### 清理下载缓存

如果工具链接仍指向旧缓存路径，先运行 `orbit link` 更新链接。然后清理当前配置中所有依赖的受管理缓存：

```sh
orbit clean
```

命令会列出选中的依赖并要求确认。使用 `orbit clean --yes` 跳过确认，或用 `orbit clean --label skills` 选择分组。不需要也不支持 `--all` 参数。已从配置中移除的缓存不在清理范围内。

清理会保留 `~/.orbit/repo` 中组装后的资产及指向它们的链接。`orbit sync` 仍可在没有缓存的情况下提交并推送这些资产；下次执行 `orbit vendor sync` 时，会重新下载已清理的缓存，并更新选中的本地资产。

## 目录与配置

源码仓库中的 Go 代码位于 `src/main.go` 和 `src/internal/`，项目技能可在本机放入根目录的 `internal/skills/`；整个根目录 `internal/` 均由 Git 忽略。`go.mod` 和 `go.sum` 保留在仓库根目录；在根目录使用 `go build -o orbit ./src` 构建，或用 `go run ./src --help` 运行。

| 默认路径 | 用途 |
| --- | --- |
| `~/.orbit/bin/orbit` | 按上面的安装方式构建的程序 |
| `~/.orbit/orbit.yaml` | 全局配置 |
| `~/.orbit/cache` | 按来源和版本共享的 Git 缓存 |
| `~/.orbit/repo` | 下载后的资产，按 `mirror_dir` 组织为 skills、workflows、scripts、commands 等目录 |

资产命令默认只读取 `~/.orbit/orbit.yaml`，项目或父目录中的配置不会覆盖它。需要使用其他文件时显式执行 `orbit --config ./orbit.yaml link`。相对路径以所选配置文件所在目录为基准，支持 `~/...`。

`link_targets` 在全局按类别声明工具根目录；依赖只设置 `link` 开关。`mirror_dir` 的首段选择类别，其余路径拼接到各工具根目录。

将已有依赖的 `link` 改为 `false` 后，再执行 `orbit link`，会移除当前目标路径上仍指向该依赖本地资产目录的软链接，包括失效链接，也会清理匹配的旧缓存链接；不会删除缓存、本地资产、真实文件、目录或指向其他位置的链接。清理无需缓存存在，仍受 `--label` 限制。配置中不再包含的旧目标路径需手工处理。

## 配置参数参考

`orbit.yaml` 只能包含一个 YAML 映射文档；未知字段、重复键和错误类型会被拒绝。字符串版本应加引号，例如 `"1.0"`；开关使用 YAML 布尔值 `true` / `false`。这些配置描述资产，`--jobs`、`--timeout`、`--message` 是命令行参数，不能写成 YAML 字段。

### 顶层字段

| 字段 | 类型 | 必填与默认值 | 说明 |
| --- | --- | --- | --- |
| `version` | 字符串 | 必填，固定为 `"1.0"` | 配置格式版本，与 Orbit 程序版本、各依赖的上游版本分别独立。 |
| `global_cache` | 路径字符串 | 必填，无自动默认值 | 内部 Git 缓存根目录。初始化模板填写 `~/.orbit/cache`；供工具使用的资产会组装到 `orbit_repo.local_path`。 |
| `orbit_repo` | 映射 | 解析时可省略；执行 `vendor sync`、`sync` 和建立启用的资产链接时必需 | 个人 Git 仓库配置，详见下表。缓存检查、清理可使用不包含此节的配置。 |
| `link_targets` | 类别到路径字符串列表的映射 | 可省略，默认无目标 | 按类别统一声明工具根目录。常用类别为 `skills`、`workflows`、`scripts`、`commands`，也支持自定义的单段类别名。每个启用链接的依赖，其类别必须至少配置一个目标。 |
| `dependencies` | 映射列表 | 可省略，默认为空列表 | 需要下载、组装、链接和发布的上游资产。初始空配置可使用 `[]`。 |

### 个人仓库：`orbit_repo`

| 字段 | 类型 | 必填与默认值 | 说明 |
| --- | --- | --- | --- |
| `remote` | 字符串 | 配置了 `orbit_repo` 时必填 | 个人 Git 远端，例如 `git@github.com:YOUR_ACCOUNT/orbit-repo.git` 或 HTTPS 地址。按原值交给 Git；本地远端应使用绝对路径，不依赖 Orbit 展开 `~` 或相对路径。 |
| `branch` | 字符串 | 必填，无默认值 | 个人仓库的目标分支名，例如 `main`；填写分支名，不使用 `refs/heads/main`。此字段不控制依赖的上游版本。 |
| `local_path` | 路径字符串 | 省略或为空时默认 `~/.orbit/repo` | 存放组装资产及 `.orbit-manifest.yaml` 的本地 checkout，与内部缓存分开。 |

远端仓库应预先创建，并确保 Git 有访问权限。Vendor sync 可能克隆个人远端以保留已有历史和未管理文件，但不提交、不推送；`orbit sync` 才发布本地受管理资产。

### 上游资产：`dependencies[]`

| 字段 | 类型 | 必填与默认值 | 说明 |
| --- | --- | --- | --- |
| `name` | 字符串 | 必填、非空且不能重复 | 报告和 manifest 中使用的资产标识；存储及链接路径由 `mirror_dir` 决定，不由名称决定。 |
| `source` | 字符串 | 必填、非空 | 上游 Git HTTPS/SSH 地址或本地仓库路径。`./vendor/example` 等本地相对路径以配置目录为基准，本地路径支持 `~/...`。 |
| `ref` | 字符串 | 必填，无默认值 | 上游 branch、tag 或完整的 40 位 commit SHA。分支建议写 `refs/heads/main`，标签建议写 `refs/tags/v1.2.3`。短名称无歧义时可用，branch/tag 同名会报错；分支跟随更新，commit SHA 固定内容，已缓存标签变化会被拒绝。 |
| `subdir` | 相对路径字符串 | 省略或为空时默认 `.` | 上游 checkout 内选取的目录，例如 `skills/skill-creator`；`.` 表示整个仓库。必须是目录，不能直接指向单个文件。 |
| `mirror_dir` | 相对路径字符串 | 可省略；`link: true` 时必填 | 个人本地 repo 内的目标目录，例如 `skills/skill-creator`。省略后该依赖只下载到缓存；首段路径用于选择链接类别。 |
| `vendor` | 布尔值 | 默认 `true` | 控制 `orbit vendor sync` 是否下载并组装此资产。`false` 会跳过更新，即使全量刷新也保留其已有本地文件和 manifest 记录；不影响链接和 `orbit sync` 发布。显式 `--name` 也不会覆盖此开关。 |
| `link` | 布尔值 | 默认 `false` | 控制 `orbit link` 是否建立该资产的链接。`false` 不影响下载和发布；执行 `orbit link` 时会清理此前匹配的受管理链接，不删除资产文件。 |
| `labels` | 字符串列表 | 默认 `[]`，各项不能为空 | 显式分组标签，例如 `["core", "backend"]`，不会根据 `mirror_dir` 或类别自动生成。不带 `--name` 或 `--label` 时选择全部依赖；`--label core,backend` 按精确匹配取并集，也支持重复参数，无匹配时报错。标签可省略，配置时放在 `vendor`、`link` 后的最后位置；多行列表与行内列表等价。 |

旧配置中的依赖 `version` 仍作为 `ref` 的别名读取；两者同时出现时报错，即使其中一个为空。生成或修复的条目使用 `ref`，顶层 `version: "1.0"` 仍表示 YAML 格式版本。已有缓存标识及 manifest 来源记录继续兼容。

当受管理本地资产已存在时，`vendor: false` 可以配合 `link: true` 使用；本地资产不存在时，链接会明确报错。运行 `orbit check` 会跳过已关闭 vendor 的缓存校验，但仍校验其启用的链接。

### 路径与链接映射

Orbit 在文件系统配置项中展开 `~` / `~/...`，适用于 `global_cache`、`orbit_repo.local_path`、`link_targets` 的值及本地依赖路径；不支持 `~otheruser` 或 `$HOME` 等 shell 变量展开。文件系统相对路径以 `orbit.yaml` 所在目录为基准，不以终端工作目录为基准。Git 远端字符串遵循上表 `remote` 的规则。Windows 路径建议使用正斜杠或 YAML 单引号，避免反斜杠被当作转义符。

`subdir`、`mirror_dir` 是仓库内部路径，不允许绝对路径、`~`、`..` 路径段、`.git` 路径段或 `.orbit-manifest.yaml` 路径段。`subdir` 可以为 `.`，`mirror_dir` 必须指向非根目录；不同依赖的 `mirror_dir` 不能相同或互相嵌套。类别名必须是安全的单段相对路径。

缓存根目录与本地 repo 根目录不能重叠，也不能是文件系统根目录或用户主目录，已有祖先不能是软链接。计算出的启用链接目标不能与受管理存储或彼此重叠，也不能是文件系统根目录或用户主目录。实际链接时，即便指定 `--force`，也会保护真实文件、目录和软链接父目录。

其余顶层参数配置完成后，下面这段配置把同一资产链接到两个工具：

```yaml
link_targets:
  skills:
    - "~/.claude/skills"
    - "~/.codex/skills"

dependencies:
  - name: "core-skill"
    source: "https://github.com/anthropics/skills.git"
    ref: "refs/heads/main"
    subdir: "skills/skill-creator"
    mirror_dir: "skills/core-skill"
    vendor: true
    link: true
    labels:
      - core
      - backend
```

使用默认 `local_path` 时，选中的上游目录会组装到 `~/.orbit/repo/skills/core-skill`；执行 `orbit link` 后，映射到 `~/.claude/skills/core-skill` 和 `~/.codex/skills/core-skill`。`skills/` 后的后缀会拼接到各工具根目录。配置多个 skill 时，应使用 `skills/core-skill`、`skills/another-skill` 等独立目录，不能都指定为 `skills`。

相同 `source`、`ref` 的依赖共用一份缓存，即使名称或 `subdir` 不同。标签筛选的是依赖，并不会复制缓存：清理被选中的共享缓存，也会移除未选中但共用该缓存的依赖缓存；已组装的 repo 资产及指向它们的链接仍然可用。

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

- `vendor sync` 在替换受管理目录前准备全部选中资产。下载或复制校验失败时，保留此前组装的资产；全量刷新只清除此前管理的陈旧资产，并保留 `vendor: false` 的资产；按名称或标签刷新保留未选中资产。刷新的受管理资产会丢弃本地修改，以恢复上游内容。
- 首次 Vendor 操作可克隆个人远端以保留历史和 README 等未管理文件，但不创建提交、不推送。后续下载不会发布待提交的本地修改。
- Vendor 组装排除各层 `.git`，并应用配置旁 `.orbitignore` 中的 Gitignore 风格规则。保留内部相对链接，拒绝绝对或越界链接；`.env` 不会被隐式排除。发布时也会拒绝本地新增的嵌套 Git 仓库。
- `sync` 发布当前受管理的本地资产，包括你的修改，不要求缓存存在，也不访问上游依赖。未管理文件的工作区修改及已有暂存修改需手工处理。仓库根目录中未跟踪的 `.DS_Store` 会保留且不发布，即使它已被 Git 忽略；已跟踪或已暂存的修改仍需手工处理。`--name` 或 `--label` 只暂存选中资产及其 manifest 条目，其他本地修改留待后续发布。
- `.orbit-manifest.yaml` 记录组装时的上游来源和实际提交，不代表后续本地修改仍与该提交一致。内容未变时不创建空提交；推送失败保留本地提交以便重试。Git 属性或 filter 转换资产字节时拒绝发布，清除本次暂存并保留本地文件。

## 检查重复配置与技能

```sh
orbit check --duplicates
orbit check --duplicates --name core-skill,go-review
```

该命令输出只读 JSON，检查重复的 `source` / `ref` / `subdir` 声明、同一来源与 ref 下嵌套的目录选择、跨资产的同名技能及字节完全一致的 `SKILL.md`。技能名称优先读取 YAML frontmatter，缺失时使用所在目录名。字节一致仅表示该文件相同，不代表整个技能目录等价。未知字段、重复 YAML 键和重复依赖名称仍作为配置错误拒绝。

检查读取 `orbit_repo.local_path` 中已有的文件，应用配置旁 `.orbitignore`，并报告排除数量和未能验证的资产。若集合中重叠的子目录已被完整排除，则不再报告该嵌套来源重叠。检查不下载、不比较上游版本、不修改配置、不删除文件，也不跟随软链接。JSON `status` 为 `ok`、`differences` 或 `incomplete`；后两种返回非零退出码。`--duplicates` 不能与 `--fix` 或 Axon 覆盖校验参数混用。

去重时保留首选来源，并使用 `skills/collection/copied-skill/` 这样的仓库相对规则排除集合中的冗余副本。仅调整声明和排除规则时，已下载文件会保留；之后执行 `vendor sync` 才会在替换受管理资产时应用规则。工具软链接需另行执行 `link` 才会变化。若需要后续通过 `link: false` 清理已有的自有链接，应保留对应的禁用声明。

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

修复前保存原始字节备份，报告包含变更项；YAML 注释与格式会规范化。自定义依赖、标签、`vendor: false` 和 `link: false` 会保留，来源冲突不自动覆盖，原 Axon 文件不改写。

在上述命令中增加 `--repo "$HOME/.orbit/repo"` 可比较本地仓库的文件清单，报告未配置的仓内资产；它不验证文件字节或版本，也不猜测未知资产的来源。

## 版本发布与二进制安装

从 [GitHub Releases](https://github.com/HuangChenning/orbit-cli/releases) 下载稳定版安装包，本文档对应 **v2.0.0**；从 v1.x 升级前请阅读下方迁移说明。成功的 [Stable release checks 工作流](https://github.com/HuangChenning/orbit-cli/actions/workflows/ci.yml) 也提供供审核的候选产物；候选产物经批准且验证合并后的提交后才正式发布。

| 平台 | 压缩包 |
| --- | --- |
| macOS Apple Silicon | `orbit_2.0.0_darwin_arm64.tar.gz` |
| macOS Intel | `orbit_2.0.0_darwin_amd64.tar.gz` |
| Linux amd64 | `orbit_2.0.0_linux_amd64.tar.gz` |
| Windows amd64 | `orbit_2.0.0_windows_amd64.zip` |

`CANDIDATE.json` 记录源码 SHA、工具链、原生运行环境及构建参数。解压前对照 `SHA256SUMS` 校验压缩包：macOS 使用 `shasum -a 256 <压缩包>`，Linux 使用 `sha256sum <压缩包>`，PowerShell 使用 `Get-FileHash <压缩包> -Algorithm SHA256`，将完整哈希与对应文件名的条目比较。

macOS / Linux 解压后，将 `orbit` 放入 `~/.orbit/bin` 并加入 PATH，再运行 `orbit --version` 和 `orbit --help`。Windows 使用 `Expand-Archive` 解压，将 `orbit.exe` 放入 `$HOME\.orbit\bin`，加入用户 PATH 后在新终端执行同样的检查。二进制用户仅需系统 Git，无需安装 Go 或 Python。

**安装包未签名**：没有 Apple Developer ID 签名、公证或 Windows Authenticode 签名。若 macOS 阻止运行，先核对来源及校验值，再在“系统设置 → 隐私与安全性”中为该程序单独批准；保持系统全局安全保护开启。Windows 创建软链接需要开发者模式或管理员终端，权限不足时 Orbit 会明确报错。Go 1.27 构建的包要求 macOS 13 或更新版本，但该矩阵未测试 macOS 13/14。

`orbit --version` 无需配置或 Git 连接：源码构建显示 `dev`，候选包显示版本、完整提交 SHA、UTC 构建时间及 Go 版本。时间采用源码提交时间以便重复构建。安装包仅含程序、MIT 许可证、中英文 README 及变更记录。

[Go 官方运行要求](https://go.dev/wiki/MinimumRequirements)还包括 Linux 内核 3.2+、Windows 10+ / Windows Server 2016+。这些是工具链下限，Orbit 的原生验收范围仅限上表 runner 系统；不支持 WSL 1。

### 从 v1.x 升级

**不兼容变更：**资产命令默认读取 `~/.orbit/orbit.yaml`，不受当前工作目录影响；项目或父目录中的 `orbit.yaml` 不再自动优先使用。继续使用项目配置时，必须显式指定路径：

```sh
orbit --config ./orbit.yaml vendor sync
orbit --config ./orbit.yaml link
orbit --config ./orbit.yaml sync
```

若使用统一全局配置，请自行核对后将选定配置复制至 `~/.orbit/orbit.yaml`。相对路径以配置所在目录为基准，移动配置时需相应调整。升级程序本身不会迁移配置、下载或删除资产，也不会改变已有软链接。YAML 格式版本仍为 `version: "1.0"`，Go 模块路径改为 `github.com/HuangChenning/orbit-cli/v2`。`link` 和 `sync` 现在会显示进度与操作汇总；脚本应通过退出码判断成功，通过 `check --duplicates` 的 JSON 获取重复检查结果。

若从早期未发布版本升级，请从 `./src` 重新构建，将 `orbit_repo.local_path` 设置为 `~/.orbit/repo`，使用 `orbit vendor sync` 下载、`orbit sync` 提交推送。运行 `orbit link` 迁移匹配的旧缓存链接，再用 `orbit clean` 清理选中的受管理缓存；其他已有路径需手工检查。

## 在线更新

通过 Homebrew 安装的用户应使用 `brew update` 和 `brew upgrade HuangChenning/tap/orbit`。下面的更新命令适用于手动安装的程序；Brew 用户也可以用 `orbit update check` 查询最新版本。

```sh
orbit update check
orbit update
```

更新只跟随 GitHub 最新**稳定 Release**，按本机系统与架构选择包，排除预发布版本。Orbit 安装前对照 Release 的 `SHA256SUMS` 校验压缩包；当前稳定版本相同或更新时保留当前程序，开发构建可以升级到已发布稳定版。这两个命令需要通过 HTTPS 访问 GitHub，无需资产配置或 Git 连接。

如果尚无稳定 Release，检查更新会明确提示；CI 候选产物不会被在线更新选中。下载、校验或解包失败时保留当前程序，安装在实际可执行文件所在目录使用临时文件，需要该目录的写权限，不尝试提权。若 Windows 锁定旧的运行程序，Orbit 会提示保留的备份路径，可在旧进程结束后删除。SHA256 用于完整性校验，安装包仍未签名。

## 常见问题与跨设备迁移

### Ubuntu：提示找不到 brew 命令

Ubuntu 默认不带 Homebrew。先安装依赖，再运行[官方安装脚本](https://brew.sh)：

```bash
sudo apt-get update
sudo apt-get install -y build-essential procps curl file git
/bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)"
```

按照安装器的后续提示配置环境。使用标准 Linux 安装路径和 Bash 时，执行以下命令，让当前及后续终端都能找到 Brew：

```bash
eval "$(/home/linuxbrew/.linuxbrew/bin/brew shellenv)"
echo 'eval "$(/home/linuxbrew/.linuxbrew/bin/brew shellenv)"' >> ~/.bashrc
brew --version
brew install HuangChenning/tap/orbit
```

如果已经在该位置安装过 Homebrew，只需配置 PATH。Zsh 用户将 `~/.bashrc` 换成 `~/.zshrc`；启动文件中的这一行添加一次即可。详见 [Homebrew Linux 文档](https://docs.brew.sh/Homebrew-on-Linux)。Orbit 当前的 Linux 安装包支持 x86_64/amd64，暂不支持 Linux ARM64。

### Homebrew：拒绝写入不安全的信任文件

如果安装时报错 `Refusing to write insecure trust store`，并指出 `~/.homebrew` 为 `group or world writable`，收紧目录及已有信任文件的权限后重试：

```bash
chmod 700 "$HOME/.homebrew"
if [ -f "$HOME/.homebrew/trust.json" ]; then
  chmod 600 "$HOME/.homebrew/trust.json"
fi
brew install HuangChenning/tap/orbit
```

上述路径适用于报错中的 `~/.homebrew`；若报错指向其他信任文件位置，请按实际路径处理。如果 `chmod` 失败，先用 `ls -ld "$HOME/.homebrew"` 检查归属，再联系系统管理员修复。不要使用 `sudo` 运行 Brew。以完整名称安装会自动信任对应 Formula，详见 [Homebrew tap 信任说明](https://docs.brew.sh/Tap-Trust)。

Brew 安装失败后出现 `orbit: command not found`，表示程序尚不可用。安装成功后再执行 `orbit --version` 和 `orbit init`。如果仍找不到程序，用 `command -v brew` 和 `"$(brew --prefix orbit)/bin/orbit" --version` 检查安装位置，再修正 PATH。

### 在新机器上配置个人仓库

先运行 `orbit init`，再编辑 `~/.orbit/orbit.yaml`。将 `orbit_repo.remote` 设置为已有个人仓库的 HTTPS 或 SSH 地址，`branch` 设置为目标分支（例如 `main`），`local_path` 设置为 `~/.orbit/repo`。远端仓库需要已存在，且当前机器的 Git 凭证有访问权限；私有仓库在下载时也需要认证。YAML 示例见[提交推送到 GitHub](#提交推送到-github)。

复用旧机器配置时，将旧机器的 `~/.orbit/orbit.yaml` 以及同目录的 `.orbitignore`（如有）传到新机器；替换前先检查目标位置已有文件。保留依赖声明和排除规则，调整机器专属的绝对路径，并在 `link_targets` 中只保留当前机器实际使用的工具目录。`~` 会解析为当前用户的主目录，例如 Ubuntu 上的 `/home/ubuntu`。`orbit init` 只生成初始模板，不会从个人资产仓库自动获取旧配置。

检查配置后执行：

```sh
orbit vendor sync
orbit link
orbit check
```

`vendor sync` 将配置的上游资产下载到 `~/.orbit/repo`，刷新的受管理资产会恢复为上游内容并丢弃本地修改；`link` 建立配置的工具链接。需要将本地受管理资产提交并推送到个人仓库时，再单独执行 `orbit sync`。

## 开发检查

安装 Go、Python 3.12 和 Git 后执行：

```sh
python3 -m pip install -r tests/requirements.txt
go build -o orbit ./src
go vet ./...
go test -race ./...
python3 -m unittest discover -s tests/acceptance -v
python3 -m unittest discover -s tests -p test_synthetic_config.py -v
```

Windows 构建文件名使用 `orbit.exe`。通过 `ORBIT_BIN` 可指定其他程序；CI 在 Go 1.27.3 中测试实际解压的候选程序，在 Go 1.26.10 中测试开发构建。测试仅使用临时本地仓库及合成资产，不依赖个人 Axon 清单。构建候选包时，在干净且已提交的源码目录中使用原生 Go 1.27.3 运行 `python3 tools/release.py build --version v2.0.0`，产物位于 `dist/`，不会发布版本。

## 限制与行为边界

| 场景 | 行为 |
| --- | --- |
| 本地修改 | Vendor sync 将选中的缓存和受管理本地资产恢复至上游；通过工具链接修改会影响本地 repo，sync 会发布这些修改 |
| 版本选择 | branch 跟随上游，完整 SHA 固定提交；branch/tag 同名或已缓存 tag 变化时拒绝同步 |
| 链接冲突 | `link --force` 仅替换错误的软链接，真实文件、目录及父目录软链接须手工处理 |
| 个人仓库历史分叉 | 默认普通推送，不自动 merge/rebase；`sync --force` 需要确认，非交互时须显式使用 `--force --yes` |
| 缓存清理 | `clean --yes` 确认删除选中的受管理缓存，保留组装后的资产及链接；单独 `--yes` 不授予强推权限 |
| Git 扩展资产 | 暂不支持 Git LFS、submodule |
| 平台验证 | PR 工作流在 macOS 15 arm64 / amd64、Ubuntu 24.04 amd64、Windows Server 2022 amd64 上执行原生功能测试；安装前查看对应候选包的运行结果 |

命令成功退出 `0`，校验差异或操作失败退出非零。完整选项见 `orbit --help` 和 `orbit <command> --help`。
