package main

import (
	"log"
	"net/http"
	"net/smtp"

	"github.com/davecgh/go-spew/spew"
	"github.com/jordan-wright/email"
	"google.golang.org/appengine"
	"google.golang.org/appengine/mail"
)

func sendEmail(subject string, body interface{}, filepath string) {
	recipient := c.GmailRecipient
	if recipient == "" {
		recipient = c.GmailSender
	}
	if recipient == "" || c.GmailSender == "" || c.GmailPasswd == "" {
		log.Println("Skipping email notification (Gmail credentials or recipient not configured)")
		return
	}

	e := email.NewEmail()
	e.From = c.GmailSender
	e.To = []string{recipient}

	e.Subject = c.ENV + " " + subject
	e.HTML = []byte(spew.Sprintln(body))
	if filepath != "" {
		att, err := e.AttachFile(filepath)
		if err != nil {
			log.Println("Warning: Failed to attach file to email:", err, spew.Sdump(att))
		}
	}
	err := e.Send("smtp.gmail.com:587", smtp.PlainAuth("", c.GmailSender, c.GmailPasswd, "smtp.gmail.com"))
	if err != nil {
		log.Println("Error sending email:", err)
	}
}

func gaeEmail(r *http.Request) (*mail.Message, error) {
	ctx := appengine.NewContext(r)
	recipient := c.GmailRecipient
	if recipient == "" {
		recipient = c.GmailSender
	}

	msg := &mail.Message{
		Sender:  c.GmailSender,
		To:      []string{recipient},
		Subject: "Notification",
		Body:    "Trading execution alert.",
	}

	if err := mail.Send(ctx, msg); err != nil {
		return msg, err
	}
	return msg, nil
}
