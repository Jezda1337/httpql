package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/chzyer/readline"
)

type Session struct {
	Host      string            `json:"host"`
	Headers   map[string]string `json:"headers"`
	Variables map[string]string `json:"variables"`
	Cookies   []*http.Cookie    `json:"cookies"`

	httpClient  *http.Client
	LastCommnad string
}

type PrintOut struct {
	verbose  bool
	response *http.Response
	request  *http.Request
	body     string
}

var allowdHTTPMethods = map[string]bool{
	"GET":    true,
	"POST":   true,
	"PUT":    true,
	"PATCH":  true,
	"DELETE": true,
}

func main() {
	args := os.Args[1:]

	jar, err := cookiejar.New(nil)
	if err != nil {
		fmt.Printf("failed to initialize cookijar >> %v\n", err)
		return
	}

	session := Session{
		Host:      "",
		Headers:   map[string]string{},
		Variables: map[string]string{},
		httpClient: &http.Client{
			Jar: jar,
			// TODO - redirect needs to show in print as well if in case there is a redirect
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}

	if len(args) != 0 {
		isVerbose := false
		var method, URL, body string
		for i := 0; i < len(args); i++ {
			switch args[i] {
			case "ls":
				if err := listSessions(); err != nil {
					fmt.Printf("failed to list sessions >> %v\n", err)
				}
				return
			case "-v", "--verbose":
				isVerbose = true
			case "-H", "--header":
				parts := strings.Split(args[i+1], ":")
				session.Headers[parts[0]] = parts[1]
				i++
			case "-M", "--method":
				m := args[i+1]
				m = strings.ToUpper(m)

				if _, found := allowdHTTPMethods[m]; !found {
					fmt.Printf("%s", "method is not valid, use HTTP methods")
					return
				}

				method = m
				i++
			case "--json":
				rawData := args[i+1]
				jsonData, err := json.Marshal(json.RawMessage(rawData))
				if err != nil {
					fmt.Printf("failed to marshal json >>%v\n", err)
					return
				}
				body = string(jsonData)
				i++
			case "--fd":
				formDataRaw := args[i+1]
				formData, err := url.ParseQuery(formDataRaw)
				if err != nil {
					fmt.Printf("failed to parse query >> %v\n", err)
					return
				}
				body = formData.Encode()

				session.Headers["Content-Type"] = "application/x-www-form-urlencoded"
				i++
			// case "-C", "--cookie": // TODO
			// 	cookieRow := args[i+1]
			// 	cookies, _ := url.ParseQuery(cookieRow)
			// 	for k, v := range cookies {
			// 		c := &http.Cookie{
			// 			Name:  k,
			// 			Value: v[0],
			// 		}
			// 		session.Cookies = append(session.Cookies, c)
			// 	}
			// 	i++
			default:
				URL = removeTrailingSlash(guessScheme(args[i]))
			}
		}

		response, err := makeRequest(&session, method, URL, body)
		if err != nil {
			fmt.Printf("ERROR: %s", err.Error())
			return
		}
		defer response.Body.Close()

		printOut(PrintOut{
			verbose:  isVerbose,
			response: response,
			request:  response.Request,
		})

		return
	}

	rl, err := readline.New("httpql> ")
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

		if line != "" && line != "\\g" {
			session.LastCommnad = line
		}

		if strings.HasPrefix(line, "\\") {
			if err := execute(line, &session); err != nil {
				fmt.Println(err)
			}
			continue
		}

		buffer = append(buffer, line)
		rl.SetPrompt(">> ")
		if strings.HasSuffix(line, ";") {
			line = strings.Join(buffer, " ")
			line = strings.TrimSuffix(line, ";")
			rl.SaveHistory(line)
			buffer = nil
			rl.SetPrompt("httpql> ")
			if err := execute(line, &session); err != nil {
				fmt.Println(err)
			}
		}

	}
}

func execute(input string, s *Session) error {
	isCommand := strings.HasPrefix(input, "\\")
	isVerbose := false
	shouldExecute := strings.HasSuffix(input, ";")

	input = strings.TrimSpace(input)
	parts := strings.Split(input, " ")
	if isCommand {
		command := parts[0]
		switch command {
		case "\\set":
			setCommand(parts[1:], s)
		case "\\session":
			return handleSession(parts[1:], s)
		case "\\cookie":
			return setCookie(parts[1:], s)
		case "\\print":
			return printSession(s)
		case "\\g":
			return execute(s.LastCommnad, s)
		case "\\q":
			// TODO - add simple confirmation prompt if user didn't save current session
			os.Exit(0)
		}
	} else {
		method := strings.ToUpper(parts[0])
		if _, found := allowdHTTPMethods[method]; !found {
			// TODO: return method is not supported error message
			return fmt.Errorf("method is not suppored: [%s]", method)
		}

		path := interpolate(parts[1], s) // e.g /users

		url := s.Host + path

		rawBody := strings.Join(parts[2:], " ")
		if shouldExecute {
			rawBody = strings.TrimSuffix(rawBody, ";")
		}

		body := interpolate(rawBody, s)

		response, err := makeRequest(s, method, url, body)
		if err != nil {
			return fmt.Errorf("request have failed: %w", err)
		}
		defer response.Body.Close()

		for k, v := range s.Variables {
			if k == "verbose" && (v == "on" || v == "true") {
				isVerbose = true
			}
		}

		printOut(PrintOut{
			verbose:  isVerbose,
			response: response,
			request:  response.Request,
		})

	}
	return nil
}

func makeRequest(s *Session, method, url, body string) (*http.Response, error) {
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}

	req, err := http.NewRequest(method, url, reader)
	if err != nil {
		return nil, err
	}

	for hk, hv := range s.Headers {
		req.Header.Set(hk, hv)
	}

	if req.Header.Get("Content-Type") == "" {
		if isJSON(body) {
			req.Header.Set("Content-Type", "application/json")
		} else {
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		}
	}

	response, err := s.httpClient.Do(req)
	if err != nil {
		return nil, err
	}

	return response, nil
}

func setCommand(parts []string, session *Session) {
	key := parts[0]
	value := parts[1:]

	switch key {
	case "host":
		session.Host = removeTrailingSlash(guessScheme(value[0]))
		fmt.Print("OK")
	case "header":
		if len(value) < 2 {
			return
		}

		hk := value[0]
		hv := value[1]
		session.Headers[hk] = hv
		fmt.Print("OK")
	default:
		// default is for variables, \set x 10
		session.Variables[key] = strings.Join(value, " ")
		fmt.Print("OK")
	}
}

func handleSession(parts []string, session *Session) error {
	key := parts[0]
	name := ""
	if len(parts) >= 2 {
		name = parts[1]
	}

	switch key {
	case "list":
		if err := listSessions(); err != nil {
			return fmt.Errorf("failed to list sessions >> %w", err)
		}
	case "save":
		if err := saveSession(session, name); err != nil {
			return fmt.Errorf("failed to save session: [%s] >> %w", name, err)
		}
		fmt.Println("OK")
	case "load":
		loadedSession, err := loadSession(name)
		if err != nil {
			return fmt.Errorf("faild to load session: [%s] >> %w", name, err)
		}
		*session = *loadedSession
		fmt.Println("OK")
	case "delete":
		if err := deleteSession(name); err != nil {
			return fmt.Errorf("failed to delete session: [%s] >> %w", name, err)
		}
		fmt.Println("OK")
	}

	return nil
}

func listSessions() error {
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

func deleteSession(name string) error {
	dir := filepath.Join(os.Getenv("HOME"), ".httpql")
	file := filepath.Join(dir, name+".json")

	return os.Remove(file)
}

func loadSession(name string) (*Session, error) {
	dir := filepath.Join(os.Getenv("HOME"), ".httpql")
	file := filepath.Join(dir, name+".json")

	b, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}

	var session Session
	if err := json.Unmarshal(b, &session); err != nil {
		return nil, err
	}

	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize cookiejar >>%w", err)
	}
	u, err := url.Parse(session.Host)
	if err != nil {
		return nil, fmt.Errorf("failed to parse host >>%w", err)
	}
	jar.SetCookies(u, session.Cookies)

	return &Session{
		Host:      session.Host,
		Variables: session.Variables,
		Headers:   session.Headers,
		Cookies:   session.Cookies,
		httpClient: &http.Client{
			Jar: jar,
		},
		LastCommnad: session.LastCommnad,
	}, nil
}

func saveSession(s *Session, name string) error {
	dir := filepath.Join(os.Getenv("HOME"), ".httpql")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	file := filepath.Join(dir, name+".json")

	u, err := url.Parse(s.Host)
	if err != nil {
		return err
	}
	cookies := s.httpClient.Jar.Cookies(u)
	s.Cookies = cookies

	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(file, b, 0644)
}

func setCookie(parts []string, s *Session) error {
	key := parts[0]
	value := strings.Join(parts[1:], " ")

	value = interpolate(value, s)

	u, err := url.Parse(s.Host)
	if err != nil {
		return fmt.Errorf("failed to parse host >>%w", err)
	}
	c := &http.Cookie{Name: key, Value: value, Path: "/"}
	s.httpClient.Jar.SetCookies(u, []*http.Cookie{c})
	fmt.Print("OK")

	return nil
}

func guessScheme(host string) string {
	if strings.HasPrefix(host, "http") || strings.HasPrefix(host, "https") {
		return host
	}

	host = removeTrailingSlash(host)

	if strings.Contains(host, "localhost:") {
		return "http://" + host
	} else {
		return "https://" + host
	}
}

func removeTrailingSlash(host string) string {
	if before, ok := strings.CutSuffix(host, "/"); ok {
		return before
	}
	return host
}

func printOut(po PrintOut) {
	// > = request data
	// < = response data
	RESET := "\x1b[0m"
	KEY_COLOR := "\x1b[38;2;150;166;200m"
	URL_COLOR := "\x1b[4;38;2;130;170;255m"

	body, err := io.ReadAll(po.response.Body)
	if err != nil {
		return
	}

	fmt.Fprintf(os.Stderr, "> %s%s%s %s%s%s\n", KEY_COLOR, po.request.Method, RESET, URL_COLOR, po.request.URL.String(), RESET)
	if !po.verbose {
		fmt.Fprintf(os.Stderr, "< %sStatus:%s %s%s%s\n", KEY_COLOR, RESET, statusColor(po.response.StatusCode), po.response.Status, RESET)
	}
	if po.verbose {
		fmt.Fprintf(os.Stderr, "> %sContent-Length:%s %d\n", KEY_COLOR, RESET, po.request.ContentLength)
		if po.request.UserAgent() != "" {
			fmt.Fprintf(os.Stderr, "> %sUser-Agent: %s%s\n", KEY_COLOR, RESET, po.request.UserAgent())
		} else {
			fmt.Fprintf(os.Stderr, "> %sUser-Agent:%s %s\n", KEY_COLOR, RESET, "httpql 0.1.0")
		}
		for k, v := range po.request.Header {
			if k == "Content-Type" {
				fmt.Fprintf(os.Stderr, "> %s%s:%s %s\n", KEY_COLOR, k, RESET, v[0])
			} else {
				fmt.Fprintf(os.Stderr, "> %s%s:%s %s\n", KEY_COLOR, k, RESET, v)
			}
		}

		fmt.Fprintf(os.Stderr, "< %sStatus:%s %s%s%s\n", KEY_COLOR, RESET, statusColor(po.response.StatusCode), po.response.Status, RESET)
		for k, v := range po.response.Header {
			fmt.Fprintf(os.Stderr, "< %s%s:%s %s\n", KEY_COLOR, k, RESET, v)
		}
	}

	fmt.Fprintf(os.Stdout, "%s\n", string(body))
}

func statusColor(code int) string {
	switch {
	case code >= 500, code >= 400:
		return "\x1b[31m" // red
	case code >= 300:
		return "\x1b[33m" // yellow
	default:
		return "\x1b[32m" // green
	}
}

func printSession(s *Session) error {
	fmt.Println("host =", s.Host)

	fmt.Println("\nvars:")
	for k, v := range s.Variables {
		fmt.Printf("\t%s = %s\n", k, v)
	}

	fmt.Println("\nheaders:")
	for k, v := range s.Headers {
		fmt.Printf("\t%s = %s\n", k, v)
	}

	u, err := url.Parse(s.Host)
	if err != nil {
		return fmt.Errorf("failed to parse host >>%w", err)
	}
	fmt.Println("\ncookies:")
	for _, c := range s.httpClient.Jar.Cookies(u) {
		fmt.Printf("\t%s = %s\n", c.Name, c.Value)
	}
	return nil
}

func interpolate(input string, s *Session) string {
	for k, v := range s.Variables {
		input = strings.ReplaceAll(
			input,
			"{{"+k+"}}",
			v,
		)
	}

	return input
}

func isJSON(rawBody string) bool {
	if strings.Contains(rawBody, "{") || strings.Contains(rawBody, "[") {
		return true
	}
	return false
}
