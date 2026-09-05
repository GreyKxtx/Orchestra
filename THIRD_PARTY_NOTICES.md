# Third-party notices

The `orchestra` binary statically links the Go modules below. Orchestra
itself is MIT-licensed (see `LICENSE`); this file lists what else is
compiled in and under what license, generated from `go.mod` via
[`go-licenses`](https://github.com/google/go-licenses).

To regenerate:

```bash
go run github.com/google/go-licenses@latest csv ./cmd/orchestra
```

Two entries below have no `LICENSE` file in the module as fetched by Go
modules and are annotated instead of left blank: `go-localereader` is
mattn's, whose other modules here are all MIT, and its public repository
states MIT; `modernc.org/mathutil`'s `LICENSE` file (present in the module
cache but not picked up by the tool's heuristic) is BSD-3-Clause-style.

Regenerated for `v0.3.0`-era dependencies (2026-09-05); a dependency bump
can add or drop rows — see the regenerate command above.

| Module | License | Source |
|---|---|---|
| github.com/alecthomas/chroma/v2 | MIT | https://github.com/alecthomas/chroma/blob/v2.20.0/COPYING |
| github.com/atotto/clipboard | BSD-3-Clause | https://github.com/atotto/clipboard/blob/v0.1.4/LICENSE |
| github.com/aymanbagabas/go-osc52/v2 | MIT | https://github.com/aymanbagabas/go-osc52/blob/v2.0.1/LICENSE |
| github.com/aymanbagabas/go-udiff | BSD-3-Clause | https://github.com/aymanbagabas/go-udiff/blob/v0.3.1/LICENSE-BSD |
| github.com/aymerick/douceur | MIT | https://github.com/aymerick/douceur/blob/v0.2.0/LICENSE |
| github.com/charmbracelet/bubbles | MIT | https://github.com/charmbracelet/bubbles/blob/v1.0.0/LICENSE |
| github.com/charmbracelet/bubbletea | MIT | https://github.com/charmbracelet/bubbletea/blob/v1.3.10/LICENSE |
| github.com/charmbracelet/colorprofile | MIT | https://github.com/charmbracelet/colorprofile/blob/v0.4.1/LICENSE |
| github.com/charmbracelet/glamour | MIT | https://github.com/charmbracelet/glamour/blob/v1.0.0/LICENSE |
| github.com/charmbracelet/lipgloss | MIT | https://github.com/charmbracelet/lipgloss/blob/76690c660834/LICENSE |
| github.com/charmbracelet/x/ansi | MIT | https://github.com/charmbracelet/x/blob/ansi/v0.11.6/ansi/LICENSE |
| github.com/charmbracelet/x/cellbuf | MIT | https://github.com/charmbracelet/x/blob/cellbuf/v0.0.15/cellbuf/LICENSE |
| github.com/charmbracelet/x/exp/slice | MIT | https://github.com/charmbracelet/x/blob/2fdc97757edf/exp/slice/LICENSE |
| github.com/charmbracelet/x/term | MIT | https://github.com/charmbracelet/x/blob/term/v0.2.2/term/LICENSE |
| github.com/clipperhouse/displaywidth | MIT | https://github.com/clipperhouse/displaywidth/blob/v0.9.0/LICENSE |
| github.com/clipperhouse/stringish | MIT | https://github.com/clipperhouse/stringish/blob/v0.1.1/LICENSE |
| github.com/clipperhouse/uax29/v2/graphemes | MIT | https://github.com/clipperhouse/uax29/blob/v2.5.0/LICENSE |
| github.com/dlclark/regexp2 | MIT | https://github.com/dlclark/regexp2/blob/v1.11.5/LICENSE |
| github.com/dustin/go-humanize | MIT | https://github.com/dustin/go-humanize/blob/v1.0.1/LICENSE |
| github.com/erikgeiser/coninput | MIT | https://github.com/erikgeiser/coninput/blob/1c3628e74d0f/LICENSE |
| github.com/google/jsonschema-go/jsonschema | MIT | https://github.com/google/jsonschema-go/blob/v0.4.3/LICENSE |
| github.com/gorilla/css/scanner | BSD-3-Clause | https://github.com/gorilla/css/blob/v1.0.1/LICENSE |
| github.com/inconshreveable/mousetrap | Apache-2.0 | https://github.com/inconshreveable/mousetrap/blob/v1.1.0/LICENSE |
| github.com/lucasb-eyer/go-colorful | MIT | https://github.com/lucasb-eyer/go-colorful/blob/v1.3.0/LICENSE |
| github.com/mattn/go-isatty | MIT | https://github.com/mattn/go-isatty/blob/v0.0.20/LICENSE |
| github.com/mattn/go-localereader | MIT (per repository; no LICENSE file in the vendored module) | https://github.com/mattn/go-localereader |
| github.com/mattn/go-runewidth | MIT | https://github.com/mattn/go-runewidth/blob/v0.0.19/LICENSE |
| github.com/microcosm-cc/bluemonday | BSD-3-Clause | https://github.com/microcosm-cc/bluemonday/blob/v1.0.27/LICENSE.md |
| github.com/modelcontextprotocol/go-sdk | Apache-2.0 | https://github.com/modelcontextprotocol/go-sdk/blob/v1.7.0/LICENSE |
| github.com/muesli/ansi | MIT | https://github.com/muesli/ansi/blob/276c6243b2f6/LICENSE |
| github.com/muesli/cancelreader | MIT | https://github.com/muesli/cancelreader/blob/v0.2.2/LICENSE |
| github.com/muesli/reflow | MIT | https://github.com/muesli/reflow/blob/v0.3.0/LICENSE |
| github.com/muesli/termenv | MIT | https://github.com/muesli/termenv/blob/v0.16.0/LICENSE |
| github.com/ncruces/go-strftime | MIT | https://github.com/ncruces/go-strftime/blob/v1.0.0/LICENSE |
| github.com/remyoudompheng/bigfft | BSD-3-Clause | https://github.com/remyoudompheng/bigfft/blob/24d4a6f8daec/LICENSE |
| github.com/rivo/uniseg | MIT | https://github.com/rivo/uniseg/blob/v0.4.7/LICENSE.txt |
| github.com/sahilm/fuzzy | MIT | https://github.com/sahilm/fuzzy/blob/v0.1.2/LICENSE |
| github.com/segmentio/asm | MIT | https://github.com/segmentio/asm/blob/v1.1.3/LICENSE |
| github.com/segmentio/encoding | MIT | https://github.com/segmentio/encoding/blob/v0.5.4/LICENSE |
| github.com/smacker/go-tree-sitter | MIT | https://github.com/smacker/go-tree-sitter/blob/dd81d9e9be82/LICENSE |
| github.com/spf13/cobra | Apache-2.0 | https://github.com/spf13/cobra/blob/v1.8.0/LICENSE.txt |
| github.com/spf13/pflag | BSD-3-Clause | https://github.com/spf13/pflag/blob/v1.0.5/LICENSE |
| github.com/xeipuuv/gojsonpointer | Apache-2.0 | https://github.com/xeipuuv/gojsonpointer/blob/4e3ac2762d5f/LICENSE-APACHE-2.0.txt |
| github.com/xeipuuv/gojsonreference | Apache-2.0 | https://github.com/xeipuuv/gojsonreference/blob/bd5ef7bd5415/LICENSE-APACHE-2.0.txt |
| github.com/xeipuuv/gojsonschema | Apache-2.0 | https://github.com/xeipuuv/gojsonschema/blob/v1.2.0/LICENSE-APACHE-2.0.txt |
| github.com/xo/terminfo | MIT | https://github.com/xo/terminfo/blob/abceb7e1c41e/LICENSE |
| github.com/yosida95/uritemplate/v3 | BSD-3-Clause | https://github.com/yosida95/uritemplate/blob/v3.0.2/LICENSE |
| github.com/yuin/goldmark | MIT | https://github.com/yuin/goldmark/blob/v1.7.13/LICENSE |
| github.com/yuin/goldmark-emoji | MIT | https://github.com/yuin/goldmark-emoji/blob/v1.0.6/LICENSE |
| golang.org/x/net/html | BSD-3-Clause | https://cs.opensource.google/go/x/net/+/v0.53.0:LICENSE |
| golang.org/x/oauth2 | BSD-3-Clause | https://cs.opensource.google/go/x/oauth2/+/v0.35.0:LICENSE |
| golang.org/x/sync/errgroup | BSD-3-Clause | https://cs.opensource.google/go/x/sync/+/v0.20.0:LICENSE |
| golang.org/x/sys | BSD-3-Clause | https://cs.opensource.google/go/x/sys/+/v0.47.0:LICENSE |
| golang.org/x/term | BSD-3-Clause | https://cs.opensource.google/go/x/term/+/v0.42.0:LICENSE |
| golang.org/x/text | BSD-3-Clause | https://cs.opensource.google/go/x/text/+/v0.36.0:LICENSE |
| golang.org/x/time/rate | BSD-3-Clause | https://cs.opensource.google/go/x/time/+/v0.15.0:LICENSE |
| gopkg.in/yaml.v3 | MIT | https://github.com/go-yaml/yaml/blob/v3.0.1/LICENSE |
| modernc.org/libc | MIT | https://gitlab.com/cznic/libc/blob/v1.72.0/LICENSE-3RD-PARTY.md |
| modernc.org/mathutil | BSD-3-Clause-style (see note above) | https://gitlab.com/cznic/mathutil |
| modernc.org/memory | BSD-3-Clause | https://gitlab.com/cznic/memory/blob/v1.11.0/LICENSE-GO |
| modernc.org/sqlite | BSD-3-Clause | https://gitlab.com/cznic/sqlite/blob/v1.50.0/LICENSE |

All licenses above are permissive (MIT, BSD-3-Clause, Apache-2.0) — none
impose copyleft obligations on a statically-linked binary. `modernc.org/sqlite`
and `smacker/go-tree-sitter` are the two that matter most for the CGO-free,
license-free-to-redistribute story: both are BSD-3-Clause / MIT.
