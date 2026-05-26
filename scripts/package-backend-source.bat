@echo off
setlocal EnableExtensions EnableDelayedExpansion

set "ROOT_DIR=%~dp0.."
for %%I in ("%ROOT_DIR%") do set "ROOT_DIR=%%~fI"
set "SCRIPTS_DIR=%ROOT_DIR%\scripts"
set "FRONTEND_DIR=%ROOT_DIR%\frontend"
set "BACKEND_DIR=%ROOT_DIR%\backend"
set "DEPLOY_DIR=%ROOT_DIR%\deploy"
set "OUTPUT_DIR=%ROOT_DIR%\output"
set "STAGING_DIR=%OUTPUT_DIR%\sub2api-linux-src"
set "STAGING_SCRIPTS_DIR=%STAGING_DIR%\scripts"
set "ZIP_PATH=%OUTPUT_DIR%\sub2api-linux-src.zip"

if not exist "%OUTPUT_DIR%" mkdir "%OUTPUT_DIR%"
if exist "%STAGING_DIR%" rmdir /s /q "%STAGING_DIR%"
mkdir "%STAGING_DIR%"
mkdir "%STAGING_SCRIPTS_DIR%"

echo [step] Build frontend assets
call pnpm --dir "%FRONTEND_DIR%" run build
if errorlevel 1 exit /b 1

echo [step] Prepare source package directory
call :copy_tree "%BACKEND_DIR%\cmd" "%STAGING_DIR%\cmd" || exit /b 1
call :copy_tree "%BACKEND_DIR%\ent" "%STAGING_DIR%\ent" || exit /b 1
call :copy_tree "%BACKEND_DIR%\internal" "%STAGING_DIR%\internal" || exit /b 1
call :copy_tree "%BACKEND_DIR%\migrations" "%STAGING_DIR%\migrations" || exit /b 1
call :copy_tree "%BACKEND_DIR%\resources" "%STAGING_DIR%\resources" || exit /b 1

copy /y "%BACKEND_DIR%\go.mod" "%STAGING_DIR%\go.mod" >nul || exit /b 1
copy /y "%BACKEND_DIR%\go.sum" "%STAGING_DIR%\go.sum" >nul || exit /b 1
copy /y "%BACKEND_DIR%\Makefile" "%STAGING_DIR%\Makefile" >nul || exit /b 1
copy /y "%DEPLOY_DIR%\config.example.yaml" "%STAGING_DIR%\config.example.yaml" >nul || exit /b 1
copy /y "%SCRIPTS_DIR%\build-linux-backend.sh" "%STAGING_SCRIPTS_DIR%\build-linux-backend.sh" >nul || exit /b 1
copy /y "%SCRIPTS_DIR%\sub2api-service.sh" "%STAGING_SCRIPTS_DIR%\sub2api-service.sh" >nul || exit /b 1
copy /y "%SCRIPTS_DIR%\cleanup-sub2api-logs.sh" "%STAGING_SCRIPTS_DIR%\cleanup-sub2api-logs.sh" >nul || exit /b 1

(
  echo package_name=sub2api-linux-src
  echo frontend_dist=backend/internal/web/dist
  echo linux_build_command=sh ./scripts/build-linux-backend.sh
  echo binary_build_command=go build -tags embed -o sub2api ./cmd/server
) > "%STAGING_DIR%\BUILD_INFO.txt"

if exist "%ZIP_PATH%" del /f /q "%ZIP_PATH%"

echo [step] Create zip archive
powershell -NoProfile -ExecutionPolicy Bypass -Command "Compress-Archive -Path '%STAGING_DIR%\*' -DestinationPath '%ZIP_PATH%' -Force"
if errorlevel 1 exit /b 1

echo.
echo Staging directory: %STAGING_DIR%
echo Archive:           %ZIP_PATH%
exit /b 0

:copy_tree
set "SRC=%~1"
set "DST=%~2"
if not exist "%SRC%" (
  echo Missing source: "%SRC%"
  exit /b 1
)
xcopy "%SRC%" "%DST%\" /E /I /Y /Q >nul
if errorlevel 1 exit /b 1
exit /b 0
