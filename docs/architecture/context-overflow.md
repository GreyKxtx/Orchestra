# Context overflow: как смотреть, упадёт ли, сравнение с Claude Code / OpenCode

Канон компакции: [`memory-context.md`](memory-context.md).  
Инвариант vLLM: `prompt_tokens + max_tokens ≤ max_model_len` (сервер **не** clamp’ит — отвечает **400**).

## Перезапуск

Свежий бинарь: `orchestra.exe` в корне репо. Старый TUI процесс нужно **закрыть и открыть заново** — иначе крутится старый код.

```powershell
.\orchestra.exe   # или ваш обычный способ запуска TUI
```

В `.orchestra.yml` для проверки удобно:

```yaml
llm:
  max_tokens: 8192          # completion budget
  extra_body:
    num_ctx: 51200          # = vLLM --max-model-len
agent:
  compact_threshold_pct: 60
```

---

## Что происходит при «больше чем контекст» (Orchestra сейчас)

Четыре линии защиты **до** падения:

| # | Где | Поведение | Упадёт? |
|---|-----|-----------|---------|
| 1 | **Agent compact** (`shouldCompactHistoryEx`) | Если оценка / прошлый `PromptTokens` + `max_tokens` не влезают в `num_ctx` → LLM-summary checkpoint или truncate | Нет (ход продолжается с ужатой историей) |
| 2 | **Wire clamp** (`maxTokensForRequest`) | Перед POST уменьшает `max_tokens`, чтобы `est(prompt)+max_tokens+2048 ≤ ctx` | Нет, если оценка не занижена |
| 3 | **Auto-retry 400** (`fixMaxTokensFromError`) | Парсит текст vLLM (старый и новый), ставит `max_tokens = room`, **один** повтор | Обычно нет; если prompt один уже ≥ окна — да |
| 4 | **Overflow recovery в Run-loop** (`recoverFromOverflow`) | Ловит 400/preflight-отказ, **учит** реальные `max_model_len` / `prompt_tokens` из ошибки, сжимает историю до token-бюджета и **повторяет тот же шаг** (до 2 раз, шаг не тратится) | Только если история уже не уменьшается |

Именно #4 — то, что делает OpenCode (`compact → rebuild request → retry step`). Ошибка контекста больше **не** завершает ход.

Дополнительно:

- **Калибровка** (`calibrateFromRealPrompt`): реальные `usage.prompt_tokens` (в т.ч. из streaming-`Done` и из текста 400) сравниваются с размером запроса в байтах → `bytes/token` подтягивается к настоящему токенайзеру (2…6), берётся более пессимистичное значение.
- **Окно от сервера** (`syncModelContextFromClient`): если клиент знает реальный `max_model_len` (из `/v1/models` или из 400), он перебивает `num_ctx` из `.orchestra.yml`.
- **Compaction сама не переполняется**: корпус для суммаризатора режется по 2000 символов на запись и по общему бюджету (`buildCompactionCorpus`), старые записи отбрасываются с маркером.

Падение в TUI (`✗ Error · request failed (status 400)…`) остаётся только если:

- история после сжатия **не уменьшилась** (окно забито system prompt + tool schemas);
- или исчерпан лимит `maxOverflowRecoveries` (2 цикла compact→retry за ход).

**Сеть / LM Studio выключен:** `dial tcp`, `connection refused`, `i/o timeout` на POST **не** запускают compaction. Ход сразу падает с `LLM Endpoint unreachable at <url>. Check if LM Studio / vLLM is running.`

UI: полный текст ошибки **переносится** по строкам (не обрезается `…`).

В чате при успешной компакции: notice **Info · Контекст сжат…** / `CONTEXT_COMPACTED`.

---

## Как руками проверить

### A. Нормальный длинный ход (ожидание: **не падает**)

1. Build-режим, длинный таск (много `read` / `grep` / больших tool results).
2. Смотри status / ctx bar: процент растёт.
3. Когда пересечётся порог → notice про сжатие контекста.
4. Ход идёт дальше; **не** должно быть 400 на каждом шаге.

Если видишь `CONTEXT_COMPACTED` — защита #1 сработала.

### B. Спровоцировать wire overflow (ожидание: **retry или compact, не hard-fail**)

1. Начни длинную сессию без `/compact`.
2. Если всё же пришёл 400 с текстом  
   `maximum context length is 51200… requested N output tokens…`  
   — в `.orchestra/llm_log.jsonl` (если включён лог) должен быть повтор с меньшим `max_tokens`.
3. В чате после фикса: **полный** текст ошибки (если retry не помог) + подсказка уменьшить `max_tokens` / компактнуть.

### C. Гарантированный fail (ожидание: **падает осознанно**)

- Prompt один уже > `num_ctx − 256` → клиент вернёт  
  `prompt too large (~X tokens) for model context Y — compact…`  
  или vLLM 400 без успешного retry.
- Лечение: `/compact`, `/clear`, новая сессия.

### D. Быстрые команды в TUI

| Команда | Зачем |
|---------|--------|
| `/compact` | Принудительно сжать LLM-историю сейчас |
| `/memory` | Посмотреть слои памяти |
| `/clear` | Новая core-сессия (история с нуля) |
| Status ctx % | Грубая оценка заполнения окна |

---

## Сравнение: Orchestra vs Claude Code vs OpenCode

Все три **не ждут**, пока провайдер «сам разберётся». Контекст — жёсткий лимит; клиент обязан ужать историю или completion.

| | **Orchestra** | **Claude Code** | **OpenCode** |
|--|---------------|-----------------|--------------|
| Когда сжимает | ~`compact_threshold_pct` от **prompt budget** (`ctx − max_tokens − safety`); плюс сразу, если last `PromptTokens` уже не оставляет room | ~**95%** окна (авто); часто советуют ручной `/compact` раньше | Preflight: estimate request vs limit − buffer (~20k) / output allowance |
| Как сжимает | Tool digests → LLM checkpoint → fallback truncate | LLM summary → новая «сессия» с summary как контекстом; `/compact [instructions]` | Prune tool tails → LLM structured summary + keep recent tokens |
| После overflow API | vLLM: **400** → clamp + retry на wire, затем **compact + повтор шага** в Run-loop | У Anthropic обычно другие ошибки; auto-compact до/около лимита | Overflow → compact → **rebuild request и retry step** |
| Ручной compact | `/compact` | `/compact` (+ фокус в инструкции) | Ручной / model-driven (зависит от версии) |
| UI scrollback | Полный чат **не** сжимается; сжимается только LLM history | Свой transcript / summary UX | Session parts + compaction checkpoint |
| Оценка размера | Байты/эвристика, **калибруемая** по real `usage.prompt_tokens` (2…6 bytes/token) | Свой tokenizer / usage API | JSON size ≈ 4 chars/token (документированная аппроксимация) |

### Вывод по «упадёт ли»

- **Claude Code / OpenCode**: цель продукта — **почти никогда не показывать raw context overflow** пользователю; сначала compact/prune, потом retry.
- **Orchestra (после правок)**: тот же идеал для **локального vLLM**: compact → clamp `max_tokens` → parse 400 → retry.  
  Сырой 400 в чате = крайний случай (окно уже забито prompt’ом) или баг оценки — тогда `/compact` или `/clear`.

---

## Код (куда смотреть)

| Кусок | Файл |
|-------|------|
| Формула vLLM + парсер overflow (`ParseContextOverflow`) | `llm/budget.go` |
| Clamp + retry 400 | `llm/client.go` |
| Когда компактить | `internal/agent/context_estimate.go` |
| Overflow → compact → retry шага, калибровка | `internal/agent/overflow.go` |
| Сам compact (+ бюджет корпуса) | `internal/agent/compact.go` |
| Бюджет байт истории | `config.EffectiveMaxPromptBytes` |
| Полные ошибки в TUI | `ui/tui/view/notice.go` |

---

## Чеклист «всё ок после пересборки»

1. Перезапущен новый `orchestra.exe`.
2. Длинный ход → появляется сжатие контекста, **без** серии 400.
3. Если 400 всё же пришёл — текст **читается целиком** (несколько строк).
4. После `/compact` ctx % падает, следующий шаг проходит.
