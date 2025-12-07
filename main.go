package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"

	"fin_bot/config"
	"fin_bot/handler"
	"fin_bot/service"

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

	// 初始化 LarkService（在启动时初始化，供 HTTP 接口和 WebSocket 使用）
	larkService := service.NewLarkService(cfg.AppID, cfg.AppSecret)
	client := larkService.GetClient()

	// 使用 WaitGroup 来等待所有 goroutine 完成
	var wg sync.WaitGroup

	// 在后台 goroutine 启动 WebSocket 连接（用于接收用户消息）
	wg.Add(1)
	go func() {
		defer wg.Done()
		startWebSocketConnection(cfg.AppID, cfg.AppSecret, client, larkService)
	}()

	// 启动 Hertz HTTP 服务
	startHTTPServer(cfg, larkService)

	// 等待所有 goroutine（实际上 HTTP 服务会一直运行）
	wg.Wait()
}

// startWebSocketConnection 启动 WebSocket 连接用于接收用户消息
func startWebSocketConnection(appID, appSecret string, client *lark.Client, larkService *service.LarkService) {
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
			fmt.Printf("[OnP2MessageReceiveV1 access], data: %s\n", larkcore.Prettify(event))
			
			// 记录最近交互的会话信息（用于 HTTP 接口默认发送）
			if event.Event.Message.ChatId != nil {
				chatType := "group"
				if *event.Event.Message.ChatType == "p2p" {
					chatType = "p2p"
				}
				larkService.UpdateRecentChat(*event.Event.Message.ChatId, chatType)
			}
			
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
func startHTTPServer(cfg *config.Config, larkService *service.LarkService) {
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

	// 启动服务器（阻塞）
	h.Spin()
}

// maskString 隐藏字符串的大部分内容，只显示前4位和后4位
func maskString(s string) string {
	if len(s) <= 8 {
		return "****"
	}
	return s[:4] + "..." + s[len(s)-4:]
}
