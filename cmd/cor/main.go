package main

import (
	"fmt"
	"c.n/ojv/cog/cor"
	"os"
	"time"
)

// AppConfig 演示结构体绑定
type AppConfig struct {
	Name    string `cfg:"app.name"`
	Version string `cfg:"app.version"`
	Env     string `cfg:"app.env"`
}

type ServerConfig struct {
	Host    string        `cfg:"host"`
	Port    int           `cfg:"port"`
	Timeout time.Duration `cfg:"timeout"`
}

func main() {
	const configFile = "config.cfg"

	// 1. 使用 cor.Init 一键初始化 (配置加载 + 日志设置 + 热更新监听)
	if err := cor.Init(configFile); err != nil {
		fmt.Printf("框架初始化失败: %v\n", err)
		os.Exit(1)
	}
	// 确保程序退出时日志刷盘
	defer cor.Close()

	cor.Info("==================================================")
	cor.Info("   Cog/COR Framework Full Demonstration")
	cor.Info("==================================================")

	// 2. 演示直接通过 cor 访问配置
	cor.Infof("应用加载成功: %s (v%s) [%s]",
		cor.GetString("app.name"),
		cor.GetString("app.version"),
		cor.GetString("app.env"))

	// 3. 演示结构体绑定 (Reflection-based)
	var appCfg AppConfig
	var svrCfg ServerConfig
	conf := cor.ConfigInstance()

	conf.Bind("", &appCfg)
	conf.Bind("server", &svrCfg)

	cor.Info("[配置绑定演示]")
	cor.Infof("AppConfig 绑定结果: %+v", appCfg)
	cor.Infof("ServerConfig 绑定结果: %+v", svrCfg)

	// 4. 演示环境变量扩展
	cor.Info("[环境变量演示]")
	cor.Infof("数据库密码 (含默认值): %s", cor.GetString("database.password"))

	// 5. 演示各种日志级别
	cor.Info("[日志功能演示]")
	cor.Debug("这是一条调试信息")
	cor.Info("这是一条普通信息")
	cor.Warn("这是一条警告信息")
	cor.Error("这是一条错误信息")

	// 6. 演示热更新
	cor.Info("--------------------------------------------------")
	cor.Info("--------------------------------------------------")

	// 模拟应用持续运行
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	count := 0
	for count < 6 {
		select {
		case <-ticker.C:
			count++
			cor.Infof("应用心跳检查... 运行中 (当前端口配置: %d)", cor.GetInt("server.port"))
		}
	}

	cor.Info("演示任务完成，程序退出。")
}
