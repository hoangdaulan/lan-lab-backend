//go:build integration

package student_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/hoangdaulan/lan-lab-backend/internal/auth"
	"github.com/hoangdaulan/lan-lab-backend/internal/student"
	"github.com/hoangdaulan/lan-lab-backend/internal/testdb"
)

// TestImportDateOfBirthLogin imports a roster into a real database, logs in
// with the date of birth as ddmmyyyy and checks that a file with one row
// missing its date of birth saves nothing.
func TestImportDateOfBirthLogin(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	authSvc := auth.NewService(auth.NewRepository(pool), auth.NewJWTSigner("test-secret-0123456789abcdef", time.Minute),
		time.Hour, auth.WithBcryptCost(bcrypt.MinCost))
	svc := student.NewService(student.NewRepository(pool), authSvc)

	res, err := svc.Import(ctx, strings.NewReader("full_name,code,date_of_birth\nNguyen Van A,b21dccn001,05/09/2004\n"))
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if len(res.Created) != 1 || res.Created[0].DateOfBirth == nil ||
		res.Created[0].DateOfBirth.Format(time.DateOnly) != "2004-09-05" {
		t.Fatalf("created = %+v", res.Created)
	}
	if _, _, err := authSvc.Login(ctx, "B21DCCN001", "05092004"); err != nil {
		t.Fatalf("login with ddmmyyyy: %v", err)
	}
	if _, _, err := authSvc.Login(ctx, "B21DCCN001", "B21DCCN001"); err == nil {
		t.Fatal("login with the code must fail")
	}

	_, err = svc.Import(ctx, strings.NewReader("full_name,code,date_of_birth\nB,SV02,01/01/2004\nC,SV03,\n"))
	var ie *student.ImportError
	if !errors.As(err, &ie) {
		t.Fatalf("err = %v, want *ImportError", err)
	}
	page, err := svc.List(ctx, "SV0", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 0 {
		t.Fatalf("rejected import saved %d students", page.Total)
	}
}
