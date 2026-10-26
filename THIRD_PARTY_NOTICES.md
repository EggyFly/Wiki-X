# 第三方组件与许可

本项目使用以下第三方组件. 完整许可证文本放在 `docs/licenses/`.

| 组件 | 版本 | 用途 | 许可证 |
|---|---|---|---|
| github.com/yuin/goldmark | v1.8.6 | Markdown 解析 | MIT |
| github.com/microcosm-cc/bluemonday | v1.0.27 | HTML 清洗 | BSD-3-Clause |
| github.com/kyokomi/emoji/v2 | v2.2.14 | emoji 短代码 | MIT |
| github.com/litao91/goldmark-mathjax | v0.0.0-20210217064022-a43cf739a50f | 数学公式解析 | MIT |
| Mermaid | 10.9.8 | 浏览器端图表渲染 | MIT |
| KaTeX | 0.19.1 | 浏览器端数学公式渲染 | MIT |

Mermaid 和 KaTeX 的浏览器 bundle 放在 `web/ui/`, 由 `cmd/frontend` 压缩并生成 hash 文件名.

许可证文本:

- `docs/licenses/mermaid.txt`
- `docs/licenses/katex.txt`
- `docs/licenses/bluemonday.txt`
- `docs/licenses/emoji.txt`
- `docs/licenses/goldmark-mathjax.txt`
