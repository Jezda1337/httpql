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
	Method    string            `json:"method"`
	Body      string            `json:"body"`
	Variables map[string]string `json:"variables"`
	Cookies   []*http.Cookie    `json:"cookies"`

	httpClient *http.Client
}

type PrintOut struct {
	verbose  bool
	response *http.Response
	request  *http.Request
	body     string
}

var allowdHTTPMethods = map[string]bool{
	"GET":   true,
	"POST":  true,
	"PUT":   true,
	"PATCH": true,
}

func main() {
	args := os.Args[1:]

	jar, _ := cookiejar.New(nil)

	session := Session{
		Host:      "",
		Headers:   map[string]string{},
		Method:    "",
		Body:      "",
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
		for i := 0; i < len(args); i++ {
			switch args[i] {
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

				session.Method = m
				i++
			case "--json":
				rawData := args[i+1]
				jsonData, _ := json.Marshal(rawData)
				session.Body = string(jsonData)
				i++
			case "--fd":
				formDataRaw := args[i+1]
				formData, _ := url.ParseQuery(formDataRaw)
				session.Body = formData.Encode()

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
				session.Host = removeTrailingSlash(guessScheme(args[i]))
			}
		}

		response, err := makeRequest(&session)
		if err != nil {
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

		if strings.HasPrefix(line, "\\") {
			execute(line, &session)
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
			execute(line, &session)
		}

	}
}

func execute(input string, s *Session) {
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
			handleSession(parts[1:], s)
		case "\\cookie":
			setCookie(parts[1:], s)
		case "\\print":
			printSession(s)
		case "\\q":
			// TODO - add simple confirmation prompt if user didn't save current session
			os.Exit(0)
		}
	} else {
		method := strings.ToUpper(parts[0])
		if _, found := allowdHTTPMethods[method]; !found {
			return
		}

		s.Method = method

		path := interpolate(parts[1], s) // e.g /users

		resetHost := s.Host

		// forging full temp url
		s.Host = s.Host + path

		rawBody := strings.Join(parts[2:], " ")
		if shouldExecute {
			rawBody = strings.TrimSuffix(rawBody, ";")
		}

		s.Body = interpolate(rawBody, s)

		if isJSON(rawBody) {
			if _, found := s.Headers["Content-Type"]; !found {
				s.Headers["Content-Type"] = "application/json"
			}
		} else {
			if _, found := s.Headers["Content-Type"]; !found {
				s.Headers["Content-Type"] = "application/x-www-form-urlencoded"
			}
		}

		response, err := makeRequest(s)
		if err != nil {
			return
		}
		defer response.Body.Close()

		s.Host = resetHost

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
}

func makeRequest(s *Session) (*http.Response, error) {
	var reader io.Reader
	if s.Body != "" {
		reader = strings.NewReader(s.Body)
	}

	req, err := http.NewRequest(s.Method, s.Host, reader)
	if err != nil {
		return nil, err
	}

	for hk, hv := range s.Headers {
		req.Header.Set(hk, hv)
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

func handleSession(parts []string, session *Session) {
	key := parts[0]
	name := ""
	if len(parts) >= 2 {
		name = parts[1]
	}

	switch key {
	case "list":
		_ = listSessions()
	case "save":
		_ = saveSession(session, name)
		fmt.Println("OK")
	case "load":
		loadedSession, _ := loadSession(name)
		*session = *loadedSession
		fmt.Println("OK")
	case "delete":
		_ = deleteSession(name)
		fmt.Println("OK")
	}
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

	jar, _ := cookiejar.New(nil)
	u, _ := url.Parse(session.Host)
	jar.SetCookies(u, session.Cookies)

	return &Session{
		Host:      session.Host,
		Variables: session.Variables,
		Headers:   session.Headers,
		Cookies:   session.Cookies,
		httpClient: &http.Client{
			Jar: jar,
		},
	}, nil
}

func saveSession(s *Session, name string) error {
	dir := filepath.Join(os.Getenv("HOME"), ".httpql")
	os.MkdirAll(dir, 0755)

	file := filepath.Join(dir, name+".json")

	u, _ := url.Parse(s.Host)
	cookies := s.httpClient.Jar.Cookies(u)
	s.Cookies = cookies

	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(file, b, 0644)
}

func setCookie(parts []string, s *Session) {
	key := parts[0]
	value := strings.Join(parts[1:], " ")

	value = interpolate(value, s)

	u, _ := url.Parse(s.Host)
	c := &http.Cookie{Name: key, Value: value, Path: "/"}
	s.httpClient.Jar.SetCookies(u, []*http.Cookie{c})
	fmt.Print("OK")
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
	body, err := io.ReadAll(po.response.Body)
	if err != nil {
		return
	}

	fmt.Fprintf(os.Stderr, "> %s %s\n", po.request.Method, po.request.URL.String())
	if po.verbose {
		fmt.Fprintf(os.Stderr, "> Content-Length: %d\n", po.request.ContentLength)
		if po.request.UserAgent() != "" {
			fmt.Fprintf(os.Stderr, "> User-Agent: %s\n", po.request.UserAgent())
		} else {
			fmt.Fprintf(os.Stderr, "> User-Agent: %s\n", "httpql 0.1.0")
		}
		for k, v := range po.request.Header {
			if k == "Content-Type" {
				fmt.Fprintf(os.Stderr, "> %s: %s\n", k, v[0])
			} else {
				fmt.Fprintf(os.Stderr, "> %s: %s\n", k, v)
			}
		}

		fmt.Fprintf(os.Stderr, "< Status: %s\n", po.response.Status)
		for k, v := range po.response.Header {
			fmt.Fprintf(os.Stderr, "< %s: %s\n", k, v)
		}
	}

	fmt.Fprintf(os.Stdout, "%s\n", string(body))
}

func printSession(s *Session) {
	fmt.Println("host =", s.Host)

	fmt.Println("\nvars:")
	for k, v := range s.Variables {
		fmt.Printf("\t%s = %s\n", k, v)
	}

	fmt.Println("\nheaders:")
	for k, v := range s.Headers {
		fmt.Printf("\t%s = %s\n", k, v)
	}

	u, _ := url.Parse(s.Host)
	fmt.Println("\ncookies:")
	for _, c := range s.httpClient.Jar.Cookies(u) {
		fmt.Printf("\t%s = %s\n", c.Name, c.Value)
	}
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
