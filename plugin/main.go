package main

import (
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/millken/plugin"
)

func main() {
	webAddr := flag.String("web", ":8080", "Web UI address")
	dnsAddr := flag.String("dns", ":53", "DNS server address")
	upstream := flag.String("upstream", "8.8.8.8:53,1.1.1.1:53", "Upstream DNS servers")
	flag.Parse()

	// 创建事件总线
	eventBus := plugin.NewEventBus(1000, 500)
	defer eventBus.Close()

	// 创建插件管理器
	pluginManager := plugin.NewPluginManager()
	defer pluginManager.Close()

	// 加载插件
	plugins := []string{
		"./plugins/cache. so",
		"./plugins/blacklist.so",
	}

	for _, p := range plugins {
		if err := pluginManager.LoadPlugin(p); err != nil {
			log.Printf("Warning: failed to load plugin %s: %v", p, err)
		} else {
			log.Printf("Loaded plugin:  %s", p)
		}
	}

	// 创建服务器配置
	config := &plugin.ServerConfig{
		ListenAddr: *dnsAddr,
		Upstream:   parseUpstreams(*upstream),
		Timeout:    5,
	}

	// 创建DNS控制器
	controller := plugin.NewDNSController(pluginManager, eventBus, config)

	// 创建Web处理器
	webHandler := plugin.NewWebHandler(controller, eventBus)

	// 注册路由
	mux := http.NewServeMux()
	webHandler.RegisterRoutes(mux)

	// 启动Web服务器
	go func() {
		log.Printf("Web UI server listening on %s", *webAddr)
		if err := http.ListenAndServe(*webAddr, mux); err != nil {
			log.Fatalf("Failed to start web server: %v", err)
		}
	}()

	// 等待信号
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	// 优雅关闭
	log.Println("Shutting down...")
	eventBus.Publish(plugin.EventServerStop, "main", nil)
}

func parseUpstreams(upstream string) []string {
	upstreams := []string{}
	for _, u := range strings.Split(upstream, ",") {
		u = strings.TrimSpace(u)
		if u != "" {
			upstreams = append(upstreams, u)
		}
	}
	return upstreams
}
