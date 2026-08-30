package bot

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"github.com/barakov-dot/tgproxy-panel-q/internal/config"
	"github.com/barakov-dot/tgproxy-panel-q/internal/models"
	"github.com/barakov-dot/tgproxy-panel-q/internal/qrcode"
	"github.com/barakov-dot/tgproxy-panel-q/internal/service"
)

const qrSize = 320

const (
	callbackGetProxy      = "get_proxy"
	callbackApprovePrefix = "approve:"
	callbackDenyPrefix    = "deny:"
)

// Sender is the subset of *tgbotapi.BotAPI the bot calls. Tests inject a fake.
type Sender interface {
	Send(c tgbotapi.Chattable) (tgbotapi.Message, error)
	Request(c tgbotapi.Chattable) (*tgbotapi.APIResponse, error)
}

// UpdatesSource yields Telegram updates (real API or test fake).
type UpdatesSource interface {
	GetUpdatesChan(cfg tgbotapi.UpdateConfig) tgbotapi.UpdatesChannel
	StopReceivingUpdates()
}

// Bot wires Telegram updates around internal/service.
type Bot struct {
	sender          Sender
	updates         UpdatesSource
	svc             *service.Service
	adminTelegramID int64
}

// New builds a Bot from config and the shared service layer.
func New(cfg *config.Config, svc *service.Service) (*Bot, error) {
	api, err := tgbotapi.NewBotAPI(cfg.BotToken)
	if err != nil {
		return nil, fmt.Errorf("bot: init: %w", err)
	}
	return newBot(api, api, svc, cfg.AdminTelegramID), nil
}

func newBot(sender Sender, updates UpdatesSource, svc *service.Service, adminTelegramID int64) *Bot {
	return &Bot{
		sender:          sender,
		updates:         updates,
		svc:             svc,
		adminTelegramID: adminTelegramID,
	}
}

// Run starts long polling and blocks until ctx is cancelled.
func (b *Bot) Run(ctx context.Context) error {
	uCfg := tgbotapi.NewUpdate(0)
	uCfg.Timeout = 60
	updates := b.updates.GetUpdatesChan(uCfg)

	for {
		select {
		case <-ctx.Done():
			b.updates.StopReceivingUpdates()
			return nil
		case update, ok := <-updates:
			if !ok {
				return nil
			}
			go b.handleUpdate(ctx, update)
		}
	}
}

// SendProxyLink implements service.BotSender: sends the proxy link and QR photo.
func (b *Bot) SendProxyLink(_ context.Context, telegramID int64, link string) error {
	b.send(htmlMessage(telegramID, existingProxyText(link)))
	png, err := qrcode.GeneratePNG(link, qrSize)
	if err != nil {
		return fmt.Errorf("bot: generate qr: %w", err)
	}
	if _, err := b.sender.Send(tgbotapi.NewPhoto(telegramID, tgbotapi.FileBytes{Name: "proxy.png", Bytes: png})); err != nil {
		return fmt.Errorf("bot: send photo: %w", err)
	}
	return nil
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

	res, err := b.svc.Request(ctx, from.ID, ptrIfNotEmpty(from.UserName),
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

	userID, err := strconv.ParseInt(strings.TrimPrefix(cq.Data, prefix), 10, 64)
	if err != nil {
		slog.Error("bot: bad callback data", "data", cq.Data, "error", err)
		b.ackCallback(cq.ID)
		return
	}

	actor := service.ActorAdmin(cq.From.ID)
	if approve {
		u, err := b.svc.Approve(ctx, userID, actor)
		if err != nil {
			b.reportAdminActionError(cq, err)
			return
		}
		b.clearAdminKeyboard(cq)
		b.send(htmlMessage(cq.Message.Chat.ID, approvedAdminConfirmText(u.DisplayName())))
		b.sendProxy(u.TelegramID, u, issuedProxyText)
	} else {
		u, err := b.svc.Deny(ctx, userID, actor)
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

func (b *Bot) sendProxy(chatID int64, u *models.User, textFor func(link string) string) {
	if u == nil || u.Secret == nil {
		slog.Error("bot: sendProxy called without a secret", "telegram_id", chatIDOrZero(u))
		b.send(htmlMessage(chatID, genericErrorText))
		return
	}

	link := b.svc.GetProxyLink(u)
	b.send(htmlMessage(chatID, textFor(link)))

	png, err := qrcode.GeneratePNG(link, qrSize)
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
	idStr := strconv.FormatInt(u.ID, 10)
	msg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(btnApprove, callbackApprovePrefix+idStr),
			tgbotapi.NewInlineKeyboardButtonData(btnDeny, callbackDenyPrefix+idStr),
		),
	)
	b.send(msg)
}

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
