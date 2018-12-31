package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"github.com/xiaopal/kube-informer/pkg/subreaper"
)

func setupHandler(handler *exec.Cmd, req *http.Request, logger *log.Logger) error {
	env := append(os.Environ(),
		fmt.Sprintf("HTTP_REQUEST_HOST=%s", req.Host),
		fmt.Sprintf("HTTP_REQUEST_METHOD=%s", req.Method),
		fmt.Sprintf("HTTP_REQUEST_URI=%s", req.URL.RequestURI()),
		fmt.Sprintf("HTTP_REQUEST_PATH=%s", req.URL.Path),
		fmt.Sprintf("HTTP_REQUEST_QUERY=%s", req.URL.RawQuery),
	)
	if remoteIP, remotePort, err := net.SplitHostPort(req.RemoteAddr); err == nil {
		env = append(env, "HTTP_REMOTE_ADDR="+remoteIP, "HTTP_REMOTE_PORT="+remotePort)
	} else {
		env = append(env, "HTTP_REMOTE_ADDR="+req.RemoteAddr)
	}

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

	if requestData {
		handler.Stdin = req.Body
	}

	if err := pipeStderr(handler, logger); err != nil {
		return fmt.Errorf("failed to pipe: %v", err)
	}

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

func handleRequest(res http.ResponseWriter, req *http.Request, limitSem chan bool, logger *log.Logger) error {
	ctx, timeoutChan := req.Context(), make(<-chan time.Time, 0)
	if handlerWaitTimeout > 0 {
		timer := time.NewTimer(time.Duration(handlerWaitTimeout) * time.Second)
		timeoutChan = timer.C
		defer timer.Stop()
	}
	select {
	case limitSem <- true:
		defer func() { <-limitSem }()
	case <-timeoutChan:
		return fmt.Errorf("request wait timeout")
	case <-ctx.Done():
		return fmt.Errorf("request canceled")
	}
	if handlerTimeout > 0 {
		ctx, _ = context.WithTimeout(ctx, time.Duration(handlerTimeout)*time.Second)
	}

	handler := exec.CommandContext(ctx, handlerArgs[0], handlerArgs[1:]...)
	if err := setupHandler(handler, req, logger); err != nil {
		return fmt.Errorf("failed to setup handler: %v", err)
	}

	subreaper.Pause()
	defer subreaper.Resume()

	if err := handlerExtractor(handler, res); err != nil {
		return fmt.Errorf("failed to execute handler: %v", err)
	}
	return nil
}
