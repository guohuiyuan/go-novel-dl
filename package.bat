@echo off
setlocal

echo ========================================
echo Novel DL - Package Desktop (Windows)
echo ========================================

echo Building Go binaries...
go build -ldflags="-s -w" -o novel-dl.exe ./cmd/novel-dl || goto :error
go build -ldflags="-s -w" -o novel-cli.exe ./cmd/novel-cli || goto :error

echo Building Rust desktop app...
pushd desktop || goto :error
cargo build --release || goto :error
copy /Y target\release\novel-dl-desktop.exe ..\novel-dl-desktop.exe >nul || goto :error
popd

echo.
echo Build complete.
echo   CLI: novel-dl.exe
echo   Compat CLI: novel-cli.exe
echo   Desktop: novel-dl-desktop.exe
goto :eof

:error
popd 2>nul
echo Packaging failed with exit code %errorlevel%.
exit /b %errorlevel%
