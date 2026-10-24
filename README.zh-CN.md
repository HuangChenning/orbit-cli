# Orbit CLI

[English](README.md) · 简体中文

**一份 YAML，把 Git 资产分发到你的开发工具。**

Go 1.20+ · 系统 Git · 声明式配置

[快速开始](#快速开始) · [命令参考](#命令参考) · [镜像到个人仓库](#镜像到个人仓库) · [Axon 迁移检查](#从-axon-校验与修复)

## 简介

Orbit 面向在多个工具和项目之间复用 **skills、workflows、scripts、commands** 的开发者：从上游同步到共享缓存，用软链接分发选中的目录，也可聚合后推送到个人 Git 仓库。

## 运行示例

以下输出来自隔离的本地 Git 示例；两个工具的 `skill-creator` 链接最终指向同一份缓存。

```console
$ orbit sync
synced skill-creator
$ orbit link
$ orbit check
{
  "issues": [],
  "status": "ok",
  "verification": "clean cache and configured symlink destinations"
}
```

## 工作原理

<img src="assets/readme/asset-flow.svg" width="100%" alt="Orbit 资产流：上游 Git 经 sync 写入共享缓存；link 将缓存链接到工具目录；mirror 将缓存汇聚到本地镜像，再 push 到个人 Git 远端。">

`sync` 只更新缓存，`link` 按配置建立或移除链接，`mirror` 才会提交并推送。三个操作独立执行；个人镜像不回写上游。

## 快速开始

先确保 **Git 在 PATH 中**，并安装 **Go 1.20+**。macOS / Linux 从源码构建：

```sh
git clone https://github.com/HuangChenning/orbit-cli.git
cd orbit-cli
mkdir -p "$HOME/.orbit/bin"
go build -o "$HOME/.orbit/bin/orbit" .
export PATH="$HOME/.orbit/bin:$PATH"
orbit init
```

将 PATH 设置加入 shell 启动文件以持久生效。`orbit init` 默认创建 `~/.orbit/orbit.yaml`，已有配置不会被覆盖。

编辑该文件，声明第一个依赖：

```yaml
version: "1.0"
global_cache: "~/.orbit/cache"

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
orbit sync
orbit link
orbit check
```

上例将资产链接到 `~/.claude/skills/skill-creator` 和 `~/.codex/skills/skill-creator`。若目标已被真实文件、目录或父目录软链接占用，先手工处理冲突。

## 命令参考

| 命令 | 作用 | 常用选项 |
| --- | --- | --- |
| `orbit init` | 生成配置，不覆盖已有文件 | `--path <dir>` |
| `orbit sync` | 从上游更新缓存 | `--label <label>`、`--jobs <n>` |
| `orbit link` | 按 `link` 开关建立或移除匹配的链接 | `--label <label>`、`--force` |
| `orbit mirror` | 聚合、提交并推送个人镜像 | `--label <label>`、`--message <text>` |
| `orbit check` | 检查缓存与启用的链接 | 显式 Axon 输入下支持 `--fix` |
| `orbit clean` | 预览并确认清理选中的受管理缓存 | `--label <label>`、`--yes` |

标签区分大小写；重复 `--label` 按 **OR** 匹配，零匹配报错。同步默认最多 4 个并发 Git 操作。Git 操作默认超时两分钟，可用 `--timeout` 调整。

```sh
orbit sync --label skills --label workflows --jobs 4
```

## 目录与配置

| 默认路径 | 用途 |
| --- | --- |
| `~/.orbit/bin/orbit` | 按上面的安装方式构建的程序 |
| `~/.orbit/orbit.yaml` | 全局配置 |
| `~/.orbit/cache` | 按来源和版本共享的 Git 缓存 |
| `~/.orbit/repo` | 首次镜像时创建的本地仓库 |

配置查找优先级：`--config <file>` → 最近的项目或父目录 `orbit.yaml` → 全局配置。相对路径以配置文件所在目录为基准，支持 `~/...`。

`link_targets` 在全局按类别声明工具根目录；依赖只设置 `link` 开关。`mirror_dir` 的首段选择类别，其余路径拼接到各工具根目录。

将已有依赖的 `link` 改为 `false` 后，再执行 `orbit link`，会移除当前目标路径上仍指向该依赖缓存子目录的软链接，包括失效链接；不会删除缓存内容、真实文件、目录或指向其他位置的链接。清理无需缓存存在，仍受 `--label` 限制。若同时更改来源、版本、`subdir`、`mirror_dir` 或全局目标，旧路径无法按当前配置匹配，需手工处理。

| 依赖字段 | 默认或省略时的行为 |
| --- | --- |
| `name`、`source`、`version` | 必填；名称不能重复 |
| `subdir` | 默认 `.`，使用完整仓库 |
| `link` | 默认 `false`；执行 `orbit link` 时清理匹配的旧链接，仍可同步和镜像 |
| `mirror_dir` | 省略后只同步，不参与镜像；启用链接时必须填写 |

## 镜像到个人仓库

在同一配置中加入 `orbit_repo`，将 `YOUR_ACCOUNT` 替换为自己的账号，并确保远端仓库已创建、Git 凭证可用：

```yaml
orbit_repo:
  remote: "git@github.com:YOUR_ACCOUNT/orbit-repo.git"
  branch: "main"
  local_path: "~/.orbit/repo"
```

`local_path` 可省略，默认即为 `~/.orbit/repo`。完成同步后运行：

```sh
orbit mirror
```

- 要求选中的缓存干净且已同步；任何一项预检失败都不发布部分快照。
- 排除各层 `.git`，保留内部相对链接，拒绝绝对或越界链接。配置旁的 `.orbitignore` 使用 Gitignore 规则；`.env` 不会被隐式排除。
- `.orbit-manifest.yaml` 记录来源和实际提交。标签镜像保留未选中资产，全量镜像只清除此前管理的陈旧资产；首次接管保留 README 等未管理内容。
- 内容未变时不创建空提交。推送失败保留本地提交，下次继续推送；Git 属性或 filter 转换资产字节时会报错并回滚。

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

在上述命令中增加 `--repo "$HOME/.orbit/repo"` 可比较镜像文件清单，报告未配置的仓内资产；它不验证文件字节或版本，也不猜测未知资产的来源。

## 限制与行为边界

| 场景 | 行为 |
| --- | --- |
| 缓存有本地改动 | Sync 使用 fetch、reset --hard、clean -ffdx 丢弃改动，包括 ignored 文件；通过链接修改也会影响共享缓存 |
| 版本选择 | branch 跟随上游，完整 SHA 固定提交；branch/tag 同名或已缓存 tag 变化时拒绝同步 |
| 链接冲突 | `link --force` 仅替换错误的软链接，真实文件、目录及父目录软链接须手工处理 |
| 镜像历史分叉 | 默认普通推送，不自动 merge/rebase；`mirror --force` 需要确认，非交互时须显式使用 `--force --yes` |
| 非交互清理 | `clean --yes` 确认删除选中的受管理缓存；单独 `--yes` 不授予镜像强推权限 |
| Git 扩展资产 | 暂不支持 Git LFS、submodule |
| 平台验证 | macOS arm64 已运行验证；macOS amd64、Linux amd64、Windows amd64 已通过交叉编译，Linux / Windows 原生行为仍待验证 |

命令成功退出 `0`，校验差异或操作失败退出非零。完整选项见 `orbit --help` 和 `orbit <command> --help`。
