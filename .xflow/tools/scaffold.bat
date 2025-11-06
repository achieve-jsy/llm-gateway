@echo off
chcp 65001 >nul
REM scaffold.bat — Xuanwu Project Scaffold Entry Script (Windows)
REM Usage: scaffold.bat [project_dir] [--config config_path]
REM   未带 --config 时：交互选择区域（1=广州玄武-活动 2=杭州玄武-活动），再传入对应 YAML。
REM Note: the default xuanwu config clones xwRef together with client.

setlocal enabledelayedexpansion

set "SCRIPT_DIR=%~dp0"
set "PYTHON_SCRIPT=%SCRIPT_DIR%..\scripts\xuanwu\scaffold-init.py"

if not exist "%PYTHON_SCRIPT%" (
    echo [ERROR] Python script not found: %PYTHON_SCRIPT%
    exit /b 1
)

REM Find Python environment
set "PYTHON_CMD="

REM Check common Python installation paths
for %%v in (3.11 3.10 3.9) do (
    if exist "C:\Python%%v\python.exe" (
        set "PYTHON_CMD=C:\Python%%v\python.exe"
        goto :found
    )
)

REM Check python in PATH
where python >nul 2>&1
if %errorlevel% equ 0 (
    set "PYTHON_CMD=python"
    goto :found
)

where python3 >nul 2>&1
if %errorlevel% equ 0 (
    set "PYTHON_CMD=python3"
    goto :found
)

:not_found
echo ============================================================
echo  [ERROR] No Python environment found
echo ============================================================
echo.
echo   Please install Python 3.9+
echo.
echo   Installation options:
echo     1. Official installer: https://www.python.org/downloads/
echo        Check "Add Python to PATH" during installation.
echo.
echo     2. Using winget:
echo        winget install Python.Python.3.11
echo.
echo     3. Using pyenv-win:
echo        winget install pyenv.pyenv
echo        pyenv install 3.11.0
echo        pyenv global 3.11.0
echo.
echo ============================================================
exit /b 1

:found
echo [INFO] Using Python: %PYTHON_CMD%

REM 若命令行已含 --config，不再询问区域，直接透传
echo %* | findstr /I /C:"--config" >nul
if not errorlevel 1 (
    call "%PYTHON_CMD%" "%PYTHON_SCRIPT%" %*
    goto :end
)

call :select_xw_profile
if errorlevel 1 (
    endlocal
    exit /b 1
)
call "%PYTHON_CMD%" "%PYTHON_SCRIPT%" %* --config "!PROFILE_CFG!"
goto :end

:select_xw_profile
echo.
echo ============================================================
echo   请选择玄武-活动区域配置
echo ============================================================
echo   1. 广州玄武-活动 (默认)
echo   2. 杭州玄武-活动
echo ============================================================
set "XW_SEL="
set /p "XW_SEL=请输入编号 [1^|2]，回车默认 1: "
if "!XW_SEL!"=="" set "XW_SEL=1"
if "!XW_SEL!"=="2" (
    for %%I in ("%SCRIPT_DIR%..\scripts\xuanwu\config-hangzhou-activity.yaml") do set "PROFILE_CFG=%%~fI"
) else (
    set "XW_SEL=1"
    for %%I in ("%SCRIPT_DIR%..\scripts\xuanwu\config.yaml") do set "PROFILE_CFG=%%~fI"
)
if not exist "!PROFILE_CFG!" (
    echo [ERROR] 配置文件不存在: !PROFILE_CFG!
    exit /b 1
)
echo [INFO] 已选: !XW_SEL! — 使用配置 !PROFILE_CFG!
exit /b 0

:end
endlocal
