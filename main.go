package main

import (
	"context"
	"fmt"
	"go.uber.org/fx/fxevent"
	"go.uber.org/zap"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"alert-mobile-notify/api"
	"alert-mobile-notify/config"
	"alert-mobile-notify/ec600n"
	"alert-mobile-notify/notification"
	"github.com/robfig/cron/v3"
	"go.uber.org/fx"
)

const (
	// DefaultNetworkCheckInterval 默认网络检查间隔（分钟）
	DefaultNetworkCheckInterval = 10
)

func main() {
	app := fx.New(
		fx.WithLogger(func() fxevent.Logger { return fxevent.NopLogger }),
		// 配置模块
		config.ProvideConfig(),
		// 通知模块
		notification.ProvideWechatNotify(),
		// EC600N模块
		ec600n.ProvideEC600N(),
		// HTTP API服务器模块
		api.ProvideHTTPServer(),
		// 启动调度器
		fx.Invoke(config.InitLogger, initScheduler),
	)

	// 启动应用
	ctx := context.Background()
	if err := app.Start(ctx); err != nil {
		zap.S().Errorf("启动应用失败: %v", err)
	}

	// 等待中断信号
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	<-sigChan

	zap.S().Info("收到停止信号，正在关闭应用...")

	// 停止应用
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := app.Stop(shutdownCtx); err != nil {
		zap.S().Errorf("停止应用失败: %v", err)
	}

	zap.S().Info("应用已停止")
}

// buildNetworkCheckCron 根据检查间隔（分钟）生成 cron 表达式
// 格式：秒 分 时 日 月 星期，例如每30分钟：0 */30 * * * ?
func buildNetworkCheckCron(intervalMinutes int) string {
	if intervalMinutes <= 0 {
		intervalMinutes = DefaultNetworkCheckInterval
	}
	return fmt.Sprintf("0 */%d * * * ?", intervalMinutes)
}

// initScheduler 初始化定时任务调度器
// 如果 EC600N 模块未启用或为 nil，则不启动调度器
func initScheduler(lifecycle fx.Lifecycle, cfg *config.Config, ec600nModule *ec600n.EC600N) {
	// 如果 EC600N 模块未启用，直接返回
	if ec600nModule == nil {
		zap.S().Info("EC600N 模块未启用，跳过调度器初始化")
		return
	}

	cronScheduler := cron.New(cron.WithSeconds())
	var mu sync.Mutex

	// 根据配置生成 cron 表达式
	checkInterval := cfg.EC600N.NetworkCheckInterval
	if checkInterval <= 0 {
		checkInterval = DefaultNetworkCheckInterval
		zap.S().Errorf("网络检查间隔配置无效，使用默认值: %d 分钟", DefaultNetworkCheckInterval)
	}
	cronExpr := buildNetworkCheckCron(checkInterval)

	lifecycle.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			// 添加网络监控任务
			_, err := cronScheduler.AddFunc(cronExpr, func() {
				// 使用 TryLock 防止任务重叠执行
				if !mu.TryLock() {
					zap.S().Warn("上一次网络监控任务仍在执行，跳过本次执行")
					return
				}
				defer mu.Unlock()

				if err := ec600nModule.StartNetworkMonitoring(); err != nil {
					zap.S().Errorf("网络监控任务执行失败: %v", err)
				}
			})
			if err != nil {
				return fmt.Errorf("添加定时任务失败: %w", err)
			}

			cronScheduler.Start()
			zap.S().Info("定时任务调度器已启动，网络检查间隔: %d 分钟 (cron: %s)", checkInterval, cronExpr)
			return nil
		},
		OnStop: func(ctx context.Context) error {
			stopCtx := cronScheduler.Stop()
			<-stopCtx.Done()

			zap.S().Warn("定时任务调度器已停止")
			return nil
		},
	})
}
