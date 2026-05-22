package main

import (
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/chzyer/readline"
)

type session struct {
	host    string
	vars    map[string]string
	headers map[string]string
	c       *http.Client
}

func main() {
	s := session{
		host:    "",
		vars:    make(map[string]string),
		headers: make(map[string]string),
		c:       &http.Client{},
	}

	defaultPrompt := "httpql> "

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
			handle(line, &s)
			continue
		}

		buffer = append(buffer, line)
		rl.SetPrompt(">> ")

		if strings.HasSuffix(line, ";") {
			cmd := strings.Join(buffer, " ")

			buffer = nil

			rl.SetPrompt(defaultPrompt)
			rl.SaveHistory(cmd)

			handle(cmd, &s)
		}
	}
}

func handle(input string, s *session) {
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

		endpoint := interpolate(parts[1], s)
		method := strings.ToUpper(parts[0])
		err := makeRequest(method, endpoint, s)
		if err != nil {
			fmt.Println("request failed: ", err)
		}
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
	}
}

func makeRequest(method, endpoint string, s *session) error {
	url := s.host + endpoint

	req, err := http.NewRequest(method, url, nil)
	if err != nil {
		return err
	}

	for k, v := range s.headers {
		req.Header.Set(k, v)
	}

	res, err := s.c.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()

	body, err := io.ReadAll(res.Body)
	if err != nil {
		return err
	}
	fmt.Println("---------")
	fmt.Println(method, url)
	fmt.Println("Status: ", res.Status)
	fmt.Println("---------")
	fmt.Println(string(body))
	return nil
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
