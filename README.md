# whatsapp-scheduler

Schedule WhatsApp messages by sending commands to yourself. A small worker that
you host yourself runs as a linked device on your account, watches your
"message yourself" chat, and sends messages at the time you asked.

It is built on [whatsmeow](https://github.com/tulir/whatsmeow) and ships as one
static Go binary with an embedded SQLite database. It has no web server and
no browser.

> **Unofficial.** This uses WhatsApp's web protocol through a third-party
> library. WhatsApp can in principle restrict accounts that use unofficial
> clients. Use it for low-volume personal messages at your own risk.

## Installation

Download the binary for your system from the
[latest release](https://github.com/rbmr/whatsapp-scheduler/releases/latest)
and make it executable:

```sh
chmod +x whatsapp-scheduler-linux-amd64
```

Or build it yourself with Go:

```sh
go install github.com/rbmr/whatsapp-scheduler@latest
```

## Setup

The worker has two commands. Run both from the same directory: it keeps its
WhatsApp session and your schedule in `whatsapp-scheduler.db` there. That file
is a login to your WhatsApp account, so treat it like a password.

**1. Pair** it with your account, using your own phone number:

```sh
whatsapp-scheduler pair +31612345678
```

It prints a code. Enter it in WhatsApp under Settings > Linked devices > Link a
device > Link with phone number instead.

**2. Run** it, and keep it running (systemd, tmux, Docker, whatever you like):

```sh
whatsapp-scheduler run
```

Dates and times use the system timezone, or the `TZ` environment variable if set.

## Usage

Open your own chat ("Message yourself") and send commands. Only messages that
start with `!` are read, so the chat stays usable for notes. Every reply from
the worker starts with 🤖.

| Command | Does |
|---|---|
| `!at <date> <time> <to>: <message>` | Send a message at a point in time. |
| `!in <n><unit> <to>: <message>` | Send a message after a duration. |
| `!list` | List the scheduled messages. |
| `!cancel <id>` | Cancel a scheduled message. |
| `!ping` | Check that the worker is up. |
| `!help` | List the commands. |

`<date>` is `today`, `tomorrow`, `DD-MM` (the next such date) or `DD-MM-YYYY`.
`<time>` is `HH:MM` or `HH:MM:SS`, 24 hours. `<n><unit>` is a whole number
followed by `s`, `m`, `h` or `d`, such as `30m`. `<to>` is a contact name or a
phone number with country code and a `+`. Everything after the first `:` is the
message.

```
!at tomorrow 09:30 Finn: Happy birthday!
!at 24-12 18:00 +31612345678: Merry Christmas
!in 2h Finn: On my way
```

The worker replies to your command when it schedules the message, and again
when the message is sent. If it was down when a message was due, it does not send
it late: it tells you the message was missed.
