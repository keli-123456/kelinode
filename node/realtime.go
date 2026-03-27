package node

import (
	"context"
	"encoding/json"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	log "github.com/sirupsen/logrus"
)

type realtimeMessage struct {
	Type     string `json:"type"`
	Topic    string `json:"topic"`
	Reason   string `json:"reason"`
	Revision int64  `json:"revision"`
	Ts       int64  `json:"ts"`
}

type RealtimeOptions struct {
	URL            string
	Token          string
	NodeID         int
	NodeType       string
	PingInterval   time.Duration
	ReconnectDelay time.Duration
	LogTag         string
}

type RealtimeClient struct {
	ctx       context.Context
	cancel    context.CancelFunc
	opts      RealtimeOptions
	onMessage func(realtimeMessage)
	wg        sync.WaitGroup
}

func NewRealtimeClient(parent context.Context, opts RealtimeOptions, onMessage func(realtimeMessage)) *RealtimeClient {
	ctx, cancel := context.WithCancel(parent)
	return &RealtimeClient{
		ctx:       ctx,
		cancel:    cancel,
		opts:      opts,
		onMessage: onMessage,
	}
}

func (c *RealtimeClient) Start() {
	c.wg.Add(1)
	go c.run()
}

func (c *RealtimeClient) Close() {
	c.cancel()
	c.wg.Wait()
}

func (c *RealtimeClient) run() {
	defer c.wg.Done()

	for {
		select {
		case <-c.ctx.Done():
			return
		default:
		}

		if err := c.connectAndServe(); err != nil && c.ctx.Err() == nil {
			log.WithFields(log.Fields{
				"tag": c.opts.LogTag,
				"err": err,
			}).Warn("Realtime connection closed")
		}

		select {
		case <-c.ctx.Done():
			return
		case <-time.After(c.opts.ReconnectDelay):
		}
	}
}

func (c *RealtimeClient) connectAndServe() error {
	dialURL, err := c.buildDialURL()
	if err != nil {
		return err
	}

	conn, _, err := websocket.DefaultDialer.DialContext(c.ctx, dialURL, nil)
	if err != nil {
		return err
	}
	defer conn.Close()

	log.WithField("tag", c.opts.LogTag).Info("Realtime websocket connected")

	var writeMu sync.Mutex
	writeJSON := func(message realtimeMessage) error {
		writeMu.Lock()
		defer writeMu.Unlock()
		if err := conn.SetWriteDeadline(time.Now().Add(10 * time.Second)); err != nil {
			return err
		}
		return conn.WriteJSON(message)
	}

	if err := writeJSON(realtimeMessage{Type: "ping", Ts: time.Now().Unix()}); err != nil {
		return err
	}

	done := make(chan struct{})
	defer close(done)

	go func() {
		if c.opts.PingInterval <= 0 {
			return
		}

		ticker := time.NewTicker(c.opts.PingInterval)
		defer ticker.Stop()

		for {
			select {
			case <-done:
				return
			case <-c.ctx.Done():
				return
			case <-ticker.C:
				if err := writeJSON(realtimeMessage{Type: "ping", Ts: time.Now().Unix()}); err != nil {
					return
				}
			}
		}
	}()

	for {
		select {
		case <-c.ctx.Done():
			return nil
		default:
		}

		_, payload, err := conn.ReadMessage()
		if err != nil {
			return err
		}

		var message realtimeMessage
		if err := json.Unmarshal(payload, &message); err != nil {
			continue
		}

		switch message.Type {
		case "hello_ack":
			log.WithField("tag", c.opts.LogTag).Info("Realtime websocket authenticated")
		case "ping":
			_ = writeJSON(realtimeMessage{Type: "pong", Ts: time.Now().Unix()})
		case "invalidate":
			if c.onMessage != nil {
				c.onMessage(message)
			}
		}
	}
}

func (c *RealtimeClient) buildDialURL() (string, error) {
	parsed, err := url.Parse(c.opts.URL)
	if err != nil {
		return "", err
	}

	query := parsed.Query()
	query.Set("token", c.opts.Token)
	query.Set("node_id", strconv.Itoa(c.opts.NodeID))
	query.Set("node_type", c.opts.NodeType)
	parsed.RawQuery = query.Encode()
	return parsed.String(), nil
}

func (c *Controller) startRealtime() {
	options := c.resolveRealtimeOptions()
	if options == nil {
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	c.realtimeCancel = cancel
	c.realtimeConfigCh = make(chan struct{}, 1)
	c.realtimeUserCh = make(chan struct{}, 1)

	go c.runRealtimeConfigWorker(ctx)
	go c.runRealtimeUserWorker(ctx)

	c.realtimeClient = NewRealtimeClient(ctx, *options, func(message realtimeMessage) {
		switch message.Topic {
		case "config":
			c.enqueueRealtimeConfigCheck()
		case "users":
			c.enqueueRealtimeUserSync()
		}
	})
	c.realtimeClient.Start()
}

func (c *Controller) resolveRealtimeOptions() *RealtimeOptions {
	var (
		panelEnabled bool
		panelURL     string
		panelPing    time.Duration
	)

	if c.info != nil && c.info.Common != nil && c.info.Common.BaseConfig != nil && c.info.Common.BaseConfig.Realtime != nil {
		panelEnabled = c.info.Common.BaseConfig.Realtime.Enabled
		panelURL = strings.TrimSpace(c.info.Common.BaseConfig.Realtime.URL)
		panelPing = realtimeIntervalToDuration(c.info.Common.BaseConfig.Realtime.PingInterval)
	}

	localURL := strings.TrimSpace(c.realtimeConfig.URL)
	enabled := c.realtimeConfig.Enabled || localURL != "" || panelEnabled
	if !enabled {
		return nil
	}

	if localURL == "" {
		localURL = panelURL
	}
	if localURL == "" {
		localURL = deriveRealtimeURL(c.conf.APIHost)
	}
	if localURL == "" {
		return nil
	}

	pingInterval := panelPing
	if c.realtimeConfig.PingInterval > 0 {
		pingInterval = time.Duration(c.realtimeConfig.PingInterval) * time.Second
	}
	if pingInterval <= 0 {
		pingInterval = 30 * time.Second
	}

	reconnectDelay := 5 * time.Second
	if c.realtimeConfig.ReconnectInterval > 0 {
		reconnectDelay = time.Duration(c.realtimeConfig.ReconnectInterval) * time.Second
	}

	return &RealtimeOptions{
		URL:            localURL,
		Token:          c.conf.Key,
		NodeID:         c.conf.NodeID,
		NodeType:       "v2node",
		PingInterval:   pingInterval,
		ReconnectDelay: reconnectDelay,
		LogTag:         c.tag,
	}
}

func (c *Controller) enqueueRealtimeConfigCheck() {
	select {
	case c.realtimeConfigCh <- struct{}{}:
	default:
	}
}

func (c *Controller) enqueueRealtimeUserSync() {
	select {
	case c.realtimeUserCh <- struct{}{}:
	default:
	}
}

func (c *Controller) runRealtimeConfigWorker(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-c.realtimeConfigCh:
			c.drainRealtimeChannel(c.realtimeConfigCh)
			execCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
			_ = c.nodeConfigMonitor(execCtx)
			cancel()
		}
	}
}

func (c *Controller) runRealtimeUserWorker(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-c.realtimeUserCh:
			c.drainRealtimeChannel(c.realtimeUserCh)
			execCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
			_ = c.nodeUserMonitor(execCtx)
			cancel()
		}
	}
}

func (c *Controller) drainRealtimeChannel(ch chan struct{}) {
	for {
		select {
		case <-ch:
		default:
			return
		}
	}
}

func deriveRealtimeURL(apiHost string) string {
	parsed, err := url.Parse(strings.TrimSpace(apiHost))
	if err != nil || parsed.Host == "" {
		return ""
	}

	switch parsed.Scheme {
	case "http":
		parsed.Scheme = "ws"
	case "https":
		parsed.Scheme = "wss"
	case "ws", "wss":
	default:
		parsed.Scheme = "ws"
	}

	parsed.Path = "/ws/node"
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String()
}

func realtimeIntervalToDuration(value interface{}) time.Duration {
	if value == nil {
		return 0
	}

	switch reflect.TypeOf(value).Kind() {
	case reflect.Int:
		return time.Duration(value.(int)) * time.Second
	case reflect.String:
		seconds, _ := strconv.Atoi(value.(string))
		return time.Duration(seconds) * time.Second
	case reflect.Float64:
		return time.Duration(value.(float64)) * time.Second
	default:
		return time.Duration(reflect.ValueOf(value).Int()) * time.Second
	}
}
