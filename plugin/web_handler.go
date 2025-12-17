package plugin

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"
)

// WebHandler Web API处理器
type WebHandler struct {
	controller *DNSController
	eventBus   *EventBus
}

// NewWebHandler 创建Web处理器
func NewWebHandler(controller *DNSController, eventBus *EventBus) *WebHandler {
	return &WebHandler{
		controller: controller,
		eventBus:   eventBus,
	}
}

// RegisterRoutes 注册路由
func (wh *WebHandler) RegisterRoutes(mux *http.ServeMux) {
	// 静态文件
	mux.Handle("/", http.FileServer(http.Dir("./web")))

	// API路由
	mux.HandleFunc("/api/server/start", wh.handleStart)
	mux.HandleFunc("/api/server/stop", wh.handleStop)
	mux.HandleFunc("/api/server/restart", wh.handleRestart)
	mux.HandleFunc("/api/server/status", wh.handleStatus)
	mux.HandleFunc("/api/server/info", wh.handleInfo)
	mux.HandleFunc("/api/server/stats", wh.handleStats)
	mux.HandleFunc("/api/config/update", wh.handleConfigUpdate)
	mux.HandleFunc("/api/events/history", wh.handleEventHistory)
	mux.HandleFunc("/api/events/stream", wh.handleEventStream)
	mux.HandleFunc("/api/plugins/list", wh.handlePluginsList)
}

// handleStart 处理启动请求
func (wh *WebHandler) handleStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// 发布启动事件
	wh.eventBus.Publish(EventServerStart, "web-api", map[string]interface{}{
		"user": r.RemoteAddr,
	})

	wh.jsonResponse(w, map[string]interface{}{
		"success": true,
		"message": "Start command sent",
	})
}

// handleStop 处理停止请求
func (wh *WebHandler) handleStop(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// 发布停止事件
	wh.eventBus.Publish(EventServerStop, "web-api", map[string]interface{}{
		"user": r.RemoteAddr,
	})

	wh.jsonResponse(w, map[string]interface{}{
		"success": true,
		"message": "Stop command sent",
	})
}

// handleRestart 处理重启请求
func (wh *WebHandler) handleRestart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// 发布重启事件
	wh.eventBus.Publish(EventServerRestart, "web-api", map[string]interface{}{
		"user": r.RemoteAddr,
	})

	wh.jsonResponse(w, map[string]interface{}{
		"success": true,
		"message": "Restart command sent",
	})
}

// handleStatus 处理状态查询
func (wh *WebHandler) handleStatus(w http.ResponseWriter, r *http.Request) {
	wh.jsonResponse(w, map[string]interface{}{
		"state": wh.controller.GetState(),
	})
}

// handleInfo 处理服务器信息查询
func (wh *WebHandler) handleInfo(w http.ResponseWriter, r *http.Request) {
	wh.jsonResponse(w, wh.controller.GetInfo())
}

// handleStats 处理统计信息查询
func (wh *WebHandler) handleStats(w http.ResponseWriter, r *http.Request) {
	wh.jsonResponse(w, wh.controller.GetStats())
}

// handleConfigUpdate 处理配置更新
func (wh *WebHandler) handleConfigUpdate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var config ServerConfig
	if err := json.NewDecoder(r.Body).Decode(&config); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// 发布配置更新事件
	wh.eventBus.Publish(EventConfigUpdate, "web-api", map[string]interface{}{
		"config": &config,
		"user":   r.RemoteAddr,
	})

	wh.jsonResponse(w, map[string]interface{}{
		"success": true,
		"message": "Configuration update sent",
	})
}

// handleEventHistory 处理事件历史查询
func (wh *WebHandler) handleEventHistory(w http.ResponseWriter, r *http.Request) {
	limit := 50
	history := wh.eventBus.GetHistory(limit)
	wh.jsonResponse(w, history)
}

// handleEventStream 处理事件流（SSE）
func (wh *WebHandler) handleEventStream(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	// 创建事件通道
	eventChan := make(chan *Event, 10)

	// 订阅所有事件
	subID := wh.eventBus.Subscribe("", func(event *Event) error {
		select {
		case eventChan <- event:
		default:
		}
		return nil
	}, 0)

	defer wh.eventBus.Unsubscribe(subID)

	// 发送事件流
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming not supported", http.StatusInternalServerError)
		return
	}

	for {
		select {
		case <-r.Context().Done():
			return
		case event := <-eventChan:
			data, _ := json.Marshal(event)
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
		case <-time.After(30 * time.Second):
			// 心跳
			fmt.Fprintf(w, ":  heartbeat\n\n")
			flusher.Flush()
		}
	}
}

// handlePluginsList 处理插件列表查询
func (wh *WebHandler) handlePluginsList(w http.ResponseWriter, r *http.Request) {
	plugins := wh.controller.pluginManager.ListPlugins()

	result := make([]map[string]interface{}, 0, len(plugins))
	for _, name := range plugins {
		plugin, exists := wh.controller.pluginManager.GetPlugin(name)
		if exists {
			result = append(result, map[string]interface{}{
				"name":        plugin.Name(),
				"version":     plugin.Version(),
				"description": plugin.Description(),
			})
		}
	}

	wh.jsonResponse(w, result)
}

// jsonResponse 发送JSON响应
func (wh *WebHandler) jsonResponse(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(data); err != nil {
		log.Printf("Error encoding JSON response: %v", err)
	}
}
