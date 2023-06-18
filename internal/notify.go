package internal

import (
	"fmt"
	"log"
	"net/smtp"
)

func notify(conf config, subject, body string) error {
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

	return smtp.SendMail(conf.SMTPServer, nil, conf.EmailFrom,
		[]string{conf.EmailTo}, []byte(message))
}

func notifyError(conf config, err error) {
	if err := notify(conf, fmt.Sprintf("GOGIOS: An error occured: %v", err), err.Error()); err != nil {
		log.Println("error: ", err)
	}
}
