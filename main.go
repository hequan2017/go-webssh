package main

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/hequan2017/go-webssh/core"
)

//go:embed web/html/index.html static
var assets embed.FS

func main() {
	cfg := core.LoadConfig()
	if err := cfg.Validate(); err != nil {
		slog.Error("配置无效", "error", err)
		os.Exit(1)
	}
	app, err := core.NewApplication(cfg, assets)
	if err != nil {
		slog.Error("初始化跳板机失败", "error", err)
		os.Exit(1)
	}

	server := &http.Server{
		Addr:              cfg.Addr,
		Handler:           app.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	go func() {
		slog.Info("go-webssh 跳板机已启动", "listen", cfg.Addr, "data_dir", cfg.DataDir)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("HTTP 服务异常退出", "error", err)
			os.Exit(1)
		}
	}()

	<-stop
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "关闭服务失败: %v\n", err)
	}
}
