# Orchestra: Стратегия тестирования

Детальные тест-кейсы и сценарии для MVP v0.1–v0.2.

---

## 1. Unit-тесты

### 1.1. Парсер ответа LLM

**Модуль:** `internal/parser/response.go`

**Тест-кейсы:**

1. **Корректный формат с одним файлом**
   ```go
   Input: `---FILE: path/to/file.go
   <<<BLOCK
   old code
   >>>BLOCK
   new code
   ---END`
   Expected: один FileChange с корректными полями
   ```

2. **Несколько файлов в одном ответе**
   ```go
   Input: несколько блоков ---FILE/---END
   Expected: массив FileChange, каждый с корректным путем
   ```

3. **Отсутствие маркеров ---FILE/---END**
   ```go
   Input: текст без маркеров
   Expected: ошибка парсинга, изменения не применяются
   ```

4. **Некорректное вложение блоков**
   ```go
   Input: ---FILE внутри другого ---FILE
   Expected: ошибка парсинга
   ```

5. **Пустые блоки <<<BLOCK/>>>BLOCK**
   ```go
   Input: <<<BLOCK>>>BLOCK (пусто)
   Expected: корректная обработка (старый блок пустой = replace_file)
   ```

6. **Отсутствие <<<BLOCK/>>>BLOCK (только новый код)**
   ```go
   Input: ---FILE: path
   new code
   ---END
   Expected: интерпретируется как replace_file
   ```

7. **Некорректные пути файлов**
   ```go
   Input: ---FILE: ../../etc/passwd
   Expected: валидация пути (относительно project_root)
   ```

8. **Специальные символы в коде**
   ```go
   Input: код с ```, ---, <<<, >>> в содержимом
   Expected: корректный парсинг (не путать с маркерами)
   ```

### 1.2. Модуль применения изменений

**Модуль:** `internal/applier/applier.go`

**Тест-кейсы:**

1. **Замена блока в середине файла**
   ```go
   File: `func a() { ... }
   func b() { old }
   func c() { ... }`
   Change: replace_block("func b() { old }", "func b() { new }")
   Expected: корректная замена только func b
   ```

2. **Замена блока в начале файла**
   ```go
   File: `package main
   import "fmt"
   func main() { ... }`
   Change: replace_block("package main", "package main\n\n// comment")
   Expected: замена с сохранением остального
   ```

3. **Замена блока в конце файла**
   ```go
   File: `... code ...
   func last() { old }`
   Change: replace_block("func last() { old }", "func last() { new }")
   Expected: корректная замена без потери данных
   ```

4. **Блок не найден (точный поиск)**
   ```go
   File: `func a() { code }`
   Change: replace_block("func b() { ... }", "new")
   Expected: ошибка "block not found", файл не изменяется
   ```

5. **Конфликтующие изменения в одном файле**
   ```go
   Changes: [
     replace_block("func a() { ... }", "new1"),  // строки 10-15
     replace_block("func b() { ... }", "new2")  // строки 12-17 (пересечение!)
   ]
   Expected: ошибка "conflicting changes", ничего не применяется
   ```

6. **Создание нового файла (replace_file)**
   ```go
   File: не существует
   Change: replace_file("new.go", "package main\n...")
   Expected: файл создается с содержимым
   ```

7. **Перезапись файла целиком (replace_file)**
   ```go
   File: `old content`
   Change: replace_file("file.go", "new content")
   Expected: файл полностью перезаписан
   ```

8. **Множественные изменения в разных файлах**
   ```go
   Changes: [
     FileChange{path: "a.go", ...},
     FileChange{path: "b.go", ...}
   ]
   Expected: оба файла изменены корректно
   ```

9. **Создание бэкапа при --apply**
   ```go
   File: exists
   Change: apply with --apply flag
   Expected: оригинал сохранен как file.go.orchestra.bak
   ```

10. **Dry-run не создает файлы**
    ```go
    Change: apply without --apply flag
    Expected: diff показан, файлы не изменены, бэкапы не созданы
    ```

### 1.3. Базовый поиск блоков в файлах

**Модуль:** `internal/search/block.go`

**Тест-кейсы:**

1. **Точное совпадение блока**
   ```go
   File: `func test() { code }`
   Search: "func test() { code }"
   Expected: найдено, возвращены координаты (start, end)
   ```

2. **Блок с разными пробелами**
   ```go
   File: `func test() { code }`
   Search: "func test(){\ncode\n}"
   Expected: нормализация пробелов перед сравнением
   ```

3. **Блок не найден**
   ```go
   File: `func a() { }`
   Search: "func b() { }"
   Expected: not found
   ```

4. **Несколько вхождений блока**
   ```go
   File: `func a() { x }
   func b() { y }
   func a() { z }`
   Search: "func a() { ... }"
   Expected: найдены оба вхождения, возвращается список
   ```

### 1.4. Валидация конфига

**Модуль:** `internal/config/config.go`

**Тест-кейсы:**

1. **Корректный конфиг**
   ```yaml
   project_root: .
   exclude_dirs: [.git, node_modules]
   llm:
     api_base: http://localhost:8000
     model: llama-3
     max_context_kb: 50
   ```
   Expected: валидация проходит

2. **Отсутствие обязательных полей**
   ```yaml
   project_root: .
   # нет llm.api_base
   ```
   Expected: ошибка валидации

3. **Некорректный лимит контекста**
   ```yaml
   llm:
     max_context_kb: -10  # отрицательное
   ```
   Expected: ошибка валидации

4. **Некорректный путь project_root**
   ```yaml
   project_root: /nonexistent/path
   ```
   Expected: предупреждение или ошибка (зависит от политики)

---

## 2. Интеграционные тесты

### 2.1. Mock LLM сервер

**Модуль:** `tests/mock_llm/server.go`

**Требования:**

* HTTP сервер, имитирующий OpenAI API (`/v1/chat/completions`)
* Возвращает предопределенные ответы из файлов `testdata/mock_responses/`
* Логирует входящие промпты для проверки

**Сценарии:**

1. **Успешный ответ с одним файлом**
   ```json
   POST /v1/chat/completions
   Response: {
     "choices": [{
       "message": {
         "content": "---FILE: main.go\n<<<BLOCK\nold\n>>>BLOCK\nnew\n---END"
       }
     }]
   }
   ```

2. **Ответ с несколькими файлами**
   ```json
   Response: {
     "choices": [{
       "message": {
         "content": "---FILE: a.go\n...\n---END\n---FILE: b.go\n...\n---END"
       }
     }]
   }
   ```

3. **Ошибка API (500)**
   ```json
   Response: 500 Internal Server Error
   Expected: инструмент показывает ошибку, файлы не изменены
   ```

4. **Таймаут запроса**
   ```go
   Server: задержка 60 секунд
   Client: timeout 30 секунд
   Expected: ошибка таймаута
   ```

5. **Некорректный формат ответа**
   ```json
   Response: {"invalid": "format"}
   Expected: ошибка парсинга, показывается сырое тело
   ```

### 2.2. Тестовые проекты

**Структура `testdata/`:**

```
testdata/
├── small/              # 5-10 файлов, простой Go проект
│   ├── main.go
│   ├── utils.go
│   └── .orchestra.yml
├── medium/             # 50-100 файлов, структурированный проект
│   ├── cmd/
│   ├── internal/
│   └── .orchestra.yml
└── mock_responses/     # Предопределенные ответы LLM
    ├── single_file.json
    ├── multiple_files.json
    └── invalid_format.json
```

**Тестовые сценарии:**

1. **Полный цикл: init → apply (dry-run) → apply (--apply)**
   ```bash
   cd testdata/small
   orchestra init
   orchestra apply "добавь функцию Add(a, b int) int"
   # Проверка: diff показан, файлы не изменены
   orchestra apply --apply "добавь функцию Add(a, b int) int"
   # Проверка: файлы изменены, бэкапы созданы
   ```

2. **Применение изменений в нескольких файлах**
   ```bash
   orchestra apply "добавь логирование в main.go и utils.go"
   # Проверка: оба файла изменены корректно
   ```

3. **Обработка ошибки парсинга**
   ```bash
   # Mock LLM возвращает некорректный формат
   orchestra apply "test"
   # Проверка: ошибка показана, файлы не изменены
   ```

4. **Проверка лимита контекста**
   ```bash
   # Проект с 100 файлами, лимит 50 KB
   orchestra apply "test"
   # Проверка: в промпт попало ограниченное количество файлов
   ```

### 2.3. Интеграция поиска (v0.2)

**Сценарии:**

1. **Автоподбор контекста по ключевым словам**
   ```bash
   orchestra apply "добавь обработчик login"
   # Проверка: файлы с "login" в названии/содержимом включены в промпт
   ```

2. **Команда search**
   ```bash
   orchestra search "login handler"
   # Проверка: список файлов с совпадениями
   ```

3. **План изменений (--plan-only)**
   ```bash
   orchestra apply --plan-only "рефакторинг auth"
   # Проверка: показан план, код не сгенерирован
   ```

---

## 3. E2E тесты

### 3.1. Реальные сценарии

1. **Инициализация проекта**
   ```bash
   mkdir test_project && cd test_project
   orchestra init
   # Проверка: .orchestra.yml создан с корректными значениями по умолчанию
   ```

2. **Применение изменений на реальном коде**
   ```bash
   # В реальном Go проекте
   orchestra apply "добавь функцию Sum для слайса int"
   # Проверка: функция добавлена, код компилируется
   ```

3. **Проверка бэкапов**
   ```bash
   orchestra apply --apply "test"
   # Проверка: .orchestra.bak файлы созданы, можно восстановить
   ```

4. **Git-интеграция (v0.2)**
   ```bash
   # В git репозитории с незакоммиченными изменениями
   orchestra apply "test"
   # Проверка: предупреждение показано
   
   orchestra apply --apply --git-commit "test"
   # Проверка: коммит создан с сообщением feat(orchestra): ...
   ```

### 3.2. Производительность

1. **Сканирование большого проекта**
   ```bash
   # Проект с 10k файлов
   time orchestra apply "test" --debug
   # Проверка: сканирование укладывается в несколько секунд
   ```

2. **Повторные запуски (v0.3 с daemon)**
   ```bash
   orchestra daemon &
   time orchestra apply "test"  # первый запуск
   time orchestra apply "test"  # второй запуск
   # Проверка: второй запуск заметно быстрее
   ```

---

## 4. Кросс-платформенное тестирование

### 4.1. Linux

* Проверка путей (`/home/user/project`)
* Разрешения файлов
* Обработка симлинков

### 4.2. Windows

* Проверка путей (`C:\Users\user\project`)
* Обработка `\r\n` vs `\n`
* Длинные пути (>260 символов, если актуально)

### 4.3. CI/CD

* Автоматический запуск на обеих платформах
* Тесты в GitHub Actions / GitLab CI

---

## 5. Структура тестов в проекте

```
orchestra/
├── cmd/
├── internal/
│   ├── parser/
│   │   └── parser_test.go        # Unit тесты парсера
│   ├── applier/
│   │   └── applier_test.go       # Unit тесты applier
│   ├── search/
│   │   └── block_test.go         # Unit тесты поиска
│   └── config/
│       └── config_test.go        # Unit тесты конфига
├── testdata/
│   ├── small/                    # Маленький тестовый проект
│   ├── medium/                   # Средний тестовый проект
│   └── mock_responses/           # Предопределенные ответы LLM
└── tests/
    ├── integration/
    │   ├── mock_llm/             # Mock LLM сервер
    │   └── integration_test.go   # Интеграционные тесты
    └── e2e/
        └── e2e_test.go           # E2E тесты
```

---

## 6. Критерии покрытия

**Для v0.1 (минимальные требования):**

* Unit-тесты: парсер (80%+), applier (80%+)
* Интеграционные: хотя бы один полный цикл `init → apply`

**Для v0.2 (рекомендуемые):**

* Unit-тесты: все модули (70%+)
* Интеграционные: все основные сценарии
* E2E: базовые сценарии на реальных проектах

**Для v0.3 (желательно):**

* Полное покрытие критических путей (90%+)
* E2E на разных платформах
* Тесты производительности

