package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

// 控制器只在本机，绝不能走代理。推送流不能设总超时，靠 stream 里的看门狗
var (
	apiClient    = &http.Client{Transport: &http.Transport{Proxy: nil}, Timeout: 90 * time.Second}
	streamClient = &http.Client{Transport: &http.Transport{Proxy: nil}}
)

// endpoint 返回控制接口地址和密钥；内核没就绪时 ok 为 false
func (e *engine) endpoint() (base, secret string, ok bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return fmt.Sprintf("http://127.0.0.1:%d", e.api.APIPort), e.api.Secret, e.ready
}

// stream 读按行推送的接口（/traffic），每行交给 fn。内核重启（连接断开）或 5 秒没有新行就返回
func (e *engine) stream(path string, fn func(line []byte)) error {
	base, secret, ok := e.endpoint()
	if !ok {
		return errors.New("内核未就绪")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, base+path, nil)
	if err != nil {
		return err
	}
	request.Header.Set("Authorization", "Bearer "+secret)
	response, err := streamClient.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return errors.New(response.Status)
	}
	watchdog := time.AfterFunc(5*time.Second, cancel)
	defer watchdog.Stop()
	scanner := bufio.NewScanner(response.Body)
	for scanner.Scan() {
		watchdog.Reset(5 * time.Second)
		fn(scanner.Bytes())
	}
	return scanner.Err()
}

func (e *engine) call(method, path string, body, out any) error {
	base, secret, ready := e.endpoint()
	if !ready {
		return errors.New("内核未就绪")
	}
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(data)
	}
	request, err := http.NewRequest(method, base+path, reader)
	if err != nil {
		return err
	}
	request.Header.Set("Authorization", "Bearer "+secret)
	response, err := apiClient.Do(request)
	if err != nil {
		return errors.New("连不上内核控制接口")
	}
	defer response.Body.Close()
	if response.StatusCode >= 300 {
		var failure struct{ Message string }
		json.NewDecoder(io.LimitReader(response.Body, 64<<10)).Decode(&failure)
		if failure.Message == "" {
			failure.Message = response.Status
		}
		return errors.New(failure.Message)
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(io.LimitReader(response.Body, 32<<20)).Decode(out)
}

// choose 切换选择组。只影响新连接，现有连接要不要断由调用的人决定（closeConnections）
func (e *engine) choose(group, name string) error {
	return e.call(http.MethodPut, "/proxies/"+url.PathEscape(group), map[string]string{"name": name}, nil)
}

// closeConnections 断开所有现有连接，浏览器会马上按新的选择重连。下载、通话也会断
func (e *engine) closeConnections() {
	e.call(http.MethodDelete, "/connections", nil, nil)
}
