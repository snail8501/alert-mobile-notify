package api

import (
	"alert-mobile-notify/config"
	"alert-mobile-notify/notification"
	"bytes"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/stretchr/testify/assert"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"
	"time"
)

// TestHandleNotify_MillisecondTimestamp 测试毫秒级时间戳
func TestHandleNotify_MillisecondTimestamp(t *testing.T) {

	cfg, err := config.LoadConfig("../config.yaml")
	assert.NoError(t, err)

	config.InitLogger(cfg)
	notify := notification.NewWechatNotify(cfg)
	server := NewHTTPServer(cfg, nil, notify)

	// 创建HTTP测试服务器
	ts := httptest.NewServer(server.server.Handler)
	defer ts.Close()

	// 准备请求数据
	timestamp := fmt.Sprintf("%d", time.Now().UnixMilli())
	signature := generateSignature("test", "13800138000", timestamp, cfg.API.SecretKey)

	reqBody := NotifyRequest{
		Name:         "test",
		PhoneNumbers: "13800138000",
		Timestamp:    timestamp,
		Signature:    signature,
	}

	body, _ := json.Marshal(reqBody)

	// 直接发送HTTP请求
	resp, err := http.Post(ts.URL+"/api/nofity", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("HTTP请求失败: %v", err)
	}
	defer resp.Body.Close()

	// 验证响应
	if resp.StatusCode != http.StatusOK {
		t.Errorf("期望状态码 %d, 得到 %d", http.StatusOK, resp.StatusCode)
	}

	var response NotifyResponse
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		t.Fatalf("解析响应失败: %v", err)
	}

	if !response.Success {
		t.Error("期望 Success 为 true")
	}
}

func TestHttpNotify_MillisecondTimestamp(t *testing.T) {

	// 准备请求数据
	secretKey := "k7mP9xQrT2vN8sL4wY6a"
	timestamp := fmt.Sprintf("%d", time.Now().UnixMilli())
	signature := generateSignature("test", "13817795074", timestamp, secretKey)

	reqBody := NotifyRequest{
		Name:         "test",
		PhoneNumbers: "13817795074",
		Timestamp:    timestamp,
		Signature:    signature,
	}

	body, _ := json.Marshal(reqBody)

	// 直接发送HTTP请求
	resp, err := http.Post("http://10.10.0.23:3124/api/nofity", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("HTTP请求失败: %v", err)
	}
	defer resp.Body.Close()

	// 验证响应
	if resp.StatusCode != http.StatusOK {
		t.Errorf("期望状态码 %d, 得到 %d", http.StatusOK, resp.StatusCode)
	}

	var response NotifyResponse
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		t.Fatalf("解析响应失败: %v", err)
	}

	if !response.Success {
		t.Error("期望 Success 为 true")
	}
}

// generateSignature 生成签名（用于测试）
func generateSignature(name, phoneNumbers, timestamp, secretKey string) string {
	params := map[string]string{
		"name":         name,
		"phoneNumbers": phoneNumbers,
		"secretKey":    secretKey,
		"timestamp":    timestamp,
	}

	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var parts []string
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s=%s", k, params[k]))
	}
	signString := strings.Join(parts, "&")

	hash := md5.Sum([]byte(signString))
	return hex.EncodeToString(hash[:])
}
