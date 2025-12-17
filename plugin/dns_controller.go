package plugin

import (
	"fmt"
	"log"
	"sync"
	"time"
)

// ServerState DNS服务器状态
type ServerState string

const (
	StateUnknown  ServerState = "unknown"
	StateStopped  ServerState = "stopped"
	StateStarting ServerState = "starting"
	StateRunning  ServerState = "running"
	StateStopping ServerState = "stopping"
	StateError    ServerState = "error"
)

// DNSController DNS服务器控制器
type DNSController struct {
	server        *DNSServer
	pluginManager *PluginManager
	eventBus      *EventBus
	state         ServerState
	stateMu       sync.RWMutex
	config        *ServerConfig
	stats         *ServerStats
	startTime     time.Time
}

// ServerConfig 服务器配置
type ServerConfig struct {
	ListenAddr string   `json:"listen_addr"`
	Upstream   []string `json:"upstream"`
	Timeout    int      `json:"timeout"`
}

// ServerStats 服务器统计
type ServerStats struct {
	TotalQueries   int64     `json:"total_queries"`
	CachedQueries  int64     `json:"cached_queries"`
	BlockedQueries int64     `json:"blocked_queries"`
	FailedQueries  int64     `json:"failed_queries"`
	AverageLatency float64   `json:"average_latency_ms"`
	LastQueryTime  time.Time `json:"last_query_time"`
	mu             sync.RWMutex
}

// NewDNSController 创建DNS控制器
func NewDNSController(pm *PluginManager, eb *EventBus, config *ServerConfig) *DNSController {
	controller := &DNSController{
		pluginManager: pm,
		eventBus:      eb,
		state:         StateStopped,
		config:        config,
		stats:         &ServerStats{},
	}

	// 注册事件处理器
	controller.registerEventHandlers()

	return controller
}

// registerEventHandlers 注册事件处理器
func (dc *DNSController) registerEventHandlers() {
	// 服务器启动事件
	dc.eventBus.Subscribe(EventServerStart, func(event *Event) error {
		return dc.handleStart(event)
	}, 100)

	// 服务器停止事件
	dc.eventBus.Subscribe(EventServerStop, func(event *Event) error {
		return dc.handleStop(event)
	}, 100)

	// 服务器重启事件
	dc.eventBus.Subscribe(EventServerRestart, func(event *Event) error {
		return dc.handleRestart(event)
	}, 100)

	// 配置更新事件
	dc.eventBus.Subscribe(EventConfigUpdate, func(event *Event) error {
		return dc.handleConfigUpdate(event)
	}, 90)

	// 查询统计事件
	dc.eventBus.Subscribe(EventQueryReceived, func(event *Event) error {
		return dc.handleQueryStats(event)
	}, 50)
}

// handleStart 处理启动事件
func (dc *DNSController) handleStart(event *Event) error {
	dc.stateMu.Lock()

	if dc.state == StateRunning || dc.state == StateStarting {
		dc.stateMu.Unlock()
		return fmt.Errorf("server is already running or starting")
	}

	dc.state = StateStarting
	dc.stateMu.Unlock()

	log.Println("Starting DNS server...")

	// 创建服务器实例
	dc.server = NewDNSServer(dc.config.ListenAddr, dc.pluginManager, dc.config.Upstream)

	// 启动服务器
	if err := dc.server.Start(); err != nil {
		dc.setState(StateError)
		dc.eventBus.Publish(EventServerStatus, "controller", map[string]interface{}{
			"state": StateError,
			"error": err.Error(),
		})
		return fmt.Errorf("failed to start server: %v", err)
	}

	dc.startTime = time.Now()
	dc.setState(StateRunning)

	// 发布状态事件
	dc.eventBus.Publish(EventServerStatus, "controller", map[string]interface{}{
		"state":      StateRunning,
		"start_time": dc.startTime,
	})

	log.Println("DNS server started successfully")
	return nil
}

// handleStop 处理停止事件
func (dc *DNSController) handleStop(event *Event) error {
	dc.stateMu.Lock()

	if dc.state != StateRunning {
		dc.stateMu.Unlock()
		return fmt.Errorf("server is not running")
	}

	dc.state = StateStopping
	dc.stateMu.Unlock()

	log.Println("Stopping DNS server...")

	// 停止服务器
	if dc.server != nil {
		if err := dc.server.Stop(); err != nil {
			log.Printf("Error stopping server: %v", err)
		}
		dc.server = nil
	}

	dc.setState(StateStopped)

	// 发布状态事件
	dc.eventBus.Publish(EventServerStatus, "controller", map[string]interface{}{
		"state":  StateStopped,
		"uptime": time.Since(dc.startTime).Seconds(),
	})

	log.Println("DNS server stopped")
	return nil
}

// handleRestart 处理重启事件
func (dc *DNSController) handleRestart(event *Event) error {
	log.Println("Restarting DNS server...")

	// 停止服务器
	if err := dc.handleStop(event); err != nil {
		log.Printf("Error during restart stop: %v", err)
	}

	// 等待一秒
	time.Sleep(1 * time.Second)

	// 启动服务器
	return dc.handleStart(event)
}

// handleConfigUpdate 处理配置更新事件
func (dc *DNSController) handleConfigUpdate(event *Event) error {
	log.Println("Updating configuration...")

	// 从事件数据中提取配置
	if config, ok := event.Data["config"].(*ServerConfig); ok {
		dc.config = config
		log.Printf("Configuration updated: %+v", config)

		// 如果服务器正在运行，重启以应用新配置
		if dc.GetState() == StateRunning {
			return dc.handleRestart(event)
		}
	}

	return nil
}

// handleQueryStats 处理查询统计
func (dc *DNSController) handleQueryStats(event *Event) error {
	dc.stats.mu.Lock()
	defer dc.stats.mu.Unlock()

	dc.stats.TotalQueries++
	dc.stats.LastQueryTime = time.Now()

	// 根据事件类型更新统计
	if cached, ok := event.Data["cached"].(bool); ok && cached {
		dc.stats.CachedQueries++
	}

	if blocked, ok := event.Data["blocked"].(bool); ok && blocked {
		dc.stats.BlockedQueries++
	}

	if failed, ok := event.Data["failed"].(bool); ok && failed {
		dc.stats.FailedQueries++
	}

	if latency, ok := event.Data["latency"].(float64); ok {
		// 计算移动平均延迟
		dc.stats.AverageLatency = (dc.stats.AverageLatency*float64(dc.stats.TotalQueries-1) + latency) / float64(dc.stats.TotalQueries)
	}

	return nil
}

// GetState 获取服务器状态
func (dc *DNSController) GetState() ServerState {
	dc.stateMu.RLock()
	defer dc.stateMu.RUnlock()
	return dc.state
}

// setState 设置服务器状态
func (dc *DNSController) setState(state ServerState) {
	dc.stateMu.Lock()
	defer dc.stateMu.Unlock()
	dc.state = state
}

// GetStats 获取服务器统计
func (dc *DNSController) GetStats() ServerStats {
	dc.stats.mu.RLock()
	defer dc.stats.mu.RUnlock()

	stats := *dc.stats
	return stats
}

// GetInfo 获取服务器信息
func (dc *DNSController) GetInfo() map[string]interface{} {
	info := map[string]interface{}{
		"state":  dc.GetState(),
		"config": dc.config,
		"stats":  dc.GetStats(),
		"uptime": 0,
	}

	if dc.state == StateRunning {
		info["uptime"] = time.Since(dc.startTime).Seconds()
	}

	return info
}
