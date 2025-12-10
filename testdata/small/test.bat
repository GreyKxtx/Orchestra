@echo off
REM Быстрый тест Orchestra
REM Убедись, что LM Studio запущен на http://10.5.0.2:1234

echo Testing Orchestra with LM Studio...
echo.

REM Dry-run тест
echo [1/2] Running dry-run test...
echo.
..\..\orchestra.exe apply "добавь функцию Subtract(a, b int) int в utils.go"
if %ERRORLEVEL% NEQ 0 (
    echo.
    echo Error occurred. Check the output above for details.
    echo The LLM might have returned invalid format or tried to replace non-existent code.
    pause
    exit /b %ERRORLEVEL%
)

echo.
echo [2/2] If you want to apply changes, run:
echo   orchestra apply --apply "добавь функцию Subtract(a, b int) int в utils.go"
echo.

pause

