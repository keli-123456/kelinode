package panel

import (
	"net/http"
	"strconv"
	"time"

	"github.com/go-resty/resty/v2"
	"github.com/keli-123456/kelinode/conf"
)

// Panel is the interface for different panel's api.

type Client struct {
	client           *resty.Client
	APIHost          string
	Token            string
	NodeId           int
	MachineID        int
	ConfigDir        string
	nodeEtag         string
	userEtag         string
	responseBodyHash string
	UserList         *UserListBody
	AliveMap         *AliveMap
}

func (c *Client) SetTransport(transport http.RoundTripper) {
	if c == nil || c.client == nil || transport == nil {
		return
	}
	c.client.SetTransport(transport)
}

func New(c *conf.NodeConfig) (*Client, error) {
	client := resty.New()
	client.SetLogger(&silentRestyLogger{})
	client.SetRetryCount(3)
	if c.Timeout > 0 {
		client.SetTimeout(time.Duration(c.Timeout) * time.Second)
	} else {
		client.SetTimeout(30 * time.Second)
	}
	client.SetBaseURL(c.APIHost)
	// set params
	client.SetQueryParams(map[string]string{
		"node_type": "v2node",
		"node_id":   strconv.Itoa(c.NodeID),
		"token":     c.Key,
	})
	if c.MachineID > 0 {
		client.SetQueryParam("machine_id", strconv.Itoa(c.MachineID))
	}
	return &Client{
		client:    client,
		Token:     c.Key,
		APIHost:   c.APIHost,
		NodeId:    c.NodeID,
		MachineID: c.MachineID,
		ConfigDir: c.ConfigDir,
		UserList:  &UserListBody{},
		AliveMap:  &AliveMap{},
	}, nil
}
