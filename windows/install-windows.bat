@echo off
chcp 65001 >nul
setlocal DisableDelayedExpansion

echo ========================================
echo Установщик Solovey UI для Windows
echo ========================================

REM Проверка запуска от имени администратора
net session >nul 2>&1
if %errorLevel% neq 0 (
    echo Ошибка: этот скрипт нужно запускать от имени администратора
    echo Щелкните файл правой кнопкой мыши и выберите "Запуск от имени администратора"
    pause
    exit /b 1
)

cd /d "%~dp0"
REM Каталог установки
set "INSTALL_DIR=C:\Program Files\Solovey UI"
set "SERVICE_NAME=solovey-ui"
set "WINSW_SUPPORTED=1"
if /I "%PROCESSOR_ARCHITECTURE%"=="ARM64" set "WINSW_SUPPORTED=0"
if /I "%PROCESSOR_ARCHITEW6432%"=="ARM64" set "WINSW_SUPPORTED=0"

echo Установка Solovey UI в каталог: %INSTALL_DIR%

if "%WINSW_SUPPORTED%"=="0" (
    sc query %SERVICE_NAME% >nul 2>&1
    if not errorlevel 1 goto unsupported_existing_service
)

REM Остановка существующей службы перед заменой исполняемых файлов.
powershell -NoProfile -ExecutionPolicy Bypass -Command "& {$ErrorActionPreference='Stop'; $service=Get-Service -Name 'solovey-ui' -ErrorAction SilentlyContinue; if ($null -ne $service -and $service.Status -ne 'Stopped') {Stop-Service -Name 'solovey-ui' -Force; $service.WaitForStatus('Stopped',[TimeSpan]::FromSeconds(30))}}"
if errorlevel 1 goto service_stop_failed

REM Создание каталога установки
if not exist "%INSTALL_DIR%" mkdir "%INSTALL_DIR%"
if not exist "%INSTALL_DIR%\db" mkdir "%INSTALL_DIR%\db"
if not exist "%INSTALL_DIR%\logs" mkdir "%INSTALL_DIR%\logs"
if not exist "%INSTALL_DIR%\cert" mkdir "%INSTALL_DIR%\cert"
if not exist "%INSTALL_DIR%\db" goto directory_failed
if not exist "%INSTALL_DIR%\logs" goto directory_failed
if not exist "%INSTALL_DIR%\cert" goto directory_failed

REM Копирование файлов
echo Копирование файлов...
copy "sui.exe" "%INSTALL_DIR%\" >nul
if errorlevel 1 goto copy_failed
copy "s-ui-windows.xml" "%INSTALL_DIR%\" >nul
if errorlevel 1 goto copy_failed
copy "s-ui-windows.bat" "%INSTALL_DIR%\" >nul
if errorlevel 1 goto copy_failed
copy "configure-windows.ps1" "%INSTALL_DIR%\" >nul
if errorlevel 1 goto copy_failed
copy "control-windows.ps1" "%INSTALL_DIR%\" >nul
if errorlevel 1 goto copy_failed
copy "uninstall-windows.bat" "%INSTALL_DIR%\" >nul
if errorlevel 1 goto copy_failed
copy "uninstall-windows.ps1" "%INSTALL_DIR%\" >nul
if errorlevel 1 goto copy_failed
copy "README.md" "%INSTALL_DIR%\" >nul
if errorlevel 1 goto copy_failed

REM Проверка наличия WinSW
set "WINSW_PATH=%INSTALL_DIR%\winsw.exe"
if "%WINSW_SUPPORTED%"=="0" (
    echo Автоматическая установка службы недоступна для Windows ARM64; бинарный файл можно запускать вручную.
) else (
    if not exist "%WINSW_PATH%" (
        echo Загрузка WinSW...
        powershell -NoProfile -ExecutionPolicy Bypass -Command "& {$ErrorActionPreference='Stop'; [Net.ServicePointManager]::SecurityProtocol=[Net.SecurityProtocolType]::Tls12; Invoke-WebRequest -UseBasicParsing -Uri 'https://github.com/winsw/winsw/releases/download/v2.12.0/WinSW-x64.exe' -OutFile '%WINSW_PATH%'}"
        if errorlevel 1 (
            if exist "%WINSW_PATH%" del /f /q "%WINSW_PATH%"
            echo Ошибка: не удалось загрузить WinSW.
            pause
            exit /b 1
        )
    )
    powershell -NoProfile -ExecutionPolicy Bypass -Command "& {if ((Get-FileHash -Algorithm SHA256 -LiteralPath '%WINSW_PATH%').Hash.ToLowerInvariant() -ne '05b82d46ad331cc16bdc00de5c6332c1ef818df8ceefcd49c726553209b3a0da') {exit 1}}"
    if errorlevel 1 (
        if exist "%WINSW_PATH%" del /f /q "%WINSW_PATH%"
        echo Ошибка: не удалось безопасно загрузить WinSW.
        pause
        exit /b 1
    ) else (
        echo WinSW проверен
    )
)

REM Подготовка файлов службы Windows
if "%WINSW_SUPPORTED%"=="1" (
    cd /d "%INSTALL_DIR%"
    copy "winsw.exe" "solovey-ui-service.exe" >nul
    if errorlevel 1 goto service_copy_failed
    copy "s-ui-windows.xml" "solovey-ui-service.xml" >nul
    if errorlevel 1 goto service_copy_failed
)

REM Запуск миграции
echo Запуск миграции базы данных...
cd /d "%INSTALL_DIR%"
sui.exe migrate
if errorlevel 1 (
    echo Ошибка: миграция базы данных не выполнена
    pause
    exit /b 1
) else (
    echo Миграция успешно завершена
)

REM Настройка выполняется в PowerShell, чтобы пользовательский ввод никогда
REM не интерпретировался командным процессором cmd.exe.
powershell -NoProfile -ExecutionPolicy Bypass -File "%INSTALL_DIR%\configure-windows.ps1" -InstallDir "%INSTALL_DIR%"
if errorlevel 1 goto configuration_failed

REM Настройка прав
echo Настройка прав доступа...
icacls "%INSTALL_DIR%" /grant "*S-1-5-32-545:(OI)(CI)RX" /T >nul
if errorlevel 1 goto permissions_failed
icacls "%INSTALL_DIR%\db" /grant "*S-1-5-32-545:(OI)(CI)F" /T >nul
if errorlevel 1 goto permissions_failed
icacls "%INSTALL_DIR%\logs" /grant "*S-1-5-32-545:(OI)(CI)F" /T >nul
if errorlevel 1 goto permissions_failed

REM Создание переменной окружения
echo Настройка переменной окружения...
setx SUI_HOME "%INSTALL_DIR%" /M >nul
if errorlevel 1 goto environment_failed

REM Регистрация и запуск службы только после успешной настройки приложения.
if "%WINSW_SUPPORTED%"=="1" (
    cd /d "%INSTALL_DIR%"
    sc query %SERVICE_NAME% >nul 2>&1
    if not errorlevel 1 (
        solovey-ui-service.exe uninstall
        if errorlevel 1 goto service_install_failed
    )
    solovey-ui-service.exe install
    if errorlevel 1 goto service_install_failed
    net start %SERVICE_NAME%
    if errorlevel 1 goto service_start_failed
    echo Служба успешно установлена и запущена
)

REM Показ итоговой конфигурации
echo.
echo ========================================
echo Установка успешно завершена!
echo ========================================
echo.
echo Solovey UI установлен в каталог: %INSTALL_DIR%
echo.
echo Имя службы: %SERVICE_NAME%
echo.
echo Полезные команды:
echo   net start %SERVICE_NAME%    - запустить службу
echo   net stop %SERVICE_NAME%     - остановить службу
echo   sc query %SERVICE_NAME%     - проверить состояние службы
echo.
echo Также можно использовать ярлык на рабочем столе или пункт меню Пуск.
echo.
pause
exit /b 0

:copy_failed
echo Ошибка: не удалось скопировать обязательные файлы установки.
pause
exit /b 1

:service_copy_failed
echo Ошибка: не удалось подготовить файлы службы Windows.
pause
exit /b 1

:configuration_failed
echo Ошибка: не удалось безопасно применить конфигурацию Windows.
pause
exit /b 1

:permissions_failed
echo Ошибка: не удалось настроить права доступа Windows.
pause
exit /b 1

:environment_failed
echo Ошибка: не удалось сохранить SUI_HOME.
pause
exit /b 1

:directory_failed
echo Ошибка: не удалось создать каталоги установки Windows.
pause
exit /b 1

:service_stop_failed
echo Ошибка: не удалось остановить существующую службу Windows.
pause
exit /b 1

:service_install_failed
echo Ошибка: не удалось зарегистрировать службу Windows.
pause
exit /b 1

:service_start_failed
echo Ошибка: служба Windows зарегистрирована, но не запустилась.
pause
exit /b 1

:unsupported_existing_service
echo Ошибка: на Windows ARM64 найдена существующая служба x64. Сначала удалите ее штатным деинсталлятором.
pause
exit /b 1
