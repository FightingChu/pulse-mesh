@echo off
chcp 936 >nul
setlocal
cd /d "%~dp0"

for /f "delims=" %%i in ('git rev-parse --abbrev-ref HEAD') do set BRANCH=%%i

git remote get-url github >nul 2>nul
if errorlevel 1 git remote add github git@github.com:FightingChu/pulse-mesh.git
git remote get-url gitee >nul 2>nul
if errorlevel 1 git remote add gitee git@gitee.com:cwyay/pulse-mesh.git

echo [1/2] push to github branch %BRANCH%
git push -u github %BRANCH%
if errorlevel 1 goto err_github

echo [2/2] push to gitee branch %BRANCH%
git push -u gitee %BRANCH%
if errorlevel 1 goto err_gitee

echo PUSH OK
goto end

:err_github
echo GITHUB PUSH FAILED
echo Check that your public key is added to GitHub account FightingChu.
echo Test with: ssh -T git@github.com
echo If port 22 is blocked add ssh.github.com 443 config in .ssh\config.
goto end

:err_gitee
echo GITEE PUSH FAILED
echo Check that your public key is added to Gitee account cwyay.
echo Test with: ssh -T git@gitee.com
echo If port 22 is blocked add gitee.com 443 config in .ssh\config.
goto end

:end
pause
