package balanceServer

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"github.com/buffge/consistentHash"
	"golang.org/x/sync/errgroup"
	"log"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

var (
	errUpstreamNotExist = errors.New("upstream not exist")
)

type Server struct {
	c                               context.Context
	cancel                          context.CancelFunc
	httpSrv                         *http.Server                   // http 服务
	reqTimes                        atomic.Uint64                  // 总请求次数
	consistent                      *consistentHash.ConsistentHash // 轮询一致性
	upstreamSet                     map[string]*Upstream           // 后端服务器set
	monitorInterval, monitorTimeout time.Duration                  // 监控间隔,超时时间
	monitorFlag                     atomic.Bool                    // 监控标记
	mu                              sync.RWMutex                   // 读写锁 修改后端时使用
}

func NewServer(addr string) *Server {
	ctx, cancel := context.WithCancel(context.TODO())
	s := &Server{
		c:               ctx,
		cancel:          cancel,
		consistent:      consistentHash.New(),
		upstreamSet:     make(map[string]*Upstream, 8),
		mu:              sync.RWMutex{},
		monitorInterval: time.Second * 5,
		monitorTimeout:  time.Second * 5,
	}
	httpSrv := &http.Server{
		Addr:    addr,
		Handler: s,
	}
	s.httpSrv = httpSrv
	return s
}

// Start 启动服务
func (s *Server) Start() error {
	g, c := errgroup.WithContext(s.c)
	g.Go(func() error { // http 服务
		err := s.httpSrv.ListenAndServe()
		return err
	})
	g.Go(func() error { // 监控服务
		return s.Monitor()
	})
	g.Go(func() error { // 服务平滑关闭
		<-c.Done()
		ctx, cancel := context.WithTimeout(context.TODO(), time.Second*5)
		defer cancel()
		err := s.httpSrv.Shutdown(ctx)
		return err
	})
	sigExitC := make(chan os.Signal, 1)
	signal.Notify(sigExitC, syscall.SIGTERM, syscall.SIGQUIT, syscall.SIGINT)
	g.Go(func() error { // 等待手动关闭或信号关闭
		for {
			select {
			case <-c.Done():
				return c.Err()
			case _ = <-sigExitC:
				_ = s.Stop()
			}
		}
	})
	if err := g.Wait(); err != nil && !errors.Is(err, context.Canceled) {
		return err
	}
	return nil
}

// Stop 停止服务
func (s *Server) Stop() error {
	s.cancel()
	return nil
}

func (s *Server) AddUpstream(baseUrl string) error {
	u, err := NewUpstream(baseUrl)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err = s.consistent.AddNode(u); err != nil {
		return err
	}
	s.upstreamSet[baseUrl] = u
	return nil
}
func (s *Server) RemoveUpstream(baseUrl string) error {
	u, exist := s.upstreamSet[baseUrl]
	if !exist {
		return errUpstreamNotExist
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.upstreamSet, baseUrl)
	s.consistent.RemoveNode(u.Key())
	return nil
}
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.reqTimes.Add(1)
	t := s.reqTimes.Load()
	u := s.consistent.GetNode(u642bytes(t))
	if u == nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}
	upstream := u.(*Upstream)
	ctx, cancel := context.WithTimeout(context.TODO(), time.Second*30)
	defer cancel()
	req := r.Clone(r.Context()).WithContext(ctx)
	upstream.Proxy(w, req)
}

// Monitor 监控
func (s *Server) Monitor() error {
	defer func() {
		if r := recover(); r != nil {
			log.Println("monitor recover", r)
		}
	}()
	for {
		select {
		case <-s.c.Done():
			return nil
		default:
		}
		time.Sleep(s.monitorInterval)
		if !s.monitorFlag.CompareAndSwap(false, true) {
			continue
		}
		c, cancel := context.WithTimeout(s.c, s.monitorTimeout)
		g, ctx := errgroup.WithContext(c)
		upstreams := make([]*Upstream, 0, len(s.upstreamSet))
		s.mu.RLock()
		for _, upstream := range s.upstreamSet {
			upstreams = append(upstreams, upstream)
		}
		s.mu.RUnlock()
		for _, v := range upstreams {
			upstream := v
			g.Go(func() (innerErr error) {
				defer func() {
					if r := recover(); r != nil {
						innerErr = errors.New(fmt.Sprint(r))
						return
					}
				}()
				if err := upstream.LiveCheck(ctx); err != nil {
					if upstream.disabled {
						return
					}
					upstream.disabled = true
					log.Println("upstream live check failed,removed", string(upstream.Key()), err)
					s.consistent.RemoveNode(upstream.Key())
					return
				}
				if upstream.disabled {
					log.Println("upstream live check success,enabled", string(upstream.Key()))
					upstream.disabled = false
					_ = s.consistent.AddNode(upstream)
				}
				return
			})
		}
		if err := g.Wait(); err != nil {
			log.Println("monitor errGroup recover", err)
		}
		s.monitorFlag.Store(false)
		cancel()

	}
}

func u642bytes(val uint64) []byte {
	return binary.LittleEndian.AppendUint64(make([]byte, 0, 8), val)
}
