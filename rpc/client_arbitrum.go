// Copyright 2022 The go-ethereum Authors
// This file is part of the go-ethereum library.
//
// The go-ethereum library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-ethereum library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-ethereum library. If not, see <http://www.gnu.org/licenses/>.

package rpc

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
)

func DialTransport(ctx context.Context, rawUrl string, transport *http.Transport) (*Client, error) {
	u, err := url.Parse(rawUrl)
	if err != nil {
		return nil, err
	}

	var rpcClient *Client
	switch u.Scheme {
	case "http", "https":
		client := &http.Client{
			Transport: transport,
		}
		rpcClient, err = DialHTTPWithClient(rawUrl, client)
	case "ws", "wss":
		rpcClient, err = DialWebsocket(ctx, rawUrl, "")
	case "stdio":
		return DialStdIO(ctx)
	case "":
		return DialIPC(ctx, rawUrl)
	default:
		return nil, fmt.Errorf("no known transport for scheme %q in URL %s", u.Scheme, rawUrl)
	}
	if err != nil {
		return nil, err
	}
	return rpcClient, nil
}

func DialContextWithRequestHook(ctx context.Context, url string, hook RequestHook) (*Client, error) {
	client, err := DialContext(ctx, url)
	if err != nil {
		return nil, err
	}
	client.requestHook = hook
	return client, nil
}

type RequestHook interface {
	OnRequest(request interface{}) ResultHook
}

type ResultHook interface {
	OnResult(response interface{}, err error)
}

type noopResultHook struct{}

func (h noopResultHook) OnResult(interface{}, error) {
}

func (c *Client) onRequest(request interface{}) ResultHook {
	hooks := c.requestHook
	var respHooks ResultHook
	if hooks != nil {
		respHooks = hooks.OnRequest(request)
	}
	if respHooks == nil {
		respHooks = noopResultHook{}
	}
	return respHooks
}
