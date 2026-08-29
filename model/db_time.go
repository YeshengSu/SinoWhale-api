package model

import (
	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

// GetDBTimestamp returns a UNIX timestamp from database time.
// Falls back to application time on error.
//
// ⚠️ 不得在 DB.Transaction 回调内调用：它通过全局 DB 取连接，在单连接
// 数据库（如测试用 sqlite :memory:）上会与外层事务互相等待而死锁。
// 事务内请使用 GetDBTimestampOn(tx)。
func GetDBTimestamp() int64 {
	return GetDBTimestampOn(DB)
}

// GetDBTimestampOn 是 GetDBTimestamp 的事务感知版本，复用传入 db/tx 的
// 连接执行时间查询，可安全在事务回调内使用。
func GetDBTimestampOn(db *gorm.DB) int64 {
	var ts int64
	var err error
	switch {
	case common.UsingMainDatabase(common.DatabaseTypePostgreSQL):
		err = db.Raw("SELECT EXTRACT(EPOCH FROM NOW())::bigint").Scan(&ts).Error
	case common.UsingMainDatabase(common.DatabaseTypeSQLite):
		err = db.Raw("SELECT strftime('%s','now')").Scan(&ts).Error
	default:
		err = db.Raw("SELECT UNIX_TIMESTAMP()").Scan(&ts).Error
	}
	if err != nil || ts <= 0 {
		return common.GetTimestamp()
	}
	return ts
}
