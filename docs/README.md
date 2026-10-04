# pace 文档站

本目录是 pace 的 [Mintlify](https://mintlify.com) 文档站源文件。
站点配置在 `docs.json`，页面为中文 MDX，按导航分组放在
`getting-started/`、`concepts/`、`positioning/`、`reference/` 子目录。

## 本地预览

Mintlify CLI 要求 Node.js v20.17.0 或更高版本。安装并启动本地预览：

```sh
npm i -g mint
cd docs
mint dev
```

预览地址为 <http://localhost:3000>，编辑 MDX 后实时刷新。常用选项：

```sh
mint dev --port 3333    # 自定义端口
mint dev --no-open      # 不自动打开浏览器
npx mint dev            # 不全局安装，一次性运行
```

如果本地渲染与线上不一致，先升级 CLI：

```sh
mint update
# 或
npm i -g mint@latest
```

`mint broken-links` 可以在提交前检查站内失效链接。

## 部署

Mintlify 通过 GitHub App 部署：在
[Mintlify dashboard](https://dashboard.mintlify.com) 创建项目、安装
GitHub App 并授权本仓库，把文档根目录指向 `docs/`。之后推送到默认分支
即自动构建发布。站点默认域名为 `<project>.mintlify.app`，自定义域名
（例如 `docs.soundadam.com`）在 dashboard 的 Settings → Custom Domain
中绑定。

文档内容的事实来源是仓库根目录的 `README.md` 与 `SPEC.md`；修改产品行为
的表述前请先核对这两个文件。
