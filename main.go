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

	rl, err := readline.New("hreq> ")
	if err != nil {
		panic(err)
	}
	defer rl.Close()

	for {
		line, err := rl.Readline()
		if err != nil {
			break
		}
		handler(line, &s)
	}
}

func handler(input string, s *session) {
	input = strings.TrimSpace(input)
	parts := strings.SplitN(input, " ", 3)

	if len(parts) < 2 {
		fmt.Println("invalid command")
		return
	}

	switch strings.ToLower(parts[0]) {
	case "\\set":
		if len(parts) != 3 {
			fmt.Println("usage: \\set key value")
			return
		}
		if strings.EqualFold(parts[1], "host") {
			if before, ok := strings.CutSuffix(parts[2], "/"); ok {
				s.host = before
			}
		} else {
			s.vars[parts[1]] = parts[2]
		}
	case "get", "post", "put", "delete":
		endpoint := parts[1]
		method := strings.ToUpper(parts[0])
		err := makeRequest(method, endpoint, s)
		if err != nil {
			fmt.Println("request failed: ", err)
		}
	}
}

func makeRequest(method, endpoint string, s *session) error {
	url := s.host + endpoint

	req, err := http.NewRequest(method, url, nil)
	if err != nil {
		return err
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

	fmt.Println(string(body))
	return nil
}
