package plugin

import (
	"net"
	"time"
)

// DNSPlugin 定义DNS插件接口
type DNSPlugin interface {
	Plugin
	// HandleQuery 处理DNS查询，返回响应或nil（如果需要继续处理）
	HandleQuery(query *DNSQuery) (*DNSResponse, error)
	// HandleResponse 处理DNS响应（用于后置处理）
	HandleResponse(query *DNSQuery, response *DNSResponse) error
}

// DNSQuery DNS查询结构
type DNSQuery struct {
	Domain     string
	QueryType  uint16
	QueryClass uint16
	ClientAddr *net.UDPAddr
	RawData    []byte
	Timestamp  time.Time
}

// DNSResponse DNS响应结构
type DNSResponse struct {
	Domain  string
	Records []DNSRecord
	TTL     uint32
	RawData []byte
}

// DNSRecord DNS记录结构
type DNSRecord struct {
	Name  string
	Type  uint16
	Class uint16
	TTL   uint32
	Data  []byte
}

// DNS记录类型常量
const (
	TypeA     uint16 = 1
	TypeNS    uint16 = 2
	TypeCNAME uint16 = 5
	TypeSOA   uint16 = 6
	TypePTR   uint16 = 12
	TypeMX    uint16 = 15
	TypeTXT   uint16 = 16
	TypeAAAA  uint16 = 28
)

// DNS类常量
const (
	ClassIN uint16 = 1
)

// DNS响应码常量
const (
	RCodeNoError  byte = 0
	RCodeFormErr  byte = 1
	RCodeServFail byte = 2
	RCodeNXDomain byte = 3
	RCodeNotImpl  byte = 4
	RCodeRefused  byte = 5
)
