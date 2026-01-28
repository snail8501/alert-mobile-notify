package ec600n

import (
	"alert-mobile-notify/config"
	"alert-mobile-notify/notification"
	"bufio"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/tarm/serial"
	"go.uber.org/fx"
	"go.uber.org/zap"
)

const (
	ATCommandTimeout  = 100 * time.Millisecond // AT 指令响应超时时间
	MaxResponseLines  = 10                     // 最大响应行数
	MinSignalStrength = 5                      // 最小正常信号强度
	IMEILength        = 15                     // IMEI 标准长度
)

var (
	// 预编译正则表达式，提升性能
	reCSQ  = regexp.MustCompile(`\+CSQ:\s*(\d+),(\d+)`)
	reCREG = regexp.MustCompile(`\+CREG:\s*\d+,(\d+)`)
	reCOPS = regexp.MustCompile(`\+COPS:\s*\d+,\d+,"([^"]+)"`)
	reIMEI = regexp.MustCompile(`^\d{15}$`)

	// 网络注册状态码映射
	networkRegStatusMap = map[string]string{
		"0": "未注册",
		"1": "已注册本地网络",
		"2": "正在搜索",
		"3": "注册被拒绝",
		"5": "已注册漫游",
	}
)

// NetworkStatus 网络状态信息
type NetworkStatus struct {
	SignalStrength   int       `json:"signal_strength"`    // 信号强度 (0-31, 99表示未知)
	NetworkRegStatus string    `json:"network_reg_status"` // 网络注册状态
	SIMStatus        string    `json:"sim_status"`         // SIM卡状态
	OperatorName     string    `json:"operator_name"`      // 运营商名称
	IMEI             string    `json:"imei"`               // 设备IMEI
	Timestamp        time.Time `json:"timestamp"`
}

// EC600N EC600N模块控制器
type EC600N struct {
	config    *config.Config
	port      *serial.Port
	connected bool

	notify *notification.WechatNotify
}

// NewEC600N 创建新的 EC600N 实例
// 如果配置中未启用 EC600N 功能，返回 nil
func NewEC600N(cfg *config.Config, notify *notification.WechatNotify) (*EC600N, error) {
	if !cfg.EC600N.Enabled {
		return nil, nil
	}

	ec := &EC600N{
		config:    cfg,
		connected: false,
		notify:    notify,
	}

	if err := ec.initSerial(); err != nil {
		return nil, fmt.Errorf("初始化串口失败: %w", err)
	}

	if err := ec.testConnection(); err != nil {
		return nil, fmt.Errorf("测试连接失败: %w", err)
	}

	ec.connected = true
	zap.S().Info("EC600N 模块初始化成功")
	return ec, nil
}

// initSerial 初始化串口连接
func (e *EC600N) initSerial() error {
	c := &serial.Config{
		Name: e.config.EC600N.SerialPort,
		Baud: e.config.EC600N.BaudRate,
	}

	port, err := serial.OpenPort(c)
	if err != nil {
		return fmt.Errorf("打开串口失败 [%s]: %w", e.config.EC600N.SerialPort, err)
	}

	e.port = port
	return nil
}

// testConnection 测试连接
func (e *EC600N) testConnection() error {
	// 发送AT指令测试连接
	response, err := e.sendATCommand("AT")
	if err != nil {
		return err
	}

	if !strings.Contains(response, "OK") {
		return fmt.Errorf("AT指令测试失败，响应: %s", response)
	}

	return nil
}

// sendATCommand 发送 AT 指令并读取响应
func (e *EC600N) sendATCommand(command string) (string, error) {
	if e.port == nil {
		return "", fmt.Errorf("串口未连接")
	}

	if err := e.port.Flush(); err != nil {
		return "", fmt.Errorf("清空串口缓冲区失败: %w", err)
	}

	fullCommand := command + "\r\n"
	if _, err := e.port.Write([]byte(fullCommand)); err != nil {
		return "", fmt.Errorf("发送 AT 指令失败: %w", err)
	}

	time.Sleep(ATCommandTimeout)

	return e.readResponse()
}

// readResponse 读取串口响应
func (e *EC600N) readResponse() (string, error) {
	reader := bufio.NewReader(e.port)
	var response strings.Builder

	for i := 0; i < MaxResponseLines; i++ {
		line, err := reader.ReadString('\n')
		if err != nil {
			break
		}
		response.WriteString(line)

		// 响应结束标志
		if strings.Contains(line, "OK") || strings.Contains(line, "ERROR") {
			break
		}
	}

	return response.String(), nil
}

// CheckNetworkStatus 检查网络状态
func (e *EC600N) CheckNetworkStatus() (*NetworkStatus, error) {
	status := &NetworkStatus{
		Timestamp: time.Now(),
	}

	// 收集各项状态信息（忽略错误，尽可能收集可用信息）
	status.SignalStrength, _ = e.getSignalStrength()
	status.NetworkRegStatus, _ = e.getNetworkRegistrationStatus()
	status.SIMStatus, _ = e.getSIMStatus()
	status.OperatorName, _ = e.getOperatorName()
	status.IMEI, _ = e.getIMEI()

	return status, nil
}

// getSignalStrength 获取信号强度 (0-31, 99表示未知)
func (e *EC600N) getSignalStrength() (int, error) {
	response, err := e.sendATCommand("AT+CSQ")
	if err != nil {
		return 0, fmt.Errorf("发送 AT+CSQ 指令失败: %w", err)
	}

	matches := reCSQ.FindStringSubmatch(response)
	if len(matches) < 3 {
		return 0, fmt.Errorf("无法解析信号强度，响应: %s", response)
	}

	var signal int
	if _, err := fmt.Sscanf(matches[1], "%d", &signal); err != nil {
		return 0, fmt.Errorf("解析信号强度数值失败: %w", err)
	}

	return signal, nil
}

// getNetworkRegistrationStatus 获取网络注册状态
func (e *EC600N) getNetworkRegistrationStatus() (string, error) {
	response, err := e.sendATCommand("AT+CREG?")
	if err != nil {
		return "", fmt.Errorf("发送 AT+CREG 指令失败: %w", err)
	}

	matches := reCREG.FindStringSubmatch(response)
	if len(matches) < 2 {
		return "", fmt.Errorf("无法解析注册状态，响应: %s", response)
	}

	statusCode := matches[1]
	if status, ok := networkRegStatusMap[statusCode]; ok {
		return status, nil
	}

	return "未知状态", nil
}

// getSIMStatus 获取SIM卡状态
func (e *EC600N) getSIMStatus() (string, error) {
	response, err := e.sendATCommand("AT+CPIN?")
	if err != nil {
		return "", err
	}

	switch {
	case strings.Contains(response, "READY"):
		return "就绪", nil
	case strings.Contains(response, "SIM PIN"):
		return "需要PIN码", nil
	case strings.Contains(response, "SIM PUK"):
		return "需要PUK码", nil
	default:
		return "未知状态", nil
	}
}

// getOperatorName 获取运营商名称
func (e *EC600N) getOperatorName() (string, error) {
	response, err := e.sendATCommand("AT+COPS?")
	if err != nil {
		return "", err
	}

	matches := reCOPS.FindStringSubmatch(response)
	if len(matches) < 2 {
		return "", fmt.Errorf("无法解析运营商名称: %s", response)
	}

	return matches[1], nil
}

// getIMEI 获取设备 IMEI
func (e *EC600N) getIMEI() (string, error) {
	response, err := e.sendATCommand("AT+CGSN")
	if err != nil {
		return "", fmt.Errorf("发送 AT+CGSN 指令失败: %w", err)
	}

	// 解析 IMEI，去除空白字符
	lines := strings.Split(response, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if len(line) == IMEILength && reIMEI.MatchString(line) {
			return line, nil
		}
	}

	return "", fmt.Errorf("无法解析 IMEI，响应: %s", response)
}

// MakeCall 拨打电话
// 使用 ATD 指令拨打电话号码
func (e *EC600N) MakeCall(phoneNumber string) error {
	if e.port == nil {
		return fmt.Errorf("串口未连接")
	}

	// 清理电话号码，移除空格和特殊字符
	phoneNumber = strings.TrimSpace(phoneNumber)
	phoneNumber = strings.ReplaceAll(phoneNumber, "-", "")
	phoneNumber = strings.ReplaceAll(phoneNumber, " ", "")

	if phoneNumber == "" {
		return fmt.Errorf("电话号码不能为空")
	}

	// 发送拨号指令 ATD<number>;
	command := fmt.Sprintf("ATD%s;", phoneNumber)
	response, err := e.sendATCommand(command)
	if err != nil {
		return fmt.Errorf("发送拨号指令失败: %w", err)
	}

	// 检查响应
	if strings.Contains(response, "OK") || strings.Contains(response, "CONNECT") {
		zap.S().Infof("拨打电话成功: %s", phoneNumber)
		return nil
	}

	return fmt.Errorf("拨打电话失败，响应: %s", response)
}

// HangupCall 挂断电话
func (e *EC600N) HangupCall() error {
	response, err := e.sendATCommand("ATH")
	if err != nil {
		return fmt.Errorf("挂断电话失败: %w", err)
	}

	if !strings.Contains(response, "OK") {
		return fmt.Errorf("挂断电话失败，响应: %s", response)
	}

	return nil
}

// Close 关闭连接
func (e *EC600N) Close() error {
	e.connected = false
	if e.port != nil {
		return e.port.Close()
	}
	return nil
}

// IsConnected 检查连接状态
func (e *EC600N) IsConnected() bool {
	return e.connected
}

// StartNetworkMonitoring 启动网络监控并发送状态报告
func (e *EC600N) StartNetworkMonitoring() error {
	status, err := e.CheckNetworkStatus()
	if err != nil {
		zap.S().Errorf("检查网络状态失败: %v", err)
		message := fmt.Sprintf("EC600N 网络检查失败: %v\n时间: %s",
			err, time.Now().Format("2006-01-02 15:04:05"))
		if notifyErr := e.notify.SendToWechat(message); notifyErr != nil {
			zap.S().Errorf("发送网络异常通知失败: %v", notifyErr)
		}
		return fmt.Errorf("检查网络状态失败: %w", err)
	}

	statusText := "正常"
	if !e.isNetworkStatusNormal(status) {
		statusText = "异常"
	}

	message := fmt.Sprintf("EC600N 网络状态报告\n状态: %s\n%s",
		statusText, e.formatNetworkStatus(status))

	if err := e.notify.SendToWechat(message); err != nil {
		zap.S().Errorf("发送网络状态报告失败: %v", err)
		return fmt.Errorf("发送网络状态报告失败: %w", err)
	}

	return nil
}

// isNetworkStatusNormal 判断网络状态是否正常
// 正常条件：信号强度大于阈值、网络已注册本地网络、SIM 卡就绪
func (e *EC600N) isNetworkStatusNormal(status *NetworkStatus) bool {
	return status.SignalStrength > MinSignalStrength &&
		status.NetworkRegStatus == "已注册本地网络" &&
		status.SIMStatus == "就绪"
}

// formatNetworkStatus 格式化网络状态信息
func (e *EC600N) formatNetworkStatus(status *NetworkStatus) string {
	return fmt.Sprintf(`信号强度: %d
网络注册状态: %s
SIM卡状态: %s
运营商: %s
IMEI: %s
时间: %s`,
		status.SignalStrength,
		status.NetworkRegStatus,
		status.SIMStatus,
		status.OperatorName,
		status.IMEI,
		status.Timestamp.Format("2006-01-02 15:04:05"))
}

// ProvideEC600N 提供EC600N依赖注入
func ProvideEC600N() fx.Option {
	return fx.Provide(NewEC600N)
}
