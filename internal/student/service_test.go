package student

import (
	"context"
	"errors"
	"sort"
	"strings"
	"sync"
	"testing"
)

type fakeRepo struct {
	mu       sync.Mutex
	byID     map[int64]*Student
	hashes   map[int64]string
	revoked  map[int64]int
	nextID   int64
	failCode string // Import fails when it reaches this code (rollback test)
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{byID: map[int64]*Student{}, hashes: map[int64]string{}, revoked: map[int64]int{}}
}

func (f *fakeRepo) codeTaken(code string) bool {
	for _, s := range f.byID {
		if s.Code == code {
			return true
		}
	}
	return false
}

func (f *fakeRepo) insert(n NewStudent) Student {
	f.nextID++
	s := &Student{ID: f.nextID, Code: n.Code, Fullname: n.Fullname}
	f.byID[s.ID] = s
	f.hashes[s.ID] = n.PasswordHash
	return *s
}

func (f *fakeRepo) Create(_ context.Context, n NewStudent) (Student, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.codeTaken(n.Code) {
		return Student{}, ErrAlreadyExists
	}
	return f.insert(n), nil
}

func (f *fakeRepo) Get(_ context.Context, id int64) (Student, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if s, ok := f.byID[id]; ok {
		return *s, nil
	}
	return Student{}, ErrNotFound
}

func (f *fakeRepo) List(_ context.Context, q string, limit, offset int) ([]Student, int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var all []Student
	for _, s := range f.byID {
		if strings.Contains(strings.ToLower(s.Code), strings.ToLower(q)) ||
			strings.Contains(strings.ToLower(s.Fullname), strings.ToLower(q)) {
			all = append(all, *s)
		}
	}
	sort.Slice(all, func(i, j int) bool { return all[i].Code < all[j].Code })
	total := len(all)
	if offset > total {
		offset = total
	}
	end := min(offset+limit, total)
	return all[offset:end], total, nil
}

func (f *fakeRepo) Import(_ context.Context, rows []NewStudent) ([]Student, []string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	// Validate first so a failure leaves nothing behind, like a rolled-back tx.
	for _, n := range rows {
		if n.Code == f.failCode {
			return nil, nil, errors.New("db error")
		}
	}
	var created []Student
	var skipped []string
	for _, n := range rows {
		if f.codeTaken(n.Code) {
			skipped = append(skipped, n.Code)
			continue
		}
		created = append(created, f.insert(n))
	}
	return created, skipped, nil
}

func (f *fakeRepo) ResetPassword(_ context.Context, id int64, hash string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.byID[id]; !ok {
		return ErrNotFound
	}
	f.hashes[id] = hash
	f.revoked[id]++
	return nil
}

func (f *fakeRepo) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.byID)
}

// fakeHasher makes hashes readable in assertions.
type fakeHasher struct{}

func (fakeHasher) HashPassword(p string) (string, error) { return "hash(" + p + ")", nil }

func TestCreateNormalizesAndUsesCodeAsPassword(t *testing.T) {
	repo := newFakeRepo()
	svc := NewService(repo, fakeHasher{})

	s, err := svc.Create(context.Background(), " b21dccn001 ", "  Nguyen   Van A ")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if s.Code != "B21DCCN001" || s.Fullname != "Nguyen Van A" {
		t.Errorf("got code=%q fullname=%q", s.Code, s.Fullname)
	}
	if got := repo.hashes[s.ID]; got != "hash(B21DCCN001)" {
		t.Errorf("password hash = %q, want hash of the code", got)
	}
}

func TestCreateValidation(t *testing.T) {
	svc := NewService(newFakeRepo(), fakeHasher{})
	cases := []struct{ code, name string }{
		{"", "A"},
		{"SV01", " "},
		{"SV 01", "A"},
		{strings.Repeat("X", maxCodeLen+1), "A"},
		{"SV01", strings.Repeat("a", maxFullnameLen+1)},
	}
	for _, c := range cases {
		if _, err := svc.Create(context.Background(), c.code, c.name); !errors.Is(err, ErrInvalidInput) {
			t.Errorf("Create(%q, %q) err = %v, want ErrInvalidInput", c.code, c.name, err)
		}
	}
}

func TestCreateDuplicate(t *testing.T) {
	svc := NewService(newFakeRepo(), fakeHasher{})
	ctx := context.Background()
	if _, err := svc.Create(ctx, "SV01", "A"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Create(ctx, "sv01", "B"); !errors.Is(err, ErrAlreadyExists) {
		t.Fatalf("err = %v, want ErrAlreadyExists", err)
	}
}

func TestImportSkipsExistingCodes(t *testing.T) {
	repo := newFakeRepo()
	svc := NewService(repo, fakeHasher{})
	ctx := context.Background()
	if _, err := svc.Create(ctx, "SV02", "Existing"); err != nil {
		t.Fatal(err)
	}

	csv := utf8BOM + "code,full_name,class\n" +
		"sv01,Nguyen Van A,D21CQCN01\n" +
		"SV02,Tran Thi B,D21CQCN01\n" +
		"\n" +
		"SV03,\"Le, Van C\",D21CQCN02\n"
	res, err := svc.Import(ctx, strings.NewReader(csv))
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if len(res.Created) != 2 {
		t.Fatalf("created = %d, want 2", len(res.Created))
	}
	if len(res.Skipped) != 1 || res.Skipped[0].Code != "SV02" || res.Skipped[0].Line != 3 {
		t.Fatalf("skipped = %+v, want SV02 on line 3", res.Skipped)
	}
	if res.Created[1].Fullname != "Le, Van C" {
		t.Errorf("quoted name = %q", res.Created[1].Fullname)
	}
	if got := repo.hashes[res.Created[0].ID]; got != "hash(SV01)" {
		t.Errorf("imported password hash = %q", got)
	}
}

func TestImportRejectsWholeFileOnInvalidRows(t *testing.T) {
	repo := newFakeRepo()
	svc := NewService(repo, fakeHasher{})

	csv := "fullname,code\n" +
		"A,SV01\n" +
		",SV02\n" + // missing name
		"C,sv01\n" // duplicate inside the file
	_, err := svc.Import(context.Background(), strings.NewReader(csv))
	var ie *ImportError
	if !errors.As(err, &ie) {
		t.Fatalf("err = %v, want *ImportError", err)
	}
	if len(ie.Rows) != 2 || ie.Rows[0].Line != 3 || ie.Rows[1].Line != 4 {
		t.Fatalf("row errors = %+v", ie.Rows)
	}
	if !strings.Contains(ie.Rows[1].Error, "duplicate of line 2") {
		t.Errorf("duplicate error = %q", ie.Rows[1].Error)
	}
	if repo.count() != 0 {
		t.Errorf("students saved = %d, want 0", repo.count())
	}
}

func TestImportHeaderAndEmpty(t *testing.T) {
	svc := NewService(newFakeRepo(), fakeHasher{})
	for name, csv := range map[string]string{
		"empty":       "",
		"no code col": "msv,full_name\nSV01,A\n",
		"header only": "code,full_name\n",
	} {
		if _, err := svc.Import(context.Background(), strings.NewReader(csv)); !errors.Is(err, ErrInvalidInput) {
			t.Errorf("%s: err = %v, want ErrInvalidInput", name, err)
		}
	}
}

func TestImportDBErrorSavesNothing(t *testing.T) {
	repo := newFakeRepo()
	repo.failCode = "SV02"
	svc := NewService(repo, fakeHasher{})
	_, err := svc.Import(context.Background(), strings.NewReader("code,full_name\nSV01,A\nSV02,B\n"))
	if err == nil {
		t.Fatal("expected error")
	}
	if repo.count() != 0 {
		t.Errorf("students saved = %d, want 0", repo.count())
	}
}

func TestListClampsPaging(t *testing.T) {
	repo := newFakeRepo()
	svc := NewService(repo, fakeHasher{})
	ctx := context.Background()
	for _, c := range []string{"SV03", "SV01", "SV02", "GV99"} {
		if _, err := svc.Create(ctx, c, "Student "+c); err != nil {
			t.Fatal(err)
		}
	}
	page, err := svc.List(ctx, "sv", 0, -5)
	if err != nil {
		t.Fatal(err)
	}
	if page.Limit != DefaultPageSize || page.Offset != 0 || page.Total != 3 {
		t.Errorf("page = %+v", page)
	}
	if page.Items[0].Code != "SV01" {
		t.Errorf("first = %q, want SV01 (sorted by code)", page.Items[0].Code)
	}
	page, _ = svc.List(ctx, "", 10000, 1)
	if page.Limit != MaxPageSize || len(page.Items) != 3 {
		t.Errorf("page limit=%d items=%d", page.Limit, len(page.Items))
	}
}

func TestResetPassword(t *testing.T) {
	repo := newFakeRepo()
	svc := NewService(repo, fakeHasher{})
	ctx := context.Background()
	s, _ := svc.Create(ctx, "SV01", "A")
	repo.hashes[s.ID] = "hash(changed)"

	if err := svc.ResetPassword(ctx, s.ID); err != nil {
		t.Fatalf("ResetPassword: %v", err)
	}
	if repo.hashes[s.ID] != "hash(SV01)" || repo.revoked[s.ID] != 1 {
		t.Errorf("hash=%q revoked=%d", repo.hashes[s.ID], repo.revoked[s.ID])
	}
	if err := svc.ResetPassword(ctx, 999); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown id err = %v, want ErrNotFound", err)
	}
}
