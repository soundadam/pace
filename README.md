# pace

**给代理、校园内网、CERNET 测速的命令行工具**——多视角、不打分、诚实数据。

[![CI](https://github.com/soundadam/pace/actions/workflows/ci.yml/badge.svg)](https://github.com/soundadam/pace/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/soundadam/pace.svg)](https://pkg.go.dev/github.com/soundadam/pace)
[![Release](https://img.shields.io/github/v/release/soundadam/pace)](https://github.com/soundadam/pace/releases)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)

pace 不把不同线路压成一个"网络分数"，而是按顺序回答几个清楚的
问题：南大校园网或校园 VPN 是否通、公网出口表现如何、负载下网络是否
仍有响应，以及附近的运营商侧参考服务器表现如何。支持 macOS、Linux 和
Windows，Go 编写，终端界面基于 Bubble Tea 内联渲染。

## 为什么是 pace

- **多视角，不打分**：每个目标独立回答一个问题；不同目标的结果永远
  不会被替换、排名或折算成综合分数。
- **教育网优先**：NJU 校园网是产品主线，同济等教育网站点是可选参考
  方向，M-Lab 提供公网出口视角。
- **诚实失败**：失败记录零速率和稳定的失败阶段；取消后未开始的目标记
  `skipped` 与 null 速率。失败不伪装成成功。
- **JSON 自动化**：全局 `--json` 输出单个无 ANSI 文档，退出码稳定，
  历史可导出 JSONL/CSV。
- **隐私 fail-closed**：M-Lab 测试要求显式接受其隐私政策，无同意记录
  时非交互命令直接失败；pace 自身无遥测、无 ASN/地理定位。

## 终端演示

```text
$ pace run --targets nju-campus,cernet,tongji --no-save
pace 0.5.0 · success · 1m6.1s
Run 8b6eb628-6b17-4e5e-a4c1-2477666182fa
Network ens65f1 · other
TARGET             METHOD                   DOWNLOAD     UPLOAD       SERVER                STATUS
NJU Campus · IPv4  librespeed-three-stream  939.36 Mbps  138.46 Mbps  speed.nju.edu.cn      success
CERNET · IPv4      librespeed-three-stream  480.81 Mbps  507.98 Mbps  speedtest.sec.edu.cn  success
Tongji · IPv4      librespeed-three-stream  76.54 Mbps   56.97 Mbps   dev.tongji.edu.cn     success
```

（2026-10-04 在南大校内有线网络上的一次实测。三个目标走三条不同路径：
校内、教育网骨干、跨省教育网，数字差异说的是路径，不是 pace 的误差。）

## 快速开始

```sh
brew install --cask soundadam/tap/pace
pace doctor --json
pace
```

首次在交互终端运行会选择语言与日常测速站（macOS 默认预选
`nju-campus`、`mlab`、`apple`；Linux/Windows 预选 `nju-campus`、
`mlab`），之后可用 `pace setup` 修改。Homebrew cask 在 macOS
上安装；Linux/Windows 使用 release 二进制或源码构建，详见
[安装文档](docs/getting-started/installation.mdx)。

## 测速视角

| 目标 ID | 默认 | 它回答的问题 | 方法与结果边界 |
| --- | --- | --- | --- |
| `nju-campus` | 是 | 南大校内服务、校园网或 NJU VPN 是否连通 | LibreSpeed 三流；IPv4/IPv6 可分别选择 |
| `mlab` | 是 | 当前公网/代理出口的 bulk-transport 表现 | M-Lab NDT7 单流；节点由 Locate 动态返回 |
| `apple` | macOS 默认 | macOS 内置的公网吞吐与负载下响应性 | `/usr/bin/networkQuality -c -s`；显示吞吐、基础 RTT 与 RPM；Linux/Windows 自动跳过 |
| `ookla` | 否 | 附近 Ookla 测试服务器的运营商侧参考 | 仅官方 Speedtest CLI；动态服务器、ID、赞助方和地址写入结果 |
| `tongji` | 否 | 上海及江浙沪方向的教育网参考 | Tongji LibreSpeed 三流，IPv4 |
| `cernet` | 否 | CERNET 公共站点兼容性 | 当前服务不可达，保留显式诊断，不进入日常设置 |

中科大网页测速是浏览器产品，不加入 CLI 测速。教育网三流是三个并发
HTTP 请求，不是三条物理线路，也不表示服务器带宽异常大。NDT7 是单流
bulk-transport；两种方法的数值不应直接横向排名。

所有选中目标按显示顺序串行执行，避免互相争抢带宽。Ookla 不自动加入：
只有在 `pace setup` 中主动选择且官方 helper 可用时才运行。

## CLI 一览

```sh
pace
pace run [--targets LIST] [--family ipv4|ipv6|dual] [--label TEXT] [--note TEXT] [--no-save]
pace campus [--ipv4|--ipv6] [--label TEXT] [--note TEXT] [--no-save]
pace domestic [--targets tongji] [--family ipv4] [--no-save]
pace mlab | apple | ookla [--label TEXT] [--note TEXT] [--no-save]
pace stations [--json]
pace history [--limit N] | last [--json] | show RUN_ID [--json]
pace export --format jsonl|csv --output PATH
pace consent status|accept|revoke
pace setup | doctor [--json] | version
```

退出码稳定：`0` 全部成功，`1` 配置/环境错误，`2` 目标失败或部分成功，
`130` 取消。完整参数见 [CLI 命令参考](docs/reference/cli.mdx)。

## 文档

文档源文件在 [`docs/`](docs/)（Mintlify 格式），在 GitHub 上可以直接阅读：

- 科普：[网络测速在测什么](docs/concepts/what-speed-tests-measure.mdx) ·
  [为什么不同工具测出的数字不一样](docs/concepts/why-results-differ.mdx) ·
  [如何读懂 pace 的结果](docs/concepts/reading-results.mdx)
- 定位：[pace 的定位](docs/positioning/philosophy.mdx) ·
  [与其他工具对比](docs/positioning/comparison.mdx)
- 参考：[CLI](docs/reference/cli.mdx) ·
  [JSON 与自动化](docs/reference/json-and-automation.mdx) ·
  [M-Lab 隐私](docs/reference/mlab-privacy.mdx) ·
  [doctor 排障](docs/reference/doctor.mdx) ·
  [FAQ](docs/reference/faq.mdx)

## 安装与可选 provider（要点）

- 基础测量依赖 pace 发布的 LibreSpeed/ndt7 helper；缺少 helper 时
  `doctor` 明确报告，**不下载未知程序**。
- Apple `networkQuality` 是 macOS 自带组件，pace 只调用、不捆绑、
  不代用户接受条款。
- Ookla 仅支持**官方** Speedtest CLI，由用户自行安装
  （`brew tap teamookla/speedtest && brew install speedtest --force`）；
  pace 不自动安装、不自动传 `--accept-license`/`--accept-gdpr`，
  并拒绝已停维护的 Python `speedtest-cli`（可用
  `PACE_OOKLA_PATH` 指定官方二进制）。
- 交互式 `pace ookla` 检测到冲突时，仅在用户按 Enter 确认后执行
  官方 Homebrew 安装序列，且从不自动卸载已有 formula。

细节（冲突修复、helper 解析顺序、平台目录）见
[安装文档](docs/getting-started/installation.mdx)。

## M-Lab 隐私（要点）

M-Lab 会公开并无限期保留测试结果和 ISP 提供的公网 IP（[隐私政策](https://www.measurementlab.net/privacy/)）。
pace 要求显式同意且 fail closed：

```sh
pace consent accept
pace consent status
```

无同意记录时，非交互命令在接触 M-Lab 前以 `consent_required` 失败；
不含 M-Lab 的计划不需要这一步。详见
[M-Lab 隐私与同意](docs/reference/mlab-privacy.mdx)。

## JSON、历史与自动化（要点）

每次结果包含按执行顺序排列的 `targets` 与 `measurements`，可选字段
（`serverId`、`serverSponsor`、三类 RPM）缺省时保持 null/absent。历史
永久保存在用户配置目录（目录 `0700`、文件 `0600`、原子写入）：

```text
macOS:   ~/Library/Application Support/pace/history/v1/<run-id>.json
Linux:   ${XDG_CONFIG_HOME:-~/.config}/pace/history/v1/<run-id>.json
Windows: %AppData%\pace\history\v1\<run-id>.json
```

`export --format jsonl|csv` 导出全部历史。字段表与脚本示例见
[JSON 输出与自动化](docs/reference/json-and-automation.mdx)。

## 本地开发

```sh
make test-offline   # 全部离线测试（fixtures/mock helper）
make test-race
make build
./bin/pace version
```

测试、Formula 与 release automation 只使用 fixtures/mock helper，常规
CI 不执行真实带宽测试；真实测速由对应平台操作者单独验收。Makefile
目标拆分在 `make/*.mk`。流程详见 [TESTING.md](TESTING.md) 与
[RELEASE.md](RELEASE.md)。

## 许可与第三方

- pace 本体：**MIT**（见 [LICENSE](LICENSE)）。
- `librespeed-cli` helper：**LGPL-3.0-only**，维护源码在
  `components/librespeed-cli`，作为独立进程执行、不静态链接。
- `ndt7-client` helper：**Apache-2.0**，按固定版本与 SHA-256 构建。
- pace **从不代替用户接受任何第三方条款**（Apple、Ookla、M-Lab
  的条款由用户自己面对），也不读取或修改 soundVPN、SFM、NJUConnect
  的私有配置。

完整清单见 [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md)；产品契约
见 [SPEC.md](SPEC.md)。

## 命名约定

产品名、仓库名、Homebrew Formula、可执行文件、TUI 标题和 CLI 输出均
保持小写 `pace`。Go 模块路径保持 `github.com/soundadam/pace`。

## Contributing

欢迎 issue 与 PR。提交前请阅读 [SPEC.md](SPEC.md)（产品契约与表述
边界）和 [TESTING.md](TESTING.md)（验证门槛），运行
`make test-offline` 并保持 `git diff --check` 干净。涉及测量语义的
改动请先开 issue 讨论——本项目对表述准确性有洁癖。
