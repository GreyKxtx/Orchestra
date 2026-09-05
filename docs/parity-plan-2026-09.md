# Orchestra — план выхода на уровень лидеров (сентябрь 2026)

**Дата:** 2026-09-05 · **Срез кода:** `master` @ `cc41475` (18 коммитов поверх `34b5324`, все локальные) · **Продолжение:** `readiness-assessment-2026-09.md` — там оценка *что есть*; здесь — *чего не хватает до «сильно» (●●) по каждой строке матрицы* и в каком порядке это делать.

Легенда как в оценке: ●● сильно · ● есть · ◐ частично · ○ нет. «Лучший в классе» — публичное состояние конкурентов на 09/2026 (источники — приложение Б оценки). Трудоёмкость: **S** — день, **M** — до недели, **L** — больше недели.

Каждая строка «есть в коде» проверена на `cc41475` по конкретному файлу, а не по памяти о нём — после пяти ложных «открыто» в предыдущем документе это правило здесь главное.

---

## 0. Что сдвинулось после оценки

| Строка матрицы | Было (`34b5324`) | Стало (`cc41475`) | Чем |
|---|:-:|:-:|---|
| Долговременная авто-память | ◐ | ● | Заметка после каждого *изменившего* хода без участия модели (`working/promote.go`), `hybrid` ≠ `eager`, grep-шум убран, UTF-8 guard, уроки в single-agent, **статус памяти в чате и в `llm_log.jsonl`** (`cc41475`) |
| MCP | ◐ stdio | ● | Streamable HTTP через официальный SDK, bearer из env, тот же allowlist/restart (`99c8168`) |
| Конфиг | ● без глобального | ●● | `~/.orchestra/config.yml` под проектным с маскировкой при сохранении (`a194576`) |
| Дистрибуция | ○ | ● | `orchestra version`, релизный workflow на `v*` для 4 платформ (`4dd5b29`), `go install` в README |
| Лицензия | ○ | ● | `LICENSE` (MIT) в корне (`9342038`) |
| Prompt caching через шлюз | ◐ | ● | `cache_control` для `anthropic/*` через OpenRouter с самоотключением (`1cd9554`); **счётчики кэша теперь в `usage.jsonl` и в `orchestra usage`** (`3d4a68c`) |
| Язык LLM-поверхности | ◐ | ● | Все 58 схем инструментов на английском, тест на кириллицу (`b550e18`) |

Что сдвинулось с публикации плана:
- **A1 закрыт** — `orchestra init` создаёт `ORCHESTRA.md` из шаблона (`docs/examples/embed.go` + `internal/cli/init_orchestra_md.go`), заполняя «Language / runtime» реальным детектом (`provision.Detect`, не LSP-фолбэк), не трогая существующий файл, и добивая старые проекты при повторном `init`.
- **A2 закрыт** — слой `orchestra` в памяти (`memory/io.go`) и ленивая инъекция по вложенным директориям (`memory/inject.go` `LazyOrchestraFile`) читают `AGENTS.md`/`CLAUDE.md`/`.cursorrules`, если `ORCHESTRA.md` нет; порядок приоритета в самом имени списка. `List()`/`memory_read` теперь называют реальный файл, а не всегда «ORCHESTRA.md». `ensureOrchestraMD` (A1) заодно не создаёт пустой `ORCHESTRA.md` поверх уже существующего `AGENTS.md` — иначе пустой файл затенял бы рабочие инструкции по этому же приоритету.

Что *не* сдвинулось и осталось в списке ниже: MCP без OAuth/resources/prompts; провайдеры — по-прежнему два протокола; README и CLI на русском; хуки — только pre/post tool; промпты пока не просят модель писать в память (A3).

---

## 1. От ● к ●● — по строкам

### 1.1 Инструкции проекта (файл в репо, иерархия) — ● → ●●

**Есть в коде.** `ORCHESTRA.md` в корне читается целиком на каждом шаге (`internal/memory/io.go:19`); вложенные `ORCHESTRA.md` подхватываются лениво — при `fs.read` файла из этой директории, подъёмом вверх до корня (`inject.go:182-210`, кап `lazy_kb`); глобальная `~/.orchestra/memory.md` инжектируется как слой памяти. Шаблон есть — `docs/examples/ORCHESTRA.template.md`.

**Лучший в классе.** Claude Code: четыре уровня (`~/.claude/CLAUDE.md` → `CLAUDE.md` → `CLAUDE.local.md` → вложенные), `@path` импорты, `/init` генерирует файл из анализа репо, `/memory` открывает в редакторе. Gemini CLI: то же плюс `@import` и `/memory refresh`. Все они **читают чужие файлы** — `AGENTS.md` стал общим стандартом (OpenCode, Crush, Codex, Cursor).

| # | Чего не хватает | Где в коде | Эффект | Трудоёмкость |
|---|---|---|---|:-:|
| ~~1~~ | ~~**`orchestra init` не создаёт `ORCHESTRA.md`**~~ — закрыто | `cli/init.go` вызывает `ensureOrchestraMD` в обеих ветках (новый проект / повторный init); языки — `provision.Detect`, не `InitServerSpecs` (тот на пустом репо возвращает go+ts+py fallback, что соврало бы про стек) | Первый запуск на новом репо сразу даёт агенту контекст | S |
| ~~2~~ | ~~**Fallback на `AGENTS.md` / `CLAUDE.md` / `.cursorrules`**~~ — закрыто | `memory/io.go` `orchestraFallbackNames` + `readOrchestraFile`; `List()`/`Read()`/`LazyOrchestraFile` называют реальный файл, `discoverInstructions` в `internal/tools/runner.go` метит текст правильным путём (была бы метка «ORCHESTRA.md» для файла, который не существует — поймано тестом) | Большинство живых репо уже имеют инструкции для другого агента; Orchestra их больше не игнорирует | S |
| 3 | **`ORCHESTRA.local.md`** — личный, gitignored слой (аналог `CLAUDE.local.md`) | `io.go` + `ensureGitignore` в `cli/init.go` | «Мои» правила отдельно от командных | S |
| 4 | **`@import path`** внутри `ORCHESTRA.md` (относительно файла, глубина ≤3, защита от цикла) | `memory/io.go` перед `truncateToMax` | Один корневой файл + подключаемые `docs/*.md` вместо копипаста | M |
| 5 | **Пользовательские инструкции ≠ пользовательская память.** Сейчас `~/.orchestra/memory.md` — единственный глобальный слой, туда пишет и человек, и (потенциально) агент | Добавить `~/.orchestra/ORCHESTRA.md` как слой `orchestra-user` с тем же приоритетом, что глобальный конфиг | Разделение «как я хочу работать» и «что агент запомнил» | S |
| 6 | **Eager-подхват вложенных `ORCHESTRA.md` по файлам из запроса**, не только по `fs.read` — @-упоминание файла в TUI/VS Code сейчас не тянет правила его директории | `agent_prompt.go` шаг инъекции: пройтись по `Attachments` и `@`-ссылкам | Правила пакета видны до первого чтения, а не после | S |
| 7 | **`/memory` в TUI — только просмотр** (`app_memory.go:57` `cmdShowMemory` строит список слоёв) | Добавить `open` (в `$EDITOR`), `refresh`, и показать *что инжектировано в последний ход* с байтами по слоям (`Store.LastInjectReport`) | Наблюдаемость инструкций, а не только памяти | M |

**Итог для ●●:** пункты 1–3 и 7 — то, что отличает «есть файл» от «инструкции работают на любом репо с первого запуска». 4–6 — паритет по мелочам.

### 1.2 Агент сам пишет долговременную память — ● → ●●

**Есть в коде.** После каждого хода, где что-то *изменилось* (`done` в дайджесте), пишется заметка в `agent.md`: сводка модели, а при недоступной модели — rule-based из дайджеста (`core/auto_summary.go`, `working/promote.go`). `memory_write` со scope `project | session | <dept>` (`tools/session/registry.go:59`), `[pin]` переживает компакцию, `compactAgentFile` держит `agent.md` в размере (`memory/store.go:119`). Статус записи — в чате и в `llm_log.jsonl` (`cc41475`).

**Лучший в классе.** Claude Code Auto Memory: пишет **по умолчанию и по явной инструкции в промпте** («когда пользователь поправил тебя, назвал предпочтение, факт о проекте, который не выводится из кода»), заметки **типизированы** (user / feedback / project / reference), лежат в topic-файлах с **индексом `MEMORY.md`**, который загружается в каждую сессию, а сами файлы — по требованию; правило «обнови существующую, не дублируй». Пользовательская память **между проектами**. Cursor Memories — то же с UI ревью.

| # | Чего не хватает | Где в коде | Эффект | Трудоёмкость |
|---|---|---|---|:-:|
| ~~1~~ | ~~**Промпты молчат о памяти.**~~ — закрыто, со скорректированным охватом | Блок `MEMORY:` добавлен в `build.txt` + все 5 family-вариантов (`build-{anthropic,gpt,gemini,kimi,local}.txt`) и в `general.txt`. **`plan.txt` не тронут**: `listToolsPlan` (`registry.go:325`) не выдаёт `memory_write` этому режиму вовсе — учить модель звать несуществующий инструмент было бы хуже, чем ничего. Триггеры пока без типов `feedback`/`user` (тех типов ещё нет — это п.3): «поправка/предпочтение/решение → `scope=project`», «факт на сессию → `scope=session`», «не писать то, что уже в коде/ORCHESTRA.md» | Тест `TestMemoryGuidanceReachesModesThatHaveTheTool` + негативный тест на `plan` | S |
| ~~2~~ | ~~**Нет scope `global` у `memory_write`**~~ — закрыто | `memory/store.go` `Append` получил `case "global"` (уважает `GlobalEnabled`, не создаёт файл если выключено); раньше `scope=global` тихо падал в `default` и уходил в `agent.md` — то есть был не просто «нет фичи», а порча данных не в тот файл. Схема `memory_write` и блоки `MEMORY:` (из A3) теперь называют `scope=global` явно | Память о *пользователе* переезжает между проектами | S |
| 3 | **Заметки нетипизированы** — `agent.md` это append-лог с датой. При инъекции нельзя отдать приоритет `feedback` над `project`, нельзя показать «что агент думает о тебе» | Префикс `[type]` в записи (`user` / `feedback` / `project` / `reference`), парсер в `sliceRepoMemory`, порядок инъекции: feedback → user → project → reference, потом по свежести | Дороже всего терять именно feedback; сегодня он тонет среди файловых фактов | M |
| 4 | **Append вместо update.** Повтор одного факта — вторая запись; `compactAgentFile` режет по размеру, а не по смыслу | Перед `Append` scope=project: поиск ближайшей записи (`memory/semantic.go` уже умеет ранжировать чанки; без embed — Jaccard по токенам ≥0.8) → замена вместо дописывания | Память не растёт мусором; `agent.md` читаемый | M |
| 5 | **Индекс + topic-файлы.** Один `agent.md` под всё; у CC — `MEMORY.md` (индекс, всегда в контексте) + файлы по темам (по требованию через `memory_read`) | `.orchestra/memory/index.md` (одна строка на факт) инжектируется всегда; полный текст — `memory_read layer=repo file=…`; `hybrid` становится «индекс всегда, тела лениво» | Бюджет `inject_kb` уходит на *список* фактов, а не на первые N по хронологии | M |
| 6 | **Заметки из дайджеста не ревьюятся.** Rule-based записи («goal / done / files») полезны как след, но это не факты; сейчас они навсегда в `agent.md` | Дайджестовые заметки — в `sessions/<id>.md`, а в `agent.md` — по `/memory review` (TUI показывает кандидаты, `y/n/e`) или по правилу «файл трогался в ≥3 сессиях» | Разделение «журнал» и «знание»; человек в цикле там, где это дёшево | M |
| ~~7~~ | ~~**VS Code не показывает статус памяти**~~ — закрыто | `memoryNotice.ts` (портирован из `ui/tui/app_rpc.go noticeTurnMemory`, то же текст на русском — 1 в 1 с TUI) → `panel.ts` постит `systemNote`. Кэш заодно: `TurnUsagePayload.cached_prompt_tokens` + строка `cache NN%` в usage-пилюле (`07-events.js appendTurnCostNote`) | Паритет TUI ↔ VS Code по наблюдаемости | S |
| 8 | **Метрика качества памяти.** Есть событие `memory.note`, нет отчёта | `orchestra usage` (или новый `orchestra memory stats`): ходов с изменениями / заметок written / failed / доля digest vs model | Прогон становится измеримым числом, а не впечатлением | S |

**Итог для ●●:** пункт 1 — самый дешёвый и самый важный; 2–5 — структура, которая делает память полезной на длинной дистанции; 6–8 — контроль.

### 1.3 Индекс и поиск по памяти — ● → ●●

**Есть.** `memory_search`: substring по всем слоям + семантическое ранжирование при `embed.model` (последние 48 чанков, порог 0.15, `memory/semantic.go`); честный `degraded` при отказе embed (`fefffb2`).

| # | Чего не хватает | Трудоёмкость |
|---|---|:-:|
| 1 | Эмбеддинги **всей** памяти, а не последних 48 чанков — переиспользовать `embedindex` (`06bdfa9`) и `node_embeddings` в `ckg.db` с типом узла `memory` | M |
| 2 | Взвешивание по типу и свежести (см. 1.2 #3) — `feedback` свежее месяца выше, чем `project` полгода назад | S |
| 3 | `/memory search <q>` в TUI и палитре — сейчас поиск доступен только модели | S |

### 1.4 Сессионная память между ходами — ● → ●● (референс: OpenCode)

**Есть.** `.orchestra/sessions/*.json` (v4), `orchestra session list/export`, resume из TUI, авто-заголовок (`title.txt`), mid-turn snapshot каждые 5 с (`session_rpc.go:437`), rewind.

| # | Чего не хватает | Трудоёмкость |
|---|---|:-:|
| 1 | **Поиск по сессиям** (`orchestra session search`, `/sessions` с фильтром) — сейчас только список | M |
| 2 | **Fork/branch от сообщения** — «попробовать иначе с шага 7» без потери ветки | M |
| 3 | Share: экспорт в самодостаточный HTML (бандл есть — `session export`; нет читаемого вида) | S |
| 4 | SQLite вместо JSON-файлов — **не нужно**: 52 сессии на диске работают; менять формат ради паритета — трата. Пункт закрыт как «не делать» | — |

### 1.5 MCP — ● → ●●

**Есть.** stdio-клиент (handshake `2024-11-05`, `client.go:23`), Streamable HTTP через `modelcontextprotocol/go-sdk` v1.7 с bearer из env и защитой заголовка на редиректе (`remote.go`), `allowed_tools`, lazy restart ≤3, `list_changed`, `orchestra mcp list-tools`, диалог `/mcp` в TUI, пресеты. Не-текстовый контент в результатах **отбрасывается с пометкой** (`client.go:280`, `remote.go:252`).

**Лучший в классе (Claude Code, Cursor, Cline).** Три транспорта + **OAuth 2.1** (PKCE, dynamic client registration) для хостинговых серверов (GitHub, Linear, Sentry, Notion — все требуют OAuth, bearer из env их не покрывает); **`.mcp.json`** в корне проекта — один файл, который читают все агенты; `mcp add/remove/get` из CLI; **resources** как контекст (`@server:resource`), **prompts** как slash-команды; картинки в результатах → в историю (у Orchestra multimodal уже есть для browser-скриншотов); sampling/elicitation; marketplace (Cline).

| # | Чего не хватает | Где в коде | Эффект | Трудоёмкость |
|---|---|---|---|:-:|
| 1 | **`.mcp.json` совместимость** — читать `mcpServers` из корня проекта как дополнительный источник (`command/args/env/url/headers`), с `disabled` и пометкой источника в `/mcp` | `config.go` `validateMCP` + новый `mcpjson.go`; конфликты имён — `.orchestra.yml` побеждает | Сервер, настроенный для Claude Code или Cursor, работает в Orchestra без переписывания | S |
| 2 | **`orchestra mcp add <name> --command … / --url … --bearer-env …`, `remove`, `get`** — сейчас только `list-tools` (`cli/mcp.go:37`) | Пишет в `.orchestra.yml` (или `.mcp.json` по флагу), проверяет подключение | Установка сервера — одна команда, как у всех | S |
| 3 | **Картинки из результатов** → `ContentPart image` в tool-message при `multimodal` (сейчас `dropped N non-text`) | `Call` возвращает `string` → `[]llm.ContentPart`; интерфейс `ServerClient` расширить методом `CallRich`, старый `Call` — обёртка | Скриншоты от Playwright-MCP и любых visual-серверов доходят до модели | M |
| 4 | **Resources**: `resources/list` при старте, `memory_read layer=mcp:<server>`/`@server:uri` в TUI → содержимое как attachment | SDK уже даёт `ListResources/ReadResource`; stdio-клиент — дописать два метода | Данные сервера как контекст, не только как инструмент | M |
| 5 | **Prompts → slash-команды**: `prompts/list` → `/mcp:<server>:<prompt>` в палитре с аргументами | палитра TUI + `skill_invoke`-подобный путь | Серверные «рецепты» доступны человеку | M |
| 6 | **OAuth 2.1** для remote: discovery (`/.well-known/oauth-protected-resource`), PKCE, dynamic client registration, токены в `~/.orchestra/auth.json` (0600), refresh, `orchestra mcp login <name>` | `remote.go` `authTransport` — точка подключения; в SDK v1.7 уже есть пакет `oauthex` (resource metadata, token exchange) | Единственный путь к хостинговым серверам у поставщиков; без него «HTTP есть» — формально | L |
| 7 | **Handshake stdio на `2025-06-18`** (structured output, elicitation) — сейчас `2024-11-05` | `client.go:23` + обработка `structuredContent` | Совместимость с новыми серверами; старые продолжают работать через negotiation | S |
| 8 | **Orchestra как MCP-сервер**: `orchestra mcp serve` — `explore`, `symbols`, `semantic_search`, `runtime_query`, `lsp.diagnostics` наружу | `mcpsdk.NewServer` уже используется в `remote_test.go`; обернуть `tools.Runner` | **Уникальный рычаг**: CKG+OTel как источник для Claude Code/Cursor; ни у кого из конкурентов нет графа кода, который можно отдать другому агенту | M |
| 9 | Sampling/elicitation — сервер спрашивает модель или человека | через `QuestionAsker`/`llmClient` | Паритет; редкие серверы | M |

**Итог для ●●:** 1–3 (дни) закрывают повседневный разрыв; 6 — тот, что делает HTTP настоящим; 8 — то, где Orchestra может быть *впереди*, а не вровень.

### 1.6 Мультипровайдер — ● → ●●

**Есть.** Каталог 16 записей, 3 категории (`llm/catalog.go`), два протокола — OpenAI-compatible и Anthropic native (`llm/factory.go`); per-family промпты (`build-{anthropic,gemini,gpt,kimi,local}.txt`); каталог окон контекста для облачных моделей (`model_context.go`), `probe/ping`, retry, budget-инвариант vLLM, OpenRouter cost/credits, prompt caching native + gateway, `providers.<name>` + тиры, `orchestra auth set-key`, `orchestra model select/status`. Стоимость — только если задана таблица `pricing` в конфиге (`cli/usage_wire.go:24`); без неё `orchestra usage` показывает «—».

**Лучший в классе (OpenCode, Crush).** 75+ провайдеров через **models.dev** — один JSON с окном, ценой и флагами возможностей (vision / tools / reasoning / cache) на каждую модель; **OAuth для подписок** (Claude Pro/Max, ChatGPT Plus, GitHub Copilot) — главная причина популярности OpenCode; Bedrock / Vertex / Azure OpenAI для корпоративных; native Gemini; **fallback-цепочки** при недоступности.

| # | Чего не хватает | Где в коде | Эффект | Трудоёмкость |
|---|---|---|---|:-:|
| 1 | **Флаги возможностей в каталоге**: `vision`, `tools`, `json_schema`, `prompt_cache`, `reasoning`, `max_output` — сейчас это эвристики по имени модели, разбросанные по `factory.go`, `prompt_cache_gateway.go`, `response_format`, `multimodal` | `CatalogEntry` + `ModelInfo{ContextWindow, Pricing, Caps}`; источник — снапшот models.dev в `llm/models_data.json` (обновляется `go generate`) | Убирает угадывание; даёт цену и окно для *всех* моделей, не 20 захардкоженных | M |
| 2 | **Встроенная таблица цен** из того же снапшота → `orchestra usage` считает $ без ручного `pricing` | `usage.lookupPrice` fallback на `llm.ModelPricing(model)` | Сегодня стоимость видна только у OpenRouter; локалки — «—» правильно, а OpenAI/Anthropic direct — незаслуженно | S (после #1) |
| 3 | **Azure OpenAI** — `api-version`, deployment-name вместо model, ключ в `api-key` | `OpenAIClient` опции `azure: {deployment, api_version}` | Корпоративный блокер; изменений мало | S |
| 4 | **Native Gemini** (`generativelanguage.googleapis.com/v1beta/models/*:generateContent`): thinking budget, `cachedContents`, `toolConfig`, нативный streaming — через OpenAI-shim доступно лишь частично и только через `extra_body` | новый `llm/gemini.go` по образцу `anthropic.go` (~800 строк) | Gemini как first-class, а не «через шим» | L |
| 5 | **Fallback-провайдер**: `llm.fallback_provider: <name>` — при `IsUnreachableError` на шаге переключиться, пометить в usage | `agent_step.go` рядом с `llmInfraErr`; `router.go` | День 07.08 в демо (183 ошибки на мёртвый ngrok) прошёл бы на резерве | M |
| 6 | **Bedrock / Vertex** — SigV4 и GCP OAuth поверх Anthropic/Gemini клиентов | `anthropic.go` транспорт-обёртка (Bedrock использует тот же формат сообщений) | Корпоративные требования; объём — в аутентификации | L |
| 7 | **OAuth для подписочных провайдеров** (Claude Pro/Max, ChatGPT, Copilot) — device flow, токены в `~/.orchestra/auth.json` | `cli/auth.go` `login <provider>` | Дешёвая работа без API-ключей — то, за что любят OpenCode. **Риск:** ToS поставщиков меняются, некоторые прямо запрещают сторонние клиенты на подписке; делать как opt-in с предупреждением | L |
| 8 | **Reasoning-параметры**: Anthropic extended thinking (`thinking: {budget}`), OpenAI `reasoning_effort`, Responses API — сейчас есть только чтение reasoning-стрима Qwen/DeepSeek | `CompleteRequest.Reasoning{Effort, Budget}` → per-client маппинг | Управление мыслительным бюджетом Lead vs Worker; сегодня Planner на `claude-*` не может попросить thinking | M |
| 9 | **Авто-детект локального сервера** при `init`: опрос `:1234`, `:11434`, `:8000` → предзаполнить `api_base/model` | `cli/init.go` + `llm/probe.go` | Первый запуск без правки YAML | S |

**Итог для ●●:** 1+2 — фундамент (после них 3, 5, 8 почти бесплатны); 4 и 6 — про охват; 7 — про привлекательность, но с юридическим риском, решение за вами.

### 1.7 Hooks — ● → ●●

**Есть.** `hooks.enabled`, `pre_tool` (ненулевой код = запрет), `post_tool`, `timeout_ms`, env `ORCH_TOOL_NAME/INPUT/WORKSPACE_ROOT` (`config.go:391`, `hooks/hooks.go`).

**Лучший в классе (Claude Code).** События жизненного цикла: `SessionStart/End`, `UserPromptSubmit`, `PreToolUse/PostToolUse` с **matcher'ами по имени инструмента**, `PreCompact`, `Stop`, `Notification`; JSON на stdin, JSON-ответ с решением (`allow / deny / modify input` + причина для модели); хуки в `.claude/settings.json` на трёх уровнях.

| # | Чего не хватает | Трудоёмкость |
|---|---|:-:|
| 1 | Matcher по инструменту: `pre_tool: [{match: "bash|write", command: …}]` вместо одного глобального | S |
| 2 | JSON-протокол: stdin `{event, tool, input, session_id}`, stdout `{decision, reason, input?}` — reason уходит модели как denial hint (путь для этого есть — `formatPolicyDeniedCompact`) | M |
| 3 | События `session_start`, `user_prompt_submit` (может дописать контекст), `pre_compact`, `turn_end`, `permission_request` (внешний approver — CI, Slack) | M |
| 4 | Уровни: `~/.orchestra/config.yml` уже наследуется (`a194576`) — хуки туда попадают автоматически; проверить merge списков | S |

### 1.8 Skills / команды — ●● с оговорками

**Есть.** Skill packs, `$ARGUMENTS`, `@refs/`, `skill_invoke`, 12 YAML-workflow, палитра `Ctrl+K`, 17 slash-команд в TUI (`app_palette.go`).

| # | Чего не хватает | Трудоёмкость |
|---|---|:-:|
| 1 | **Skill → slash-команда**: `/skillname args` в TUI и VS Code (у CC skills = `/commands`) | S |
| 2 | Читать `.claude/commands/*.md` и `.claude/skills/*` как источник skills (та же frontmatter-модель) | S |
| 3 | Marketplace / plugin-реестр — **не сейчас**; закрыть как P2 без срока | — |

### 1.9 Поверхности — ● → ●●

| # | Чего не хватает | Трудоёмкость |
|---|---|:-:|
| 1 | VS Code: rich tool-блоки и reasoning-UI (в TUI есть, в webview — нет; честный пробел из оценки §5) | M |
| 2 | VS Code: статус памяти (см. 1.2 #7) и колонка кэша в usage-пилюле (`cached_prompt_tokens` уже в payload) | S |
| 3 | Desktop: в README заявлен, кода 0 (`ui/desktop` — 361 байт). **Решение нужно сейчас**: либо убрать из README, либо Wails/Tauri-обёртка над `orchestra core --http` (транспорт уже есть). Рекомендация — убрать до появления кода | S / L |
| 4 | Web-UI поверх `--http` — тот же вопрос; не раньше Desktop-решения | — |

### 1.10 Дистрибуция и лицензия — ● → ●●

**Есть.** `release.yml` (4 платформы, CGO на нативных раннерах), `orchestra version` с ldflags-штампом, `go install` в README, VSIX собирается (`package:marketplace` в `package.json`), `LICENSE` MIT.

| # | Чего не хватает | Трудоёмкость |
|---|---|:-:|
| 1 | **Лицензия — дописать:** в `LICENSE` держатель «Orchestra contributors» — поставить реальное имя/год; добавить `THIRD_PARTY_NOTICES.md` (генерация `go-licenses report` в `release.yml`) — бинарник статически линкует tree-sitter грамматики (MIT) и go-sdk (MIT), для релиза это обязательный файл; в матрице оценки строка «Лицензия: ○ файла нет» → ● | S |
| 2 | Install-скрипты: `install.sh` (curl \| sh, выбор платформы, checksum) и `install.ps1`; ссылки в README | S |
| 3 | Checksums + подпись релизов (`cosign` keyless) в `release.yml` | S |
| 4 | Homebrew tap / Scoop bucket / winget-манифест — генерировать из релиза | M |
| 5 | Публикация VSIX в Marketplace и Open VSX (сейчас только артефакт) | S |
| 6 | `orchestra version --check` (сравнить с latest release) | S |
| 7 | **English README** (русский — отрезает аудиторию независимо от качества кода; политика `docs/language-policy.md` про CLI-help остаётся вашим решением) | M |

### 1.11 Learning stack — ◐ → ●●

**Есть.** Lessons из worker-детей и (с `a8511eb`) из single-agent ходов с ошибками; L0–L3 с human-gate; `<dept_lessons>` в промптах.

| # | Чего не хватает | Трудоёмкость |
|---|---|:-:|
| 1 | Уроки → **предложение правила человеку** («3× StaleContent на `src/App.jsx` — добавить в ORCHESTRA.md: читать перед edit?») в чате, `y` дописывает файл; у CC/Gemini Auto Memory это и есть «предлагает skills» | M |
| 2 | Поставляемые L2-playbooks по умолчанию (`docs/examples/playbooks/` → реальные для go/ts/py) | S |
| 3 | Dept авто-детект из путей файлов для single-agent (сейчас `NormalizeDept("")` → общий) | S |

### 1.12 Оркестрация — ●● по дизайну, не доказано

Единственный пункт из оценки: **worktree-изоляция воркеров** (`git.worktree.*` есть, в spawn не подключено) — **L**; до прогона в orchestra-режиме не трогать.

---

## 2. План по волнам

Порядок — по отношению «эффект / трудоёмкость», с учётом того, что прогон на живой модели идёт параллельно и не должен ломаться.

### Волна A — до и во время прогона (дни, всё S)

| # | Задача | Строка | Почему первой |
|---|---|---|---|
| ~~A1~~ | ~~`orchestra init` создаёт `ORCHESTRA.md` из шаблона (+ `--dry-run`)~~ — сделано | 1.1 #1 | Закрыт |
| ~~A2~~ | ~~Fallback `AGENTS.md` / `CLAUDE.md` / `.cursorrules`~~ — сделано | 1.1 #2 | Закрыт |
| ~~A3~~ | ~~Блок `MEMORY:` в build/general промптах~~ — сделано (без `plan`: нет инструмента) | 1.2 #1 | Проверяется прогоном: `memory_write` был 0 из 91 |
| ~~A4~~ | ~~`memory_write scope=global`~~ — сделано | 1.2 #2 | Закрыт; заодно исправлена порча данных (см. п.2 выше) |
| ~~A5~~ | ~~VS Code: строка статуса памяти + кэш в usage-пилюле~~ — сделано | 1.2 #7, 1.9 #2 | `chat-src/07-events.js` — обычный JS без тестовой обвязки, как и остальной файл; изменение проверено вручную + typecheck/бандл |
| A6 | `orchestra memory stats` (или в `usage`): written / skipped / failed, digest vs model | 1.2 #8 | Метрика для прогона |
| A7 | Лицензия: имя держателя, `THIRD_PARTY_NOTICES.md` через `go-licenses` в `release.yml`; `install.sh` / `install.ps1`; checksums | 1.10 #1–3 | Закрывает «дистрибуция» до ● честно, не формально |
| A8 | `.mcp.json` чтение + `orchestra mcp add/remove/get` | 1.5 #1–2 | Повседневный MCP-разрыв; чистая конфигурация |
| A9 | Desktop: убрать из README (или завести issue с решением) | 1.9 #3 | Честность документации; 10 минут |

### Волна B — недели (M)

| # | Задача | Строка |
|---|---|---|
| B1 | Каталог возможностей моделей из снапшота models.dev + встроенные цены | 1.6 #1–2 |
| B2 | Azure OpenAI; fallback-провайдер; reasoning-параметры | 1.6 #3, #5, #8 |
| B3 | MCP: картинки в результатах; resources; prompts → slash; handshake `2025-06-18` | 1.5 #3–5, #7 |
| B4 | Память: типы записей + порядок инъекции; update-вместо-append; индекс + topic-файлы; `/memory review` | 1.2 #3–6 |
| B5 | Hooks: matcher'ы, JSON-протокол, lifecycle-события | 1.7 #1–3 |
| B6 | Skills как slash-команды; `.claude/commands` как источник | 1.8 #1–2 |
| B7 | `@import` в `ORCHESTRA.md`; `ORCHESTRA.local.md`; `/memory open/refresh` с отчётом инъекции | 1.1 #3–4, #7 |
| B8 | Уроки → предложение правила; дефолтные playbooks | 1.11 #1–2 |
| B9 | English README; Marketplace/Open VSX; brew/scoop/winget | 1.10 #4–5, #7 |

### Волна C — квартал (L)

| # | Задача | Строка | Заметка |
|---|---|---|---|
| C1 | MCP OAuth 2.1 (PKCE, DCR, `mcp login`) | 1.5 #6 | Делает HTTP-транспорт настоящим для хостинговых серверов |
| C2 | **Orchestra как MCP-сервер** (CKG/explore/semantic/runtime_query/LSP наружу) | 1.5 #8 | Единственный пункт плана, где Orchestra выходит *вперёд*, а не вровень; технически M, но нужен дизайн API |
| C3 | Native Gemini | 1.6 #4 | |
| C4 | Bedrock / Vertex | 1.6 #6 | По запросу корпоративного пользователя, не раньше |
| C5 | OAuth для подписочных провайдеров | 1.6 #7 | Opt-in, с предупреждением о ToS; решение за владельцем проекта |
| C6 | Worktree-изоляция воркеров | 1.12 | После прогона orchestra-режима |
| C7 | Поиск/fork по сессиям; VS Code rich tool-блоки | 1.4 #1–2, 1.9 #1 | |

---

## 3. Что прогон теперь покажет — и чем это читать

Прогон был бы бесполезен как измерение до `3d4a68c`/`cc41475`; теперь три числа снимаются с диска без ручного чтения файлов:

| Вопрос | Команда / файл | Что считать хорошим |
|---|---|---|
| Пишется ли память и откуда | `grep '"event":"memory.note"' .orchestra/llm_log.jsonl \| jq -r '.kind+" "+.source' \| sort \| uniq -c` | `written` ≈ число ходов с правками; `failed` = 0; доля `digest` показывает, сколько раз модель была недоступна или коротка |
| Модель сама пишет факты | `grep '"tool_name":"memory_write"' .orchestra/llm_log.jsonl \| wc -l` | В демо было 0/91. После A3 — хотя бы несколько на сессию с правками; 0 снова = промпт не сработал у этой модели |
| Кэш окупается | `orchestra usage --last N` → колонка **CACHED** | На Anthropic через OpenRouter на длинных ходах ≥60 %; 0 % при ≥3 шагах = маркеры отклонены (клиент сам выключится — см. `disablePromptCacheMarkers`) |
| Инструкции подхватились | `/memory` в TUI (после B7 — с байтами по слоям) | `ORCHESTRA.md` присутствует — сейчас в демо его нет, A1 это исправляет |

Если после прогона `memory_write` снова 0 при выполненном A3 — это уже свойство модели (Qwen 27B локально), и тогда ставка на rule-based путь (`digest`) была правильной, а B4 #6 (ревью кандидатов) — следующий шаг.

---

## Приложение. Проверенные факты на `cc41475`

- `internal/cli/init.go` — нет строки `ORCHESTRA` (grep по `internal/cli`); шаблон `docs/examples/ORCHESTRA.template.md` существует.
- `internal/` — нет `AGENTS.md` / `CLAUDE.md` / `.cursorrules` / `@import` (единственное `CLAUDE.md` — в комментарии `mcp/client.go:450`).
- `internal/prompt/files/*.txt` — `memory_write` упомянут только в `todowrite.txt:24`, `orchestra.txt:24`, `architecture.txt:11`; в `build*.txt`, `general.txt`, `plan*.txt` — нет.
- `tools/session/registry.go:59-84` — `memory_write` scope: project / session / dept; `memory_read` layer включает `global`.
- `memory/store.go:107-122` — append с timestamp, `compactAgentFile` только для project.
- `mcp/client.go:23` — `2024-11-05`; `mcp/client.go:280`, `remote.go:252` — не-текст отбрасывается с пометкой; `internal/` — нет `resources/`, `prompts/`, `oauth`, `.mcp.json`; `cli/mcp.go` — только `list-tools`; `mcpsdk.NewServer` — только в `remote_test.go`.
- `llm/factory.go` — два клиента; `llm/catalog.go` — 16 записей без флагов возможностей; `cli/usage_wire.go:24` — цены только из конфига; `anthropic.go` — нет `thinking`.
- `config.go:391-403` — хуки: `pre_tool`/`post_tool`/`timeout_ms`, без matcher'ов и событий.
- `app_palette.go` — 17 slash-команд; `app_memory.go:57` — `/memory` только просмотр; `cli/model.go` — `select`/`status`, без `list`.
- `.github/workflows/release.yml` — 4 платформы, без checksums/подписи/нотисов; `LICENSE` — «Orchestra contributors».
