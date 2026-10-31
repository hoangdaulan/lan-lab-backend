package problem

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/hoangdaulan/lan-lab-backend/internal/apperr"
)

// MaxTests bounds one problem's test set.
const MaxTests = 500

// TestDir is the problem's test directory relative to DATA_DIR. Teachers put
// N.in / N.out pairs here on the server, then press "rejudge".
func TestDir(problemID int64) string {
	return path.Join("problems", strconv.FormatInt(problemID, 10), "tests")
}

// ScanTests lists the .in/.out pairs in dataDir/relDir. Every .in needs a
// matching .out and vice versa; other files and hidden files are ignored.
// Symlinks and subdirectories are rejected so paths cannot leave DATA_DIR.
// Pairs are ordered numerically when names are numbers ("2" before "10"),
// then by name.
func ScanTests(dataDir, relDir string) ([]TestFile, error) {
	entries, err := os.ReadDir(filepath.Join(dataDir, filepath.FromSlash(relDir)))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, apperr.Conflict("test directory %s does not exist under DATA_DIR", relDir)
	}
	if err != nil {
		return nil, fmt.Errorf("read test directory: %w", err)
	}

	type pair struct {
		in, out       bool
		inSize, outSz int64
	}
	pairs := map[string]*pair{}
	var problems []string
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		ext := path.Ext(name)
		if ext != ".in" && ext != ".out" {
			continue
		}
		base := strings.TrimSuffix(name, ext)
		if base == "" {
			problems = append(problems, name+": empty test name")
			continue
		}
		if !e.Type().IsRegular() {
			problems = append(problems, name+": not a regular file")
			continue
		}
		info, err := e.Info()
		if err != nil {
			return nil, fmt.Errorf("stat %s: %w", name, err)
		}
		p := pairs[base]
		if p == nil {
			p = &pair{}
			pairs[base] = p
		}
		if ext == ".in" {
			p.in, p.inSize = true, info.Size()
		} else {
			p.out, p.outSz = true, info.Size()
		}
	}

	names := make([]string, 0, len(pairs))
	for name, p := range pairs {
		switch {
		case !p.out:
			problems = append(problems, name+".in has no "+name+".out")
		case !p.in:
			problems = append(problems, name+".out has no "+name+".in")
		default:
			names = append(names, name)
		}
	}
	if len(problems) > 0 {
		sort.Strings(problems)
		return nil, apperr.Conflict("invalid test files in %s: %s", relDir, strings.Join(problems, "; "))
	}
	if len(names) == 0 {
		return nil, apperr.Conflict("no tests in %s (expected pairs like 1.in / 1.out)", relDir)
	}
	if len(names) > MaxTests {
		return nil, apperr.Conflict("too many tests in %s (%d > %d)", relDir, len(names), MaxTests)
	}

	sort.Slice(names, func(i, j int) bool { return testNameLess(names[i], names[j]) })
	out := make([]TestFile, 0, len(names))
	for _, name := range names {
		p := pairs[name]
		out = append(out, TestFile{
			Name:       name,
			InputPath:  path.Join(relDir, name+".in"),
			OutputPath: path.Join(relDir, name+".out"),
			InputSize:  p.inSize,
			OutputSize: p.outSz,
		})
	}
	return out, nil
}

func testNameLess(a, b string) bool {
	na, errA := strconv.Atoi(a)
	nb, errB := strconv.Atoi(b)
	switch {
	case errA == nil && errB == nil && na != nb:
		return na < nb
	case errA == nil && errB != nil:
		return true
	case errA != nil && errB == nil:
		return false
	}
	return a < b
}
