package internal

import (
	"bufio"
	"net"
	"strings"
	"testing"
	"time"
)

// fakeSMTP serves one SMTP session on a local port and sends the received
// DATA to the returned channel. With silent it accepts the connection but
// never greets, like a wedged MTA.
func fakeSMTP(t *testing.T, silent bool) (string, <-chan string) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	data := make(chan string, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		if silent {
			time.Sleep(5 * time.Second)
			return
		}
		serveSMTP(conn, data)
	}()
	return ln.Addr().String(), data
}

func serveSMTP(conn net.Conn, data chan<- string) {
	r := bufio.NewReader(conn)
	reply := func(s string) { _, _ = conn.Write([]byte(s + "\r\n")) }
	reply("220 fake ESMTP")
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		switch cmd := strings.ToUpper(strings.TrimSpace(line)); {
		case strings.HasPrefix(cmd, "EHLO"):
			reply("250 fake")
		case strings.HasPrefix(cmd, "DATA"):
			reply("354 go ahead")
			var sb strings.Builder
			for {
				l, err := r.ReadString('\n')
				if err != nil || l == ".\r\n" {
					break
				}
				sb.WriteString(l)
			}
			data <- sb.String()
			reply("250 queued")
		case strings.HasPrefix(cmd, "QUIT"):
			reply("221 bye")
			return
		default:
			reply("250 ok")
		}
	}
}

func TestSendMail(t *testing.T) {
	addr, data := fakeSMTP(t, false)
	if err := sendMail(addr, 5*time.Second, "from@example.org", "to@example.org", []byte("Subject: hi\r\n\r\nbody\r\n")); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-data:
		if !strings.Contains(got, "Subject: hi") || !strings.Contains(got, "body") {
			t.Fatalf("server received %q", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("server received no mail")
	}
}

// An MTA that accepts but never answers fails the mail after the timeout
// instead of blocking the run (and its lock) forever.
func TestSendMailSilentServerTimesOut(t *testing.T) {
	addr, _ := fakeSMTP(t, true)
	start := time.Now()
	err := sendMail(addr, 200*time.Millisecond, "from@example.org", "to@example.org", []byte("x"))
	if err == nil {
		t.Fatal("silent server: want an error")
	}
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Fatalf("sendMail blocked for %v", elapsed)
	}
}

func TestSendMailErrors(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	closed := ln.Addr().String()
	_ = ln.Close()
	for name, addr := range map[string]string{"no port": "localhost", "refused": closed} {
		t.Run(name, func(t *testing.T) {
			if err := sendMail(addr, time.Second, "a@example.org", "b@example.org", []byte("x")); err == nil {
				t.Fatalf("sendMail(%q): want an error", addr)
			}
		})
	}
	addr, _ := fakeSMTP(t, false)
	if err := sendMail(addr, time.Second, "a@example.org", "b@example.org\r\nRCPT TO:<c@example.org>", []byte("x")); err == nil {
		t.Fatal("recipient with CRLF: want an error")
	}
}

// notify skips SMTP entirely when it is disabled.
func TestNotifyDisabled(t *testing.T) {
	if err := notify(config{SMTPDisable: true, SMTPServer: "localhost"}, "s", "b"); err != nil {
		t.Fatal(err)
	}
}
