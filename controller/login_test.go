package controller

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

const (
	loginTestPhone             = "13800138000"
	loginTestUnregisteredPhone = "13900139000"
	loginTestUsername          = "alice"
	loginTestPassword          = "correct-horse-battery"
	loginTestSmsCode           = "654321"
)

type loginAPIResponse struct {
	Success bool            `json:"success"`
	Message string          `json:"message"`
	Code    string          `json:"code"`
	Data    json.RawMessage `json:"data"`
}

func setupLoginTestRouter(t *testing.T) (*gin.Engine, *gorm.DB) {
	t.Helper()

	gin.SetMode(gin.TestMode)
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	common.RedisEnabled = false
	common.PasswordLoginEnabled = true
	common.AgentModeEnabled = false

	// 重置 per-phone 登录失败锁定器，隔离用例间状态
	phoneLoginLimiter = newPhoneLoginLimiter()

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err, "failed to open sqlite db")
	model.DB = db
	model.LOG_DB = db
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.Token{}, &model.Log{}, &model.TwoFA{}), "failed to migrate tables")

	t.Cleanup(func() {
		sqlDB, err := db.DB()
		if err == nil {
			_ = sqlDB.Close()
		}
	})

	router := gin.New()
	router.Use(sessions.Sessions("session", cookie.NewStore([]byte("login-controller-test"))))
	router.POST("/api/user/login", Login)
	router.POST("/api/user/register", Register)
	return router, db
}

func seedLoginUser(t *testing.T, db *gorm.DB, username, phone, password string) *model.User {
	t.Helper()

	hash, err := common.Password2Hash(password)
	require.NoError(t, err, "failed to hash password")
	user := &model.User{
		Username:    username,
		DisplayName: username,
		Password:    hash,
		Phone:       phone,
		Status:      common.UserStatusEnabled,
		Role:        common.RoleCommonUser,
		Group:       "default",
	}
	require.NoError(t, db.Create(user).Error, "failed to seed user")
	return user
}

func registerLoginSmsCode(t *testing.T, phone, code string) {
	t.Helper()
	common.RegisterVerificationCodeWithKey(phone, code, common.SmsLoginPurpose)
}

func performLogin(t *testing.T, router *gin.Engine, body map[string]any) (*httptest.ResponseRecorder, loginAPIResponse) {
	t.Helper()

	payload, err := common.Marshal(body)
	require.NoError(t, err, "failed to marshal login body")
	req := httptest.NewRequest(http.MethodPost, "/api/user/login", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	var resp loginAPIResponse
	require.NoError(t, common.Unmarshal(rec.Body.Bytes(), &resp), "failed to decode login response: %s", rec.Body.String())
	return rec, resp
}

func requireLoginFailedWithCode(t *testing.T, resp loginAPIResponse, wantCode string) {
	t.Helper()
	require.False(t, resp.Success, "expected failed login, got success")
	require.Equal(t, wantCode, resp.Code, "unexpected error code, message: %s", resp.Message)
}

// TestLoginMethodRoutingMatrix 覆盖统一登录三方式判定矩阵（PRD 4.1）与错误码字段。
func TestLoginMethodRoutingMatrix(t *testing.T) {
	cases := []struct {
		name     string
		body     map[string]any
		smsCode  string // 非空时预先注册该短信验证码
		wantCode string // "" 表示登录成功
	}{
		{
			name:    "phone sms password routes to sms login",
			body:    map[string]any{"phone": loginTestPhone, "sms_code": loginTestSmsCode, "password": loginTestPassword},
			smsCode: loginTestSmsCode,
		},
		{
			name: "phone password routes to phone password login",
			body: map[string]any{"phone": loginTestPhone, "password": loginTestPassword},
		},
		{
			name: "username password routes to username login",
			body: map[string]any{"username": loginTestUsername, "password": loginTestPassword},
		},
		{
			name:     "sms_code without phone is invalid_params even when username looks like a phone",
			body:     map[string]any{"sms_code": loginTestSmsCode, "username": loginTestPhone, "password": loginTestPassword},
			wantCode: "invalid_params",
		},
		{
			name:     "phone with invalid format is phone_invalid",
			body:     map[string]any{"phone": "12345", "password": loginTestPassword},
			wantCode: "phone_invalid",
		},
		{
			name:     "phone sms with wrong code is verification_code_error",
			body:     map[string]any{"phone": loginTestPhone, "sms_code": "000000", "password": loginTestPassword},
			smsCode:  loginTestSmsCode,
			wantCode: "verification_code_error",
		},
		{
			name:     "phone sms without registered code is verification_code_error",
			body:     map[string]any{"phone": loginTestPhone, "sms_code": "111111", "password": loginTestPassword},
			wantCode: "verification_code_error",
		},
		{
			name:     "unregistered phone password is invalid_credentials",
			body:     map[string]any{"phone": loginTestUnregisteredPhone, "password": loginTestPassword},
			wantCode: "invalid_credentials",
		},
		{
			name:     "registered phone with wrong password is invalid_credentials",
			body:     map[string]any{"phone": loginTestPhone, "password": "totally-wrong"},
			wantCode: "invalid_credentials",
		},
		{
			name:     "wrong username is invalid_credentials",
			body:     map[string]any{"username": "ghost", "password": loginTestPassword},
			wantCode: "invalid_credentials",
		},
		{
			name:     "empty body is invalid_params",
			body:     map[string]any{},
			wantCode: "invalid_params",
		},
		{
			name:     "username without password is invalid_params",
			body:     map[string]any{"username": loginTestUsername},
			wantCode: "invalid_params",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			router, db := setupLoginTestRouter(t)
			seedLoginUser(t, db, loginTestUsername, loginTestPhone, loginTestPassword)
			if tc.smsCode != "" {
				registerLoginSmsCode(t, loginTestPhone, tc.smsCode)
			}

			rec, resp := performLogin(t, router, tc.body)
			if tc.wantCode == "" {
				require.True(t, resp.Success, "expected successful login, got code=%q message=%q", resp.Code, resp.Message)
				assert.NotContains(t, rec.Body.String(), "api_key", "universal login must not return agent api_key extras")
				return
			}
			requireLoginFailedWithCode(t, resp, tc.wantCode)
		})
	}
}

// TestLoginSmsFlowKeepsPhoneNotFound 验证码方式保留 phone_not_found（PRD 4.2：发码阶段已隐式确认号码存在）。
func TestLoginSmsFlowKeepsPhoneNotFound(t *testing.T) {
	router, _ := setupLoginTestRouter(t)
	registerLoginSmsCode(t, loginTestUnregisteredPhone, loginTestSmsCode)

	_, resp := performLogin(t, router, map[string]any{
		"phone":    loginTestUnregisteredPhone,
		"sms_code": loginTestSmsCode,
		"password": loginTestPassword,
	})
	requireLoginFailedWithCode(t, resp, "phone_not_found")
}

// TestLoginSmsSuccessDestroysVerificationCode 登录成功后验证码销毁，不可复用。
func TestLoginSmsSuccessDestroysVerificationCode(t *testing.T) {
	router, db := setupLoginTestRouter(t)
	seedLoginUser(t, db, loginTestUsername, loginTestPhone, loginTestPassword)
	registerLoginSmsCode(t, loginTestPhone, loginTestSmsCode)

	_, first := performLogin(t, router, map[string]any{
		"phone":    loginTestPhone,
		"sms_code": loginTestSmsCode,
		"password": loginTestPassword,
	})
	require.True(t, first.Success, "first sms login should succeed, message: %s", first.Message)

	_, second := performLogin(t, router, map[string]any{
		"phone":    loginTestPhone,
		"sms_code": loginTestSmsCode,
		"password": loginTestPassword,
	})
	requireLoginFailedWithCode(t, second, "verification_code_error")
}

// TestLoginPasswordLoginDisabledGate 密码登录关闭时的机器可读错误码。
func TestLoginPasswordLoginDisabledGate(t *testing.T) {
	router, _ := setupLoginTestRouter(t)
	common.PasswordLoginEnabled = false
	t.Cleanup(func() { common.PasswordLoginEnabled = true })

	_, resp := performLogin(t, router, map[string]any{"username": loginTestUsername, "password": loginTestPassword})
	requireLoginFailedWithCode(t, resp, "password_login_disabled")
}

// TestLoginPerPhoneLockout 覆盖 per-phone 双层限流第二层全场景（PRD 4.5）。
func TestLoginPerPhoneLockout(t *testing.T) {
	t.Run("locks after limit failures even with correct password", func(t *testing.T) {
		router, db := setupLoginTestRouter(t)
		seedLoginUser(t, db, loginTestUsername, loginTestPhone, loginTestPassword)

		for i := 0; i < phoneLoginFailLimit; i++ {
			_, resp := performLogin(t, router, map[string]any{"phone": loginTestPhone, "password": "totally-wrong"})
			requireLoginFailedWithCode(t, resp, "invalid_credentials")
		}

		_, locked := performLogin(t, router, map[string]any{"phone": loginTestPhone, "password": loginTestPassword})
		requireLoginFailedWithCode(t, locked, "account_temporarily_locked")
	})

	t.Run("sms login still allowed while locked", func(t *testing.T) {
		router, db := setupLoginTestRouter(t)
		seedLoginUser(t, db, loginTestUsername, loginTestPhone, loginTestPassword)

		for i := 0; i < phoneLoginFailLimit; i++ {
			_, _ = performLogin(t, router, map[string]any{"phone": loginTestPhone, "password": "totally-wrong"})
		}
		registerLoginSmsCode(t, loginTestPhone, loginTestSmsCode)

		_, resp := performLogin(t, router, map[string]any{
			"phone":    loginTestPhone,
			"sms_code": loginTestSmsCode,
			"password": loginTestPassword,
		})
		require.True(t, resp.Success, "sms login must be allowed during lock, got code=%q message=%q", resp.Code, resp.Message)
	})

	t.Run("username login unaffected by phone lock", func(t *testing.T) {
		router, db := setupLoginTestRouter(t)
		seedLoginUser(t, db, loginTestUsername, loginTestPhone, loginTestPassword)

		for i := 0; i < phoneLoginFailLimit; i++ {
			_, _ = performLogin(t, router, map[string]any{"phone": loginTestPhone, "password": "totally-wrong"})
		}

		_, resp := performLogin(t, router, map[string]any{"username": loginTestUsername, "password": loginTestPassword})
		require.True(t, resp.Success, "username login must be unaffected by phone lock, got code=%q", resp.Code)
	})

	t.Run("successful login clears failure counter", func(t *testing.T) {
		router, db := setupLoginTestRouter(t)
		seedLoginUser(t, db, loginTestUsername, loginTestPhone, loginTestPassword)

		for i := 0; i < 2; i++ {
			_, _ = performLogin(t, router, map[string]any{"phone": loginTestPhone, "password": "totally-wrong"})
		}
		_, ok := performLogin(t, router, map[string]any{"phone": loginTestPhone, "password": loginTestPassword})
		require.True(t, ok.Success, "correct login should succeed after failures, code=%q", ok.Code)

		// 清零后需要再错满 phoneLoginFailLimit 次才锁定
		for i := 0; i < phoneLoginFailLimit; i++ {
			_, resp := performLogin(t, router, map[string]any{"phone": loginTestPhone, "password": "totally-wrong"})
			requireLoginFailedWithCode(t, resp, "invalid_credentials")
		}
		_, locked := performLogin(t, router, map[string]any{"phone": loginTestPhone, "password": loginTestPassword})
		requireLoginFailedWithCode(t, locked, "account_temporarily_locked")
	})

	t.Run("lock expires after ttl", func(t *testing.T) {
		origTTL := phoneLoginFailTTL
		phoneLoginFailTTL = 0
		t.Cleanup(func() { phoneLoginFailTTL = origTTL })

		router, db := setupLoginTestRouter(t)
		seedLoginUser(t, db, loginTestUsername, loginTestPhone, loginTestPassword)

		for i := 0; i < phoneLoginFailLimit+1; i++ {
			_, resp := performLogin(t, router, map[string]any{"phone": loginTestPhone, "password": "totally-wrong"})
			requireLoginFailedWithCode(t, resp, "invalid_credentials")
		}
		_, ok := performLogin(t, router, map[string]any{"phone": loginTestPhone, "password": loginTestPassword})
		require.True(t, ok.Success, "expired lock must allow login, code=%q message=%q", ok.Code, ok.Message)
	})
}

// TestRegisterUniversalUsernamePassword 注册回归通用：用户名+密码注册成功，
// 不再返回 agent api_key，且用户已落库（PRD 7.3）。
func TestRegisterUniversalUsernamePassword(t *testing.T) {
	router, db := setupLoginTestRouter(t)
	common.RegisterEnabled = true
	common.PasswordRegisterEnabled = true

	// 注册页强制手机验证：需手机号 + 预置验证码
	seedRegisterCode("13600136000", registerTestSmsCode)
	payload, err := common.Marshal(map[string]any{
		"username": "newuser",
		"password": "password123",
		"phone":    "13600136000",
		"sms_code": registerTestSmsCode,
	})
	require.NoError(t, err, "failed to marshal register body")
	req := httptest.NewRequest(http.MethodPost, "/api/user/register", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, "register response: %s", rec.Body.String())
	assert.NotContains(t, rec.Body.String(), "api_key", "universal register must not return agent api_key extras")
	require.Contains(t, rec.Body.String(), `"success":true`, "register should succeed: %s", rec.Body.String())

	var stored model.User
	require.NoError(t, db.First(&stored, "username = ?", "newuser").Error, "registered user should be persisted")
	require.Equal(t, "13600136000", stored.Phone, "phone should be persisted")
}
