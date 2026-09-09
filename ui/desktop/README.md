# Orchestra Desktop

Tauri-оболочка поверх `orchestra web`: приложение запускает ядро как дочерний
процесс, открывает окно на его адрес и штатно гасит ядро при закрытии окна.
Внутри окна — тот же `ui/web`, что и в браузере; в оболочке нет ни IPC, ни
своего фронтенда. Спека: `docs/superpowers/specs/2026-09-09-desktop-shell-design.md`.

## Как это работает

1. Проект: аргумент командной строки → `last_project` из
   `~/.orchestra/desktop.json` → каждая запись `~/.orchestra/projects.json` по
   порядку → нативный диалог выбора папки (отмена — выход).
2. Ядро: `orchestra[.exe]` рядом с exe оболочки, иначе `orchestra` из `PATH`
   (dev-режим, о чём пишется в stderr). Запуск:
   `orchestra web --workspace-root <dir> --no-open --port 0 --init --announce`.
3. Ядро печатает одну JSON-строку (объект discovery) в stdout — оболочка берёт
   из неё `url` и `token` и открывает окно на `<url>/?token=…`; сервер ставит
   cookie и редиректит на `/`.
4. Закрытие окна: оболочка закрывает stdin ядра (сигнал завершения под
   `--announce`), ждёт до 3 с, затем kill.

## Сборка и запуск

Нужны Rust stable (rustup), на Windows — MSVC Build Tools и WebView2; на Linux —
пакеты из `.github/workflows/ci.yml` (job `desktop`).

```bash
# ядро рядом с оболочкой (иначе берётся из PATH)
node ui/desktop/scripts/build-sidecar.mjs
cd ui/desktop/src-tauri
cargo run -- /path/to/project      # или без аргумента
cargo test                         # boot.rs и sidecar.rs
```

## Части

- **A — реестр проектов** (сделано): несколько ядер в одном `orchestra web`,
  `/api/projects`, `/ws?project=`, cookie-аутентификация.
- **B — оболочка** (этот каталог): окно, sidecar, выбор проекта.
- **C — упаковка**: `bundle.externalBin`, инсталляторы, подпись, автообновление.
  До C `cargo build` собирает только оболочку; ядро кладётся рядом скриптом.

Переключатель проектов, список сессий по проектам и настройки живут в `ui/web`
и делаются там.
