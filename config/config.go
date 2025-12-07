package config

import (
	"log"
	"os"

	"github.com/joho/godotenv"
)

// Config 存储应用程序配置
type Config struct {
	// 飞书应用配置
	AppID     string
	AppSecret string
	
	// 其他配置
	APIKey      string
	SecretKey   string
	DatabaseURL string
	DatabasePath string // SQLite 数据库文件路径
	AppEnv      string
	Port        string
}

// Load 加载环境变量配置
func Load() *Config {
	// 尝试加载 .env 文件（如果存在）
	// 如果文件不存在也不会报错，因为可能使用系统环境变量
	if err := godotenv.Load(); err != nil {
		log.Println("未找到 .env 文件，将使用系统环境变量")
	}

	return &Config{
		// 飞书应用配置
		AppID:     getEnv("APP_ID", ""),
		AppSecret: getEnv("APP_SECRET", ""),
		
		// 其他配置
		APIKey:       getEnv("API_KEY", ""),
		SecretKey:    getEnv("SECRET_KEY", ""),
		DatabaseURL:  getEnv("DATABASE_URL", ""),
		DatabasePath: getEnv("DATABASE_PATH", "data/fin_bot.db"), // 默认数据库路径
		AppEnv:       getEnv("APP_ENV", "development"),
		Port:         getEnv("PORT", "8080"),
	}
}

// getEnv 获取环境变量，如果不存在则返回默认值
func getEnv(key, defaultValue string) string {
	value := os.Getenv(key)
	if value == "" {
		return defaultValue
	}
	return value
}

