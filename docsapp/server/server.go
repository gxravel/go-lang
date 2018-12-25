package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/pkg/errors"

	"github.com/rav1L/docsapp/server/modules/docsdb"
	"github.com/satori/go.uuid"

	_ "github.com/mattn/go-sqlite3"
)

const (
	statusOk                  = 200
	statusInvalidParameters   = 400
	statusNotAuthorized       = 401
	statusAccessDenied        = 403
	statusInvalidMethod       = 405
	statusNotExpected         = 500
	statusUnimplementedMethod = 501

	loginQuery    = "login"
	passwordQuery = "password"
	tokenQuery    = "token"
	metaQuery     = "meta"
	jsonQuery     = "json"
	fileQuery     = "file"
	keyQuery      = "key"
	valueQuery    = "value"
	limitQuery    = "limit"

	timeFormat         = "2006-01-02 15:04:05"
	dbPath             = `database\sqliteDocs.db`
	dataPath           = "data"
	host               = "localhost:8080"
	serverLogs         = "server.log"
	contentTypeJSON    = "application/json; charset=utf-8"
	configName         = "config.json"
	maxMB              = 32 << 20
	filterLimitDefault = 3
	fileNameLength     = 8
	idNameLength       = 6
)

var (
	errNoRows    = sql.ErrNoRows
	errCustomNil = errors.New("it will be ignored in the end but not before")
	clientError  *errorModel
	statusText   = map[int]string{
		statusInvalidParameters:   "Invalid parameters",
		statusNotAuthorized:       "Not authorized",
		statusAccessDenied:        "Access denied",
		statusInvalidMethod:       "Invalid request method",
		statusNotExpected:         "Not expected trouble",
		statusUnimplementedMethod: "The request method is not implemented",
		statusOk:                  ""}
	db                   *sql.DB
	myDB                 docsdb.ISQL
	routes               = map[string]string{"index": "/", "docs": "/docs", "docsID": "/docs/", "register": "/register", "auth": "/auth", "logout": "/auth/"}
	config               *configuration
	possibleFilterColumn = []string{"id", "name", "mime", "file", "public", "created", "json"}
)

type configuration struct {
	AdminToken string `json:"token"`
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

func init() {
	myDB = &docsdb.Handler{}
	err := myDB.Init("sqlite3", dbPath)
	if err != nil {
		log.Fatal(err)
	}
	file, err := os.Open(configName)
	if err != nil {
		log.Fatal(err)
	}
	config = &configuration{}
	err = json.NewDecoder(file).Decode(config)
	if err != nil {
		log.Fatal(err)
	}
	clientError = &errorModel{Code: 0}
}

func main() {
	http.HandleFunc(routes["register"], makeHandler(registerHandler))
	http.HandleFunc(routes["auth"], makeHandler(authHandler))
	http.HandleFunc(routes["docs"], makeHandler(docsHandler))
	http.HandleFunc(routes["docsID"], makeHandler(docsIDHandler))
	http.HandleFunc(routes["logout"], makeHandler(logoutHandler))
	defer myDB.Disconnect()
	err := http.ListenAndServe(host, nil)
	log.Panic(err)
}

// errCustomNil is used for letting someHandler to know that an error was occured
// but it is not to be logged to the server

func makeHandler(handler func(http.ResponseWriter, *http.Request) error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		err := handler(w, r)
		if err != nil && err != errCustomNil {
			log.Printf("%+v", err)
		}
		if clientError.Code != 0 {
			if r.Method == "HEAD" {
				w.Header().Set("Content-Type", contentTypeJSON)
				w.WriteHeader(clientError.Code)
			} else {
				responseError(w)
			}
		}
		clientError.Code = 0
		clientError.Text = ""
	}
}

/* #region Auxiliary functions *********************************************************************************** */
func errorHandler(code int, text string, err *error) {
	var ok bool
	clientError.Text, ok = statusText[code]
	if !ok {
		errorHandler(statusNotExpected, "", err)
		return
	}
	clientError.Code = code
	if text != "" {
		clientError.Text += ": " + text
	}
	if code == statusNotExpected {
		*err = errors.WithStack(*err)
	} else {
		*err = errCustomNil
	}
}

func responseError(w http.ResponseWriter) {
	model := &outModel{}
	model.Error = clientError
	err := sendJSON(w, model)
	if err != nil {
		http.Error(w, clientError.Text, clientError.Code)
	}
}

func sendJSON(w http.ResponseWriter, model *outModel) (err error) {
	modelJSON, err := json.Marshal(model)
	if err != nil {
		errorHandler(statusNotExpected, "", &err)
		return
	}
	w.Header().Set("Content-Type", contentTypeJSON)
	_, err = w.Write(modelJSON)
	if err != nil {
		errorHandler(statusNotExpected, "", &err)
		return
	}
	return
}

func validateUserCredentials(r *http.Request, user *docsdb.User) (err error) {
	reg := regexp.MustCompile(`^[\w]{8,}$`)
	if !reg.MatchString(user.Login) {
		errorHandler(statusInvalidParameters, "Invalid login: minimum length: 8, only latin and digits", &err)
		return
	}
	reg = regexp.MustCompile(`^[\S]{8,}$`)
	if !reg.MatchString(user.Password) {
		errorHandler(statusInvalidParameters, "Invalid password: minimum length: 8, no spaces, minimum 1 digit and 1 letter", &err)
		return
	}
	isLetterPresent, _ := regexp.MatchString(`(?i)[A-ZА-ЯЁ]`, user.Password)
	isDigitPresent, _ := regexp.MatchString(`[\d]`, user.Password)
	if !isLetterPresent || !isDigitPresent {
		errorHandler(statusInvalidParameters, "Invalid password: minimum length: 8, no spaces, minimum 1 digit and 1 letter", &err)
		return
	}
	return
}

func doesPasswordMatch(password1 string, password2 string) bool {
	return password1 == password2
}

func getLogin(token string) (login string, err error) {
	if token == "" {
		errorHandler(statusNotAuthorized, "", &err)
		return
	}
	login, err = myDB.GetLogin(token)
	if err != nil && err != errNoRows {
		errorHandler(statusNotExpected, "", &err)
		return
	}
	if login == "" {
		errorHandler(statusNotAuthorized, "", &err)
	}
	return
}

func readMultipartFile(r *http.Request, fpath string) (filename string, err error) {
	var file multipart.File
	var handler *multipart.FileHeader
	file, handler, err = r.FormFile(fileQuery)
	if err != nil {
		errorHandler(statusNotExpected, "", &err)
		return
	}
	defer file.Close()
	name, err := uuid.FromString(handler.Filename)
	if err != nil {
		name = uuid.NewV3(uuid.NamespaceOID, handler.Filename)
	}
	path := filepath.Join(fpath, name.String()) + filepath.Ext(handler.Filename)
	os.MkdirAll(filepath.Dir(path), os.ModeDir)
	var f *os.File
	f, err = os.Create(path)
	if err != nil {
		errorHandler(statusNotExpected, "", &err)
		return
	}
	defer f.Close()
	_, err = io.Copy(f, file)
	if err != nil {
		errorHandler(statusNotExpected, "", &err)
		return
	}
	filename = filepath.Clean(strings.TrimLeft(path, dataPath))
	return
}

func readMulitpart(r *http.Request) (metaModel *docsdb.Doc, modelJSON []byte, err error) {
	err = r.ParseMultipartForm(maxMB)
	if err != nil {
		errorHandler(statusInvalidParameters, "Memory limit size was overloaded", &err)
		return
	}
	meta := r.Form.Get(metaQuery)
	token := r.Form.Get(tokenQuery)
	JSON := r.Form.Get(jsonQuery)
	var login string
	login, err = getLogin(token)
	if err != nil {
		return
	}
	metaModel = &docsdb.Doc{Created: time.Now().Format(timeFormat)}
	err = json.Unmarshal([]byte(meta), metaModel)
	if err != nil {
		errorHandler(statusNotExpected, "", &err)
		return
	}
	model := &outModel{}
	model.Data = make(map[string]interface{}, 2)
	if JSON != "" {
		model.Data[jsonQuery] = JSON
	}
	if metaModel.File {
		var name string
		name, err = readMultipartFile(r, filepath.Join(dataPath, login))
		if err != nil {
			return
		}
		metaModel.Name = name
		model.Data[fileQuery] = name
	}
	var selfGranted bool
	for _, v := range metaModel.Grant {
		if v == login {
			selfGranted = true
		}
	}
	if !selfGranted {
		metaModel.Grant = append(metaModel.Grant, login)
	}
	modelJSON, err = json.Marshal(model)
	if err != nil {
		errorHandler(statusNotExpected, "", &err)
		return
	}
	return
}

/* #endregion *************************************************************************************************** */

func registerHandler(w http.ResponseWriter, r *http.Request) (err error) {
	switch r.Method {
	case "POST":
		err = r.ParseForm()
		if err != nil {
			errorHandler(statusInvalidParameters, "", &err)
			return
		}
		login := r.PostForm.Get(loginQuery)
		password := r.PostForm.Get(passwordQuery)
		user := &docsdb.User{Login: login, Password: password}
		err = validateUserCredentials(r, user)
		if err != nil {
			return
		}
		token := r.PostForm.Get(tokenQuery)
		if token != config.AdminToken {
			user.AdminRights = false
		} else {
			user.AdminRights = true
		}
		err = myDB.AddUser(user)
		if err != nil {
			if strings.Contains(err.Error(), "UNIQUE") {
				errorHandler(statusInvalidParameters, "user "+user.Login+" already exists", &err)
				return
			}
			errorHandler(statusNotExpected, "", &err)
			return
		}
		model := &outModel{}
		if user.AdminRights {
			model.Response = map[string]interface{}{loginQuery: user.Login, "message": "here's my man!"}
		} else {
			model.Response = map[string]interface{}{loginQuery: user.Login}
		}
		err = sendJSON(w, model)
		if err != nil {
			return
		}
	case "GET", "HEAD", "PUT", "PATCH", "DELETE", "OPTIONS", "TRACE", "CONNECT":
		errorHandler(statusUnimplementedMethod, "", &err)
	default:
		errorHandler(statusInvalidMethod, "", &err)
	}
	return
}

func authHandler(w http.ResponseWriter, r *http.Request) (err error) {
	switch r.Method {
	case "POST":
		err = r.ParseForm()
		if err != nil {
			errorHandler(statusInvalidParameters, "", &err)
			return
		}
		login := r.PostForm.Get(loginQuery)
		password := r.PostForm.Get(passwordQuery)
		user := &docsdb.User{Login: login, Password: password}
		err = validateUserCredentials(r, user)
		if err != nil {
			return
		}
		password, err = myDB.GetPassword(user.Login)
		if err != nil && err != errNoRows {
			errorHandler(statusNotExpected, "", &err)
			return
		}
		if password == "" {
			errorHandler(statusNotAuthorized, "Invalid login", &err)
			return
		}
		if !doesPasswordMatch(user.Password, password) {
			errorHandler(statusNotAuthorized, "Wrong password", &err)
			return
		}
		var v4 uuid.UUID
		v4, err = uuid.NewV4()
		if err != nil {
			errorHandler(statusNotExpected, "", &err)
			return
		}
		user.Token = v4.String()
		err = myDB.UpdateToken(user.Login, user.Token)
		if err != nil {
			errorHandler(statusNotExpected, "", &err)
			return
		}
		model := &outModel{}
		model.Response = map[string]interface{}{tokenQuery: user.Token}
		err = sendJSON(w, model)
		if err != nil {
			return
		}
	case "GET", "HEAD", "PUT", "PATCH", "DELETE", "OPTIONS", "TRACE", "CONNECT":
		errorHandler(statusUnimplementedMethod, "", &err)
	default:
		errorHandler(statusInvalidMethod, "", &err)
	}
	return
}

func docsHandler(w http.ResponseWriter, r *http.Request) (err error) {
	switch r.Method {
	case "GET", "HEAD":
		err = r.ParseForm()
		if err != nil {
			errorHandler(statusInvalidParameters, "", &err)
			return
		}
		token := r.Form.Get(tokenQuery)
		var login string
		login, err = getLogin(token)
		if err != nil {
			return
		}
		filter := &docsdb.Filter{
			Login:  r.FormValue(loginQuery),
			Column: r.FormValue(keyQuery),
			Value:  r.FormValue(valueQuery)}
		limit := r.FormValue(limitQuery)
		if filter.Column != "" {
			var isColumnGood bool
			for _, v := range possibleFilterColumn {
				if strings.EqualFold(filter.Column, v) {
					isColumnGood = true
				}
			}
			if !isColumnGood {
				errorHandler(statusInvalidParameters, "possible variants of column: "+strings.Join(possibleFilterColumn, ", "), &err)
				return
			}
		}
		filter.Limit, _ = strconv.Atoi(limit)
		if filter.Limit == 0 {
			filter.Limit = filterLimitDefault
		}
		if filter.Login == "" {
			filter.Login = login
		} else if filter.Login != login {
			var admin bool
			admin, err = myDB.IsAdmin(login)
			if err != nil {
				errorHandler(statusInvalidParameters, "", &err)
				return
			}
			if !admin {
				errorHandler(statusAccessDenied, "YOU SHALL NOT PASS", &err)
				return
			}
		}
		var docs []*docsdb.Doc
		docs, err = myDB.GetDocumentsList(filter)
		if err != nil && err != errNoRows {
			errorHandler(statusNotExpected, "", &err)
			return
		}
		if docs == nil {
			errorHandler(statusOk, "there are no enquiring documents in our database", &err)
			return
		}
		s := make([]*docsdb.Doc, 0)
		for _, v := range docs {
			s = append(s, v)
		}
		model := &outModel{}
		model.Data = map[string]interface{}{"docs": s}
		var modelJSON []byte
		modelJSON, err = json.Marshal(model)
		if err != nil {
			errorHandler(statusNotExpected, "", &err)
			return
		}
		w.Header().Set("Content-Type", contentTypeJSON)
		if r.Method == "GET" {
			_, err = w.Write(modelJSON)
			if err != nil {
				errorHandler(statusNotExpected, "", &err)
			}
		} else {
			w.Header().Set("Content-Length", fmt.Sprint(len(modelJSON)))
			errorHandler(statusOk, "", &err)
		}
	case "POST":
		var meta *docsdb.Doc
		var modelJSON []byte
		r.Body = http.MaxBytesReader(w, r.Body, maxMB)
		meta, modelJSON, err = readMulitpart(r)
		if err != nil {
			return
		}
		var v3 uuid.UUID
		v3 = uuid.NewV3(uuid.NamespaceURL, meta.Name)
		meta.ID = v3.String()
		if len(meta.ID) > idNameLength {
			meta.ID = meta.ID[:idNameLength]
		}
		err = myDB.CreateDocument(meta, modelJSON)
		if err != nil {
			if err == errNoRows {
				errorHandler(statusInvalidParameters, "some granted users you enumerated don't exist", &err)
				return
			}
			if strings.Contains(err.Error(), "UNIQUE") {
				errorHandler(statusInvalidParameters, "Such document already exists", &err)
				return
			}
			errorHandler(statusNotExpected, "", &err)
			return
		}
		w.Header().Set("Content-Type", contentTypeJSON)
		_, err = w.Write(modelJSON)
		if err != nil {
			errorHandler(statusNotExpected, "", &err)
			return
		}
	case "PUT", "PATCH", "DELETE", "OPTIONS", "TRACE", "CONNECT":
		errorHandler(statusUnimplementedMethod, "", &err)
	default:
		errorHandler(statusInvalidMethod, "", &err)
	}
	return
}

func docsIDHandler(w http.ResponseWriter, r *http.Request) (err error) {
	id := path.Base(r.URL.Path)
	if id == routes["docs"] {
		errorHandler(statusInvalidParameters, "id is missing or it is `docs` - offensive and inappropriate value", &err)
		return
	}
	switch r.Method {
	case "GET", "HEAD", "DELETE":
		err = r.ParseForm()
		if err != nil {
			errorHandler(statusInvalidParameters, "", &err)
			return
		}
		token := r.Form.Get(tokenQuery)
		var login string
		login, err = getLogin(token)
		if err != nil {
			return
		}
		switch r.Method {
		case "DELETE":
			err = myDB.DeleteDocument(id)
			if err != nil {
				if err == errNoRows {
					errorHandler(statusInvalidParameters, "wrong id", &err)
					return
				}
				errorHandler(statusNotExpected, "", &err)
				return
			}
			model := &outModel{}
			model.Response = map[string]interface{}{id: true}
			err = sendJSON(w, model)
			if err != nil {
				return
			}
		case "GET", "HEAD":
			var doc *docsdb.Doc
			doc, err = myDB.GetDocument(id)
			if err != nil && err != errNoRows {
				errorHandler(statusNotExpected, "", &err)
				return
			}
			if doc == nil {
				errorHandler(statusInvalidParameters, "wrong id", &err)
				return
			}
			var admin bool
			admin, err = myDB.IsAdmin(login)
			if err != nil {
				errorHandler(statusNotExpected, "", &err)
				return
			}
			if !admin {
				if !doc.Public {
					var isGranted bool
					for _, v := range doc.Grant {
						if v == login {
							isGranted = true
						}
					}
					if !isGranted {
						errorHandler(statusAccessDenied, "YOU SHALL NOT PASS", &err)
						return
					}
				}
			}
			var f *os.File
			f, err = os.Open(filepath.Join(dataPath, doc.Name))
			if err != nil {
				errorHandler(statusNotExpected, "", &err)
				return
			}
			var fi os.FileInfo
			fi, err = f.Stat()
			if err != nil {
				errorHandler(statusNotExpected, "", &err)
				return
			}
			w.Header().Set("Content-Disposition", "attachment; filename="+doc.Name)
			w.Header().Set("Content-Type", doc.Mime)
			w.Header().Set("ContentLength", fmt.Sprint(fi.Size()))
			if r.Method == "GET" {
				_, err = io.Copy(w, f)
				if err != nil {
					errorHandler(statusNotExpected, "", &err)
					return
				}
			} else {
				errorHandler(statusOk, "", &err)
			}
		}
	case "PUT":
		var metaModel *docsdb.Doc
		var modelJSON []byte
		r.Body = http.MaxBytesReader(w, r.Body, maxMB)
		metaModel, modelJSON, err = readMulitpart(r)
		if err != nil {
			return
		}
		metaModel.ID = id
		err = myDB.UpdateDocument(metaModel, modelJSON)
		if err != nil {
			if err == errNoRows {
				errorHandler(statusInvalidParameters, "id or grant are incorect", &err)
				return
			}
			if strings.Contains(err.Error(), "UNIQUE") {
				errorHandler(statusInvalidParameters, "this id ("+id+") is already exist", &err)
				return
			}
			errorHandler(statusNotExpected, "", &err)
			return
		}
		w.Header().Set("Content-Type", contentTypeJSON)
		_, err = w.Write(modelJSON)
		if err != nil {
			errorHandler(statusNotExpected, "", &err)
			return
		}
	case "POST", "PATCH", "OPTIONS", "TRACE", "CONNECT":
		errorHandler(statusUnimplementedMethod, "", &err)
	default:
		errorHandler(statusInvalidMethod, "", &err)
	}
	return
}

func logoutHandler(w http.ResponseWriter, r *http.Request) (err error) {
	token := path.Base(r.URL.Path)
	if token == "auth" {
		errorHandler(statusNotAuthorized, "", &err)
		return
	}
	switch r.Method {
	case "DELETE":
		err = myDB.ClearToken(token)
		if err != nil {
			errorHandler(statusNotExpected, "", &err)
			return
		}
		model := &outModel{}
		model.Response = map[string]interface{}{token: true}
		model.Response[token] = true
		err = sendJSON(w, model)
		if err != nil {
			return
		}
	case "GET", "HEAD", "POST", "PUT", "PATCH", "OPTIONS", "TRACE", "CONNECT":
		errorHandler(statusUnimplementedMethod, "", &err)
	default:
		errorHandler(statusInvalidMethod, "", &err)
	}
	return
}
