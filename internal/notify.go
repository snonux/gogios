package internal

import (
	"crypto/tls"
	"fmt"
	"log"
	"net"
	"net/smtp"
	"time"
)

// smtpTimeout bounds one whole mail delivery (dial, dialogue and data). The
// stdlib smtp.SendMail has no timeout at all: an MTA that accepts the
// connection but never answers kept Gogios, and the run lock it holds,
// waiting forever.
const smtpTimeout = 60 * time.Second

func notify(conf config, subject, body string) error {
	if conf.SMTPDisable {
		log.Println("Notification disabled")
		return nil
	}
	log.Println("notify", subject, body)

	headers := map[string]string{
		"From":         conf.EmailFrom,
		"To":           conf.EmailTo,
		"Subject":      subject,
		"MIME-Version": "1.0",
		"Content-Type": "text/plain; charset=\"utf-8\"",
	}

	header := ""
	for k, v := range headers {
		header += fmt.Sprintf("%s: %s\r\n", k, v)
	}

	message := header + "\r\n" + body
	log.Println("Using SMTP server", conf.SMTPServer)

	return sendMail(conf.SMTPServer, smtpTimeout, conf.EmailFrom, conf.EmailTo, []byte(message))
}

func notifyError(conf config, err error) {
	if err := notify(conf, fmt.Sprintf("GOGIOS: An error occured: %v", err), err.Error()); err != nil {
		log.Println("error: ", err)
	}
}

// sendMail delivers msg like smtp.SendMail (EHLO, STARTTLS when offered, no
// auth), but under one deadline of timeout for the whole exchange.
func sendMail(addr string, timeout time.Duration, from, to string, msg []byte) error {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("smtp server %q: %w", addr, err)
	}
	conn, err := net.DialTimeout("tcp", addr, timeout)
	if err != nil {
		return fmt.Errorf("smtp dial %s: %w", addr, err)
	}
	// The deadline covers every read and write that follows, including
	// the server greeting that smtp.NewClient waits for.
	if err := conn.SetDeadline(time.Now().Add(timeout)); err != nil {
		_ = conn.Close()
		return fmt.Errorf("smtp deadline: %w", err)
	}
	c, err := smtp.NewClient(conn, host)
	if err != nil {
		_ = conn.Close()
		return fmt.Errorf("smtp greeting from %s: %w", addr, err)
	}
	defer func() { _ = c.Close() }()

	if err := smtpDeliver(c, host, from, to, msg); err != nil {
		return fmt.Errorf("smtp %s: %w", addr, err)
	}
	return nil
}

// smtpDeliver runs the SMTP dialogue of one mail on an established client.
func smtpDeliver(c *smtp.Client, host, from, to string, msg []byte) error {
	if err := c.Hello("localhost"); err != nil {
		return err
	}
	if ok, _ := c.Extension("STARTTLS"); ok {
		if err := c.StartTLS(&tls.Config{ServerName: host}); err != nil {
			return err
		}
	}
	if err := c.Mail(from); err != nil {
		return err
	}
	if err := c.Rcpt(to); err != nil {
		return err
	}
	w, err := c.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write(msg); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return c.Quit()
}
