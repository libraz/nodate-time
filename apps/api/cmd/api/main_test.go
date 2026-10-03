package main

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/libraz/nodate-time/apps/api/internal/config"
)

// Configured storage whose client could not be built is reported down, not
// left out of the report as if no storage had been asked for.
func TestHealthReportsConfiguredStorageEvenWithoutAClient(t *testing.T) {
	// A handle to nowhere: only the storage result is under test.
	db, err := sql.Open("mysql", "u:p@tcp(127.0.0.1:1)/none")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	cfg := &config.Config{S3Endpoint: "minio:9000"}
	_, results := healthReporter(db, nil, errors.New("bad endpoint"), cfg).Run(context.Background())
	for _, r := range results {
		if r.Name == "object_storage" {
			if r.Status == "ok" || r.Error != "bad endpoint" {
				t.Fatalf("object_storage = %+v, want down with the build error", r)
			}
			return
		}
	}
	t.Fatal("object_storage missing from the report although TC_S3_ENDPOINT is set")
}

// A store that is not answering at boot is retried until it is.
func TestEnsureBucketRetriesUntilTheStoreAnswers(t *testing.T) {
	var calls atomic.Int32
	ensure := func(context.Context) error {
		if calls.Add(1) < 3 {
			return errors.New("connection refused")
		}
		return nil
	}
	done := make(chan struct{})
	go func() {
		ensureBucket(context.Background(), ensure, "b", time.Millisecond, 4*time.Millisecond)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("ensureBucket did not finish once the store answered")
	}
	if got := calls.Load(); got != 3 {
		t.Fatalf("ensure called %d times, want 3", got)
	}
}

type stuckServer struct{}

func (stuckServer) Shutdown(context.Context) error { return context.DeadlineExceeded }

// A server that does not drain in time must not cost the mail its turn.
func TestShutdownDrainsMailEvenWhenTheServerDoesNot(t *testing.T) {
	var waited atomic.Bool
	err := shutdown([]shutdowner{stuckServer{}}, func() { waited.Store(true) })
	if err == nil {
		t.Error("a server that failed to drain should be reported")
	}
	if !waited.Load() {
		t.Fatal("mail was not given its drain budget after the server failed to drain")
	}
}

// The orchestrator has to wait at least as long as the process may take.
func TestComposeStopGraceCoversTheShutdownBudgets(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "compose.prod.yml"))
	if err != nil {
		t.Fatal(err)
	}
	_, api, ok := strings.Cut(string(raw), "\n  api:\n")
	if !ok {
		t.Fatal("no api service in compose.prod.yml")
	}
	m := regexp.MustCompile(`(?m)^    stop_grace_period: (\S+)$`).FindStringSubmatch(strings.SplitN(api, "\n  web:\n", 2)[0])
	if m == nil {
		t.Fatal("the api service sets no stop_grace_period; Docker's 10s default is shorter than the shutdown budgets")
	}
	grace, err := time.ParseDuration(m[1])
	if err != nil {
		t.Fatal(err)
	}
	if need := httpDrainBudget + mailDrainBudget; grace < need {
		t.Fatalf("stop_grace_period %v is shorter than the shutdown budgets (%v)", grace, need)
	}
}
