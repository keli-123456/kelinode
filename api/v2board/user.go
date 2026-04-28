package panel

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"encoding/json/jsontext"
	"encoding/json/v2"

	"github.com/vmihailenco/msgpack/v5"
)

type OnlineUser struct {
	UID     int
	IP      string
	TagUUID string
}

type UserInfo struct {
	Id          int    `json:"id" msgpack:"id"`
	Uuid        string `json:"uuid" msgpack:"uuid"`
	SpeedLimit  int    `json:"speed_limit" msgpack:"speed_limit"`
	DeviceLimit int    `json:"device_limit" msgpack:"device_limit"`
}

type UserListBody struct {
	Users []UserInfo `json:"users" msgpack:"users"`
}

var ErrUserDeltaNotSupported = errors.New("user_delta not supported")

type UserDeltaBody struct {
	Full     bool       `json:"full" msgpack:"full"`
	Revision int64      `json:"revision" msgpack:"revision"`
	Users    []UserInfo `json:"users" msgpack:"users"`
	Deleted  []UserInfo `json:"deleted" msgpack:"deleted"`
	Upsert   []UserInfo `json:"upsert" msgpack:"upsert"`
}

type AliveMap struct {
	Alive    map[int]int      `json:"alive"`
	AliveIPs map[int][]string `json:"alive_ips"`
	Mode     int              `json:"mode"`
}

func cloneAliveMap(src map[int]int) map[int]int {
	if len(src) == 0 {
		return map[int]int{}
	}
	dst := make(map[int]int, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}

func cloneAliveSnapshot(src *AliveMap) *AliveMap {
	if src == nil {
		return &AliveMap{
			Alive:    map[int]int{},
			AliveIPs: map[int][]string{},
		}
	}

	snapshot := &AliveMap{
		Alive:    cloneAliveMap(src.Alive),
		AliveIPs: make(map[int][]string, len(src.AliveIPs)),
		Mode:     src.Mode,
	}
	for uid, ips := range src.AliveIPs {
		if len(ips) == 0 {
			snapshot.AliveIPs[uid] = []string{}
			continue
		}
		cloned := append([]string(nil), ips...)
		snapshot.AliveIPs[uid] = cloned
	}
	return snapshot
}

func (c *Client) CachedAliveMap() map[int]int {
	if c == nil || c.AliveMap == nil {
		return map[int]int{}
	}
	return cloneAliveMap(c.AliveMap.Alive)
}

func (c *Client) CachedAliveSnapshot() *AliveMap {
	if c == nil {
		return cloneAliveSnapshot(nil)
	}
	return cloneAliveSnapshot(c.AliveMap)
}

// GetUserList will pull user from v2board
func (c *Client) GetUserList(ctx context.Context) ([]UserInfo, error) {
	r, err := c.client.R().
		SetContext(ctx).
		SetHeader("If-None-Match", c.userEtag).
		SetHeader(HeaderResponseFormat, ResponseFormatMsgpack).
		SetDoNotParseResponse(true).
		Get(PathV1UniProxyUser)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return nil, nil
		}
		return nil, err
	}
	if r == nil || r.RawResponse == nil {
		return nil, fmt.Errorf("received nil response or raw response")
	}
	defer r.RawResponse.Body.Close()

	if r.StatusCode() == 304 {
		if etag := r.Header().Get("ETag"); etag != "" {
			c.userEtag = etag
		}
		return nil, nil
	}
	if r.StatusCode() >= 400 {
		data, _ := io.ReadAll(io.LimitReader(r.RawResponse.Body, 2048))
		return nil, fmt.Errorf("user list request failed: %s body=%s", r.Status(), strings.TrimSpace(string(data)))
	}
	userlist := &UserListBody{}
	if strings.Contains(r.Header().Get("Content-Type"), ContentTypeMsgpack) {
		decoder := msgpack.NewDecoder(r.RawResponse.Body)
		if err := decoder.Decode(userlist); err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(ctx.Err(), context.Canceled) {
				return nil, nil
			}
			return nil, fmt.Errorf("decode user list error: %w", err)
		}
	} else {
		dec := jsontext.NewDecoder(r.RawResponse.Body)
		for {
			tok, err := dec.ReadToken()
			if err != nil {
				if errors.Is(err, context.Canceled) || errors.Is(ctx.Err(), context.Canceled) {
					return nil, nil
				}
				return nil, fmt.Errorf("decode user list error: %w", err)
			}
			if tok.Kind() == '"' && tok.String() == "users" {
				break
			}
		}
		tok, err := dec.ReadToken()
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(ctx.Err(), context.Canceled) {
				return nil, nil
			}
			return nil, fmt.Errorf("decode user list error: %w", err)
		}
		if tok.Kind() != '[' {
			return nil, fmt.Errorf(`decode user list error: expected "users" array`)
		}
		for dec.PeekKind() != ']' {
			val, err := dec.ReadValue()
			if err != nil {
				if errors.Is(err, context.Canceled) || errors.Is(ctx.Err(), context.Canceled) {
					return nil, nil
				}
				return nil, fmt.Errorf("decode user list error: read user object: %w", err)
			}
			var u UserInfo
			if err := json.Unmarshal(val, &u); err != nil {
				return nil, fmt.Errorf("decode user list error: unmarshal user error: %w", err)
			}
			userlist.Users = append(userlist.Users, u)
		}
	}
	c.userEtag = r.Header().Get("ETag")
	return userlist.Users, nil
}

func (c *Client) GetUserDelta(ctx context.Context, since int64) (*UserDeltaBody, error) {
	r, err := c.client.R().
		SetContext(ctx).
		SetQueryParam("since", strconv.FormatInt(since, 10)).
		SetHeader(HeaderResponseFormat, ResponseFormatMsgpack).
		ForceContentType("application/json").
		Get(PathV1UniProxyUserDelta)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return nil, nil
		}
		return nil, err
	}
	if r == nil {
		return nil, fmt.Errorf("received nil response")
	}
	if isUserDeltaUnsupportedStatus(r.StatusCode()) {
		return nil, ErrUserDeltaNotSupported
	}
	if r.StatusCode() >= 400 {
		return nil, fmt.Errorf("user_delta request failed: %s", r.Status())
	}

	resp := &UserDeltaBody{}
	if err := json.Unmarshal(r.Body(), resp); err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(ctx.Err(), context.Canceled) {
			return nil, nil
		}
		return nil, fmt.Errorf("decode user delta error: %w", err)
	}
	return resp, nil
}

func isUserDeltaUnsupportedStatus(statusCode int) bool {
	switch statusCode {
	case 403, 404, 405, 501:
		return true
	default:
		return false
	}
}

// GetUserAlive will fetch the alive_ip count for users
func (c *Client) GetUserAlive(ctx context.Context) (map[int]int, error) {
	if c.AliveMap == nil {
		c.AliveMap = &AliveMap{}
	}
	r, err := c.client.R().
		SetContext(ctx).
		ForceContentType("application/json").
		Get(PathV1UniProxyAliveList)

	if err != nil {
		if errors.Is(err, context.Canceled) {
			return nil, nil
		}
		return nil, err
	}
	if r == nil || r.RawResponse == nil {
		return nil, fmt.Errorf("received nil response or raw response")
	}
	defer r.RawResponse.Body.Close()
	if r.StatusCode() >= 399 {
		return nil, fmt.Errorf("fetch user alive list failed: %s", r.Status())
	}
	next := &AliveMap{}
	if err := json.Unmarshal(r.Body(), next); err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(ctx.Err(), context.Canceled) {
			return nil, nil
		}
		return nil, fmt.Errorf("decode user alive list error: %w", err)
	}
	if next.Alive == nil {
		next.Alive = make(map[int]int)
	}
	if next.AliveIPs == nil {
		next.AliveIPs = make(map[int][]string)
	}
	c.AliveMap = next

	return cloneAliveMap(next.Alive), nil
}

type UserTraffic struct {
	UID      int
	Upload   int64
	Download int64
}

// ReportUserTraffic reports the user traffic
func (c *Client) ReportUserTraffic(ctx context.Context, userTraffic []UserTraffic) error {
	data := make(map[int][]int64, len(userTraffic))
	for i := range userTraffic {
		data[userTraffic[i].UID] = []int64{userTraffic[i].Upload, userTraffic[i].Download}
	}
	r, err := c.client.R().
		SetContext(ctx).
		SetBody(data).
		ForceContentType("application/json").
		Post(PathV1UniProxyPush)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return nil
		}
		return err
	}
	if r != nil && r.StatusCode() >= 400 {
		return fmt.Errorf("report user traffic failed: %s", r.Status())
	}
	return nil
}

func (c *Client) ReportNodeOnlineUsers(ctx context.Context, data *map[int][]string) error {
	r, err := c.client.R().
		SetContext(ctx).
		SetBody(data).
		ForceContentType("application/json").
		Post(PathV1UniProxyAlive)

	if err != nil {
		if errors.Is(err, context.Canceled) {
			return nil
		}
		return err
	}
	if r != nil && r.StatusCode() >= 400 {
		return fmt.Errorf("report online users failed: %s", r.Status())
	}

	return nil
}
