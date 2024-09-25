package balanceServer

import (
	"context"
	"fmt"
	"github.com/buffge/consistentHash"
	"net/http"
	"net/http/httputil"
	"net/url"
	"time"
	"unsafe"
)

var (
	_ consistentHash.Node = (*Upstream)(nil)
)

type (
	Upstream struct {
		baseUrl  *url.URL               // 基础url
		disabled bool                   // 是否已禁用
		cli      *http.Client           // http 检查时使用
		proxy    *httputil.ReverseProxy // 反向代理工具
	}
)

func (u *Upstream) Key() []byte {
	return str2Bytes(u.baseUrl.String())
}

func NewUpstream(baseUrl string) (*Upstream, error) {
	fmtBaseUrl, err := url.Parse(baseUrl)
	if err != nil {
		return nil, err
	}
	return &Upstream{
		baseUrl:  fmtBaseUrl,
		disabled: false,
		cli: &http.Client{
			Timeout: time.Second * 5,
		},
		proxy: httputil.NewSingleHostReverseProxy(fmtBaseUrl),
	}, nil
}
func (u *Upstream) Proxy(w http.ResponseWriter, r *http.Request) {
	u.proxy.ServeHTTP(w, r)
}
func (u *Upstream) LiveCheck(ctx context.Context) error {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, u.baseUrl.String(), nil)
	resp, err := u.cli.Do(req)
	if err != nil {
		return err
	}
	defer func() {
		_ = resp.Body.Close()
	}()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("bad status: %s", resp.Status)
	}
	return nil
}
func str2Bytes(s string) []byte {
	return *(*[]byte)(unsafe.Pointer(&s))
}
