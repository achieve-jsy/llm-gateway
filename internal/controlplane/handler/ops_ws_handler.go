package handler

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/redis/go-redis/v9"
)

var wsUpgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

type OpsWSHandler struct {
	rdb *redis.Client
}

func NewOpsWSHandler(rdb *redis.Client) *OpsWSHandler {
	return &OpsWSHandler{rdb: rdb}
}

func (h *OpsWSHandler) HandleQPSWebSocket(c *gin.Context) {
	responseHeader := http.Header{}
	if proto := c.GetHeader("Sec-WebSocket-Protocol"); proto != "" {
		responseHeader.Set("Sec-WebSocket-Protocol", "ops-admin")
	}

	conn, err := wsUpgrader.Upgrade(c.Writer, c.Request, responseHeader)
	if err != nil {
		return
	}
	defer conn.Close()

	conn.SetReadDeadline(time.Now().Add(5 * time.Minute))
	conn.SetPongHandler(func(string) error {
		conn.SetReadDeadline(time.Now().Add(5 * time.Minute))
		return nil
	})

	go func() {
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}()

	ctx := c.Request.Context()
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()
	pingTicker := time.NewTicker(30 * time.Second)
	defer pingTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			now := time.Now()
			minKey := now.Format("200601021504")

			qpsCount, _ := h.rdb.Get(ctx, "ops:qps:"+minKey).Int64()
			tpsCount, _ := h.rdb.Get(ctx, "ops:tps:"+minKey).Int64()

			msg := map[string]any{
				"qps":       float64(qpsCount) / 60.0,
				"tps":       float64(tpsCount) / 60.0,
				"timestamp": now.Format(time.RFC3339),
				"sample":    qpsCount,
			}
			data, _ := json.Marshal(msg)
			if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
				return
			}

		case <-pingTicker.C:
			if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

func extractTokenFromProtocol(c *gin.Context) string {
	protocols := c.Request.Header.Get("Sec-WebSocket-Protocol")
	for _, p := range strings.Split(protocols, ",") {
		p = strings.TrimSpace(p)
		if strings.HasPrefix(p, "jwt.") {
			return strings.TrimPrefix(p, "jwt.")
		}
	}
	return ""
}
