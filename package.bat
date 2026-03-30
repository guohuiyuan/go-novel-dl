@echo off
setlocal

echo ========================================
echo Novel DL - Package Desktop (Windows)
echo ========================================

echo Preparing icon assets...
go run ./tools/iconprep || goto :error

echo Generating Windows resources...
del /Q cmd\novel-dl\*.syso 2>nul
go run github.com/tc-hib/go-winres@latest make --in winres/winres.json --out cmd/novel-dl/rsrc || goto :error

echo Building Go binaries...
go build -ldflags="-s -w" -o novel-dl.exe ./cmd/novel-dl || goto :error

echo Building Rust desktop app...
pushd desktop || goto :error
cargo build --release || goto :error
copy /Y target\release\novel-dl-desktop.exe ..\novel-dl-desktop.exe >nul || goto :error
popd

echo.
echo Build complete.
echo   novel-dl: novel-dl.exe
echo   Desktop: novel-dl-desktop.exe
goto :eof

:error
popd 2>nul
echo Packaging failed with exit code %errorlevel%.
exit /b %errorlevel%
