package student

import (
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"strings"
)

// Repository is the persistence boundary for students.
type Repository interface {
	// Create inserts a users row with role student. It returns
	// ErrAlreadyExists if any user (teacher or student) has the code.
	Create(ctx context.Context, s NewStudent) (Student, error)
	// Get, List and ResetPassword only see role = student rows.
	Get(ctx context.Context, id int64) (Student, error)
	// List filters by case-insensitive substring of code or fullname.
	List(ctx context.Context, query string, limit, offset int) ([]Student, int, error)
	// Import inserts all rows in one transaction. Rows whose code already
	// exists are skipped and returned with the existing user's role; any
	// other error rolls everything back.
	Import(ctx context.Context, rows []NewStudent) (created []Student, skipped []ExistingCode, err error)
	// ResetPassword sets the hash and deletes all refresh tokens of the
	// student in one transaction (lock order account -> refresh_token).
	ResetPassword(ctx context.Context, id int64, passwordHash string) error
}

// PasswordHasher is satisfied by *auth.Service.
type PasswordHasher interface {
	HashPassword(password string) (string, error)
}

type Service struct {
	repo   Repository
	hasher PasswordHasher
}

func NewService(repo Repository, hasher PasswordHasher) *Service {
	return &Service{repo: repo, hasher: hasher}
}

// Create adds one student whose initial password is the code (MSV).
func (s *Service) Create(ctx context.Context, code, fullname string) (Student, error) {
	code, fullname, err := normalize(code, fullname)
	if err != nil {
		return Student{}, err
	}
	hash, err := s.hasher.HashPassword(code)
	if err != nil {
		return Student{}, err
	}
	return s.repo.Create(ctx, NewStudent{Code: code, Fullname: fullname, PasswordHash: hash})
}

const (
	DefaultPageSize = 50
	MaxPageSize     = 200
)

type Page struct {
	Items  []Student
	Total  int
	Limit  int
	Offset int
}

// List clamps limit to 1..MaxPageSize (0 means DefaultPageSize) and offset to >= 0.
func (s *Service) List(ctx context.Context, query string, limit, offset int) (Page, error) {
	if limit <= 0 {
		limit = DefaultPageSize
	}
	limit = min(limit, MaxPageSize)
	offset = max(offset, 0)
	items, total, err := s.repo.List(ctx, strings.TrimSpace(query), limit, offset)
	if err != nil {
		return Page{}, err
	}
	return Page{Items: items, Total: total, Limit: limit, Offset: offset}, nil
}

// ResetPassword sets the password back to the student code and revokes all
// refresh tokens. Access tokens already issued stay valid until they expire.
func (s *Service) ResetPassword(ctx context.Context, id int64) error {
	st, err := s.repo.Get(ctx, id)
	if err != nil {
		return err
	}
	hash, err := s.hasher.HashPassword(st.Code)
	if err != nil {
		return err
	}
	return s.repo.ResetPassword(ctx, id, hash)
}

// MaxImportRows bounds one CSV import (a class is ~50 students).
const MaxImportRows = 1000

// utf8BOM is stripped from the header: Excel writes it when saving CSV UTF-8.
var utf8BOM = string(rune(0xFEFF))

// RowError reports a CSV problem; Line is the 1-based line in the file.
type RowError struct {
	Line  int    `json:"line"`
	Code  string `json:"code,omitempty"`
	Error string `json:"error"`
}

// ImportError is returned when the file is rejected; nothing was inserted.
type ImportError struct {
	Rows []RowError
}

func (e *ImportError) Error() string {
	return fmt.Sprintf("%d invalid row(s)", len(e.Rows))
}

type SkippedRow struct {
	Line   int    `json:"line"`
	Code   string `json:"code"`
	Reason string `json:"reason"`
}

type ImportResult struct {
	Created []Student
	Skipped []SkippedRow
}

// Import reads a CSV with a header row containing "code" and "full_name"
// (or "fullname"); other columns such as "class" are ignored because the
// users table has no such column.
//
// Policy: the import is all-or-nothing for invalid rows (missing/too long
// fields, a code repeated inside the file) — any such row rejects the whole
// file with per-row errors. Codes that already exist in the DB are skipped
// and reported, so re-importing the same roster is safe.
func (s *Service) Import(ctx context.Context, r io.Reader) (ImportResult, error) {
	rows, lines, err := s.parseCSV(r)
	if err != nil {
		return ImportResult{}, err
	}

	created, existing, err := s.repo.Import(ctx, rows)
	if err != nil {
		return ImportResult{}, err
	}
	lineOf := make(map[string]int, len(rows))
	for i, row := range rows {
		lineOf[row.Code] = lines[i]
	}
	res := ImportResult{Created: created}
	for _, ex := range existing {
		reason := "already exists"
		if ex.Role == "teacher" {
			reason = "code belongs to a teacher account"
		}
		res.Skipped = append(res.Skipped, SkippedRow{Line: lineOf[ex.Code], Code: ex.Code, Reason: reason})
	}
	return res, nil
}

func (s *Service) parseCSV(r io.Reader) ([]NewStudent, []int, error) {
	cr := csv.NewReader(r)
	cr.FieldsPerRecord = -1
	cr.TrimLeadingSpace = true

	header, err := cr.Read()
	if errors.Is(err, io.EOF) {
		return nil, nil, fmt.Errorf("%w: empty file", ErrInvalidInput)
	}
	if err != nil {
		return nil, nil, fmt.Errorf("%w: %v", ErrInvalidInput, err)
	}
	codeCol, nameCol := -1, -1
	for i, h := range header {
		h = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(h, utf8BOM)))
		switch h {
		case "code":
			codeCol = i
		case "full_name", "fullname":
			nameCol = i
		}
	}
	if codeCol < 0 || nameCol < 0 {
		return nil, nil, fmt.Errorf("%w: header must contain columns code and full_name", ErrInvalidInput)
	}

	var (
		rows    []NewStudent
		lines   []int
		rowErrs []RowError
		seen    = map[string]int{}
	)
	for {
		rec, err := cr.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		line, _ := cr.FieldPos(0)
		if err != nil {
			rowErrs = append(rowErrs, RowError{Line: line, Error: err.Error()})
			continue
		}
		if len(rec) == 1 && strings.TrimSpace(rec[0]) == "" {
			continue // blank line
		}
		if len(rows)+len(rowErrs) >= MaxImportRows {
			return nil, nil, fmt.Errorf("%w: more than %d rows", ErrInvalidInput, MaxImportRows)
		}
		var rawCode, rawName string
		if codeCol < len(rec) {
			rawCode = rec[codeCol]
		}
		if nameCol < len(rec) {
			rawName = rec[nameCol]
		}
		code, name, err := normalize(rawCode, rawName)
		if err != nil {
			rowErrs = append(rowErrs, RowError{Line: line, Code: strings.TrimSpace(rawCode), Error: strings.TrimPrefix(err.Error(), ErrInvalidInput.Error()+": ")})
			continue
		}
		if first, dup := seen[code]; dup {
			rowErrs = append(rowErrs, RowError{Line: line, Code: code, Error: fmt.Sprintf("duplicate of line %d in this file", first)})
			continue
		}
		seen[code] = line
		rows = append(rows, NewStudent{Code: code, Fullname: name})
		lines = append(lines, line)
	}
	if len(rowErrs) > 0 {
		return nil, nil, &ImportError{Rows: rowErrs}
	}
	if len(rows) == 0 {
		return nil, nil, fmt.Errorf("%w: no data rows", ErrInvalidInput)
	}

	// Hash outside the DB transaction; bcrypt is the slow part.
	for i := range rows {
		hash, err := s.hasher.HashPassword(rows[i].Code)
		if err != nil {
			return nil, nil, err
		}
		rows[i].PasswordHash = hash
	}
	return rows, lines, nil
}
