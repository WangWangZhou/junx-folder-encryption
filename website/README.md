# 文件夹隐藏工具 · 产品站

基于 Astro 5 + Cloudflare Workers 的产品文档站，版本与桌面应用同步为 v1.1.0。

站点与桌面应用同仓库但完全独立：

- 桌面应用前端：`frontend/`（Vite + Wails 3）
- 产品文档站：`website/`（Astro）

两者各有独立的 `package.json` 与依赖，Wails 构建只处理 `frontend/`，`website/` 的改动不会触发桌面应用重建。

## 命令

在 `website/` 目录下执行：

| 命令 | 作用 |
| --- | --- |
| `npm install` | 安装依赖 |
| `npm run dev` | 本地开发（http://localhost:4321） |
| `npm run build` | 构建到 `./dist/` |
| `npm run preview` | 本地预览构建产物 |
| `npm run deploy` | 部署到 Cloudflare Workers |

## 目录结构

- `src/pages/` — 页面路由（指南、进阶、更多）
- `src/components/` — 页头、页脚、文档布局组件
- `src/consts.ts` — 站点标题、说明与导航
- `public/screenshots/` — 软件截图（4 张）
- `astro.config.mjs` — Astro 配置（含 Cloudflare adapter）
- `wrangler.json` — Workers 部署配置

## 部署

沿用 Cloudflare Workers：在 Cloudflare Workers Builds 中把仓库根目录设为 `website/`，构建命令 `npm run build`，部署命令 `npm run deploy`。本地也可直接 `npm run build && npm run deploy`。

发布新版本时，同步更新 `src/consts.ts` 中的 `SITE_VERSION`、`src/pages/more/changelog.astro` 以及桌面应用的版本号。
