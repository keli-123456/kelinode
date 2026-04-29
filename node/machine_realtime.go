package node

import (
	"context"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/keli-123456/kelinode/conf"
	log "github.com/sirupsen/logrus"
)

type MachineRealtimeManager struct {
	ctx       context.Context
	cancel    context.CancelFunc
	profiles  []conf.MachineProfileConfig
	fallback  conf.RealtimeConfig
	onReload  func()
	clientsMu sync.Mutex
	clients   []*RealtimeClient
}

func NewMachineRealtimeManager(parent context.Context, profiles []conf.MachineProfileConfig, fallback conf.RealtimeConfig, onReload func()) *MachineRealtimeManager {
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithCancel(parent)
	return &MachineRealtimeManager{
		ctx:      ctx,
		cancel:   cancel,
		profiles: append([]conf.MachineProfileConfig(nil), profiles...),
		fallback: fallback,
		onReload: onReload,
	}
}

func (m *MachineRealtimeManager) Start() {
	if m == nil {
		return
	}
	for i := range m.profiles {
		options := resolveMachineRealtimeOptions(m.profiles[i], m.fallback)
		if options == nil {
			continue
		}
		var client *RealtimeClient
		client = NewRealtimeClient(m.ctx, *options, func(message realtimeMessage) {
			if message.Topic != "config" {
				return
			}
			if client != nil {
				client.Send(machineRealtimeReceipt(message, "received", ""))
				client.Send(machineRealtimeReceipt(message, "applying", ""))
			}
			if m.onReload != nil {
				m.onReload()
			}
			if client != nil {
				client.Send(machineRealtimeReceipt(message, "applied", "reload queued"))
			}
		})
		client.Start()
		m.clientsMu.Lock()
		m.clients = append(m.clients, client)
		m.clientsMu.Unlock()
		log.WithFields(log.Fields{
			"machine_id": options.MachineID,
			"url":        options.URL,
		}).Info("Machine realtime websocket started")
	}
}

func (m *MachineRealtimeManager) Close() {
	if m == nil {
		return
	}
	m.cancel()
	m.clientsMu.Lock()
	clients := append([]*RealtimeClient(nil), m.clients...)
	m.clients = nil
	m.clientsMu.Unlock()
	for _, client := range clients {
		client.Close()
	}
}

func resolveMachineRealtimeOptions(profile conf.MachineProfileConfig, fallback conf.RealtimeConfig) *RealtimeOptions {
	if profile.MachineID <= 0 || strings.TrimSpace(profile.Key) == "" {
		return nil
	}

	profileRealtime := profile.Realtime
	url := strings.TrimSpace(fallback.URL)
	if url == "" {
		url = strings.TrimSpace(profileRealtime.URL)
	}
	if url == "" {
		url = deriveRealtimeURL(profile.APIHost)
	}

	enabled := fallback.Enabled || strings.TrimSpace(fallback.URL) != "" ||
		profileRealtime.Enabled || strings.TrimSpace(profileRealtime.URL) != ""
	if !enabled || url == "" {
		return nil
	}

	pingInterval := fallback.PingInterval
	if pingInterval <= 0 {
		pingInterval = profileRealtime.PingInterval
	}
	if pingInterval <= 0 {
		pingInterval = 30
	}

	reconnectInterval := fallback.ReconnectInterval
	if reconnectInterval <= 0 {
		reconnectInterval = 5
	}

	return &RealtimeOptions{
		URL:            url,
		Token:          strings.TrimSpace(profile.Key),
		NodeID:         0,
		MachineID:      profile.MachineID,
		NodeType:       "v2node",
		PingInterval:   time.Duration(pingInterval) * time.Second,
		ReconnectDelay: time.Duration(reconnectInterval) * time.Second,
		LogTag:         "[" + strings.TrimRight(strings.TrimSpace(profile.APIHost), "/") + "]-machine:" + strconv.Itoa(profile.MachineID),
	}
}

func machineRealtimeReceipt(source realtimeMessage, status string, message string) realtimeMessage {
	return realtimeMessage{
		Type:    "receipt",
		Topic:   "config",
		EventID: source.EventID,
		Reason:  source.Reason,
		Status:  status,
		Message: truncateRealtimeReceiptMessage(message),
		Ts:      time.Now().Unix(),
	}
}
