package plugin

import (
	"context"
	"fmt"
	"log"
	"net"
	"sync"
	"time"
)

type DNSServer struct {
	addr          string
	pluginManager *PluginManager
	conn          *net.UDPConn
	upstream      []string
	timeout       time.Duration
	mu            sync.RWMutex
	ctx           context.Context
	cancel        context.CancelFunc
	wg            sync.WaitGroup
}

func NewDNSServer(addr string, pm *PluginManager, upstream []string) *DNSServer {
	ctx, cancel := context.WithCancel(context.Background())
	return &DNSServer{
		addr:          addr,
		pluginManager: pm,
		upstream:      upstream,
		timeout:       5 * time.Second,
		ctx:           ctx,
		cancel:        cancel,
	}
}

func (s *DNSServer) Start() error {
	addr, err := net.ResolveUDPAddr("udp", s.addr)
	if err != nil {
		return fmt.Errorf("failed to resolve address: %v", err)
	}

	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		return fmt.Errorf("failed to listen on %s: %v", s.addr, err)
	}

	s.conn = conn
	log.Printf("DNS server listening on %s", s.addr)

	// 启动多个工作协程处理请求
	for i := 0; i < 10; i++ {
		s.wg.Add(1)
		go s.handleRequests()
	}

	return nil
}

func (s *DNSServer) handleRequests() {
	defer s.wg.Done()
	buffer := make([]byte, 512) // DNS UDP最大512字节

	for {
		select {
		case <-s.ctx.Done():
			return
		default:
			s.conn.SetReadDeadline(time.Now().Add(1 * time.Second))
			n, clientAddr, err := s.conn.ReadFromUDP(buffer)
			if err != nil {
				if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
					continue
				}
				log.Printf("Error reading from UDP: %v", err)
				continue
			}

			// 复制数据到新的slice，避免被覆盖
			data := make([]byte, n)
			copy(data, buffer[:n])

			// 异步处理请求
			go s.processQuery(data, clientAddr)
		}
	}
}

func (s *DNSServer) processQuery(data []byte, clientAddr *net.UDPAddr) {
	// 解析DNS查询
	query, err := parseDNSQuery(data)
	if err != nil {
		log.Printf("Failed to parse DNS query: %v", err)
		return
	}
	query.ClientAddr = clientAddr
	query.Timestamp = time.Now()

	log.Printf("Query from %s: %s (type=%d)", clientAddr.String(), query.Domain, query.QueryType)

	var response *DNSResponse

	// 执行插件链处理（前置处理）
	plugins := s.pluginManager.ListPlugins()
	for _, name := range plugins {
		p, exists := s.pluginManager.GetPlugin(name)
		if !exists {
			continue
		}

		if dnsPlugin, ok := p.(DNSPlugin); ok {
			resp, err := dnsPlugin.HandleQuery(query)
			if err != nil {
				log.Printf("Plugin %s error: %v", name, err)
				continue
			}
			if resp != nil {
				log.Printf("Plugin %s handled query for %s", name, query.Domain)
				response = resp
				break
			}
		}
	}

	// 如果插件未处理，查询上游DNS
	if response == nil {
		response, err = s.queryUpstream(query)
		if err != nil {
			log.Printf("Failed to query upstream: %v", err)
			response = createErrorResponse(query, 2) // SERVFAIL
		}
	}

	// 执行插件链处理（后置处理）
	for _, name := range plugins {
		p, exists := s.pluginManager.GetPlugin(name)
		if !exists {
			continue
		}

		if dnsPlugin, ok := p.(DNSPlugin); ok {
			if err := dnsPlugin.HandleResponse(query, response); err != nil {
				log.Printf("Plugin %s response error: %v", name, err)
			}
		}
	}

	// 发送响应
	if response != nil && len(response.RawData) > 0 {
		_, err = s.conn.WriteToUDP(response.RawData, clientAddr)
		if err != nil {
			log.Printf("Failed to send response:  %v", err)
		}
	}
}

func (s *DNSServer) queryUpstream(query *DNSQuery) (*DNSResponse, error) {
	for _, upstream := range s.upstream {
		conn, err := net.DialTimeout("udp", upstream, s.timeout)
		if err != nil {
			log.Printf("Failed to connect to upstream %s: %v", upstream, err)
			continue
		}
		defer conn.Close()

		conn.SetDeadline(time.Now().Add(s.timeout))

		_, err = conn.Write(query.RawData)
		if err != nil {
			log.Printf("Failed to write to upstream %s: %v", upstream, err)
			continue
		}

		buffer := make([]byte, 512)
		n, err := conn.Read(buffer)
		if err != nil {
			log.Printf("Failed to read from upstream %s: %v", upstream, err)
			continue
		}

		response := &DNSResponse{
			Domain:  query.Domain,
			RawData: buffer[:n],
		}

		return response, nil
	}

	return nil, fmt.Errorf("all upstream servers failed")
}

func (s *DNSServer) Stop() error {
	log.Println("Stopping DNS server...")
	s.cancel()

	if s.conn != nil {
		s.conn.Close()
	}

	s.wg.Wait()
	log.Println("DNS server stopped")
	return nil
}

func parseDNSQuery(data []byte) (*DNSQuery, error) {
	if len(data) < 12 {
		return nil, fmt.Errorf("DNS query too short")
	}

	query := &DNSQuery{
		RawData: data,
	}

	// 解析域名（从第12字节开始）
	domain, qtype, qclass, err := parseDNSQuestion(data[12:])
	if err != nil {
		return nil, err
	}

	query.Domain = domain
	query.QueryType = qtype
	query.QueryClass = qclass

	return query, nil
}

func parseDNSQuestion(data []byte) (string, uint16, uint16, error) {
	domain := ""
	pos := 0

	for pos < len(data) {
		length := int(data[pos])
		if length == 0 {
			pos++
			break
		}

		if pos+length+1 > len(data) {
			return "", 0, 0, fmt.Errorf("invalid domain name")
		}

		if domain != "" {
			domain += "."
		}
		domain += string(data[pos+1 : pos+1+length])
		pos += length + 1
	}

	if pos+4 > len(data) {
		return "", 0, 0, fmt.Errorf("incomplete question section")
	}

	qtype := uint16(data[pos])<<8 | uint16(data[pos+1])
	qclass := uint16(data[pos+2])<<8 | uint16(data[pos+3])

	return domain, qtype, qclass, nil
}

func createErrorResponse(query *DNSQuery, rcode byte) *DNSResponse {
	response := make([]byte, len(query.RawData))
	copy(response, query.RawData)

	// 设置QR=1 (响应), RCODE=rcode
	response[2] = 0x80 | (response[2] & 0x78) | rcode
	response[3] = 0x00

	return &DNSResponse{
		Domain:  query.Domain,
		RawData: response,
	}
}
