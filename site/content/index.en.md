---
title: pace
excerpt: Speed tests for campus networks, the education network and proxy exits, one result per path and no overall score.
weight: 30
presentation:
  category: Network measurement / CLI
  label: Install with Homebrew
  command: brew install --cask soundadam/tap/pace
  note: No score. No telemetry. A failure is recorded as a failure.
registry:
  github: soundadam/pace
  homebrew: soundadam/tap/pace
  docs: https://github.com/soundadam/pace#readme
release:
  version: "0.5.0"
  license: MIT
modules:
  - type: promo
    description: When the network is slow, the first question is which part is slow. Is the campus network or campus VPN up, how does the education network look, how fast is the proxy or public exit? {title} measures each of these separately and reports each on its own.
    line: "{title} {version} is an open-source {license} command line for macOS, Linux and Windows. The terminal view and `--json` output come from the same results."
    actions:
      - label: Docs (Chinese)
        url: "{docs}"
      - label: GitHub
        url: "https://github.com/{github}"

  - type: snapshot
    title: Why not a speed-test website
    body: A typical speed-test site picks the ISP server closest to you, measures the best-looking stretch of the path, and hands you one number. On an education network the trouble is usually elsewhere. A campus service is unreachable, the VPN dropped, the CERNET exit is congested, or the proxy exit is slow. {title} runs every selected target in order and lists the results side by side. It never substitutes, ranks or folds them into one score.
    note: Nanjing University's on-campus test uses `speed.nju.edu.cn`. The public CERNET test site is unreachable at the moment; {title} reports that as it is and does not swap in another site.
    metrics:
      - value: "6"
        label: "Vantage points: NJU campus, Tongji, CERNET, M-Lab, Apple, Ookla"
      - value: "0"
        label: Overall scores · telemetry
      - value: "4"
        label: "Stable exit codes: `0` `1` `2` `130`"
      - value: "{version}"
        label: "Current release · {license}"

  - type: section
    title: Campus, education network and proxy exit in one run
    index: 1/3
    lede: "`pace` opens the interactive view, and the first run asks which targets to test day to day. `nju-campus` measures the NJU campus network or campus VPN with three concurrent streams, IPv4 and IPv6 separately if you like. `tongji` is an education-network reference toward Shanghai and the Yangtze delta. `mlab` measures M-Lab from the current public exit, so with a proxy on, that exit is the proxy. Targets run one after another so they do not compete for bandwidth."

  - type: section
    title: A failure is recorded as a failure
    index: 2/3
    lede: A failed measurement records zero throughput and the stage where it stopped (`dns`, `connect`, `download`, `timeout`…). After Ctrl-C, targets that had not started are marked `skipped`. Every run is saved locally as JSON. `pace history` reads it back, `pace export` writes JSONL or CSV, and `--json` gives scripts and scheduled jobs something to parse.

  - type: section
    title: Privacy is your decision
    index: 3/3
    lede: M-Lab publishes test results and your public IP and keeps them indefinitely, so {title} measures M-Lab only after you run `pace consent accept`; without that record the run fails before contacting M-Lab. {title} itself uploads nothing and does no geolocation, and it does not accept Apple's or Ookla's terms for you. Ookla runs only through the official Speedtest CLI you install yourself.
---
