// airgate-studio 独立部署的多模态内容创作服务。
//
// 启动流程：env 配置 → Postgres 连接 + 迁移 → 任务 worker → HTTP 路由（含 SPA 托管）→ 优雅退出。
// 用户身份经 core 的 OAuth2（授权码 + PKCE）单点登录；生成任务由自带 worker
// 拿用户的 sk- key 调 core 的 OpenAI 兼容端点执行，产物图片存本地磁盘。
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/DouDOU-start/airgate-studio/backend/internal/studio"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	if err := run(logger); err != nil {
		logger.Error("server_exit", "error", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	cfg, err := studio.LoadConfig()
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// ── 数据库 ──
	db, err := studio.OpenDB(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()
	if err := studio.Migrate(ctx, db); err != nil {
		return err
	}

	// ── 依赖装配 ──
	tasks := studio.NewPGTaskStore(db)
	users := studio.NewPGUserStore(db)
	assets, err := studio.NewAssetStore(cfg.DataDir)
	if err != nil {
		return err
	}
	core := studio.NewCoreClient(cfg.AirgateBaseURL)
	server := studio.NewServer(cfg, tasks, users, assets, core, logger)

	// ── 任务 worker（单 goroutine 轮询）──
	worker := studio.NewWorker(tasks, users, assets, core, logger)
	workerDone := make(chan struct{})
	go func() {
		defer close(workerDone)
		worker.Run(ctx)
	}()

	// ── HTTP 服务 ──
	httpServer := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           server.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	serveErr := make(chan error, 1)
	go func() {
		logger.Info("server_started", "addr", cfg.ListenAddr, "core", cfg.AirgateBaseURL)
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serveErr <- err
		}
	}()

	// ── 优雅退出 ──
	select {
	case err := <-serveErr:
		stop()
		<-workerDone
		return err
	case <-ctx.Done():
	}
	logger.Info("server_stopping")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		logger.Warn("http_shutdown_error", "error", err)
	}
	<-workerDone
	logger.Info("server_stopped")
	return nil
}
