package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/chzyer/readline"
)

type session struct {
	host    string
	vars    map[string]string
	headers map[string]string
	c       *http.Client
}

type sessionFile struct {
	Host    string            `json:"host"`
	Vars    map[string]string `json:"vars"`
	Headers map[string]string `json:"headers"`
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
	case "get", "post", "put", "delete", "patch":
		if len(parts) < 2 {
			fmt.Println("usage: <method> /path")
			return
		}
		cmd := strings.Join(parts, " ")

		var jsonBody string
		pipeIndex := strings.Index(cmd, "|")
		rawAfterPath := cmd[len(parts[0])+1+len(parts[1]):]
		if pipeIndex != -1 {
			rawAfterPath = rawAfterPath[:strings.Index(rawAfterPath, "|")]
		}
		jsonBody = strings.TrimSpace(rawAfterPath)

		_, err := exec.LookPath("jq")
		if err != nil {
			fmt.Println("jq not found in the $PATH")
			return
		}

		endpoint := interpolate(parts[1], s)
		method := strings.ToUpper(parts[0])
		output, err := makeRequest(method, endpoint, s, jsonBody)
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
	case "\\session":
		key := parts[1]
		name := parts[2]

		if key == "save" {
			err := saveSession(name, s)
			if err != nil {
				fmt.Printf("failed to save session %s\n", name)
			}
		}
		if key == "use" {
			ls, err := loadSession(name)
			*s = *ls
			if err != nil {
				fmt.Printf("faild to load session %s\n", name)
			}
		}
	case "\\sessions":
		err := printSessions()
		if err != nil {
			fmt.Println(err)
		}
	case "\\q":
		os.Exit(0)
	}
}

func makeRequest(method, endpoint string, s *session, b string) (*output, error) {
	url := s.host + endpoint

	var bodyReader io.Reader
	if b != "" {
		bodyReader = strings.NewReader(b)
	}

	req, err := http.NewRequest(method, url, bodyReader)
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

func saveSession(name string, s *session) error {
	dir := filepath.Join(os.Getenv("HOME"), ".httpql")
	os.MkdirAll(dir, 0755)

	file := filepath.Join(dir, name+".json")

	data := sessionFile{
		Host:    s.host,
		Vars:    s.vars,
		Headers: s.headers,
	}

	b, _ := json.MarshalIndent(data, "", "  ")
	return os.WriteFile(file, b, 0644)
}

func loadSession(name string) (*session, error) {
	dir := filepath.Join(os.Getenv("HOME"), ".httpql")
	file := filepath.Join(dir, name+".json")

	b, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}

	var sf sessionFile
	json.Unmarshal(b, &sf)

	return &session{
		host:    sf.Host,
		vars:    sf.Vars,
		headers: sf.Headers,
		c:       &http.Client{},
	}, nil
}

func printSessions() error {
	dir := filepath.Join(os.Getenv("HOME"), ".httpql")
	entry, err := os.ReadDir(dir)
	if err != nil {
		return err
	}

	for k, v := range entry {
		fmt.Println(k, v)
	}

	return nil
}
