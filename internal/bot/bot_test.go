package bot

import (
	"context"
	"errors"
	"testing"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"github.com/barakov-dot/tgproxy-panel/internal/models"
	"github.com/barakov-dot/tgproxy-panel/internal/service"
)

// fakeSender is a Sender that records every call instead of hitting the
// real Telegram API.
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

const testAdminID = int64(999)

func testBot(sender Sender, st *fakeStore, ap *fakeApplier, defaultAutoIssue bool) *Bot {
	return newBot(sender, testActions(st, ap, defaultAutoIssue), testAdminID, "proxy.example.com")
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

func TestHandleUpdate_DispatchesStartCommand(t *testing.T) {
	sender := &fakeSender{}
	b := testBot(sender, newFakeStore(), &fakeApplier{}, true)

	update := tgbotapi.Update{Message: &tgbotapi.Message{
		Chat: &tgbotapi.Chat{ID: 7},
		Text: "/start",
		Entities: []tgbotapi.MessageEntity{
			{Type: "bot_command", Offset: 0, Length: 6},
		},
	}}
	b.handleUpdate(context.Background(), update)

	if len(sender.sent) != 1 {
		t.Fatalf("sent = %d messages, want 1 (start greeting)", len(sender.sent))
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

func TestHandleGetProxy_AutoIssue(t *testing.T) {
	sender := &fakeSender{}
	st := newFakeStore()
	ap := &fakeApplier{}
	b := testBot(sender, st, ap, true)

	b.handleCallback(context.Background(), getProxyCallback(200, 200))

	var texts, photos int
	for _, c := range sender.sent {
		switch c.(type) {
		case tgbotapi.MessageConfig:
			texts++
		case tgbotapi.PhotoConfig:
			photos++
		}
	}
	if texts != 1 || photos != 1 {
		t.Fatalf("sent %d text messages and %d photos, want 1 and 1", texts, photos)
	}
	if len(sender.requests) != 1 {
		t.Fatalf("requests = %d, want 1 (callback ack)", len(sender.requests))
	}
	if _, ok := sender.requests[0].(tgbotapi.CallbackConfig); !ok {
		t.Errorf("request[0] type = %T, want CallbackConfig", sender.requests[0])
	}
}

func TestHandleGetProxy_PendingNotifiesAdmin(t *testing.T) {
	sender := &fakeSender{}
	st := newFakeStore()
	ap := &fakeApplier{}
	b := testBot(sender, st, ap, false)

	b.handleCallback(context.Background(), getProxyCallback(201, 201))

	var userMsgs, adminMsgs int
	for _, c := range sender.sent {
		msg, ok := c.(tgbotapi.MessageConfig)
		if !ok {
			continue
		}
		switch msg.ChatID {
		case 201:
			userMsgs++
		case testAdminID:
			adminMsgs++
			kb, ok := msg.ReplyMarkup.(tgbotapi.InlineKeyboardMarkup)
			if !ok || len(kb.InlineKeyboard) != 1 || len(kb.InlineKeyboard[0]) != 2 {
				t.Fatalf("admin notification keyboard = %+v", msg.ReplyMarkup)
			}
			approveData := kb.InlineKeyboard[0][0].CallbackData
			denyData := kb.InlineKeyboard[0][1].CallbackData
			if approveData == nil || *approveData != callbackApprovePrefix+"201" {
				t.Errorf("approve callback data = %v, want %q", approveData, callbackApprovePrefix+"201")
			}
			if denyData == nil || *denyData != callbackDenyPrefix+"201" {
				t.Errorf("deny callback data = %v, want %q", denyData, callbackDenyPrefix+"201")
			}
		}
	}
	if userMsgs != 1 {
		t.Errorf("user messages = %d, want 1", userMsgs)
	}
	if adminMsgs != 1 {
		t.Errorf("admin messages = %d, want 1", adminMsgs)
	}
	if len(ap.calls) != 0 {
		t.Errorf("applier calls = %d, want 0 (auto-issue off)", len(ap.calls))
	}
}

func adminDecisionCallback(data string, adminID int64) *tgbotapi.CallbackQuery {
	return &tgbotapi.CallbackQuery{
		ID:      "cb2",
		From:    &tgbotapi.User{ID: adminID, FirstName: "Admin"},
		Message: &tgbotapi.Message{MessageID: 5, Chat: &tgbotapi.Chat{ID: adminID}},
		Data:    data,
	}
}

func TestHandleAdminDecision_Approve(t *testing.T) {
	sender := &fakeSender{}
	st := newFakeStore()
	ap := &fakeApplier{}
	b := testBot(sender, st, ap, false)
	ctx := context.Background()

	// Seed a pending request the way RequestProxy would.
	if _, err := b.actions.RequestProxy(ctx, 300, nil, nil, nil); err != nil {
		t.Fatalf("RequestProxy() error = %v", err)
	}
	sender.sent = nil // discard the admin notification from RequestProxy's caller (not sent here)

	b.handleCallback(ctx, adminDecisionCallback(callbackApprovePrefix+"300", testAdminID))

	var clearedKeyboard, adminConfirm, userMsg, userPhoto bool
	for _, c := range sender.sent {
		switch v := c.(type) {
		case tgbotapi.EditMessageReplyMarkupConfig:
			clearedKeyboard = true
		case tgbotapi.MessageConfig:
			if v.ChatID == testAdminID {
				adminConfirm = true
			} else if v.ChatID == 300 {
				userMsg = true
			}
		case tgbotapi.PhotoConfig:
			if v.BaseFile.BaseChat.ChatID == 300 {
				userPhoto = true
			}
		}
	}
	if !clearedKeyboard {
		t.Error("admin keyboard was not cleared")
	}
	if !adminConfirm {
		t.Error("admin confirmation message not sent")
	}
	if !userMsg || !userPhoto {
		t.Errorf("user notification incomplete: msg=%v photo=%v", userMsg, userPhoto)
	}
	if st.users[300].Status != models.StatusActive {
		t.Errorf("user status = %v, want active", st.users[300].Status)
	}
	if len(sender.requests) != 1 {
		t.Fatalf("requests = %d, want 1 (callback ack)", len(sender.requests))
	}
}

func TestHandleAdminDecision_Deny(t *testing.T) {
	sender := &fakeSender{}
	st := newFakeStore()
	ap := &fakeApplier{}
	b := testBot(sender, st, ap, false)
	ctx := context.Background()

	if _, err := b.actions.RequestProxy(ctx, 301, nil, nil, nil); err != nil {
		t.Fatalf("RequestProxy() error = %v", err)
	}
	sender.sent = nil

	b.handleCallback(ctx, adminDecisionCallback(callbackDenyPrefix+"301", testAdminID))

	if st.users[301].Status != models.StatusDenied {
		t.Errorf("user status = %v, want denied", st.users[301].Status)
	}
	var userDenied bool
	for _, c := range sender.sent {
		if msg, ok := c.(tgbotapi.MessageConfig); ok && msg.ChatID == 301 {
			userDenied = true
		}
	}
	if !userDenied {
		t.Error("denied user was not notified")
	}
}

func TestHandleAdminDecision_NotAuthorized(t *testing.T) {
	sender := &fakeSender{}
	st := newFakeStore()
	ap := &fakeApplier{}
	b := testBot(sender, st, ap, false)
	ctx := context.Background()

	if _, err := b.actions.RequestProxy(ctx, 302, nil, nil, nil); err != nil {
		t.Fatalf("RequestProxy() error = %v", err)
	}

	const intruderID = int64(555)
	b.handleCallback(ctx, adminDecisionCallback(callbackApprovePrefix+"302", intruderID))

	if st.users[302].Status != models.StatusPending {
		t.Fatalf("user status = %v, want still pending (unauthorized approve must be a no-op)", st.users[302].Status)
	}
	if len(sender.sent) != 0 {
		t.Errorf("sent = %+v, want no messages sent to an unauthorized approver", sender.sent)
	}
	if len(sender.requests) != 1 {
		t.Fatalf("requests = %d, want exactly 1 (the alert)", len(sender.requests))
	}
	cb, ok := sender.requests[0].(tgbotapi.CallbackConfig)
	if !ok || !cb.ShowAlert {
		t.Errorf("request[0] = %+v, want an alert CallbackConfig", sender.requests[0])
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
