package main

import (
	"context"
	"database/sql"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func TestStoreAndLoop(t *testing.T) {
	ctx := context.Background()
	db, err := sql.Open("sqlite", "file:"+t.TempDir()+"/t.db")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(schema); err != nil {
		t.Fatal(err)
	}
	later, _ := add(ctx, db, job{runAt: time.Now().Add(time.Hour), to: "a", label: "a", body: "later", srcID: "1"})
	soon, _ := add(ctx, db, job{runAt: time.Now().Add(200 * time.Millisecond), to: "b", label: "b", body: "soon", srcID: "2"})
	if id, _ := add(ctx, db, job{runAt: time.Now(), to: "b", label: "b", body: "soon", srcID: "2"}); id != 0 {
		t.Fatal("a redelivered command must not create a second job")
	}

	done := make(chan job, 1)
	cctx, cancel := context.WithCancel(ctx)
	defer cancel()
	go loop(cctx, db, nil, func(j job) {
		finish(ctx, db, j.id, "sent")
		done <- j
	})
	select {
	case j := <-done:
		if j.id != soon {
			t.Fatalf("ran job %d, want %d", j.id, soon)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("due job did not run")
	}

	if !finish(ctx, db, later, "cancelled") {
		t.Fatal("cancelling a pending job failed")
	}
	if finish(ctx, db, later, "sent") {
		t.Fatal("finishing a cancelled job must be a no-op")
	}
	if p, _ := pending(ctx, db); len(p) != 0 {
		t.Fatalf("%d jobs pending, want 0", len(p))
	}
}
