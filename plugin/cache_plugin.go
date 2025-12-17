package main

import (
	"fmt"
	"sync"
	"time"
	"unsafe"

	"github.com/millken/plugin"
)

type CacheEntry struct {
	Response  *plugin.DNSResponse
	ExpiresAt time.Time
}

type CachePlugin struct {
	cache      map[string]*CacheEntry
	mu         sync.RWMutex
	maxSize    int
	defaultTTL time.Duration
}

func NewCachePlugin() *CachePlugin {
	cp := &CachePlugin{
		cache:      make(map[string]*CacheEntry),
		maxSize:    10000,
		defaultTTL: 5 * time.Minute,
	}

	// 启动清理协程
	go cp.cleanup()

	return cp
}

func (cp *CachePlugin) Name() string {
	return "cache"
}

func (cp *CachePlugin) Version() string {
	return "1.0.0"
}

func (cp *CachePlugin) Description() string {
	return "DNS response cache plugin with LRU eviction"
}

func (cp *CachePlugin) Init(config map[string]interface{}) error {
	if maxSize, ok := config["max_size"].(int); ok {
		cp.maxSize = maxSize
	}
	if ttl, ok := config["ttl"].(int); ok {
		cp.defaultTTL = time.Duration(ttl) * time.Second
	}
	return nil
}

func (cp *CachePlugin) Start() error {
	return nil
}

func (cp *CachePlugin) Stop() error {
	cp.mu.Lock()
	defer cp.mu.Unlock()
	cp.cache = make(map[string]*CacheEntry)
	return nil
}

func (cp *CachePlugin) HandleQuery(query *plugin.DNSQuery) (*plugin.DNSResponse, error) {
	key := cp.getCacheKey(query)

	cp.mu.RLock()
	entry, exists := cp.cache[key]
	cp.mu.RUnlock()

	if exists && time.Now().Before(entry.ExpiresAt) {
		// 缓存命中
		return entry.Response, nil
	}

	// 缓存未命中或已过期
	if exists {
		cp.mu.Lock()
		delete(cp.cache, key)
		cp.mu.Unlock()
	}

	return nil, nil
}

func (cp *CachePlugin) HandleResponse(query *plugin.DNSQuery, response *plugin.DNSResponse) error {
	if response == nil {
		return nil
	}

	key := cp.getCacheKey(query)

	cp.mu.Lock()
	defer cp.mu.Unlock()

	// LRU淘汰策略
	if len(cp.cache) >= cp.maxSize {
		// 简单实现：删除第一个元素（应使用更好的LRU算法）
		for k := range cp.cache {
			delete(cp.cache, k)
			break
		}
	}

	ttl := cp.defaultTTL
	if response.TTL > 0 {
		ttl = time.Duration(response.TTL) * time.Second
	}

	cp.cache[key] = &CacheEntry{
		Response:  response,
		ExpiresAt: time.Now().Add(ttl),
	}

	return nil
}

func (cp *CachePlugin) getCacheKey(query *plugin.DNSQuery) string {
	return fmt.Sprintf("%s:%d:%d", query.Domain, query.QueryType, query.QueryClass)
}

func (cp *CachePlugin) cleanup() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		cp.mu.Lock()
		now := time.Now()
		for key, entry := range cp.cache {
			if now.After(entry.ExpiresAt) {
				delete(cp.cache, key)
			}
		}
		cp.mu.Unlock()
	}
}

// 导出符号供C接口使用
var globalPlugin *CachePlugin

//export plugin_new
func plugin_new() uintptr {
	globalPlugin = NewCachePlugin()
	return uintptr(unsafe.Pointer(globalPlugin))
}

//export plugin_name
func plugin_name(ptr uintptr) *C.char {
	return C.CString(globalPlugin.Name())
}

//export plugin_version
func plugin_version(ptr uintptr) *C.char {
	return C.CString(globalPlugin.Version())
}

//export plugin_description
func plugin_description(ptr uintptr) *C.char {
	return C.CString(globalPlugin.Description())
}
