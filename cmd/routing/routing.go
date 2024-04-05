package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

const (
	debugLevelCst = 11

	signalChannelSize = 10

	promListenCst           = ":2081"
	promPathCst             = "/metrics"
	promMaxRequestsInFlight = 10
	promEnableOpenMetrics   = true
	quantileError           = 0.05
	summaryVecMaxAge        = 5 * time.Minute

	httpListenCst    = ":2080"
	httpRoutePathCst = "/example1/"

	configurePath = "/configure/"
	deletePath    = "/delete/"
	streamPath    = "/stream"

	httpMagicKeyValue = "abc123"
)

type liveStream struct {
	source string
	group  string
	port   string
	file   string
}

var (
	// Passed by "go build -ldflags" for the show version
	commit string
	date   string

	debugLevel int

	channels sync.Map

	pC = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Subsystem: "counters",
			Name:      "webffmpeg",
			Help:      "webffmpeg counters",
		},
		[]string{"function", "variable", "type"},
	)
	pH = promauto.NewSummaryVec(
		prometheus.SummaryOpts{
			Subsystem: "histrograms",
			Name:      "webffmpeg",
			Help:      "webffmpeg historgrams",
			Objectives: map[float64]float64{
				0.1:  quantileError,
				0.5:  quantileError,
				0.99: quantileError,
			},
			MaxAge: summaryVecMaxAge,
		},
		[]string{"function", "variable", "type"},
	)
)

func main() {

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go initSignalHandler(cancel)

	version := flag.Bool("version", false, "version")

	// https://pkg.go.dev/net#Listen
	promListen := flag.String("promListen", promListenCst, "Prometheus http listening socket")
	promPath := flag.String("promPath", promPathCst, "Prometheus http path")
	// curl -s http://[::1]:9111/metrics 2>&1 | grep -v "#"
	// curl -s http://127.0.0.1:9111/metrics 2>&1 | grep -v "#"
	httpListen := flag.String("httpListen", httpListenCst, "Http listening socket")

	dl := flag.Int("dl", debugLevelCst, "nasty debugLevel")

	flag.Parse()

	if *version {
		fmt.Println("commit:", commit, "\tdate(UTC):", date)
		os.Exit(0)
	}

	debugLevel = *dl

	go initPromHandler(ctx, *promPath, *promListen)

	debugLog(debugLevel > 10, fmt.Sprint("keep aliver start, starting web server on:", *httpListen))

	// It's recommended NOT to use the default mux
	mux := http.NewServeMux()

	sh := http.HandlerFunc(streamHandler)

	mux.Handle("POST "+configurePath, checkAuthHeaderMiddleware(configurePathMiddleware(sh)))
	mux.Handle("POST "+deletePath, checkAuthHeaderMiddleware(deletePathMiddleware(sh)))

	mux.Handle("GET "+streamPath, streamMiddleware(sh))

	mux.Handle(httpRoutePathCst, sanitizeRequestMiddleware(sh))

	srv := &http.Server{
		MaxHeaderBytes:               1 << 18, // 262,144 bytes, which is 1/4 the default
		DisableGeneralOptionsHandler: true,
		ReadHeaderTimeout:            5 * time.Second,
		ReadTimeout:                  10 * time.Second,
		WriteTimeout:                 10 * time.Second,
		IdleTimeout:                  65 * time.Second,
		Addr:                         *httpListen,
		Handler:                      mux,
	}
	// https://pkg.go.dev/net/http#DefaultMaxHeaderBytes
	// const DefaultMaxHeaderBytes = 1 << 20 // 1 MB

	err := srv.ListenAndServe()
	if err != nil {
		log.Fatal("http server error:", err)
	}

	log.Println("main: That's all Folks!")
}

func checkAuthHeaderMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

		val, ok := r.Header["Magic-Key"]
		if !ok {
			response := "Auth header missing"
			http.Error(w, response, http.StatusMethodNotAllowed)
			return
		}

		if val[0] != httpMagicKeyValue {
			response := "Auth failure"
			http.Error(w, response, http.StatusMethodNotAllowed)
			return
		}

		debugLog(debugLevel > 10, "checkAuthHeaderMiddleware, auth passed")

		next.ServeHTTP(w, r)
	})
}

func configurePathMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

		if strings.Count(r.URL.String(), "/") != 6 {
			debugLog(debugLevel > 10, "configurePathMiddleware, '/' != 6 ")
			response := "Invalid request must be of form /configure/stream/source/group/port/blah.m3u8"
			http.Error(w, response, http.StatusBadRequest)
			return
		}

		pathParts := strings.Split(r.URL.String(), "/")

		debugLog(debugLevel > 10, "configurePathMiddleware, pathParts:"+fmt.Sprint(pathParts))
		debugLog(debugLevel > 10, "configurePathMiddleware, len(pathParts):"+fmt.Sprint(len(pathParts)))

		name := pathParts[2]

		_, exists := channels.Load(name)
		if !exists {
			if lenSyncMap(&channels) > 2 {
				debugLog(debugLevel > 10, "configurePathMiddleware, !exists lenSyncMap(&channels) > 2")
				response := "Maximum number of configured live streams has been reached.  You could reconfigure an existing channel"
				http.Error(w, response, http.StatusBadRequest)
				return
			}
		}

		liveStream := &liveStream{
			source: pathParts[3],
			group:  pathParts[4],
			port:   pathParts[5],
			file:   pathParts[6],
		}
		debugLog(debugLevel > 10, "configurePathMiddleware, name:"+name)

		channels.Store(name, liveStream)
		debugLog(debugLevel > 10, "configurePathMiddleware, channels.Store("+name+",liveStream config")

	})
}

func deletePathMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

		if strings.Count(r.URL.String(), "/") != 2 {
			debugLog(debugLevel > 10, "deletePathMiddleware, '/' != 2 ")
			response := "Invalid request must be of form /delete/stream"
			http.Error(w, response, http.StatusBadRequest)
			return
		}

		pathParts := strings.Split(r.URL.String(), "/")

		debugLog(debugLevel > 10, "deletePathMiddleware, pathParts:"+fmt.Sprint(pathParts))
		debugLog(debugLevel > 10, "deletePathMiddleware, len(pathParts):"+fmt.Sprint(len(pathParts)))

		name := pathParts[2]

		_, exists := channels.Load(name)
		if !exists {
			response := "Stream does not exist"
			debugLog(debugLevel > 10, "deletePathMiddleware, "+response)
			http.Error(w, response, http.StatusBadRequest)
			return
		}

		channels.Delete(name)
		response := "Stream deleted"
		debugLog(debugLevel > 10, "deletePathMiddleware, "+response)
		http.Error(w, response, http.StatusOK)
		return
	})
}

func streamMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

		pathParts := strings.Split(r.URL.String(), "/")

		debugLog(debugLevel > 10, "streamMiddleware, pathParts:"+fmt.Sprint(pathParts))
		debugLog(debugLevel > 10, "streamMiddleware, len(pathParts):"+fmt.Sprint(len(pathParts)))

		name := pathParts[2]

		liveChannelConfig, ok := channels.Load(name)
		if !ok {
			debugLog(debugLevel > 10, "streamMiddleware, unknown channel:"+name)
			response := "unknown channel"
			http.Error(w, response, http.StatusBadRequest)
			return
		}

		debugLog(debugLevel > 10, "streamMiddleware,liveChannelConfig:"+fmt.Sprint(liveChannelConfig))

		response := "liveChannelConfig:" + fmt.Sprint(liveChannelConfig)
		http.Error(w, response, http.StatusBadRequest)
		//return

		//next.ServeHTTP(w, r)
	})
}

// sanitizeRequestMiddleware rejects any requests we don't like
// GETs only
// Only x2 "/" chars
// Must have suffix iether .m3u8 or .ts
func sanitizeRequestMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

		startTime := time.Now()
		defer func() {
			pH.WithLabelValues("sanitizeRequest", "start", "complete").Observe(time.Since(startTime).Seconds())
		}()
		pC.WithLabelValues("sanitizeRequest", "start", "counter").Inc()

		debugLog(debugLevel > 10, fmt.Sprintf("sanitizeRequestMiddleware, url:%s", r.URL.String()))

		if r.Method != "GET" {
			debugLog(debugLevel > 10, "sanitizeRequestMiddleware, not GET")
			response := "Only GET requests are allowed"
			http.Error(w, response, http.StatusMethodNotAllowed)
			return
		}

		if strings.Count(r.URL.String(), "/") > 2 {
			debugLog(debugLevel > 10, "sanitizeRequestMiddleware, '/' > 2 ")
			response := "Invalid request with too many slashes"
			http.Error(w, response, http.StatusBadRequest)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// streamHandler looks at the request file extention (.m3u8 or .ts )
// and calls the respective handler
func streamHandler(w http.ResponseWriter, r *http.Request) {

	startTime := time.Now()
	defer func() {
		pH.WithLabelValues("streamHandler", "start", "complete").Observe(time.Since(startTime).Seconds())
	}()
	pC.WithLabelValues("streamHandler", "start", "counter").Inc()

	debugLog(debugLevel > 1000, fmt.Sprintf("streamHandler, r.URL.String():%s:", r.URL.String()))

	// Request input validation has moved the sanitizeRequestMiddleware middleware

	extention := path.Ext(r.URL.String())
	switch extention {
	case ".m3u8":
		//manifestHandler(w, r)
		debugLog(debugLevel > 10, "streamHandler, .m3u8")
		response := "m3u8"
		http.Error(w, response, http.StatusBadRequest)
	case ".ts":
		//segmentHandler(w, r)
		debugLog(debugLevel > 10, "streamHandler, .ts")
		response := "ts"
		http.Error(w, response, http.StatusBadRequest)
	default:
		debugLog(debugLevel > 10, "streamHandler, not (.m3u8 or .ts )")
		response := "Invalid request not (.m3u8 or .ts )"
		http.Error(w, response, http.StatusBadRequest)
	}
}

// initSignalHandler sets up signal handling for the process, and
// will call cancel() when recieved
func initSignalHandler(cancel context.CancelFunc) {

	c := make(chan os.Signal, signalChannelSize)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)

	<-c
	log.Printf("Signal caught, closing application")
	cancel()
	os.Exit(0)
}

// initPromHandler starts the prom handler with error checking
func initPromHandler(ctx context.Context, promPath string, promListen string) {
	// https: //pkg.go.dev/github.com/prometheus/client_golang/prometheus/promhttp?tab=doc#HandlerOpts
	http.Handle(promPath, promhttp.HandlerFor(
		prometheus.DefaultGatherer,
		promhttp.HandlerOpts{
			EnableOpenMetrics:   promEnableOpenMetrics,
			MaxRequestsInFlight: promMaxRequestsInFlight,
		},
	))
	go func() {
		err := http.ListenAndServe(promListen, nil)
		if err != nil {
			log.Fatal("prometheus error", err)
		}
	}()
}

func debugLog(logIt bool, str string) {
	if logIt {
		log.Print(str)
	}
}

func lenSyncMap(m *sync.Map) (len int) {
	m.Range(func(key, value interface{}) bool {
		len++
		return true
	})
	return len
}

// usePocessTracker recieves notifications from keepaliver about the state of the
// process being kept alive
// Once the process reaches the minimim age, we can use the live stream
// If the process is stopped, then we can't
func usePocessTracker(runCh <-chan string, minAgeCh <-chan string, stoppedCh <-chan string) {

	for {
		forStartTime := time.Now()
		select {
		case stream <- runCh:

		case <-minAgeCh:
			pH.WithLabelValues("usePocessTracker", "minAgeCh", "complete").Observe(time.Since(forStartTime).Seconds())
			useLiveStreamLock.Lock()
			useLiveStream = true
			pC.WithLabelValues("usePocessTracker", "minAgeCh", "counter").Inc()
			debugLog(debugLevel > 10, "usePocessTracker, <-minAgeCh")
		case <-stoppedCh:
			pH.WithLabelValues("usePocessTracker", "minAgeCh", "complete").Observe(time.Since(forStartTime).Seconds())
			useLiveStreamLock.Lock()
			useLiveStream = false
			pC.WithLabelValues("usePocessTracker", "stoppedCh", "counter").Inc()
			debugLog(debugLevel > 10, "usePocessTracker, <-stoppedCh")
		}
		useLiveStreamLock.Unlock()
	}
}
