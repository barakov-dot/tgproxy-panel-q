package bot

import (
	"fmt"
	"html"
)

// Message copy (plan.md §6: emoji icons, HTML formatting, inline
// keyboards). Kept in one place so the Russian tone stays consistent.
const (
	btnGetProxy = "🔑 Получить прокси"
	btnApprove  = "✅ Одобрить"
	btnDeny     = "❌ Отклонить"

	startText = "👋 Привет! Я бот выдачи доступа к прокси.\n\n" +
		"Нажми кнопку ниже, чтобы получить свою ссылку."

	pendingText = "⏳ Заявка отправлена администратору на рассмотрение. " +
		"Как только её одобрят, я пришлю ссылку и QR-код."

	alreadyPendingText = "⏳ Ваша заявка уже на рассмотрении, ожидайте решения администратора."

	deniedUserText = "❌ Ваша заявка на доступ отклонена администратором."

	genericErrorText = "⚠️ Что-то пошло не так, попробуйте ещё раз позже."

	applyFailedText = "⚠️ Прокси записан, но применить его на сервере не удалось. " +
		"Мы уже разбираемся — попробуйте нажать кнопку ещё раз чуть позже или обратитесь к администратору."

	notAuthorizedAlertText = "Эта кнопка не для вас."

	adminNotifyTemplate = "🆕 <b>Новая заявка на прокси</b>\n\n" +
		"👤 %s\n🆔 <code>%d</code>"

	existingProxyTemplate = "🔒 У вас уже есть прокси:\n\n%s"
	issuedProxyTemplate   = "🎉 Прокси выдан!\n\n%s"

	approvedAdminConfirmTemplate = "✅ Заявка от %s одобрена, пользователю отправлена ссылка."
	deniedAdminConfirmTemplate   = "❌ Заявка от %s отклонена."
)

// escapeHTML escapes text embedded inside an HTML-parse-mode Telegram
// message (user-controlled names, and the "&" in proxy links), per
// Telegram's HTML formatting rules.
func escapeHTML(s string) string {
	return html.EscapeString(s)
}

// proxyLink builds the tappable t.me proxy link (plan.md §5's exact
// format).
func proxyLink(host, secret string) string {
	return fmt.Sprintf("https://t.me/webproxy?server=%s&secret=%s", host, secret)
}

func adminNotifyText(displayName string, telegramID int64) string {
	return fmt.Sprintf(adminNotifyTemplate, escapeHTML(displayName), telegramID)
}

func existingProxyText(link string) string {
	return fmt.Sprintf(existingProxyTemplate, escapeHTML(link))
}

func issuedProxyText(link string) string {
	return fmt.Sprintf(issuedProxyTemplate, escapeHTML(link))
}

func approvedAdminConfirmText(displayName string) string {
	return fmt.Sprintf(approvedAdminConfirmTemplate, escapeHTML(displayName))
}

func deniedAdminConfirmText(displayName string) string {
	return fmt.Sprintf(deniedAdminConfirmTemplate, escapeHTML(displayName))
}
