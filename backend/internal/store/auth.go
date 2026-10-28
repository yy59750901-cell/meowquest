package store

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// NewToken 随机设备 token（孩子端免登录，cookie 1 年）
func NewToken() string {
	b := make([]byte, 24)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// SaveToken 签发并登记
func (d *DB) SaveToken(token string) error {
	return d.Where(DeviceToken{Token: token}).
		FirstOrCreate(&DeviceToken{Token: token}).Error
}

// ValidToken 校验孩子端 token
func (d *DB) ValidToken(token string) bool {
	if token == "" {
		return false
	}
	var n int64
	d.Model(&DeviceToken{}).Where("token=?", token).Count(&n)
	return n > 0
}

// ===== 家长端：无状态 HMAC token（服务重启不失效） =====
// token = 过期时间戳.签名(exp, secret)[:16]，24 小时有效
var adminSecret = []byte("meowquest-admin-v1") // 家用固定密钥；生产用环境变量覆盖思路：改此常量

func NewAdminToken() string {
	exp := time.Now().Add(24 * time.Hour).Unix()
	msg := strconv.FormatInt(exp, 10)
	mac := hmac.New(sha256.New, adminSecret)
	mac.Write([]byte(msg))
	return fmt.Sprintf("%s.%s", msg, hex.EncodeToString(mac.Sum(nil))[:16])
}

func AdminLogin(token string) bool {
	parts := strings.SplitN(token, ".", 2)
	if len(parts) != 2 {
		return false
	}
	exp, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || time.Now().Unix() > exp {
		return false
	}
	mac := hmac.New(sha256.New, adminSecret)
	mac.Write([]byte(parts[0]))
	return hex.EncodeToString(mac.Sum(nil))[:16] == parts[1]
}
