package storage

import (
	"database/sql"
	"fmt"
	"log"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

// Storage 数据库存储接口
type Storage struct {
	db *sql.DB
}

// NewStorage 创建新的存储实例
func NewStorage(dbPath string) (*Storage, error) {
	// 确保数据库目录存在
	dir := filepath.Dir(dbPath)
	if dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return nil, fmt.Errorf("创建数据库目录失败: %w", err)
		}
	}

	// 打开数据库连接
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("打开数据库失败: %w", err)
	}

	// 测试连接
	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("数据库连接测试失败: %w", err)
	}

	storage := &Storage{db: db}

	// 初始化数据库表
	if err := storage.initTables(); err != nil {
		db.Close()
		return nil, fmt.Errorf("初始化数据库表失败: %w", err)
	}

	log.Printf("数据库初始化成功: %s", dbPath)
	return storage, nil
}

// Close 关闭数据库连接
func (s *Storage) Close() error {
	if s.db != nil {
		return s.db.Close()
	}
	return nil
}

// GetDB 获取数据库连接（用于直接操作数据库）
func (s *Storage) GetDB() *sql.DB {
	return s.db
}

// initTables 初始化数据库表
func (s *Storage) initTables() error {
	// 创建消息表
	createMessagesTable := `
	CREATE TABLE IF NOT EXISTS messages (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		chat_id TEXT NOT NULL,
		message_id TEXT NOT NULL UNIQUE,
		sender_id TEXT,
		sender_type TEXT,
		content TEXT NOT NULL,
		message_type TEXT,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);
	`

	if _, err := s.db.Exec(createMessagesTable); err != nil {
		return fmt.Errorf("创建消息表失败: %w", err)
	}

	// 创建索引（SQLite 需要单独创建索引）
	createIndexes := []string{
		`CREATE INDEX IF NOT EXISTS idx_chat_id ON messages(chat_id);`,
		`CREATE INDEX IF NOT EXISTS idx_created_at ON messages(created_at);`,
	}

	for _, indexSQL := range createIndexes {
		if _, err := s.db.Exec(indexSQL); err != nil {
			return fmt.Errorf("创建索引失败: %w", err)
		}
	}

	log.Println("数据库表初始化完成")
	return nil
}


