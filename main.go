package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"fin_bot/config"
	"fin_bot/handler"
	"fin_bot/service"
	"fin_bot/storage"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/app/server"
	lark "github.com/larksuite/oapi-sdk-go/v3"
	larkcore "github.com/larksuite/oapi-sdk-go/v3/core"
	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
	larkws "github.com/larksuite/oapi-sdk-go/v3/ws"
)

func main() {
	// 加载配置（从 .env 文件或系统环境变量）
	cfg := config.Load()

	// 检查必要的配置
	if cfg.AppID == "" || cfg.AppSecret == "" {
		log.Fatal("错误: APP_ID 和 APP_SECRET 必须设置（请在 .env 文件中配置）")
	}

	fmt.Printf("正在启动飞书机器人服务...\n")
	fmt.Printf("App ID: %s\n", maskString(cfg.AppID))

	// 初始化数据库
	dbStorage, err := storage.NewStorage(cfg.DatabasePath)
	if err != nil {
		log.Fatalf("初始化数据库失败: %v", err)
	}
	defer dbStorage.Close()
	fmt.Printf("数据库已初始化: %s\n", cfg.DatabasePath)

	// 初始化 LarkService（在启动时初始化，供 HTTP 接口和 WebSocket 使用）
	larkService := service.NewLarkService(cfg.AppID, cfg.AppSecret)
	client := larkService.GetClient()

	// 创建可取消的 context，用于优雅关闭
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 设置信号处理，当收到 Ctrl+C 时取消 context
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigChan
		log.Println("\n收到退出信号，开始优雅关闭...")
		cancel() // 取消 context，通知所有 goroutine 退出
	}()

	// 在后台 goroutine 启动 WebSocket 连接（用于接收用户消息）
	go startWebSocketConnection(ctx, cfg.AppID, cfg.AppSecret, client, larkService, dbStorage)

	// 启动 HTTP 服务（在主 goroutine 中运行，使用 context 控制）
	startHTTPServer(ctx, cfg, larkService, dbStorage)

	log.Println("程序已退出")
}

// startWebSocketConnection 启动 WebSocket 连接用于接收用户消息
func startWebSocketConnection(ctx context.Context, appID, appSecret string, client *lark.Client, larkService *service.LarkService, dbStorage *storage.Storage) {
	/**
	 * 注册事件处理器。
	 * Register event handler.
	 */
	eventHandler := dispatcher.NewEventDispatcher("", "").
		/**
		 * 注册接收消息事件，处理接收到的消息。
		 * Register event handler to handle received messages.
		 * https://open.feishu.cn/document/uAjLw4CM/ukTMukTMukTM/reference/im-v1/message/events/receive
		 */
		OnP2MessageReceiveV1(func(ctx context.Context, event *larkim.P2MessageReceiveV1) error {
			log.Printf("========== 收到新消息事件 ==========")
			log.Printf("[OnP2MessageReceiveV1] 完整事件数据: %s", larkcore.Prettify(event))
			
			// 记录消息基本信息
			var messageID, chatID, messageType, chatType string
			var contentLen int
			
			if event.Event.Message.MessageId != nil {
				messageID = *event.Event.Message.MessageId
			}
			if event.Event.Message.ChatId != nil {
				chatID = *event.Event.Message.ChatId
			}
			if event.Event.Message.MessageType != nil {
				messageType = *event.Event.Message.MessageType
			}
			if event.Event.Message.ChatType != nil {
				chatType = *event.Event.Message.ChatType
			}
			if event.Event.Message.Content != nil {
				contentLen = len(*event.Event.Message.Content)
			}
			
			log.Printf("[消息信息] message_id=%s, chat_id=%s, message_type=%s, chat_type=%s, content_length=%d",
				messageID, chatID, messageType, chatType, contentLen)
			
			// 记录最近交互的会话信息（用于 HTTP 接口默认发送）
			if event.Event.Message.ChatId != nil {
				chatTypeStr := "group"
				if *event.Event.Message.ChatType == "p2p" {
					chatTypeStr = "p2p"
				}
				log.Printf("[更新最近会话] chat_id=%s, chat_type=%s", *event.Event.Message.ChatId, chatTypeStr)
				larkService.UpdateRecentChat(*event.Event.Message.ChatId, chatTypeStr)
			} else {
				log.Printf("[警告] ChatId 为 nil，无法更新最近会话")
			}
			
			// 保存消息到数据库
			if event.Event.Message.MessageId != nil && event.Event.Message.ChatId != nil {
				log.Printf("[开始保存消息] message_id=%s, chat_id=%s", *event.Event.Message.MessageId, *event.Event.Message.ChatId)
				
				var content string
				if event.Event.Message.Content != nil {
					content = *event.Event.Message.Content
					log.Printf("[消息内容] 长度=%d, 预览=%s", len(content), truncateString(content, 100))
				} else {
					log.Printf("[警告] Content 为 nil")
				}
				
				var messageTypeStr string
				if event.Event.Message.MessageType != nil {
					messageTypeStr = *event.Event.Message.MessageType
				}
				
				msg := &storage.Message{
					ChatID:      *event.Event.Message.ChatId,
					MessageID:   *event.Event.Message.MessageId,
					SenderID:    "", // EventMessage 中没有直接的发送者信息
					SenderType:  "user",
					Content:     content,
					MessageType: messageTypeStr,
					CreatedAt:   time.Now(),
				}
				
				log.Printf("[准备保存] Message对象: chat_id=%s, message_id=%s, content_len=%d, created_at=%s",
					msg.ChatID, msg.MessageID, len(msg.Content), msg.CreatedAt.Format(time.RFC3339))
				
				if err := dbStorage.SaveMessage(ctx, msg); err != nil {
					log.Printf("[错误] 保存消息到数据库失败: chat_id=%s, message_id=%s, error=%v", 
						msg.ChatID, msg.MessageID, err)
				} else {
					log.Printf("[成功] 消息已保存到数据库: chat_id=%s, message_id=%s, content_length=%d", 
						msg.ChatID, msg.MessageID, len(msg.Content))
				}
			} else {
				log.Printf("[警告] 消息未保存: message_id=%v, chat_id=%v (其中一个或两个为nil)",
					event.Event.Message.MessageId, event.Event.Message.ChatId)
			}
			
			log.Printf("========== 消息处理完成 ==========")
			
			/**
			 * 解析用户发送的消息。
			 * Parse the message sent by the user.
			 */
			var respContent map[string]string
			err := json.Unmarshal([]byte(*event.Event.Message.Content), &respContent)
			/**
			 * 检查消息类型是否为文本
			 * Check if the message type is text
			 */
			if err != nil || *event.Event.Message.MessageType != "text" {
				respContent = map[string]string{
					"text": "解析消息失败，请发送文本消息\nparse message failed, please send text message",
				}
			}

			/**
			 * 构建回复消息
			 * Build reply message
			 */
			content := larkim.NewTextMsgBuilder().
				TextLine("收到你发送的消息: " + respContent["text"]).
				TextLine("Received message: " + respContent["text"]).
				Build()

			if *event.Event.Message.ChatType == "p2p" {
				/**
				 * 使用SDK调用发送消息接口。 Use SDK to call send message interface.
				 * https://open.feishu.cn/document/uAjLw4CM/ukTMukTMukTM/reference/im-v1/message/create
				 */
				resp, err := client.Im.Message.Create(context.Background(), larkim.NewCreateMessageReqBuilder().
					ReceiveIdType(larkim.ReceiveIdTypeChatId). // 消息接收者的 ID 类型，设置为会话ID。 ID type of the message receiver, set to chat ID.
					Body(larkim.NewCreateMessageReqBodyBuilder().
						MsgType(larkim.MsgTypeText).            // 设置消息类型为文本消息。 Set message type to text message.
						ReceiveId(*event.Event.Message.ChatId). // 消息接收者的 ID 为消息发送的会话ID。 ID of the message receiver is the chat ID of the message sending.
						Content(content).
						Build()).
					Build())

				if err != nil || !resp.Success() {
					fmt.Println(err)
					fmt.Println(resp.Code, resp.Msg, resp.RequestId())
					return nil
				}

			} else {
				/**
				 * 使用SDK调用回复消息接口。 Use SDK to call send message interface.
				 * https://open.feishu.cn/document/server-docs/im-v1/message/reply
				 */
				resp, err := client.Im.Message.Reply(context.Background(), larkim.NewReplyMessageReqBuilder().
					MessageId(*event.Event.Message.MessageId).
					Body(larkim.NewReplyMessageReqBodyBuilder().
						MsgType(larkim.MsgTypeText). // 设置消息类型为文本消息。 Set message type to text message.
						Content(content).
						Build()).
					Build())
				if err != nil || !resp.Success() {
					fmt.Printf("logId: %s, error response: \n%s", resp.RequestId(), larkcore.Prettify(resp.CodeError))
					return nil
				}
			}

			return nil
		})

	/**
	 * 启动长连接，并注册事件处理器。
	 * Start long connection and register event handler.
	 */
	cli := larkws.NewClient(appID, appSecret,
		larkws.WithEventHandler(eventHandler),
		larkws.WithLogLevel(larkcore.LogLevelDebug),
	)

	fmt.Println("WebSocket 连接已启动，等待接收用户消息...")
	err := cli.Start(context.Background())
	if err != nil {
		log.Fatalf("WebSocket 启动失败: %v", err)
	}
}

// startHTTPServer 启动 HTTP 服务
func startHTTPServer(ctx context.Context, cfg *config.Config, larkService *service.LarkService, dbStorage *storage.Storage) {
	// 创建 Hertz 服务器
	port := ":" + cfg.Port
	h := server.Default(server.WithHostPorts(port))

	// 创建消息处理器
	messageHandler := handler.NewMessageHandler(larkService)

	// 注册路由
	h.GET("/api/send-message", messageHandler.SendMessage)

	// 健康检查接口
	h.GET("/health", func(ctx context.Context, c *app.RequestContext) {
		c.JSON(200, map[string]interface{}{
			"status":  "ok",
			"message": "服务运行正常",
		})
	})

	fmt.Printf("HTTP 服务已启动，监听端口: %s\n", cfg.Port)
	fmt.Printf("发送消息接口: GET http://localhost:%s/api/send-message?receive_id=xxx&content=xxx\n", cfg.Port)
	fmt.Printf("健康检查接口: GET http://localhost:%s/health\n", cfg.Port)

	// 在 goroutine 中启动服务器
	serverDone := make(chan struct{})
	go func() {
		h.Spin()
		close(serverDone)
	}()

	// 等待 context 取消或服务器退出
	select {
	case <-ctx.Done():
		log.Println("收到关闭信号，正在关闭 HTTP 服务器...")
		// 优雅关闭 HTTP 服务器
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutdownCancel()
		if err := h.Shutdown(shutdownCtx); err != nil {
			log.Printf("关闭 HTTP 服务器时出错: %v", err)
		} else {
			log.Println("HTTP 服务器已关闭")
		}
	case <-serverDone:
		log.Println("HTTP 服务器已退出")
	}
}

// maskString 隐藏字符串的大部分内容，只显示前4位和后4位
func maskString(s string) string {
	if len(s) <= 8 {
		return "****"
	}
	return s[:4] + "..." + s[len(s)-4:]
}

// truncateString 截断字符串，用于日志输出
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
