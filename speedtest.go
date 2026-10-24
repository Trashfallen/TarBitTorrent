package main

// Замер скорости интернета.
//
// Speedtest встроен: ближайший сервер Ookla (как на speedtest.net), пинг,
// загрузка и отдача; текущая скорость читается во время замера для шкалы.
// На время замера раздачи притормаживаются, иначе они съедают канал и портят
// результат. Яндекс Интернетометр открытого API не имеет, поэтому открывается
// в отдельном окне программы (webwindow.go) - замер там официальный.

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"sync"
	"time"

	"github.com/showwin/speedtest-go/speedtest"
)

type SpeedResult struct {
	Time    int64   `json:"time"`
	Ping    float64 `json:"ping"`   // мс
	Jitter  float64 `json:"jitter"` // мс
	Down    float64 `json:"down"`   // Мбит/с
	Up      float64 `json:"up"`     // Мбит/с
	Server  string  `json:"server"`
	Sponsor string  `json:"sponsor"`
	ISP     string  `json:"isp"`
}

type SpeedState struct {
	Running bool          `json:"running"`
	Phase   string        `json:"phase"` // servers | ping | download | upload | done | error | stopped
	Live    float64       `json:"live"`  // Мбит/с прямо сейчас
	Current SpeedResult   `json:"current"`
	Error   string        `json:"error"`
	History []SpeedResult `json:"history"`
}

type SpeedTest struct {
	app    *App
	mu     sync.Mutex
	st     SpeedState
	cancel context.CancelFunc
}

func newSpeedTest(app *App) *SpeedTest {
	s := &SpeedTest{app: app}
	if b, err := os.ReadFile(app.paths.file("speedtests.json")); err == nil {
		_ = json.Unmarshal(b, &s.st.History)
	}
	if s.st.History == nil {
		s.st.History = []SpeedResult{}
	}
	return s
}

func (s *SpeedTest) State() SpeedState {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.st
	st.History = append([]SpeedResult(nil), s.st.History...)
	return st
}

func (s *SpeedTest) set(f func(*SpeedState)) {
	s.mu.Lock()
	f(&s.st)
	s.mu.Unlock()
}

func (s *SpeedTest) Start(throttle bool) error {
	s.mu.Lock()
	if s.st.Running {
		s.mu.Unlock()
		return errors.New("замер уже идет")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	s.cancel = cancel
	s.st.Running, s.st.Phase, s.st.Live, s.st.Error = true, "servers", 0, ""
	s.st.Current = SpeedResult{}
	s.mu.Unlock()
	go s.run(ctx, throttle)
	return nil
}

func (s *SpeedTest) Stop() {
	s.mu.Lock()
	c := s.cancel
	s.mu.Unlock()
	if c != nil {
		c()
	}
}

func mbps(bytesPerSec float64) float64 { return finite(bytesPerSec * 8 / 1e6) }

func (s *SpeedTest) run(ctx context.Context, throttle bool) {
	if throttle {
		s.app.engine.throttle(true)
		defer s.app.engine.throttle(false)
	}
	fail := func(err error) {
		phase, msg := "error", err.Error()
		if errors.Is(ctx.Err(), context.Canceled) {
			phase, msg = "stopped", ""
		} else if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			msg = "замер не уложился в 2 минуты"
		}
		s.set(func(st *SpeedState) { st.Running, st.Phase, st.Error, st.Live = false, phase, msg, 0 })
	}
	defer s.cancel()

	client := speedtest.New(speedtest.WithUserConfig(&speedtest.UserConfig{UserAgent: appName + "/" + appVersion}))
	if user, err := client.FetchUserInfoContext(ctx); err == nil {
		s.set(func(st *SpeedState) { st.Current.ISP = user.Isp })
	}
	servers, err := client.FetchServerListContext(ctx)
	if err != nil {
		fail(errors.New("список серверов Speedtest недоступен: " + err.Error()))
		return
	}
	srv := fastestServer(ctx, servers)
	if srv == nil {
		fail(errors.New("ни один сервер Speedtest не ответил"))
		return
	}
	s.set(func(st *SpeedState) {
		st.Phase, st.Current.Server, st.Current.Sponsor = "ping", srv.Name, srv.Sponsor
	})
	s.set(func(st *SpeedState) {
		st.Current.Ping = float64(srv.Latency.Microseconds()) / 1000
		st.Current.Jitter = float64(srv.Jitter.Microseconds()) / 1000
		st.Phase = "download"
	})

	live := func(rate func() float64) (stop func()) {
		done := make(chan struct{})
		go func() {
			t := time.NewTicker(200 * time.Millisecond)
			defer t.Stop()
			for {
				select {
				case <-done:
					return
				case <-t.C:
					v := mbps(rate())
					s.set(func(st *SpeedState) { st.Live = v })
				}
			}
		}()
		return func() { close(done) }
	}

	stop := live(client.GetEWMADownloadRate)
	err = srv.DownloadTestContext(ctx)
	stop()
	if err != nil || ctx.Err() != nil {
		fail(errors.Join(err, ctx.Err()))
		return
	}
	down := mbps(float64(srv.DLSpeed))
	s.set(func(st *SpeedState) { st.Current.Down, st.Phase, st.Live = down, "upload", 0 })
	client.Reset()

	stop = live(client.GetEWMAUploadRate)
	err = srv.UploadTestContext(ctx)
	stop()
	if err != nil || ctx.Err() != nil {
		fail(errors.Join(err, ctx.Err()))
		return
	}
	up := mbps(float64(srv.ULSpeed))

	s.mu.Lock()
	s.st.Current.Up = up
	s.st.Current.Time = time.Now().Unix()
	s.st.Running, s.st.Phase, s.st.Live = false, "done", 0
	s.st.History = appendRing(append([]SpeedResult(nil), s.st.History...), s.st.Current, 20)
	hist := append([]SpeedResult(nil), s.st.History...)
	s.mu.Unlock()
	if b, err := json.MarshalIndent(hist, "", " "); err == nil {
		_ = writeAtomic(s.app.paths.file("speedtests.json"), b)
	}
	logf("Замер скорости: пинг %.0f мс, загрузка %.1f Мбит/с, отдача %.1f Мбит/с (%s)", s.st.Current.Ping, down, up, srv.Sponsor)
}

// fastestServer: из 10 ближайших по геолокации берет сервер с самым низким
// пингом. Геолокация по IP часто ошибается (особенно через VPN), а пинг - нет.
func fastestServer(ctx context.Context, servers speedtest.Servers) *speedtest.Server {
	cands := []*speedtest.Server(servers)
	if len(cands) > 10 {
		cands = cands[:10]
	}
	pctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	var wg sync.WaitGroup
	for _, c := range cands {
		wg.Add(1)
		go func(c *speedtest.Server) {
			defer wg.Done()
			if err := c.PingTestContext(pctx, nil); err != nil {
				c.Latency = 0
			}
		}(c)
	}
	wg.Wait()
	var best *speedtest.Server
	for _, c := range cands {
		if c.Latency > 0 && (best == nil || c.Latency < best.Latency) {
			best = c
		}
	}
	return best
}
