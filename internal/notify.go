package internal

import (
	"fmt"
	"log"
	"net/smtp"
)

func notify(config config, subject, body string) error {
	log.Println("notify", subject, body)

	headers := make(map[string]string)
	headers["From"] = config.EmailFrom
	headers["To"] = config.EmailTo
	headers["Subject"] = subject
	headers["MIME-Version"] = "1.0"
	headers["Content-Type"] = "text/plain; charset=\"utf-8\""

	header := ""
	for k, v := range headers {
		header += fmt.Sprintf("%s: %s\r\n", k, v)
	}

	message := header + "\r\n" + body
	log.Println("Using SMTP server", config.SMTPServer)

	return smtp.SendMail(config.SMTPServer, nil, config.EmailFrom,
		[]string{config.EmailTo}, []byte(message))
}

func notifyError(config config, err error) error {
	return notify(config, fmt.Sprintf("GOGIOS: An error occured: %v", err), err.Error())
}
