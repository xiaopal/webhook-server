package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"sync"
)

var extractors = map[string]func(*exec.Cmd, http.ResponseWriter) error{
	handlerTypeSimple: extractSimple,
	handlerTypeJSON:   extractJSON,
	handlerTypeFd:     extractFd,
}

func extractSimple(handler *exec.Cmd, res http.ResponseWriter) error {
	output, err := handler.Output()
	if err != nil {
		return err
	}
	res.Write(output)
	return nil
}

func extractJSON(handler *exec.Cmd, res http.ResponseWriter) error {
	stdout, err := handler.Output()
	if err != nil {
		return err
	}
	output, err := writeJsonOutput(res, stdout)
	if err != nil {
		return err
	}
	io.WriteString(res, output.Body)
	return nil
}

func extractFd(handler *exec.Cmd, res http.ResponseWriter) error {
	stdout, fdout, err := runWithFd(handler)
	if err != nil {
		return err
	}
	_, err = writeJsonOutput(res, fdout)
	if err != nil {
		return err
	}
	res.Write(stdout)
	return nil
}

func runWithFd(cmd *exec.Cmd) ([]byte, []byte, error) {
	var stdout, fdout bytes.Buffer
	cmd.Stdout = &stdout
	fdr, fdw, err := os.Pipe()
	if err != nil {
		return nil, nil, err
	}
	defer func() {
		fdw.Close()
		fdr.Close()
	}()
	cmd.ExtraFiles = append(cmd.ExtraFiles, fdw)
	if err := cmd.Start(); err != nil {
		return nil, nil, err
	}
	fdw.Close()
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		io.Copy(&fdout, fdr)
		wg.Done()
	}()
	err = cmd.Wait()
	wg.Wait()
	return stdout.Bytes(), fdout.Bytes(), err
}

type jsonOutput struct {
	Status  int               `json:"status"`
	Headers map[string]string `json:"headers"`
	Body    string            `json:"body"`
}

func writeJsonOutput(res http.ResponseWriter, raw []byte) (*jsonOutput, error) {
	var output jsonOutput
	if err := json.Unmarshal(raw, &output); err != nil {
		return nil, fmt.Errorf("failed to parse handler output: %v", err)
	}
	for k, v := range output.Headers {
		res.Header().Set(k, v)
	}
	if output.Status > 0 {
		res.WriteHeader(output.Status)
	}
	return &output, nil
}
