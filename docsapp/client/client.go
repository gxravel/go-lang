package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"mime"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

const (
	loginQuery    = "login"
	passwordQuery = "password"
	tokenQuery    = "token"
	metaQuery     = "meta"
	jsonQuery     = "json"
	fileQuery     = "file"
	keyQuery      = "key"
	valueQuery    = "value"
	limitQuery    = "limit"
	idQuery       = "id"
	fpathQuery    = "fpath"
	grantQuery    = "grant"
	publicQuery   = "public"

	host             = "http://localhost:8080"
	contentTypeJSON  = "application/json; charset=utf-8"
	contentTypeURL   = "application/x-www-form-urlencoded"
	contentTypeText  = "text/plain; charset=utf-8"
	contentTypeOctet = "application/octet-stream"
	dataPath         = "data/"
	configName       = "config.json"
	maxOptionNumber  = 7
	maxOptionLength  = 6
)

const (
	optionInitial  = 1
	optionRegister = iota + optionInitial - 1
	optionAuth
	optionLoadDoc
	optionGetDocs
	optionDocByID
	optionDeleteDoc
	optionLogout
	optionFinal = iota + optionInitial - 1
)

var (
	routes = map[string]string{"register": "/register", "auth": "/auth", "docs": "/docs", "docsID": "/docs/",
		"logout": "/auth/"}
	basePath       string
	config         *configuration
	errWrongMethod = errors.New("Wrong method")
	isplit         bufio.SplitFunc
	handlerCase    = map[int]handlerFunc{
		optionRegister:  registerHandler,
		optionAuth:      authHandler,
		optionLoadDoc:   loadDocHandler,
		optionGetDocs:   getDocsHandler,
		optionDocByID:   docByIDHandler,
		optionDeleteDoc: deleteDocHandler,
		optionLogout:    logoutHandler}
	methodCase = map[int][]string{
		optionRegister:  {"POST"},
		optionAuth:      {"POST"},
		optionLoadDoc:   {"POST", "PUT"},
		optionGetDocs:   {"GET", "HEAD"},
		optionDocByID:   {"GET", "HEAD"},
		optionDeleteDoc: {"DELETE"},
		optionLogout:    {"DELETE"}}
	paramsCase = map[int]map[string]string{
		optionRegister:  {loginQuery: "", passwordQuery: "", tokenQuery: ""},
		optionAuth:      {loginQuery: "", passwordQuery: ""},
		optionLoadDoc:   {fpathQuery: "", idQuery: "", grantQuery: "", publicQuery: ""},
		optionGetDocs:   {loginQuery: "", keyQuery: "", valueQuery: "", limitQuery: ""},
		optionDocByID:   {idQuery: ""},
		optionDeleteDoc: {idQuery: ""},
		optionLogout:    {}}
	actionCase = map[int]string{
		optionRegister:  "Register",
		optionAuth:      "Authorize",
		optionLoadDoc:   "Load document",
		optionGetDocs:   "Get documents",
		optionDocByID:   "Get document by ID",
		optionDeleteDoc: "Delete the document",
		optionLogout:    "Logout"}
)

type handlerFunc func(string, map[string]string) error

type metaModel struct {
	Name   string
	File   bool
	Public bool
	Mime   string
	Grant  []string
}

type outModel struct {
	Error    *errorModel            `json:"error,omitempty"`
	Response map[string]interface{} `json:"response,omitempty"`
	Data     map[string]interface{} `json:"data,omitempty"`
}

type errorModel struct {
	Code int    `json:"code"`
	Text string `json:"text"`
}

type configuration struct {
	Token string `json:"token"`
}

func init() {
	f, err := os.OpenFile(configName, os.O_RDONLY, 0777)
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()
	fd := json.NewDecoder(f)
	config = &configuration{}
	err = fd.Decode(config)
	if err != nil {
		log.Fatal(err)
	}

	isplit = func(data []byte, atEOF bool) (advance int, token []byte, err error) {
		advance, token, err = bufio.ScanLines(data, atEOF)
		if err == nil && token != nil {
			if advance <= maxOptionLength {
				var i int
				i, err = strconv.Atoi(string(token))
				if err != nil {
					err = errors.New("I don't get it")
				}
				if i > maxOptionNumber {
					err = errors.New("doubt")
				}
			} else {
				err = errors.New("choose an option, no need to tell me about your miserable life")
			}
		}
		return
	}

	basePath, err = os.Getwd()
	if err != nil {
		log.Fatal(err)
	}
	basePath = filepath.Join(basePath, dataPath)
}

func main() {
	rw := bufio.NewReadWriter(bufio.NewReader(os.Stdin), bufio.NewWriter(os.Stdout))
	for err := menu(rw); err != nil; {
		log.Println(err.Error())
		err = menu(rw)
	}
}

func menu(rw *bufio.ReadWriter) (err error) {
	var handlerOption int
	var optionNumber int
	optionNumber = len(actionCase)
	entityMap := make(map[int]interface{}, optionNumber)
	for i, v := range actionCase {
		entityMap[i] = v
	}
	handlerOption, err = showOptionsMenu(rw, entityMap, "")
	if err != nil {
		return
	}
	var handler handlerFunc
	handler = handlerCase[handlerOption]

	optionNumber = len(methodCase[handlerOption])
	entityMap = make(map[int]interface{}, optionNumber)
	for i, v := range methodCase[handlerOption] {
		entityMap[i+1] = v
	}
	var methodOption int
	if len(entityMap) > 1 {
		methodOption, err = showOptionsMenu(rw, entityMap, "")
		if err != nil {
			return
		}
	} else {
		methodOption = 1
	}
	var method string
	method = entityMap[methodOption].(string)

	var params map[string]string
	params = paramsCase[handlerOption]
	if handlerOption != optionLogout {
		err = showParamsMenu(rw, params, "")
		if err != nil {
			return
		}
	}
	for {
		err = handler(method, params)
		if err != nil {
			return
		}
		_, err = rw.WriteString("\nrequest is awesome. repeat? [Y/N]\n")
		if err != nil {
			return
		}
		err = rw.Flush()
		if err != nil {
			return
		}
		var ans string
		s := bufio.NewScanner(rw)
		s.Split(bufio.ScanBytes)
		s.Scan()
		ans = s.Text()
		if !strings.EqualFold(ans, "Y") {
			return errors.New("OK")
		}
	}
}

func showOptionsMenu(rw *bufio.ReadWriter, entityMap map[int]interface{}, initMessage string) (option int, err error) {
	_, err = rw.WriteString(initMessage)
	if err != nil {
		return
	}
	for i := optionInitial; i < len(entityMap)+optionInitial; i++ {
		_, err = rw.WriteString(fmt.Sprintf("%v. %s\n", i, entityMap[i]))
		if err != nil {
			return
		}
	}
	_, err = rw.WriteString("\n0. Exit\n")
	if err != nil {
		return
	}
	err = rw.Flush()
	if err != nil {
		return
	}
	scanner := bufio.NewScanner(rw)
	scanner.Split(isplit)
	scanner.Scan()
	err = scanner.Err()
	if err != nil {
		return
	}
	option, _ = strconv.Atoi(scanner.Text())
	if option == 0 {
		os.Exit(0)
	} else if option > len(entityMap) {
		return 0, errors.New("you won't screw me up")
	}
	return
}

func showParamsMenu(rw *bufio.ReadWriter, params map[string]string, initMessage string) (err error) {
	scanner := bufio.NewScanner(rw)
	_, err = rw.WriteString(initMessage)
	if err != nil {
		return
	}
	for k := range params {
		_, err = rw.WriteString(fmt.Sprintf("%s = ", k))
		if err != nil {
			return
		}
		err = rw.Flush()
		if err != nil {
			return
		}
		scanner.Scan()
		err = scanner.Err()
		if err != nil {
			return
		}
		params[k] = scanner.Text()
	}
	err = rw.Flush()
	if err != nil {
		return
	}
	return
}

func updateConfig(con *configuration) (err error) {
	config = con
	var f *os.File
	f, err = os.OpenFile(configName, os.O_TRUNC|os.O_CREATE, 0777)
	if err != nil {
		return
	}
	defer f.Close()
	var configJSON []byte
	configJSON, err = json.MarshalIndent(config, "", "	")
	if err != nil {
		return
	}
	_, err = io.Copy(f, bytes.NewBuffer(configJSON))
	return
}

func generateModel(respBody io.Reader) (model *outModel, err error) {
	body := new(bytes.Buffer)
	_, err = io.Copy(body, respBody)
	if err != nil {
		return
	}
	bodyIndent := new(bytes.Buffer)
	err = json.Indent(bodyIndent, body.Bytes(), "", "    ")
	if err != nil {
		return
	}
	model = &outModel{}
	err = json.Unmarshal(body.Bytes(), model)
	if err != nil {
		return
	}
	fmt.Println("body\n", bodyIndent)
	return
}

func sendRequest(req *http.Request) (resp *http.Response, model *outModel, err error) {
	client := &http.Client{}
	resp, err = client.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()
	model, err = generateModel(resp.Body)
	return
}

func specifyContent(w *multipart.Writer, ct string, name string, filename string) (io.Writer, error) {
	h := make(textproto.MIMEHeader)
	if filename == "" {
		h.Set("Content-Disposition", fmt.Sprintf(`form-data; name="%s"`, name))
	} else {
		h.Set("Content-Disposition", fmt.Sprintf(`form-data; name="%s"; filename="%s"`, name, filename))
	}
	h.Set("Content-Type", ct)
	return w.CreatePart(h)
}

func registerHandler(method string, params map[string]string) (err error) {
	q := make(url.Values, 0)
	for k, v := range params {
		q.Add(k, v)
	}
	body := strings.NewReader(q.Encode())
	method = strings.ToUpper(method)
	var req *http.Request
	req, err = http.NewRequest(method, host+routes["register"], body)
	if err != nil {
		return
	}
	req.Header.Set("Content-type", contentTypeURL)
	switch method {
	case "POST":
		_, _, err = sendRequest(req)
	default:
		return errWrongMethod
	}
	return
}

func authHandler(method string, params map[string]string) (err error) {
	q := make(url.Values, 0)
	for k, v := range params {
		q.Add(k, v)
	}
	body := strings.NewReader(q.Encode())
	method = strings.ToUpper(method)
	req, err := http.NewRequest(method, host+routes["auth"], body)
	if err != nil {
		return
	}
	req.Header.Set("Content-type", contentTypeURL)
	switch method {
	case "POST":
		var model *outModel
		_, model, err = sendRequest(req)
		if err != nil {
			return
		}
		token, ok := model.Response[tokenQuery].(string)
		if !ok {
			return
		}
		config.Token = token
		err = updateConfig(config)
		if err != nil {
			return
		}
	default:
		return errWrongMethod
	}
	return
}

func loadDocHandler(method string, params map[string]string) (err error) {
	file, err := os.Open(filepath.Clean(params[fpathQuery]))
	if err != nil {
		return
	}
	defer file.Close()
	var fpath string
	var absPath string
	absPath, err = filepath.Abs(params[fpathQuery])
	if err != nil {
		return
	}
	fpath, err = filepath.Rel(basePath, absPath)
	if err != nil {
		fpath = absPath
	}
	body := new(bytes.Buffer)
	bodyWriter := multipart.NewWriter(body)
	wmeta, err := specifyContent(bodyWriter, contentTypeJSON, metaQuery, "")
	if err != nil {
		return
	}
	fileExt := filepath.Ext(fpath)
	meta := &metaModel{}
	meta.Name = fpath
	meta.File = true
	meta.Public, err = strconv.ParseBool(params[publicQuery])
	if err != nil {
		meta.Public = false
	}
	if params[grantQuery] != "" {
		meta.Grant = strings.Split(params[grantQuery], " ")
	}
	meta.Mime = mime.TypeByExtension(fileExt)
	if meta.Mime == "" {
		meta.Mime = contentTypeOctet
	}
	metaJSON, err := json.Marshal(&meta)
	if err != nil {
		return
	}
	_, err = wmeta.Write(metaJSON)
	if err != nil {
		return
	}
	wtoken, err := specifyContent(bodyWriter, contentTypeText, tokenQuery, "")
	if err != nil {
		return
	}
	_, err = wtoken.Write(bytes.NewBufferString(config.Token).Bytes())
	if err != nil {
		return
	}
	wfile, err := specifyContent(bodyWriter, meta.Mime, fileQuery, fpath)
	if err != nil {
		return
	}
	_, err = io.Copy(wfile, file)
	if err != nil {
		return
	}
	bodyWriter.Close()
	var req *http.Request
	method = strings.ToUpper(method)
	switch method {
	case "POST":
		req, err = http.NewRequest(method, host+routes["docs"], body)
	case "PUT":
		req, err = http.NewRequest(method, host+routes["docsID"]+params[idQuery], body)
	default:
		return errWrongMethod
	}
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", bodyWriter.FormDataContentType())
	if err != nil {
		return
	}
	_, _, err = sendRequest(req)
	return
}

func getDocsHandler(method string, params map[string]string) (err error) {
	var req *http.Request
	method = strings.ToUpper(method)
	req, err = http.NewRequest(method, host+routes["docs"], nil)
	if err != nil {
		return
	}
	req.Header.Set("Content-type", contentTypeURL)
	q := req.URL.Query()
	for k, v := range params {
		q.Add(k, v)
	}
	q.Add(tokenQuery, config.Token)
	req.URL.RawQuery = q.Encode()
	switch method {
	case "GET":
		_, _, err = sendRequest(req)
		if err != nil {
			return
		}
	case "HEAD":
		client := &http.Client{}
		var resp *http.Response
		resp, err = client.Do(req)
		if err != nil {
			return
		}
		fmt.Println("status\n", resp.Status)
		fmt.Println("header\n", resp.Header)
	default:
		return errWrongMethod
	}
	return
}

func docByIDHandler(method string, params map[string]string) (err error) {
	client := &http.Client{}
	var req *http.Request
	var resp *http.Response
	method = strings.ToUpper(method)
	req, err = http.NewRequest(method, host+routes["docsID"]+params[idQuery], nil)
	if err != nil {
		return
	}
	req.Header.Set("Content-type", contentTypeURL)
	req.URL.RawQuery = tokenQuery + "=" + config.Token
	switch method {
	case "GET":
		resp, err = client.Do(req)
		if err != nil {
			return
		}
		defer resp.Body.Close()
		reg := regexp.MustCompile(`filename=(.*)$`)
		res := reg.FindStringSubmatch(resp.Header.Get("Content-Disposition"))
		if res != nil {
			var f *os.File
			fname := filepath.Join(dataPath, res[1])
			os.MkdirAll(filepath.Dir(fname), os.ModeDir)
			f, err = os.OpenFile(fname, os.O_EXCL|os.O_CREATE, 0777)
			if err != nil {
				return errors.New(fname + "already exists")
			}
			defer f.Close()
			_, err = io.Copy(f, resp.Body)
			return
		}
		_, err = generateModel(resp.Body)
	case "HEAD":
		resp, err = client.Do(req)
		if err != nil {
			return
		}
		fmt.Println("status\n", resp.Status)
		fmt.Println("header\n", resp.Header)
	default:
		return errWrongMethod
	}
	return
}

func deleteDocHandler(method string, params map[string]string) (err error) {
	var req *http.Request
	method = strings.ToUpper(method)
	req, err = http.NewRequest(method, host+routes["docsID"]+params[idQuery], nil)
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", contentTypeURL)
	req.URL.RawQuery = tokenQuery + "=" + config.Token
	switch method {
	case "DELETE":
		_, _, err = sendRequest(req)
	default:
		return errWrongMethod
	}
	return
}

func logoutHandler(method string, params map[string]string) (err error) {
	var req *http.Request
	method = strings.ToUpper(method)
	req, err = http.NewRequest(method, host+routes["logout"]+config.Token, nil)
	if err != nil {
		return
	}
	switch method {
	case "DELETE":
		var model *outModel
		_, model, err = sendRequest(req)
		if err != nil {
			return
		}
		var cool bool
		var ok bool
		cool, ok = model.Response[config.Token].(bool)
		if !ok {
			return
		}
		if cool {
			log.Println("successively logged out")
			config.Token = ""
			updateConfig(config)
			return
		}
	default:
		return errWrongMethod
	}
	return
}
