package main

// demo_seed_allowed_models.go — utility script that:
//  1. Promotes (or creates) a local admin user.
//  2. Updates one of the seeded agent plans to carry a small allowed_models
//     list so the desktop model's NewApi-bound picker has something to
//     filter against during demo runs.
//
// Run with:
//
//	cd D:/Chenxv/SinoWhale-api && go run ./cmd/demo-seed
//
// Defaults to username=admin / password=123456 and plan level=plus.

import (
	"encoding/json"
	"fmt"
	"log"
	"os"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
)

type allowedModel struct {
	Model string  `json:"model"`
	Ratio float64 `json:"ratio"`
}

func main() {
	os.Setenv("SQLITE_PATH", "one-api.db")
	common.RedisEnabled = false
	common.RDB = nil
	common.InitEnv()
	if err := model.InitDB(); err != nil {
		log.Fatalf("init db: %v", err)
	}

	adminUser := "admin"
	hashed, err := common.Password2Hash("123456")
	if err != nil {
		log.Fatalf("hash: %v", err)
	}

	// Upsert admin user
	res := model.DB.Model(&model.User{}).
		Where("username = ?", adminUser).
		Updates(map[string]interface{}{
			"password":     hashed,
			"role":         common.RoleAdminUser,
			"status":       common.UserStatusEnabled,
			"display_name": adminUser,
			"group":        "default",
		})
	if res.Error != nil {
		log.Fatalf("update admin: %v", res.Error)
	}
	if res.RowsAffected == 0 {
		if err := model.DB.Create(&model.User{
			Username:    adminUser,
			Password:    hashed,
			Role:        common.RoleAdminUser,
			Status:      common.UserStatusEnabled,
			DisplayName: adminUser,
			Group:       "default",
		}).Error; err != nil {
			log.Fatalf("create admin: %v", err)
		}
	}
	fmt.Println("[ok] admin user ensured (username=admin password=123456)")

	// Re-run the seed (idempotent) so the three base plans exist.
	if err := model.SeedAgentPlans(); err != nil {
		log.Fatalf("seed agent plans: %v", err)
	}
	fmt.Println("[ok] agent plans seeded")

	// Configure the "plus" plan with a sample allowed_models list.
	var plusPlan model.SubscriptionPlan
	if err := model.DB.Where("plan_level = ? AND tag = ?", "plus", model.PlanTagAgent).
		First(&plusPlan).Error; err != nil {
		log.Fatalf("query plus plan: %v", err)
	}
	plusAllowed := []allowedModel{
		{"anthropic/claude-opus-4.6", 1.0},
		{"anthropic/claude-sonnet-4-20250514", 1.0},
		{"openai/gpt-4.1", 1.0},
	}
	plusBlob, _ := json.Marshal(plusAllowed)
	if err := model.DB.Model(&plusPlan).
		Where("id = ?", plusPlan.Id).
		Update("allowed_models", string(plusBlob)).Error; err != nil {
		log.Fatalf("update plus plan: %v", err)
	}
	fmt.Printf("[ok] plus plan id=%d updated with %d allowed_models\n", plusPlan.Id, len(plusAllowed))

	// Also configure max with a richer list for demo purposes.
	var maxPlan model.SubscriptionPlan
	if err := model.DB.Where("plan_level = ? AND tag = ?", "max", model.PlanTagAgent).
		First(&maxPlan).Error; err != nil {
		log.Fatalf("query max plan: %v", err)
	}
	maxAllowed := []allowedModel{
		{"anthropic/claude-opus-4.6", 1.0},
		{"anthropic/claude-sonnet-4-20250514", 1.0},
		{"openai/gpt-4.1", 1.0},
		{"AtlasCloud/deepseek-ai/deepseek-v4-pro", 1.0},
		{"AtlasCloud/deepseek-ai/deepseek-v4-flash", 1.0},
		{"AtlasCloud/Qwen/Qwen3-235B-A22B-Instruct-2507", 1.0},
	}
	maxBlob, _ := json.Marshal(maxAllowed)
	if err := model.DB.Model(&maxPlan).
		Where("id = ?", maxPlan.Id).
		Update("allowed_models", string(maxBlob)).Error; err != nil {
		log.Fatalf("update max plan: %v", err)
	}
	fmt.Printf("[ok] max plan id=%d updated with %d allowed_models\n", maxPlan.Id, len(maxAllowed))

	fmt.Println("\nNext steps:")
	fmt.Println("  1. Restart the backend (it picks up DB edits automatically).")
	fmt.Println("  2. In the desktop, log in as admin to access /api/subscription/admin/*")
	fmt.Println("      and bind a subscription to a user via AdminBindSubscription.")
	fmt.Println("  3. The chat model's list will then be filtered to that plan's allow-list.")
}