package main

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"

	"github.com/chzyer/readline"
)

type session struct {
	host    string
	vars    map[string]string
	headers map[string]string
	c       *http.Client
}

type output struct {
	req    *http.Request
	body   string
	method string
	url    string
	status string
}

func main() {
	s := session{
		host:    "",
		vars:    make(map[string]string),
		headers: make(map[string]string),
		c:       &http.Client{},
	}

	// defaultPrompt := "httpql> "
	defaultPrompt := "\033[31m»\033[0m "

	rl, err := readline.New(defaultPrompt)
	if err != nil {
		panic(err)
	}
	defer rl.Close()

	var buffer []string

	for {
		line, err := rl.Readline()
		if err != nil {
			break
		}

		line = strings.TrimSpace(line)

		if strings.HasPrefix(line, "\\") {
			executeCommand(line, &s)
			continue
		}

		buffer = append(buffer, line)
		rl.SetPrompt(">> ")

		if strings.HasSuffix(line, ";") {
			cmd := strings.Join(buffer, " ")
			cmd = strings.TrimSuffix(cmd, ";")

			buffer = nil

			rl.SetPrompt(defaultPrompt)
			rl.SaveHistory(cmd)

			executeCommand(cmd, &s)
		}
	}
}

func executeCommand(input string, s *session) {
	input = strings.TrimSpace(input)
	parts := strings.Fields(input)

	if len(parts) == 0 || parts[0] == "" {
		return
	}

	switch strings.ToLower(parts[0]) {
	case "\\set":
		if len(parts) < 3 {
			fmt.Println("usage: \\set key value")
			return
		}

		key := parts[1]
		value := strings.Join(parts[2:], " ")

		if strings.EqualFold(key, "host") {
			s.host = strings.TrimRight(value, "/")
		} else {
			s.vars[key] = value
		}
		fmt.Println("ok")
	case "\\header":
		if len(parts) < 3 {
			fmt.Println("usage: \\header key value")
			return
		}

		key := parts[1]
		value := strings.Join(parts[2:], " ")

		value = interpolate(value, s)

		s.headers[key] = value
		fmt.Println("ok")
	case "get", "post", "put", "delete":
		if len(parts) < 2 {
			fmt.Println("usage: <method> /path")
			return
		}
		cmd := strings.Join(parts, " ")
		pipeIndex := strings.Index(cmd, "|")

		_, err := exec.LookPath("jq")
		if err != nil {
			fmt.Println("jq not found in the $PATH")
			return
		}

		endpoint := interpolate(parts[1], s)
		method := strings.ToUpper(parts[0])
		output, err := makeRequest(method, endpoint, s)
		if err != nil {
			fmt.Println("request failed: ", err)
		}

		if pipeIndex != -1 {
			jqCmd := cmd[pipeIndex:]
			cmd = fmt.Sprintf("echo '%s' %s ", output.body, jqCmd)
			c := exec.Command("bash", "-c", cmd)
			out := bytes.Buffer{}
			c.Stdout = &out
			if err := c.Run(); err != nil {
				fmt.Println("jq command failed: ", err)
			}
			output.body = strings.TrimSpace(out.String())
		}

		printOutput(*output)

	case "\\env":
		fmt.Println("host =", s.host)

		fmt.Println("\nvars:")
		for k, v := range s.vars {
			fmt.Printf("\t%s = %s\n", k, v)
		}

		fmt.Println("\nheaders:")
		for k, v := range s.headers {
			fmt.Printf("\t%s = %s\n", k, v)
		}
	case "\\q":
		os.Exit(0)
	}
}

func makeRequest(method, endpoint string, s *session) (*output, error) {
	url := s.host + endpoint

	req, err := http.NewRequest(method, url, nil)
	if err != nil {
		return nil, err
	}

	for k, v := range s.headers {
		req.Header.Set(k, v)
	}

	res, err := s.c.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	body, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, err
	}
	o := &output{
		req:    req,
		body:   string(body),
		method: method,
		url:    url,
		status: res.Status,
	}

	return o, nil
}

func interpolate(input string, s *session) string {
	for k, v := range s.vars {
		input = strings.ReplaceAll(
			input,
			"{{"+k+"}}",
			v,
		)
	}

	return input
}

func printOutput(o output) {
	fmt.Println("---------")
	fmt.Println(o.method, o.url)
	fmt.Println("Status: ", o.status)
	fmt.Println("---------")
	fmt.Println(o.body)
}
