// Command whatsapp-scheduler sends WhatsApp messages at a time you choose. You
// control it with messages to yourself in WhatsApp.
package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"
	"os/signal"
	"runtime/debug"
	"syscall"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types/events"
	waLog "go.mau.fi/whatsmeow/util/log"
	_ "modernc.org/sqlite"
)

const usage = `usage:
  whatsapp-scheduler pair <phone>   link this worker to your WhatsApp (phone like +31612345678)
  whatsapp-scheduler run            start the worker

The WhatsApp session and the schedule are kept in whatsapp-scheduler.db in the
current directory. Dates and times use the system timezone, or TZ if set.`

func main() {
	if len(os.Args) < 2 || os.Args[1] == "pair" && len(os.Args) != 3 || os.Args[1] != "pair" && os.Args[1] != "run" {
		fmt.Fprintln(os.Stderr, usage)
		os.Exit(2)
	}
	debug.SetMemoryLimit(40 << 20) // keep the heap small on low-power machines

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	db, err := sql.Open("sqlite", "file:whatsapp-scheduler.db"+
		"?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)")
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()
	logger := waLog.Stdout("whatsmeow", "WARN", false)
	container := sqlstore.NewWithDB(db, "sqlite", logger)
	if err := container.Upgrade(ctx); err != nil {
		log.Fatal(err)
	}
	device, err := container.GetFirstDevice(ctx)
	if err != nil {
		log.Fatal(err)
	}
	cli := whatsmeow.NewClient(device, logger)

	if os.Args[1] == "pair" {
		pair(ctx, cli, os.Args[2])
		return
	}
	if cli.Store.ID == nil {
		log.Fatal("not paired yet: run `whatsapp-scheduler pair <phone>` first")
	}
	if _, err := db.ExecContext(ctx, schema); err != nil {
		log.Fatal(err)
	}
	b := &bot{ctx: ctx, cli: cli, db: db, wake: make(chan struct{}, 1), started: time.Now()}
	cli.AddEventHandler(b.handle)
	if err := cli.Connect(); err != nil {
		log.Fatal(err)
	}
	defer cli.Disconnect()
	log.Printf("running as %s, timezone %s", cli.Store.ID.ToNonAD(), time.Local)
	loop(ctx, db, b.wake, b.run)
}

func pair(ctx context.Context, cli *whatsmeow.Client, phone string) {
	if cli.Store.ID != nil {
		log.Fatalf("already paired as %s", cli.Store.ID.ToNonAD())
	}
	ready, paired := make(chan struct{}, 1), make(chan struct{})
	cli.AddEventHandler(func(evt any) {
		switch evt.(type) {
		case *events.QR:
			select {
			case ready <- struct{}{}:
			default:
			}
		case *events.PairSuccess:
			close(paired)
		}
	})
	if err := cli.Connect(); err != nil {
		log.Fatal(err)
	}
	defer cli.Disconnect()
	select {
	case <-ready:
	case <-ctx.Done():
		return
	}
	code, err := cli.PairPhone(ctx, phone, true, whatsmeow.PairClientChrome, "Chrome (Linux)")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("Pairing code: %s\nEnter it in WhatsApp: Settings > Linked devices > Link a device > Link with phone number instead.\n", code)
	select {
	case <-paired:
		time.Sleep(3 * time.Second) // let the first sync settle
		fmt.Println("Paired. Start the worker with: whatsapp-scheduler run")
	case <-ctx.Done():
	}
}
