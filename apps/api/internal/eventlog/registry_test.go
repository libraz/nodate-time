package eventlog

import (
	"bufio"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"sort"
	"strconv"
	"strings"
	"testing"
)

var registryFiles = []string{
	"../../../../sql/core/event-kinds.yaml",
	"../../../../sql/time/event-kinds.yaml",
}

// readRegistry returns kind -> subject from one registry. Only the `- kind:`
// and `subject:` lines are read, the way the conformance tooling reads them.
func readRegistry(t *testing.T, path string) map[string]string {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open registry: %v", err)
	}
	defer f.Close()
	out := map[string]string{}
	current := ""
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		switch {
		case strings.HasPrefix(line, "- kind:"):
			current = strings.TrimSpace(strings.TrimPrefix(line, "- kind:"))
			if _, dup := out[current]; dup {
				t.Fatalf("%s lists %s twice", path, current)
			}
			out[current] = ""
		case strings.HasPrefix(line, "subject:") && current != "":
			v := strings.TrimSpace(strings.TrimPrefix(line, "subject:"))
			if i := strings.Index(v, "#"); i >= 0 {
				v = strings.TrimSpace(v[:i])
			}
			out[current] = strings.Trim(v, `"'`)
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

// declaredKinds returns the value of every Type* constant in eventlog.go.
func declaredKinds(t *testing.T) []string {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), "eventlog.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.CONST {
			continue
		}
		for _, spec := range gen.Specs {
			vs := spec.(*ast.ValueSpec)
			for i, name := range vs.Names {
				if !strings.HasPrefix(name.Name, "Type") {
					continue
				}
				lit, ok := vs.Values[i].(*ast.BasicLit)
				if !ok {
					t.Fatalf("%s is not a string literal", name.Name)
				}
				v, err := strconv.Unquote(lit.Value)
				if err != nil {
					t.Fatal(err)
				}
				out = append(out, v)
			}
		}
	}
	return out
}

func sortedKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// The union of the core and time registries is exactly the set of kind
// constants, and the subject each kind carries in Append matches the one the
// registry gives it.
func TestRegistry(t *testing.T) {
	union := map[string]string{}
	for _, path := range registryFiles {
		for kind, subject := range readRegistry(t, path) {
			if _, dup := union[kind]; dup {
				t.Errorf("%s is listed in more than one registry", kind)
			}
			union[kind] = subject
		}
	}

	constants := map[string]bool{}
	for _, v := range declaredKinds(t) {
		if constants[v] {
			t.Errorf("constant value %s is declared twice", v)
		}
		constants[v] = true
	}

	for _, kind := range sortedKeys(union) {
		if !constants[kind] {
			t.Errorf("registry kind %s has no Type constant", kind)
		}
		got, ok := kinds[kind]
		switch {
		case !ok:
			t.Errorf("registry kind %s is missing from the subject table", kind)
		case got != union[kind]:
			t.Errorf("kind %s: subject table says %q, registry says %q", kind, got, union[kind])
		}
	}
	for kind := range constants {
		if _, ok := union[kind]; !ok {
			t.Errorf("constant %s is in no registry", kind)
		}
	}
	for kind := range kinds {
		if _, ok := union[kind]; !ok {
			t.Errorf("subject table kind %s is in no registry", kind)
		}
	}
	if len(union) == 0 {
		t.Fatal("registries are empty, so nothing was checked")
	}
}

// Append refuses a subject-bearing kind without a subject, a subject on a
// kind that has none, and a kind no registry lists. The checks run before the
// database is reached, so no connection is needed.
func TestAppendSubjectRules(t *testing.T) {
	subject := make([]byte, 16)
	cases := []struct {
		name  string
		entry Entry
	}{
		{"subject missing", Entry{WorkspaceID: 1, Type: TypeEventCreated}},
		{"subject wrong length", Entry{WorkspaceID: 1, Type: TypeMemoCreated, Subject: []byte{1, 2}}},
		{"subject on a kind without one", Entry{WorkspaceID: 1, Type: TypeCalendarSetUp, Subject: subject}},
		{"unregistered kind", Entry{WorkspaceID: 1, Type: "calendar.comment.added"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if err := Append(nil, nil, c.entry); err == nil {
				t.Fatal("Append accepted the entry")
			}
		})
	}
}

// or in neither: a kind the time registry adds follows the same rule.
func TestEverySubjectNamesAKnownTable(t *testing.T) {
	for kind, table := range kinds {
		if table == "" {
			continue
		}
		if _, ok := subjectPayloadKey[table]; !ok {
			t.Errorf("kind %s names subject table %q, which has no payload key", kind, table)
		}
	}
}
