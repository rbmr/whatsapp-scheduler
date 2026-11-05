package main

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// A job is one scheduled message. The database is the queue: the scheduler only
// ever looks at the earliest pending job, so memory use does not grow with it.
type job struct {
	id            int64
	runAt         time.Time
	to, label     string // recipient JID, and the recipient as the user typed it
	body          string
	status        string // pending, sent, failed, missed or cancelled
	srcID, srcMsg string // the command message that created the job, quoted in replies
}

const cols = `id, run_at, to_jid, label, body, status, src_id, src_msg`

const schema = `CREATE TABLE IF NOT EXISTS wsched_jobs (
	id      INTEGER PRIMARY KEY,
	run_at  INTEGER NOT NULL,
	to_jid  TEXT NOT NULL,
	label   TEXT NOT NULL,
	body    TEXT NOT NULL,
	status  TEXT NOT NULL DEFAULT 'pending',
	src_id  TEXT NOT NULL,
	src_msg TEXT NOT NULL
);
CREATE UNIQUE INDEX IF NOT EXISTS wsched_jobs_src ON wsched_jobs (src_id);`

func scan(r interface{ Scan(...any) error }) (j job, err error) {
	var runAt int64
	err = r.Scan(&j.id, &runAt, &j.to, &j.label, &j.body, &j.status, &j.srcID, &j.srcMsg)
	j.runAt = time.Unix(runAt, 0)
	return
}

// add stores j and returns its id, or 0 if the command message was already handled.
func add(ctx context.Context, db *sql.DB, j job) (int64, error) {
	res, err := db.ExecContext(ctx,
		`INSERT OR IGNORE INTO wsched_jobs (run_at, to_jid, label, body, src_id, src_msg) VALUES (?, ?, ?, ?, ?, ?)`,
		j.runAt.Unix(), j.to, j.label, j.body, j.srcID, j.srcMsg)
	if err != nil {
		return 0, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return 0, nil
	}
	return res.LastInsertId()
}

func get(ctx context.Context, db *sql.DB, id int64) (job, error) {
	return scan(db.QueryRowContext(ctx, `SELECT `+cols+` FROM wsched_jobs WHERE id = ?`, id))
}

// finish sets the final status of a pending job. It reports false if the job
// was not pending any more, for example because it was cancelled meanwhile.
func finish(ctx context.Context, db *sql.DB, id int64, status string) bool {
	res, err := db.ExecContext(ctx, `UPDATE wsched_jobs SET status = ? WHERE id = ? AND status = 'pending'`, status, id)
	if err != nil {
		return false
	}
	n, _ := res.RowsAffected()
	return n == 1
}

func pending(ctx context.Context, db *sql.DB) (out []job, err error) {
	rows, err := db.QueryContext(ctx, `SELECT `+cols+` FROM wsched_jobs WHERE status = 'pending' ORDER BY run_at, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		j, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, j)
	}
	return out, rows.Err()
}

// loop calls run for each job when it is due, sleeping on one timer in between.
// Send on wake after adding or cancelling a job. run must finish the job.
func loop(ctx context.Context, db *sql.DB, wake <-chan struct{}, run func(job)) {
	timer := time.NewTimer(time.Hour)
	defer timer.Stop()
	for {
		wait := time.Hour
		j, err := scan(db.QueryRowContext(ctx,
			`SELECT `+cols+` FROM wsched_jobs WHERE status = 'pending' ORDER BY run_at, id LIMIT 1`))
		switch {
		case errors.Is(err, sql.ErrNoRows):
		case err != nil:
			wait = 10 * time.Second
		case time.Until(j.runAt) <= 0:
			run(j)
			if cur, err := get(ctx, db, j.id); err != nil || cur.status == "pending" {
				wait = 5 * time.Second // run could not record an outcome; do not spin
			} else {
				continue
			}
		default:
			wait = time.Until(j.runAt)
		}
		timer.Reset(wait)
		select {
		case <-ctx.Done():
			return
		case <-wake:
		case <-timer.C:
		}
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
	}
}
