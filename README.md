# Orbit CLI

使用系统 Git 同步上游资产，通过软链接分发到开发工具目录，并聚合为个人 Git 镜像。

需要 Git。源码构建需要 Go 1.20+：

```sh
git clone https://github.com/HuangChenning/orbit-cli.git
cd orbit-cli
mkdir -p "$HOME/.orbit/bin"
go build -o "$HOME/.orbit/bin/orbit" .
export PATH="$HOME/.orbit/bin:$PATH"
orbit init
```

将上述 PATH 设置加入 shell 启动文件即可持久生效。默认目录：

```text
~/.orbit/
  bin/orbit
  orbit.yaml
  cache/
  repo/
```

命令优先使用 `--config` 指定的文件，否则向上查找最近的项目 `orbit.yaml`，最后使用 `~/.orbit/orbit.yaml`。相对路径以配置文件所在目录为基准。

配置示例：

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
  workflows:
    - "~/.codeium/windsurf/global_workflows"

dependencies:
  - name: "skill-creator"
    source: "https://github.com/anthropics/skills.git"
    version: "refs/heads/main"
    subdir: "skills/skill-creator"
    mirror_dir: "skills/skill-creator"
    link: true
    labels: ["skills"]
```

统一的 `link_targets` 按类别声明工具根目录。上例把同一缓存资产链接到两个工具的 `skills/skill-creator`。依赖的 `link: false` 只关闭链接，仍可同步和镜像。`subdir` 默认为 `.`；省略 `mirror_dir` 的依赖仅同步，不参与镜像。省略 `orbit_repo.local_path` 时使用 `~/.orbit/repo`。

```sh
orbit sync                         # 仅同步缓存，默认最多 4 个并发 Git 操作
orbit sync --label skills           # --label 可重复，按 OR 匹配，区分大小写
orbit link                         # 仅处理 link: true 的依赖
orbit mirror                       # 提交并普通推送至 orbit_repo
orbit check                        # 检查缓存及启用的链接
orbit clean                        # 预览并确认清理当前配置引用的缓存
```

Sync 使用 fetch、reset --hard、clean -ffdx 恢复受管理的缓存，丢弃包括 ignored 文件在内的本地改动。通过链接修改也会影响共享缓存。相同来源和版本的依赖共享克隆，不同版本隔离；branch 跟随上游，完整 SHA 固定提交。branch/tag 同名和已缓存 tag 移动会报错。暂不支持 Git LFS 或 submodule。

Link 幂等；`link --force` 仅允许替换错误的软链接。真实文件、目录以及父目录软链接都需要先手工处理，工具不会递归删除它们。

Mirror 要求干净且已同步的缓存。它排除各层 `.git`，保留内部相对链接，拒绝绝对或越界链接。配置旁的 `.orbitignore` 使用 Gitignore 规则；`.env` 不会被隐式排除。`.orbit-manifest.yaml` 记录来源和实际提交。标签镜像保留未选中资产，全量镜像仅清除此前管理的陈旧资产；首次接管保留 README 等未管理内容。

提交前会核对 Git 索引中的资产字节。镜像仓或用户 Git 属性、filter 会转换字节时，命令报错并回滚；处理这些转换规则后再运行镜像。

普通镜像拒绝分叉历史，不自动 merge/rebase。推送失败保留本地提交，下次继续推送。`mirror --force` 需要交互确认，自动化须显式使用 `--force --yes`；单独 `--yes` 不授予强推权限。`clean --yes` 用于非交互确认。Git 操作默认超时两分钟，可通过 `--timeout` 设置。

从 Axon 校验和修复配置：

```sh
orbit check --axon-config "$HOME/.axon/axon.yaml" \
  --vendors "$HOME/.axon/repo/axon.vendors.yaml"

orbit check --axon-config "$HOME/.axon/axon.yaml" \
  --vendors "$HOME/.axon/repo/axon.vendors.yaml" --fix
```

检查 vendor 声明和 Hub 已提交的 skills、workflows、scripts、commands，报告缺失依赖、工具映射、来源冲突及未覆盖文件。`--fix` 补齐已知来源的遗漏并去重，保留自定义依赖、标签和 `link: false`；来源冲突不自动覆盖。修复前创建原始字节备份，报告包含变更项；YAML 注释与格式会规范化。

增加 `--repo "$HOME/.orbit/repo"` 可比较镜像文件清单，报告未配置的仓内资产；它不验证这些文件的字节或版本，也不会猜测未知资产的来源。日常命令以 Orbit 配置为准，不隐式读取或改写 Axon。

命令成功退出 0，校验差异或操作失败退出非零。更多参数见 `orbit --help` 和各子命令的 `--help`。
