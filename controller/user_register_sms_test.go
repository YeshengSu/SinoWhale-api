package controller

import (
	"bytes"
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
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

const (
	registerTestPhone   = "13700137000"
	registerTestSmsCode = "246810"
)

// setupRegisterTestRouter 与 setupLoginTestRouter 同构：独立内存库 + 注册路由。
func setupRegisterTestRouter(t *testing.T) (*gin.Engine, *gorm.DB) {
	t.Helper()

	gin.SetMode(gin.TestMode)
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	common.RedisEnabled = false
	common.RegisterEnabled = true
	common.PasswordRegisterEnabled = true

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err, "failed to open sqlite db")
	model.DB = db
	model.LOG_DB = db
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.Token{}, &model.Log{}), "failed to migrate tables")

	t.Cleanup(func() {
		sqlDB, err := db.DB()
		if err == nil {
			_ = sqlDB.Close()
		}
	})

	router := gin.New()
	router.Use(sessions.Sessions("session", cookie.NewStore([]byte("register-controller-test"))))
	router.POST("/api/user/register", Register)
	return router, db
}

func seedRegisterCode(phone, code string) {
	common.RegisterVerificationCodeWithKey(phone, code, common.SmsVerificationPurpose)
}

func postRegister(t *testing.T, router *gin.Engine, body map[string]any) loginAPIResponse {
	t.Helper()
	payload, err := common.Marshal(body)
	require.NoError(t, err, "failed to marshal register body")
	req := httptest.NewRequest(http.MethodPost, "/api/user/register", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	var resp loginAPIResponse
	require.NoError(t, common.Unmarshal(rec.Body.Bytes(), &resp), "failed to decode register response: %s", rec.Body.String())
	return resp
}

// TestRegisterForcesPhoneVerification 覆盖注册页强制手机验证的错误矩阵：
// 缺字段 → phone_verification_required；格式错 → phone_invalid；
// 验证码错 → verification_code_error；手机号被占 → phone_already_taken。
func TestRegisterForcesPhoneVerification(t *testing.T) {
	cases := []struct {
		name      string
		body      map[string]any
		seedPhone string
		takenUser string
		wantCode  string
	}{
		{
			name:     "missing sms_code is phone_verification_required",
			body:     map[string]any{"username": "u1", "password": "password123", "phone": registerTestPhone},
			wantCode: "phone_verification_required",
		},
		{
			name:     "missing phone is phone_verification_required",
			body:     map[string]any{"username": "u2", "password": "password123", "sms_code": registerTestSmsCode},
			wantCode: "phone_verification_required",
		},
		{
			name:     "invalid cn phone is phone_invalid",
			body:     map[string]any{"username": "u3", "password": "password123", "phone": "12345", "sms_code": registerTestSmsCode},
			wantCode: "phone_invalid",
		},
		{
			name:     "wrong code is verification_code_error",
			body:     map[string]any{"username": "u4", "password": "password123", "phone": registerTestPhone, "sms_code": "000000"},
			wantCode: "verification_code_error",
		},
		{
			name:      "taken phone is phone_already_taken",
			body:      map[string]any{"username": "u5", "password": "password123", "phone": registerTestPhone, "sms_code": registerTestSmsCode},
			seedPhone: registerTestPhone,
			takenUser: "takenuser",
			wantCode:  "phone_already_taken",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			router, db := setupRegisterTestRouter(t)
			if tc.seedPhone != "" {
				// 预置占用该手机号的用户（EnsurePhoneAvailable 查重依据）
				hash, err := common.Password2Hash("password123")
				require.NoError(t, err)
				require.NoError(t, db.Create(&model.User{
					Username: tc.takenUser,
					Password: hash,
					Phone:    tc.seedPhone,
				}).Error)
			}
			seedRegisterCode(tc.seedPhone, registerTestSmsCode)

			resp := postRegister(t, router, tc.body)
			require.False(t, resp.Success, "expected failed register, got success")
			require.Equal(t, tc.wantCode, resp.Code, "unexpected error code, message: %s", resp.Message)
		})
	}
}

// TestRegisterSmsSuccessBurnsCode 验证正确验证码注册成功、手机号入库且验证码
// 被销毁（同码重放被拒）。
func TestRegisterSmsSuccessBurnsCode(t *testing.T) {
	router, db := setupRegisterTestRouter(t)
	seedRegisterCode(registerTestPhone, registerTestSmsCode)

	resp := postRegister(t, router, map[string]any{
		"username": "smsuser",
		"password": "password123",
		"phone":    registerTestPhone,
		"sms_code": registerTestSmsCode,
	})
	require.True(t, resp.Success, "register should succeed: %s", resp.Message)

	var stored model.User
	require.NoError(t, db.First(&stored, "username = ?", "smsuser").Error, "user should be persisted")
	require.Equal(t, registerTestPhone, stored.Phone, "phone should be persisted")

	// 验证码已被烧毁：换个用户名用同一手机号 + 验证码重放必须被拒
	replay := postRegister(t, router, map[string]any{
		"username": "smsuser2",
		"password": "password123",
		"phone":    registerTestPhone,
		"sms_code": registerTestSmsCode,
	})
	require.False(t, replay.Success, "burned code must not be reusable")
	require.Equal(t, "verification_code_error", replay.Code)
}
