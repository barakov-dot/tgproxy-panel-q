package bot

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"github.com/barakov-dot/tgproxy-panel/internal/applier"
	"github.com/barakov-dot/tgproxy-panel/internal/config"
	"github.com/barakov-dot/tgproxy-panel/internal/models"
	"github.com/barakov-dot/tgproxy-panel/internal/qrcode"
	"github.com/barakov-dot/tgproxy-panel/internal/service"
	"github.com/barakov-dot/tgproxy-panel/internal/store"
)

// qrSize is the pixel width/height of QR codes sent in chat — large enough
// to scan comfortably on a phone screen without the photo feeling oversized.
const qrSize = 320

const (
	callbackGetProxy      = "get_proxy"
	callbackApprovePrefix = "approve:"
	callbackDenyPrefix    = "deny:"
)

// Sender is the subset of *tgbotapi.BotAPI the bot package calls: Send for
// messages/photos, Request for calls that don't return a Message (callback
// acknowledgements, reply-markup-only edits). *tgbotapi.BotAPI satisfies
// this directly, so tests can inject a fake that records calls instead of
// hitting the real Telegram API.
type Sender interface {
	Send(c tgbotapi.Chattable) (tgbotapi.Message, error)
	Request(c tgbotapi.Chattable) (*tgbotapi.APIResponse, error)
}

// Bot wires Telegram-specific plumbing (updates, keyboards, HTML messages)
// around internal/service.Actions, the framework-agnostic orchestration
// layer shared with internal/httpserver.
type Bot struct {
	sender          Sender
	actions         *service.Actions
	adminTelegramID int64
	proxyHost       string
}

func newBot(sender Sender, actions *service.Actions, adminTelegramID int64, proxyHost string) *Bot {
	return &Bot{
		sender:          sender,
		actions:         actions,
		adminTelegramID: adminTelegramID,
		proxyHost:       proxyHost,
	}
}

// Run starts long polling and blocks, dispatching updates until ctx is
// cancelled — designed so a future cmd/tgproxy-panel/main.go can
// `go bot.Run(ctx, ...)` alongside the HTTP server and shut both down
// together on the same signal.
func Run(ctx context.Context, cfg *config.Config, st *store.Store, ap *applier.Applier) error {
	api, err := tgbotapi.NewBotAPI(cfg.BotToken)
	if err != nil {
		return fmt.Errorf("bot: init: %w", err)
	}

	actions := service.New(st, ap, cfg.AutoIssue)
	b := newBot(api, actions, cfg.AdminTelegramID, cfg.TproxyHostname)

	uCfg := tgbotapi.NewUpdate(0)
	uCfg.Timeout = 60
	updates := api.GetUpdatesChan(uCfg)

	for {
		select {
		case <-ctx.Done():
			api.StopReceivingUpdates()
			return nil
		case update, ok := <-updates:
			if !ok {
				return nil
			}
			// Each update runs in its own goroutine: issuing a profile
			// involves a systemctl restart plus polling /readyz (tens of
			// seconds, see internal/applier), which must not stall the poll
			// loop from picking up other users' updates in the meantime.
			go b.handleUpdate(ctx, update)
		}
	}
}

func (b *Bot) handleUpdate(ctx context.Context, update tgbotapi.Update) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("bot: panic handling update", "recover", r)
		}
	}()

	switch {
	case update.Message != nil && update.Message.IsCommand() && update.Message.Command() == "start":
		b.handleStart(update.Message)
	case update.CallbackQuery != nil:
		b.handleCallback(ctx, update.CallbackQuery)
	}
}

func (b *Bot) handleStart(msg *tgbotapi.Message) {
	reply := tgbotapi.NewMessage(msg.Chat.ID, startText)
	reply.ParseMode = tgbotapi.ModeHTML
	reply.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(btnGetProxy, callbackGetProxy),
		),
	)
	b.send(reply)
}

func (b *Bot) handleCallback(ctx context.Context, cq *tgbotapi.CallbackQuery) {
	switch {
	case cq.Data == callbackGetProxy:
		b.handleGetProxy(ctx, cq)
		b.ackCallback(cq.ID)
	case strings.HasPrefix(cq.Data, callbackApprovePrefix):
		b.handleAdminDecision(ctx, cq, callbackApprovePrefix, true)
	case strings.HasPrefix(cq.Data, callbackDenyPrefix):
		b.handleAdminDecision(ctx, cq, callbackDenyPrefix, false)
	default:
		b.ackCallback(cq.ID)
	}
}

func (b *Bot) handleGetProxy(ctx context.Context, cq *tgbotapi.CallbackQuery) {
	if cq.From == nil || cq.Message == nil {
		return
	}
	from := cq.From
	chatID := cq.Message.Chat.ID

	res, err := b.actions.RequestProxy(ctx, from.ID, ptrIfNotEmpty(from.UserName),
		ptrIfNotEmpty(from.FirstName), ptrIfNotEmpty(from.LastName))
	if err != nil {
		b.reportError(chatID, err)
		return
	}

	switch res.Outcome {
	case service.OutcomeAlreadyActive:
		b.sendProxy(chatID, res.User, existingProxyText)
	case service.OutcomeIssued:
		b.sendProxy(chatID, res.User, issuedProxyText)
	case service.OutcomePendingCreated:
		b.send(htmlMessage(chatID, pendingText))
		b.notifyAdmin(res.User)
	case service.OutcomeAlreadyPending:
		b.send(htmlMessage(chatID, alreadyPendingText))
	}
}

func (b *Bot) handleAdminDecision(ctx context.Context, cq *tgbotapi.CallbackQuery, prefix string, approve bool) {
	if cq.From == nil || cq.Message == nil {
		return
	}
	if cq.From.ID != b.adminTelegramID {
		if _, err := b.sender.Request(tgbotapi.NewCallbackWithAlert(cq.ID, notAuthorizedAlertText)); err != nil {
			slog.Error("bot: alert callback failed", "error", err)
		}
		return
	}

	telegramID, err := strconv.ParseInt(strings.TrimPrefix(cq.Data, prefix), 10, 64)
	if err != nil {
		slog.Error("bot: bad callback data", "data", cq.Data, "error", err)
		b.ackCallback(cq.ID)
		return
	}

	if approve {
		u, err := b.actions.Approve(ctx, telegramID, service.ActorAdmin(cq.From.ID))
		if err != nil {
			b.reportAdminActionError(cq, err)
			return
		}
		b.clearAdminKeyboard(cq)
		b.send(htmlMessage(cq.Message.Chat.ID, approvedAdminConfirmText(u.DisplayName())))
		b.sendProxy(u.TelegramID, u, issuedProxyText)
	} else {
		u, err := b.actions.Deny(ctx, telegramID, service.ActorAdmin(cq.From.ID))
		if err != nil {
			b.reportAdminActionError(cq, err)
			return
		}
		b.clearAdminKeyboard(cq)
		b.send(htmlMessage(cq.Message.Chat.ID, deniedAdminConfirmText(u.DisplayName())))
		b.send(htmlMessage(u.TelegramID, deniedUserText))
	}
	b.ackCallback(cq.ID)
}

// sendProxy sends the profile link as HTML text (Telegram auto-links the
// https://t.me/... URL) followed by the same link rendered as a scannable
// QR photo.
func (b *Bot) sendProxy(chatID int64, u *models.User, textFor func(link string) string) {
	if u == nil || u.Secret == nil {
		slog.Error("bot: sendProxy called without a secret", "telegram_id", chatIDOrZero(u))
		b.send(htmlMessage(chatID, genericErrorText))
		return
	}

	link := proxyLink(b.proxyHost, *u.Secret)
	b.send(htmlMessage(chatID, textFor(link)))

	png, err := qrcode.PNG(link, qrSize)
	if err != nil {
		slog.Error("bot: generate qr failed", "error", err)
		return
	}
	b.send(tgbotapi.NewPhoto(chatID, tgbotapi.FileBytes{Name: "proxy.png", Bytes: png}))
}

func chatIDOrZero(u *models.User) int64 {
	if u == nil {
		return 0
	}
	return u.TelegramID
}

func (b *Bot) notifyAdmin(u *models.User) {
	msg := tgbotapi.NewMessage(b.adminTelegramID, adminNotifyText(u.DisplayName(), u.TelegramID))
	msg.ParseMode = tgbotapi.ModeHTML
	idStr := strconv.FormatInt(u.TelegramID, 10)
	msg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(btnApprove, callbackApprovePrefix+idStr),
			tgbotapi.NewInlineKeyboardButtonData(btnDeny, callbackDenyPrefix+idStr),
		),
	)
	b.send(msg)
}

// clearAdminKeyboard removes the ✅/❌ buttons from the admin's notification
// message so it can't be acted on twice. It only strips the reply markup —
// deliberately not rewriting cq.Message.Text, since Telegram returns that
// field already stripped of the original HTML tags, and reconstructing an
// HTML-parse-mode message from it without re-escaping user-controlled
// content (a requester's display name) would be fragile. The confirmation
// text is sent as a separate message instead (see approvedAdminConfirmText/
// deniedAdminConfirmText callers).
func (b *Bot) clearAdminKeyboard(cq *tgbotapi.CallbackQuery) {
	empty := tgbotapi.NewInlineKeyboardMarkup()
	edit := tgbotapi.NewEditMessageReplyMarkup(cq.Message.Chat.ID, cq.Message.MessageID, empty)
	if _, err := b.sender.Send(edit); err != nil {
		slog.Error("bot: clear admin keyboard failed", "error", err)
	}
}

func (b *Bot) reportError(chatID int64, err error) {
	slog.Error("bot: request failed", "error", err)
	text := genericErrorText
	if errors.Is(err, service.ErrIssueFailed) {
		text = applyFailedText
	}
	b.send(htmlMessage(chatID, text))
}

func (b *Bot) reportAdminActionError(cq *tgbotapi.CallbackQuery, err error) {
	slog.Error("bot: admin decision failed", "error", err)
	text := genericErrorText
	if errors.Is(err, service.ErrIssueFailed) {
		text = applyFailedText
	}
	b.send(htmlMessage(cq.Message.Chat.ID, text))
	b.ackCallback(cq.ID)
}

func (b *Bot) ackCallback(id string) {
	if _, err := b.sender.Request(tgbotapi.NewCallback(id, "")); err != nil {
		slog.Error("bot: ack callback failed", "error", err)
	}
}

func (b *Bot) send(c tgbotapi.Chattable) {
	if _, err := b.sender.Send(c); err != nil {
		slog.Error("bot: send failed", "error", err)
	}
}

func htmlMessage(chatID int64, text string) tgbotapi.MessageConfig {
	m := tgbotapi.NewMessage(chatID, text)
	m.ParseMode = tgbotapi.ModeHTML
	return m
}

func ptrIfNotEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
