@echo off
rem AI Test Tool 一键启动：后端(:8080) + 前端(:4173)
cd /d "%~dp0"

start "AI Test Tool 后端" /min cmd /c "cd server && set AI_TEST_TOOL_DATA=%~dp0server\data\state.json && go run ./cmd/api"

start "AI Test Tool 前端" /min cmd /c "cd web && npm run dev"

echo 服务启动中...
echo   前端: http://localhost:4173
echo   后端: http://localhost:8080
echo 关闭窗口即可停止服务。
start http://localhost:4173/
