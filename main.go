package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/http/pprof"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.uber.org/atomic"
)

var (
	apiAddress        string
	monitoringAddress string
	launchDelay       = time.Second * 3
	shutdownDelay     = time.Second * 3
	isReady           = atomic.NewBool(false)
	err               error
)

func init() {
	apiAddress = os.Getenv("API_ADDRESS")
	if apiAddress == "" {
		apiAddress = ":8000"
	}

	monitoringAddress = os.Getenv("MONITORING_ADDRESS")
	if monitoringAddress == "" {
		monitoringAddress = ":8001"
	}

	launchDelayStr := os.Getenv("LAUNCH_DELAY")
	if launchDelayStr != "" {
		launchDelay, err = time.ParseDuration(launchDelayStr)
		if err != nil {
			log.Fatalf("Incorrect format of LAUNCH_DELAY %s\n", launchDelayStr)
		}
	}

	shutdownDelayStr := os.Getenv("SHUTDOWN_DELAY")
	if shutdownDelayStr != "" {
		shutdownDelay, err = time.ParseDuration(shutdownDelayStr)
		if err != nil {
			log.Fatalf("Incorrect format of SHUTDOWN_DELAY %s\n", shutdownDelayStr)
		}
	}
}

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		ch := make(chan os.Signal, 1)
		signal.Notify(ch, syscall.SIGHUP, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT)
		<-ch
		cancel()
	}()

	log.Printf("Run monitoring server by address %s\n", monitoringAddress)
	monitoringServer := runServer(monitoringAddress, getMonitoringHandler())

	log.Printf("Emulate launch application, delay %s\n", launchDelay)
	time.Sleep(launchDelay)

	log.Printf("Run api server by address %s\n", apiAddress)
	apiServer := runServer(apiAddress, &apiHandler{})
	isReady.Store(true)

	<-ctx.Done()
	isReady.Store(false)
	log.Printf("Shutdown application, delay %s\n", shutdownDelay)

	ctx, cancel = context.WithTimeout(ctx, shutdownDelay)
	defer cancel()

	go func() {
		_ = apiServer.Shutdown(ctx)
	}()
	go func() {
		_ = monitoringServer.Shutdown(ctx)
	}()

	time.Sleep(shutdownDelay)
}

func getMonitoringHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/debug/pprof/", pprof.Index)
	mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	mux.HandleFunc("/debug/pprof/trace", pprof.Trace)
	mux.HandleFunc("/metrics", promhttp.Handler().ServeHTTP)
	mux.HandleFunc("/ready", func(rw http.ResponseWriter, r *http.Request) {
		if !isReady.Load() {
			rw.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		rw.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/live", func(rw http.ResponseWriter, r *http.Request) {
		if !isReady.Load() {
			rw.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		rw.WriteHeader(http.StatusOK)
	})
	return mux
}

type apiHandler struct {
}

func (h *apiHandler) ServeHTTP(rw http.ResponseWriter, r *http.Request) {
	builder := &strings.Builder{}
	_, _ = fmt.Fprintf(builder, "%s %s %s\n", r.Method, r.URL, r.Proto)
	_, _ = fmt.Fprintln(builder, "Headers:")
	for k, v := range r.Header {
		_, _ = fmt.Fprintf(builder, "\t%q = %q\n", k, v)
	}
	_, _ = fmt.Fprintf(builder, "Host = %q\n", r.Host)
	_, _ = fmt.Fprintf(builder, "RemoteAddr = %q\n", r.RemoteAddr)

	_, _ = rw.Write([]byte(builder.String()))
}

func runServer(address string, handler http.Handler) *http.Server {
	server := &http.Server{Addr: address, Handler: handler}

	wg := sync.WaitGroup{}
	wg.Add(1)
	go func() {
		wg.Done()
		err := server.ListenAndServe()
		if err != nil {
			if errors.Is(err, http.ErrServerClosed) {
				log.Printf("The server by address %s was closed\n", address)
				return
			}
			log.Fatalf("An error %v occurred while running server by address %s", err, address)
		}
	}()
	wg.Wait()
	return server
}
