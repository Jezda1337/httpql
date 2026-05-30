package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
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
	Cookies []*http.Cookie    `json:"cookies"`
}

type output struct {
	req    *http.Request
	body   string
	method string
	url    string
	status string
}

func main() {
	jar, _ := cookiejar.New(nil)
	s := session{
		host:    "",
		vars:    make(map[string]string),
		headers: make(map[string]string),
		c: &http.Client{
			Jar: jar,
		},
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

			if err := executeCommand(cmd, &s); err != nil {
				fmt.Println(err)
				continue
			}
		}
	}
}

func executeCommand(input string, s *session) error {
	input = strings.TrimSpace(input)
	parts := strings.Fields(input)

	if len(parts) == 0 || parts[0] == "" {
		return fmt.Errorf("input is empty")
	}

	switch strings.ToLower(parts[0]) {
	case "\\set":
		if len(parts) < 3 {
			return fmt.Errorf("usage: \\set key value")
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
			return fmt.Errorf("usage: \\header key value")
		}

		key := parts[1]
		value := strings.Join(parts[2:], " ")

		value = interpolate(value, s)

		s.headers[key] = value
		fmt.Println("ok")
	case "\\cookie":
		if len(parts) < 3 {
			return fmt.Errorf("usage: \\cookie key value")
		}

		if s.host == "" {
			return fmt.Errorf("error: must set host before setting cookies")
		}

		key := parts[1]
		value := strings.Join(parts[2:], " ")

		value = interpolate(value, s)

		u, _ := url.Parse(s.host)
		c := &http.Cookie{Name: key, Value: value, Path: "/"}
		s.c.Jar.SetCookies(u, []*http.Cookie{c})

		fmt.Println("ok")
	case "get", "post", "put", "delete", "patch":
		if len(parts) < 2 {
			return fmt.Errorf("usage: <method> /path")
		}
		cmd := strings.Join(parts, " ")

		var jsonBody string
		pipeIndex := strings.Index(cmd, "|") // TODO - need better/robust parser
		rawAfterPath := cmd[len(parts[0])+1+len(parts[1]):]
		if pipeIndex != -1 {
			rawAfterPath = rawAfterPath[:strings.Index(rawAfterPath, "|")]
		}
		jsonBody = strings.TrimSpace(rawAfterPath)

		if jsonBody != "" && !strings.HasPrefix(jsonBody, "{") && !strings.HasPrefix(jsonBody, "[") && strings.Contains(jsonBody, "=") {
			form, err := url.ParseQuery(jsonBody)
			if err != nil {
				return fmt.Errorf("invalid form data: %w", err)
			}
			jsonBody = form.Encode()

			if _, ok := s.headers["Content-Type"]; !ok {
				s.headers["Content-Type"] = "application/x-www-form-urlencoded"
			}

		} else {
			if _, ok := s.headers["Content-Type"]; !ok {
				s.headers["Content-Type"] = "application/json"
			}
		}

		_, err := exec.LookPath("jq")
		if err != nil {
			return fmt.Errorf("jq not found in the $PATH")
		}

		if jsonBody != "" && s.headers["Content-Type"] == "application/json" {
			if !json.Valid([]byte(jsonBody)) {
				return fmt.Errorf("error: invalid JSON body")
			}
		}

		jsonBody = interpolate(jsonBody, s)
		endpoint := interpolate(parts[1], s)
		method := strings.ToUpper(parts[0])
		output, err := makeRequest(method, endpoint, s, jsonBody)
		if err != nil {
			return fmt.Errorf("request failed: %w", err)
		}

		if pipeIndex != -1 {
			jqCmd := cmd[pipeIndex:]
			jqArgs := strings.Fields(jqCmd)[1:]
			// TODO - maybe not the best DX
			c := exec.Command("jq", jqArgs...)
			c.Stdin = strings.NewReader(output.body)
			out := bytes.Buffer{}
			c.Stdout = &out
			if err := c.Run(); err != nil {
				fmt.Println(jqArgs)
				fmt.Println("jq command failed: ", err)
			} else {
				output.body = strings.TrimSpace(out.String())
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

		u, _ := url.Parse(s.host)
		fmt.Println("\ncookies:")
		for _, c := range s.c.Jar.Cookies(u) {
			fmt.Printf("\t%s = %s\n", c.Name, c.Value)
		}
	case "\\session":
		if len(parts) < 3 {
			return fmt.Errorf("usage: \\session <save|use> <name>")
		}

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
			if err != nil {
				return fmt.Errorf("failed to load session %s\n", name)
			}
			*s = *ls
		}
	case "\\sessions":
		err := printSessions()
		if err != nil {
			fmt.Println(err)
		}
	case "\\q":
		os.Exit(0)
	}
	return nil
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

	u, _ := url.Parse(s.host)
	cookies := s.c.Jar.Cookies(u)

	data := sessionFile{
		Host:    s.host,
		Vars:    s.vars,
		Headers: s.headers,
		Cookies: cookies,
	}

	b, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(file, b, 0644); err != nil {
		return err
	}

	return nil
}

func loadSession(name string) (*session, error) {
	dir := filepath.Join(os.Getenv("HOME"), ".httpql")
	file := filepath.Join(dir, name+".json")

	b, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}

	var sf sessionFile
	if err := json.Unmarshal(b, &sf); err != nil {
		return nil, err
	}

	jar, _ := cookiejar.New(nil)
	u, _ := url.Parse(sf.Host)
	jar.SetCookies(u, sf.Cookies)

	return &session{
		host:    sf.Host,
		vars:    sf.Vars,
		headers: sf.Headers,
		c: &http.Client{
			Jar: jar,
		},
	}, nil
}

func printSessions() error {
	dir := filepath.Join(os.Getenv("HOME"), ".httpql")
	entry, err := os.ReadDir(dir)
	if err != nil {
		return err
	}

	for _, v := range entry {
		if !v.IsDir() {
			fmt.Println(strings.TrimSuffix(v.Name(), ".json"))
		}
	}

	return nil
}
