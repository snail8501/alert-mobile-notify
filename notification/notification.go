package notification

import (
	"alert-mobile-notify/config"
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"go.uber.org/zap"
	"io"
	"net/http"
	"time"

	"go.uber.org/fx"
)

const (
	// HTTPRequestTimeout HTTP 请求超时时间
	HTTPRequestTimeout = 10 * time.Second
	// WechatMsgTypeText 企业微信文本消息类型
	WechatMsgTypeText = "text"
	// ContentTypeJSON JSON 内容类型
	ContentTypeJSON = "application/json"
)

// WechatNotify 企业微信通知器实现
type WechatNotify struct {
	config     *config.Config
	client     *http.Client
	webhookURL string
}

// NewWechatNotify 创建新的企业微信通知器
func NewWechatNotify(cfg *config.Config) *WechatNotify {
	// 创建 HTTP 客户端，配置 TLS（生产环境建议使用有效的证书）
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: true, // 注意：生产环境应使用有效证书
		},
	}

	return &WechatNotify{
		config: cfg,
		client: &http.Client{
			Transport: transport,
			Timeout:   HTTPRequestTimeout,
		},
		webhookURL: cfg.Wechat.WebhookURL,
	}
}

// SendToWechat 发送消息到企业微信
func (w *WechatNotify) SendToWechat(message string) error {
	// 始终记录日志
	zap.S().Infof("[通知] %s", message)

	// 如果未配置 webhook URL，只记录日志
	if w.webhookURL == "" {
		zap.S().Error("未配置 webhook URL，仅记录日志")
		return nil
	}

	// 构造企业微信文本消息
	payload := map[string]interface{}{
		"msgtype": WechatMsgTypeText,
		"text": map[string]string{
			"content": message,
		},
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("JSON 序列化失败: %w", err)
	}

	// 发送 HTTP POST 请求
	resp, err := w.client.Post(w.webhookURL, ContentTypeJSON, bytes.NewBuffer(data))
	if err != nil {
		zap.S().Errorf("发送 webhook 请求失败: %v", err)
		return fmt.Errorf("发送 webhook 请求失败: %w", err)
	}
	defer resp.Body.Close()

	// 读取响应内容
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		zap.S().Errorf("读取 webhook 响应失败: %v", err)
		return fmt.Errorf("读取 webhook 响应失败: %w", err)
	}

	// 检查响应状态码
	if resp.StatusCode != http.StatusOK {
		zap.S().Errorf("Webhook 返回错误状态码: %d, 响应内容: %s", resp.StatusCode, string(body))
		return fmt.Errorf("webhook 返回错误状态码: %d", resp.StatusCode)
	}

	zap.S().Infof("Webhook 消息发送成功: %s", string(body))
	return nil
}

// ProvideWechatNotify 提供通知器依赖注入
func ProvideWechatNotify() fx.Option {
	return fx.Provide(NewWechatNotify)
}
