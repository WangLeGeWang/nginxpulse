package main

import (
	"os"
	"os/exec"
	"runtime"
	"strings"

	"github.com/likaia/nginxpulse/internal/app"
	"github.com/sirupsen/logrus"
)

func main() {
	if err := app.Run(); err != nil {
		logrus.WithError(err).Error("服务启动失败")
		// 在容器的 "direct" 启动模式中，nginx 往往是 PID 1，而 Go 后端是后台启动的子进程。
		// 此时即使后端启动失败退出，容器仍可能因为 nginx 仍在前台而保持 Running。
		// 为了让“启动失败=容器退出”成立，这里检测到 PID 1 为 nginx 时会向其发送 SIGTERM。
		// 安全性：仅在确认 PID 1 是 nginx 时才执行，避免误伤其它运行环境。
		tryTerminatePID1IfNginx()
		os.Exit(1)
	}
}

func tryTerminatePID1IfNginx() {
	// 仅 Linux 容器场景需要处理 PID 1；其它平台直接跳过，保证跨平台编译/运行安全。
	if runtime.GOOS != "linux" {
		return
	}

	commBytes, err := os.ReadFile("/proc/1/comm")
	if err != nil {
		return
	}
	comm := strings.TrimSpace(string(commBytes))
	if comm != "nginx" {
		return
	}

	// 避免直接依赖 syscall.Kill/SIGTERM（Windows 上不可编译）。
	// Linux 容器内通常存在 `kill`（busybox/coreutils），这里用外部命令发送 SIGTERM 给 PID 1。
	if err := exec.Command("kill", "-TERM", "1").Run(); err != nil {
		logrus.WithError(err).Warn("服务启动失败：尝试终止 PID 1 (nginx) 失败")
		return
	}
	logrus.Warn("服务启动失败：检测到 PID 1 为 nginx，已发送 SIGTERM 以终止容器")
}


