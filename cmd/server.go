package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"strings"
)

var (
	logger           *log.Logger
	serverBindAddr   string
	location         string
	exposeFormValues bool
	exposeHeaders    bool
	handlerArgs      []string
	jsonHandlers     bool
	accessLogs       bool
)

func newLogger(prefix string) *log.Logger {
	return log.New(os.Stderr, prefix, log.Flags())
}

func setupEnv(handler *exec.Cmd, req *http.Request) error {
	env := append(os.Environ(),
		fmt.Sprintf("HTTP_REQUEST_HOST=%s", req.Host),
		fmt.Sprintf("HTTP_REQUEST_METHOD=%s", req.Method),
		fmt.Sprintf("HTTP_REQUEST_URI=%s", req.URL.RequestURI()),
		fmt.Sprintf("HTTP_REQUEST_PATH=%s", req.URL.Path),
		fmt.Sprintf("HTTP_REQUEST_QUERY=%s", req.URL.RawQuery),
	)

	if err := req.ParseForm(); err != nil {
		return err
	}

	form, err := json.Marshal(req.Form)
	if err != nil {
		return err
	}
	env = append(env, fmt.Sprintf("HTTP_REQUEST_FORM=%s", form))

	headers, err := json.Marshal(req.Header)
	if err != nil {
		return err
	}
	env = append(env, fmt.Sprintf("HTTP_REQUEST_HEADERS=%s", headers))

	envName := func(s string) string {
		return regexp.MustCompile(`[^\w\d]+`).ReplaceAllString(strings.ToUpper(s), "_")
	}

	if exposeFormValues {
		for k, v := range req.Form {
			env = append(env, fmt.Sprintf("FORM_%s=%s", envName(k), strings.Join(v, ",")))
		}
	}
	if exposeHeaders {
		for k, v := range req.Header {
			env = append(env, fmt.Sprintf("HEADER_%s=%s", envName(k), strings.Join(v, ",")))
		}
	}
	handler.Env = env
	return nil
}

func pipeStderr(cmd *exec.Cmd, logger *log.Logger) error {
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}
	go func() {
		o := bufio.NewScanner(stderr)
		for o.Scan() {
			logger.Println(o.Text())
		}
	}()
	return nil
}

type handlerResponse struct {
	Status  int               `json:"status"`
	Headers map[string]string `json:"headers"`
	Body    string            `json:"body"`
}

func handleRequest(res http.ResponseWriter, req *http.Request, logger *log.Logger) error {
	handler := exec.Command(handlerArgs[0], handlerArgs[1:]...)
	if err := setupEnv(handler, req); err != nil {
		return fmt.Errorf("failed to setup env: %v", err)
	}

	if err := pipeStderr(handler, logger); err != nil {
		return fmt.Errorf("failed to pipe: %v", err)
	}

	handlerOut, err := handler.Output()
	if err != nil {
		return fmt.Errorf("failed to execute handler: %v", err)
	}

	if !jsonHandlers {
		res.Write(handlerOut)
		return nil
	}

	var response handlerResponse
	if err := json.Unmarshal(handlerOut, &response); err != nil {
		return fmt.Errorf("failed to parse handler output: %v", err)
	}
	for k, v := range response.Headers {
		res.Header().Set(k, v)
	}
	if response.Status > 0 {
		res.WriteHeader(response.Status)
	}
	io.WriteString(res, response.Body)
	return nil
}

func main() {
	logger = newLogger("[server ] ")

	flag.StringVar(&serverBindAddr, "bind-addr", ":8080", "server bind addr")
	flag.StringVar(&location, "location", "/", "location")
	flag.BoolVar(&exposeFormValues, "form-values", false, "expose form values")
	flag.BoolVar(&exposeHeaders, "headers", false, "expose headers")
	flag.BoolVar(&jsonHandlers, "json-handlers", false, "use json handlers")
	flag.BoolVar(&accessLogs, "v", false, "show access logs")
	flag.Parse()
	if handlerArgs = flag.Args(); len(handlerArgs) < 1 {
		logger.Fatal("handler args required")
	}

	http.HandleFunc(location, func(res http.ResponseWriter, req *http.Request) {
		logger := newLogger("[handler] ")
		if accessLogs {
			logger.Printf("%s %s - %s %s", req.Method, req.URL.RequestURI(), req.RemoteAddr, req.UserAgent())
		}
		if err := handleRequest(res, req, logger); err != nil {
			logger.Printf("%s %s: %v", req.Method, req.URL.RequestURI(), err)
			http.Error(res, err.Error(), http.StatusInternalServerError)
		}
	})
	logger.Printf("serving %s ...", serverBindAddr)
	logger.Fatal(http.ListenAndServe(serverBindAddr, nil))
}
