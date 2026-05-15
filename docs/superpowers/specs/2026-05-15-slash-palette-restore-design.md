# Дизайн: восстановление инлайн слэш-палитры

**Дата**: 2026-05-15  
**Статус**: одобрен  

## Проблема

В `ui/tui/app_palette.go::syncPalette()` явно установлен `a.paletteActive = false` (комментарий «legacy inline slash palette is now disabled»). Из-за этого ввод `/` в строке ввода не открывает список команд.

## Цель

При вводе `/` в строке чата немедленно показывать инлайн-палитру команд над полем ввода. Фильтрация по мере набора. Ctrl+K модальная палитра остаётся нетронутой.

## Архитектура

### Что существует и работает (не трогаем)

| Компонент | Файл | Роль |
|-----------|------|------|
| `SlashPalette` | `ui/tui/view/palette.go` | Рендеринг, Filter(), CursorUp/Down, Selected() |
| Рендер палитры | `ui/tui/app_view.go:View()` | Вставляет палитру при `paletteActive == true` |
| Layout | `ui/tui/app_view.go:layout()` | Резервирует строки высоты под палитру |
| Навигация | `ui/tui/app_update.go:routeKey()` | ↑↓ перемещают курсор, Enter выполняет, Esc закрывает |
| Выполнение | `ui/tui/app_palette.go:executePaletteCmd()` | Запускает выбранную команду |
| Ctrl+K модаль | `ui/tui/view/palette_modal.go` | Центрированный оверлей с поиском — остаётся |

### Единственное изменение

**Файл**: `ui/tui/app_palette.go`  
**Метод**: `syncPalette()`

```go
// Было:
func (a *App) syncPalette() {
    a.paletteActive = false
    a.syncMention()
}

// Станет:
func (a *App) syncPalette() {
    val := a.input.Value()
    if strings.HasPrefix(val, "/") && !strings.Contains(val, " ") {
        query := val[1:]
        a.slashPalette.Filter(query)
        a.paletteActive = len(a.slashPalette.Items) > 0
    } else {
        a.paletteActive = false
    }
    a.syncMention()
}
```

## Поведение

| Ввод пользователя | Палитра |
|-------------------|---------|
| `/` | открыта, показывает ВСЕ команды из `AllSlashCmds` |
| `/cl` | фильтрует: только команды со `cl` в имени → `/clear` |
| `/help ` (пробел после слова) | закрывается — пробел = конец команды, начало текста |
| Esc | палитра закрывается, поле ввода сбрасывается |
| Enter при активной палитре | выполняется `Selected()`, ввод сбрасывается |
| ↑↓ | навигация по пунктам |
| Ctrl+K | по-прежнему открывает центральный модальный оверлей |

## Визуал

Стиль остаётся текущим (`SlashPalette.Render()`):
- SplitBorder — толстые `┃` по бокам, цвет `Primary`
- Фон `BackgroundSecondary`
- Выделение = полная заливка `Primary`, текст `Background`
- Колонки: `cmd` (bold) выровнено по ширине + `desc` (muted)
- Показывается сразу над `renderChatInputBox()`

## Тестирование

- `ui/tui/view/palette_test.go` уже покрывает `SlashPalette.Filter()`, `Render()`, навигацию.
- Добавить 3 кейса в `ui/tui/app_test.go`:
  1. ввод `/` → `paletteActive == true`, все команды показаны
  2. ввод `/cl` → отфильтрован только `/clear`
  3. ввод `/clear ` (с пробелом) → `paletteActive == false`

## Ограничения / не в скопе

- Слэш-палитра не активируется в середине текста (`hello /cl` — не триггерит, нужен `/` в начале).
- `@`-упоминания и Ctrl+K модаль не затронуты.
- Никакого авто-выбора при единственном совпадении — пользователь всегда нажимает Enter явно.
