# Архитектура Orchestra — целевая модель (UML)

Документ фиксирует **целевой** облик Orchestra — то, к чему ведёт
vNext-транзиция. Используется как «карта местности» для будущих фич
(TUI, кастомные агенты, Skills, GitHub-tools, multi-provider) и как
основа для код-ревью больших изменений.

Все диаграммы — Mermaid. Открываются в GitHub, VS Code, IntelliJ из коробки.

---

## 1. Контекст продукта (C4 — System Context)

Что такое Orchestra с точки зрения пользователя и внешних систем.

```mermaid
flowchart TB
    classDef user fill:#dff,stroke:#06c
    classDef orchestra fill:#fea,stroke:#a60,stroke-width:2px
    classDef ext fill:#eee,stroke:#666

    Dev["👤 Developer<br/>(CLI / IDE)"]:::user
    IDE["🧩 IDE plugin<br/>(VSCode / JetBrains)<br/>JSON-RPC client"]:::user
    Orch["🎼 Orchestra Core<br/>(Go binary)"]:::orchestra
    LLM["🤖 LLM Provider<br/>(OpenAI-compat:<br/>local LM Studio,<br/>vLLM, OpenRouter…)"]:::ext
    Repo["📁 Project repo<br/>(workspace)"]:::ext
    OTel["📡 OTel Collector<br/>(traces JSON)"]:::ext
    Git["🌿 git binary"]:::ext
    MCP["🔌 MCP servers"]:::ext

    Dev -- "CLI: apply / chat / search<br/>...etc" --> Orch
    IDE -- "JSON-RPC over stdio" --> Orch
    Orch -- "OpenAI-style<br/>chat.completions + tools" --> LLM
    Orch -- "atomic R/W<br/>under project_root" --> Repo
    Orch -- "ingest spans" --> OTel
    Orch -- "diff / commit / status" --> Git
    Orch -- "stdio JSON-RPC<br/>(tools)" --> MCP
```

---

## 2. Контейнеры (C4 — Containers)

Что развёрнуто и как. Главный поток — **stdio JSON-RPC**; всё остальное —
debug/legacy.

```mermaid
flowchart LR
    classDef supported fill:#cfc,stroke:#080
    classDef debug fill:#ffd,stroke:#aa0
    classDef legacy fill:#fdd,stroke:#a00

    subgraph User_side[" "]
        CLI["orchestra CLI<br/>(cobra)"]:::supported
        IDE["IDE plugin"]:::supported
    end

    subgraph Process[orchestra core process]
        RPC["JSON-RPC server<br/>internal/jsonrpc"]:::supported
        Core["Core<br/>internal/core"]:::supported
        HTTPDbg["HTTP debug endpoint<br/>(--http, 127.0.0.1)"]:::debug
        Daemon["HTTP daemon (v0.3)<br/>internal/daemon"]:::legacy
    end

    Filesystem[".orchestra/<br/>plan.json · diff.txt<br/>last_run.jsonl · ckg.db"]:::supported
    LLM["LLM Provider"]:::supported

    CLI -- "spawn + stdin/stdout" --> RPC
    IDE -- "stdio framing<br/>Content-Length" --> RPC
    RPC --> Core
    Core --> LLM
    Core --> Filesystem

    HTTPDbg -. "loopback + token<br/>(only with --http)" .-> Core
    Daemon -. "search/scan cache,<br/>used by orchestra search" .-> Filesystem
```

**Принцип:** stdio JSON-RPC — *единственный* поддерживаемый транспорт.
HTTP-debug и HTTP-daemon — служебные/унаследованные, без гарантий стабильности
протокола.

---

## 3. Внутренние компоненты (C4 — Components)

Как устроен `internal/core` и его соседи. Сгруппировано по слоям ответственности.

```mermaid
flowchart TB
    subgraph L1[Транспорт / RPC]
        JR["jsonrpc.Server<br/>(LSP framing)"]
        RPCH["core.RPCHandler<br/>методы: initialize,<br/>agent.run, session.*,<br/>tool.call"]
    end

    subgraph L2[Ядро]
        CORE["core.Core<br/>cfg · llm · tools · validator"]
        SESS["session store<br/>(per-project, in-memory)"]
        HEALTH["Health<br/>versions · project_id"]
    end

    subgraph L3[Domain — агент и инструменты]
        AG["agent.Agent<br/>цикл: prompt→LLM→tools→final"]
        CB["CircuitBreaker"]
        TR["tools.Runner<br/>Call(name, input)"]
        REG["tools.Registry<br/>ListToolsForMode"]
        PIPE["pipeline.Run<br/>Investigator→Coder→Critic"]
    end

    subgraph L4[Patches — слой намерения и применения]
        EXT["externalpatch<br/>(LLM-формат:<br/>search_replace,<br/>unified_diff,<br/>write_atomic)"]
        RES["resolver<br/>External→Internal"]
        OPS["ops<br/>(детерминированные:<br/>replace_range,<br/>write_atomic, mkdir_all)"]
        APPL["applier<br/>(atomic temp+rename<br/>+ *.orchestra.bak)"]
    end

    subgraph L5[Контекст / знание о коде]
        PRJ["projectfs<br/>(walk + exclude)"]
        SRCH["search<br/>(text/regex)"]
        SYM["code.symbols<br/>(tree-sitter)"]
        CKG["ckg<br/>(SQLite, Polyglot parser,<br/>FQN-edges)"]
        RT["runtime bridge<br/>(OTel ingest +<br/>span↔node link)"]
    end

    subgraph L6[Внешние интеграции]
        LLMc["llm.Client<br/>(OpenAI-compat)"]
        MCPc["mcp client"]
        HOOKS["hooks runner<br/>(pre/post tool)"]
        QA["QuestionAsker<br/>(stdin)"]
    end

    JR --> RPCH
    RPCH --> CORE
    CORE --> SESS
    CORE --> HEALTH
    CORE --> AG
    CORE --> PIPE
    AG --> CB
    AG --> TR
    AG --> REG
    AG --> EXT
    AG -- "при final" --> RES
    RES --> OPS
    TR -- "fs.apply_ops" --> APPL
    APPL --> OPS
    TR --> PRJ
    TR --> SRCH
    TR --> SYM
    TR --> CKG
    TR --> RT
    AG --> LLMc
    AG --> HOOKS
    AG --> QA
    TR --> MCPc
```

---

## 4. Доменная модель (Class-style)

Ключевые типы. Не один-в-один Go-структуры, а *логическая* схема —
что от чего зависит и какие у чего инварианты.

```mermaid
classDiagram
    class Core {
        +cfg Config
        +llm Client
        +tools Runner
        +validator Validator
        +Health() Health
        +AgentRun(params) Result
    }

    class RPCHandler {
        +Initialize(params) InitResult
        +AgentRun(params) Result
        +ToolCall(name, input) bytes
        +SessionStart() SessionID
        +SessionMessage(id, content) MsgResult
        +SessionCancel(id)
        +SessionClose(id)
        +SessionApplyPending(id) ApplyResp
    }

    class Session {
        +ID string
        +History []Message
        +PendingPatches []ExternalPatch
        +Todos []TodoItem
        +Mode string
    }

    class Agent {
        -llm Client
        -tools Runner
        -validator Validator
        -opts Options
        -todos []TodoItem
        -ckgContext string
        +Run(ctx, history, query) (history, Result, err)
    }

    class Options {
        +Mode build|plan|explore
        +Apply bool
        +AllowExec bool
        +ExecAllow []string
        +ExecDeny []string
        +MaxSteps int
        +MaxInvalidRetries int
        +MaxFinalFailures int
        +LLMStepTimeout duration
        +SubtaskRunner
        +HooksRunner
        +QuestionAsker
        +OnEvent callback
    }

    class CircuitBreaker {
        +RecordDenied(tool) error
        +RecordToolError(tool) error
        +RecordFinalFailure(err) error
        +ResetToolErrors()
    }

    class Runner {
        +project_root string
        +exclude_dirs []string
        +Call(name, input) bytes
        +FSApplyOps(req) Response
        +FetchCKGContext(ctx, query) string
    }

    class ToolDef {
        +Name string
        +Description string
        +Parameters JSONSchema
    }

    class ExternalPatch {
        <<interface>>
        +Path string
        +FileHash string
    }
    class FileSearchReplace {
        +Search string
        +Replace string
    }
    class FileUnifiedDiff {
        +Diff string
    }
    class FileWriteAtomic {
        +Content string
        +MustNotExist bool
    }

    class InternalOp {
        <<interface>>
        +Path string
        +Conditions FileHash
    }
    class ReplaceRange {
        +Start int
        +End int
        +Replacement string
        +BeforeAnchor string
        +AfterAnchor string
    }
    class WriteAtomic {
        +Content string
    }
    class MkdirAll {
        +Mode int
    }

    class Resolver {
        +ResolveExternalPatches(root, patches) []InternalOp
    }

    class Plan {
        +ProtocolVersion int
        +OpsVersion int
        +ToolsVersion int
        +Query string
        +GeneratedAtUnix int64
        +Ops []InternalOp
    }

    Core --> Agent
    Core --> Runner
    Core --> Session
    RPCHandler --> Core
    Session "1" o-- "*" ExternalPatch : pending
    Agent --> Options
    Agent --> CircuitBreaker
    Agent --> Runner
    Agent --> ExternalPatch : produces
    Agent --> Resolver : on final
    Resolver --> InternalOp : produces
    ExternalPatch <|.. FileSearchReplace
    ExternalPatch <|.. FileUnifiedDiff
    ExternalPatch <|.. FileWriteAtomic
    InternalOp <|.. ReplaceRange
    InternalOp <|.. WriteAtomic
    InternalOp <|.. MkdirAll
    Runner --> ToolDef : exposes
    Plan o-- InternalOp
```

**Инварианты, которые удерживает эта модель:**
- LLM никогда не видит и не порождает `InternalOp` — только `ExternalPatch`.
- Каждый `ExternalPatch` несёт `file_hash` версии, которую модель читала.
- `Resolver` перечитывает файл и заново вычисляет ranges + хэш →
  если файл сместился, мы получаем `StaleContent`, а не «применили в неверное место».
- Любой `InternalOp` несёт `Conditions.FileHash`, и applier перепроверяет
  его *прямо перед* записью.
- Запись = atomic (temp → fsync → rename) + опциональный `*.orchestra.bak`.

---

## 5. Состояния сессии (`session.*`)

```mermaid
stateDiagram-v2
    [*] --> NotInitialized
    NotInitialized --> Initialized : initialize{<br/>project_root,<br/>versions}
    Initialized --> SessionOpen : session.start
    SessionOpen --> Running : session.message
    Running --> SessionOpen : result (no patches)
    Running --> Pending : result (patches & !apply)
    Pending --> Applied : session.apply_pending
    Pending --> SessionOpen : new session.message<br/>(replaces pending)
    Running --> SessionOpen : session.cancel
    Applied --> SessionOpen
    SessionOpen --> Closed : session.close
    Initialized --> Closed : (process exit)
    Closed --> [*]
```

Ключевое:
- `pending` патчи живут до следующего `session.message` или явного `apply_pending`;
- `cancel` прерывает текущий ход, но сессию не закрывает;
- режим (`build`/`plan`) — атрибут *хода* (`session.message`), не сессии.

---

## 6. Жизненный цикл патча (sequence)

Полный путь от промта до диска — самый важный sequence, потому что
именно здесь живут гарантии безопасности.

```mermaid
sequenceDiagram
    autonumber
    participant LLM
    participant Ag as Agent
    participant V as Validator
    participant Res as Resolver
    participant FS as Filesystem
    participant App as Applier

    LLM->>Ag: {type:"final", final:{patches:[ext1, ext2]}}
    Ag->>V: schema check (PatchSet)
    V-->>Ag: ok
    loop for each ExternalPatch
        Ag->>Res: resolve(patch)
        Res->>FS: read(patch.path)
        FS-->>Res: bytes + sha256
        alt sha256 != patch.file_hash
            Res-->>Ag: StaleContent
            Ag->>LLM: "перечитай fs.read, обнови file_hash"<br/>(continue loop)
        else search ambiguous (multiple matches)
            Res-->>Ag: AmbiguousMatch
            Ag->>LLM: "уточни search-блок"
        else ok
            Res-->>Ag: InternalOp{<br/>replace_range,<br/>conditions{file_hash}}
        end
    end
    Ag->>App: FSApplyOps(ops, dryRun, backup)
    loop per op
        App->>FS: re-read & re-check file_hash
        alt mismatch
            App-->>Ag: StaleContent (recoverable)
        else
            App->>FS: write tmp
            App->>FS: fsync
            App->>FS: rename (atomic)
            opt backup && !dryRun
                App->>FS: copy original to *.orchestra.bak
            end
        end
    end
    App-->>Ag: ApplyResponse{diffs, changed_files}
    Ag-->>LLM: (loop ends, control to caller)
```

---

## 7. Слои (architecture layers)

Цель этой диаграммы — зафиксировать правило зависимостей: **внутренние слои
не знают о внешних**. Поломка этого правила = архитектурный долг.

```mermaid
flowchart TB
    classDef layer fill:#fff,stroke:#333

    subgraph IO[Слой 1 — I/O & транспорт]
        direction LR
        cli["cmd/orchestra<br/>internal/cli"]
        rpc["internal/jsonrpc"]
        http["http debug / daemon"]
    end

    subgraph App[Слой 2 — приложение]
        direction LR
        core["internal/core"]
        pipeline["internal/pipeline"]
    end

    subgraph Domain[Слой 3 — домен]
        direction LR
        agent["internal/agent"]
        tools["internal/tools"]
        prompt["internal/prompt"]
        schema["internal/schema"]
    end

    subgraph Patches[Слой 4 — патчи и ops]
        direction LR
        ext["internal/externalpatch"]
        res["internal/resolver"]
        ops["internal/ops"]
        apl["internal/applier"]
    end

    subgraph Knowledge[Слой 5 — знания о коде]
        direction LR
        ckg["internal/ckg"]
        srch["internal/search"]
        prsr["internal/parser"]
        prjfs["internal/projectfs"]
    end

    subgraph Foundation[Слой 6 — фундамент]
        direction LR
        cfg["internal/config"]
        store["internal/store"]
        proto["internal/protocol"]
        gitp["internal/git"]
        llmp["internal/llm"]
        mcpp["internal/mcp"]
        hooks["internal/hooks"]
    end

    IO --> App
    App --> Domain
    Domain --> Patches
    Domain --> Knowledge
    Patches --> Foundation
    Knowledge --> Foundation
    Domain --> Foundation
    App --> Foundation
```

**Правила:**
1. `internal/agent` не знает про `internal/cli` и `internal/jsonrpc`.
2. `internal/tools` не знает про `internal/agent` (только про `Runner`-API).
3. `internal/resolver` не знает про `internal/agent`.
4. `internal/ops` — *только* типы и валидация, никакой логики применения
   (это в `applier`).
5. `internal/protocol` — версии, коды ошибок, никаких зависимостей наружу.

---

## 8. Целевая схема расширений (то, чего пока нет, но к чему движемся)

```mermaid
flowchart LR
    classDef now fill:#cfc,stroke:#080
    classDef target fill:#dde,stroke:#44a,stroke-dasharray: 4 4

    subgraph Now[Сейчас]
        ag["Agent (build/plan/explore)"]:::now
        tools["Tools registry (built-in)"]:::now
        mcp["MCP client (есть, без CLI)"]:::now
    end

    subgraph Target[План]
        cust["Кастомные агенты в .orchestra.yml<br/>(role, prompt, permissions)"]:::target
        perm["Permission ruleset<br/>per tool · per glob<br/>(allow/ask/deny)"]:::target
        skills["Skills (per-project<br/>директива поведения)"]:::target
        plug["Plugins<br/>(внешний код, hooks)"]:::target
        web["webfetch / websearch tools"]:::target
        lsp["LSP integration<br/>(diagnostics, hover)"]:::target
        gh["GitHub tools<br/>(pr.create, issue.comment)"]:::target
        wt["worktree-first commands"]:::target
        tui["TUI (Bubble Tea)"]:::target
        mcpcli["mcp CLI<br/>(add/list/test)"]:::target
        compact["compaction agent<br/>(автосжатие истории)"]:::target
        title["title / summary agents<br/>(служебные)"]:::target
    end

    cust -->|configures| ag
    perm -->|gates| tools
    skills -->|inject prompt| ag
    plug -->|adds| tools
    plug -->|adds| hooksY[hooks pre/post]
    web -->|adds| tools
    lsp -->|adds| tools
    gh -->|adds| tools
    mcpcli -->|manages| mcp
    tui -->|drives| ag
    compact -->|reduces history| ag
```

Каждый блок справа — потенциальная отдельная sub-feature. Левая сторона
показывает точку расширения, в которую он встраивается. Это чек-лист для
будущих спецификаций в `docs/superpowers/specs/`.

---

## 8.1. Архитектурный контраст с OpenCode

Сравнительный анализ инструментов (детально — `docs/commands-and-modes.md`,
разделы 3.3–3.4) показывает, что главная архитектурная ставка отличается:

```mermaid
flowchart TB
    subgraph OpenCode["OpenCode (TS, Bun)"]
        oc_llm[LLM]
        oc_tool["edit.ts / write.ts /<br/>apply_patch.ts"]
        oc_disk[(Disk)]
        oc_lsp[LSP diagnostics]
        oc_llm -->|tool_call| oc_tool
        oc_tool -->|direct write| oc_disk
        oc_tool -->|after-edit feedback| oc_lsp
        oc_lsp -->|errors back| oc_llm
    end

    subgraph Orchestra["Orchestra (Go)"]
        or_llm[LLM]
        or_ext["External Patch<br/>(file.search_replace,<br/>file.unified_diff,<br/>file.write_atomic)"]
        or_res["Resolver<br/>(ranges + anchors +<br/>file_hash check)"]
        or_int["Internal Ops<br/>(replace_range,<br/>write_atomic, mkdir_all)"]
        or_app["Applier<br/>(atomic write +<br/>.orchestra.bak)"]
        or_disk[(Disk)]
        or_plan[(plan.json)]
        or_llm -->|final.patches| or_ext
        or_ext -->|resolve| or_res
        or_res -->|typed ops| or_int
        or_int -->|persist| or_plan
        or_int -->|apply| or_app
        or_app --> or_disk
        or_plan -.->|--from-plan replay| or_app
    end

    classDef oc fill:#fdd,stroke:#a44
    classDef or fill:#dfd,stroke:#4a4
    class oc_llm,oc_tool,oc_disk,oc_lsp oc
    class or_llm,or_ext,or_res,or_int,or_app,or_disk,or_plan or
```

**Ключевая разница**: у OpenCode инструмент = функция, которая решает всё
сразу (понять патч, вычислить ранжи, проверить permission, записать,
прогнать LSP). У нас инструмент возвращает **намерение** (External Patch),
дальше есть отдельные слои за валидацию, нормализацию и применение.

Что это даёт:

| Свойство | OpenCode | Orchestra |
|---|---|---|
| Replayable план без LLM | ❌ нет понятия плана | ✅ `plan.json` + `--from-plan` |
| Hash-условие на момент записи | ❌ только «должен был Read» | ✅ `conditions.file_hash` в каждой mutating op |
| Точка вставки policy/audit | внутри тула | `resolver` или `applier` |
| Forgiving edit | ✅ 9 fallback-стратегий | ❌ строгий, hard-fail |
| LSP feedback loop | ✅ | ❌ (но есть CKG) |

Это компромисс по дизайну: они оптимизируют **first-shot success rate**
(LLM с большей вероятностью попадёт), мы — **корректность и
аудитируемость** (если LLM не попал, провал явный и diagnostic-понятный).

Идеи на стыке (в `commands-and-modes.md` §3.4 «Топ-5 заимствований»)
позволяют забрать UX-выгоды OpenCode без слома нашего двухслойного
контракта.

---

## 9. Стрелки версий (контракт)

Изменение протокола ломает интеграции. Чтобы это было заметно — три
независимых счётчика, в `internal/protocol/version.go`:

```mermaid
flowchart LR
    PV["ProtocolVersion<br/>(JSON-RPC методы<br/>и их параметры)"] -->|bump together with| Doc["docs/PROTOCOL.md"]
    OV["OpsVersion<br/>(InternalOp shape)"] -->|bump together with| Apl["applier compatibility"]
    TV["ToolsVersion<br/>(набор + JSONSchema tools)"] -->|bump together with| Reg["tools/registry.go"]
    InitH["initialize handshake"] --> PV
    InitH --> OV
    InitH --> TV
    InitH -- "mismatch → hard fail" --> Err["jsonrpc error VersionMismatch"]
```

`initialize` — единственная точка, где клиент и сервер согласуют
все три версии. После — только совместимые вызовы; иначе — отказ.

---

## 10. Где смотреть код

| Концепт диаграммы | Файлы |
|---|---|
| RPC handler | `internal/core/rpc_handler.go` |
| Agent loop | `internal/agent/agent.go` |
| Tool registry | `internal/tools/registry.go` |
| Modes | `agent.go::ModeBuild/Plan/Explore`, `registry.go::ListToolsForMode` |
| External patches | `internal/externalpatch/` |
| Internal ops | `internal/ops/` |
| Resolver | `internal/resolver/` |
| Applier | `internal/applier/` |
| Pipeline | `internal/pipeline/pipeline.go` |
| CKG | `internal/ckg/` |
| Runtime bridge | `ckg.IngestTrace`, `internal/cli/runtime.go` |
| Versions | `internal/protocol/version.go` |
| Session methods | `internal/cli/chat.go`, `core/session.go` |
