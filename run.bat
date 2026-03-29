@echo off
setlocal

echo ========================================
echo Novel DL - Build And Run (Windows)
echo ========================================

echo Checking Go environment...
go version || goto :error
go env -w GOPROXY=https://goproxy.cn,direct
go env -w GO111MODULE=on

echo Tidying modules...
go mod tidy || goto :error

echo Building novel-dl.exe...
go build -ldflags="-s -w" -o novel-dl.exe ./cmd/novel-dl || goto :error

echo.
echo Usage:
echo   1. Interactive novel-dl: novel-dl.exe
echo   2. Search by keyword: novel-dl.exe "三体"
echo   3. Start Web UI: novel-dl.exe web --no-browser
echo.

novel-dl.exe
goto :eof

:error
echo Build failed with exit code %errorlevel%.
exit /b %errorlevel%
