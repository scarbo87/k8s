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
	isReady           = atomic.NewBool(false)
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

	log.Println("Emulate launch application")
	time.Sleep(time.Second * 3)

	log.Printf("Run api server by address %s\n", apiAddress)
	apiServer := runServer(apiAddress, &apiHandler{})
	isReady.Store(true)

	<-ctx.Done()
	isReady.Store(false)
	log.Println("Shutdown application")

	ctx, cancel = context.WithTimeout(ctx, time.Second)
	defer cancel()

	_ = apiServer.Shutdown(ctx)
	_ = monitoringServer.Shutdown(ctx)
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
