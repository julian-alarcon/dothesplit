package service

import (
	"fmt"
	"strings"
)

// Plain-text email templates.
//
// Why plain prose with no `<a>` tags:
//   - mail clients with proportional default fonts (Proton Mail, Gmail web)
//     turn any monospace decoration into garbled spacing,
//   - Brevo's free plan rewrites every `<a href>` to a tracking redirector
//     even on transactional mail; bare URLs in plain-text bodies are left
//     alone, so users can copy them safely,
//   - the security model uses a 6-digit code the user types — there are no
//     links to click in the verification flow at all.

// TemplateVars carries the values rendered into each template. Not every
// field is used by every template; absent values render as empty strings.
type TemplateVars struct {
	DisplayName string
	Code        string
	GroupName   string
	ActorName   string
	Description string
	Amount      string // already formatted with currency
	NewEmail    string
	// WebOrigin is the public base URL of this instance (e.g.
	// "https://split.example.com"). Rendered as bare text in templates that
	// reference the app — never wrapped in `<a>` so Brevo can't rewrite it.
	WebOrigin string
}

// emailHeader is the masthead every template starts with. Underlining the
// product name with `=` is the convention used by RFC docs, Gnu mail, and
// most plain-text transactional email — it survives every renderer because
// it's just text and doesn't depend on font metrics.
const emailHeader = "DoTheSplit\n==========\n\n"

// RenderVerifyRegister builds the registration verification email.
func RenderVerifyRegister(v TemplateVars) (subject, body string) {
	subject = "Confirm your DoTheSplit registration"
	body = emailHeader +
		"Hi " + nameOr(v.DisplayName, "there") + ",\n\n" +
		"Welcome to DoTheSplit! Your registration code is:\n\n" +
		"    " + v.Code + "\n\n" +
		"Open the verification page in DoTheSplit and paste this code to\n" +
		"finish creating your account. The code expires in 15 minutes.\n\n" +
		"If you did not register, you can ignore this email.\n"
	return
}

// RenderWelcome is sent right after a successful verification.
func RenderWelcome(v TemplateVars) (subject, body string) {
	subject = "Welcome to DoTheSplit"
	body = emailHeader +
		"Hi " + nameOr(v.DisplayName, "there") + ",\n\n" +
		"Your account is now active. Thanks for joining DoTheSplit!\n\n" +
		"By default we keep your inbox quiet: no activity emails until you\n" +
		"opt in. You can turn on email notifications for recurring expenses,\n" +
		"settlements, or being added to a group from the notification\n" +
		"preferences page in your account settings.\n\n" +
		"Have fun splitting bills!\n"
	return
}

// RenderVerifyChangeEmail is sent to the *new* address during change-email.
func RenderVerifyChangeEmail(v TemplateVars) (subject, body string) {
	subject = "Confirm your new DoTheSplit email address"
	body = emailHeader +
		"Hi " + nameOr(v.DisplayName, "there") + ",\n\n" +
		"Use this code to confirm " + v.NewEmail + " as your new\n" +
		"DoTheSplit email address:\n\n" +
		"    " + v.Code + "\n\n" +
		"The code expires in 15 minutes. Until you confirm it, your old\n" +
		"address keeps working.\n\n" +
		"If you did not request this change, ignore this email and consider\n" +
		"changing your DoTheSplit password.\n"
	return
}

// RenderRecurringRun notifies a member that a recurring expense fired.
func RenderRecurringRun(v TemplateVars) (subject, body string) {
	subject = "New recurring expense in " + v.GroupName
	body = emailHeader +
		"Hi " + nameOr(v.DisplayName, "there") + ",\n\n" +
		"A recurring expense was just added to " + v.GroupName + ":\n\n" +
		"    " + v.Description + "\n" +
		"    " + v.Amount + "\n\n" +
		notificationsFooter()
	return
}

// RenderSettlementCreated notifies a group member that a settlement was recorded.
func RenderSettlementCreated(v TemplateVars) (subject, body string) {
	subject = "Settlement recorded in " + v.GroupName
	body = emailHeader +
		"Hi " + nameOr(v.DisplayName, "there") + ",\n\n" +
		v.ActorName + " was settled in " + v.GroupName + ":\n\n" +
		"    " + v.Description + "\n" +
		"    " + v.Amount + "\n\n" +
		notificationsFooter()
	return
}

// RenderSmtpTest is the body sent by the admin "Send test email" button.
// Plain text, bare URL — Brevo only rewrites links in `<a href>` tags, so a
// raw URL in a text/plain body stays intact and admins can copy it.
func RenderSmtpTest(v TemplateVars) (subject, body string) {
	subject = "DoTheSplit SMTP test"
	origin := nameOr(v.WebOrigin, "your DoTheSplit instance")
	body = emailHeader +
		"Hi " + nameOr(v.DisplayName, "admin") + ",\n\n" +
		"This is a test email from " + origin + ".\n" +
		"If you can read this, outbound mail is working end to end:\n" +
		"server connection, authentication, and message delivery.\n\n" +
		"You can dismiss this email.\n"
	return
}

// RenderGroupMemberAdded notifies the added user that they were added to a group.
func RenderGroupMemberAdded(v TemplateVars) (subject, body string) {
	subject = "You were added to " + v.GroupName
	body = emailHeader +
		"Hi " + nameOr(v.DisplayName, "there") + ",\n\n" +
		v.ActorName + " added you to the group " + v.GroupName + " on\n" +
		"DoTheSplit. You can start logging expenses with the group right away.\n\n" +
		notificationsFooter()
	return
}

func notificationsFooter() string {
	return "--\n" +
		"You are receiving this because you opted in to notifications.\n" +
		"Manage them from your account's notification settings.\n"
}

func nameOr(name, fallback string) string {
	if strings.TrimSpace(name) == "" {
		return fallback
	}
	return name
}

// rfc5322Headers builds a minimal RFC-5322 header block. The body's first
// non-whitespace bytes decide Content-Type: a "<" starts an HTML payload,
// anything else is plain text. Currently every template renders plain text,
// but the auto-detection stays so future HTML templates Just Work.
func rfc5322Headers(from, to, subject, body string) string {
	contentType := "text/plain; charset=utf-8"
	if isHTMLBody(body) {
		contentType = "text/html; charset=utf-8"
	}
	return fmt.Sprintf(
		"From: %s\r\nTo: %s\r\nSubject: %s\r\nMIME-Version: 1.0\r\nContent-Type: %s\r\nContent-Transfer-Encoding: 8bit\r\n\r\n",
		from, to, subject, contentType,
	)
}

func isHTMLBody(body string) bool {
	trimmed := strings.TrimLeft(body, " \t\r\n")
	return strings.HasPrefix(trimmed, "<")
}
