package balanceServer

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"testing"
	"time"
)

func newHttpServer(addr string, id int) *http.Server {
	mux := http.NewServeMux()
	httpSrv := &http.Server{
		Addr:    addr,
		Handler: mux,
	}
	mux.HandleFunc("/", func(w http.ResponseWriter, request *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(fmt.Sprintf("Hello World, this is %d node", id)))
	})
	go func() {
		_ = httpSrv.ListenAndServe()
	}()
	return httpSrv

}
func TestServer(t *testing.T) {
	t.Run("测试负载均衡服务", func(t *testing.T) {
		s := NewServer(":8888")
		// 初始化3个demo http服务器 并加入负载均衡中
		c1 := newHttpServer(":9990", 1)
		_ = s.AddUpstream("http://:9990")
		c2 := newHttpServer(":9991", 2)
		_ = s.AddUpstream("http://:9991")
		c3 := newHttpServer(":9992", 3)
		_ = s.AddUpstream("http://:9992")
		c4 := newHttpServer(":9993", 4)
		_ = c1
		_ = c2
		_ = c3
		_ = c4
		go func() { // 启动服务
			_ = s.Start()
		}()
		go func() { // 3秒后关闭上游2
			time.Sleep(3 * time.Second)
			_ = c2.Shutdown(context.Background())
		}()
		go func() { // 6秒后添加上游4
			time.Sleep(6 * time.Second)
			_ = s.AddUpstream("http://:9993")
		}()
		time.Sleep(10 * time.Millisecond)
		var resp string
		var err error
		reqUrl := "http://127.0.0.1:8888"
		for i := 0; i < 30; i++ { // 请求负载均衡
			resp, err = getResp(reqUrl)
			log.Println(resp, err)
			time.Sleep(time.Second)
		}
	})
}
func getResp(url string) (string, error) {
	resp, err := http.Get(url)
	if err != nil {
		return "", err
	}
	defer func() {
		_ = resp.Body.Close()
	}()
	if resp.StatusCode != 200 {
		return fmt.Sprintf("req error status code: %d\n", resp.StatusCode), nil
	}
	bts, _ := io.ReadAll(resp.Body)
	return string(bts), nil
}
