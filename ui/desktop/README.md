# Desktop App

Реализация — Этап 3 product roadmap, после VS Code extension.

**Стек: Tauri** поверх того же фронтенда `ui/web/`, который сегодня отдаёт
`orchestra web`. Именно поэтому этот фронтенд не использует API, которых нет в
системном webview (WebView2 / WebKitGTK / WKWebView): всё, что он делает, — это
`fetch`, `WebSocket` и cookie против локального ядра.

Работа разбита на три части:

- **A — мультипроектный реестр** (сделано): один процесс `orchestra web` держит
  несколько проектов, по ядру на каждый; `/api/projects` открывает, перечисляет
  и закрывает их, `/ws?project=<id>` подключает к конкретному; аутентификация
  через `HttpOnly`-cookie. Спека и план —
  `docs/superpowers/specs/2026-09-09-project-registry-design.md`,
  `docs/superpowers/plans/2026-09-09-project-registry.md`.
- **B — desktop-оболочка**: Tauri-окно, боковая панель сессий и выбор проекта,
  экраны настроек.
- **C — упаковка**: сборки под Windows/macOS/Linux, автообновление.

Полный контроль над UX, нативная встройка CKG-визуализации — цели частей B и C.
