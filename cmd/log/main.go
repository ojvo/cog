package main

import (
	"fmt"
	"ojv/cog/cfg"
	"ojv/cog/log"
	"os"
	"strings"
	"time"
)

func main() {
	const configFile = "config.cfg"

	// 1. 加载配置文件
	conf, err := cfg.Load(configFile)
	if err != nil {
		fmt.Printf("加载配置失败: %v\n", err)
		os.Exit(1)
	}
	conf = conf.ExpandEnv()

	// 2. 手动初始化日志模块 (不使用 cor 包装)
	initLogger(conf)
	defer log.Close()

	log.Info("=== 独立日志应用示例 (Standalone Log Demo - No Cor) ===")
	log.Infof("应用名称: %s", conf.StringOr("app.name", ""))
	log.Infof("应用版本: %s", conf.StringOr("app.version", ""))

	// 基础级别日志测试
	log.Debug("这是一条 DEBUG 级别的日志")
	log.Info("这是一条 INFO 级别的日志")
	log.Warn("这是一条 WARN 级别的日志")
	log.Error("这是一条 ERROR 级别的日志")

	// 结构化日志测试
	log.Info("[测试结构化日志]")
	log.DefaultLogger().WithFields(log.Fields{
		"user_id": 9527,
		"action":  "standalone_test",
	}).Info("直接调用 log 模块成功")

	// 模拟运行
	log.Info("----------------------------------------------")
	log.Info("正在运行中。按 Ctrl+C 退出。")

	count := 0
	for count < 10 {
		log.Infof("循环运行中... %d", count)
		time.Sleep(2 * time.Second)
		count++
	}

	log.Info("演示结束。")
}

// initLogger 演示了如何直接从 cfg.Config 提取参数并初始化 log 模块
func initLogger(c *cfg.Config) {
	// 默认配置
	logOpts := log.LogOpts{
		Level:         log.InfoLevel,
		EnableConsole: true,
	}

	// 级别解析辅助
	parseLevel := func(s string, defaultLevel log.Level) log.Level {
		switch strings.ToLower(s) {
		case "debug":
			return log.DebugLevel
		case "info":
			return log.InfoLevel
		case "warn":
			return log.WarnLevel
		case "error":
			return log.ErrorLevel
		case "fatal":
			return log.FatalLevel
		}
		return defaultLevel
	}

	// 尝试解析多输出流
	if streams, ok := c.Slice("log.streams"); ok {
		for _, s := range streams {
			var opts log.StreamOpts
			if s.Bind("", &opts) {
				// 手动处理可能为字符串的 level
				if levelStr := s.StringOr("level", ""); levelStr != "" && opts.Level == 0 {
					opts.Level = parseLevel(levelStr, log.InfoLevel)
				}
				logOpts.Streams = append(logOpts.Streams, opts)
			}
		}
	}

	// 基础全局配置
	logOpts.EnableCaller = c.BoolOr("log.caller", false)
	logOpts.Async = c.BoolOr("log.async", false)
	if levelStr := c.StringOr("log.level", ""); levelStr != "" {
		logOpts.Level = parseLevel(levelStr, log.InfoLevel)
	}

	// 初始化
	if err := log.Init(logOpts); err != nil {
		fmt.Printf("初始化日志失败: %v\n", err)
	}
}
