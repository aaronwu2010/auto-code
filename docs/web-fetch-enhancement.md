# WebFetch 能力增强设计文档

## 1. 问题

用户报告：auto-code 的 `WebFetch` 工具无法获取 https://docs.yugabyte.com/stable/reference/configuration/yugabyted/ 的完整内容，只返回了页面标题。

而我（Trae）的 WebFetch 工具能拿到 20.6KB 完整内容。

## 2. 根因分析

### 2.1 测试环境

| 工具 | URL | 拿到的内容 |
|------|-----|-----------|
| Trae WebFetch | Yugabyte yugabyted 页面 | **20.6KB 完整正文** |
| curl.exe (模拟 auto-code HTTP) | 同上 | **276KB 完整 HTML** (SSR) |
| auto-code WebFetch (预期) | 同上 | 51KB 截断 → htmlToMarkdown 后只剩头部 |

### 2.2 根因链

```
auto-code WebFetch:
  maxResponseSize = 51200 bytes  ← 太小！
       ↓
  Yugabyte 页面 HTML = 276KB
       ↓
  io.LimitReader 截断到 51KB
       ↓
  htmlToMarkdown() 只处理了前 51KB（只有 header + nav + 少部分正文）
       ↓
  返回的 markdown 内容不完整
       ↓
  用户看到"只有标题"
```

**核心问题不是 Yugabyte 页面是 SPA**——它是 Hugo SSR 渲染的，curl 能拿到完整 HTML。问题是 auto-code 的 **响应体截断太小（51KB）** + **markdown 转换质量差**（手写字符串替换 vs 真正的 HTML→Markdown 库）。

### 2.3 次要问题

| # | 问题 | 影响 |
|---|------|------|
| 1 | `maxResponseSize = 51200` 太小 | 任何 >50KB 的页面都会被截断 |
| 2 | `maxResultChars = 100000` 和 maxResponseSize 不匹配 | 即使 body 不截断，结果也会被二次截断 |
| 3 | `htmlToMarkdown()` 是手写字符串替换 | `<table>`、`<thead>`、`<tbody>`、`<img>`、`<a>` 的处理很粗糙 |
| 4 | 没有 SSR/CSR 自动检测 | 对 Next.js SPA 等 JS 渲染页面无能为力 |
| 5 | `removeNoisySections` 硬编码只移除 nav/header/footer/aside/noscript | 有些站点用 `<main>` 里藏 `<aside>`，有些用 `<script type="application/ld+json">` 不是 script 块 |

## 3. 设计方案

### 3.1 Step 1: 扩大响应体限制（紧急修复）

**改动**: [webfetch.go](file:///d:/auto-code/internal/tools/webfetch/webfetch.go)

```go
const (
-    maxResponseSize = 51200    // 50KB → 太小
+    maxResponseSize = 2097152  // 2MB → 覆盖 99% 的文档页面
     maxResultChars  = 100000   // 返回给模型的上限（token 限制）
+    maxResultChars  = 500000   // 500KB → 更大的正文
)
```

**权衡**: 2MB 响应体在 30s 超时内完全可行（平均带宽 ~70KB/s）。如果遇到超大页面（>2MB），用流式分块策略。

### 3.2 Step 2: 替换 htmlToMarkdown（质量提升）

当前的 `htmlToMarkdown()` 是 ~100 行手写字符串替换，对复杂页面处理很粗糙。

**方案 A**（推荐）: 引入 `github.com/JohannesKaufmann/html-to-markdown`

```bash
go get github.com/JohannesKaufmann/html-to-markdown
```

这是 Go 生态最成熟的 HTML→Markdown 转换库：
- 正确处理表格（`<table>` → markdown table）
- 正确处理图片（`<img>` → `![alt](src)`）
- 正确处理链接（保留 title 属性）
- 正确处理代码块（保留语言类名）
- 自动移除 script/style/nav/footer
- 可配置哪些标签忽略/保留

**替换点**: `htmlToMarkdown()` 函数整体替换为调用 `html_to_markdown.Convert()`.

**方案 B**（不推荐）: 引入 `github.com/charmbracelet/glamour`

glamour 是反过来的（Markdown→终端渲染），不能用于 HTML→Markdown。

### 3.3 Step 3: SSR/CSR 自动检测 + 降级策略（未来增强）

有些站点确实是 JS 渲染的（Next.js CSR、React SPA 等），curl 只能拿到空壳。需要检测 + 降级：

```
WebFetch.Call():
  1. HTTP GET → 拿到原始 HTML
  2. 检测 SSR 还是 CSR:
     - 检查 HTML 里是否有实质性 <p>/<h1>/<li>/<table> 等内容标签
     - 数一下 <body> 内的文本字符数 vs HTML 标签数
     - 如果文本字符 < 500 且 主要是 <div>/<script> → 判定为 CSR 空壳
  3. 如果是 SSR（有内容）→ htmlToMarkdown → 返回
  4. 如果是 CSR 空壳 → 返回 "页面需要 JavaScript 渲染，建议用 WebBrowser 工具手动打开"
     （或者在未来引入 headless Chrome）
```

**实现 headless Chrome 的方案**:

| 方案 | 依赖 | 优点 | 缺点 |
|------|------|------|------|
| **chromedp** | 无外部二进制（自动下载 Chrome） | Go 原生、轻量 | 需要 ~150MB Chrome 二进制 |
| **playwright-go** | 需要安装 playwright | 功能最全 | 重、复杂 |
| **系统 Chrome + CDP** | 假设用户有 Chrome | 零额外依赖 | 需要检测 Chrome 路径 |

**建议**: 先用 SSR/CSR 检测 + 友好提示。headless Chrome 作为 v2 增强。

### 3.4 已实现的优势（对比我自己的工具）

| 能力 | Trae WebFetch | auto-code WebFetch (增强后) |
|------|--------------|--------------------------|
| HTTP 直取 | ✅ | ✅ |
| HTML→Markdown | ✅ (html-to-markdown) | ✅ (step 2 引入) |
| 响应体大小 | 大 | 2MB (step 1 提升) |
| SSR 自动识别 | ✅ | ✅ (step 3 实现) |
| Headless 渲染 | ✅ (browser_use) | 🔜 v2 |
| 无外部依赖 | ✅ | ✅ (step 1-3 后) |

## 4. 实施计划

| Step | 改动 | 风险 | 优先级 |
|------|------|------|--------|
| 1 | 扩大 maxResponseSize 51KB→2MB, maxResultChars 100KB→500KB | 低（更多内存） | 🔴 紧急 |
| 2 | 引入 html-to-markdown 替换手写实现 | 中（需要新依赖、回归测试） | 🟡 高 |
| 3 | SSR/CSR 检测 + 降级提示 | 低 | 🟢 中 |
| 4 | headless Chrome (chromedp) | 中（需要下载 Chrome） | 🔵 v2 |

## 5. 验证方法

改完后测试以下 URL：

| URL | 类型 | 预期 |
|-----|------|------|
| https://docs.yugabyte.com/stable/reference/configuration/yugabyted/ | Hugo SSR | ✅ 完整正文 + 配置表格 |
| https://pkg.go.dev/net/http | Go Docs SSR | ✅ API 文档 + 函数签名 |
| https://react.dev/learn | Next.js SSR | ✅ React 教程内容 |
| https://www.google.com/search?q=test | CSR + JS 渲染 | ✅ 降级提示 |
| https://httpbin.org/html | 纯 HTML | ✅ 完整内容 |

## 6. 不做什么

- **不引入 headless Chrome**（v2 再说，当前 80% 的文档站是 SSR）
- **不引入反爬虫绕过**（遵守 robots.txt + 正常 User-Agent）
- **不并发爬取**（保持单请求、超时 30s）
