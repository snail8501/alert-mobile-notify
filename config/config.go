package config

import (
	"fmt"
	"os"

	"go.uber.org/fx"
	"gopkg.in/yaml.v3"
)

const (
	// DefaultConfigFile 默认配置文件路径
	DefaultConfigFile = "config.yaml"
	// ConfigFileEnvKey 配置文件路径环境变量名
	ConfigFileEnvKey = "CONFIG_FILE"
)

// Config 应用配置结构
type Config struct {
	Wechat struct {
		WebhookURL string `yaml:"webhook_url"` // 企业微信机器人 webhook 地址
	} `yaml:"wechat"`
	EC600N struct {
		Enabled              bool   `yaml:"enabled"`                // 是否启用 EC600N 功能
		SerialPort           string `yaml:"serial_port"`            // 串口设备路径
		BaudRate             int    `yaml:"baud_rate"`              // 波特率
		HTTPPort             int    `yaml:"http_port"`              // HTTP 服务端口
		CallDuration         int    `yaml:"call_duration"`          // 通话时长（秒）
		NetworkCheckInterval int    `yaml:"network_check_interval"` // 网络状态检查间隔（分钟）
		CheckInterval        int    `yaml:"check_interval"`         // 检查间隔（分钟）
	} `yaml:"ec600n"`
	Logger struct {
		FileName   string `yaml:"fileName"`
		Path       string `yaml:"path"`
		MaxAge     int    `yaml:"maxAge"`
		MaxSize    int    `yaml:"maxSize"`
		MaxBackups int    `yaml:"maxBackups"`
	} `yaml:"logger"`
}

// LoadConfig 从 YAML 文件加载配置
func LoadConfig(configFile string) (*Config, error) {
	data, err := os.ReadFile(configFile)
	if err != nil {
		return nil, fmt.Errorf("读取配置文件失败 [%s]: %w", configFile, err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("解析配置文件失败 [%s]: %w", configFile, err)
	}

	return &cfg, nil
}

// ProvideConfig 提供配置依赖注入
// 配置文件路径可通过环境变量 CONFIG_FILE 指定，默认为 config.yaml
func ProvideConfig() fx.Option {
	return fx.Provide(func() (*Config, error) {
		configFile := DefaultConfigFile
		if envFile := os.Getenv(ConfigFileEnvKey); envFile != "" {
			configFile = envFile
		}
		return LoadConfig(configFile)
	})
}
