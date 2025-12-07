package handler

import (
	"context"
	"fin_bot/service"

	"github.com/cloudwego/hertz/pkg/app"
)

// MessageHandler 消息处理器
type MessageHandler struct {
	larkService *service.LarkService
}

// NewMessageHandler 创建新的消息处理器
func NewMessageHandler(larkService *service.LarkService) *MessageHandler {
	return &MessageHandler{
		larkService: larkService,
	}
}

// SendMessage 发送消息的 HTTP 接口
// 该接口会找到所有已加入的群聊，并向每个群聊发送 "helloworld" 消息
func (h *MessageHandler) SendMessage(ctx context.Context, c *app.RequestContext) {
	// 固定发送 "helloworld" 消息
	content := "helloworld"

	// 向所有群聊发送消息
	result, err := h.larkService.SendMessageToAllChats(ctx, content)
	if err != nil {
		c.JSON(500, map[string]interface{}{
			"code":    500,
			"message": "发送消息失败",
			"error":   err.Error(),
		})
		return
	}

	// 返回成功响应
	c.JSON(200, map[string]interface{}{
		"code":    200,
		"message": "消息发送完成",
		"data":    result,
	})
}


