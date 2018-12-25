package docsdb

import (
	"database/sql"
)

// Doc is the model of the database table Document
// (exception Grant which the database table Grant is responsible for)
type Doc struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Mime    string   `json:"mime"`
	File    bool     `json:"file,boolean"`
	Public  bool     `json:"public,boolean"`
	Created string   `json:"created"`
	Grant   []string `json:"grant"`
	JSON    []byte   `json:"json,omitempty"`
}

// User is the model of the databse table User
type User struct {
	Login       string `json:"login"`
	Password    string `json:"password"`
	Token       string `json:"token"`
	AdminRights bool   `json:"admin,boolean"`
}

// Filter is the parameters for building queries
type Filter struct {
	Login  string `json:"login"`
	Column string `json:"column"`
	Value  string `json:"value"`
	Limit  int    `json:"limit"`
}

// ISQL is the interface of sql database primarily for flexibility and mocking
type ISQL interface {
	AddUser(*User) error
	ClearToken(string) error
	Connect() error
	CreateDocument(*Doc, []byte) error
	DeleteDocument(string) error
	Disconnect()
	GetDocument(string) (*Doc, error)
	GetDocumentsList(*Filter) ([]*Doc, error)
	GetLogin(string) (string, error)
	GetPassword(string) (string, error)
	Init(string, string) error
	IsAdmin(string) (bool, error)
	UpdateDocument(*Doc, []byte) error
	UpdateToken(string, string) error
}

// Handler is sql database tool to work with sqlDriver
type Handler struct {
	db                       *sql.DB
	path                     string
	driver                   string
	stmtClearToken           *sql.Stmt
	stmtDeleteDoc            *sql.Stmt
	stmtDeleteGrantDocID     *sql.Stmt
	stmtGetAdmin             *sql.Stmt
	stmtGetDoc               *sql.Stmt
	stmtGetDocsDefaultFilter *sql.Stmt
	stmtGetDocID             *sql.Stmt
	stmtGetLogin             *sql.Stmt
	stmtGetPassword          *sql.Stmt
	stmtGetUserLogin         *sql.Stmt
	stmtGetUserUID           *sql.Stmt
	stmtInsDoc               *sql.Stmt
	stmtInsGrant             *sql.Stmt
	stmtInsUser              *sql.Stmt
	stmtUpdateDoc            *sql.Stmt
	stmtUpdateToken          *sql.Stmt
}

// AddUser inserts into User login, password and admin
func (h *Handler) AddUser(user *User) (err error) {
	_, err = h.stmtInsUser.Exec(user.Login, user.Password, user.AdminRights)
	return
}

// ClearToken updates user to set token as "" (empty string)
func (h *Handler) ClearToken(token string) (err error) {
	_, err = h.stmtClearToken.Exec(token)
	return
}

// Connect creates connection to the database
func (h *Handler) Connect() (err error) {
	h.db, err = sql.Open(h.driver, h.path)
	return
}

// CreateDocument inserts into Document and Grant values,
// then finds user uid by login and fill the Grant table
func (h *Handler) CreateDocument(d *Doc, JSON []byte) (err error) {
	tx, err := h.db.Begin()
	if err != nil {
		return
	}
	defer tx.Rollback()
	res, err := tx.Stmt(h.stmtInsDoc).Exec(d.ID, d.Name, d.Mime, d.File, d.Public, d.Created, d.JSON)
	if err != nil {
		return
	}
	docID, err := res.LastInsertId()
	if err != nil {
		return
	}
	for _, v := range d.Grant {
		uidRow := tx.Stmt(h.stmtGetUserUID).QueryRow(v)
		var uid int
		for i := 0; i < 5; i++ {
			err = uidRow.Scan(&uid)
			if err != nil {
				if err == sql.ErrConnDone {
					err = h.Connect()
					if err != nil {
						return
					}
				}
				return
			}
			break
		}
		_, err = tx.Stmt(h.stmtInsGrant).Exec(docID, uid)
		if err != nil {
			return
		}
	}
	tx.Commit()
	return
}

// DeleteDocument finds docid by id, deletes documents from Grant and then from Document
func (h *Handler) DeleteDocument(id string) (err error) {
	tx, err := h.db.Begin()
	if err != nil {
		return
	}
	defer tx.Rollback()
	row := tx.Stmt(h.stmtGetDocID).QueryRow(id)
	var docID int
	for i := 0; i < 5; i++ {
		err = row.Scan(&docID)
		if err != nil {
			if err == sql.ErrConnDone {
				err = h.Connect()
				if err != nil {
					return
				}
			}
			return
		}
		break
	}
	_, err = tx.Stmt(h.stmtDeleteGrantDocID).Exec(docID)
	if err != nil {
		return
	}
	_, err = tx.Stmt(h.stmtDeleteDoc).Exec(docID)
	if err != nil {
		return
	}
	tx.Commit()
	return
}

// Disconnect closes connection of the database
func (h *Handler) Disconnect() {
	h.db.Close()
}

//GetDocument finds document by id and then finds all the granted logins by joining Document, Grant, User
func (h *Handler) GetDocument(id string) (doc *Doc, err error) {
	var docID int
	d := &Doc{}
	row := h.stmtGetDoc.QueryRow(id)
	for i := 0; i < 5; i++ {
		err = row.Scan(&docID, &d.Name, &d.Mime, &d.File, &d.Public, &d.Created, &d.JSON)
		if err != nil {
			if err == sql.ErrConnDone {
				err = h.Connect()
				if err != nil {
					return
				}
			}
			return
		}
		break
	}
	rows, err := h.stmtGetLogin.Query(docID)
	if err != nil {
		return
	}
	var grant []string
	for rows.Next() {
		var s string
		for i := 0; i < 5; i++ {
			err = rows.Scan(&s)
			if err != nil {
				if err == sql.ErrConnDone {
					err = h.Connect()
					if err != nil {
						return
					}
				}
				return
			}
			break
		}
		grant = append(grant, s)
	}
	d.Grant = grant
	doc = d
	return
}

// GetDocumentsList finds all documents that filter.Login has access to depending on filter parameters
func (h *Handler) GetDocumentsList(filter *Filter) (doc []*Doc, err error) {
	var rows *sql.Rows
	if filter.Column == "" || filter.Value == "" {
		rows, err = h.stmtGetDocsDefaultFilter.Query(filter.Login, filter.Limit)
		if err != nil {
			return
		}
	} else {
		rows, err = h.db.Query(`SELECT d.docid, d.id, d.name, d.mime, d.file, d.public, d.created, d.json 
		FROM Document as d INNER JOIN Grant as g ON(d.docID=g.docID) INNER JOIN User as u ON(g.uid=u.uid)
		WHERE u.login=? AND `+filter.Column+`=?
		UNION
		SELECT d.docid, d.id, d.name, d.mime, d.file, d.public, d.created, d.json
		FROM Document as d
		WHERE d.public=true AND `+filter.Column+`=?
		ORDER BY d.name, d.created
		LIMIT ?`, filter.Login, filter.Value, filter.Value, filter.Limit)
		if err != nil {
			return
		}
	}
	var gRows *sql.Rows
	var docid int
	i := 0
	var d []*Doc
	defer rows.Close()
	for rows.Next() {
		d = append(d, &Doc{})
		for i := 0; i < 5; i++ {
			err = rows.Scan(&docid, &d[i].ID, &d[i].Name, &d[i].Mime, &d[i].File, &d[i].Public, &d[i].Created, &d[i].JSON)
			if err != nil {
				if err == sql.ErrConnDone {
					err = h.Connect()
					if err != nil {
						return
					}
				}
				return
			}
			break
		}
		gRows, err = h.stmtGetLogin.Query(docid)
		if err != nil {
			return
		}
		for gRows.Next() {
			var login string
			for i := 0; i < 5; i++ {
				err = gRows.Scan(&login)
				if err != nil {
					if err == sql.ErrConnDone {
						err = h.Connect()
						if err != nil {
							return
						}
					}
					return
				}
				break
			}
			d[i].Grant = append(d[i].Grant, login)
		}
		i++
		gRows.Close()
	}
	doc = d
	return
}

// GetLogin finds login by token
func (h *Handler) GetLogin(token string) (login string, err error) {
	row := h.stmtGetUserLogin.QueryRow(token)
	for i := 0; i < 5; i++ {
		err = row.Scan(&login)
		if err != nil {
			if err == sql.ErrConnDone {
				err = h.Connect()
				if err != nil {
					return
				}
			}
			return
		}
		break
	}
	return
}

// GetPassword finds password by login
func (h *Handler) GetPassword(login string) (password string, err error) {
	row := h.stmtGetPassword.QueryRow(login)
	for i := 0; i < 5; i++ {
		err = row.Scan(&password)
		if err != nil {
			if err == sql.ErrConnDone {
				err = h.Connect()
				if err != nil {
					return
				}
			}
			return
		}
		break
	}
	return
}

// Init creates connection to the database and prepares the statements
func (h *Handler) Init(driver string, path string) (err error) {
	h.driver = driver
	h.path = path
	err = h.Connect()
	if err != nil {
		return
	}
	h.stmtInsUser, err = h.db.Prepare(`INSERT INTO User (login, password, admin) VALUES (?, ?, ?)`)
	if err != nil {
		return
	}
	h.stmtUpdateToken, err = h.db.Prepare(`UPDATE User SET token=? WHERE login=?`)
	if err != nil {
		return
	}
	h.stmtClearToken, err = h.db.Prepare(`UPDATE User SET token="" WHERE token=?`)
	if err != nil {
		return
	}
	h.stmtInsDoc, err = h.db.Prepare(`INSERT INTO Document(id, name, mime, file, public, created, json) values (?,?,?,?,?,?,?)`)
	if err != nil {
		return
	}
	h.stmtInsGrant, err = h.db.Prepare("INSERT INTO Grant(docid, uid) values (?,?)")
	if err != nil {
		return
	}
	h.stmtGetUserUID, err = h.db.Prepare("SELECT uid FROM User WHERE login=?")
	if err != nil {
		return
	}
	h.stmtGetDoc, err = h.db.Prepare(`SELECT d.docid, d.name, d.mime, d.file, d.public, d.created, d.json FROM Document as d WHERE d.id=?`)
	if err != nil {
		return
	}
	h.stmtGetLogin, err = h.db.Prepare(`SELECT u.login FROM Grant INNER JOIN User as u USING(uid) WHERE Grant.docid=?`)
	if err != nil {
		return
	}
	h.stmtGetDocsDefaultFilter, err = h.db.Prepare(`
	SELECT d.docid, d.id, d.name, d.mime, d.file, d.public, d.created, d.json 
	FROM Document as d INNER JOIN Grant as g ON(d.docid=g.docid) INNER JOIN User as u ON(g.uid=u.uid)
	WHERE u.login=?
	UNION
	SELECT d.docid, d.id, d.name, d.mime, d.file, d.public, d.created, d.json
	FROM Document as d
	WHERE d.public=true
	ORDER BY d.name, d.created
	LIMIT ?`)
	if err != nil {
		return
	}
	h.stmtGetUserLogin, err = h.db.Prepare(`SELECT login FROM User WHERE token=?`)
	if err != nil {
		return
	}
	h.stmtUpdateDoc, err = h.db.Prepare(`UPDATE Document SET name=?, mime=?, file=?, public=?, created=?, json=? WHERE id=?`)
	if err != nil {
		return
	}
	h.stmtGetPassword, err = h.db.Prepare(`SELECT password FROM User WHERE login=?`)
	if err != nil {
		return
	}
	h.stmtGetDocID, err = h.db.Prepare(`SELECT docid from Document WHERE id=?`)
	if err != nil {
		return
	}
	h.stmtDeleteGrantDocID, err = h.db.Prepare(`DELETE FROM Grant WHERE docid=?`)
	if err != nil {
		return
	}
	h.stmtDeleteDoc, err = h.db.Prepare(`DELETE FROM Document WHERE docid=?`)
	if err != nil {
		return
	}
	h.stmtGetAdmin, err = h.db.Prepare(`SELECT admin FROM User WHERE login=?`)
	if err != nil {
		return
	}
	return
}

// IsAdmin checks if User.login has admin rights
func (h *Handler) IsAdmin(login string) (admin bool, err error) {
	row := h.stmtGetAdmin.QueryRow(login)
	for i := 0; i < 5; i++ {
		err = row.Scan(&admin)
		if err != nil {
			if err == sql.ErrConnDone {
				err = h.Connect()
				if err != nil {
					return
				}
			}
			return
		}
		break
	}
	return
}

// UpdateDocument updates Document, finds docid and uids and deletes from Grant then updates Grant wtih new ones
func (h *Handler) UpdateDocument(d *Doc, JSON []byte) (err error) {
	dCurrent, err := h.GetDocument(d.ID)
	if err != nil {
		if err == sql.ErrNoRows {
			err = h.CreateDocument(d, JSON)
		}
		return
	}
	tx, err := h.db.Begin()
	if err != nil {
		return
	}
	defer tx.Rollback()
	_, err = tx.Stmt(h.stmtUpdateDoc).Exec(d.Name, d.Mime, d.File, d.Public, d.Created, d.JSON, d.ID)
	if err != nil {
		return
	}
	var docID int
	row := tx.Stmt(h.stmtGetDocID).QueryRow(d.ID)
	for i := 0; i < 5; i++ {
		err = row.Scan(&docID)
		if err != nil {
			if err == sql.ErrConnDone {
				err = h.Connect()
				if err != nil {
					return
				}
			}
			return
		}
		break
	}
	for _, v := range d.Grant {
		var uid int
		needDelete := true
		for _, v2 := range dCurrent.Grant {
			if v == v2 {
				needDelete = false
			}
		}
		if !needDelete {
			continue
		}
		row := tx.Stmt(h.stmtGetUserUID).QueryRow(v)
		for i := 0; i < 5; i++ {
			err = row.Scan(&uid)
			if err != nil {
				if err == sql.ErrConnDone {
					err = h.Connect()
					if err != nil {
						return
					}
				}
				return
			}
			break
		}
		_, err = tx.Stmt(h.stmtDeleteGrantDocID).Exec(d.ID)
		if err != nil {
			return
		}
		_, err = tx.Stmt(h.stmtInsGrant).Exec(docID, uid)
		if err != nil {
			return
		}
	}
	tx.Commit()
	return
}

// UpdateToken updates User with provided login to set new token
func (h *Handler) UpdateToken(login string, token string) (err error) {
	_, err = h.stmtUpdateToken.Exec(token, login)
	return
}
