# rime-ice-installer

一个面向 Arch Linux 的 TUI 安装器，用来自动安装并配置：

- Fcitx5
- Fcitx5 Rime
- Fcitx5 自定义深灰主题 `installer-dark`
- 雾凇拼音 `rime-ice`
- 万象 LTS 语法模型

## 特性

- 基于 `dialog` 的终端交互界面
- 安装过程中显示步骤进度条
- 自动检测 KDE Wayland / 通用 X11
- 自动写入 Fcitx5 `installer-dark` 主题和快捷键清理配置
- 自动把 `~/.config/fcitx/rime` 和 `~/.local/share/fcitx5/rime` 备份为同级 `_bak`
- 自动下载 `rime-ice` nightly 和 `wanxiang-lts-zh-hans.gram`
- 安装后自动编译并激活 `rime_ice` 方案

## 构建与安装

在仓库根目录直接构建本地二进制：

```bash
go build -o rime-ice-installer .
```

安装到 `$GOBIN` 或 `$GOPATH/bin`：

```bash
go install .
```

常用 Go 命令：

```bash
go test ./...
go run .
go run . --yes --dry-run
go mod tidy
```

## 使用

如果是本地构建出的二进制，交互模式运行：

```bash
./rime-ice-installer
```

如果已经执行过 `go install .`，且安装目录已加入 `PATH`，也可以直接运行：

```bash
rime-ice-installer
```

非交互模式：

```bash
./rime-ice-installer --yes
```

只看执行计划：

```bash
./rime-ice-installer --yes --dry-run
```

可用参数：

- `--yes`
- `--dry-run`
- `--verbose`
- `--enable-wanxiang`
- `--workspace-dir`

## 实际安装内容

通过 `pacman` 安装：

- `fcitx5`
- `fcitx5-gtk`
- `fcitx5-qt`
- `fcitx5-configtool`
- `fcitx5-rime`
- `librime`
- `opencc`

说明：

- 默认主题改为自定义的 `installer-dark`，它基于 Fcitx5 自带 `default-dark` 生成，但移除了 `AccentColorField`，这样 KDE 的紫色强调色不会再污染候选框。
- 当前 Arch 仓库里的 `librime` 已包含 `librime-octagram.so`，因此没有额外安装单独的 `librime-plugin-octagram` 包。
- `fcitx5-im` 在 Arch 中是包组，不是独立包名，因此安装器直接安装实际需要的包。
- 安装器会显式安装 `opencc`，避免 `librime` 与旧版 OpenCC 的 soname 不匹配，导致“中州韵输入法不可用”。

## 会修改的 Fcitx5 配置

- `~/.config/fcitx5/conf/classicui.conf`
- `~/.config/fcitx5/conf/clipboard.conf`
- `~/.config/fcitx5/conf/quickphrase.conf`
- `~/.config/fcitx5/conf/unicode.conf`
- `~/.config/fcitx5/profile`

其中会自动清空这些多余快捷键：

- `clipboard.TriggerKey`
- `clipboard.PastePrimaryKey`
- `quickphrase.TriggerKey`
- `unicode.TriggerKey`

## Rime 部署目录

- `~/.config/fcitx/rime`
- `~/.local/share/fcitx5/rime`

部署前会先生成：

- `~/.config/fcitx/rime_bak`
- `~/.local/share/fcitx5/rime_bak`
