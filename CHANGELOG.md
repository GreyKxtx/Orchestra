# Changelog

All notable changes to Orchestra will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [v0.2.0] - 2025-01-11

### 🚀 Features
- **Smart Context:** Автоматический подбор файлов для контекста на основе поиска (`orchestra apply`).
- **Plan-First:** Режим `--plan-only` и обязательная генерация плана перед изменением кода.
- **Search Command:** Новая команда `orchestra search` с поддержкой regex и исключений.
- **Git Safety:**
  - Автоматическая проверка "чистоты" репозитория.
  - Создание коммитов после успешного применения изменений (`--git-commit`).
  - Защита от потери данных (создание `.orchestra.bak`).

### 🛠 Technical
- **Architecture:** Внедрение Dependency Injection для LLM клиента.
- **Testing:** Добавлены интеграционные тесты с Mock LLM (`tests/integration`).
- **Config:** Поддержка `.orchestra.yml` для настройки лимитов и исключений.

### 📝 Documentation
- Добавлен детальный чек-лист готовности v0.2 (`V0.2_CHECKLIST.md`).

---

## [v0.1.0] - 2024-12-XX

### 🚀 Initial Release
- Базовая функциональность применения изменений через LLM
- Парсинг ответов LLM в формате Orchestra
- Создание бэкапов файлов (`.orchestra.bak`)
- Dry-run режим для безопасного тестирования

