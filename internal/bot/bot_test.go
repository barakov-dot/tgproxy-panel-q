package bot

import (
	"context"
	"errors"
	"strconv"
	"testing"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"github.com/barakov-dot/tgproxy-panel-q/internal/models"
	"github.com/barakov-dot/tgproxy-panel-q/internal/service"
)

type fakeSender struct {
	sent       []tgbotapi.Chattable
	sendErr    error
	requests   []tgbotapi.Chattable
	requestErr error
}

func (f *fakeSender) Send(c tgbotapi.Chattable) (tgbotapi.Message, error) {
	f.sent = append(f.sent, c)
	if f.sendErr != nil {
		return tgbotapi.Message{}, f.sendErr
	}
	return tgbotapi.Message{}, nil
}

func (f *fakeSender) Request(c tgbotapi.Chattable) (*tgbotapi.APIResponse, error) {
	f.requests = append(f.requests, c)
	if f.requestErr != nil {
		return nil, f.requestErr
	}
	return &tgbotapi.APIResponse{Ok: true}, nil
}

type fakeUpdates struct{}

func (fakeUpdates) GetUpdatesChan(_ tgbotapi.UpdateConfig) tgbotapi.UpdatesChannel {
	ch := make(chan tgbotapi.Update)
	close(ch)
	return ch
}

func (fakeUpdates) StopReceivingUpdates() {}

const testAdminID = int64(999)

func testBot(sender Sender, st *fakeStore, ap *fakeApplier, defaultAutoIssue bool) *Bot {
	return newBot(sender, fakeUpdates{}, testService(st, ap, defaultAutoIssue), testAdminID)
}

func TestHandleUpdate_Routing(t *testing.T) {
	tests := []struct {
		name     string
		update   tgbotapi.Update
		wantSent int
		wantReq  int
	}{
		{
			name: "start command",
			update: tgbotapi.Update{Message: &tgbotapi.Message{
				Chat: &tgbotapi.Chat{ID: 7},
				Text: "/start",
				Entities: []tgbotapi.MessageEntity{
					{Type: "bot_command", Offset: 0, Length: 6},
				},
			}},
			wantSent: 1,
		},
		{
			name: "unknown text ignored",
			update: tgbotapi.Update{Message: &tgbotapi.Message{
				Chat: &tgbotapi.Chat{ID: 7},
				Text: "hello",
			}},
			wantSent: 0,
		},
		{
			name: "unknown callback acked",
			update: tgbotapi.Update{CallbackQuery: &tgbotapi.CallbackQuery{
				ID:   "cb0",
				Data: "unknown",
			}},
			wantReq: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sender := &fakeSender{}
			b := testBot(sender, newFakeStore(), &fakeApplier{}, true)
			b.handleUpdate(context.Background(), tt.update)

			if len(sender.sent) != tt.wantSent {
				t.Errorf("sent = %d, want %d", len(sender.sent), tt.wantSent)
			}
			if len(sender.requests) != tt.wantReq {
				t.Errorf("requests = %d, want %d", len(sender.requests), tt.wantReq)
			}
		})
	}
}

func TestHandleStart(t *testing.T) {
	sender := &fakeSender{}
	b := testBot(sender, newFakeStore(), &fakeApplier{}, true)

	b.handleStart(&tgbotapi.Message{Chat: &tgbotapi.Chat{ID: 42}})

	if len(sender.sent) != 1 {
		t.Fatalf("sent = %d messages, want 1", len(sender.sent))
	}
	msg, ok := sender.sent[0].(tgbotapi.MessageConfig)
	if !ok {
		t.Fatalf("sent[0] type = %T, want MessageConfig", sender.sent[0])
	}
	if msg.ChatID != 42 {
		t.Errorf("ChatID = %d, want 42", msg.ChatID)
	}
	if msg.ParseMode != tgbotapi.ModeHTML {
		t.Errorf("ParseMode = %q, want HTML", msg.ParseMode)
	}
	kb, ok := msg.ReplyMarkup.(tgbotapi.InlineKeyboardMarkup)
	if !ok {
		t.Fatalf("ReplyMarkup type = %T, want InlineKeyboardMarkup", msg.ReplyMarkup)
	}
	if len(kb.InlineKeyboard) != 1 || len(kb.InlineKeyboard[0]) != 1 {
		t.Fatalf("unexpected keyboard shape: %+v", kb.InlineKeyboard)
	}
	btn := kb.InlineKeyboard[0][0]
	if btn.Text != btnGetProxy || btn.CallbackData == nil || *btn.CallbackData != callbackGetProxy {
		t.Errorf("button = %+v", btn)
	}
}

func getProxyCallback(telegramID int64, chatID int64) *tgbotapi.CallbackQuery {
	return &tgbotapi.CallbackQuery{
		ID:      "cb1",
		From:    &tgbotapi.User{ID: telegramID, FirstName: "Test", UserName: "testuser"},
		Message: &tgbotapi.Message{MessageID: 1, Chat: &tgbotapi.Chat{ID: chatID}},
		Data:    callbackGetProxy,
	}
}

func TestHandleGetProxy_Outcomes(t *testing.T) {
	tests := []struct {
		name        string
		autoIssue   bool
		seed        func(st *fakeStore)
		wantTexts   int
		wantPhotos  int
		wantAdmin   int
		wantApplier int
	}{
		{
			name:        "auto issue",
			autoIssue:   true,
			wantTexts:   1,
			wantPhotos:  1,
			wantApplier: 1,
		},
		{
			name:      "pending notifies admin",
			autoIssue: false,
			wantTexts: 1,
			wantAdmin: 1,
		},
		{
			name:      "already pending",
			autoIssue: false,
			seed: func(st *fakeStore) {
				st.nextID = 1
				st.users[201] = &models.User{ID: 1, TelegramID: 201, Status: models.StatusPending}
			},
			wantTexts: 1,
		},
		{
			name:      "already active resends proxy",
			autoIssue: false,
			seed: func(st *fakeStore) {
				secret := "deadbeefdeadbeefdeadbeefdeadbeef"
				st.nextID = 1
				st.users[202] = &models.User{
					ID: 1, TelegramID: 202, Status: models.StatusActive, Secret: &secret,
				}
			},
			wantTexts:  1,
			wantPhotos: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sender := &fakeSender{}
			st := newFakeStore()
			if tt.seed != nil {
				tt.seed(st)
			}
			ap := &fakeApplier{}
			b := testBot(sender, st, ap, tt.autoIssue)

			telegramID := int64(200)
			if tt.seed != nil && tt.name == "already pending" {
				telegramID = 201
			}
			if tt.name == "already active resends proxy" {
				telegramID = 202
			}

			b.handleCallback(context.Background(), getProxyCallback(telegramID, telegramID))

			var texts, photos, adminMsgs int
			for _, c := range sender.sent {
				switch v := c.(type) {
				case tgbotapi.MessageConfig:
					switch v.ChatID {
					case testAdminID:
						adminMsgs++
					default:
						texts++
					}
				case tgbotapi.PhotoConfig:
					photos++
				}
			}
			if texts != tt.wantTexts {
				t.Errorf("text messages = %d, want %d", texts, tt.wantTexts)
			}
			if photos != tt.wantPhotos {
				t.Errorf("photos = %d, want %d", photos, tt.wantPhotos)
			}
			if adminMsgs != tt.wantAdmin {
				t.Errorf("admin messages = %d, want %d", adminMsgs, tt.wantAdmin)
			}
			if len(ap.calls) != tt.wantApplier {
				t.Errorf("applier calls = %d, want %d", len(ap.calls), tt.wantApplier)
			}
			if tt.wantAdmin > 0 {
				assertAdminKeyboard(t, sender)
			}
			if len(sender.requests) != 1 {
				t.Errorf("callback acks = %d, want 1", len(sender.requests))
			}
		})
	}
}

func assertAdminKeyboard(t *testing.T, sender *fakeSender) {
	t.Helper()
	for _, c := range sender.sent {
		msg, ok := c.(tgbotapi.MessageConfig)
		if !ok || msg.ChatID != testAdminID {
			continue
		}
		kb, ok := msg.ReplyMarkup.(tgbotapi.InlineKeyboardMarkup)
		if !ok || len(kb.InlineKeyboard) != 1 || len(kb.InlineKeyboard[0]) != 2 {
			t.Fatalf("admin notification keyboard = %+v", msg.ReplyMarkup)
		}
		approveData := kb.InlineKeyboard[0][0].CallbackData
		denyData := kb.InlineKeyboard[0][1].CallbackData
		if approveData == nil || !hasPrefix(*approveData, callbackApprovePrefix) {
			t.Errorf("approve callback data = %v", approveData)
		}
		if denyData == nil || !hasPrefix(*denyData, callbackDenyPrefix) {
			t.Errorf("deny callback data = %v", denyData)
		}
	}
}

func hasPrefix(s, prefix string) bool {
	return len(s) > len(prefix) && s[:len(prefix)] == prefix
}

func adminDecisionCallback(data string, adminID int64) *tgbotapi.CallbackQuery {
	return &tgbotapi.CallbackQuery{
		ID:      "cb2",
		From:    &tgbotapi.User{ID: adminID, FirstName: "Admin"},
		Message: &tgbotapi.Message{MessageID: 5, Chat: &tgbotapi.Chat{ID: adminID}},
		Data:    data,
	}
}

func TestHandleAdminDecision(t *testing.T) {
	tests := []struct {
		name           string
		approve        bool
		adminID        int64
		setup          func(ctx context.Context, b *Bot, st *fakeStore) string
		wantStatus     models.UserStatus
		wantUserNotify bool
		wantUserPhoto  bool
		wantUnauthorized bool
	}{
		{
			name:    "approve",
			approve: true,
			adminID: testAdminID,
			setup: func(ctx context.Context, b *Bot, st *fakeStore) string {
				res, err := b.svc.Request(ctx, 300, nil, nil, nil)
				if err != nil {
					t.Fatalf("Request() error = %v", err)
				}
				return callbackApprovePrefix + itoa(res.User.ID)
			},
			wantStatus:     models.StatusActive,
			wantUserNotify: true,
			wantUserPhoto:  true,
		},
		{
			name:    "deny",
			approve: false,
			adminID: testAdminID,
			setup: func(ctx context.Context, b *Bot, st *fakeStore) string {
				res, err := b.svc.Request(ctx, 301, nil, nil, nil)
				if err != nil {
					t.Fatalf("Request() error = %v", err)
				}
				return callbackDenyPrefix + itoa(res.User.ID)
			},
			wantStatus:     models.StatusDenied,
			wantUserNotify: true,
		},
		{
			name:    "not authorized",
			approve: true,
			adminID: 555,
			setup: func(ctx context.Context, b *Bot, st *fakeStore) string {
				res, err := b.svc.Request(ctx, 302, nil, nil, nil)
				if err != nil {
					t.Fatalf("Request() error = %v", err)
				}
				return callbackApprovePrefix + itoa(res.User.ID)
			},
			wantStatus:       models.StatusPending,
			wantUnauthorized: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sender := &fakeSender{}
			st := newFakeStore()
			ap := &fakeApplier{}
			b := testBot(sender, st, ap, false)
			ctx := context.Background()

			data := tt.setup(ctx, b, st)
			sender.sent = nil
			sender.requests = nil

			b.handleCallback(ctx, adminDecisionCallback(data, tt.adminID))

			u := st.users[300]
			if tt.name == "deny" {
				u = st.users[301]
			}
			if tt.name == "not authorized" {
				u = st.users[302]
			}
			if u.Status != tt.wantStatus {
				t.Errorf("user status = %v, want %v", u.Status, tt.wantStatus)
			}

			if tt.wantUnauthorized {
				if len(sender.sent) != 0 {
					t.Errorf("sent = %d, want 0 for unauthorized", len(sender.sent))
				}
				if len(sender.requests) != 1 {
					t.Fatalf("requests = %d, want 1 alert", len(sender.requests))
				}
				cb, ok := sender.requests[0].(tgbotapi.CallbackConfig)
				if !ok || !cb.ShowAlert {
					t.Errorf("request[0] = %+v, want alert CallbackConfig", sender.requests[0])
				}
				return
			}

			var userMsg, userPhoto, adminConfirm, clearedKeyboard bool
			for _, c := range sender.sent {
				switch v := c.(type) {
				case tgbotapi.EditMessageReplyMarkupConfig:
					clearedKeyboard = true
				case tgbotapi.MessageConfig:
					if v.ChatID == testAdminID {
						adminConfirm = true
					} else {
						userMsg = true
					}
				case tgbotapi.PhotoConfig:
					userPhoto = true
				}
			}
			if !clearedKeyboard {
				t.Error("admin keyboard was not cleared")
			}
			if !adminConfirm {
				t.Error("admin confirmation message not sent")
			}
			if tt.wantUserNotify && !userMsg {
				t.Error("user was not notified")
			}
			if tt.wantUserPhoto && !userPhoto {
				t.Error("user QR photo not sent")
			}
			if len(sender.requests) != 1 {
				t.Errorf("callback acks = %d, want 1", len(sender.requests))
			}
		})
	}
}

func TestReportError_ApplyFailedGetsDistinctMessage(t *testing.T) {
	sender := &fakeSender{}
	b := testBot(sender, newFakeStore(), &fakeApplier{}, true)

	b.reportError(1, service.ErrIssueFailed)
	b.reportError(2, errors.New("some other failure"))

	if len(sender.sent) != 2 {
		t.Fatalf("sent = %d, want 2", len(sender.sent))
	}
	m1 := sender.sent[0].(tgbotapi.MessageConfig)
	m2 := sender.sent[1].(tgbotapi.MessageConfig)
	if m1.Text != applyFailedText {
		t.Errorf("ErrIssueFailed text = %q, want %q", m1.Text, applyFailedText)
	}
	if m2.Text != genericErrorText {
		t.Errorf("generic error text = %q, want %q", m2.Text, genericErrorText)
	}
}

func TestSendProxyLink(t *testing.T) {
	sender := &fakeSender{}
	b := testBot(sender, newFakeStore(), &fakeApplier{}, true)

	link := "https://t.me/webproxy?server=proxy.example.com&secret=abc"
	if err := b.SendProxyLink(context.Background(), 42, link); err != nil {
		t.Fatalf("SendProxyLink() error = %v", err)
	}
	if len(sender.sent) != 2 {
		t.Fatalf("sent = %d, want 2 (text + photo)", len(sender.sent))
	}
	if _, ok := sender.sent[0].(tgbotapi.MessageConfig); !ok {
		t.Errorf("sent[0] type = %T, want MessageConfig", sender.sent[0])
	}
	if _, ok := sender.sent[1].(tgbotapi.PhotoConfig); !ok {
		t.Errorf("sent[1] type = %T, want PhotoConfig", sender.sent[1])
	}
}

func itoa(n int64) string {
	return strconv.FormatInt(n, 10)
}
