package service

import (
	"context"
	"fmt"
	"log"
	"sync"

	lark "github.com/larksuite/oapi-sdk-go/v3"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
)

// RecentChat 最近交互的会话信息
type RecentChat struct {
	ChatID   string
	ChatType string // "p2p" 或 "group"
}

// LarkService 飞书服务
type LarkService struct {
	client     *lark.Client
	recentChat *RecentChat
	mu         sync.RWMutex // 保护 recentChat 的并发访问
}

// NewLarkService 创建新的飞书服务实例
func NewLarkService(appID, appSecret string) *LarkService {
	client := lark.NewClient(appID, appSecret)
	return &LarkService{
		client: client,
	}
}

// SendTextMessage 发送文本消息
// receiveID: 接收者的ID（可以是 open_id, user_id, chat_id 等）
// receiveIDType: 接收者ID类型，如 "open_id", "user_id", "chat_id"
// content: 消息内容
func (s *LarkService) SendTextMessage(ctx context.Context, receiveID, receiveIDType, content string) error {
	// 验证并规范化 receiveIDType
	var receiveIDTypeStr string
	switch receiveIDType {
	case "open_id", "user_id", "union_id", "email", "chat_id":
		receiveIDTypeStr = receiveIDType
	default:
		receiveIDTypeStr = larkim.ReceiveIdTypeOpenId // 默认使用 open_id
	}

	// 构建消息内容
	msgContent := larkim.NewTextMsgBuilder().
		TextLine(content).
		Build()

	// 发送消息
	resp, err := s.client.Im.Message.Create(ctx, larkim.NewCreateMessageReqBuilder().
		ReceiveIdType(receiveIDTypeStr).
		Body(larkim.NewCreateMessageReqBodyBuilder().
			MsgType(larkim.MsgTypeText).
			ReceiveId(receiveID).
			Content(msgContent).
			Build()).
		Build())

	if err != nil {
		return fmt.Errorf("发送消息失败: %w", err)
	}

	if !resp.Success() {
		return fmt.Errorf("发送消息失败: code=%d, msg=%s, request_id=%s", 
			resp.Code, resp.Msg, resp.RequestId())
	}

	log.Printf("消息发送成功: message_id=%s", *resp.Data.MessageId)
	return nil
}

// GetClient 获取 Lark 客户端（用于其他需要直接使用 client 的场景）
func (s *LarkService) GetClient() *lark.Client {
	return s.client
}

// UpdateRecentChat 更新最近交互的会话信息（当收到用户消息时调用）
func (s *LarkService) UpdateRecentChat(chatID, chatType string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.recentChat = &RecentChat{
		ChatID:   chatID,
		ChatType: chatType,
	}
	log.Printf("更新最近交互会话: chat_id=%s, chat_type=%s", chatID, chatType)
}

// GetRecentChat 获取最近交互的会话信息
func (s *LarkService) GetRecentChat() *RecentChat {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.recentChat
}

// GetChatList 获取机器人已加入的所有群聊列表
func (s *LarkService) GetChatList(ctx context.Context) ([]string, error) {
	var chatIDs []string
	pageToken := ""
	pageSize := 50

	for {
		req := larkim.NewListChatReqBuilder().
			UserIdType(larkim.UserIdTypeListChatUserId).
			PageSize(pageSize)

		if pageToken != "" {
			req.PageToken(pageToken)
		}

		resp, err := s.client.Im.Chat.List(ctx, req.Build())
		if err != nil {
			return nil, fmt.Errorf("获取群聊列表失败: %w", err)
		}

		if !resp.Success() {
			return nil, fmt.Errorf("获取群聊列表失败: code=%d, msg=%s", resp.Code, resp.Msg)
		}

		// 提取群聊ID（ListChat API 返回的都是群聊）
		for _, chat := range resp.Data.Items {
			if chat.ChatId != nil {
				chatIDs = append(chatIDs, *chat.ChatId)
			}
		}

		// 检查是否还有更多数据
		if resp.Data.HasMore != nil && *resp.Data.HasMore && resp.Data.PageToken != nil {
			pageToken = *resp.Data.PageToken
		} else {
			break
		}
	}

	log.Printf("获取到 %d 个群聊", len(chatIDs))
	return chatIDs, nil
}

// SendMessageToAllChats 向所有群聊发送消息
func (s *LarkService) SendMessageToAllChats(ctx context.Context, content string) (map[string]interface{}, error) {
	// 获取所有群聊
	chatIDs, err := s.GetChatList(ctx)
	if err != nil {
		return nil, fmt.Errorf("获取群聊列表失败: %w", err)
	}

	if len(chatIDs) == 0 {
		return map[string]interface{}{
			"total":   0,
			"success": 0,
			"failed":  0,
			"results": []string{},
		}, nil
	}

	// 统计结果
	successCount := 0
	failedCount := 0
	results := []map[string]interface{}{}

	// 遍历所有群聊并发送消息
	for _, chatID := range chatIDs {
		err := s.SendTextMessage(ctx, chatID, "chat_id", content)
		if err != nil {
			failedCount++
			results = append(results, map[string]interface{}{
				"chat_id": chatID,
				"status":  "failed",
				"error":   err.Error(),
			})
			log.Printf("向群聊 %s 发送消息失败: %v", chatID, err)
		} else {
			successCount++
			results = append(results, map[string]interface{}{
				"chat_id": chatID,
				"status":  "success",
			})
			log.Printf("成功向群聊 %s 发送消息", chatID)
		}
	}

	return map[string]interface{}{
		"total":   len(chatIDs),
		"success": successCount,
		"failed":  failedCount,
		"results": results,
	}, nil
}


