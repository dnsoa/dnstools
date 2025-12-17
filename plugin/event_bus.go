package plugin

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"
)

// EventType 事件类型
type EventType string

const (
	// DNS服务器事件
	EventServerStart   EventType = "server. start"
	EventServerStop    EventType = "server.stop"
	EventServerRestart EventType = "server.restart"
	EventServerStatus  EventType = "server.status"

	// 插件事件
	EventPluginLoad   EventType = "plugin.load"
	EventPluginUnload EventType = "plugin.unload"
	EventPluginReload EventType = "plugin. reload"

	// 配置事件
	EventConfigUpdate EventType = "config.update"
	EventConfigReload EventType = "config. reload"

	// DNS查询事件
	EventQueryReceived EventType = "query. received"
	EventQueryResolved EventType = "query. resolved"
	EventQueryBlocked  EventType = "query. blocked"
	EventQueryCached   EventType = "query.cached"
)

// Event 事件结构
type Event struct {
	Type      EventType              `json:"type"`
	Timestamp time.Time              `json:"timestamp"`
	Source    string                 `json:"source"`
	Data      map[string]interface{} `json:"data"`
	Context   context.Context        `json:"-"`
}

// EventHandler 事件处理器函数
type EventHandler func(event *Event) error

// EventSubscription 事件订阅
type EventSubscription struct {
	ID        string
	EventType EventType
	Handler   EventHandler
	Filter    func(*Event) bool
	Priority  int // 优先级，数值越大越先执行
}

// EventBus 事件总线
type EventBus struct {
	subscriptions map[EventType][]*EventSubscription
	eventQueue    chan *Event
	mu            sync.RWMutex
	ctx           context.Context
	cancel        context.CancelFunc
	wg            sync.WaitGroup
	historySize   int
	history       []*Event
	historyMu     sync.RWMutex
}

// NewEventBus 创建新的事件总线
func NewEventBus(queueSize int, historySize int) *EventBus {
	ctx, cancel := context.WithCancel(context.Background())
	eb := &EventBus{
		subscriptions: make(map[EventType][]*EventSubscription),
		eventQueue:    make(chan *Event, queueSize),
		ctx:           ctx,
		cancel:        cancel,
		historySize:   historySize,
		history:       make([]*Event, 0, historySize),
	}

	// 启动事件处理协程
	eb.wg.Add(1)
	go eb.processEvents()

	return eb
}

// Subscribe 订阅事件
func (eb *EventBus) Subscribe(eventType EventType, handler EventHandler, priority int) string {
	return eb.SubscribeWithFilter(eventType, handler, nil, priority)
}

// SubscribeWithFilter 订阅事件（带过滤器）
func (eb *EventBus) SubscribeWithFilter(eventType EventType, handler EventHandler, filter func(*Event) bool, priority int) string {
	eb.mu.Lock()
	defer eb.mu.Unlock()

	subscription := &EventSubscription{
		ID:        fmt.Sprintf("%s-%d", eventType, time.Now().UnixNano()),
		EventType: eventType,
		Handler:   handler,
		Filter:    filter,
		Priority:  priority,
	}

	eb.subscriptions[eventType] = append(eb.subscriptions[eventType], subscription)

	// 按优先级排序
	eb.sortSubscriptions(eventType)

	log.Printf("Subscribed to event %s with ID %s", eventType, subscription.ID)
	return subscription.ID
}

// Unsubscribe 取消订阅
func (eb *EventBus) Unsubscribe(subscriptionID string) bool {
	eb.mu.Lock()
	defer eb.mu.Unlock()

	for eventType, subs := range eb.subscriptions {
		for i, sub := range subs {
			if sub.ID == subscriptionID {
				eb.subscriptions[eventType] = append(subs[:i], subs[i+1:]...)
				log.Printf("Unsubscribed from event %s (ID: %s)", eventType, subscriptionID)
				return true
			}
		}
	}

	return false
}

// Publish 发布事件（异步）
func (eb *EventBus) Publish(eventType EventType, source string, data map[string]interface{}) {
	event := &Event{
		Type:      eventType,
		Timestamp: time.Now(),
		Source:    source,
		Data:      data,
		Context:   context.Background(),
	}

	select {
	case eb.eventQueue <- event:
		log.Printf("Event published: %s from %s", eventType, source)
	default:
		log.Printf("Event queue full, dropping event: %s", eventType)
	}
}

// PublishSync 发布事件（同步）
func (eb *EventBus) PublishSync(eventType EventType, source string, data map[string]interface{}) error {
	event := &Event{
		Type:      eventType,
		Timestamp: time.Now(),
		Source:    source,
		Data:      data,
		Context:   context.Background(),
	}

	return eb.dispatchEvent(event)
}

// processEvents 处理事件队列
func (eb *EventBus) processEvents() {
	defer eb.wg.Done()

	for {
		select {
		case <-eb.ctx.Done():
			return
		case event := <-eb.eventQueue:
			if err := eb.dispatchEvent(event); err != nil {
				log.Printf("Error dispatching event %s: %v", event.Type, err)
			}
		}
	}
}

// dispatchEvent 分发事件给订阅者
func (eb *EventBus) dispatchEvent(event *Event) error {
	// 记录到历史
	eb.addToHistory(event)

	eb.mu.RLock()
	subscriptions, exists := eb.subscriptions[event.Type]
	eb.mu.RUnlock()

	if !exists || len(subscriptions) == 0 {
		return nil
	}

	var errors []error
	for _, sub := range subscriptions {
		// 应用过滤器
		if sub.Filter != nil && !sub.Filter(event) {
			continue
		}

		// 执行处理器
		if err := eb.executeHandler(sub.Handler, event); err != nil {
			errors = append(errors, fmt.Errorf("handler %s error: %v", sub.ID, err))
		}
	}

	if len(errors) > 0 {
		return fmt.Errorf("multiple handler errors: %v", errors)
	}

	return nil
}

// executeHandler 执行事件处理器（带超时保护）
func (eb *EventBus) executeHandler(handler EventHandler, event *Event) error {
	done := make(chan error, 1)

	go func() {
		defer func() {
			if r := recover(); r != nil {
				done <- fmt.Errorf("handler panic: %v", r)
			}
		}()
		done <- handler(event)
	}()

	select {
	case err := <-done:
		return err
	case <-time.After(10 * time.Second):
		return fmt.Errorf("handler timeout")
	}
}

// sortSubscriptions 按优先级排序订阅
func (eb *EventBus) sortSubscriptions(eventType EventType) {
	subs := eb.subscriptions[eventType]
	for i := 0; i < len(subs)-1; i++ {
		for j := i + 1; j < len(subs); j++ {
			if subs[i].Priority < subs[j].Priority {
				subs[i], subs[j] = subs[j], subs[i]
			}
		}
	}
}

// addToHistory 添加事件到历史记录
func (eb *EventBus) addToHistory(event *Event) {
	eb.historyMu.Lock()
	defer eb.historyMu.Unlock()

	if len(eb.history) >= eb.historySize {
		eb.history = eb.history[1:]
	}

	eb.history = append(eb.history, event)
}

// GetHistory 获取事件历史
func (eb *EventBus) GetHistory(limit int) []*Event {
	eb.historyMu.RLock()
	defer eb.historyMu.RUnlock()

	if limit <= 0 || limit > len(eb.history) {
		limit = len(eb.history)
	}

	start := len(eb.history) - limit
	result := make([]*Event, limit)
	copy(result, eb.history[start:])

	return result
}

// Close 关闭事件总线
func (eb *EventBus) Close() {
	log.Println("Closing event bus...")
	eb.cancel()
	close(eb.eventQueue)
	eb.wg.Wait()
	log.Println("Event bus closed")
}
