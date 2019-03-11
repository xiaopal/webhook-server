package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/elazarl/goproxy"
	"github.com/xiaopal/kube-informer/pkg/appctx"
	"github.com/xiaopal/kube-informer/pkg/subreaper"
)

const (
	handlerTypeSimple = "simple"
	handlerTypeJSON   = "json"
	handlerTypeFd     = "fd"
)

var (
	logger             *log.Logger
	serverBindAddr     string
	egressProxy        string
	location           string
	exposeFormValues   bool
	exposeHeaders      bool
	handlerName        string
	handlerType        string
	handlerArgs        []string
	jsonHandlers       bool
	accessLogs         bool
	requestData        bool
	handlerConcurrency int
	handlerWaitTimeout int
	handlerTimeout     int
	handlerExtractor   func(*exec.Cmd, http.ResponseWriter) error
)

func newLogger(prefix string) *log.Logger {
	return log.New(os.Stderr, prefix, log.Flags())
}

func httpServ() error {
	app, server := appctx.Start(), &http.Server{Addr: serverBindAddr}
	defer app.End()

	if os.Getpid() == 1 {
		subreaper.Start(app.Context())
	}

	limitSem := make(chan bool, handlerConcurrency)
	http.HandleFunc(location, func(res http.ResponseWriter, req *http.Request) {
		if accessLogs {
			res = &accessLogWriter{res, func(statusCode int) {
				newLogger("[access] ").Printf("%d %s %s - %s %s", statusCode, req.Method, req.URL.RequestURI(), req.RemoteAddr, req.UserAgent())
			}}
		}
		logger := newLogger(fmt.Sprintf("[%s] ", handlerName))
		if err := handleRequest(res, req, limitSem, logger); err != nil {
			logger.Printf("%s %s: %v", req.Method, req.URL.RequestURI(), err)
			http.Error(res, err.Error(), http.StatusInternalServerError)
		}
	})

	logger.Printf("Serving %s ...", serverBindAddr)
	if egressProxy != "" {
		proxy := goproxy.NewProxyHttpServer()
		proxy.Logger = newLogger("[egress] ")
		proxyserver := &http.Server{Addr: egressProxy, Handler: goproxy.NewProxyHttpServer()}
		if addr := strings.Split(egressProxy, ":"); len(addr) == 2 {
			egressProxy = fmt.Sprintf("127.0.0.1:%s", addr[len(addr)-1])
		} else {
			return fmt.Errorf("invalid --egress-proxy")
		}
		proxy.Logger.Printf("Serving egress proxy %s (http_proxy=%s) ...", proxyserver.Addr, egressProxy)
		go func() {
			if err := proxyserver.ListenAndServe(); err != nil {
				proxy.Logger.Printf("egress proxy: %v", err)
				app.End()
			}
		}()
		defer func() {
			proxy.Logger.Printf("Closing %s ...", proxyserver.Addr)
			if err := proxyserver.Close(); err != nil {
				proxy.Logger.Printf("failed to close egress proxy: %v", err)
			}
		}()
	}
	go func() {
		app.WaitGroup().Add(1)
		defer app.WaitGroup().Done()
		<-app.Context().Done()
		logger.Printf("Closing %s ...", serverBindAddr)
		ctx, _ := context.WithTimeout(context.TODO(), time.Second*60)
		if err := server.Shutdown(ctx); err != nil {
			logger.Printf("failed to shutdown server: %v", err)
			if err = server.Close(); err != nil {
				logger.Printf("failed to close server: %v", err)
			}
		}
	}()
	return server.ListenAndServe()
}

type accessLogWriter struct {
	res    http.ResponseWriter
	logger func(int)
}

func (w *accessLogWriter) Header() http.Header {
	return w.res.Header()
}
func (w *accessLogWriter) Write(body []byte) (int, error) {
	w.log(200)
	return w.res.Write(body)
}
func (w *accessLogWriter) WriteHeader(statusCode int) {
	w.log(statusCode)
	w.res.WriteHeader(statusCode)
}

func (w *accessLogWriter) log(statusCode int) {
	if w.logger != nil {
		w.logger(statusCode)
		w.logger = nil
	}
}

func main() {
	logger = newLogger("[server] ")

	flag.StringVar(&serverBindAddr, "bind-addr", ":8080", "server bind addr")
	flag.StringVar(&location, "location", "/", "location")
	flag.StringVar(&handlerName, "name", "handler", "handler name")
	flag.StringVar(&handlerType, "type", "simple", "handler type: simple, json, fd")
	flag.IntVar(&handlerConcurrency, "concurrency", 50, "handler max concurrency")
	flag.IntVar(&handlerWaitTimeout, "wait-timeout", -1, "handler max wait time")
	flag.IntVar(&handlerTimeout, "timeout", -1, "handler max time")
	flag.BoolVar(&exposeFormValues, "form-values", false, "expose form values")
	flag.BoolVar(&exposeHeaders, "headers", false, "expose headers")
	flag.BoolVar(&jsonHandlers, "json-handlers", false, "deprecated, same as --type=json")
	flag.BoolVar(&accessLogs, "v", false, "show access logs")
	flag.BoolVar(&requestData, "data", false, "pass request data to stdin")
	flag.StringVar(&egressProxy, "egress-proxy", "", "start and setup egress http/https proxy, eg :8888")
	flag.Parse()

	if jsonHandlers {
		handlerType = handlerTypeJSON
	}

	if extractor, ok := extractors[handlerType]; ok {
		handlerExtractor = extractor
	} else {
		logger.Fatal(fmt.Sprintf("unknown handler type: %s", handlerType))
	}

	if handlerArgs = flag.Args(); len(handlerArgs) < 1 {
		logger.Fatal("handler args required")
	}

	logger.Fatal(httpServ())
}
