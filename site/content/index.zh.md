---
title: pace
excerpt: 给校园网、教育网和代理出口分别测速的命令行工具，每条路径单独出结果，不打综合分。
weight: 30
presentation:
  category: 网络测速 / CLI
  label: 用 Homebrew 安装
  command: brew install --cask soundadam/tap/pace
  note: 不打分。没有遥测。失败就记失败。
registry:
  github: soundadam/pace
  homebrew: soundadam/tap/pace
  docs: https://github.com/soundadam/pace#readme
release:
  version: "0.5.0"
  license: MIT
modules:
  - type: promo
    description: 网慢的时候，先要知道慢在哪一段：校园网和校园 VPN 通不通，教育网方向怎么样，代理或公网出口跑得多快。{title} 把这几段分开测，各出各的结果。
    line: "{title} {version} 是 {license} 开源的命令行工具，支持 macOS、Linux 和 Windows，终端界面和 `--json` 输出用的是同一套结果。"
    actions:
      - label: 文档
        url: "{docs}"
      - label: GitHub
        url: "https://github.com/{github}"

  - type: snapshot
    title: 为什么不用一个测速网站
    body: 常见的测速网站挑离你最近的运营商服务器，测出来的是最好看的那一段路，再给你一个数字。教育网用户的问题往往在别处：校内服务连不上、VPN 掉了、CERNET 出口拥堵，或者代理出口慢。{title} 按顺序测每一个选中的目标，结果并排列出，从不互相替换、排名或折算成一个分数。
    note: 南大校内测速用 `speed.nju.edu.cn`。CERNET 公共测速站目前不可达，{title} 照实报告，不拿别的站点顶替。
    metrics:
      - value: "6"
        label: 个测速视角：南大校园网、同济、CERNET、M-Lab、Apple、Ookla
      - value: "0"
        label: 综合分数 · 遥测
      - value: "4"
        label: "个稳定退出码：`0` `1` `2` `130`"
      - value: "{version}"
        label: "当前版本 · {license}"

  - type: section
    title: 校园网、教育网、代理出口，一次测完
    index: 1/3
    lede: "`pace` 打开交互界面，第一次运行时挑好日常测速站。`nju-campus` 用三条并发流测南大校园网或校园 VPN，IPv4 和 IPv6 可以分开测；`tongji` 是江浙沪方向的教育网参考；`mlab` 从当前公网出口测 M-Lab，开着代理测到的就是代理出口。目标串行执行，互不抢带宽。"

  - type: section
    title: 失败就记失败
    index: 2/3
    lede: 测量失败时记零速率，并标明卡在哪一步（`dns`、`connect`、`download`、`timeout`…）；按 Ctrl-C 取消后，还没开始的目标记为 `skipped`。每次结果都存成本地 JSON，`pace history` 回看，`pace export` 导出 JSONL 或 CSV，`--json` 方便脚本和定时任务直接读。

  - type: section
    title: 隐私由你决定
    index: 3/3
    lede: M-Lab 会公开并长期保留测试结果和你的公网 IP，所以 {title} 要你先运行 `pace consent accept` 才会测 M-Lab，没有同意记录就直接失败。{title} 自己不上传任何数据，不做地理定位，也不替你接受 Apple、Ookla 的条款；Ookla 只调用你自己装的官方 Speedtest CLI。
---
