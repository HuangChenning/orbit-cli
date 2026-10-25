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

先确保 **Git 在 PATH 中**。源码构建需要 Go 1.26 或 1.27，CI 固定验证 1.26.10 和 1.27.3。macOS / Linux 从源码构建：

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
| `orbit update check` | 检查 GitHub 最新稳定版 | 无需资产配置 |
| `orbit update` | 下载、校验 SHA256 后替换当前程序 | 无需资产配置 |
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

源码仓库中的 Go 代码位于 `src/main.go` 和 `src/internal/`，项目技能可在本机放入根目录的 `internal/skills/`；整个根目录 `internal/` 均由 Git 忽略。`go.mod` 和 `go.sum` 保留在仓库根目录；在根目录使用 `go build -o orbit ./src` 构建，或用 `go run ./src --help` 运行。

| 默认路径 | 用途 |
| --- | --- |
| `~/.orbit/bin/orbit` | 按上面的安装方式构建的程序 |
| `~/.orbit/orbit.yaml` | 全局配置 |
| `~/.orbit/cache` | 按来源和版本共享的 Git 缓存 |
| `~/.orbit/repo` | 下载后的资产，按 `mirror_dir` 组织为 skills、workflows、scripts、commands 等目录 |

配置查找优先级：`--config <file>` → 最近的项目或父目录 `orbit.yaml` → 全局配置。相对路径以配置文件所在目录为基准，支持 `~/...`。

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
| `version` | 字符串 | 必填，无默认值 | 上游 branch、tag 或完整的 40 位 commit SHA。分支建议写 `refs/heads/main`，标签建议写 `refs/tags/v1.2.3`。短名称无歧义时可用，branch/tag 同名会报错；分支跟随更新，commit SHA 固定内容，已缓存标签变化会被拒绝。 |
| `subdir` | 相对路径字符串 | 省略或为空时默认 `.` | 上游 checkout 内选取的目录，例如 `skills/skill-creator`；`.` 表示整个仓库。必须是目录，不能直接指向单个文件。 |
| `mirror_dir` | 相对路径字符串 | 可省略；`link: true` 时必填 | 个人本地 repo 内的目标目录，例如 `skills/skill-creator`。省略后该依赖只下载到缓存；首段路径用于选择链接类别。 |
| `link` | 布尔值 | 默认 `false` | 控制 `orbit link` 是否建立该资产的链接。`false` 不影响下载和发布；执行 `orbit link` 时会清理此前匹配的受管理链接，不删除资产文件。 |
| `labels` | 字符串列表 | 默认 `[]`，各项不能为空 | 显式分组标签，例如 `["core", "backend"]`，不会根据 `mirror_dir` 或类别自动生成。不带 `--label` 时包含全部依赖；重复传入 `--label` 按精确、区分大小写的 OR 规则取并集，无匹配时报错。 |

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
    version: "refs/heads/main"
    subdir: "skills/skill-creator"
    mirror_dir: "skills/core-skill"
    link: true
    labels: ["core", "backend"]
```

使用默认 `local_path` 时，选中的上游目录会组装到 `~/.orbit/repo/skills/core-skill`；执行 `orbit link` 后，映射到 `~/.claude/skills/core-skill` 和 `~/.codex/skills/core-skill`。`skills/` 后的后缀会拼接到各工具根目录。配置多个 skill 时，应使用 `skills/core-skill`、`skills/another-skill` 等独立目录，不能都指定为 `skills`。

相同 `source`、`version` 的依赖共用一份缓存，即使名称或 `subdir` 不同。标签筛选的是依赖，并不会复制缓存：清理被选中的共享缓存，也会移除未选中但共用该缓存的依赖缓存；已组装的 repo 资产及指向它们的链接仍然可用。

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

## 版本发布与二进制安装

从 [GitHub Releases](https://github.com/HuangChenning/orbit-cli/releases) 下载稳定版安装包，首个稳定版本为 **v1.0.0**。成功的 [Stable release checks 工作流](https://github.com/HuangChenning/orbit-cli/actions/workflows/ci.yml) 也提供供审核的候选产物；候选产物经批准且验证合并后的提交后才正式发布。

| 平台 | 压缩包 |
| --- | --- |
| macOS Apple Silicon | `orbit_1.0.0_darwin_arm64.tar.gz` |
| macOS Intel | `orbit_1.0.0_darwin_amd64.tar.gz` |
| Linux amd64 | `orbit_1.0.0_linux_amd64.tar.gz` |
| Windows amd64 | `orbit_1.0.0_windows_amd64.zip` |

`CANDIDATE.json` 记录源码 SHA、工具链、原生运行环境及构建参数。解压前对照 `SHA256SUMS` 校验压缩包：macOS 使用 `shasum -a 256 <压缩包>`，Linux 使用 `sha256sum <压缩包>`，PowerShell 使用 `Get-FileHash <压缩包> -Algorithm SHA256`，将完整哈希与对应文件名的条目比较。

macOS / Linux 解压后，将 `orbit` 放入 `~/.orbit/bin` 并加入 PATH，再运行 `orbit --version` 和 `orbit --help`。Windows 使用 `Expand-Archive` 解压，将 `orbit.exe` 放入 `$HOME\.orbit\bin`，加入用户 PATH 后在新终端执行同样的检查。二进制用户仅需系统 Git，无需安装 Go 或 Python。

**安装包未签名**：没有 Apple Developer ID 签名、公证或 Windows Authenticode 签名。若 macOS 阻止运行，先核对来源及校验值，再在“系统设置 → 隐私与安全性”中为该程序单独批准；保持系统全局安全保护开启。Windows 创建软链接需要开发者模式或管理员终端，权限不足时 Orbit 会明确报错。Go 1.27 构建的包要求 macOS 13 或更新版本，但该矩阵未测试 macOS 13/14。

`orbit --version` 无需配置或 Git 连接：源码构建显示 `dev`，候选包显示版本、完整提交 SHA、UTC 构建时间及 Go 版本。时间采用源码提交时间以便重复构建。安装包仅含程序、MIT 许可证、中英文 README 及变更记录。

[Go 官方运行要求](https://go.dev/wiki/MinimumRequirements)还包括 Linux 内核 3.2+、Windows 10+ / Windows Server 2016+。这些是工具链下限，Orbit 的原生验收范围仅限上表 runner 系统；不支持 WSL 1。

若从早期未发布版本升级，请从 `./src` 重新构建，将 `orbit_repo.local_path` 设置为 `~/.orbit/repo`，使用 `orbit vendor sync` 下载、`orbit sync` 提交推送。运行 `orbit link` 迁移匹配的旧缓存链接，再用 `orbit clean` 清理选中的受管理缓存；其他已有路径需手工检查。

## 在线更新

```sh
orbit update check
orbit update
```

更新只跟随 GitHub 最新**稳定 Release**，按本机系统与架构选择包，排除预发布版本。Orbit 安装前对照 Release 的 `SHA256SUMS` 校验压缩包；当前稳定版本相同或更新时保留当前程序，开发构建可以升级到已发布稳定版。这两个命令需要通过 HTTPS 访问 GitHub，无需资产配置或 Git 连接。

如果尚无稳定 Release，检查更新会明确提示；CI 候选产物不会被在线更新选中。下载、校验或解包失败时保留当前程序，安装在实际可执行文件所在目录使用临时文件，需要该目录的写权限，不尝试提权。若 Windows 锁定旧的运行程序，Orbit 会提示保留的备份路径，可在旧进程结束后删除。SHA256 用于完整性校验，安装包仍未签名。

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

Windows 构建文件名使用 `orbit.exe`。通过 `ORBIT_BIN` 可指定其他程序；CI 在 Go 1.27.3 中测试实际解压的候选程序，在 Go 1.26.10 中测试开发构建。测试仅使用临时本地仓库及合成资产，不依赖个人 Axon 清单。构建候选包时，在干净且已提交的源码目录中使用原生 Go 1.27.3 运行 `python3 tools/release.py build --version v1.0.0`，产物位于 `dist/`，不会发布版本。

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
