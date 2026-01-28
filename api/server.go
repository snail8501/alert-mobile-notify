package api

import (
	"alert-mobile-notify/config"
	"alert-mobile-notify/ec600n"
	"alert-mobile-notify/notification"
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"go.uber.org/fx"
	"go.uber.org/zap"
)

const (
	// DefaultHTTPPort 默认HTTP端口
	DefaultHTTPPort = 8080
	// TimestampTolerance 时间戳容差（分钟）
	TimestampTolerance = 5
)

// NotifyRequest API请求结构
type NotifyRequest struct {
	Name         string `json:"name"`
	PhoneNumbers string `json:"phoneNumbers"`
	Timestamp    string `json:"timestamp"`
	Signature    string `json:"signature"`
}

// NotifyResponse API响应结构
type NotifyResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message,omitempty"`
}

// HTTPServer HTTP服务器
type HTTPServer struct {
	config    *config.Config
	server    *http.Server
	secretKey string
	ec600n    *ec600n.EC600N
	notify    *notification.WechatNotify
}

// NewHTTPServer 创建新的HTTP服务器
func NewHTTPServer(cfg *config.Config, ec600nModule *ec600n.EC600N, notify *notification.WechatNotify) *HTTPServer {
	secretKey := cfg.API.SecretKey
	if secretKey == "" {
		zap.S().Warn("API secret_key 未配置，签名验证将失败")
	}

	port := cfg.API.HTTPPort
	if port == 0 {
		port = DefaultHTTPPort
		zap.S().Infof("API http_port 未配置，使用默认端口: %d", DefaultHTTPPort)
	}

	mux := http.NewServeMux()
	server := &HTTPServer{
		config:    cfg,
		secretKey: secretKey,
		ec600n:    ec600nModule,
		notify:    notify,
		server: &http.Server{
			Addr:    fmt.Sprintf(":%d", port),
			Handler: mux,
		},
	}

	// 注册路由
	mux.HandleFunc("/api/nofity", server.handleNotify)

	return server
}

// generateSignature 生成签名
// 参数按字母升序排序：name, phoneNumbers, secretKey, timestamp
// 拼接格式：name=value&phoneNumbers=value&secretKey=value&timestamp=value
// 使用MD5生成签名
func (s *HTTPServer) generateSignature(name, phoneNumbers, timestamp string) string {
	// 创建参数映射
	params := map[string]string{
		"name":         name,
		"phoneNumbers": phoneNumbers,
		"secretKey":    s.secretKey,
		"timestamp":    timestamp,
	}

	// 按键名排序
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	// 拼接参数字符串
	var parts []string
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s=%s", k, params[k]))
	}
	signString := strings.Join(parts, "&")

	// 生成MD5签名
	hash := md5.Sum([]byte(signString))
	return hex.EncodeToString(hash[:])
}

// validateSignature 验证签名
func (s *HTTPServer) validateSignature(req *NotifyRequest) bool {
	expectedSignature := s.generateSignature(req.Name, req.PhoneNumbers, req.Timestamp)
	return strings.EqualFold(expectedSignature, req.Signature)
}

// validateTimestamp 验证时间戳
// timestamp为UTC时间戳（秒或毫秒），必须在当前时间±5分钟内
func (s *HTTPServer) validateTimestamp(timestampStr string) (bool, error) {
	// 尝试解析为秒级时间戳
	timestamp, err := strconv.ParseInt(timestampStr, 10, 64)
	if err != nil {
		return false, fmt.Errorf("时间戳格式错误: %w", err)
	}

	// 判断是秒级还是毫秒级时间戳（通常毫秒级时间戳大于10位数）
	if timestamp > 9999999999 {
		timestamp = timestamp / 1000 // 毫秒转秒
	}

	// 转换为UTC时间
	reqTime := time.Unix(timestamp, 0).UTC()
	now := time.Now().UTC()

	// 计算时间差
	diff := now.Sub(reqTime)
	tolerance := time.Duration(TimestampTolerance) * time.Minute

	// 验证时间戳在容差范围内
	if diff > tolerance || diff < -tolerance {
		return false, fmt.Errorf("时间戳超出容差范围，当前时间: %s, 请求时间: %s, 差值: %v",
			now.Format(time.RFC3339), reqTime.Format(time.RFC3339), diff)
	}

	return true, nil
}

// writeErrorResponse 写入错误响应
func (s *HTTPServer) writeErrorResponse(w http.ResponseWriter, statusCode int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(NotifyResponse{Success: false, Message: message})
}

// parseAndValidateRequest 解析并验证请求
func (s *HTTPServer) parseAndValidateRequest(r *http.Request) (*NotifyRequest, error) {
	if r.Method != http.MethodPost {
		return nil, fmt.Errorf("method not allowed")
	}

	var req NotifyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return nil, fmt.Errorf("解析请求体失败: %w", err)
	}

	if !s.validateSignature(&req) {
		zap.S().Warnf("签名验证失败: name=%s, phoneNumbers=%s, timestamp=%s",
			req.Name, req.PhoneNumbers, req.Timestamp)
		return nil, fmt.Errorf("签名验证失败")
	}

	if valid, err := s.validateTimestamp(req.Timestamp); !valid {
		zap.S().Warnf("时间戳验证失败: %v", err)
		return nil, fmt.Errorf("时间戳验证失败: %w", err)
	}

	return &req, nil
}

// parsePhoneNumbers 解析并清理电话号码列表
func parsePhoneNumbers(phoneNumbersStr string) []string {
	if phoneNumbersStr == "" {
		return nil
	}

	numbers := strings.Split(phoneNumbersStr, ",")
	var cleanNumbers []string
	for _, num := range numbers {
		if num = strings.TrimSpace(num); num != "" {
			cleanNumbers = append(cleanNumbers, num)
		}
	}
	return cleanNumbers
}

// sendWechatNotification 发送企业微信通知
func (s *HTTPServer) sendWechatNotification(name string, phoneNumbers []string) {
	if s.notify == nil || len(phoneNumbers) == 0 {
		return
	}

	message := fmt.Sprintf(`📞 名称: %s
电话号码: %s
时间: %s
即将开始拨打电话...`,
		name,
		strings.Join(phoneNumbers, ", "),
		time.Now().Format("2006-01-02 15:04:05"))

	if err := s.notify.SendToWechat(message); err != nil {
		zap.S().Errorf("发送企业微信通知失败: %v", err)
	}
}

// makePhoneCall 拨打电话
func (s *HTTPServer) makePhoneCall(phoneNumber string, duration int) string {
	zap.S().Infof("开始拨打电话: %s", phoneNumber)
	if err := s.ec600n.MakeCall(phoneNumber); err != nil {
		zap.S().Errorf("拨打电话失败 [%s]: %v", phoneNumber, err)
		return fmt.Sprintf("%s: 失败 - %v", phoneNumber, err)
	}

	zap.S().Infof("通话中，等待 %d 秒后挂断...", duration)
	time.Sleep(time.Duration(duration) * time.Second)

	if err := s.ec600n.HangupCall(); err != nil {
		zap.S().Errorf("挂断电话失败 [%s]: %v", phoneNumber, err)
		return fmt.Sprintf("%s: 拨打成功但挂断失败 - %v", phoneNumber, err)
	}

	zap.S().Infof("电话已挂断: %s", phoneNumber)
	return fmt.Sprintf("%s: 成功", phoneNumber)
}

// processPhoneCalls 处理拨打电话流程
// 只处理第一个电话号码并返回结果，其他电话号码异步执行
func (s *HTTPServer) processPhoneCalls(name string, phoneNumbersStr string) error {
	if phoneNumbersStr == "" || s.ec600n == nil || !s.ec600n.IsConnected() {
		if s.ec600n == nil || !s.ec600n.IsConnected() {
			zap.S().Warn("EC600N 模块未启用或未连接，跳过拨打电话")
			return fmt.Errorf("EC600N 模块未启用或未连接")
		}
		return fmt.Errorf("电话号码为空")
	}

	phoneNumbers := parsePhoneNumbers(phoneNumbersStr)
	if len(phoneNumbers) == 0 {
		return fmt.Errorf("电话号码为空")
	}

	s.sendWechatNotification(name, phoneNumbers)

	go func() {
		callDuration := s.config.EC600N.CallDuration
		if callDuration <= 0 {
			callDuration = 10
		}

		for _, phoneNumber := range phoneNumbers[1:] {
			zap.S().Infof("已启动 %d 个异步拨打电话任务", len(phoneNumbers)-1)
			s.makePhoneCall(phoneNumber, callDuration)
		}
	}()

	return nil
}

// handleNotify 处理 /api/nofity 请求
func (s *HTTPServer) handleNotify(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	req, err := s.parseAndValidateRequest(r)
	if err != nil {
		statusCode := http.StatusBadRequest
		if strings.Contains(err.Error(), "签名验证失败") || strings.Contains(err.Error(), "时间戳验证失败") {
			statusCode = http.StatusUnauthorized
		} else if strings.Contains(err.Error(), "method not allowed") {
			statusCode = http.StatusMethodNotAllowed
		}
		s.writeErrorResponse(w, statusCode, err.Error())
		return
	}

	zap.S().Infof("API请求验证成功: name=%s, phoneNumbers=%s, timestamp=%s",
		req.Name, req.PhoneNumbers, req.Timestamp)

	message := "验证成功"
	if err := s.processPhoneCalls(req.Name, req.PhoneNumbers); err != nil {
		message = fmt.Sprintf("验证成功，但拨打电话失败: %v", err)
		zap.S().Warnf("拨打电话失败: %v", err)
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(NotifyResponse{Success: true, Message: message})
}

// ProvideHTTPServer 提供HTTP服务器依赖注入
func ProvideHTTPServer() fx.Option {
	return fx.Options(
		fx.Provide(NewHTTPServer),
		fx.Invoke(registerHTTPServerLifecycle),
	)
}

// registerHTTPServerLifecycle 注册HTTP服务器生命周期
func registerHTTPServerLifecycle(lifecycle fx.Lifecycle, server *HTTPServer) {
	lifecycle.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			// 在goroutine中启动HTTP服务器
			go func() {
				zap.S().Infof("启动HTTP服务器，监听端口: %s", server.server.Addr)
				if err := server.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
					zap.S().Errorf("HTTP服务器启动失败: %v", err)
				}
			}()
			return nil
		},
		OnStop: func(ctx context.Context) error {
			zap.S().Info("正在关闭HTTP服务器...")
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			if err := server.server.Shutdown(shutdownCtx); err != nil {
				return fmt.Errorf("关闭HTTP服务器失败: %w", err)
			}
			zap.S().Info("HTTP服务器已关闭")
			return nil
		},
	})
}
