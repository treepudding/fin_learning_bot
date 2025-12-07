package storage

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"time"
)

// Message 消息结构
type Message struct {
	ID          int64
	ChatID      string
	MessageID   string
	SenderID    string
	SenderType  string
	Content     string
	MessageType string
	CreatedAt   time.Time
}

// SaveMessage 保存消息到数据库
func (s *Storage) SaveMessage(ctx context.Context, msg *Message) error {
	log.Printf("[Storage.SaveMessage] 开始保存: chat_id=%s, message_id=%s", msg.ChatID, msg.MessageID)
	
	// 先检查消息是否已存在
	var existingID int64
	checkQuery := `SELECT id FROM messages WHERE message_id = ?`
	err := s.db.QueryRowContext(ctx, checkQuery, msg.MessageID).Scan(&existingID)
	if err == nil {
		log.Printf("[Storage.SaveMessage] 消息已存在: message_id=%s, existing_id=%d, 将执行更新", msg.MessageID, existingID)
	} else if err != sql.ErrNoRows {
		log.Printf("[Storage.SaveMessage] 检查消息是否存在时出错: %v", err)
	} else {
		log.Printf("[Storage.SaveMessage] 消息不存在，将执行插入: message_id=%s", msg.MessageID)
	}
	
	query := `
		INSERT INTO messages (chat_id, message_id, sender_id, sender_type, content, message_type, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(message_id) DO UPDATE SET
			content = excluded.content,
			created_at = excluded.created_at
	`

	log.Printf("[Storage.SaveMessage] 执行SQL: chat_id=%s, message_id=%s, content_len=%d", 
		msg.ChatID, msg.MessageID, len(msg.Content))
	
	result, err := s.db.ExecContext(ctx, query,
		msg.ChatID,
		msg.MessageID,
		msg.SenderID,
		msg.SenderType,
		msg.Content,
		msg.MessageType,
		msg.CreatedAt,
	)

	if err != nil {
		log.Printf("[Storage.SaveMessage] SQL执行失败: error=%v", err)
		return fmt.Errorf("保存消息失败: %w", err)
	}

	// 获取影响的行数
	rowsAffected, _ := result.RowsAffected()
	log.Printf("[Storage.SaveMessage] SQL执行成功: rows_affected=%d, message_id=%s", rowsAffected, msg.MessageID)

	return nil
}

// GetMessagesByChatID 根据 chat_id 获取消息历史（按时间倒序）
func (s *Storage) GetMessagesByChatID(ctx context.Context, chatID string, limit int) ([]*Message, error) {
	if limit <= 0 {
		limit = 50 // 默认返回最近 50 条
	}

	query := `
		SELECT id, chat_id, message_id, sender_id, sender_type, content, message_type, created_at
		FROM messages
		WHERE chat_id = ?
		ORDER BY created_at DESC
		LIMIT ?
	`

	rows, err := s.db.QueryContext(ctx, query, chatID, limit)
	if err != nil {
		return nil, fmt.Errorf("查询消息失败: %w", err)
	}
	defer rows.Close()

	var messages []*Message
	for rows.Next() {
		var msg Message
		var createdAtStr string
		err := rows.Scan(
			&msg.ID,
			&msg.ChatID,
			&msg.MessageID,
			&msg.SenderID,
			&msg.SenderType,
			&msg.Content,
			&msg.MessageType,
			&createdAtStr,
		)
		if err != nil {
			return nil, fmt.Errorf("扫描消息失败: %w", err)
		}

		// 解析时间
		msg.CreatedAt, err = time.Parse("2006-01-02 15:04:05", createdAtStr)
		if err != nil {
			// 如果解析失败，使用当前时间
			msg.CreatedAt = time.Now()
		}

		messages = append(messages, &msg)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("遍历消息失败: %w", err)
	}

	// 反转顺序，使最早的消息在前
	for i, j := 0, len(messages)-1; i < j; i, j = i+1, j-1 {
		messages[i], messages[j] = messages[j], messages[i]
	}

	return messages, nil
}

// GetRecentMessagesByChatID 获取最近的消息（用于大模型上下文）
func (s *Storage) GetRecentMessagesByChatID(ctx context.Context, chatID string, limit int) ([]*Message, error) {
	return s.GetMessagesByChatID(ctx, chatID, limit)
}

// DeleteOldMessages 删除指定 chat_id 的旧消息，只保留最近的 N 条
func (s *Storage) DeleteOldMessages(ctx context.Context, chatID string, keepCount int) error {
	// 先获取要保留的消息 ID
	query := `
		SELECT id FROM messages
		WHERE chat_id = ?
		ORDER BY created_at DESC
		LIMIT ?
	`

	rows, err := s.db.QueryContext(ctx, query, chatID, keepCount)
	if err != nil {
		return fmt.Errorf("查询要保留的消息失败: %w", err)
	}
	defer rows.Close()

	var keepIDs []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return fmt.Errorf("扫描ID失败: %w", err)
		}
		keepIDs = append(keepIDs, id)
	}

	if err := rows.Err(); err != nil {
		return fmt.Errorf("遍历ID失败: %w", err)
	}

	// 如果没有需要保留的消息，删除所有
	if len(keepIDs) == 0 {
		deleteQuery := `DELETE FROM messages WHERE chat_id = ?`
		_, err = s.db.ExecContext(ctx, deleteQuery, chatID)
		return err
	}

	// 构建 IN 子句
	placeholders := ""
	args := []interface{}{chatID}
	for i, id := range keepIDs {
		if i > 0 {
			placeholders += ","
		}
		placeholders += "?"
		args = append(args, id)
	}

	deleteQuery := fmt.Sprintf(`
		DELETE FROM messages
		WHERE chat_id = ? AND id NOT IN (%s)
	`, placeholders)

	_, err = s.db.ExecContext(ctx, deleteQuery, args...)
	if err != nil {
		return fmt.Errorf("删除旧消息失败: %w", err)
	}

	return nil
}


